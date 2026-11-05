package mensajeria

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Etiqueta del OpenAPI.
const Etiqueta = "mensajeria"

// Prefijo de las rutas del servicio (la API las reenvía a este proceso).
const Prefijo = "/api/v1/mensajeria"

// IdempotenciaPropia: POST /avisos y POST /consentimientos aplican la Idempotency-Key en el caso de
// uso (una repetición devuelve el estado actual), no en el middleware.
func IdempotenciaPropia(r *http.Request) bool {
	return r.Method == http.MethodPost && (r.URL.Path == Prefijo+"/avisos" || r.URL.Path == Prefijo+"/consentimientos")
}

type entradaAvisoHTTP struct {
	nucleo.ConIdempotencia
	Body EntradaAviso
}

type salidaAviso struct {
	Repetido string `header:"Idempotent-Replayed" doc:"true si la clave ya se había usado (el cuerpo trae el estado actual)"`
	Body     AvisoCreado
}

type entradaConsentimientoHTTP struct {
	nucleo.ConIdempotencia
	Body EntradaConsentimiento
}

type salidaConsentimiento struct {
	Status int
	Body   Consentimiento
}

type cuerpo[T any] struct {
	Body T
}

type entradaActualizarHTTP struct {
	nucleo.ConIdempotencia
	ID   int64 `path:"id" minimum:"1"`
	Body EntradaActualizarCanal
}

type entradaCrearHTTP struct {
	nucleo.ConIdempotencia
	Body EntradaCrearCanal
}

type entradaCambioHTTP struct {
	nucleo.ConIdempotencia
	ID int64 `path:"id" minimum:"1"`
}

type entradaMetaHTTP struct {
	ID      int64  `path:"id" minimum:"1"`
	Firma   string `header:"X-Hub-Signature-256" doc:"sha256=<hex(HMAC-SHA256(app_secret, cuerpo))>"`
	RawBody []byte
}

type entradaVerificarHTTP struct {
	ID        int64  `path:"id" minimum:"1"`
	Modo      string `query:"hub.mode"`
	Token     string `query:"hub.verify_token"`
	Challenge string `query:"hub.challenge" maxLength:"256"`
}

type salidaTexto struct {
	ContentType string `header:"Content-Type"`
	Body        []byte
}

func responder[T any](v T, err error) (*cuerpo[T], error) {
	if err != nil {
		return nil, err
	}
	return &cuerpo[T]{Body: v}, nil
}

func op(id, metodo, ruta, resumen string, permiso nucleo.Permiso, status int, errores ...int) huma.Operation {
	return conector.Operacion(Etiqueta, id, metodo, Prefijo+ruta, resumen, permiso, status, errores...)
}

// Registrar publica las rutas (en el proceso de mensajería y en el OpenAPI de la API).
func (m *Modulo) Registrar(api huma.API) {
	enviar := op("enviarAviso", http.MethodPost, "/avisos", "Pedir un aviso (contrato Iris ↔ ArgosISP §6.2)", nucleo.PermMensajeriaEnviar,
		http.StatusAccepted, http.StatusBadRequest, http.StatusServiceUnavailable)
	enviar.Description += " Idempotency-Key obligatoria (`argos:aviso:<plantilla>:<envio_id>`; 400 `idempotency_key_requerida`): repetirla con el mismo cuerpo " +
		"devuelve el mismo `aviso_id` con su **estado actual** (`Idempotent-Replayed: true`); con otro cuerpo, 422 `idempotencia_reutilizada`. " +
		"Sin teléfono en el cuerpo: Iris toma el contacto de su instantánea de Argos (refrescada si tiene más de 24 h; sin instantánea y con Argos caído, 503). " +
		"`variables` solo acepta las de la plantilla (422 `variable_desconocida`; falta o formato: 422 `validacion`). 422 `cliente_fuera_de_empresa` / `servicio_fuera_de_empresa`. " +
		"Responde 202 `{ aviso_id, estado: en_cola | bloqueado, motivo? }`; un bloqueado (`sin_permiso`, `baja`, `limite_diario`, `sin_contacto`, `canal_no_disponible`) no se reintenta. " +
		"En horario de silencio (21:00–07:00 Guatemala) queda en cola y sale al terminar; si para entonces venció, `aviso.fallido` con `vencido`. " +
		"Cada aviso en cola termina en un solo webhook: `aviso.entregado` o `aviso.fallido` (§6.6)."
	huma.Register(api, enviar, func(ctx context.Context, in *entradaAvisoHTTP) (*salidaAviso, error) {
		in.Body.IdempotencyKey = in.IdempotencyKey
		r, err := m.EnviarAviso.Ejecutar(ctx, in.Body)
		if err != nil {
			return nil, err
		}
		s := &salidaAviso{Body: r}
		if r.Repetido {
			s.Repetido = "true"
		}
		return s, nil
	})

	ver := op("verAviso", http.MethodGet, "/avisos/{id}", "Estado de un aviso", nucleo.PermMensajeriaEnviar, http.StatusOK, http.StatusNotFound)
	ver.Description += " Para conciliar un webhook perdido. Una integración solo ve los avisos que pidió: cualquier otro, 404."
	huma.Register(api, ver, func(ctx context.Context, in *EntradaVerAviso) (*cuerpo[AvisoDetalle], error) {
		return responder(m.VerAviso.Ejecutar(ctx, *in))
	})

	cons := op("registrarConsentimiento", http.MethodPost, "/consentimientos", "Registrar un permiso de contacto (§6.4)",
		nucleo.PermMensajeriaRegistrarConsentimiento, http.StatusCreated, http.StatusBadRequest)
	cons.Description += " Idempotency-Key obligatoria (`argos:consentimiento:<empresa_id>:<cliente_id>:<canal>:<ocurrido_en>`). 201 si es nuevo, 200 si es una repetición; " +
		"los dos con `{ consentimiento_id, estado_vigente }`. Manda el registro con `ocurrido_en` más reciente: un `acepta` más viejo se guarda pero no gana a una `baja` más nueva. " +
		"422 `cliente_fuera_de_empresa` si el cliente no es de una empresa de la integración (según la instantánea de Argos)."
	cons.Responses = map[string]*huma.Response{"200": {Description: "Repetición: el mismo registro y el estado vigente"}}
	huma.Register(api, cons, func(ctx context.Context, in *entradaConsentimientoHTTP) (*salidaConsentimiento, error) {
		in.Body.IdempotencyKey = in.IdempotencyKey
		r, err := m.RegistrarConsentimiento.Ejecutar(ctx, in.Body)
		if err != nil {
			return nil, err
		}
		status := http.StatusOK
		if r.Nuevo {
			status = http.StatusCreated
		}
		return &salidaConsentimiento{Status: status, Body: r.Consentimiento}, nil
	})

	plant := op("listarPlantillasMensajeria", http.MethodGet, "/plantillas", "Plantillas vigentes (§6.3)", nucleo.PermMensajeriaVer, http.StatusOK)
	plant.Description += " Con sus variables y los medios en que están aprobadas para las empresas de quien consulta. Una no aprobada responde `bloqueado` / `canal_no_disponible` al pedir un aviso."
	huma.Register(api, plant, func(ctx context.Context, in *EntradaPlantillas) (*cuerpo[ListaPlantillas], error) {
		return responder(m.ListarPlantillas.Ejecutar(ctx, *in))
	})

	huma.Register(api, op("listarCanalesMensajeria", http.MethodGet, "/canales", "Canales de mensajería configurados (sin credenciales)",
		nucleo.PermMensajeriaConfigurar, http.StatusOK), func(ctx context.Context, _ *struct{}) (*cuerpo[ListaCanales], error) {
		return responder(m.ListarCanales.Ejecutar(ctx, struct{}{}))
	})
	crear := op("crearCanalMensajeria", http.MethodPost, "/canales", "Configurar WhatsApp Cloud API de una empresa", nucleo.PermMensajeriaConfigurar,
		http.StatusCreated, http.StatusConflict)
	crear.Description += " Token, app secret y verify token se guardan cifrados (IRIS_CLAVE_CIFRADO) y nunca se devuelven. Uno activo por empresa (409 `canal_existente`). " +
		"`webhookRuta` es la ruta que se configura en la app de Meta (verificación con el verify token y firma con el app secret)."
	huma.Register(api, crear, func(ctx context.Context, in *entradaCrearHTTP) (*cuerpo[Canal], error) {
		return responder(m.CrearCanal.Ejecutar(ctx, in.Body))
	})
	act := op("actualizarCanalMensajeria", http.MethodPost, "/canales/{id}/actualizar", "Actualizar un canal (credenciales vacías: no cambian)",
		nucleo.PermMensajeriaConfigurar, http.StatusOK, http.StatusNotFound, http.StatusConflict)
	huma.Register(api, act, func(ctx context.Context, in *entradaActualizarHTTP) (*cuerpo[Canal], error) {
		in.Body.ID = in.ID
		return responder(m.ActualizarCanal.Ejecutar(ctx, in.Body))
	})
	huma.Register(api, op("activarCanalMensajeria", http.MethodPost, "/canales/{id}/activar", "Activar un canal", nucleo.PermMensajeriaConfigurar,
		http.StatusOK, http.StatusNotFound, http.StatusConflict), func(ctx context.Context, in *entradaCambioHTTP) (*cuerpo[Canal], error) {
		return responder(m.ActivarCanal.Ejecutar(ctx, EntradaCambioCanal{ID: in.ID}))
	})
	huma.Register(api, op("desactivarCanalMensajeria", http.MethodPost, "/canales/{id}/desactivar", "Desactivar un canal", nucleo.PermMensajeriaConfigurar,
		http.StatusOK, http.StatusNotFound), func(ctx context.Context, in *entradaCambioHTTP) (*cuerpo[Canal], error) {
		return responder(m.DesactivarCanal.Ejecutar(ctx, EntradaCambioCanal{ID: in.ID}))
	})

	webhook := op("webhookMeta", http.MethodPost, "/canales/{id}/webhook", "Webhook de WhatsApp Cloud API (Meta)", "", http.StatusOK, http.StatusRequestEntityTooLarge)
	webhook.Security = nil
	webhook.MaxBodyBytes = MaxCuerpoMeta
	webhook.BodyReadTimeout = 10 * time.Second
	webhook.Description = "Estados de entrega (`sent`, `delivered`, `read`, `failed`), mensajes del cliente (`BAJA` o `ALTO` dan de baja sus avisos) y calidad del número. " +
		"Firmado con el app secret del canal (`X-Hub-Signature-256: sha256=<hex>`; 401 `firma_invalida`, sin auditar). Se guarda deduplicado y se procesa en un job."
	huma.Register(api, webhook, func(ctx context.Context, in *entradaMetaHTTP) (*cuerpo[MetaRecibido], error) {
		ctx = contexto.ConCanal(ctx, contexto.CanalWebhook)
		return responder(m.RecibirMeta.Ejecutar(ctx, EntradaMeta{CanalID: in.ID, Firma: in.Firma, Cuerpo: in.RawBody}))
	})
	verif := op("verificarWebhookMeta", http.MethodGet, "/canales/{id}/webhook", "Verificación de la suscripción del webhook de Meta", "",
		http.StatusOK, http.StatusForbidden)
	verif.Security = nil
	verif.Description = "Meta llama con `hub.mode=subscribe`, `hub.verify_token` (el del canal) y `hub.challenge`; responde el challenge en texto plano. 403 `verificacion_invalida`."
	huma.Register(api, verif, func(ctx context.Context, in *entradaVerificarHTTP) (*salidaTexto, error) {
		ctx = contexto.ConCanal(ctx, contexto.CanalWebhook)
		c, err := m.VerificarMeta.Ejecutar(ctx, EntradaVerificarMeta{CanalID: in.ID, Modo: in.Modo, Token: in.Token, Challenge: in.Challenge})
		if err != nil {
			return nil, err
		}
		return &salidaTexto{ContentType: "text/plain; charset=utf-8", Body: []byte(c)}, nil
	})
}
