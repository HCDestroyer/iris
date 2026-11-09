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
		"(409 `conflicto_version`). Activar exige URL y modelo (Anthropic: clave); 422 `sin_configurar`, `url_no_permitida`. Evento `asistente.ajustes_cambiados`."
	huma.Register(api, act, func(ctx context.Context, in *cuerpoHTTP[EntradaAjustesAsistente]) (*cuerpo[AjustesAsistente], error) {
		return responder(k.FijarAjustes.Ejecutar(ctx, in.Body))
	})
	prob := op("probarAsistente", http.MethodPost, "/asistente/probar", "Probar la conexión con el proveedor de IA", conf, http.StatusOK)
	prob.Description += " Manda un pedido corto real (no cuenta para el tope) y responde `estado` ok o error con `detalle` en español, sin secretos. " +
		"Sin `clave` usa la guardada solo si proveedor y URL son los guardados. No guarda nada."
	huma.Register(api, prob, func(ctx context.Context, in *cuerpoHTTP[EntradaProbarAsistente]) (*cuerpo[PruebaAsistente], error) {
		return responder(k.Probar.Ejecutar(ctx, in.Body))
	})
	mods := op("modelosAsistente", http.MethodGet, "/asistente/modelos", "Modelos que ofrece el proveedor", conf, http.StatusOK, http.StatusBadGateway)
	mods.Description += " Sin parámetros: lo guardado. 502 `proveedor_no_responde` / `proveedor_error` / `clave_invalida` si el proveedor falla."
	huma.Register(api, mods, func(ctx context.Context, in *EntradaModelosAsistente) (*cuerpo[ListaModelosAsistente], error) {
		return responder(k.Modelos.Ejecutar(ctx, *in))
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
		http.StatusAccepted, http.StatusNotFound)
	pedir.Description += " → 202 con la sugerencia en `generando`; cuando termina llega por el flujo (evento `sugerencia`) o en `GET …/sugerencias/actual`. " +
		"La vigente queda `descartada`. 422 `asistente_inactivo`, `tope_alcanzado`, `sin_mensaje`, `conversacion_resuelta`. El asistente nunca envía."
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
		http.StatusOK, http.StatusNotFound, http.StatusBadGateway)
	res.Description += " Síncrono (hasta 90 s). Cuenta para el tope; no se guarda el texto. 422 `asistente_inactivo`, `tope_alcanzado`, `sin_mensaje`; " +
		"502 `proveedor_no_responde`, `proveedor_error`, `clave_invalida`, `rechazo_modelo`."
	huma.Register(api, res, func(ctx context.Context, in *idHTTP) (*cuerpo[ResumenConversacion], error) {
		return responder(k.Resumen.Ejecutar(ctx, EntradaConversacion{ID: in.ID}))
	})
}
