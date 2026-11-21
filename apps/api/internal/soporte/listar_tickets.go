package soporte

import (
	"context"
	"net/http"
	"strings"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	soportebd "github.com/HCDestroyer/iris/apps/api/internal/soporte/bd"
)

// EntradaListarTickets: bandeja con filtros, más recientes primero. El alcance del permiso
// tickets.ver (zonas, cuadrillas o lo asignado al actor) y la empresa activa filtran siempre.
//
// Filtros de cliente (contrato con Argos, pedido A7): el nombre es `clienteId` y se acepta
// `cliente_id` como alias (lo mismo `servicioId`/`servicio_id`); si llegan los dos con valores
// distintos, 422 validacion. Un token (de integración o de API) sin filtro de cliente recibe 422
// filtro_cliente_requerido, salvo que tenga tickets.listar_todos: así un filtro mal escrito no
// devuelve los tickets de todos los clientes de la empresa. Un parámetro de consulta que la ruta
// no conoce responde 422 validacion (regla `desconocido`) para cualquier actor.
type EntradaListarTickets struct {
	nucleo.ParamsPagina
	Estado      []string `query:"estado" enum:"nuevo,abierto,en_espera,resuelto,cerrado" doc:"Uno o varios estados (estado=nuevo,abierto)"`
	Prioridad   []string `query:"prioridad" enum:"baja,media,alta,critica" doc:"Una o varias prioridades"`
	CategoriaID int64    `query:"categoriaId" minimum:"0" doc:"Categoría"`
	AsignadoA   int64    `query:"asignadoA" minimum:"0" doc:"Usuario asignado"`
	SinAsignar  bool     `query:"sinAsignar" doc:"Solo tickets sin usuario asignado"`
	ClienteID   int64    `query:"clienteId" minimum:"0" doc:"Cliente de Argos (historial del cliente)"`
	ServicioID  int64    `query:"servicioId" minimum:"0" doc:"Servicio de Argos"`
	// Alias en snake_case: los que usa Argos (contrato A7). Mismo significado que clienteId y
	// servicioId; si llegan los dos con valores distintos, 422.
	ClienteIDAlias  int64  `query:"cliente_id" minimum:"0" doc:"Alias de clienteId (el nombre del contrato con Argos, A7). Si llega junto con clienteId y difieren: 422 validacion"`
	ServicioIDAlias int64  `query:"servicio_id" minimum:"0" doc:"Alias de servicioId. Si llega junto con servicioId y difieren: 422 validacion"`
	CuadrillaID     int64  `query:"cuadrillaId" minimum:"0" doc:"Cuadrilla"`
	TicketPadreID   int64  `query:"ticketPadreId" minimum:"0" doc:"Hijos de un incidente maestro"`
	Q               string `query:"q" maxLength:"120" doc:"Busca en número, asunto y nombre de contacto"`

	// desconocidos: los parámetros de consulta que la ruta no declara (los llena Resolve).
	desconocidos []string
}

// unirAlias resuelve los alias: devuelve el valor que vale (0 = sin filtro) o un ErrorCampo si
// llegaron los dos con valores distintos.
func unirAlias(valor, alias int64, campo, campoAlias string) (int64, *nucleo.ErrorCampo) {
	switch {
	case alias == 0:
		return valor, nil
	case valor == 0 || valor == alias:
		return alias, nil
	}
	return 0, &nucleo.ErrorCampo{Field: campoAlias, Rule: "distinto",
		Message: campoAlias + " es un alias de " + campo + " y llegaron con valores distintos; manda solo uno"}
}

// normalizar valida los parámetros desconocidos y une los alias en ClienteID y ServicioID.
func (in *EntradaListarTickets) normalizar() error {
	if len(in.desconocidos) > 0 {
		return nucleo.Validacion(nucleo.ErroresParametrosDesconocidos(in.desconocidos)...)
	}
	var errs []nucleo.ErrorCampo
	var e *nucleo.ErrorCampo
	if in.ClienteID, e = unirAlias(in.ClienteID, in.ClienteIDAlias, "clienteId", "cliente_id"); e != nil {
		errs = append(errs, *e)
	}
	if in.ServicioID, e = unirAlias(in.ServicioID, in.ServicioIDAlias, "servicioId", "servicio_id"); e != nil {
		errs = append(errs, *e)
	}
	if len(errs) > 0 {
		return nucleo.Validacion(errs...)
	}
	in.ClienteIDAlias, in.ServicioIDAlias = 0, 0
	return nil
}

// exigirFiltroCliente: un token (de integración o de API de una persona) que lista sin cliente
// recibe 422, salvo con tickets.listar_todos. Las personas del panel (sesión) no cambian.
func exigirFiltroCliente(ctx context.Context, clienteID int64) error {
	switch contexto.ActorDe(ctx).Tipo {
	case contexto.ActorIntegracion, contexto.ActorToken:
	default:
		return nil
	}
	if clienteID > 0 || contexto.PermisosDe(ctx).Tiene(string(nucleo.PermTicketsListarTodos)) {
		return nil
	}
	return nucleo.NuevoProblema(http.StatusUnprocessableEntity, "filtro_cliente_requerido", "Falta el filtro de cliente",
		"Con un token, la lista de tickets exige clienteId (o su alias cliente_id); para listar todos, el token necesita tickets.listar_todos",
		nucleo.ErrorCampo{Field: "clienteId", Rule: "requerido", Message: "Indica el cliente de Argos (clienteId o cliente_id)"})
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
			if err := in.normalizar(); err != nil {
				return nucleo.Pagina[Ticket]{}, err
			}
			if err := exigirFiltroCliente(ctx, in.ClienteID); err != nil {
				return nucleo.Pagina[Ticket]{}, err
			}
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
