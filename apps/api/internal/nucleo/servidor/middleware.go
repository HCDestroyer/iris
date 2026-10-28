package servidor

import (
	"errors"
	"log/slog"
	"net"
	"net/http"
	"regexp"
	"runtime/debug"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// CabeceraRequestID viaja de entrada (si el cliente la manda) y de salida.
const CabeceraRequestID = "X-Request-Id"

var requestIDValido = regexp.MustCompile(`^[A-Za-z0-9._:-]{8,128}$`)

func middlewareRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(CabeceraRequestID)
		if !requestIDValido.MatchString(id) {
			id = contexto.NuevoRequestID()
		}
		w.Header().Set(CabeceraRequestID, id)
		ctx := contexto.ConRequestID(r.Context(), id)
		ctx = contexto.ConCanal(ctx, contexto.CanalAPI)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

type registro struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *registro) WriteHeader(s int) {
	if r.status == 0 {
		r.status = s
	}
	r.ResponseWriter.WriteHeader(s)
}

func (r *registro) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

func (r *registro) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// middlewareLog escribe una línea JSON por petición. El actor se toma del contexto final
// (lo deja el middleware de identidad en la petición interna), por eso se registra el request id.
func middlewareLog(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			inicio := time.Now()
			reg := &registro{ResponseWriter: w}
			next.ServeHTTP(reg, r)
			nivel := slog.LevelInfo
			if reg.status >= 500 {
				nivel = slog.LevelError
			}
			log.LogAttrs(r.Context(), nivel, "http",
				slog.String("metodo", r.Method),
				slog.String("ruta", r.URL.Path),
				slog.String("ip", ipDe(r.RemoteAddr)),
				slog.Int("status", reg.status),
				slog.Int("bytes", reg.bytes),
				slog.Duration("duracion", time.Since(inicio)),
				slog.String("request_id", contexto.RequestIDDe(r.Context())),
			)
		})
	}
}

func ipDe(remoteAddr string) string {
	if h, _, err := net.SplitHostPort(remoteAddr); err == nil {
		return h
	}
	return remoteAddr
}

func middlewarePanico(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					if err, ok := rec.(error); ok && errors.Is(err, http.ErrAbortHandler) {
						panic(rec)
					}
					log.ErrorContext(r.Context(), "pánico en la petición", "panico", rec,
						"request_id", contexto.RequestIDDe(r.Context()), "pila", string(debug.Stack()))
					nucleo.EscribirProblema(w, nucleo.ErrorInterno())
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
