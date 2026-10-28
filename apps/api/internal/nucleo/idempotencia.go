package nucleo

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	nucleobd "github.com/HCDestroyer/iris/apps/api/internal/nucleo/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// CabeceraIdempotencia es el nombre de la cabecera (CLAUDE.md §6).
const CabeceraIdempotencia = "Idempotency-Key"

// CabeceraRepetida marca una respuesta servida desde nucleo.idempotencia.
const CabeceraRepetida = "Idempotent-Replayed"

// ConIdempotencia se embebe en la entrada de las operaciones que cambian estado para que la
// cabecera quede documentada en el OpenAPI. La aplica MiddlewareIdempotencia.
type ConIdempotencia struct {
	IdempotencyKey string `header:"Idempotency-Key" maxLength:"255" doc:"Clave de idempotencia: con la misma clave y el mismo cuerpo se devuelve la misma respuesta; con otro cuerpo, 422"`
}

// OpcionesIdempotencia configura el middleware.
type OpcionesIdempotencia struct {
	// Vigencia de una clave (24 h por defecto).
	Vigencia time.Duration
	// Tamaño máximo del cuerpo que se acepta con clave (1 MiB por defecto).
	MaxCuerpo int64
	Log       *slog.Logger
}

// MiddlewareIdempotencia aplica Idempotency-Key a POST, PUT, PATCH y DELETE:
//
//   - misma clave, mismo método, ruta y cuerpo → la misma respuesta guardada (status y cuerpo),
//     con la cabecera Idempotent-Replayed: true;
//   - misma clave con otra petición → 422 idempotencia_reutilizada;
//   - misma clave mientras la primera sigue en curso → 409 idempotencia_en_proceso;
//   - si la primera respondió 5xx, la clave se libera y se puede reintentar.
//
// Las claves son por actor (dos usuarios pueden usar la misma). Las peticiones anónimas no se
// guardan (la respuesta podría servirse a otro). Va después de la autenticación y de ConBD.
func MiddlewareIdempotencia(op OpcionesIdempotencia) func(http.Handler) http.Handler {
	if op.Vigencia <= 0 {
		op.Vigencia = 24 * time.Hour
	}
	if op.MaxCuerpo <= 0 {
		op.MaxCuerpo = 1 << 20
	}
	if op.Log == nil {
		op.Log = slog.Default()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			clave := r.Header.Get(CabeceraIdempotencia)
			if clave == "" || !metodoConEstado(r.Method) {
				next.ServeHTTP(w, r)
				return
			}
			ctx := r.Context()
			actor := contexto.ActorDe(ctx)
			if actor.Tipo == contexto.ActorAnonimo {
				next.ServeHTTP(w, r)
				return
			}
			if len(clave) > 255 {
				EscribirProblema(w, Validacion(ErrorCampo{Field: CabeceraIdempotencia, Message: "Máximo 255 caracteres", Rule: "maxLength"}))
				return
			}
			bd := BDDe(ctx)
			if bd == nil {
				EscribirProblema(w, ErrorInterno())
				return
			}

			cuerpo, err := io.ReadAll(io.LimitReader(r.Body, op.MaxCuerpo+1))
			if err != nil {
				EscribirProblema(w, SolicitudInvalida("No se pudo leer el cuerpo"))
				return
			}
			if int64(len(cuerpo)) > op.MaxCuerpo {
				EscribirProblema(w, NuevoProblema(http.StatusRequestEntityTooLarge, "cuerpo_muy_grande",
					"Cuerpo demasiado grande", "El cuerpo excede el máximo admitido con Idempotency-Key"))
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(cuerpo))

			ruta := r.URL.RequestURI()
			h := sha256.New()
			_, _ = fmt.Fprintf(h, "%s\n%s\n", r.Method, ruta)
			h.Write(cuerpo)
			hash := h.Sum(nil)

			q := nucleobd.New(bd)
			claveActor := fmt.Sprintf("%s:%d", actor.Tipo, actor.ID)
			bdCtx := context.WithoutCancel(ctx)

			if _, err := q.BorrarIdempotenciaExpirada(bdCtx, nucleobd.BorrarIdempotenciaExpiradaParams{Actor: claveActor, Clave: clave}); err != nil {
				op.Log.ErrorContext(ctx, "idempotencia: limpiar clave expirada", "err", err)
			}
			filas, err := q.ReservarIdempotencia(bdCtx, nucleobd.ReservarIdempotenciaParams{
				Actor: claveActor, Clave: clave, Metodo: r.Method, Ruta: ruta,
				HashPeticion: hash, ExpiraEn: time.Now().Add(op.Vigencia),
			})
			if err != nil {
				op.Log.ErrorContext(ctx, "idempotencia: reservar clave", "err", err)
				EscribirProblema(w, ErrorInterno())
				return
			}

			if filas == 0 {
				previa, err := q.ObtenerIdempotencia(bdCtx, nucleobd.ObtenerIdempotenciaParams{Actor: claveActor, Clave: clave})
				if err != nil {
					op.Log.ErrorContext(ctx, "idempotencia: leer clave", "err", err)
					EscribirProblema(w, ErrorInterno())
					return
				}
				switch {
				case !bytes.Equal(previa.HashPeticion, hash):
					EscribirProblema(w, NuevoProblema(http.StatusUnprocessableEntity, "idempotencia_reutilizada",
						"Clave de idempotencia reutilizada",
						"La clave ya se usó con otra petición (método, ruta o cuerpo distintos)"))
				case previa.Estado != "completa":
					EscribirProblema(w, Conflicto("idempotencia_en_proceso",
						"Una petición con esta clave sigue en curso; vuelve a intentar en unos segundos"))
				default:
					if previa.ContentType != nil && *previa.ContentType != "" {
						w.Header().Set("Content-Type", *previa.ContentType)
					}
					w.Header().Set(CabeceraRepetida, "true")
					status := http.StatusOK
					if previa.Status != nil {
						status = int(*previa.Status)
					}
					w.WriteHeader(status)
					_, _ = w.Write(previa.Respuesta)
				}
				return
			}

			grabadora := &grabadora{ResponseWriter: w, status: http.StatusOK}
			defer func() {
				// Ante un pánico la clave se libera; el pánico sigue su camino.
				if rec := recover(); rec != nil {
					_ = q.LiberarIdempotencia(bdCtx, nucleobd.LiberarIdempotenciaParams{Actor: claveActor, Clave: clave})
					panic(rec)
				}
			}()
			next.ServeHTTP(grabadora, r)

			if grabadora.status >= 500 {
				if err := q.LiberarIdempotencia(bdCtx, nucleobd.LiberarIdempotenciaParams{Actor: claveActor, Clave: clave}); err != nil {
					op.Log.ErrorContext(ctx, "idempotencia: liberar clave", "err", err)
				}
				return
			}
			status := int32(grabadora.status) //nolint:gosec // códigos HTTP caben en int32
			tipo := grabadora.Header().Get("Content-Type")
			if err := q.CompletarIdempotencia(bdCtx, nucleobd.CompletarIdempotenciaParams{
				Actor: claveActor, Clave: clave, Status: &status, ContentType: &tipo, Respuesta: grabadora.cuerpo.Bytes(),
			}); err != nil {
				op.Log.ErrorContext(ctx, "idempotencia: guardar respuesta", "err", err)
			}
		})
	}
}

func metodoConEstado(m string) bool {
	return m == http.MethodPost || m == http.MethodPut || m == http.MethodPatch || m == http.MethodDelete
}

// grabadora copia status y cuerpo de la respuesta mientras se escribe.
type grabadora struct {
	http.ResponseWriter
	status   int
	escribio bool
	cuerpo   bytes.Buffer
}

func (g *grabadora) WriteHeader(status int) {
	if !g.escribio {
		g.status = status
		g.escribio = true
	}
	g.ResponseWriter.WriteHeader(status)
}

func (g *grabadora) Write(b []byte) (int, error) {
	if !g.escribio {
		g.escribio = true
	}
	g.cuerpo.Write(b)
	return g.ResponseWriter.Write(b)
}

// Unwrap permite a http.ResponseController llegar al writer original.
func (g *grabadora) Unwrap() http.ResponseWriter { return g.ResponseWriter }
