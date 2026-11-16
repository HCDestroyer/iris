package mensajeria

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Reconexión temporal (contrato con Argos §7.3) y desafío de identidad (§7.5) desde una conversación.
// Los dos los pide a Argos el asistente de IA cuando atiende solo (contratos/conversaciones.md §17.8);
// Argos decide. Iris guarda lo pedido y lo decidido, nunca el dato de identidad.

// Motivos de una reconexión temporal: solo se pide cuando nadie de la empresa puede revisar la boleta.
const (
	ReconexionFueraDeHorario = "fuera_de_horario"
	ReconexionSinPersonal    = "sin_personal"
)

// EntradaReconexion identifica la conversación.
type EntradaReconexion struct {
	ID int64 `json:"conversacion_id"`
}

// ReconexionConv es la reconexión pedida y lo que decidió Argos.
type ReconexionConv struct {
	ID             uuid.UUID  `json:"id"`
	Estado         string     `json:"estado" enum:"aceptada,rechazada"`
	Motivo         string     `json:"motivo" enum:"fuera_de_horario,sin_personal"`
	ComprobanteID  int64      `json:"comprobanteId"`
	PromesaArgosID *int64     `json:"promesaArgosId" nullable:"true"`
	Hasta          *time.Time `json:"hasta" nullable:"true"`
	Repetida       bool       `json:"-"`
}

func aReconexion(f mensajeriabd.MensajeriaReconexione) ReconexionConv {
	return ReconexionConv{ID: f.ID, Estado: f.Estado, Motivo: f.Motivo, ComprobanteID: f.ComprobanteID, PromesaArgosID: f.PromesaArgosID, Hasta: f.Hasta}
}

// PedirReconexion pide a Argos la reconexión temporal de la última boleta registrada de la
// conversación: valida en una transacción, llama a Argos fuera de ella y guarda el resultado en otra.
type PedirReconexion struct {
	m      *Modulo
	previo *auditoria.Caso[EntradaReconexion, previaReconexion]
	final  *auditoria.Caso[finalReconexion, ReconexionConv]
}

type previaReconexion struct {
	repetida *ReconexionConv
	a        alcanceConv
	boleta   mensajeriabd.MensajeriaBoleta
	servicio *int64
	cliente  int64
	motivo   string
}

type finalReconexion struct {
	EntradaReconexion
	pre     previaReconexion
	estado  string
	promesa int64
	hasta   time.Time
	detalle string
}

// MarshalJSON: lo auditado.
func (e finalReconexion) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{"conversacion_id": e.ID, "boleta_id": e.pre.boleta.ID, "comprobante_id": e.pre.boleta.ComprobanteID,
		"motivo": e.pre.motivo, "estado": e.estado})
}

func nuevoPedirReconexion(m *Modulo) *PedirReconexion {
	const caso = "conversaciones.pedir_reconexion"
	k := &PedirReconexion{m: m}
	k.previo = auditoria.Nuevo(auditoria.Caso[EntradaReconexion, previaReconexion]{
		Accion: caso, Permiso: nucleo.PermConversacionesPedirReconexion, AuditarExito: sinAuditar[previaReconexion],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaReconexion) (previaReconexion, error) {
			a, err := alcanceAccionDe(ctx, caso)
			if err != nil {
				return previaReconexion{}, err
			}
			q := mensajeriabd.New(t)
			c, err := convVisible(ctx, q, a, in.ID, false)
			if err != nil {
				return previaReconexion{}, err
			}
			if lectura, err := soloLecturaConv(ctx, q, c); err != nil || lectura {
				if err == nil {
					err = conversacionSoloLectura()
				}
				return previaReconexion{}, err
			}
			if c.ClienteID == nil {
				return previaReconexion{}, reglaConv("sin_cliente", "Asocia la conversación a un cliente de Argos antes de pedir una reconexión")
			}
			// Mueve el estado de un servicio: exige la verificación fuerte.
			if verificacionDe(c) != VerificacionFuerte {
				return previaReconexion{}, reglaConv("verificacion_requerida", "La reconexión temporal exige verificar la identidad del cliente")
			}
			b, err := q.UltimaBoletaConv(ctx, mensajeriabd.UltimaBoletaConvParams{ConversacionID: c.ID, ClienteID: *c.ClienteID, Empresa: empresaDeConv(c)})
			if errors.Is(err, pgx.ErrNoRows) || (err == nil && (b.ComprobanteID == nil || b.Estado != "registrada")) {
				return previaReconexion{}, reglaConv("sin_comprobante", "Primero hay que registrar la boleta del cliente y que llegue a caja: todavía no hay un comprobante pendiente")
			}
			if err != nil {
				return previaReconexion{}, err
			}
			if previa, err := q.ReconexionPorComprobante(ctx, *b.ComprobanteID); err == nil {
				r := aReconexion(previa)
				r.Repetida = true
				return previaReconexion{repetida: &r}, nil
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return previaReconexion{}, err
			}
			// Solo cuando nadie puede revisar la boleta pronto: fuera de horario, o sin nadie disponible
			// en la cola de la conversación. El motivo que va a Argos es el real.
			motivo, err := m.motivoReconexion(ctx, q, c)
			if err != nil {
				return previaReconexion{}, err
			}
			if motivo == "" {
				return previaReconexion{}, reglaConv("reconexion_no_corresponde", "Hay personas disponibles para revisar la boleta: la reconexión temporal solo se pide fuera de horario o sin nadie disponible")
			}
			servicio := b.ServicioID
			if servicio == nil {
				servicio = c.ServicioID
			}
			return previaReconexion{a: a, boleta: b, servicio: servicio, cliente: *c.ClienteID, motivo: motivo}, nil
		},
	})
	k.final = auditoria.Nuevo(auditoria.Caso[finalReconexion, ReconexionConv]{
		Accion: caso, Permiso: nucleo.PermConversacionesPedirReconexion,
		Logica: func(ctx context.Context, t *auditoria.Tx, in finalReconexion) (ReconexionConv, error) {
			var r ReconexionConv
			_, err := m.accionConvDe(ctx, t, caso, in.ID, 0, func(a alcanceConv, q *mensajeriabd.Queries, c *mensajeriabd.MensajeriaConversacion) error {
				id, err := uuid.NewV7()
				if err != nil {
					return err
				}
				p := mensajeriabd.InsertarReconexionParams{ID: id, EmpresaID: c.EmpresaID, ConversacionID: c.ID, BoletaID: in.pre.boleta.ID,
					ComprobanteID: *in.pre.boleta.ComprobanteID, ClienteID: in.pre.cliente, ServicioID: in.pre.servicio, Motivo: in.pre.motivo,
					Estado: in.estado, SolicitadaPor: a.usuarioPtr(), PorAsistente: a.asistente}
				if in.estado == "aceptada" {
					p.PromesaArgosID, p.Hasta = &in.promesa, &in.hasta
				}
				if in.detalle != "" {
					p.Detalle = &in.detalle
				}
				f, err := q.InsertarReconexion(ctx, p)
				if errors.Is(err, pgx.ErrNoRows) {
					// Otra petición la guardó primero (misma clave hacia Argos): la misma reconexión.
					if f, err = q.ReconexionPorComprobante(ctx, p.ComprobanteID); err != nil {
						return err
					}
					r = aReconexion(f)
					return nil
				}
				if err != nil {
					return err
				}
				r = aReconexion(f)
				t.RegistrarCambios("reconexion", f.ID, nil, map[string]any{"comprobante_id": f.ComprobanteID, "servicio_id": f.ServicioID, "motivo": f.Motivo,
					"estado": f.Estado, "promesa_argos_id": f.PromesaArgosID})
				nombre := nombrePtr(NombreAsistenteIA)
				if u := a.usuarioPtr(); u != nil {
					nombre = actorNombre(ctx, q, *u)
				}
				if err := m.sistema(ctx, t, c, EventoSistema{Tipo: "reconexion_pedida", ActorNombre: nombre, Detalle: &f.Estado}); err != nil {
					return err
				}
				return t.Emitir("reconexion.solicitada", map[string]any{"reconexion_id": f.ID, "conversacion_id": c.ID, "empresa_id": c.EmpresaID,
					"cliente_id": f.ClienteID, "servicio_id": f.ServicioID, "comprobante_id": f.ComprobanteID, "motivo": f.Motivo, "estado": f.Estado,
					"promesa_argos_id": f.PromesaArgosID})
			})
			return r, err
		},
	})
	return k
}

// motivoReconexion: fuera_de_horario, sin_personal o "" (hay quien revise la boleta).
func (m *Modulo) motivoReconexion(ctx context.Context, q *mensajeriabd.Queries, c mensajeriabd.MensajeriaConversacion) (string, error) {
	cola, err := m.colaDeConv(ctx, q, c)
	if err != nil {
		return "", err
	}
	var horario *int64
	if cola != nil {
		horario = cola.HorarioID
	}
	cal, err := calendarioEfectivo(ctx, q, horario, c.CanalID)
	if err != nil {
		return "", err
	}
	if cal != nil {
		if abierto, _, _ := cal.estado(m.ahora()); !abierto {
			return ReconexionFueraDeHorario, nil
		}
	}
	disp, err := disponiblesDe(ctx, q, empresaDeConv(c), c.ColaID)
	if err != nil {
		return "", err
	}
	if len(disp) == 0 {
		return ReconexionSinPersonal, nil
	}
	return "", nil
}

// Ejecutar pide la reconexión temporal.
func (k *PedirReconexion) Ejecutar(ctx context.Context, in EntradaReconexion) (ReconexionConv, error) {
	rec, ok := k.m.cfg.Argos.(ArgosReconexion)
	if !ok {
		return ReconexionConv{}, noDisponible("Pedir reconexiones temporales a Argos")
	}
	pre, err := k.previo.Ejecutar(ctx, in)
	if err != nil {
		return ReconexionConv{}, err
	}
	if pre.repetida != nil {
		return *pre.repetida, nil
	}
	fin := finalReconexion{EntradaReconexion: in, pre: pre, estado: "aceptada"}
	comprobante := *pre.boleta.ComprobanteID
	fin.promesa, fin.hasta, err = rec.PedirReconexionTemporal(ctx, atencionDe(pre.a, in.ID), comprobante, pre.servicio, pre.motivo,
		"iris:reconexion:"+strconv.FormatInt(comprobante, 10))
	if err != nil {
		// 422 (reconexion_no_permitida) o 409: Argos dijo que no. Es de negocio: se guarda y no se reintenta.
		var er *conector.ErrorRemoto
		if !errors.As(err, &er) || (er.Status != http.StatusUnprocessableEntity && er.Status != http.StatusConflict) {
			return ReconexionConv{}, argos.Problema(err)
		}
		fin.estado, fin.detalle = "rechazada", recortar(er.Tipo, 200)
	}
	return k.final.Ejecutar(context.WithoutCancel(ctx), fin)
}

// Desafío de identidad ------------------------------------------------------------------------------

// verificacionHecha es un intento del desafío de identidad (lo auditado: nunca el dato).
type verificacionHecha struct {
	ConversacionID int64  `json:"conversacion_id"`
	Resultado      string `json:"resultado"`
	// mensajeID y textoEnmascarado: el mensaje del cliente que traía el dato, ya con ***.
	mensajeID        uuid.UUID
	textoEnmascarado string
}

// nuevoRegistrarVerificacion guarda el intento, deja el dato del cliente como *** en su mensaje y, si
// coincidió, sube la verificación de la conversación a «fuerte».
func nuevoRegistrarVerificacion(m *Modulo) *auditoria.Caso[verificacionHecha, struct{}] {
	return auditoria.Nuevo(auditoria.Caso[verificacionHecha, struct{}]{
		Accion: "conversaciones.verificar_identidad", Acceso: auditoria.Autenticado,
		Logica: func(ctx context.Context, t *auditoria.Tx, in verificacionHecha) (struct{}, error) {
			empresa, err := soloAsistente(ctx)
			if err != nil {
				return struct{}{}, err
			}
			q := mensajeriabd.New(t)
			c, err := convDelAsistente(ctx, q, alcanceConv{asistente: true, empresa: empresa}, in.ConversacionID, true)
			if err != nil {
				return struct{}{}, err
			}
			t.FijarEmpresa(empresa)
			t.FijarEntidad("conversacion", c.ID)
			if c.ClienteID == nil {
				return struct{}{}, conversacionClienteNoEncontrada()
			}
			ahora := m.ahora()
			if err := q.InsertarVerificacion(ctx, mensajeriabd.InsertarVerificacionParams{EmpresaID: c.EmpresaID, ConversacionID: c.ID, ClienteID: *c.ClienteID,
				Resultado: in.Resultado, CreadaEn: ahora}); err != nil {
				return struct{}{}, err
			}
			// El dato no se guarda: el mensaje del cliente queda con *** (contrato con Argos §7.5).
			if in.mensajeID != uuid.Nil {
				if err := q.EnmascararTextoMensaje(ctx, mensajeriabd.EnmascararTextoMensajeParams{ID: in.mensajeID, Texto: &in.textoEnmascarado}); err != nil {
					return struct{}{}, err
				}
			}
			if in.Resultado == "coincide" {
				if err := q.FijarIdentidadVerificada(ctx, mensajeriabd.FijarIdentidadVerificadaParams{ID: c.ID, Ahora: &ahora}); err != nil {
					return struct{}{}, err
				}
				c.IdentidadVerificadaEn = &ahora
			}
			if err := m.emitirEventoConv(ctx, t, c, EventoConvActualizada, nil, nil); err != nil {
				return struct{}{}, err
			}
			return struct{}{}, t.Emitir("conversacion.identidad_verificada", map[string]any{"conversacion_id": c.ID, "empresa_id": c.EmpresaID,
				"cliente_id": c.ClienteID, "resultado": in.Resultado, "origen": "asistente"})
		},
	})
}
