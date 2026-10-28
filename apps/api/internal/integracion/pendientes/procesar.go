package pendientes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	pendientesbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/pendientes/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// maxPorCorrida acota una corrida (lo que quede, lo toma la siguiente).
const maxPorCorrida = 500

// Procesar ejecuta todo lo vencido hasta ahora (CLAUDE.md §6: «todo lo pendiente hasta
// ahora»), uno por uno: toma el pendiente en una transacción corta (en_curso), llama a Argos
// fuera de transacción y guarda el resultado en un caso de uso (auditado, con su evento).
// Devuelve cuántos procesó. ctx debe traer el pool (nucleo.ConBD) y el actor sistema.
func (m *Modulo) Procesar(ctx context.Context) (int, error) {
	ahora := m.ahora()
	n := 0
	for n < maxPorCorrida {
		f, ok, err := m.tomar(ctx, ahora)
		if err != nil || !ok {
			return n, err
		}
		n++
		res := m.intentar(ctx, f)
		res.ahora = ahora
		if _, err := m.resolver.Ejecutar(ctx, res); err != nil {
			return n, fmt.Errorf("pendientes: guardar el resultado de %d: %w", f.ID, err)
		}
	}
	return n, nil
}

func (m *Modulo) tomar(ctx context.Context, ahora time.Time) (pendientesbd.IntegracionPendiente, bool, error) {
	var f pendientesbd.IntegracionPendiente
	hay := false
	err := nucleo.EnTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		q := pendientesbd.New(tx)
		var err error
		f, err = q.TomarVencido(ctx, pendientesbd.TomarVencidoParams{Ahora: ahora, ReclamarAntes: ahora.Add(-m.cfg.Reclamo)})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		hay = true
		return q.MarcarEnCurso(ctx, pendientesbd.MarcarEnCursoParams{Ahora: ahora, ID: f.ID})
	})
	return f, hay, err
}

// resultadoIntento es lo que pasó al llamar a Argos.
type resultadoIntento struct {
	ID int64 `json:"pendiente_id"`
	// Exito, o error de negocio (Negocio = true) o temporal.
	Exito   bool   `json:"exito"`
	Negocio bool   `json:"negocio"`
	Status  *int32 `json:"status"`
	Error   string `json:"error,omitempty"`

	ahora time.Time
}

// intentar hace la llamada a Argos y clasifica el resultado.
func (m *Modulo) intentar(ctx context.Context, f pendientesbd.IntegracionPendiente) resultadoIntento {
	r := resultadoIntento{ID: f.ID}
	if m.cfg.Argos == nil {
		r.Error = "conector Argos no configurado en el worker"
		return r
	}
	cliente, err := m.cfg.Argos.ClienteActivo(ctx, nucleo.BDDe(ctx))
	if err != nil {
		// Sin conector activo (o sin poder descifrar su token): cuenta como intento fallido y
		// se reintenta con la misma espera; al agotarse, alerta.
		r.Error = "sin conector Argos activo: " + resumenProblema(err)
		return r
	}
	var cuerpo any = json.RawMessage(f.Payload)
	err = cliente.Escribir(ctx, f.Metodo, f.Ruta, cuerpo, f.IdempotencyKey, nil)
	if err == nil {
		r.Exito = true
		return r
	}
	var er *conector.ErrorRemoto
	if !errors.As(err, &er) {
		r.Error = "error al llamar a Argos: " + sinCredenciales(err.Error())
		return r
	}
	if er.Status != 0 {
		s := int32(er.Status) //nolint:gosec // código HTTP
		r.Status = &s
	}
	r.Negocio = esNegocio(er)
	r.Error = describir(er)
	return r
}

// esNegocio: un 4xx que reintentar no arregla (datos, estado del servicio, existencias). No lo
// son 401/403 (credencial o permiso del token: se arregla en Argos y el reintento pasa), 408,
// 409 (Argos procesando la misma Idempotency-Key), 425 y 429.
func esNegocio(er *conector.ErrorRemoto) bool {
	if er.Bloqueado() || er.Status < 400 || er.Status >= 500 {
		return false
	}
	switch er.Status {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusRequestTimeout, http.StatusConflict,
		http.StatusTooEarly, http.StatusTooManyRequests:
		return false
	}
	return true
}

var credencial = regexp.MustCompile(`(?i)(atk_|iris_pat_|hf_pat_|bearer\s+)[A-Za-z0-9._~+/=-]+`)

// sinCredenciales quita tokens que el otro sistema pudiera repetir en su detail.
func sinCredenciales(s string) string {
	s = credencial.ReplaceAllString(s, "${1}***")
	if len(s) > 500 {
		s = s[:500]
	}
	return s
}

// describir resume el error para ultimo_error: HTTP, type y el detail de Argos (sin
// credenciales, a lo más 500 caracteres). Es para el supervisor (contrato §3).
func describir(er *conector.ErrorRemoto) string {
	switch {
	case er.Bloqueado():
		return "destino_no_permitido: la URL de Argos resuelve a una red interna (IRIS_ARGOS_REDES_PERMITIDAS)"
	case er.Status == 0:
		return "Argos no respondió: " + sinCredenciales(er.Error())
	}
	partes := []string{fmt.Sprintf("HTTP %d", er.Status)}
	if er.Tipo != "" {
		partes = append(partes, er.Tipo)
	}
	msg := strings.Join(partes, " ")
	if er.Detalle != "" {
		msg += ": " + er.Detalle
	} else if er.Titulo != "" {
		msg += ": " + er.Titulo
	}
	return sinCredenciales(msg)
}

func resumenProblema(err error) string {
	p, interno := nucleo.AProblema(err)
	if interno {
		return "error interno"
	}
	return p.Codigo()
}

type eventoPendiente struct {
	PendienteID    int64  `json:"pendiente_id"`
	Operacion      string `json:"operacion"`
	Entidad        string `json:"entidad"`
	EntidadID      string `json:"entidad_id"`
	IdempotencyKey string `json:"idempotency_key"`
}

type eventoFallo struct {
	eventoPendiente
	Intentos int32  `json:"intentos"`
	Status   *int32 `json:"status"`
	Detalle  string `json:"detalle"`
}

func eventoDe(f pendientesbd.IntegracionPendiente) eventoPendiente {
	return eventoPendiente{PendienteID: f.ID, Operacion: f.Operacion, Entidad: f.Entidad, EntidadID: f.EntidadID, IdempotencyKey: f.IdempotencyKey}
}

// nuevoResolver guarda el resultado de un intento: hecho, fallido_negocio, reintento con la
// espera del contrato o agotado. Corre como sistema (worker).
func nuevoResolver(m *Modulo) *auditoria.Caso[resultadoIntento, Pendiente] {
	return auditoria.Nuevo(auditoria.Caso[resultadoIntento, Pendiente]{
		Accion: "integraciones.procesar_pendiente", Acceso: auditoria.Autenticado,
		Logica: func(ctx context.Context, t *auditoria.Tx, in resultadoIntento) (Pendiente, error) {
			q := pendientesbd.New(t)
			antes, err := q.PendientePorIDParaActualizar(ctx, in.ID)
			if err != nil {
				return Pendiente{}, err
			}
			t.FijarEntidad("pendiente", antes.ID)
			if antes.EmpresaID != nil {
				t.FijarEmpresa(*antes.EmpresaID)
			}
			p := pendientesbd.ResolverPendienteParams{ID: in.ID, ProximoIntento: in.ahora, UltimoStatus: in.Status}
			evento, payload := "", any(nil)
			intentos := antes.Intentos + 1
			switch {
			case in.Exito:
				p.Estado, p.HechoEn = EstadoHecho, &in.ahora
				evento, payload = EventoHecho, eventoDe(antes)
			case in.Negocio:
				p.Estado, p.UltimoError = EstadoFallidoNegocio, &in.Error
				evento, payload = EventoFallido, eventoFallo{eventoPendiente: eventoDe(antes), Intentos: intentos, Status: in.Status, Detalle: in.Error}
			case int(intentos) <= len(Esperas):
				p.Estado, p.UltimoError = EstadoPendiente, &in.Error
				p.ProximoIntento = in.ahora.Add(Esperas[intentos-1])
			default:
				p.Estado, p.UltimoError = EstadoAgotado, &in.Error
				evento, payload = EventoAgotado, eventoFallo{eventoPendiente: eventoDe(antes), Intentos: intentos, Status: in.Status, Detalle: in.Error}
			}
			f, err := q.ResolverPendiente(ctx, p)
			if errors.Is(err, pgx.ErrNoRows) {
				// Ya no estaba en curso (otro worker lo reclamó): no se pisa.
				return aPendiente(antes), nil
			}
			if err != nil {
				return Pendiente{}, err
			}
			t.RegistrarCambios("pendiente", f.ID, map[string]any{"estado": antes.Estado, "intentos": antes.Intentos},
				map[string]any{"estado": f.Estado, "intentos": f.Intentos})
			if evento != "" {
				if err := t.Emitir(evento, payload); err != nil {
					return Pendiente{}, err
				}
			}
			if f.Estado == EstadoFallidoNegocio || f.Estado == EstadoAgotado {
				slog.WarnContext(ctx, "pendientes: acción hacia Argos sin completar", "pendiente", f.ID,
					"operacion", f.Operacion, "estado", f.Estado, "error", in.Error)
			}
			return aPendiente(f), nil
		},
	})
}

// Jobs de River ----------------------------------------------------------------------------

// ArgsProcesar procesa todo lo vencido de la cola.
type ArgsProcesar struct{}

// Kind implementa river.JobArgs.
func (ArgsProcesar) Kind() string { return "integracion.pendientes_procesar" }

// InsertOpts: cola de integración, único por ventana de 10 s (el periódico y los avisos al
// encolar no se amontonan).
func (ArgsProcesar) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: integracion.ColaIntegracion, MaxAttempts: 3, UniqueOpts: river.UniqueOpts{ByPeriod: 10 * time.Second}}
}

type trabajador struct {
	river.WorkerDefaults[ArgsProcesar]
	m  *Modulo
	bd nucleo.BD
}

func (w *trabajador) Work(ctx context.Context, _ *river.Job[ArgsProcesar]) error {
	ctx = nucleo.ConBD(contexto.Sistema(ctx, contexto.CanalWorker), w.bd)
	_, err := w.m.Procesar(ctx)
	return err
}

func (w *trabajador) Timeout(*river.Job[ArgsProcesar]) time.Duration { return 10 * time.Minute }

// RegistrarWorkers agrega el worker de la cola al cliente de River.
func (m *Modulo) RegistrarWorkers(workers *river.Workers, bd nucleo.BD) {
	river.AddWorker(workers, &trabajador{m: m, bd: bd})
}

// TrabajoPeriodico procesa la cola cada intervalo (1 min por defecto: la espera más corta del
// contrato).
func TrabajoPeriodico(intervalo time.Duration) *river.PeriodicJob {
	if intervalo <= 0 {
		intervalo = time.Minute
	}
	return river.NewPeriodicJob(river.PeriodicInterval(intervalo),
		func() (river.JobArgs, *river.InsertOpts) { return ArgsProcesar{}, nil },
		&river.PeriodicJobOpts{RunOnStart: true})
}
