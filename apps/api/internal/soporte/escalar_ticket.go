package soporte

import (
	"context"
	"errors"
	"slices"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	soportebd "github.com/HCDestroyer/iris/apps/api/internal/soporte/bd"
)

// EntradaEscalarTicket es la entrada de tickets.escalar: sube un nivel y, si viene, la
// prioridad (que debe ser mayor). Con otra prioridad, los vencimientos del SLA se recalculan
// con sus objetivos desde el inicio del ticket (descontando las pausas).
type EntradaEscalarTicket struct {
	ID        int64   `json:"-"`
	Version   int32   `json:"version" minimum:"1" doc:"Versión leída; si cambió, 409"`
	Prioridad *string `json:"prioridad,omitempty" enum:"baja,media,alta,critica" doc:"Nueva prioridad, mayor que la actual"`
	Motivo    string  `json:"motivo" minLength:"3" maxLength:"500"`
}

func (m *Modulo) nuevoEscalarTicket() *auditoria.Caso[EntradaEscalarTicket, Ticket] {
	return auditoria.Nuevo(auditoria.Caso[EntradaEscalarTicket, Ticket]{
		Accion:  "tickets.escalar",
		Permiso: nucleo.PermTicketsEscalar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaEscalarTicket) (Ticket, error) {
			q := soportebd.New(t)
			ahora := m.ahora()
			motivo, err := texto("motivo", in.Motivo, 3, 500)
			if err != nil {
				return Ticket{}, err
			}
			if in.Prioridad != nil && !slices.Contains(Prioridades, *in.Prioridad) {
				return Ticket{}, nucleo.Validacion(nucleo.ErrorCampo{Field: "prioridad", Message: "Prioridad desconocida", Rule: "enum"})
			}
			antes, err := tomar(ctx, t, q, in.ID, nucleo.PermTicketsEscalar, &in.Version)
			if err != nil {
				return Ticket{}, err
			}
			if err := exigirEstado(antes, "escalar", EstadoNuevo, EstadoAbierto, EstadoEnEspera); err != nil {
				return Ticket{}, err
			}
			despues := antes
			despues.NivelEscalamiento++
			if in.Prioridad != nil {
				if rangoPrioridad(*in.Prioridad) <= rangoPrioridad(antes.Prioridad) {
					return Ticket{}, nucleo.ReglaNegocio("prioridad_no_mayor", "Escalar solo sube la prioridad (actual: "+antes.Prioridad+")")
				}
				despues.Prioridad = *in.Prioridad
				if antes.SlaID != nil {
					obj, err := q.ObjetivoSLA(ctx, soportebd.ObjetivoSLAParams{SlaID: *antes.SlaID, Prioridad: despues.Prioridad})
					switch {
					case errors.Is(err, pgx.ErrNoRows):
						despues.SlaRespuestaMin, despues.SlaResolucionMin = nil, nil
					case err != nil:
						return Ticket{}, err
					default:
						despues.SlaRespuestaMin, despues.SlaResolucionMin = &obj.RespuestaMin, &obj.ResolucionMin
					}
				}
			}
			cal, err := cargarCalendario(ctx, q, antes.CalendarioID)
			if err != nil {
				return Ticket{}, err
			}
			f, err := guardar(ctx, t, q, antes, despues, cal)
			if err != nil {
				return Ticket{}, err
			}
			if err := historial(ctx, t, q, f, "escalado", &antes.Prioridad, &f.Prioridad, &motivo,
				map[string]any{"nivel": f.NivelEscalamiento}); err != nil {
				return Ticket{}, err
			}
			if err := t.Emitir("ticket.escalado", map[string]any{
				"ticket_id": f.ID, "de": antes.Prioridad, "a": f.Prioridad, "nivel": f.NivelEscalamiento, "motivo": motivo,
			}); err != nil {
				return Ticket{}, err
			}
			return aTicket(f, ahora), nil
		},
	})
}
