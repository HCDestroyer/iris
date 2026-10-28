package soporte

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
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
			despues := antes
			despues.AsignadoA, despues.CuadrillaID = in.UsuarioID, in.CuadrillaID
			if in.UsuarioID != nil {
				if err := validarAsignable(ctx, q, *in.UsuarioID, despues); err != nil {
					return Ticket{}, err
				}
				// El responsable opera en la empresa del ticket (asignada o empresas.todas global;
				// sin conector, cualquiera mientras no haya empresas registradas).
				enLaEmpresa, err := identidad.UsuarioEnEmpresa(ctx, t, *in.UsuarioID, deref(antes.EmpresaID))
				if err != nil {
					return Ticket{}, err
				}
				if !enLaEmpresa {
					return Ticket{}, nucleo.ReglaNegocio("asignado_fuera_de_empresa",
						"El usuario no opera en la empresa del ticket")
				}
			}
			cal, err := cargarCalendario(ctx, q, antes.CalendarioID)
			if err != nil {
				return Ticket{}, err
			}
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

// validarAsignable: el responsable es una persona activa que puede ver el ticket con su
// alcance (global, la zona del ticket o la cuadrilla asignada). La cuadrilla no se valida
// todavía: agenda.cuadrillas llega en la Fase 3 (I2), y entonces tendrá FK.
func validarAsignable(ctx context.Context, q *soportebd.Queries, usuarioID int64, f soportebd.SoporteTicket) error {
	invalido := func(detalle string) error {
		return nucleo.Validacion(nucleo.ErrorCampo{Field: "usuarioId", Message: detalle, Rule: "usuario"})
	}
	u, err := q.UsuarioAsignable(ctx, usuarioID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !u.Activo) {
		return invalido("El usuario no existe o está inactivo")
	}
	if err != nil {
		return err
	}
	if u.Tipo != "persona" {
		return invalido("Solo se asigna a personas, no a integraciones")
	}
	alcances, err := q.AlcancesDePermisoDeUsuario(ctx, soportebd.AlcancesDePermisoDeUsuarioParams{
		UsuarioID: usuarioID, Permiso: string(nucleo.PermTicketsVer),
	})
	if err != nil {
		return err
	}
	for _, a := range alcances {
		if (a.ZonaID == nil && a.CuadrillaID == nil) ||
			(a.ZonaID != nil && f.ZonaID != nil && *a.ZonaID == *f.ZonaID) ||
			(a.CuadrillaID != nil && f.CuadrillaID != nil && *a.CuadrillaID == *f.CuadrillaID) {
			return nil
		}
	}
	return invalido("El usuario no puede ver este ticket (tickets.ver con alcance sobre su zona o cuadrilla)")
}
