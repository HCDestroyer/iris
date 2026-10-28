package soporte

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Handlers delgados: leen la petición, llaman al caso de uso y arman la respuesta.

const etiqueta = "soporte"

var seguridad = []map[string][]string{{"sesion": {}}, {"bearer": {}}}

func operacion(id, metodo, ruta, resumen string, permiso nucleo.Permiso, status int, errores ...int) huma.Operation {
	return huma.Operation{
		OperationID:   id,
		Method:        metodo,
		Path:          ruta,
		Summary:       resumen,
		Description:   "Permiso: `" + string(permiso) + "`.",
		Tags:          []string{etiqueta},
		DefaultStatus: status,
		Errors:        append([]int{http.StatusUnauthorized, http.StatusForbidden, http.StatusUnprocessableEntity}, errores...),
		Security:      seguridad,
		Extensions:    map[string]any{"x-permiso": string(permiso)},
	}
}

// accion documenta una acción de estado: 404, 409 (versión) y 422 (transicion_invalida).
func accion(id, ruta, resumen, detalle string, permiso nucleo.Permiso) huma.Operation {
	op := operacion(id, http.MethodPost, "/api/v1/tickets/{id}/"+ruta, resumen, permiso, http.StatusOK, http.StatusNotFound, http.StatusConflict)
	op.Description += " " + detalle
	return op
}

type cuerpo[T any] struct {
	Body T
}

type conClave[T any] struct {
	nucleo.ConIdempotencia
	Body T
}

type conID[T any] struct {
	nucleo.ConIdempotencia
	ID   int64 `path:"id" minimum:"1"`
	Body T
}

type vacio struct{}

func responder[T any](v T, err error) (*cuerpo[T], error) {
	if err != nil {
		return nil, err
	}
	return &cuerpo[T]{Body: v}, nil
}

// registrarAccion publica POST /tickets/{id}/<ruta> para un caso cuya entrada lleva el id.
func registrarAccion[In, Out any](api huma.API, op huma.Operation, ejecutar func(context.Context, In) (Out, error), conIDFn func(*In, int64)) {
	huma.Register(api, op, func(ctx context.Context, in *conID[In]) (*cuerpo[Out], error) {
		e := in.Body
		conIDFn(&e, in.ID)
		return responder(ejecutar(ctx, e))
	})
}

// Registrar publica las rutas de soporte.
func (m *Modulo) Registrar(api huma.API) {
	huma.Register(api, operacion("listarTickets", http.MethodGet, "/api/v1/tickets", "Listar tickets (bandeja)", nucleo.PermTicketsVer, http.StatusOK),
		func(ctx context.Context, in *EntradaListarTickets) (*cuerpo[nucleo.Pagina[Ticket]], error) {
			return responder(m.ListarTickets.Ejecutar(ctx, *in))
		})

	crear := operacion("crearTicket", http.MethodPost, "/api/v1/tickets", "Crear un ticket", nucleo.PermTicketsCrear, http.StatusCreated)
	crear.Description += " Calcula el SLA (categoría → SLA, o el por defecto; objetivos de la prioridad) y emite `ticket.creado`."
	huma.Register(api, crear, func(ctx context.Context, in *conClave[EntradaCrearTicket]) (*cuerpo[Ticket], error) {
		return responder(m.CrearTicket.Ejecutar(ctx, in.Body))
	})

	huma.Register(api, operacion("verTicket", http.MethodGet, "/api/v1/tickets/{id}", "Ver un ticket", nucleo.PermTicketsVer, http.StatusOK, http.StatusNotFound),
		func(ctx context.Context, in *EntradaVerTicket) (*cuerpo[Ticket], error) {
			return responder(m.VerTicket.Ejecutar(ctx, *in))
		})

	huma.Register(api, operacion("listarMensajesTicket", http.MethodGet, "/api/v1/tickets/{id}/mensajes", "Conversación del ticket (mensajes y notas internas)", nucleo.PermTicketsVer, http.StatusOK, http.StatusNotFound),
		func(ctx context.Context, in *EntradaListarMensajes) (*cuerpo[nucleo.Pagina[Mensaje]], error) {
			return responder(m.ListarMensajes.Ejecutar(ctx, *in))
		})

	huma.Register(api, operacion("listarEventosTicket", http.MethodGet, "/api/v1/tickets/{id}/eventos", "Historial del ticket (estado, asignación, escalamiento y SLA)", nucleo.PermTicketsVer, http.StatusOK, http.StatusNotFound),
		func(ctx context.Context, in *EntradaListarEventos) (*cuerpo[nucleo.Pagina[EventoTicket]], error) {
			return responder(m.ListarEventos.Ejecutar(ctx, *in))
		})

	registrarAccion(api, accion("asignarTicket", "asignar", "Asignar un ticket",
		"A un usuario y/o una cuadrilla (sin ambos, queda sin asignar). Un ticket nuevo pasa a abierto. Emite `ticket.asignado`.",
		nucleo.PermTicketsAsignar), m.AsignarTicket.Ejecutar, func(e *EntradaAsignarTicket, id int64) { e.ID = id })

	registrarAccion(api, accion("responderTicket", "responder", "Responder al cliente (mensaje público)",
		"Cuenta como primera respuesta del SLA; un ticket nuevo pasa a abierto. No se permite en cerrado. Emite `ticket.respondido`.",
		nucleo.PermTicketsResponder), m.ResponderTicket.Ejecutar, func(e *EntradaResponderTicket, id int64) { e.ID = id })

	registrarAccion(api, accion("agregarNotaInterna", "nota-interna", "Agregar una nota interna",
		"El cliente no la ve; no cambia el estado ni el SLA. Se permite en cualquier estado.",
		nucleo.PermTicketsNotaInterna), m.AgregarNotaInterna.Ejecutar, func(e *EntradaNotaInterna, id int64) { e.ID = id })

	registrarAccion(api, accion("escalarTicket", "escalar", "Escalar un ticket",
		"Sube el nivel y, si viene, la prioridad (mayor que la actual; 422 `prioridad_no_mayor`); recalcula el SLA. Emite `ticket.escalado`.",
		nucleo.PermTicketsEscalar), m.EscalarTicket.Ejecutar, func(e *EntradaEscalarTicket, id int64) { e.ID = id })

	registrarAccion(api, accion("ponerTicketEnEspera", "poner-en-espera", "Poner un ticket en espera",
		"abierto → en_espera. Pausa el reloj de resolución. Emite `ticket.estado_cambiado`.",
		nucleo.PermTicketsPonerEnEspera), m.PonerTicketEnEspera.Ejecutar, func(e *EntradaPonerEnEspera, id int64) { e.ID = id })

	registrarAccion(api, accion("reanudarTicket", "reanudar", "Reanudar un ticket en espera",
		"en_espera → abierto. El tiempo laboral en espera corre el vencimiento de resolución. Emite `ticket.estado_cambiado`.",
		nucleo.PermTicketsReanudar), m.ReanudarTicket.Ejecutar, func(e *EntradaReanudarTicket, id int64) { e.ID = id })

	registrarAccion(api, accion("resolverTicket", "resolver", "Resolver un ticket",
		"nuevo, abierto o en_espera → resuelto, con la solución como mensaje público. Emite `ticket.estado_cambiado`.",
		nucleo.PermTicketsResolver), m.ResolverTicket.Ejecutar, func(e *EntradaResolverTicket, id int64) { e.ID = id })

	registrarAccion(api, accion("reabrirTicket", "reabrir", "Reabrir un ticket resuelto",
		"resuelto → abierto. El tiempo en resuelto no cuenta para el SLA de resolución. Emite `ticket.estado_cambiado`.",
		nucleo.PermTicketsReabrir), m.ReabrirTicket.Ejecutar, func(e *EntradaReabrirTicket, id int64) { e.ID = id })

	registrarAccion(api, accion("cerrarTicket", "cerrar", "Cerrar un ticket",
		"Desde resuelto, o desde nuevo, abierto o en_espera con motivo. Cerrado es final. Emite `ticket.estado_cambiado`.",
		nucleo.PermTicketsCerrar), m.CerrarTicket.Ejecutar, func(e *EntradaCerrarTicket, id int64) { e.ID = id })

	registrarAccion(api, accion("fusionarTickets", "fusionar", "Fusionar un ticket duplicado en otro",
		"El ticket {id} se cierra fusionado en `destinoId` (no cerrado, misma empresa; si no, 422 `fusion_invalida`). Emite `ticket.estado_cambiado` y `ticket.fusionado`.",
		nucleo.PermTicketsFusionar), m.FusionarTickets.Ejecutar, func(e *EntradaFusionarTickets, id int64) { e.ID = id })

	huma.Register(api, operacion("listarCategoriasTicket", http.MethodGet, "/api/v1/categorias", "Categorías de ticket", nucleo.PermTicketsVer, http.StatusOK),
		func(ctx context.Context, in *EntradaListarCategorias) (*cuerpo[nucleo.Pagina[Categoria]], error) {
			return responder(m.ListarCategorias.Ejecutar(ctx, *in))
		})

	huma.Register(api, operacion("listarSLAs", http.MethodGet, "/api/v1/slas", "SLA con sus objetivos por prioridad", nucleo.PermTicketsVer, http.StatusOK),
		func(ctx context.Context, _ *vacio) (*cuerpo[nucleo.Pagina[SLA]], error) {
			return responder(m.ListarSLAs.Ejecutar(ctx, struct{}{}))
		})
}
