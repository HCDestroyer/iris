package soporte

import (
	"context"
	"strings"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	soportebd "github.com/HCDestroyer/iris/apps/api/internal/soporte/bd"
)

// EntradaListarTickets: bandeja con filtros, más recientes primero. El alcance del permiso
// tickets.ver (zonas, cuadrillas o lo asignado al actor) y la empresa activa filtran siempre.
type EntradaListarTickets struct {
	nucleo.ParamsPagina
	Estado        []string `query:"estado" enum:"nuevo,abierto,en_espera,resuelto,cerrado" doc:"Uno o varios estados (estado=nuevo,abierto)"`
	Prioridad     []string `query:"prioridad" enum:"baja,media,alta,critica" doc:"Una o varias prioridades"`
	CategoriaID   int64    `query:"categoriaId" minimum:"0" doc:"Categoría"`
	AsignadoA     int64    `query:"asignadoA" minimum:"0" doc:"Usuario asignado"`
	SinAsignar    bool     `query:"sinAsignar" doc:"Solo tickets sin usuario asignado"`
	ClienteID     int64    `query:"clienteId" minimum:"0" doc:"Cliente de Argos (historial del cliente)"`
	ServicioID    int64    `query:"servicioId" minimum:"0" doc:"Servicio de Argos"`
	CuadrillaID   int64    `query:"cuadrillaId" minimum:"0" doc:"Cuadrilla"`
	TicketPadreID int64    `query:"ticketPadreId" minimum:"0" doc:"Hijos de un incidente maestro"`
	Q             string   `query:"q" maxLength:"120" doc:"Busca en número, asunto y nombre de contacto"`
}

func filtro(v int64) *int64 {
	if v <= 0 {
		return nil
	}
	return &v
}

func noNulo[T any](v []T) []T {
	if v == nil {
		return []T{}
	}
	return v
}

func (m *Modulo) nuevoListarTickets() *auditoria.Caso[EntradaListarTickets, nucleo.Pagina[Ticket]] {
	return auditoria.Nuevo(auditoria.Caso[EntradaListarTickets, nucleo.Pagina[Ticket]]{
		Accion:       "tickets.ver",
		Permiso:      nucleo.PermTicketsVer,
		AuditarExito: soloLecturas[nucleo.Pagina[Ticket]],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaListarTickets) (nucleo.Pagina[Ticket], error) {
			var antesDe int64
			hay, err := nucleo.DecodificarCursor(in.Cursor, &antesDe)
			if err != nil {
				return nucleo.Pagina[Ticket]{}, err
			}
			global, zonas, cuadrillas := contexto.PermisosDe(ctx).Alcances(string(nucleo.PermTicketsVer))
			todas, empresa := filtroEmpresa(ctx)
			p := soportebd.ListarTicketsParams{
				TodasEmpresas: todas, EmpresaID: empresa, Estados: noNulo(in.Estado), Prioridades: noNulo(in.Prioridad),
				CategoriaID: filtro(in.CategoriaID), AsignadoA: filtro(in.AsignadoA), SinAsignar: in.SinAsignar,
				ClienteID: filtro(in.ClienteID), ServicioID: filtro(in.ServicioID), CuadrillaID: filtro(in.CuadrillaID),
				TicketPadreID: filtro(in.TicketPadreID), AlcanceGlobal: global, Zonas: noNulo(zonas), Cuadrillas: noNulo(cuadrillas),
				Limite: int32(in.Limite() + 1), //nolint:gosec // ≤ 201
			}
			if hay {
				p.AntesDe = &antesDe
			}
			if q := strings.TrimSpace(in.Q); q != "" {
				p.Q = &q
			}
			if actor := contexto.ActorDe(ctx); actor.EsUsuario() {
				p.ActorID = &actor.ID
			}
			filas, err := soportebd.New(t).ListarTickets(ctx, p)
			if err != nil {
				return nucleo.Pagina[Ticket]{}, err
			}
			ahora := m.ahora()
			return nucleo.Paginar(filas, in.Limite(),
				func(f soportebd.SoporteTicket) Ticket { return aTicket(f, ahora) },
				func(f soportebd.SoporteTicket) []any { return []any{f.ID} }), nil
		},
	})
}
