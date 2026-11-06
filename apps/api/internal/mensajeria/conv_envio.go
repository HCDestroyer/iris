package mensajeria

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/adjuntos"
)

// Responder y anotar (contratos/conversaciones.md §8): el mensaje queda en_cola y sale por el job
// mensajeria.enviar_mensaje. Ventana de 24 h, plantillas aprobadas en el número y permiso de
// contacto como en los avisos. El contenido nunca va a la auditoría (solo id, tipo y hash).

// MotivoFueraDeVentana: Meta rechazó el texto porque pasaron 24 h (o el cliente nunca escribió).
const MotivoFueraDeVentana = "fuera_de_ventana"

// MaxAdjuntoSaliente: bytes de un adjunto que se manda por WhatsApp (16 MB de Meta para medios).
const MaxAdjuntoSaliente = 16 << 20

// EntradaResponder es POST /conversaciones/{id}/mensajes.
type EntradaResponder struct {
	ConversacionID    int64             `json:"-"`
	ID                uuid.UUID         `json:"id" doc:"uuid v7 generado en el cliente: el servidor es idempotente por él"`
	Texto             *string           `json:"texto,omitempty" maxLength:"4096" doc:"Texto plano (o pie del adjunto)"`
	Plantilla         *PlantillaMensaje `json:"plantilla,omitempty" doc:"Plantilla aprobada en el número (la única opción fuera de la ventana de 24 h)"`
	AdjuntoID         *uuid.UUID        `json:"adjuntoId,omitempty" doc:"Adjunto completo de esta conversación (POST /api/v1/adjuntos, entidad conversacion)"`
	RespuestaRapidaID *int64            `json:"respuestaRapidaId,omitempty" minimum:"1" doc:"Si el texto salió de una respuesta rápida (métricas)"`
	IdempotencyKey    string            `json:"-"`
}

// MarshalJSON: a la auditoría van el id, el tipo y el hash; nunca el texto ni las variables.
func (e EntradaResponder) MarshalJSON() ([]byte, error) {
	var plantilla *string
	if e.Plantilla != nil {
		plantilla = &e.Plantilla.Codigo
	}
	return json.Marshal(map[string]any{"conversacion_id": e.ConversacionID, "id": e.ID, "con_texto": e.Texto != nil, "plantilla": plantilla,
		"adjunto_id": e.AdjuntoID, "respuesta_rapida_id": e.RespuestaRapidaID, "hash": hashContenidoConv(e.ConversacionID, e.Texto, e.Plantilla, e.AdjuntoID)})
}

// EntradaNota es POST /conversaciones/{id}/notas.
type EntradaNota struct {
	ConversacionID int64     `json:"-"`
	ID             uuid.UUID `json:"id" doc:"uuid v7 generado en el cliente"`
	Texto          string    `json:"texto" minLength:"1" maxLength:"4000" doc:"Nota interna en texto plano (nunca sale a WhatsApp)"`
	Menciones      []int64   `json:"menciones,omitempty" maxItems:"10" uniqueItems:"true" doc:"Usuarios mencionados: pasan a ver la conversación y reciben aviso"`
	IdempotencyKey string    `json:"-"`
}

// MarshalJSON: sin el texto.
func (e EntradaNota) MarshalJSON() ([]byte, error) {
	t := e.Texto
	return json.Marshal(map[string]any{"conversacion_id": e.ConversacionID, "id": e.ID, "menciones": e.Menciones,
		"hash": hashContenidoConv(e.ConversacionID, &t, nil, nil)})
}

// MensajeConvEnviado es la respuesta: Repetido = el id ya se había recibido (200).
type MensajeConvEnviado struct {
	MensajeConversacion
	Repetido bool `json:"-"`
}

func hashContenidoConv(conv int64, texto *string, p *PlantillaMensaje, adjunto *uuid.UUID) []byte {
	b, _ := json.Marshal(struct {
		C int64             `json:"c"`
		T *string           `json:"t"`
		P *PlantillaMensaje `json:"p"`
		A *uuid.UUID        `json:"a"`
	}{conv, texto, p, adjunto})
	h := hashDe(json.RawMessage(b))
	return h
}

// repetidoConv: el mismo id ya está (misma conversación y contenido → el mensaje; si no, 409); la
// misma Idempotency-Key con otro id → 422.
func repetidoConv(ctx context.Context, q *mensajeriabd.Queries, conv int64, id uuid.UUID, autor int64, clave string, hash []byte) (*mensajeriabd.MensajeriaConversacionMensaje, error) {
	f, err := q.MensajeConversacionPorID(ctx, id)
	switch {
	case err == nil:
		if f.ConversacionID != conv || f.AutorID == nil || *f.AutorID != autor || !bytes.Equal(f.HashContenido, hash) {
			return nil, nucleo.Conflicto("mensaje_id_reutilizado", "Ese id de mensaje ya se usó con otro contenido")
		}
		return &f, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return nil, err
	}
	if clave != "" {
		previo, err := q.MensajeConversacionPorClave(ctx, mensajeriabd.MensajeConversacionPorClaveParams{AutorID: &autor, IdempotencyKey: &clave})
		if err == nil && previo.ID != id {
			return nil, idempotenciaReutilizada()
		}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
	}
	return nil, nil
}

func plantillasDeCanal(c mensajeriabd.MensajeriaCanal) map[string]PlantillaMeta {
	p := map[string]PlantillaMeta{}
	_ = json.Unmarshal(c.Plantillas, &p)
	return p
}

// permisoPlantilla: una plantilla exige cliente de Argos sin baja; fuera de la ventana, además un
// permiso vigente acepta (§6.4 del contrato con Argos).
func permisoPlantilla(ctx context.Context, q *mensajeriabd.Queries, c mensajeriabd.MensajeriaConversacion, fueraDeVentana bool) error {
	if c.ClienteID == nil {
		if fueraDeVentana {
			return reglaConv(MotivoSinPermiso, "Fuera de la ventana de 24 h solo se escribe a un cliente de Argos con permiso de contacto")
		}
		return nil
	}
	v, err := q.ConsentimientoVigente(ctx, mensajeriabd.ConsentimientoVigenteParams{EmpresaID: empresaDeConv(c), ClienteID: *c.ClienteID, Canal: CanalWhatsApp})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		if fueraDeVentana {
			return reglaConv(MotivoSinPermiso, "El cliente no tiene permiso de contacto registrado por WhatsApp")
		}
		return nil
	case err != nil:
		return err
	case v.Estado == "baja":
		return reglaConv(MotivoBaja, "El cliente pidió no recibir mensajes por WhatsApp")
	}
	return nil
}

func tipoDeMime(mime string) string {
	switch {
	case strings.HasPrefix(mime, "image/"):
		return "imagen"
	case strings.HasPrefix(mime, "audio/"):
		return "audio"
	case strings.HasPrefix(mime, "video/"):
		return "video"
	}
	return "documento"
}

func nuevoResponder(m *Modulo) *auditoria.Caso[EntradaResponder, MensajeConvEnviado] {
	return auditoria.Nuevo(auditoria.Caso[EntradaResponder, MensajeConvEnviado]{
		Accion: "conversaciones.responder", Permiso: nucleo.PermConversacionesResponder,
		AuditarExito: func(r MensajeConvEnviado) bool { return !r.Repetido },
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaResponder) (MensajeConvEnviado, error) {
			a, err := alcanceConvDe(ctx)
			if err != nil {
				return MensajeConvEnviado{}, err
			}
			if in.ID == uuid.Nil {
				return MensajeConvEnviado{}, campoConv("id", "Falta el id del mensaje", "required")
			}
			if in.Texto != nil {
				tx := strings.TrimSpace(*in.Texto)
				if tx == "" {
					in.Texto = nil
				} else {
					in.Texto = &tx
				}
			}
			switch {
			case in.Plantilla != nil && (in.Texto != nil || in.AdjuntoID != nil):
				return MensajeConvEnviado{}, campoConv("plantilla", "Una plantilla va sola (sin texto ni adjunto)", "exclusivo")
			case in.Plantilla == nil && in.Texto == nil && in.AdjuntoID == nil:
				return MensajeConvEnviado{}, reglaConv("mensaje_vacio", "Escribe un texto, adjunta un archivo o elige una plantilla")
			}
			q := mensajeriabd.New(t)
			c, err := convVisible(ctx, q, a, in.ConversacionID, true)
			if err != nil {
				return MensajeConvEnviado{}, err
			}
			t.FijarEmpresa(empresaDeConv(c))
			t.FijarEntidad("conversacion", c.ID)
			hash := hashContenidoConv(c.ID, in.Texto, in.Plantilla, in.AdjuntoID)
			if previo, err := repetidoConv(ctx, q, c.ID, in.ID, a.usuario, in.IdempotencyKey, hash); err != nil || previo != nil {
				if err != nil {
					return MensajeConvEnviado{}, err
				}
				ms, err := mensajesConv(ctx, q, []mensajeriabd.MensajeriaConversacionMensaje{*previo})
				if err != nil {
					return MensajeConvEnviado{}, err
				}
				return MensajeConvEnviado{MensajeConversacion: ms[0], Repetido: true}, nil
			}
			canal, err := q.CanalPorID(ctx, c.CanalID)
			if err != nil {
				return MensajeConvEnviado{}, err
			}
			if !canal.Activo {
				return MensajeConvEnviado{}, reglaConv("canal_inactivo", "El número de esta conversación está desactivado")
			}
			ahora := m.ahora()
			ventana := ventanaDe(c.UltimoEntranteEn, ahora)
			n := nuevoMensaje{id: in.ID, direccion: DirSaliente, tipo: "texto", texto: in.Texto, autor: &a.usuario, hash: hash,
				estadoEntrega: nombrePtr(EntregaEnCola), respuesta: in.RespuestaRapidaID}
			if in.IdempotencyKey != "" {
				n.clave = &in.IdempotencyKey
			}
			if in.Plantilla != nil {
				p, ok := PlantillaPorCodigo(in.Plantilla.Codigo)
				if !ok {
					return MensajeConvEnviado{}, campoConv("plantilla.codigo", "No es una plantilla de Iris", "plantilla")
				}
				if _, ok := plantillasDeCanal(canal)[p.Codigo]; !ok {
					return MensajeConvEnviado{}, reglaConv("plantilla_no_aprobada", "La plantilla no está aprobada en el número de esta conversación")
				}
				if in.Plantilla.Variables == nil {
					in.Plantilla.Variables = map[string]string{}
				}
				if err := p.validarVariables(in.Plantilla.Variables); err != nil {
					return MensajeConvEnviado{}, err
				}
				if err := permisoPlantilla(ctx, q, c, !ventana.Abierta); err != nil {
					return MensajeConvEnviado{}, err
				}
				n.tipo, n.plantilla = "plantilla", &p.Codigo
				if n.variables, err = json.Marshal(in.Plantilla.Variables); err != nil {
					return MensajeConvEnviado{}, err
				}
				texto := p.Render(in.Plantilla.Variables)
				n.texto = &texto
			} else if !ventana.Abierta {
				return MensajeConvEnviado{}, reglaConv(MotivoFueraDeVentana,
					"Pasaron más de 24 h desde el último mensaje del cliente: solo se puede mandar una plantilla aprobada")
			}
			if in.AdjuntoID != nil {
				adj, err := adjuntos.DeEntidad(ctx, t, *in.AdjuntoID, EntidadAdjuntosConversacion, strconv.FormatInt(c.ID, 10))
				if err != nil || adj.Estado != adjuntos.EstadoCompleto {
					return MensajeConvEnviado{}, reglaConv("adjunto_no_listo", "El adjunto no es de esta conversación o aún no termina de subir")
				}
				if adj.Tamano > MaxAdjuntoSaliente {
					return MensajeConvEnviado{}, reglaConv("adjunto_no_listo", "El adjunto pesa más de 16 MB")
				}
				n.adjunto, n.tipo = in.AdjuntoID, tipoDeMime(adj.Mime)
			}
			if in.RespuestaRapidaID != nil {
				if _, err := q.RespuestaRapidaPorID(ctx, mensajeriabd.RespuestaRapidaPorIDParams{ID: *in.RespuestaRapidaID, Empresa: empresaDeConv(c)}); err != nil {
					return MensajeConvEnviado{}, campoConv("respuestaRapidaId", "No es una respuesta rápida de la empresa", "respuesta_rapida")
				}
			}
			f, err := m.agregarMensaje(ctx, t, &c, n)
			if err != nil {
				return MensajeConvEnviado{}, err
			}
			antes := c.Estado
			if c.Estado == EstadoConvAbierta {
				c.Estado = EstadoConvEsperando
			}
			c.EsperaDesde, c.SlaVenceEn, c.SlaAvisado = nil, nil, false
			if c, err = guardarConv(ctx, q, c, antes != c.Estado); err != nil {
				return MensajeConvEnviado{}, err
			}
			if err := m.emitirEventoConv(ctx, t, c, EventoConvActualizada, nil, nil); err != nil {
				return MensajeConvEnviado{}, err
			}
			if err := m.encolar(ctx, t.Tx, ArgsEnviarMensaje{MensajeID: f.ID}); err != nil {
				return MensajeConvEnviado{}, err
			}
			t.Anotar(map[string]any{"mensaje_id": f.ID, "tipo": f.Tipo})
			if err := t.Emitir("conversacion.mensaje_enviado", map[string]any{"conversacion_id": c.ID, "mensaje_id": f.ID, "empresa_id": c.EmpresaID,
				"autor_id": a.usuario, "tipo": f.Tipo, "plantilla": f.Plantilla, "respuesta_rapida_id": f.RespuestaRapidaID}); err != nil {
				return MensajeConvEnviado{}, err
			}
			ms, err := mensajesConv(ctx, q, []mensajeriabd.MensajeriaConversacionMensaje{f})
			if err != nil {
				return MensajeConvEnviado{}, err
			}
			return MensajeConvEnviado{MensajeConversacion: ms[0]}, nil
		},
	})
}

func nuevoAnotar(m *Modulo) *auditoria.Caso[EntradaNota, MensajeConvEnviado] {
	return auditoria.Nuevo(auditoria.Caso[EntradaNota, MensajeConvEnviado]{
		Accion: "conversaciones.nota", Permiso: nucleo.PermConversacionesResponder,
		AuditarExito: func(r MensajeConvEnviado) bool { return !r.Repetido },
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaNota) (MensajeConvEnviado, error) {
			a, err := alcanceConvDe(ctx)
			if err != nil {
				return MensajeConvEnviado{}, err
			}
			texto := strings.TrimSpace(in.Texto)
			if texto == "" || in.ID == uuid.Nil {
				return MensajeConvEnviado{}, campoConv("texto", "La nota está vacía", "required")
			}
			q := mensajeriabd.New(t)
			c, err := convVisible(ctx, q, a, in.ConversacionID, true)
			if err != nil {
				return MensajeConvEnviado{}, err
			}
			t.FijarEmpresa(empresaDeConv(c))
			t.FijarEntidad("conversacion", c.ID)
			hash := hashContenidoConv(c.ID, &texto, nil, nil)
			if previo, err := repetidoConv(ctx, q, c.ID, in.ID, a.usuario, in.IdempotencyKey, hash); err != nil || previo != nil {
				if err != nil {
					return MensajeConvEnviado{}, err
				}
				ms, err := mensajesConv(ctx, q, []mensajeriabd.MensajeriaConversacionMensaje{*previo})
				if err != nil {
					return MensajeConvEnviado{}, err
				}
				return MensajeConvEnviado{MensajeConversacion: ms[0], Repetido: true}, nil
			}
			menciones := []int64{}
			for _, u := range in.Menciones {
				if u == a.usuario || slices.Contains(menciones, u) {
					continue
				}
				if _, _, err := usuarioAtiende(ctx, t, empresaDeConv(c), u); err != nil {
					return MensajeConvEnviado{}, err
				}
				menciones = append(menciones, u)
			}
			n := nuevoMensaje{id: in.ID, direccion: DirNota, tipo: "texto", texto: &texto, autor: &a.usuario, menciones: menciones, hash: hash}
			if in.IdempotencyKey != "" {
				n.clave = &in.IdempotencyKey
			}
			f, err := m.agregarMensaje(ctx, t, &c, n)
			if err != nil {
				return MensajeConvEnviado{}, err
			}
			if err := m.mencionar(ctx, t, c, f.ID, a.usuario, menciones, "mencion"); err != nil {
				return MensajeConvEnviado{}, err
			}
			if _, err := guardarConv(ctx, q, c, false); err != nil {
				return MensajeConvEnviado{}, err
			}
			t.Anotar(map[string]any{"mensaje_id": f.ID})
			if err := t.Emitir("conversacion.nota_agregada", map[string]any{"conversacion_id": c.ID, "mensaje_id": f.ID, "empresa_id": c.EmpresaID,
				"autor_id": a.usuario, "menciones": menciones}); err != nil {
				return MensajeConvEnviado{}, err
			}
			ms, err := mensajesConv(ctx, q, []mensajeriabd.MensajeriaConversacionMensaje{f})
			if err != nil {
				return MensajeConvEnviado{}, err
			}
			return MensajeConvEnviado{MensajeConversacion: ms[0]}, nil
		},
	})
}

// mencionar agrega a los usuarios como participantes, les manda el evento del flujo y emite
// conversacion.mencion (para un futuro push).
func (m *Modulo) mencionar(ctx context.Context, t *auditoria.Tx, c mensajeriabd.MensajeriaConversacion, mensaje uuid.UUID, autor int64, usuarios []int64, origen string) error {
	q := mensajeriabd.New(t)
	tipo := EventoConvMencion
	if origen == "guardia" {
		tipo = EventoConvGuardia
	}
	for _, u := range usuarios {
		if _, err := q.InsertarParticipante(ctx, mensajeriabd.InsertarParticipanteParams{ConversacionID: c.ID, UsuarioID: u, Origen: origen}); err != nil {
			return err
		}
		uu := u
		if err := m.emitirEventoConv(ctx, t, c, tipo, &mensaje, &uu); err != nil {
			return err
		}
		if origen == "mencion" {
			if err := t.Emitir("conversacion.mencion", map[string]any{"conversacion_id": c.ID, "mensaje_id": mensaje, "empresa_id": c.EmpresaID,
				"usuario_id": u, "autor_id": autor}); err != nil {
				return err
			}
		}
	}
	return nil
}

// Envío (job mensajeria.enviar_mensaje) --------------------------------------------------------------

// ArgsEnviarMensaje manda un mensaje saliente de una conversación.
type ArgsEnviarMensaje struct {
	MensajeID uuid.UUID `json:"mensaje_id"`
}

// Kind implementa river.JobArgs.
func (ArgsEnviarMensaje) Kind() string { return "mensajeria.enviar_mensaje" }

// InsertOpts: cola de mensajería, 8 intentos, uno por mensaje.
func (ArgsEnviarMensaje) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: Cola, MaxAttempts: 8, UniqueOpts: river.UniqueOpts{ByArgs: true}}
}

type trabajadorMensaje struct {
	river.WorkerDefaults[ArgsEnviarMensaje]
	m  *Modulo
	bd nucleo.BD
}

func (w *trabajadorMensaje) Work(ctx context.Context, job *river.Job[ArgsEnviarMensaje]) error {
	espera, err := w.m.procesarMensaje(ctxSistema(ctx, w.bd), job.Args.MensajeID, job.Attempt >= job.MaxAttempts)
	if err != nil {
		return err
	}
	if espera > 0 {
		return river.JobSnooze(espera)
	}
	return nil
}

type mensajePreparado struct {
	Accion  string        `json:"accion"`
	Espera  time.Duration `json:"espera,omitempty"`
	salida  MensajeSaliente
	adaptad Adaptador
}

type entradaRegistrarMensaje struct {
	MensajeID uuid.UUID   `json:"mensaje_id"`
	Resultado *Resultado  `json:"resultado,omitempty"`
	Error     *ErrorCanal `json:"error,omitempty"`
	Ultimo    bool        `json:"ultimo"`
}

func nuevoPrepararMensaje(m *Modulo) *auditoria.Caso[uuid.UUID, mensajePreparado] {
	return auditoria.Nuevo(auditoria.Caso[uuid.UUID, mensajePreparado]{
		Accion: "mensajeria.preparar_mensaje", Acceso: auditoria.Autenticado, AuditarExito: func(mensajePreparado) bool { return false },
		Logica: func(ctx context.Context, t *auditoria.Tx, id uuid.UUID) (mensajePreparado, error) {
			q := mensajeriabd.New(t)
			f, err := q.MensajeConversacionParaActualizar(ctx, id)
			if errors.Is(err, pgx.ErrNoRows) {
				return mensajePreparado{Accion: accionNada}, nil
			}
			if err != nil {
				return mensajePreparado{}, err
			}
			if f.EstadoEntrega == nil || *f.EstadoEntrega != EntregaEnCola || f.ProveedorID != nil {
				return mensajePreparado{Accion: accionNada}, nil
			}
			c, err := q.ConversacionClientePorID(ctx, f.ConversacionID)
			if err != nil {
				return mensajePreparado{}, err
			}
			t.FijarEmpresa(empresaDeConv(c))
			canal, err := q.CanalPorID(ctx, c.CanalID)
			if err != nil {
				return mensajePreparado{}, err
			}
			ahora := m.ahora()
			if !canal.Activo {
				return mensajePreparado{Accion: accionNada}, m.fallarMensaje(ctx, t, f, c, MotivoRechazado, "el número se desactivó")
			}
			if canal.PausadoHasta != nil && canal.PausadoHasta.After(ahora) {
				return mensajePreparado{Accion: accionEsperar, Espera: canal.PausadoHasta.Sub(ahora)}, nil
			}
			r, err := m.resueltoDe(canal)
			if err != nil {
				return mensajePreparado{}, err
			}
			s := MensajeSaliente{ID: f.ID, Destino: c.Numero, Texto: f.Texto}
			if f.Plantilla != nil {
				p, ok := PlantillaPorCodigo(*f.Plantilla)
				if !ok {
					return mensajePreparado{Accion: accionNada}, m.fallarMensaje(ctx, t, f, c, MotivoRechazado, "plantilla desconocida")
				}
				vars := map[string]string{}
				_ = json.Unmarshal(f.Variables, &vars)
				s.Texto, s.Plantilla, s.Variables = nil, &p, vars
			}
			if id := uuidDe(f.AdjuntoID); id != nil {
				datos, adj, err := adjuntos.Contenido(ctx, m.cfg.Almacen, t, *id, EntidadAdjuntosConversacion, strconv.FormatInt(c.ID, 10), MaxAdjuntoSaliente)
				if err != nil {
					return mensajePreparado{Accion: accionNada}, m.fallarMensaje(ctx, t, f, c, MotivoRechazado, "adjunto no disponible")
				}
				s.Medio = &MedioSaliente{Tipo: f.Tipo, Mime: adj.Mime, Nombre: "archivo", Datos: datos}
			}
			return mensajePreparado{Accion: accionEnviar, salida: s, adaptad: r.a}, nil
		},
	})
}

// fallarMensaje deja el mensaje fallido (con su motivo) y emite conversacion.mensaje_fallido.
func (m *Modulo) fallarMensaje(ctx context.Context, t *auditoria.Tx, f mensajeriabd.MensajeriaConversacionMensaje, c mensajeriabd.MensajeriaConversacion, motivo, detalle string) error {
	d := recortar(detalle, 300)
	g, err := mensajeriabd.New(t).MarcarEnvioMensaje(ctx, mensajeriabd.MarcarEnvioMensajeParams{ID: f.ID, EstadoEntrega: EntregaFallido,
		MotivoFalla: &motivo, DetalleFalla: &d, Intento: 1})
	if err != nil {
		return err
	}
	t.FijarEntidad("mensaje_conversacion", f.ID)
	t.RegistrarCambios("mensaje_conversacion", f.ID, map[string]any{"estado_entrega": f.EstadoEntrega}, map[string]any{"estado_entrega": g.EstadoEntrega, "motivo": motivo})
	if err := m.emitirEventoConv(ctx, t, c, EventoConvMensajeActualizad, &f.ID, nil); err != nil {
		return err
	}
	return t.Emitir("conversacion.mensaje_fallido", map[string]any{"conversacion_id": c.ID, "mensaje_id": f.ID, "empresa_id": c.EmpresaID,
		"motivo": motivo, "autor_id": f.AutorID})
}

func nuevoRegistrarMensaje(m *Modulo) *auditoria.Caso[entradaRegistrarMensaje, registrado] {
	return auditoria.Nuevo(auditoria.Caso[entradaRegistrarMensaje, registrado]{
		Accion: "mensajeria.registrar_mensaje", Acceso: auditoria.Autenticado, AuditarExito: func(registrado) bool { return false },
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaRegistrarMensaje) (registrado, error) {
			q := mensajeriabd.New(t)
			f, err := q.MensajeConversacionParaActualizar(ctx, in.MensajeID)
			if err != nil {
				return registrado{}, err
			}
			if f.EstadoEntrega == nil || *f.EstadoEntrega != EntregaEnCola {
				return registrado{}, nil
			}
			c, err := q.ConversacionClientePorID(ctx, f.ConversacionID)
			if err != nil {
				return registrado{}, err
			}
			t.FijarEmpresa(empresaDeConv(c))
			ahora := m.ahora()
			e := in.Error
			switch {
			case in.Resultado != nil:
				p := mensajeriabd.MarcarEnvioMensajeParams{ID: f.ID, EstadoEntrega: EntregaEnviado, ProveedorID: &in.Resultado.ProveedorID, EnviadoEn: &ahora, Intento: 1}
				if in.Resultado.Entregado {
					p.EstadoEntrega, p.EntregadoEn = EntregaEntregado, &ahora
				}
				if _, err := q.MarcarEnvioMensaje(ctx, p); err != nil {
					return registrado{}, err
				}
				return registrado{Cambio: true}, m.emitirEventoConv(ctx, t, c, EventoConvMensajeActualizad, &f.ID, nil)
			case e != nil && e.Pausar:
				hasta := ahora.Add(time.Hour)
				if err := q.PausarCanal(ctx, mensajeriabd.PausarCanalParams{ID: c.CanalID, PausadoHasta: &hasta}); err != nil {
					return registrado{}, err
				}
				fallthrough
			case e != nil && e.Temporal:
				if in.Ultimo {
					return registrado{Cambio: true}, m.fallarMensaje(ctx, t, f, c, MotivoAgotado, e.Detalle)
				}
				return registrado{Reintentar: true}, nil
			case e != nil:
				return registrado{Cambio: true}, m.fallarMensaje(ctx, t, f, c, e.Motivo, e.Detalle)
			}
			return registrado{}, nil
		},
	})
}

// procesarMensaje corre un intento de envío de un mensaje de conversación.
func (m *Modulo) procesarMensaje(ctx context.Context, id uuid.UUID, ultimo bool) (time.Duration, error) {
	p, err := m.prepararMensaje.Ejecutar(ctx, id)
	if err != nil {
		return 0, err
	}
	switch p.Accion {
	case accionNada:
		return 0, nil
	case accionEsperar:
		return p.Espera, nil
	}
	res, errEnvio := p.adaptad.EnviarConversacion(ctx, p.salida)
	in := entradaRegistrarMensaje{MensajeID: id, Ultimo: ultimo}
	var ec *ErrorCanal
	switch {
	case errEnvio == nil:
		in.Resultado = &res
	case errors.As(errEnvio, &ec):
		in.Error = ec
	default:
		in.Error = &ErrorCanal{Motivo: MotivoRechazado, Detalle: "error del canal", Temporal: true}
	}
	r, err := m.registrarMensaje.Ejecutar(context.WithoutCancel(ctx), in)
	if err != nil {
		return 0, err
	}
	if r.Reintentar {
		return 0, errReintentar
	}
	return 0, nil
}
