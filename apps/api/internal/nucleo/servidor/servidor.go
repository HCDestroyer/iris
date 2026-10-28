// Package servidor arma el handler HTTP de la API: chi con sus middlewares (request id, log,
// pánicos, base, identidad, idempotencia), huma v2 para el OpenAPI 3.1 y los errores RFC 9457.
package servidor

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/netip"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
)

// Deps son las dependencias del servidor. BD puede ser nil solo para generar el OpenAPI.
type Deps struct {
	BD           nucleo.BD
	Log          *slog.Logger
	Version      string
	Identidad    *identidad.Modulo
	Idempotencia nucleo.OpcionesIdempotencia
	// Registradores de otros módulos (I2: soporte, campo…). Cada uno publica sus rutas.
	Modulos []func(huma.API)
	// ProxiesConfiables: redes cuyo X-Forwarded-For se cree (IRIS_PROXIES_CONFIABLES). Vacío =
	// la IP del cliente es la de la conexión.
	ProxiesConfiables []netip.Prefix
}

// Nuevo devuelve el handler y la API (para leer el OpenAPI).
func Nuevo(d Deps) (http.Handler, huma.API) {
	if d.Log == nil {
		d.Log = slog.Default()
	}
	instalarErrores(d.Log)

	r := chi.NewRouter()
	r.Use(middlewareIPReal(d.ProxiesConfiables), middlewareRequestID, middlewareLog(d.Log), middlewarePanico(d.Log))
	if d.BD != nil {
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				next.ServeHTTP(w, req.WithContext(nucleo.ConBD(req.Context(), d.BD)))
			})
		})
	}
	r.Use(identidad.Middleware(d.Log))
	idem := d.Idempotencia
	if idem.Log == nil {
		idem.Log = d.Log
	}
	r.Use(nucleo.MiddlewareIdempotencia(idem))
	r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		nucleo.EscribirProblema(w, nucleo.NoEncontrado("La ruta no existe"))
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
		nucleo.EscribirProblema(w, nucleo.NuevoProblema(http.StatusMethodNotAllowed, "metodo_no_permitido", "Método no permitido", ""))
	})

	api := humachi.New(r, configuracion(d.Version))
	registrarSistema(api, d)
	if d.Identidad != nil {
		d.Identidad.Registrar(api)
	}
	for _, registrar := range d.Modulos {
		registrar(api)
	}
	return r, api
}

func configuracion(version string) huma.Config {
	if version == "" {
		version = "0.0.0-dev"
	}
	cfg := huma.DefaultConfig("Iris API", version)
	cfg.CreateHooks = nil // sin $schema en las respuestas ni cabecera Link
	cfg.OpenAPIPath = "/api/v1/openapi"
	cfg.DocsPath = "/api/v1/docs"
	cfg.SchemasPath = "/api/v1/esquemas"
	cfg.Info.Description = "API de Iris: operación de campo y atención del ISP (familia Horus). " +
		"Errores RFC 9457 (application/problem+json), listas con cursor (`{ data, nextCursor }`) e " +
		"`Idempotency-Key` en las operaciones que cambian estado. Con sesión de panel, toda petición " +
		"que cambia estado lleva `X-CSRF-Token` (valor de la cookie `iris_csrf`)."
	cfg.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		"sesion": {Type: "apiKey", In: "cookie", Name: identidad.CookieSesion,
			Description: "Sesión del panel. Las peticiones que cambian estado llevan además X-CSRF-Token."},
		"bearer": {Type: "http", Scheme: "bearer", BearerFormat: "iris_pat_<32B base62>",
			Description: "Token de API o de integración."},
	}
	return cfg
}

// Salud ---------------------------------------------------------------------------------

type estadoSalud struct {
	Body struct {
		Estado  string `json:"estado" enum:"ok"`
		Version string `json:"version"`
	}
}

func registrarSistema(api huma.API, d Deps) {
	huma.Register(api, huma.Operation{
		OperationID: "salud", Method: http.MethodGet, Path: "/api/v1/salud",
		Summary: "El proceso responde", Tags: []string{"sistema"},
	}, func(context.Context, *struct{}) (*estadoSalud, error) {
		s := &estadoSalud{}
		s.Body.Estado, s.Body.Version = "ok", d.Version
		return s, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "listo", Method: http.MethodGet, Path: "/api/v1/listo",
		Summary: "La API puede atender: la base responde", Tags: []string{"sistema"},
		Errors: []int{http.StatusServiceUnavailable},
	}, func(ctx context.Context, _ *struct{}) (*estadoSalud, error) {
		if d.BD == nil {
			return nil, nucleo.NoDisponible("Sin base de datos configurada")
		}
		ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		var uno int
		if err := d.BD.QueryRow(ctx, "SELECT 1").Scan(&uno); err != nil {
			d.Log.WarnContext(ctx, "listo: la base no responde", "err", err)
			return nil, nucleo.NoDisponible("La base de datos no responde")
		}
		s := &estadoSalud{}
		s.Body.Estado, s.Body.Version = "ok", d.Version
		return s, nil
	})
}

// Errores -------------------------------------------------------------------------------

var instalar sync.Once

// instalarErrores hace que huma responda siempre con nucleo.Problema (RFC 9457): validación
// de entrada, errores de dominio y errores inesperados (que se registran en el log y salen
// como 500 genérico).
func instalarErrores(log *slog.Logger) {
	instalar.Do(func() {
		huma.NewError = func(status int, msg string, errs ...error) huma.StatusError {
			return problemaDeHuma(context.Background(), log, status, msg, errs)
		}
		huma.NewErrorWithContext = func(hctx huma.Context, status int, msg string, errs ...error) huma.StatusError {
			ctx := context.Background()
			if hctx != nil {
				ctx = hctx.Context()
			}
			return problemaDeHuma(ctx, log, status, msg, errs)
		}
	})
}

func problemaDeHuma(ctx context.Context, log *slog.Logger, status int, msg string, errs []error) *nucleo.Problema {
	var campos []nucleo.ErrorCampo
	var otros []error
	for _, e := range errs {
		if e == nil {
			continue
		}
		var detalle huma.ErrorDetailer
		if errors.As(e, &detalle) {
			d := detalle.ErrorDetail()
			campos = append(campos, nucleo.ErrorCampo{Field: campoDeUbicacion(d.Location), Message: d.Message})
			continue
		}
		otros = append(otros, e)
	}
	if status >= 500 && len(otros) > 0 {
		p, interno := nucleo.AProblema(otros[0])
		if interno || p.Status >= 500 {
			log.ErrorContext(ctx, "error inesperado", "err", otros[0], "request_id", contexto.RequestIDDe(ctx))
		}
		return p
	}
	switch {
	case status == 0:
		return nucleo.NuevoProblema(0, "error", "Error", "")
	case status == http.StatusUnprocessableEntity:
		return nucleo.Validacion(campos...)
	case status == http.StatusBadRequest:
		return nucleo.SolicitudInvalida(msg, campos...)
	case status >= 500:
		log.ErrorContext(ctx, "error del servidor", "msg", msg, "request_id", contexto.RequestIDDe(ctx))
		return nucleo.ErrorInterno()
	}
	return nucleo.NuevoProblema(status, codigoDeStatus(status), http.StatusText(status), msg, campos...)
}

// campoDeUbicacion: "body.roles[0].rol" → "roles[0].rol"; "query.limit" → "limit".
func campoDeUbicacion(loc string) string {
	for _, prefijo := range []string{"body.", "query.", "path.", "header.", "cookie."} {
		if strings.HasPrefix(loc, prefijo) {
			return loc[len(prefijo):]
		}
	}
	if loc == "body" {
		return ""
	}
	return loc
}

var noAlfanumerico = regexp.MustCompile(`[^a-z0-9]+`)

func codigoDeStatus(status int) string {
	return strings.Trim(noAlfanumerico.ReplaceAllString(strings.ToLower(http.StatusText(status)), "_"), "_")
}
