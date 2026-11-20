package mensajeria

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Rutas del asistente de IA (contratos/conversaciones.md §16).

type sugerenciaHTTP struct {
	nucleo.ConIdempotencia
	ID uuid.UUID `path:"id"`
}

type sugerenciaCuerpoHTTP[T any] struct {
	nucleo.ConIdempotencia
	ID   uuid.UUID `path:"id"`
	Body T
}

type entradaPedirHTTP struct {
	nucleo.ConIdempotencia
	ID   int64 `path:"id" minimum:"1"`
	Body *CuerpoPedirSugerencia
}

func (m *Modulo) registrarAsistente(api huma.API) {
	k := m.Asistente
	conf, sug := nucleo.PermMensajeriaAsistenteConfigurar, nucleo.PermConversacionesSugerencias

	// Ajustes (admin) -------------------------------------------------------------------------------
	huma.Register(api, op("verAjustesAsistente", http.MethodGet, "/asistente/ajustes", "Ajustes del asistente de IA de la empresa", conf, http.StatusOK),
		func(ctx context.Context, _ *struct{}) (*cuerpo[AjustesAsistente], error) {
			return responder(k.VerAjustes.Ejecutar(ctx, struct{}{}))
		})
	act := op("actualizarAjustesAsistente", http.MethodPost, "/asistente/ajustes/actualizar", "Guardar los ajustes del asistente de IA", conf,
		http.StatusOK, http.StatusConflict)
	act.Description += " `clave` es solo escritura (ausente la deja, \"\" la borra) y nunca se devuelve: `claveGuardadaEn`. `version` 0 la primera vez " +
		"(409 `conflicto_version`). Activar exige URL y modelo (Anthropic: clave; `servidor_ia`: solo el modelo); 422 `sin_configurar`, `url_no_permitida`; 422 `clave_requerida` si " +
		"cambia el proveedor o la URL con clave guardada y sin `clave`. Evento `asistente.ajustes_cambiados`."
	huma.Register(api, act, func(ctx context.Context, in *cuerpoHTTP[EntradaAjustesAsistente]) (*cuerpo[AjustesAsistente], error) {
		return responder(k.FijarAjustes.Ejecutar(ctx, in.Body))
	})
	// Reconexión temporal sin personal de caja (contrato con Argos 0.8.0 §7.3, B4).
	huma.Register(api, op("verAjustesReconexion", http.MethodGet, "/asistente/reconexion", "Reconexión temporal: ajustes de la empresa", conf, http.StatusOK),
		func(ctx context.Context, _ *struct{}) (*cuerpo[AjustesReconexion], error) {
			return responder(k.VerReconexion.Ejecutar(ctx, struct{}{}))
		})
	rec := op("actualizarAjustesReconexion", http.MethodPost, "/asistente/reconexion/actualizar", "Encender o apagar la reconexión sin personal de caja", conf,
		http.StatusOK, http.StatusConflict)
	rec.Description += " `sinPersonal`: también en horario hábil cuando ninguna persona con el rol caja está disponible (apagado de fábrica). Solo se pide si " +
		"el Argos conectado informa contrato ≥ 0.8.0 (`argosLoPermite`) y la empresa tiene a alguien con el rol caja. `version` 0 la primera vez (409 " +
		"`conflicto_version`). Evento `reconexion.ajustes_cambiados`."
	huma.Register(api, rec, func(ctx context.Context, in *cuerpoHTTP[EntradaAjustesReconexion]) (*cuerpo[AjustesReconexion], error) {
		return responder(k.FijarReconexion.Ejecutar(ctx, in.Body))
	})
	prob := op("probarAsistente", http.MethodPost, "/asistente/probar", "Probar la conexión con el proveedor de IA", conf, http.StatusOK, http.StatusConflict, http.StatusBadGateway)
	prob.Description += " Manda un pedido corto real (no cuenta para el tope). Cualquier falla responde lo mismo (`estado: error`, `latenciaMs: 0`, " +
		"`detalle` único) para no servir como escáner de puertos. Sin `clave` usa la guardada solo si proveedor y URL son los guardados. No guarda nada. " +
		"Con `servidor_ia` (sin URL ni clave que revisar) la falla es propia: 409 `servidor_desactivado` o `servidor_ia_ocupado`, 502 `servidor_ia_no_disponible`."
	huma.Register(api, prob, func(ctx context.Context, in *cuerpoHTTP[EntradaProbarAsistente]) (*cuerpo[PruebaAsistente], error) {
		return responder(k.Probar.Ejecutar(ctx, in.Body))
	})
	mods := op("modelosAsistente", http.MethodGet, "/asistente/modelos", "Modelos del proveedor guardado", conf, http.StatusOK, http.StatusConflict, http.StatusBadGateway)
	mods.Description += " Solo con lo guardado (para otra URL o clave: `POST /asistente/modelos`). Si el proveedor falla: 502 `proveedor_error` con un " +
		"detalle único (no distingue conexión rechazada, tiempo agotado ni respuesta inesperada). Con `servidor_ia`: 409 `servidor_desactivado`, 502 `servidor_ia_no_disponible`."
	huma.Register(api, mods, func(ctx context.Context, _ *struct{}) (*cuerpo[ListaModelosAsistente], error) {
		return responder(k.Modelos.Ejecutar(ctx, EntradaModelosAsistente{}))
	})
	modsURL := op("consultarModelosAsistente", http.MethodPost, "/asistente/modelos", "Modelos de un proveedor (URL y clave dadas)", conf,
		http.StatusOK, http.StatusBadGateway)
	modsURL.Description += " Con sesión y CSRF. Sin `clave` usa la guardada solo si proveedor y URL son los guardados. Mismas fallas que el GET."
	huma.Register(api, modsURL, func(ctx context.Context, in *cuerpoHTTP[EntradaModelosAsistente]) (*cuerpo[ListaModelosAsistente], error) {
		if in.Body.Proveedor == "" {
			return nil, campoConv("proveedor", "Elige el proveedor", "required")
		}
		return responder(k.Modelos.Ejecutar(ctx, in.Body))
	})
	huma.Register(api, op("usoAsistente", http.MethodGet, "/asistente/uso", "Uso del asistente en un mes (tokens, sugerencias y costo)", conf, http.StatusOK),
		func(ctx context.Context, in *EntradaUsoAsistente) (*cuerpo[UsoAsistente], error) {
			return responder(k.Uso.Ejecutar(ctx, *in))
		})
	est := op("estadoAsistente", http.MethodGet, "/asistente/estado", "Si el asistente está activo, su modo y si está en pausa", sug, http.StatusOK)
	est.Description += " Para decidir si el panel muestra «Sugerir». Sin proveedor, URL, clave ni uso."
	huma.Register(api, est, func(ctx context.Context, _ *struct{}) (*cuerpo[EstadoAsistente], error) {
		return responder(k.Estado.Ejecutar(ctx, struct{}{}))
	})

	// Sugerencias -------------------------------------------------------------------------------------
	const base = "/conversaciones"
	pedir := op("pedirSugerencia", http.MethodPost, base+"/{id}/sugerencias", "Pedir una sugerencia de respuesta (también «Otra»)", sug,
		http.StatusAccepted, http.StatusNotFound, http.StatusTooManyRequests)
	pedir.Description += " → 202 con la sugerencia en `generando`; cuando termina llega por el flujo (evento `sugerencia`) o en `GET …/sugerencias/actual`. " +
		"La vigente queda `descartada` (y su generación se corta). 422 `asistente_inactivo`, `tope_alcanzado`, `sin_mensaje`, `conversacion_resuelta`; " +
		"429 `demasiados_pedidos` (10 por minuto por persona) o `demasiadas_sugerencias` (20 por hora por conversación) con `Retry-After`. El asistente nunca envía."
	huma.Register(api, pedir, func(ctx context.Context, in *entradaPedirHTTP) (*cuerpo[Sugerencia], error) {
		e := EntradaPedirSugerencia{ConversacionID: in.ID}
		if in.Body != nil {
			e.Otra = in.Body.Otra
		}
		return responder(k.Pedir.Ejecutar(ctx, e))
	})
	huma.Register(api, op("sugerenciaActual", http.MethodGet, base+"/{id}/sugerencias/actual", "La sugerencia vigente (generando, lista o error)", sug,
		http.StatusOK, http.StatusNotFound), func(ctx context.Context, in *EntradaConversacion) (*cuerpo[Sugerencia], error) {
		return responder(k.Actual.Ejecutar(ctx, *in))
	})
	usar := op("usarSugerencia", http.MethodPost, "/sugerencias/{id}/usar", "Usar la sugerencia (el panel copia el texto a la caja)", sug,
		http.StatusOK, http.StatusNotFound)
	usar.Description += " Solo desde `lista` (422 `sugerencia_no_vigente`); repetir sobre `usada` es 200. No envía nada: lo envía la persona."
	huma.Register(api, usar, func(ctx context.Context, in *sugerenciaHTTP) (*cuerpo[Sugerencia], error) {
		return responder(k.Usar.Ejecutar(ctx, EntradaSugerencia{ID: in.ID}))
	})
	huma.Register(api, op("descartarSugerencia", http.MethodPost, "/sugerencias/{id}/descartar", "Ocultar la sugerencia", sug, http.StatusOK, http.StatusNotFound),
		func(ctx context.Context, in *sugerenciaHTTP) (*cuerpo[Sugerencia], error) {
			return responder(k.Descartar.Ejecutar(ctx, EntradaSugerencia{ID: in.ID}))
		})
	huma.Register(api, op("calificarSugerencia", http.MethodPost, "/sugerencias/{id}/calificar", "👍 / 👎 a una sugerencia", sug, http.StatusOK, http.StatusNotFound),
		func(ctx context.Context, in *sugerenciaCuerpoHTTP[CuerpoCalificar]) (*cuerpo[Sugerencia], error) {
			return responder(k.Calificar.Ejecutar(ctx, EntradaCalificar{ID: in.ID, Util: in.Body.Util}))
		})
	res := op("resumirConversacion", http.MethodPost, base+"/{id}/resumen", "Resumen de la conversación (ficha del cliente)", sug,
		http.StatusOK, http.StatusNotFound, http.StatusTooManyRequests, http.StatusBadGateway)
	res.Description += " Genera el resumen (o lo regenera) y lo **guarda en la conversación**, reemplazando al anterior. Síncrono (hasta 90 s). " +
		"Reserva y cuenta para el tope. Para verlo sin gastar tokens: `GET` de esta misma ruta. 422 `asistente_inactivo`, `tope_alcanzado`, `sin_mensaje`; " +
		"429 `demasiados_pedidos`; 502 `proveedor_no_responde`, `proveedor_error`, `clave_invalida`, `proveedor_redirige`, `rechazo_modelo` " +
		"(si falla, el resumen anterior se conserva). Evento `conversacion.resumen_guardado` (sin el texto)."
	huma.Register(api, res, func(ctx context.Context, in *idHTTP) (*cuerpo[ResumenConversacion], error) {
		return responder(k.Resumen.Ejecutar(ctx, EntradaConversacion{ID: in.ID}))
	})
	verRes := op("verResumenConversacion", http.MethodGet, base+"/{id}/resumen", "El resumen guardado de la conversación", sug, http.StatusOK, http.StatusNotFound)
	verRes.Description += " No llama al modelo ni gasta tokens. `desactualizado`: hay mensajes del hilo después de `hastaMensajeId` (`mensajesNuevos` dice cuántos). " +
		"404 `sin_resumen` si nadie lo ha pedido (el detalle de la conversación trae `resumen` cuando existe, para no preguntar de más)."
	huma.Register(api, verRes, func(ctx context.Context, in *EntradaConversacion) (*cuerpo[ResumenConversacion], error) {
		return responder(k.VerResumen.Ejecutar(ctx, *in))
	})
	m.registrarAsistente3(api)
}
