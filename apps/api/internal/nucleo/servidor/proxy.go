package servidor

import (
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Delegados: prefijos de ruta que la API no atiende y reenvía tal cual a otro proceso de Iris (la
// mensajería, `/api/v1/mensajeria`, contrato Iris ↔ ArgosISP 0.6.0 §6). Van antes de la identidad
// y la idempotencia: el proceso de destino las aplica con las mismas piezas (servidor.Nuevo).

// delegar envuelve el router: lo que empieza con un prefijo delegado va a su handler, con request
// id, log de acceso y pánicos; el resto, al router.
func delegar(r http.Handler, delegados map[string]http.Handler, log *slog.Logger) http.Handler {
	if len(delegados) == 0 {
		return r
	}
	envueltos := make(map[string]http.Handler, len(delegados))
	for prefijo, h := range delegados {
		envueltos[strings.TrimRight(prefijo, "/")] = middlewareRequestID(middlewareLog(log)(middlewarePanico(log)(h)))
	}
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		for prefijo, h := range envueltos {
			if req.URL.Path == prefijo || strings.HasPrefix(req.URL.Path, prefijo+"/") {
				h.ServeHTTP(w, req)
				return
			}
		}
		r.ServeHTTP(w, req)
	})
}

// ProxyHacia devuelve el handler que reenvía a destino (http://mensajeria:8081): misma ruta y
// consulta, todas las cabeceras de la petición (Authorization, Idempotency-Key, X-Iris-Empresa…),
// el request id y la IP de quien conectó agregada al final de X-Forwarded-For (el destino la
// cree solo si esta API está en sus IRIS_PROXIES_CONFIABLES). Sin destino responde 503
// servicio_no_disponible. Si el destino no responde, 502 servicio_no_disponible.
func ProxyHacia(destino string, nombre string, log *slog.Logger) (http.Handler, error) {
	noDisponible := func(w http.ResponseWriter, status int) {
		nucleo.EscribirProblema(w, nucleo.NuevoProblema(status, "servicio_no_disponible", nombre+" no disponible",
			"El servicio de "+strings.ToLower(nombre)+" de Iris no responde; inténtalo de nuevo en un momento").
			ConCabecera("Retry-After", "30"))
	}
	if strings.TrimSpace(destino) == "" {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			noDisponible(w, http.StatusServiceUnavailable)
		}), nil
	}
	u, err := url.Parse(strings.TrimRight(destino, "/"))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, &url.Error{Op: "proxy", URL: destino, Err: http.ErrNotSupported}
	}
	if log == nil {
		log = slog.Default()
	}
	transporte := http.DefaultTransport.(*http.Transport).Clone()
	transporte.Proxy = nil
	transporte.ResponseHeaderTimeout = 30 * time.Second
	p := &httputil.ReverseProxy{
		Transport: transporte,
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(u)
			pr.Out.Host = pr.In.Host
			// Rewrite quita X-Forwarded-*: se reponen las que trajo la petición (del panel) y se agrega
			// la IP que vio esta API, como un proxy más de la cadena.
			for _, h := range []string{"X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto"} {
				if v := pr.In.Header.Values(h); len(v) > 0 {
					pr.Out.Header[h] = v
				}
			}
			if ip, _, err := net.SplitHostPort(pr.In.RemoteAddr); err == nil {
				if previa := pr.Out.Header.Get("X-Forwarded-For"); previa != "" {
					pr.Out.Header.Set("X-Forwarded-For", previa+", "+ip)
				} else {
					pr.Out.Header.Set("X-Forwarded-For", ip)
				}
			}
			pr.Out.Header.Set(CabeceraRequestID, contexto.RequestIDDe(pr.In.Context()))
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			log.WarnContext(r.Context(), "proxy: el destino no responde", "servicio", nombre, "err", err,
				"request_id", contexto.RequestIDDe(r.Context()))
			noDisponible(w, http.StatusBadGateway)
		},
	}
	return p, nil
}
