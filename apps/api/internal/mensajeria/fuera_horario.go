package mensajeria

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
)

// Fuera de horario (contratos/conversaciones.md §10.2): al llegar un mensaje del cliente con el
// horario cerrado, la respuesta automática del número sale una sola vez por ventana cerrada y, si
// aplica la regla, se avisa a la guardia (una vez por conversación y ventana). «Que atienda un
// asistente» queda reservado para la fase 2.

// GuardiaFueraDeHorario es la regla de escalamiento a guardia.
type GuardiaFueraDeHorario struct {
	Activa      bool    `json:"activa"`
	UsuarioID   *int64  `json:"usuarioId,omitempty" minimum:"1" nullable:"true" doc:"Usuario de guardia (con conversaciones.ver)"`
	EtiquetaIDs []int64 `json:"etiquetaIds,omitempty" maxItems:"10" uniqueItems:"true" doc:"Conversaciones con alguna de estas etiquetas (p. ej. Empresarial)"`
	FallaMasiva bool    `json:"fallaMasiva,omitempty" doc:"Router del servicio del cliente caído según Horus"`
}

// AsistenteFueraDeHorario es el punto de extensión de la fase 2.
type AsistenteFueraDeHorario struct {
	Activo bool `json:"activo" doc:"Fase 2: en la fase 1 solo false (422 disponible_en_fase_2)"`
}

// FueraDeHorario es lo que hace un número con el horario cerrado.
type FueraDeHorario struct {
	Responder bool                    `json:"responder" doc:"Contestar el mensaje una vez por ventana cerrada"`
	Mensaje   string                  `json:"mensaje,omitempty" maxLength:"1000" doc:"Texto plano de la respuesta automática"`
	Guardia   GuardiaFueraDeHorario   `json:"guardia,omitempty"`
	Asistente AsistenteFueraDeHorario `json:"asistente,omitempty"`
}

func fueraDeHorarioDe(crudo []byte) FueraDeHorario {
	var f FueraDeHorario
	_ = json.Unmarshal(crudo, &f)
	return f
}

// validarFueraDeHorario aplica las reglas de la configuración.
func validarFueraDeHorario(ctx context.Context, t *auditoria.Tx, empresa int64, f *FueraDeHorario) error {
	if f.Asistente.Activo {
		return reglaConv("disponible_en_fase_2", "Que atienda un asistente estará disponible en la fase 2")
	}
	f.Mensaje = strings.TrimSpace(f.Mensaje)
	if f.Responder && f.Mensaje == "" {
		return campoConv("fueraDeHorario.mensaje", "Escribe la respuesta automática", "required")
	}
	if f.Guardia.Activa {
		if f.Guardia.UsuarioID == nil {
			return campoConv("fueraDeHorario.guardia.usuarioId", "Indica el usuario de guardia", "required")
		}
		if _, _, err := usuarioAtiende(ctx, t, empresa, *f.Guardia.UsuarioID); err != nil {
			return err
		}
		if len(f.Guardia.EtiquetaIDs) == 0 && !f.Guardia.FallaMasiva {
			return campoConv("fueraDeHorario.guardia", "Indica cuándo escalar: etiquetas o falla masiva", "regla")
		}
		if len(f.Guardia.EtiquetaIDs) > 0 {
			es, err := mensajeriabd.New(t).EtiquetasActivasPorIDs(ctx, mensajeriabd.EtiquetasActivasPorIDsParams{Ids: f.Guardia.EtiquetaIDs, Empresa: empresa})
			if err != nil {
				return err
			}
			if len(es) != len(f.Guardia.EtiquetaIDs) {
				return campoConv("fueraDeHorario.guardia.etiquetaIds", "Alguna no es una etiqueta activa de la empresa", "etiqueta")
			}
		}
	}
	if f.Guardia.EtiquetaIDs == nil {
		f.Guardia.EtiquetaIDs = []int64{}
	}
	return nil
}

// alRecibirFueraDeHorario corre después de guardar un mensaje del cliente.
func (m *Modulo) alRecibirFueraDeHorario(ctx context.Context, t *auditoria.Tx, c *mensajeriabd.MensajeriaConversacion, canal mensajeriabd.MensajeriaCanal,
	_ mensajeriabd.MensajeriaConversacionMensaje) error {
	q := mensajeriabd.New(t)
	var colaHorario *int64
	if c.ColaID != nil {
		cola, err := q.ColaPorID(ctx, mensajeriabd.ColaPorIDParams{ID: *c.ColaID, Empresa: empresaDeConv(*c)})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		colaHorario = cola.HorarioID
	}
	cal, err := calendarioEfectivo(ctx, q, colaHorario, canal.ID)
	if err != nil || cal == nil {
		return err
	}
	ahora := m.ahora()
	abierto, _, apertura := cal.estado(ahora)
	if abierto {
		return nil
	}
	// Hasta cuándo vale «ya se hizo»: la próxima apertura (sin apertura conocida, 24 h).
	hasta := apertura
	if hasta.IsZero() {
		hasta = ahora.Add(24 * time.Hour)
	}
	f := fueraDeHorarioDe(canal.FueraHorario)
	if f.Responder && f.Mensaje != "" && (c.FueraHorarioHasta == nil || !ahora.Before(*c.FueraHorarioHasta)) {
		if err := m.responderFueraDeHorario(ctx, t, c, f, hasta); err != nil {
			return err
		}
	}
	if f.Guardia.Activa && f.Guardia.UsuarioID != nil && (c.GuardiaHasta == nil || !ahora.Before(*c.GuardiaHasta)) {
		motivo, err := m.motivoGuardia(ctx, q, c, f.Guardia)
		if err != nil || motivo == "" {
			return err
		}
		return m.avisarGuardia(ctx, t, c, *f.Guardia.UsuarioID, motivo, hasta)
	}
	return nil
}

func (m *Modulo) responderFueraDeHorario(ctx context.Context, t *auditoria.Tx, c *mensajeriabd.MensajeriaConversacion, f FueraDeHorario, hasta time.Time) error {
	q := mensajeriabd.New(t)
	// Nunca a quien pidió no recibir mensajes.
	if c.ClienteID != nil {
		v, err := q.ConsentimientoVigente(ctx, mensajeriabd.ConsentimientoVigenteParams{EmpresaID: empresaDeConv(*c), ClienteID: *c.ClienteID, Canal: CanalWhatsApp})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if err == nil && v.Estado == "baja" {
			return nil
		}
	}
	texto := recortar(f.Mensaje, MaxTextoWhatsApp)
	msg, err := m.agregarMensaje(ctx, t, c, nuevoMensaje{direccion: DirSaliente, tipo: "texto", texto: &texto, estadoEntrega: nombrePtr(EntregaEnCola)})
	if err != nil {
		return err
	}
	if err := m.encolar(ctx, t.Tx, ArgsEnviarMensaje{MensajeID: msg.ID}); err != nil {
		return err
	}
	if err := m.sistema(ctx, t, c, EventoSistema{Tipo: "fuera_de_horario"}); err != nil {
		return err
	}
	if err := q.FijarFueraHorario(ctx, mensajeriabd.FijarFueraHorarioParams{ID: c.ID, Hasta: &hasta}); err != nil {
		return err
	}
	c.FueraHorarioHasta = &hasta
	return t.Emitir("conversacion.fuera_de_horario", map[string]any{"conversacion_id": c.ID, "empresa_id": c.EmpresaID, "mensaje_id": msg.ID,
		"hasta": hasta})
}

// motivoGuardia: «etiqueta» o «falla_masiva» si la regla aplica ("" si no).
func (m *Modulo) motivoGuardia(ctx context.Context, q *mensajeriabd.Queries, c *mensajeriabd.MensajeriaConversacion, g GuardiaFueraDeHorario) (string, error) {
	if len(g.EtiquetaIDs) > 0 {
		es, err := q.EtiquetasDeConversacion(ctx, c.ID)
		if err != nil {
			return "", err
		}
		for _, e := range es {
			if slices.Contains(g.EtiquetaIDs, e) {
				return "etiqueta", nil
			}
		}
	}
	if g.FallaMasiva && c.ClienteID != nil {
		caido, err := q.RouterCaidoDelCliente(ctx, mensajeriabd.RouterCaidoDelClienteParams{ClienteID: *c.ClienteID, ServicioID: c.ServicioID})
		if err != nil {
			return "", err
		}
		if caido {
			return "falla_masiva", nil
		}
	}
	return "", nil
}

func (m *Modulo) avisarGuardia(ctx context.Context, t *auditoria.Tx, c *mensajeriabd.MensajeriaConversacion, usuario int64, motivo string, hasta time.Time) error {
	q := mensajeriabd.New(t)
	u, _, err := usuarioAtiende(ctx, t, empresaDeConv(*c), usuario)
	if err != nil {
		// La guardia ya no atiende (desactivada, sin permiso): no se escala; queda en el log de auditoría.
		t.Anotar(map[string]any{"guardia_no_disponible": usuario})
		return nil
	}
	detalle := motivo
	if err := m.agregarSistemaYMencion(ctx, t, c, u, detalle); err != nil {
		return err
	}
	if err := q.FijarGuardia(ctx, mensajeriabd.FijarGuardiaParams{ID: c.ID, Hasta: &hasta}); err != nil {
		return err
	}
	c.GuardiaHasta = &hasta
	return t.Emitir("conversacion.guardia_avisada", map[string]any{"conversacion_id": c.ID, "empresa_id": c.EmpresaID, "usuario_id": u.ID,
		"motivo": motivo})
}

func (m *Modulo) agregarSistemaYMencion(ctx context.Context, t *auditoria.Tx, c *mensajeriabd.MensajeriaConversacion, u UsuarioRef, motivo string) error {
	msg, err := m.agregarMensaje(ctx, t, c, nuevoMensaje{direccion: DirSistema, tipo: "sistema",
		evento: &EventoSistema{Tipo: "guardia_avisada", AsignadoNombre: &u.Nombre, Detalle: &motivo}})
	if err != nil {
		return err
	}
	return m.mencionar(ctx, t, *c, msg.ID, 0, []int64{u.ID}, "guardia")
}

// slaConHorario: el SLA empieza a contar al abrir el horario si el mensaje llegó cerrado.
func slaConHorario(cal *calendario, desde time.Time) (time.Time, bool) {
	if cal == nil {
		return desde, true
	}
	abierto, _, apertura := cal.estado(desde)
	if abierto {
		return desde, true
	}
	if apertura.IsZero() {
		return time.Time{}, false
	}
	return apertura, true
}
