package mensajeria

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Rutas de la fase 3a del asistente (contratos/conversaciones.md §17).
func (m *Modulo) registrarAsistente3(api huma.API) {
	k := m.Asistente
	conf := nucleo.PermMensajeriaAsistenteConfigurar

	lst := op("listarCategoriasAsistente", http.MethodGet, "/asistente/categorias", "Categorías de mensajes de la empresa", nucleo.PermConversacionesVer, http.StatusOK)
	lst.Description += " Por `orden` («Otro» al final). La primera vez se siembran las iniciales. `nextCursor` siempre null."
	huma.Register(api, lst, func(ctx context.Context, in *EntradaListarCategoriasMensajes) (*cuerpo[ListaCategoriasMensajes], error) {
		return responder(k.Categorias.Ejecutar(ctx, *in))
	})
	crear := op("crearCategoriaAsistente", http.MethodPost, "/asistente/categorias", "Crear una categoría de mensajes", conf, http.StatusCreated, http.StatusConflict)
	crear.Description += " 409 `categoria_duplicada`; 422 `demasiadas_categorias` (30 activas). Evento `categoria.creada`."
	huma.Register(api, crear, func(ctx context.Context, in *cuerpoHTTP[CamposCategoriaMensajes]) (*cuerpo[CategoriaMensajes], error) {
		return responder(k.CrearCategoria.Ejecutar(ctx, in.Body))
	})
	act := op("actualizarCategoriaAsistente", http.MethodPost, "/asistente/categorias/{id}/actualizar", "Editar una categoría de mensajes", conf,
		http.StatusOK, http.StatusNotFound, http.StatusConflict)
	act.Description += " Con `version` (409 `conflicto_version`); 409 `categoria_duplicada`. Evento `categoria.actualizada`."
	huma.Register(api, act, func(ctx context.Context, in *idCuerpoHTTP[EntradaActualizarCategoriaMensajes]) (*cuerpo[CategoriaMensajes], error) {
		in.Body.ID = in.ID
		return responder(k.ActualizarCategoria.Ejecutar(ctx, in.Body))
	})
	des := op("desactivarCategoriaAsistente", http.MethodPost, "/asistente/categorias/{id}/desactivar", "Desactivar una categoría de mensajes", conf,
		http.StatusOK, http.StatusNotFound, http.StatusConflict)
	des.Description += " No se borran: las conversaciones ya clasificadas la conservan. «Otro» no se desactiva (422 `categoria_otro`)."
	huma.Register(api, des, func(ctx context.Context, in *idCuerpoHTTP[CuerpoVersion]) (*cuerpo[CategoriaMensajes], error) {
		return responder(k.DesactivarCategoria.Ejecutar(ctx, EntradaCambioCategoriaMensajes{ID: in.ID, Version: in.Body.Version}))
	})
	huma.Register(api, op("activarCategoriaAsistente", http.MethodPost, "/asistente/categorias/{id}/activar", "Activar una categoría de mensajes", conf,
		http.StatusOK, http.StatusNotFound, http.StatusConflict), func(ctx context.Context, in *idCuerpoHTTP[CuerpoVersion]) (*cuerpo[CategoriaMensajes], error) {
		return responder(k.ActivarCategoria.Ejecutar(ctx, EntradaCambioCategoriaMensajes{ID: in.ID, Version: in.Body.Version}))
	})

	cat := op("categorizarConversacion", http.MethodPost, "/conversaciones/{id}/categorizar", "Corregir la categoría de una conversación",
		nucleo.PermConversacionesCategorizar, http.StatusOK, http.StatusNotFound, http.StatusConflict)
	cat.Description += " `categoriaId: null` la quita. Queda `categoriaOrigen: agente` y el asistente no la cambia hasta que se resuelva y se reabra. " +
		"Sin asignar y con cola en la categoría: pasa a esa cola. El SLA de la categoría gana sobre el de la cola. 422 `categoria_inactiva`. " +
		"Evento `conversacion.categorizada`."
	huma.Register(api, cat, func(ctx context.Context, in *idCuerpoHTTP[EntradaCategorizar]) (*cuerpo[ConversacionClienteDetalle], error) {
		in.Body.ID = in.ID
		return responder(k.Categorizar.Ejecutar(ctx, in.Body))
	})

	pau := op("pausarAsistenteConversacion", http.MethodPost, "/conversaciones/{id}/asistente/pausar", "Pausar el asistente de IA en esta conversación",
		nucleo.PermConversacionesPausarAsistente, http.StatusOK, http.StatusNotFound, http.StatusConflict)
	pau.Description += " El asistente deja de contestarle solo al cliente en esta conversación (las sugerencias al agente siguen) hasta que alguien lo " +
		"reanude o la conversación se resuelva. Vale aunque nadie la tenga asignada. Si estaba atendiendo, queda `detenido` y el turno en curso no envía nada. " +
		"Repetirla es 200 sin cambios. 422 `conversacion_resuelta`. Evento `conversacion.asistente_pausado`; `atencionAsistente` lo refleja."
	huma.Register(api, pau, func(ctx context.Context, in *idCuerpoHTTP[EntradaPausarAsistente]) (*cuerpo[ConversacionClienteDetalle], error) {
		in.Body.ID = in.ID
		return responder(k.Pausar.Ejecutar(ctx, in.Body))
	})
	rea := op("reanudarAsistenteConversacion", http.MethodPost, "/conversaciones/{id}/asistente/reanudar", "Devolverle la conversación al asistente de IA",
		nucleo.PermConversacionesPausarAsistente, http.StatusOK, http.StatusNotFound, http.StatusConflict)
	rea.Description += " Quita la pausa y, si el asistente quedó detenido porque una persona la tomó o le escribió al cliente (o él mismo la pasó a una persona), " +
		"vuelve a poder atender. Si el cliente está esperando y nadie la tiene asignada, contesta ese mensaje enseguida; con una persona asignada espera a que " +
		"la suelte. Sin nada que reanudar es 200 sin cambios. 422 `conversacion_resuelta`. Evento `conversacion.asistente_reanudado`."
	huma.Register(api, rea, func(ctx context.Context, in *idCuerpoHTTP[EntradaAccionConv]) (*cuerpo[ConversacionClienteDetalle], error) {
		in.Body.ID = in.ID
		return responder(k.Reanudar.Ejecutar(ctx, in.Body))
	})

	prob := op("probarMensajeAsistente", http.MethodPost, "/asistente/probar-mensaje", "«Pruébalo antes de guardar»: qué haría el asistente con un mensaje", conf,
		http.StatusOK, http.StatusTooManyRequests, http.StatusBadGateway)
	prob.Description += " Con los ajustes del formulario sin guardar (lo que falta sale de lo guardado). **No ejecuta nada**: las acciones solo se describen; " +
		"no toca conversaciones ni Argos. Cuenta para el tope (422 `tope_alcanzado`) y para los 10 pedidos por minuto (429 `demasiados_pedidos`). " +
		"422 `sin_configurar`; 502 `proveedor_no_responde`, `proveedor_error`, `clave_invalida`, `proveedor_redirige`, `rechazo_modelo`, `respuesta_invalida`."
	huma.Register(api, prob, func(ctx context.Context, in *cuerpoHTTP[EntradaProbarMensaje]) (*cuerpo[MensajeProbado], error) {
		return responder(k.ProbarMensaje.Ejecutar(ctx, in.Body))
	})
	ef := op("efectoAsistente", http.MethodGet, "/asistente/efecto", "Efecto del asistente en un mes (primera respuesta y resueltas sin persona)", conf, http.StatusOK)
	ef.Description += " Sobre las conversaciones creadas en el mes que el asistente clasificó. `primeraRespuestaPromedioAntesSeg`: los 30 días antes de activarlo."
	huma.Register(api, ef, func(ctx context.Context, in *EntradaUsoAsistente) (*cuerpo[EfectoAsistente], error) {
		return responder(k.Efecto.Ejecutar(ctx, *in))
	})
}
