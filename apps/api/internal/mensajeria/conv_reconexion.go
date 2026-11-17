package mensajeria

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
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

// ReconexionFueraDeHorario: el único motivo que acepta Argos (§7.3). La reconexión temporal solo se
// pide fuera de horario; en horario hábil, aunque no haya nadie disponible, sigue una persona.
const (
	ReconexionFueraDeHorario = "fuera_de_horario"
	ReconexionPendiente      = "pendiente"
	// MaxIntentosReconexion: llamadas a Argos por una reconexión que no responde, antes de darla por rechazada.
	MaxIntentosReconexion = 10
	// EsperaReintentoReconexion: lo que espera el mantenimiento antes de repetir una pendiente.
	EsperaReintentoReconexion = 2 * time.Minute
)

// EntradaReconexion identifica la conversación.
type EntradaReconexion struct {
	ID int64 `json:"conversacion_id"`
}

// ReconexionConv es la reconexión pedida y lo que decidió Argos.
type ReconexionConv struct {
	ID             uuid.UUID  `json:"id"`
	Estado         string     `json:"estado" enum:"pendiente,aceptada,rechazada"`
	Motivo         string     `json:"motivo" enum:"fuera_de_horario"`
	ComprobanteID  int64      `json:"comprobanteId"`
	PromesaArgosID *int64     `json:"promesaArgosId" nullable:"true"`
	Hasta          *time.Time `json:"hasta" nullable:"true"`
	Repetida       bool       `json:"-"`
}

func aReconexion(f mensajeriabd.MensajeriaReconexione) ReconexionConv {
	return ReconexionConv{ID: f.ID, Estado: f.Estado, Motivo: f.Motivo, ComprobanteID: f.ComprobanteID, PromesaArgosID: f.PromesaArgosID, Hasta: f.Hasta}
}

// PedirReconexion pide a Argos la reconexión temporal de la última boleta registrada de la
// conversación. Valida y deja el intento registrado (fila «pendiente», auditado) en una transacción,
// llama a Argos fuera de ella y guarda lo que decidió en otra. Si la respuesta se pierde, la fila
// queda pendiente y el mantenimiento repite la llamada con la misma clave.
type PedirReconexion struct {
	m         *Modulo
	previo    *auditoria.Caso[EntradaReconexion, previaReconexion]
	final     *auditoria.Caso[finalReconexion, ReconexionConv]
	reintento *auditoria.Caso[finalReconexion, ReconexionConv]
}

type previaReconexion struct {
	repetida *ReconexionConv
	a        alcanceConv
	fila     mensajeriabd.MensajeriaReconexione
}

// MarshalJSON: lo auditado del intento.
func (p previaReconexion) MarshalJSON() ([]byte, error) {
	if p.repetida != nil {
		return json.Marshal(map[string]any{"reconexion_id": p.repetida.ID, "repetida": true})
	}
	return json.Marshal(map[string]any{"reconexion_id": p.fila.ID, "comprobante_id": p.fila.ComprobanteID, "estado": p.fila.Estado})
}

type finalReconexion struct {
	ID      int64
	fila    mensajeriabd.MensajeriaReconexione
	estado  string
	promesa int64
	hasta   time.Time
	detalle string
}

// MarshalJSON: lo auditado.
func (e finalReconexion) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{"conversacion_id": e.ID, "reconexion_id": e.fila.ID, "boleta_id": e.fila.BoletaID,
		"comprobante_id": e.fila.ComprobanteID, "motivo": e.fila.Motivo, "estado": e.estado})
}

func nuevoPedirReconexion(m *Modulo) *PedirReconexion {
	const caso = "conversaciones.pedir_reconexion"
	k := &PedirReconexion{m: m}
	// El intento queda auditado aquí, antes de llamar a Argos.
	k.previo = auditoria.Nuevo(auditoria.Caso[EntradaReconexion, previaReconexion]{
		Accion: caso, Permiso: nucleo.PermConversacionesPedirReconexion,
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
			vig, err := vigenciaFuerte(ctx, q, empresaDeConv(c))
			if err != nil {
				return previaReconexion{}, err
			}
			if verificacionDe(c, m.ahora(), vig) != VerificacionFuerte {
				return previaReconexion{}, reglaConv("verificacion_requerida", "La reconexión temporal exige verificar la identidad del cliente")
			}
			b, err := q.UltimaBoletaConv(ctx, mensajeriabd.UltimaBoletaConvParams{ConversacionID: c.ID, ClienteID: *c.ClienteID, Empresa: empresaDeConv(c)})
			if errors.Is(err, pgx.ErrNoRows) || (err == nil && (b.ComprobanteID == nil || b.Estado != "registrada")) {
				return previaReconexion{}, reglaConv("sin_comprobante", "Primero hay que registrar la boleta del cliente y que llegue a caja: todavía no hay un comprobante pendiente")
			}
			if err != nil {
				return previaReconexion{}, err
			}
			previa, err := q.ReconexionPorComprobante(ctx, *b.ComprobanteID)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return previaReconexion{}, err
			}
			if err == nil && previa.Estado != ReconexionPendiente {
				r := aReconexion(previa)
				r.Repetida = true
				return previaReconexion{repetida: &r}, nil
			}
			if err == nil {
				// Un intento anterior quedó sin respuesta de Argos: se repite con la misma clave.
				return previaReconexion{a: a, fila: previa}, nil
			}
			// Solo fuera de horario: en horario hábil la boleta la revisa una persona.
			fuera, err := m.fueraDeHorarioConv(ctx, q, c)
			if err != nil {
				return previaReconexion{}, err
			}
			if !fuera {
				return previaReconexion{}, reglaConv("reconexion_no_corresponde", "La reconexión temporal solo se pide fuera de horario: en horario hábil la boleta la revisa una persona")
			}
			servicio := b.ServicioID
			if servicio == nil {
				servicio = c.ServicioID
			}
			id, err := uuid.NewV7()
			if err != nil {
				return previaReconexion{}, err
			}
			t.FijarEmpresa(empresaDeConv(c))
			t.FijarEntidad("conversacion", c.ID)
			f, err := q.InsertarReconexion(ctx, mensajeriabd.InsertarReconexionParams{ID: id, EmpresaID: c.EmpresaID, ConversacionID: c.ID, BoletaID: b.ID,
				ComprobanteID: *b.ComprobanteID, ClienteID: *c.ClienteID, ServicioID: servicio, Motivo: ReconexionFueraDeHorario,
				Estado: ReconexionPendiente, SolicitadaPor: a.usuarioPtr(), PorAsistente: a.asistente})
			if errors.Is(err, pgx.ErrNoRows) {
				// Otra petición la registró primero: la misma reconexión (y la misma clave hacia Argos).
				f, err = q.ReconexionPorComprobante(ctx, *b.ComprobanteID)
			}
			if err != nil {
				return previaReconexion{}, err
			}
			return previaReconexion{a: a, fila: f}, nil
		},
	})
	k.final = auditoria.Nuevo(auditoria.Caso[finalReconexion, ReconexionConv]{
		Accion: caso, Permiso: nucleo.PermConversacionesPedirReconexion,
		Logica: func(ctx context.Context, t *auditoria.Tx, in finalReconexion) (ReconexionConv, error) {
			var r ReconexionConv
			_, err := m.accionConvDe(ctx, t, caso, in.ID, 0, func(_ alcanceConv, q *mensajeriabd.Queries, c *mensajeriabd.MensajeriaConversacion) error {
				var err error
				r, err = m.aplicarReconexion(ctx, t, q, c, in)
				return err
			})
			return r, err
		},
	})
	// reintento: el mantenimiento guarda lo que dijo Argos de una reconexión que había quedado pendiente.
	k.reintento = auditoria.Nuevo(auditoria.Caso[finalReconexion, ReconexionConv]{
		Accion: "conversaciones.reconexion_reintento", Acceso: auditoria.Autenticado,
		Logica: func(ctx context.Context, t *auditoria.Tx, in finalReconexion) (ReconexionConv, error) {
			if err := soloSistema(ctx); err != nil {
				return ReconexionConv{}, err
			}
			q := mensajeriabd.New(t)
			c, err := q.ConversacionClienteParaActualizar(ctx, in.ID)
			if err != nil {
				return ReconexionConv{}, err
			}
			t.FijarEmpresa(empresaDeConv(c))
			t.FijarEntidad("conversacion", c.ID)
			return m.aplicarReconexion(ctx, t, q, &c, in)
		},
	})
	return k
}

// aplicarReconexion guarda lo que decidió Argos sobre una reconexión pendiente (una sola vez), deja el
// mensaje de sistema en la conversación y emite reconexion.solicitada.
func (m *Modulo) aplicarReconexion(ctx context.Context, t *auditoria.Tx, q *mensajeriabd.Queries, c *mensajeriabd.MensajeriaConversacion,
	in finalReconexion) (ReconexionConv, error) {
	p := mensajeriabd.ResolverReconexionParams{ID: in.fila.ID, Estado: in.estado}
	if in.estado == "aceptada" {
		p.PromesaArgosID, p.Hasta = &in.promesa, &in.hasta
	}
	if in.detalle != "" {
		p.Detalle = &in.detalle
	}
	f, err := q.ResolverReconexion(ctx, p)
	if errors.Is(err, pgx.ErrNoRows) {
		// Ya la resolvió otra petición (o el mantenimiento): la misma reconexión, sin repetir nada.
		f, err = q.ReconexionPorComprobante(ctx, in.fila.ComprobanteID)
		if err != nil {
			return ReconexionConv{}, err
		}
		return aReconexion(f), nil
	}
	if err != nil {
		return ReconexionConv{}, err
	}
	t.RegistrarCambios("reconexion", f.ID, map[string]any{"estado": ReconexionPendiente}, map[string]any{"comprobante_id": f.ComprobanteID,
		"servicio_id": f.ServicioID, "motivo": f.Motivo, "estado": f.Estado, "promesa_argos_id": f.PromesaArgosID})
	nombre := nombrePtr(NombreAsistenteIA)
	if !f.PorAsistente && f.SolicitadaPor != nil {
		nombre = actorNombre(ctx, q, *f.SolicitadaPor)
	}
	if err := m.sistema(ctx, t, c, EventoSistema{Tipo: "reconexion_pedida", ActorNombre: nombre, Detalle: &f.Estado}); err != nil {
		return ReconexionConv{}, err
	}
	return aReconexion(f), t.Emitir("reconexion.solicitada", map[string]any{"reconexion_id": f.ID, "conversacion_id": c.ID, "empresa_id": c.EmpresaID,
		"cliente_id": f.ClienteID, "servicio_id": f.ServicioID, "comprobante_id": f.ComprobanteID, "motivo": f.Motivo, "estado": f.Estado,
		"promesa_argos_id": f.PromesaArgosID})
}

// fueraDeHorarioConv: la conversación está fuera del horario de su cola (o de su número).
func (m *Modulo) fueraDeHorarioConv(ctx context.Context, q *mensajeriabd.Queries, c mensajeriabd.MensajeriaConversacion) (bool, error) {
	cola, err := m.colaDeConv(ctx, q, c)
	if err != nil {
		return false, err
	}
	var horario *int64
	if cola != nil {
		horario = cola.HorarioID
	}
	cal, err := calendarioEfectivo(ctx, q, horario, c.CanalID)
	if err != nil || cal == nil {
		return false, err
	}
	abierto, _, _ := cal.estado(m.ahora())
	return !abierto, nil
}

// pedirAArgos llama a Argos por una reconexión registrada y traduce la respuesta: aceptada, rechazada
// (422 o 409: de negocio, no se reintenta) o un error (no se sabe: la fila sigue pendiente).
func (k *PedirReconexion) pedirAArgos(ctx context.Context, rec ArgosReconexion, a alcanceConv, f mensajeriabd.MensajeriaReconexione) (finalReconexion, error) {
	fin := finalReconexion{ID: f.ConversacionID, fila: f, estado: "aceptada"}
	_ = mensajeriabd.New(nucleo.BDDe(ctx)).ContarIntentoReconexion(ctx, f.ID)
	var err error
	fin.promesa, fin.hasta, err = rec.PedirReconexionTemporal(ctx, atencionDe(a, f.ConversacionID), f.ComprobanteID, f.ServicioID, f.Motivo,
		"iris:reconexion:"+strconv.FormatInt(f.ComprobanteID, 10))
	if err != nil {
		var er *conector.ErrorRemoto
		if !errors.As(err, &er) || (er.Status != http.StatusUnprocessableEntity && er.Status != http.StatusConflict) ||
			strings.HasSuffix(er.Tipo, "idempotencia_en_curso") {
			return fin, err
		}
		fin.estado, fin.detalle = "rechazada", recortar(er.Tipo, 200)
	}
	return fin, nil
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
	fin, err := k.pedirAArgos(ctx, rec, pre.a, pre.fila)
	if err != nil {
		// No se sabe qué hizo Argos: la fila queda pendiente y el mantenimiento repite con la misma clave.
		return ReconexionConv{}, argos.Problema(err)
	}
	return k.final.Ejecutar(context.WithoutCancel(ctx), fin)
}

// Reintentar repite, con la misma clave, las reconexiones que quedaron pendientes (la respuesta de
// Argos se perdió). Después de MaxIntentosReconexion se da por rechazada (sin_respuesta) para que
// una persona la revise. Corre con el actor sistema (mantenimiento).
func (k *PedirReconexion) Reintentar(ctx context.Context, bd nucleo.BD, antes time.Time, log *slog.Logger) error {
	rec, ok := k.m.cfg.Argos.(ArgosReconexion)
	if !ok {
		return nil
	}
	fs, err := mensajeriabd.New(bd).ReconexionesPendientes(ctx, antes)
	if err != nil {
		return err
	}
	for _, f := range fs {
		var fin finalReconexion
		if f.Intentos >= MaxIntentosReconexion {
			fin = finalReconexion{ID: f.ConversacionID, fila: f, estado: "rechazada", detalle: "sin_respuesta"}
		} else {
			a := alcanceConv{asistente: f.PorAsistente, empresa: derefEmpresa(f.EmpresaID)}
			if f.SolicitadaPor != nil {
				a.usuario = *f.SolicitadaPor
			}
			if fin, err = k.pedirAArgos(nucleo.ConBD(ctx, bd), rec, a, f); err != nil {
				log.WarnContext(ctx, "mensajeria: reintentar reconexión", "reconexion_id", f.ID, "err", err)
				continue
			}
		}
		if _, err := k.reintento.Ejecutar(ctx, fin); err != nil {
			log.WarnContext(ctx, "mensajeria: guardar reconexión reintentada", "reconexion_id", f.ID, "err", err)
		}
	}
	return nil
}

// Desafío de identidad ------------------------------------------------------------------------------

// verificacionHecha es un intento del desafío de identidad (lo auditado: nunca el dato).
type verificacionHecha struct {
	ConversacionID int64  `json:"conversacion_id"`
	Resultado      string `json:"resultado"`
	// ClienteID: el cliente con el que Argos hizo el desafío (la conversación debe seguir siendo suya).
	ClienteID int64 `json:"-"`
	// Detalle: lo que se muestra en el hilo (lista cerrada: coincide, no_coincide, no_disponible,
	// demasiados_intentos).
	Detalle string `json:"detalle"`
	// desde: inicio del periodo de la conversación (los mensajes del cliente a enmascarar).
	desde time.Time
	// reserva: el intento ya reservado en asistente_verificaciones (cuenta para el tope del cliente).
	reserva int64
}

// nuevoRegistrarVerificacion guarda el intento, deja el dato del cliente como *** en su mensaje y, si
// coincidió, sube la verificación de la conversación a «fuerte».
func nuevoRegistrarVerificacion(m *Modulo) *auditoria.Caso[verificacionHecha, bool] {
	return auditoria.Nuevo(auditoria.Caso[verificacionHecha, bool]{
		Accion: "conversaciones.verificar_identidad", Acceso: auditoria.Autenticado,
		Logica: func(ctx context.Context, t *auditoria.Tx, in verificacionHecha) (bool, error) {
			empresa, err := soloAsistente(ctx)
			if err != nil {
				return false, err
			}
			q := mensajeriabd.New(t)
			c, err := convDelAsistente(ctx, q, alcanceConv{asistente: true, empresa: empresa}, in.ConversacionID, true)
			if err != nil {
				return false, err
			}
			t.FijarEmpresa(empresa)
			t.FijarEntidad("conversacion", c.ID)
			// El dato no se guarda: los mensajes del cliente quedan con *** (contrato con Argos §7.5),
			// también si la conversación cambió de cliente mientras Argos respondía.
			if err := m.taparDocumentos(ctx, q, c, in.desde); err != nil {
				return false, err
			}
			// El intento reservado queda con su resultado, para el cliente con el que se hizo (cuenta
			// para su tope aunque la conversación haya cambiado de cliente).
			if _, err := q.CerrarVerificacion(ctx, mensajeriabd.CerrarVerificacionParams{ID: in.reserva, ClienteID: in.ClienteID, Resultado: in.Resultado}); err != nil {
				return false, err
			}
			// El desafío vale para el cliente con el que se hizo: si la conversación ya es de otro
			// (la reasociaron mientras Argos respondía), no cuenta.
			if c.ClienteID == nil || *c.ClienteID != in.ClienteID {
				return false, nil
			}
			if in.Resultado == "coincide" {
				if err := m.fijarVerificacionFuerte(ctx, q, &c); err != nil {
					return false, err
				}
			}
			// En el hilo: el resultado del intento, de una lista cerrada (nunca el documento ni texto libre).
			detalle := in.Detalle
			if !slices.Contains([]string{IdentidadCoincide, IdentidadNoCoincide, IdentidadNoDisponible, IdentidadDemasiadosIntentos}, detalle) {
				detalle = IdentidadNoDisponible
			}
			if err := m.sistema(ctx, t, &c, EventoSistema{Tipo: "identidad_verificada", ActorNombre: nombrePtr(NombreAsistenteIA), Detalle: &detalle}); err != nil {
				return false, err
			}
			if err := m.emitirEventoConv(ctx, t, c, EventoConvActualizada, nil, nil); err != nil {
				return false, err
			}
			return in.Resultado == "coincide", t.Emitir("conversacion.identidad_verificada", map[string]any{"conversacion_id": c.ID, "empresa_id": c.EmpresaID,
				"cliente_id": c.ClienteID, "resultado": in.Resultado, "origen": "asistente"})
		},
	})
}
