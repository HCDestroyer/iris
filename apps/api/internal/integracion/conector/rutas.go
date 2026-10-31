package conector

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

// EtiquetaIntegraciones agrupa las rutas de conectores en el OpenAPI.
const EtiquetaIntegraciones = "integraciones"

// Operacion arma una operación de huma con la seguridad, los errores comunes y x-permiso.
func Operacion(etiqueta, id, metodo, ruta, resumen string, permiso nucleo.Permiso, status int, errores ...int) huma.Operation {
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

// Cuerpo es la respuesta con cuerpo JSON.
type Cuerpo[T any] struct {
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

type vacio struct{}

type entradaWebhookHTTP struct {
	ConectorID int64  `path:"conector_id" minimum:"1"`
	WebhookID  string `header:"X-Webhook-Id" doc:"Id estable del webhook (deduplicación)"`
	Timestamp  string `header:"X-Webhook-Timestamp" doc:"Segundos Unix del envío"`
	Firma      string `header:"X-Webhook-Signature" doc:"v1=<hex(HMAC-SHA256(secreto, \"<timestamp>.<cuerpo>\"))>"`
	RawBody    []byte
}

// Responder arma la respuesta de un caso de uso.
func Responder[T any](v T, err error) (*Cuerpo[T], error) {
	if err != nil {
		return nil, err
	}
	return &Cuerpo[T]{Body: v}, nil
}

// Registrar publica las rutas comunes (por id de conector, cualquier sistema): listar,
// activar, desactivar, URL pública y enlaces «Ver en …».
func (n *Nucleo) Registrar(api huma.API) {
	huma.Register(api, Operacion(EtiquetaIntegraciones, "listarConectores", http.MethodGet, "/api/v1/integraciones/conectores",
		"Listar conectores (sin token ni secreto)", nucleo.PermIntegracionesVer, http.StatusOK),
		func(ctx context.Context, in *nucleo.ParamsPagina) (*Cuerpo[nucleo.Pagina[Conector]], error) {
			return Responder(n.ListarConectores.Ejecutar(ctx, *in))
		})

	activar := Operacion(EtiquetaIntegraciones, "activarConector", http.MethodPost, "/api/v1/integraciones/conectores/{id}/activar",
		"Activar un conector", nucleo.PermIntegracionesActivar, http.StatusOK, http.StatusNotFound, http.StatusConflict)
	activar.Description += " A lo más uno activo por sistema: activar uno reemplaza al activo del mismo sistema (queda apagado y su token se revoca en la misma transacción, así la integración no se corta al reconectar). Con Argos, encola la primera carga de zonas y routers."
	huma.Register(api, activar, func(ctx context.Context, in *conID[EntradaCambioConector]) (*Cuerpo[Conector], error) {
		e := in.Body
		e.ID = in.ID
		return Responder(n.ActivarConector.Ejecutar(ctx, e))
	})

	huma.Register(api, Operacion(EtiquetaIntegraciones, "desactivarConector", http.MethodPost, "/api/v1/integraciones/conectores/{id}/desactivar",
		"Desactivar un conector", nucleo.PermIntegracionesDesactivar, http.StatusOK, http.StatusNotFound, http.StatusConflict),
		func(ctx context.Context, in *conID[EntradaCambioConector]) (*Cuerpo[Conector], error) {
			e := in.Body
			e.ID = in.ID
			return Responder(n.DesactivarConector.Ejecutar(ctx, e))
		})

	url := Operacion(EtiquetaIntegraciones, "configurarURLPublicaConector", http.MethodPost, "/api/v1/integraciones/conectores/{id}/url-publica",
		"Configurar la URL pública de un conector", nucleo.PermIntegracionesConectar, http.StatusOK, http.StatusNotFound, http.StatusConflict)
	url.Description += " Origen de la interfaz del otro sistema para los enlaces «Ver en Argos/Horus» (null = la URL base del conector). Emite `conector.actualizado`."
	huma.Register(api, url, func(ctx context.Context, in *conID[EntradaURLPublica]) (*Cuerpo[Conector], error) {
		e := in.Body
		e.ID = in.ID
		return Responder(n.CambiarURLPublica.Ejecutar(ctx, e))
	})

	enlaces := Operacion(EtiquetaIntegraciones, "obtenerEnlacesExternos", http.MethodGet, "/api/v1/integraciones/enlaces",
		"Orígenes de Argos y Horus para los enlaces «Ver en …»", nucleo.PermClientesVer, http.StatusOK)
	enlaces.Description += " No exige empresa activa. Sin secretos: solo la URL pública de cada sistema con conector activo (ausente si no hay)."
	huma.Register(api, enlaces, func(ctx context.Context, _ *vacio) (*Cuerpo[EnlacesExternos], error) {
		return Responder(n.ObtenerEnlaces.Ejecutar(ctx, struct{}{}))
	})
}

// Registrar publica las rutas de conexión y webhook del sistema bajo
// /api/v1/integraciones/<sistema>/.
func (c *Conexion) Registrar(api huma.API) {
	s := c.s
	prefijo := "/api/v1/integraciones/" + s.Nombre
	nombre := s.Remoto.Nombre

	conectar := Operacion(EtiquetaIntegraciones, "conectar"+s.Etiqueta, http.MethodPost, prefijo+"/conectar",
		"Conectar con "+nombre+" usando un enlace generado en "+nombre, nucleo.PermIntegracionesConectar, http.StatusCreated,
		http.StatusBadGateway, http.StatusServiceUnavailable)
	conectar.Description += " Iris canjea el código en " + nombre + " (servidor a servidor), guarda el token `" + s.PrefijoToken +
		"…` y el secreto del webhook cifrados y deja el conector **apagado** hasta que se active. Entrega a " + nombre +
		" un `iris_pat_` del usuario de integración que vence en 15 min si el conector no se activa. Solo para orígenes de `" + s.Remoto.VariableOrigenes +
		"` o del conector activo de este sistema (422 `origenes_no_configurados` / `origen_no_permitido`; ver `conexion/previsualizar`) y exige `empresas.todas` sin límite (403). 502 `" + s.Remoto.Codigo +
		"_rechazo` si " + nombre + " rechaza el código (vencido o usado)."
	huma.Register(api, conectar, func(ctx context.Context, in *conClave[EntradaConectar]) (*Cuerpo[Conector], error) {
		return Responder(c.Conectar.Ejecutar(ctx, in.Body))
	})

	previa := Operacion(EtiquetaIntegraciones, "previsualizarConexion"+s.Etiqueta, http.MethodPost, prefijo+"/conexion/previsualizar",
		"Ver a qué origen irá el token antes de conectar con "+nombre, nucleo.PermIntegracionesConectar, http.StatusOK)
	previa.Description += " No llama a " + nombre + " ni canjea el código: devuelve el origen (esquema://host[:puerto]) del enlace y si está permitido (`" +
		s.Remoto.VariableOrigenes + "` o el del conector activo). El panel lo muestra para que el administrador confirme antes de `conectar`."
	huma.Register(api, previa, func(ctx context.Context, in *Cuerpo[EntradaConectar]) (*Cuerpo[Previsualizacion], error) {
		return Responder(c.Previsualizar.Ejecutar(ctx, in.Body))
	})

	enlace := Operacion(EtiquetaIntegraciones, "generarEnlace"+s.Etiqueta, http.MethodPost, prefijo+"/conexion/enlaces",
		"Generar un enlace de conexión para pegar en "+nombre, nucleo.PermIntegracionesConectar, http.StatusCreated, http.StatusConflict)
	enlace.Description += " Válido 15 min y de un solo uso. " + nombre + " lo canjea en `POST " + prefijo +
		"/conexion/canjear`. La respuesta lleva un secreto que no se guarda para `Idempotency-Key`: repetir la clave responde 409 `idempotencia_no_repetible`."
	huma.Register(api, enlace, func(ctx context.Context, _ *soloClave) (*Cuerpo[EnlaceGenerado], error) {
		v, err := c.GenerarEnlace.Ejecutar(ctx, struct{}{})
		if err == nil {
			nucleo.NoRepetible(ctx)
		}
		return Responder(v, err)
	})

	if c.Autorizar != nil {
		c.registrarAutorizacion(api, prefijo)
	}

	canje := Operacion(EtiquetaIntegraciones, "canjearConexion"+s.Etiqueta, http.MethodPost, prefijo+"/conexion/canjear",
		"Canje del enlace de conexión (lo llama "+nombre+", servidor a servidor)", "", http.StatusOK)
	canje.Security = nil
	canje.Description = "Sin credenciales: autoriza el código del enlace (15 min, un solo uso; 401 `codigo_invalido`). " + nombre +
		" entrega su URL y su token `" + s.PrefijoToken + "…` (`sistema: \"" + s.Nombre + "\"`); Iris responde la URL del webhook y el secreto HMAC. El conector queda apagado hasta que un administrador de Iris lo active."
	huma.Register(api, canje, func(ctx context.Context, in *Cuerpo[EntradaCanjear]) (*Cuerpo[ConexionCanjeada], error) {
		ctx = contexto.ConCanal(ctx, contexto.CanalIntegracion)
		return Responder(c.CanjearConexion.Ejecutar(ctx, in.Body))
	})

	webhook := Operacion(EtiquetaIntegraciones, "webhook"+s.Etiqueta, http.MethodPost, prefijo+"/webhook/{conector_id}",
		"Webhook de "+nombre+" (firmado)", "", http.StatusOK, http.StatusRequestEntityTooLarge, http.StatusServiceUnavailable)
	webhook.Security = nil
	webhook.MaxBodyBytes = MaxCuerpoWebhook
	webhook.BodyReadTimeout = 10 * time.Second
	webhook.Description = "Firmado con HMAC-SHA256 (`X-Webhook-Signature: v1=<hex>` sobre `\"<X-Webhook-Timestamp>.<cuerpo>\"`), ventana de 5 min. 401 `firma_invalida` / `firma_vencida`. Se guarda en `integracion.entrantes` deduplicado por el `id` del cuerpo (firmado, igual a `X-Webhook-Id`; un repetido responde 200 con `duplicado: true`) y se procesa en un job. Con el conector apagado responde 503 `conector_apagado` con `Retry-After`. Máximo 1 MiB."
	// El cuerpo se lee crudo (la firma es sobre los bytes exactos) y no lo valida huma antes
	// de la firma; su forma queda publicada como el esquema CuerpoWebhook.
	api.OpenAPI().Components.Schemas.Schema(reflect.TypeOf(CuerpoWebhook{}), true, "CuerpoWebhook")
	webhook.Description += " Cuerpo JSON con la forma del esquema `CuerpoWebhook`: `{ id, evento, ocurrido_en, payload }`."
	huma.Register(api, webhook, func(ctx context.Context, in *entradaWebhookHTTP) (*Cuerpo[WebhookRecibido], error) {
		ctx = contexto.ConCanal(ctx, contexto.CanalWebhook)
		return Responder(c.RecibirWebhook.Ejecutar(ctx, EntradaWebhook{
			ConectorID: in.ConectorID, WebhookID: in.WebhookID, Marca: in.Timestamp, Firma: in.Firma, Cuerpo: in.RawBody,
		}))
	})
}

// registrarAutorizacion publica «Conectar con un botón» (contrato Iris ↔ ArgosISP 0.4.0 §1.4).
func (c *Conexion) registrarAutorizacion(api huma.API, prefijo string) {
	s := c.s
	nombre := s.Remoto.Nombre

	iniciar := Operacion(EtiquetaIntegraciones, "autorizarConexion"+s.Etiqueta, http.MethodPost, prefijo+"/autorizaciones",
		"Iniciar «Conectar con "+nombre+"» (autorización con state y PKCE)", nucleo.PermIntegracionesConectar, http.StatusCreated)
	iniciar.Description += " Solo con la sesión del panel (422 `sesion_requerida` con un token). Normaliza `panelUrl` a `https://host[:puerto]`, genera un `state` " +
		"(32 bytes, un uso, 10 min, ligado a la sesión y al usuario) y un `code_verifier` (32 bytes, cifrado en el servidor; nunca sale) y devuelve la URL de autorizar del panel de " +
		nombre + " con el `code_challenge` S256. Exige `empresas.todas` sin límite (403) y `" + s.Remoto.VariableOrigenes + "` (422 `origenes_no_configurados`)."
	iniciar.Description += " La URL lleva el `state`, que no se guarda para `Idempotency-Key`: repetir la clave responde 409 `idempotencia_no_repetible` sin iniciar otra autorización."
	huma.Register(api, iniciar, func(ctx context.Context, in *conClave[EntradaAutorizar]) (*Cuerpo[AutorizacionIniciada], error) {
		v, err := c.Autorizar.Ejecutar(ctx, in.Body)
		if err == nil {
			nucleo.NoRepetible(ctx)
		}
		return Responder(v, err)
	})

	completar := Operacion(EtiquetaIntegraciones, "completarAutorizacion"+s.Etiqueta, http.MethodPost, prefijo+"/autorizaciones/completar",
		"Completar «Conectar con "+nombre+"» con lo que trae la vuelta", nucleo.PermIntegracionesConectar, http.StatusCreated,
		http.StatusBadGateway, http.StatusServiceUnavailable)
	completar.Description += " Recibe `{ codigo, api, state }` o `{ error, state }` del fragmento de `/administracion/integraciones/argos/vuelta`. " +
		"Verifica el `state` (422 `state_invalido` si no existe, ya se usó o es de otra sesión; 422 `state_vencido`) y lo gasta; con `error`, 422 `rechazado`. " +
		"`api` debe estar en `" + s.Remoto.VariableOrigenes + "` (422 `origen_no_permitido`). Canjea servidor a servidor con el `code_verifier` (422 `codigo_invalido` si " +
		nombre + " no lo acepta), guarda `panelUrl` y `apiUrl` por separado y deja el conector **apagado** hasta «Activar». " +
		"Con la misma `Idempotency-Key`, un doble envío devuelve la misma respuesta."
	huma.Register(api, completar, func(ctx context.Context, in *conClave[EntradaCompletarAutorizacion]) (*Cuerpo[AutorizacionCompletada], error) {
		return Responder(c.CompletarAutorizacion.Ejecutar(ctx, in.Body))
	})
}
