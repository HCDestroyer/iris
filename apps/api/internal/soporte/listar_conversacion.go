package soporte

import (
	"context"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	soportebd "github.com/HCDestroyer/iris/apps/api/internal/soporte/bd"
)

// EntradaListarMensajes: conversación del ticket en orden (más antiguos primero).
type EntradaListarMensajes struct {
	nucleo.ParamsPagina
	ID           int64 `path:"id" minimum:"1"`
	SoloPublicos bool  `query:"soloPublicos" doc:"Sin notas internas (lo que ve el cliente)"`
}

// EntradaListarEventos: historial del ticket en orden.
type EntradaListarEventos struct {
	nucleo.ParamsPagina
	ID int64 `path:"id" minimum:"1"`
}

func (m *Modulo) nuevoListarMensajes() *auditoria.Caso[EntradaListarMensajes, nucleo.Pagina[Mensaje]] {
	return auditoria.Nuevo(auditoria.Caso[EntradaListarMensajes, nucleo.Pagina[Mensaje]]{
		Accion:       "tickets.ver",
		Permiso:      nucleo.PermTicketsVer,
		AuditarExito: soloLecturas[nucleo.Pagina[Mensaje]],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaListarMensajes) (nucleo.Pagina[Mensaje], error) {
			t.FijarEntidad("ticket", in.ID)
			var despues int64
			if _, err := nucleo.DecodificarCursor(in.Cursor, &despues); err != nil {
				return nucleo.Pagina[Mensaje]{}, err
			}
			q := soportebd.New(t)
			if _, err := leer(ctx, q, in.ID); err != nil {
				return nucleo.Pagina[Mensaje]{}, err
			}
			filas, err := q.ListarMensajes(ctx, soportebd.ListarMensajesParams{
				TicketID: in.ID, DespuesDe: despues, SoloPublicos: in.SoloPublicos,
				Limite: int32(in.Limite() + 1), //nolint:gosec // ≤ 201
			})
			if err != nil {
				return nucleo.Pagina[Mensaje]{}, err
			}
			return nucleo.Paginar(filas, in.Limite(), aMensaje,
				func(f soportebd.SoporteTicketMensaje) []any { return []any{f.ID} }), nil
		},
	})
}

func (m *Modulo) nuevoListarEventos() *auditoria.Caso[EntradaListarEventos, nucleo.Pagina[EventoTicket]] {
	return auditoria.Nuevo(auditoria.Caso[EntradaListarEventos, nucleo.Pagina[EventoTicket]]{
		Accion:       "tickets.ver",
		Permiso:      nucleo.PermTicketsVer,
		AuditarExito: soloLecturas[nucleo.Pagina[EventoTicket]],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaListarEventos) (nucleo.Pagina[EventoTicket], error) {
			t.FijarEntidad("ticket", in.ID)
			var despues int64
			if _, err := nucleo.DecodificarCursor(in.Cursor, &despues); err != nil {
				return nucleo.Pagina[EventoTicket]{}, err
			}
			q := soportebd.New(t)
			if _, err := leer(ctx, q, in.ID); err != nil {
				return nucleo.Pagina[EventoTicket]{}, err
			}
			filas, err := q.ListarEventosTicket(ctx, soportebd.ListarEventosTicketParams{
				TicketID: in.ID, DespuesDe: despues, Limite: int32(in.Limite() + 1), //nolint:gosec // ≤ 201
			})
			if err != nil {
				return nucleo.Pagina[EventoTicket]{}, err
			}
			return nucleo.Paginar(filas, in.Limite(), aEventoTicket,
				func(f soportebd.SoporteTicketEvento) []any { return []any{f.ID} }), nil
		},
	})
}
