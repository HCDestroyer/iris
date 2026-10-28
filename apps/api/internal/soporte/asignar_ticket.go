package soporte

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	soportebd "github.com/HCDestroyer/iris/apps/api/internal/soporte/bd"
)

// EntradaAsignarTicket es la entrada de tickets.asignar. Sin usuario ni cuadrilla, el ticket
// queda sin asignar.
type EntradaAsignarTicket struct {
	ID          int64  `json:"-"`
	Version     int32  `json:"version" minimum:"1" doc:"Versión leída; si cambió, 409"`
	UsuarioID   *int64 `json:"usuarioId,omitempty" minimum:"1" doc:"Usuario responsable; ausente = ninguno"`
	CuadrillaID *int64 `json:"cuadrillaId,omitempty" minimum:"1" doc:"Cuadrilla; ausente = ninguna"`
}

func mismoID(a, b *int64) bool { return (a == nil && b == nil) || (a != nil && b != nil && *a == *b) }

func (m *Modulo) nuevoAsignarTicket() *auditoria.Caso[EntradaAsignarTicket, Ticket] {
	return auditoria.Nuevo(auditoria.Caso[EntradaAsignarTicket, Ticket]{
		Accion:  "tickets.asignar",
		Permiso: nucleo.PermTicketsAsignar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaAsignarTicket) (Ticket, error) {
			q := soportebd.New(t)
			ahora := m.ahora()
			antes, err := tomar(ctx, t, q, in.ID, nucleo.PermTicketsAsignar, &in.Version)
			if err != nil {
				return Ticket{}, err
			}
			if err := exigirEstado(antes, "asignar", EstadoNuevo, EstadoAbierto, EstadoEnEspera); err != nil {
				return Ticket{}, err
			}
			if mismoID(antes.AsignadoA, in.UsuarioID) && mismoID(antes.CuadrillaID, in.CuadrillaID) {
				return Ticket{}, nucleo.ReglaNegocio("sin_cambios", "El ticket ya tiene esa asignación")
			}
			if in.UsuarioID != nil {
				u, err := q.UsuarioAsignable(ctx, *in.UsuarioID)
				if errors.Is(err, pgx.ErrNoRows) || (err == nil && !u.Activo) {
					return Ticket{}, nucleo.Validacion(nucleo.ErrorCampo{Field: "usuarioId", Message: "El usuario no existe o está inactivo", Rule: "usuario"})
				}
				if err != nil {
					return Ticket{}, err
				}
			}
			cal, err := cargarCalendario(ctx, q, antes.CalendarioID)
			if err != nil {
				return Ticket{}, err
			}
			despues := antes
			despues.AsignadoA, despues.CuadrillaID = in.UsuarioID, in.CuadrillaID
			if in.UsuarioID != nil || in.CuadrillaID != nil {
				abrirSiNuevo(&despues)
			}
			f, err := guardar(ctx, t, q, antes, despues, cal)
			if err != nil {
				return Ticket{}, err
			}
			if err := historial(ctx, t, q, f, "asignado", nil, nil, nil, map[string]any{
				"usuario_id": f.AsignadoA, "cuadrilla_id": f.CuadrillaID,
				"usuario_anterior": antes.AsignadoA, "cuadrilla_anterior": antes.CuadrillaID,
			}); err != nil {
				return Ticket{}, err
			}
			if err := t.Emitir("ticket.asignado", map[string]any{
				"ticket_id": f.ID, "usuario_id": f.AsignadoA, "cuadrilla_id": f.CuadrillaID,
			}); err != nil {
				return Ticket{}, err
			}
			if err := cambioDeEstado(ctx, t, q, antes, f, ptr("asignado")); err != nil {
				return Ticket{}, err
			}
			return aTicket(f, ahora), nil
		},
	})
}
