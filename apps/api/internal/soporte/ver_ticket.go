package soporte

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	soportebd "github.com/HCDestroyer/iris/apps/api/internal/soporte/bd"
)

// EntradaVerTicket es la entrada de tickets.ver (detalle).
type EntradaVerTicket struct {
	ID int64 `path:"id" minimum:"1"`
}

// leer devuelve el ticket si el actor lo puede ver (si no, 404).
func leer(ctx context.Context, q *soportebd.Queries, id int64) (soportebd.SoporteTicket, error) {
	f, err := q.TicketPorID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return f, noEncontrado()
	}
	if err != nil {
		return f, err
	}
	return f, verificarAcceso(ctx, nucleo.PermTicketsVer, f)
}

func (m *Modulo) nuevoVerTicket() *auditoria.Caso[EntradaVerTicket, Ticket] {
	return auditoria.Nuevo(auditoria.Caso[EntradaVerTicket, Ticket]{
		Accion:       "tickets.ver",
		Permiso:      nucleo.PermTicketsVer,
		AuditarExito: soloLecturas[Ticket],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaVerTicket) (Ticket, error) {
			t.FijarEntidad("ticket", in.ID)
			f, err := leer(ctx, soportebd.New(t), in.ID)
			if err != nil {
				return Ticket{}, err
			}
			tk := aTicket(f, m.ahora())
			// La copia trae datos personales del cliente (documento, correo, teléfonos): solo con
			// clientes.ver, como la ficha.
			if contexto.PermisosDe(ctx).Tiene(string(nucleo.PermClientesVer)) {
				if tk.DatosArgos, err = argos.CopiaDe(ctx, t, argos.CopiaTicket, f.ID, f.EmpresaID); err != nil {
					return Ticket{}, err
				}
			}
			return tk, nil
		},
	})
}
