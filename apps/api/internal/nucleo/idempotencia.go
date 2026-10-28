package nucleo

import (
	"bytes"
	"context"
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
	// Reclamo: una clave que sigue en_proceso después de este tiempo se puede retomar (el
	// proceso cayó entre el COMMIT y guardar la respuesta). 2 min por defecto: más que el
	// WriteTimeout del servidor (60 s), para no retomar una petición que sigue viva.
	Reclamo time.Duration
	// Claves HMAC del hash de la petición (ClavesIdempotencia): con la primera se escribe; las
	// demás (la anterior durante una rotación) solo se usan para comparar. Vacía = una clave
	// aleatoria del proceso (desarrollo sin IRIS_CLAVE_CIFRADO).
	Claves []ClaveIdempotencia
	Log    *slog.Logger
}

type claveNoRepetible int

// NoRepetible marca la respuesta en curso como no repetible: lleva un secreto (el valor de un
// token, un secreto TOTP) que no debe quedar guardado en nucleo.idempotencia. La clave se
// consume igual, pero sin cuerpo: repetirla responde 409 idempotencia_no_repetible. La llama el
// handler cuando la operación terminó bien; sin Idempotency-Key no hace nada.
func NoRepetible(ctx context.Context) {
	if marca, ok := ctx.Value(claveNoRepetible(0)).(*bool); ok {
		*marca = true
	}
}

// claveDeActor separa las claves por credencial: cada token tiene las suyas (dos tokens del
// mismo usuario no comparten claves ni respuestas) y la sesión las del usuario.
func claveDeActor(a contexto.Actor) string {
	if a.TokenID != 0 {
		return fmt.Sprintf("token:%d", a.TokenID)
	}
	if a.Tipo == contexto.ActorCliente {
		// El actor cliente no es un usuario (ID 0): sus claves son por cliente y empresa.
		return fmt.Sprintf("cliente:%d:%d", a.EmpresaID, a.ClienteID)
	}
	return fmt.Sprintf("%s:%d", a.Tipo, a.ID)
}

// MiddlewareIdempotencia aplica Idempotency-Key a POST, PUT, PATCH y DELETE:
//
//   - misma clave, mismo método, ruta y cuerpo → la misma respuesta guardada (status y cuerpo),
//     con la cabecera Idempotent-Replayed: true;
//   - misma clave con otra petición → 422 idempotencia_reutilizada;
//   - misma clave mientras la primera sigue en curso → 409 idempotencia_en_proceso;
//   - si la primera respondió 5xx, la clave se libera y se puede reintentar;
//   - si la primera sigue en_proceso después de Reclamo (el proceso cayó entre el COMMIT y
//     guardar la respuesta), la misma petición la retoma y se ejecuta de nuevo. Riesgo
//     residual: la acción puede correr dos veces; los casos de uso lo toleran con version,
//     unicidad o reglas de estado (un 409/422 en el reintento);
//   - si la respuesta llevaba un secreto (NoRepetible), no se guarda su cuerpo y repetir la
//     clave responde 409 idempotencia_no_repetible;
//   - si la fila se guardó con una clave HMAC que el servidor ya no conoce (se rotó
//     IRIS_CLAVE_CIFRADO y se quitó la anterior), no se puede comparar: 422
//     idempotencia_no_verificable (no se ejecuta otra vez a ciegas).
//
// El cuerpo se guarda como HMAC-SHA256 con clave del servidor, nunca como hash plano (puede
// llevar contraseñas): ver idempotencia_hash.go.
//
// Las claves son por credencial (claveDeActor): dos usuarios, o dos tokens, pueden usar la
// misma. Las peticiones anónimas no se guardan (la respuesta podría servirse a otro). Va
// después de la autenticación y de ConBD.
func MiddlewareIdempotencia(op OpcionesIdempotencia) func(http.Handler) http.Handler {
	if op.Vigencia <= 0 {
		op.Vigencia = 24 * time.Hour
	}
	if op.MaxCuerpo <= 0 {
		op.MaxCuerpo = 1 << 20
	}
	if op.Reclamo <= 0 {
		op.Reclamo = 2 * time.Minute
	}
	if op.Log == nil {
		op.Log = slog.Default()
	}
	if len(op.Claves) == 0 {
		op.Claves = []ClaveIdempotencia{ClaveIdempotenciaAleatoria()}
	}
	actual := op.Claves[0]
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
			hash := actual.hashPeticion(r.Method, ruta, cuerpo)

			q := nucleobd.New(bd)
			claveActor := claveDeActor(actor)
			bdCtx := context.WithoutCancel(ctx)

			if _, err := q.BorrarIdempotenciaExpirada(bdCtx, nucleobd.BorrarIdempotenciaExpiradaParams{Actor: claveActor, Clave: clave}); err != nil {
				op.Log.ErrorContext(ctx, "idempotencia: limpiar clave expirada", "err", err)
			}
			filas, err := q.ReservarIdempotencia(bdCtx, nucleobd.ReservarIdempotenciaParams{
				Actor: claveActor, Clave: clave, Metodo: r.Method, Ruta: ruta,
				HashPeticion: hash, HashClaveID: actual.id, ExpiraEn: time.Now().Add(op.Vigencia),
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
				igual, verificable := compararPeticion(op.Claves, previa.HashFormato, previa.HashClaveID, previa.HashPeticion, r.Method, ruta, cuerpo)
				if !op.responderPrevia(ctx, w, q, previa, igual, verificable, hash, actual.id) {
					return
				}
				// Clave abandonada en en_proceso: esta petición la retomó y se ejecuta.
				op.Log.WarnContext(ctx, "idempotencia: clave en proceso abandonada; se ejecuta de nuevo", "actor", claveActor)
			}

			noRepetible := false
			r = r.WithContext(context.WithValue(ctx, claveNoRepetible(0), &noRepetible))
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
			completa := nucleobd.CompletarIdempotenciaParams{
				Actor: claveActor, Clave: clave, Estado: "completa", Status: &status, ContentType: &tipo, Respuesta: grabadora.cuerpo.Bytes(),
			}
			if noRepetible {
				completa.Estado, completa.ContentType, completa.Respuesta = "no_repetible", nil, nil
			}
			if err := q.CompletarIdempotencia(bdCtx, completa); err != nil {
				op.Log.ErrorContext(ctx, "idempotencia: guardar respuesta", "err", err)
			}
		})
	}
}

// responderPrevia responde a una clave ya reservada. Devuelve true solo si la clave estaba
// abandonada en en_proceso y esta petición (la misma) la retomó: entonces hay que ejecutarla.
func (op OpcionesIdempotencia) responderPrevia(ctx context.Context, w http.ResponseWriter, q *nucleobd.Queries,
	previa nucleobd.NucleoIdempotencium, igual, verificable bool, hash, idClave []byte,
) bool {
	switch {
	case !verificable:
		// La fila es de una clave HMAC que ya no se conoce: no se sabe si es la misma petición.
		// Ejecutarla de nuevo podría repetir la acción (lo que la idempotencia evita): 422.
		EscribirProblema(w, NuevoProblema(http.StatusUnprocessableEntity, "idempotencia_no_verificable",
			"Clave de idempotencia no verificable",
			"La clave se usó antes de un cambio de la clave del servidor y no se puede comparar la petición; "+
				"revisa si la operación ya se hizo y, si no, repítela con otra Idempotency-Key"))
	case !igual:
		EscribirProblema(w, NuevoProblema(http.StatusUnprocessableEntity, "idempotencia_reutilizada",
			"Clave de idempotencia reutilizada",
			"La clave ya se usó con otra petición (método, ruta o cuerpo distintos)"))
	case previa.Estado == "no_repetible":
		EscribirProblema(w, Conflicto("idempotencia_no_repetible",
			"La operación ya se hizo con esta clave, pero su respuesta llevaba un secreto que no se guarda; "+
				"consulta el recurso creado o usa otra clave"))
	case previa.Estado == "en_proceso":
		ahora := time.Now()
		filas, err := q.ReclamarIdempotencia(context.WithoutCancel(ctx), nucleobd.ReclamarIdempotenciaParams{
			Actor: previa.Actor, Clave: previa.Clave, HashPeticion: hash, HashClaveID: idClave, HashPrevio: previa.HashPeticion,
			Antes: ahora.Add(-op.Reclamo), ExpiraEn: ahora.Add(op.Vigencia),
		})
		if err != nil {
			op.Log.ErrorContext(ctx, "idempotencia: retomar clave", "err", err)
			EscribirProblema(w, ErrorInterno())
			return false
		}
		if filas == 1 {
			return true
		}
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
	return false
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
