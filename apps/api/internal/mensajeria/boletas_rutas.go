package mensajeria

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Rutas de boletas (contratos/conversaciones.md §6.4.1; contrato con Argos 0.7.0 §7.7).

type boletaHTTP struct {
	ID uuid.UUID `path:"id" format:"uuid"`
}

type boletaCuerpoHTTP[T any] struct {
	nucleo.ConIdempotencia
	ID   uuid.UUID `path:"id" format:"uuid"`
	Body T
}

const descErroresDecision = " Solo una persona con su sesión del panel (token, integración o asistente de IA: 403 `solo_personas`) que vea la conversación, en su empresa activa (si no, 404). " +
	"La boleta debe estar `registrada` (con `comprobanteId`): si no, 422 `boleta_no_registrada`; ya decidida, 409 `boleta_ya_decidida` (repetir la misma decisión la misma persona responde 200 con la boleta). " +
	"Iris llama a Argos en el momento (`X-Iris-Actor: usuario:<id>`, `X-Iris-Conversacion: iris:conversacion:<id>`) y **Argos decide** con el usuario de Argos vinculado. " +
	"Acepta `Idempotency-Key`, pero la respuesta no se guarda con ella: la idempotencia la da el comprobante (repetir es seguro, también después de un error). " +
	"Errores: 409 `argos_sin_vinculo` («Vincula tu usuario de Argos»: `POST /integraciones/argosisp/abrir` sin destino y reintentar); 403 `argos_usuario_inactivo`, `argos_sin_permiso` («Tu usuario de Argos no tiene permiso de caja») y `argos_sin_empresa`; " +
	"409 `comprobante_revisado` (ya lo resolvió caja u otra persona: la boleta local queda sincronizada, vuelve a leerla), `otra_persona_decidio`, `comprobante_no_es_de_iris`, `iris_apagado`, `idempotencia_en_curso` y `comprobante_no_existe`; " +
	"422 `argos_sin_aprobacion` (Argos no informa contrato ≥ 0.7.0), `decision_no_permitida` y `conversacion_solo_lectura`; 502 `argos_no_disponible` (Argos no respondió: «Reintentar», nada quedó a medias) y `argos_rechazo`; 503 `conector_apagado`. " +
	"Con el 200 la boleta queda resuelta, la conversación recibe un mensaje de sistema (`boleta_aprobada` / `boleta_rechazada`) y al cliente se le avisa una sola vez (`avisoCliente`)."

func (m *Modulo) registrarBoletas(api huma.API) {
	ver := op("verBoleta", http.MethodGet, "/boletas/{id}", "Ver una boleta y si se puede aprobar o rechazar", nucleo.PermConversacionesVer, http.StatusOK, http.StatusNotFound)
	ver.Description += " Exige ver su conversación (si no, 404). `puedeDecidir`: está `registrada` en Argos y el Argos conectado informa contrato ≥ 0.7.0. " +
		"Decidida: `estado` (`aprobada` o `rechazada`), `verificadoPorNombre`, `decididaPor` (si fue desde Iris), `pagoId` o `motivoRechazo` y `causaRechazo`."
	huma.Register(api, ver, func(ctx context.Context, in *boletaHTTP) (*cuerpo[Boleta], error) {
		return responder(m.verBoleta.Ejecutar(ctx, EntradaBoletaID{ID: in.ID}))
	})
	lista := op("listarBoletasConversacion", http.MethodGet, "/conversaciones/{id}/boletas", "Boletas de una conversación", nucleo.PermConversacionesVer, http.StatusOK, http.StatusNotFound)
	lista.Description += " En orden de registro, con `puedeDecidir` y la decisión de cada una. El panel la vuelve a leer cuando llega un mensaje de sistema `boleta_registrada`, `boleta_fallida`, `boleta_aprobada` o `boleta_rechazada`."
	huma.Register(api, lista, func(ctx context.Context, in *struct {
		ID int64 `path:"id" minimum:"1"`
	}) (*cuerpo[ListaBoletas], error) {
		return responder(m.listarBoletas.Ejecutar(ctx, EntradaConversacion{ID: in.ID}))
	})
	aprobar := op("aprobarBoleta", http.MethodPost, "/boletas/{id}/aprobar", "Aprobar la boleta y registrar el pago en Argos", nucleo.PermBoletasAprobar, http.StatusOK,
		http.StatusNotFound, http.StatusConflict, http.StatusBadGateway, http.StatusServiceUnavailable)
	aprobar.Description += " Se aprueba por el monto del comprobante (sin monto ni fecha): si el depósito es por otro monto, se rechaza con causa `monto_distinto`." + descErroresDecision
	huma.Register(api, aprobar, func(ctx context.Context, in *boletaCuerpoHTTP[EntradaAprobarBoleta]) (*cuerpo[Boleta], error) {
		in.Body.ID = in.ID
		return responder(m.boletas.Aprobar(ctx, in.Body))
	})
	rechazar := op("rechazarBoleta", http.MethodPost, "/boletas/{id}/rechazar", "Rechazar la boleta en Argos, con motivo y causa", nucleo.PermBoletasRechazar, http.StatusOK,
		http.StatusNotFound, http.StatusConflict, http.StatusBadGateway, http.StatusServiceUnavailable)
	rechazar.Description += " El `motivo` se le dice al cliente. Con causa `falta_dato` el cliente recibe «le falta un dato» y puede mandar la boleta de nuevo." + descErroresDecision
	huma.Register(api, rechazar, func(ctx context.Context, in *boletaCuerpoHTTP[EntradaRechazarBoleta]) (*cuerpo[Boleta], error) {
		in.Body.ID = in.ID
		return responder(m.boletas.Rechazar(ctx, in.Body))
	})
}
