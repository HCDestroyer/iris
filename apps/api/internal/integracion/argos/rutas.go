package argos

import (
	"context"
	"net/http"
	"reflect"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Handlers delgados: leen la petición, llaman al caso de uso y arman la respuesta.

var seguridad = []map[string][]string{{"sesion": {}}, {"bearer": {}}}

func operacion(etiqueta, id, metodo, ruta, resumen string, permiso nucleo.Permiso, status int, errores ...int) huma.Operation {
	op := huma.Operation{
		OperationID: id, Method: metodo, Path: ruta, Summary: resumen, Tags: []string{etiqueta},
		DefaultStatus: status, Security: seguridad,
		Errors: append([]int{http.StatusUnauthorized, http.StatusUnprocessableEntity}, errores...),
	}
	if permiso != "" {
		op.Extensions = map[string]any{"x-permiso": string(permiso)}
		op.Errors = append(op.Errors, http.StatusForbidden)
		op.Description = "Permiso: `" + string(permiso) + "`."
	}
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

type soloClave struct {
	nucleo.ConIdempotencia
}

type entradaWebhookHTTP struct {
	ConectorID int64  `path:"conector_id" minimum:"1"`
	WebhookID  string `header:"X-Webhook-Id" doc:"Id estable del webhook (deduplicación)"`
	Timestamp  string `header:"X-Webhook-Timestamp" doc:"Segundos Unix del envío"`
	Firma      string `header:"X-Webhook-Signature" doc:"v1=<hex(HMAC-SHA256(secreto, \"<timestamp>.<cuerpo>\"))>"`
	RawBody    []byte
}

func responder[T any](v T, err error) (*cuerpo[T], error) {
	if err != nil {
		return nil, err
	}
	return &cuerpo[T]{Body: v}, nil
}

const (
	etiquetaIntegraciones = "integraciones"
	etiquetaClientes      = "clientes"
	descConectorApagado   = " Sin conector Argos activo responde 503 `conector_apagado` (el panel trabaja con datos locales)."
)

// Registrar publica las rutas del conector Argos y de clientes en la API.
func (m *Modulo) Registrar(api huma.API) {
	// Conectores -------------------------------------------------------------------------
	huma.Register(api, operacion(etiquetaIntegraciones, "listarConectores", http.MethodGet, "/api/v1/integraciones/conectores",
		"Listar conectores (sin token ni secreto)", nucleo.PermIntegracionesVer, http.StatusOK),
		func(ctx context.Context, in *nucleo.ParamsPagina) (*cuerpo[nucleo.Pagina[Conector]], error) {
			return responder(m.ListarConectores.Ejecutar(ctx, *in))
		})

	conectar := operacion(etiquetaIntegraciones, "conectarArgos", http.MethodPost, "/api/v1/integraciones/argosisp/conectar",
		"Conectar con Argos usando un enlace generado en Argos", nucleo.PermIntegracionesConectar, http.StatusCreated,
		http.StatusBadGateway, http.StatusServiceUnavailable)
	conectar.Description += " Iris canjea el código en Argos (servidor a servidor), guarda el token `atk_…` y el secreto del webhook cifrados y deja el conector **apagado** hasta que se active. 502 `argos_rechazo` si Argos rechaza el código (vencido o usado)."
	huma.Register(api, conectar, func(ctx context.Context, in *conClave[EntradaConectar]) (*cuerpo[Conector], error) {
		return responder(m.Conectar.Ejecutar(ctx, in.Body))
	})

	enlace := operacion(etiquetaIntegraciones, "generarEnlaceArgos", http.MethodPost, "/api/v1/integraciones/argosisp/conexion/enlaces",
		"Generar un enlace de conexión para pegar en Argos", nucleo.PermIntegracionesConectar, http.StatusCreated, http.StatusConflict)
	enlace.Description += " Válido 15 min y de un solo uso. Argos lo canjea en `POST /api/v1/integraciones/argosisp/conexion/canjear`. La respuesta lleva un secreto que no se guarda para `Idempotency-Key`: repetir la clave responde 409 `idempotencia_no_repetible`."
	huma.Register(api, enlace, func(ctx context.Context, _ *soloClave) (*cuerpo[EnlaceGenerado], error) {
		v, err := m.GenerarEnlace.Ejecutar(ctx, struct{}{})
		if err == nil {
			nucleo.NoRepetible(ctx)
		}
		return responder(v, err)
	})

	canje := operacion(etiquetaIntegraciones, "canjearConexionArgos", http.MethodPost, "/api/v1/integraciones/argosisp/conexion/canjear",
		"Canje del enlace de conexión (lo llama Argos, servidor a servidor)", "", http.StatusOK)
	canje.Security = nil
	canje.Description = "Sin credenciales: autoriza el código del enlace (15 min, un solo uso; 401 `codigo_invalido`). Argos entrega su URL y el token `atk_…`; Iris responde la URL del webhook y el secreto HMAC. El conector queda apagado hasta que un administrador de Iris lo active."
	huma.Register(api, canje, func(ctx context.Context, in *cuerpo[EntradaCanjear]) (*cuerpo[ConexionCanjeada], error) {
		ctx = contexto.ConCanal(ctx, contexto.CanalIntegracion)
		return responder(m.CanjearConexion.Ejecutar(ctx, in.Body))
	})

	activar := operacion(etiquetaIntegraciones, "activarConector", http.MethodPost, "/api/v1/integraciones/conectores/{id}/activar",
		"Activar un conector", nucleo.PermIntegracionesActivar, http.StatusOK, http.StatusNotFound, http.StatusConflict)
	activar.Description += " A lo más uno activo por sistema (409 `otro_conector_activo`). Encola la primera carga de zonas y routers."
	huma.Register(api, activar, func(ctx context.Context, in *conID[EntradaCambioConector]) (*cuerpo[Conector], error) {
		e := in.Body
		e.ID = in.ID
		return responder(m.ActivarConector.Ejecutar(ctx, e))
	})

	huma.Register(api, operacion(etiquetaIntegraciones, "desactivarConector", http.MethodPost, "/api/v1/integraciones/conectores/{id}/desactivar",
		"Desactivar un conector", nucleo.PermIntegracionesDesactivar, http.StatusOK, http.StatusNotFound, http.StatusConflict),
		func(ctx context.Context, in *conID[EntradaCambioConector]) (*cuerpo[Conector], error) {
			e := in.Body
			e.ID = in.ID
			return responder(m.DesactivarConector.Ejecutar(ctx, e))
		})

	webhook := operacion(etiquetaIntegraciones, "webhookArgos", http.MethodPost, "/api/v1/integraciones/argosisp/webhook/{conector_id}",
		"Webhook de Argos (firmado)", "", http.StatusOK, http.StatusServiceUnavailable)
	webhook.Security = nil
	webhook.MaxBodyBytes = 1 << 20
	webhook.BodyReadTimeout = 10 * time.Second
	webhook.Description = "Firmado con HMAC-SHA256 (`X-Webhook-Signature: v1=<hex>` sobre `\"<X-Webhook-Timestamp>.<cuerpo>\"`), ventana de 5 min. 401 `firma_invalida` / `firma_vencida`. Se guarda en `integracion.entrantes` deduplicado por `X-Webhook-Id` (un repetido responde 200 con `duplicado: true`) y se procesa en un job. Con el conector apagado responde 503 `conector_apagado` con `Retry-After`."
	// El cuerpo se lee crudo (la firma es sobre los bytes exactos) y no lo valida huma antes
	// de la firma; su forma queda publicada como el esquema CuerpoWebhook.
	api.OpenAPI().Components.Schemas.Schema(reflect.TypeOf(CuerpoWebhook{}), true, "CuerpoWebhook")
	webhook.Description += " Cuerpo JSON con la forma del esquema `CuerpoWebhook`: `{ id, evento, ocurrido_en, payload }`."
	huma.Register(api, webhook, func(ctx context.Context, in *entradaWebhookHTTP) (*cuerpo[WebhookRecibido], error) {
		ctx = contexto.ConCanal(ctx, contexto.CanalWebhook)
		return responder(m.RecibirWebhook.Ejecutar(ctx, EntradaWebhook{
			ConectorID: in.ConectorID, WebhookID: in.WebhookID, Marca: in.Timestamp, Firma: in.Firma, Cuerpo: in.RawBody,
		}))
	})

	// Clientes (lectura de Argos) --------------------------------------------------------
	buscar := operacion(etiquetaClientes, "buscarClientes", http.MethodGet, "/api/v1/clientes",
		"Buscar clientes en Argos", nucleo.PermClientesVer, http.StatusOK, http.StatusBadGateway, http.StatusServiceUnavailable)
	buscar.Description += " Lectura auditada. Si Argos no responde, busca en las instantáneas locales y responde `stale: true`." + descConectorApagado
	huma.Register(api, buscar, func(ctx context.Context, in *EntradaBuscarClientes) (*cuerpo[ResultadoBusqueda], error) {
		return responder(m.BuscarClientes.Ejecutar(ctx, *in))
	})

	ip := operacion(etiquetaClientes, "resolverIP", http.MethodGet, "/api/v1/clientes/resolver-ip",
		"Cliente y servicio dueños de una IP", nucleo.PermClientesVer, http.StatusOK, http.StatusNotFound, http.StatusBadGateway, http.StatusServiceUnavailable)
	ip.Description += " Lectura auditada. Pregunta a Argos (pedido A8, con respaldo a `GET /ip/{ip}`); si no responde, usa la instantánea (`stale: true`)." + descConectorApagado
	huma.Register(api, ip, func(ctx context.Context, in *EntradaResolverIP) (*cuerpo[IPResuelta], error) {
		return responder(m.ResolverIP.Ejecutar(ctx, *in))
	})

	ficha := operacion(etiquetaClientes, "obtenerFichaCliente", http.MethodGet, "/api/v1/clientes/{id}",
		"Ficha del cliente: datos, servicios y estado de cuenta resumido", nucleo.PermClientesVer, http.StatusOK,
		http.StatusNotFound, http.StatusBadGateway, http.StatusServiceUnavailable)
	ficha.Description += " Lectura auditada. Refresca la instantánea si tiene más de 24 h; si Argos no responde, devuelve la última con `stale: true`. El estado de cuenta exige además `clientes.estado_cuenta` y se lee en vivo." + descConectorApagado
	huma.Register(api, ficha, func(ctx context.Context, in *EntradaFicha) (*cuerpo[Ficha], error) {
		return responder(m.ObtenerFicha.Ejecutar(ctx, *in))
	})
}
