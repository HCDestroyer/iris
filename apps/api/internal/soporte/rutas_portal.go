package soporte

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Rutas del portal del cliente (/api/v1/portal/…, cookie iris_portal). Handlers delgados.

var seguridadPortal = []map[string][]string{{"portal": {}}}

func operacionPortal(id, metodo, ruta, resumen, detalle string, status int, errores ...int) huma.Operation {
	return huma.Operation{
		OperationID:   id,
		Method:        metodo,
		Path:          ruta,
		Summary:       resumen,
		Description:   "Solo la sesión del portal (actor cliente: cliente y empresa de Argos de la sesión); cualquier otra credencial, 403. " + detalle,
		Tags:          []string{"portal"},
		DefaultStatus: status,
		Errors:        append([]int{http.StatusUnauthorized, http.StatusForbidden, http.StatusUnprocessableEntity}, errores...),
		Security:      seguridadPortal,
	}
}

type conIDPortal[T any] struct {
	nucleo.ConIdempotencia
	ID   int64 `path:"id" minimum:"1"`
	Body T
}

func (m *Modulo) registrarPortal(api huma.API) {
	huma.Register(api, operacionPortal("listarTicketsPortal", http.MethodGet, "/api/v1/portal/tickets", "Mis tickets",
		"Solo los del cliente de la sesión; más recientes primero (cursor).", http.StatusOK),
		func(ctx context.Context, in *EntradaListarTicketsPortal) (*cuerpo[nucleo.Pagina[TicketPortal]], error) {
			return responder(m.ListarTicketsPortal.Ejecutar(ctx, *in))
		})

	crear := operacionPortal("crearTicketPortal", http.MethodPost, "/api/v1/portal/tickets", "Abrir un ticket",
		"Origen `portal`, prioridad media, categoría de `GET /portal/categorias` (otra: 422 en `categoriaId`), servicio del cliente según la instantánea de Argos (si no: 422 en `servicioId`). El contacto sale de la ficha del cliente. Si su servicio está en un router con un incidente maestro abierto, se vincula a él. Emite `ticket.creado`.",
		http.StatusCreated)
	huma.Register(api, crear, func(ctx context.Context, in *conClave[EntradaCrearTicketPortal]) (*cuerpo[TicketPortal], error) {
		e := in.Body
		e.ClaveIdempotencia = in.IdempotencyKey
		return responder(m.CrearTicketPortal.Ejecutar(ctx, e))
	})

	huma.Register(api, operacionPortal("verTicketPortal", http.MethodGet, "/api/v1/portal/tickets/{id}", "Ver mi ticket",
		"De otro cliente o de otra empresa: 404.", http.StatusOK, http.StatusNotFound),
		func(ctx context.Context, in *EntradaVerTicketPortal) (*cuerpo[TicketPortal], error) {
			return responder(m.VerTicketPortal.Ejecutar(ctx, *in))
		})

	huma.Register(api, operacionPortal("listarMensajesPortal", http.MethodGet, "/api/v1/portal/tickets/{id}/mensajes", "Conversación de mi ticket",
		"Solo los mensajes públicos (nunca notas internas ni el historial), más antiguos primero.", http.StatusOK, http.StatusNotFound),
		func(ctx context.Context, in *EntradaListarMensajesPortal) (*cuerpo[nucleo.Pagina[MensajePortal]], error) {
			return responder(m.ListarMensajesPortal.Ejecutar(ctx, *in))
		})

	responderOp := operacionPortal("responderTicketPortal", http.MethodPost, "/api/v1/portal/tickets/{id}/responder", "Escribir en mi ticket",
		"Mensaje del cliente. en_espera → abierto (reanuda) y resuelto → abierto (reabre); en cerrado, 422 `transicion_invalida`. Emite `ticket.mensaje_cliente` (y `ticket.estado_cambiado` si cambió).",
		http.StatusOK, http.StatusNotFound, http.StatusConflict)
	huma.Register(api, responderOp, func(ctx context.Context, in *conIDPortal[EntradaResponderTicketPortal]) (*cuerpo[TicketPortalConMensaje], error) {
		e := in.Body
		e.ID, e.ClaveIdempotencia = in.ID, in.IdempotencyKey
		return responder(m.ResponderTicketPortal.Ejecutar(ctx, e))
	})

	confirmar := operacionPortal("confirmarSolucionPortal", http.MethodPost, "/api/v1/portal/tickets/{id}/confirmar-solucion", "Confirmar la solución",
		"resuelto → cerrado. Si no está resuelto, 422 `transicion_invalida`. Emite `ticket.estado_cambiado`.",
		http.StatusOK, http.StatusNotFound, http.StatusConflict)
	huma.Register(api, confirmar, func(ctx context.Context, in *conIDPortal[EntradaConfirmarSolucion]) (*cuerpo[TicketPortal], error) {
		e := in.Body
		e.ID = in.ID
		return responder(m.ConfirmarSolucionPortal.Ejecutar(ctx, e))
	})

	huma.Register(api, operacionPortal("listarCategoriasPortal", http.MethodGet, "/api/v1/portal/categorias", "Categorías que el cliente puede elegir",
		"Las activas marcadas como visibles en el portal (globales y de su empresa).", http.StatusOK),
		func(ctx context.Context, _ *vacio) (*cuerpo[nucleo.Pagina[CategoriaPortal]], error) {
			return responder(m.ListarCategoriasPortal.Ejecutar(ctx, struct{}{}))
		})
}
