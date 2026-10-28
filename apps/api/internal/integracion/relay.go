package integracion

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	integracionbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Suscriptor recibe un evento del outbox. Debe ser idempotente: un evento puede llegar más de
// una vez (reintentos, caída del relay a mitad de lote). Si devuelve error, el evento se
// reintenta con espera exponencial para TODOS sus suscriptores.
//
// El ctx trae actor sistema, canal worker, el request id de la acción que emitió el evento y
// el pool (nucleo.ConBD): un suscriptor que escribe llama a un caso de uso, que abre su propia
// transacción.
type Suscriptor func(ctx context.Context, e Evento) error

type suscripcion struct {
	nombre string
	evento string // "*" = todos
	fn     Suscriptor
}

// Bus es el registro de suscriptores internos.
type Bus struct {
	mu   sync.RWMutex
	subs []suscripcion
}

// NuevoBus crea un bus vacío.
func NuevoBus() *Bus { return &Bus{} }

// Suscribir registra fn para el evento (o "*" para todos). nombre identifica al suscriptor en
// el log y en ultimo_error.
func (b *Bus) Suscribir(nombre, evento string, fn Suscriptor) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subs = append(b.subs, suscripcion{nombre: nombre, evento: evento, fn: fn})
}

func (b *Bus) para(evento string) []suscripcion {
	b.mu.RLock()
	defer b.mu.RUnlock()
	var salida []suscripcion
	for _, s := range b.subs {
		if s.evento == "*" || s.evento == evento {
			salida = append(salida, s)
		}
	}
	return salida
}

// candadoRelay es la clave del advisory lock que garantiza un solo relay a la vez.
const candadoRelay int64 = 0x1715_0001

// Relay publica los eventos pendientes del outbox a los suscriptores del Bus y marca
// publicado_en. Procesa "todo lo pendiente hasta ahora" en lotes.
type Relay struct {
	BD   nucleo.BD
	Bus  *Bus
	Lote int32
	Log  *slog.Logger
	// EsperaMaxima acota el reintento exponencial (1 h por defecto).
	EsperaMaxima time.Duration
}

// Ejecutar corre el relay hasta vaciar lo pendiente. Si otro relay tiene el candado, sale sin
// hacer nada. Devuelve cuántos eventos publicó.
func (r *Relay) Ejecutar(ctx context.Context) (int, error) {
	total := 0
	for {
		n, seguir, err := r.lote(ctx)
		total += n
		if err != nil || !seguir {
			return total, err
		}
	}
}

func (r *Relay) lote(ctx context.Context) (publicados int, seguir bool, err error) {
	lote := r.Lote
	if lote <= 0 {
		lote = 100
	}
	log := r.Log
	if log == nil {
		log = slog.Default()
	}
	ctx = nucleo.ConBD(ctx, r.BD)
	base := ctx // sin la transacción del relay: los suscriptores abren la suya

	err = nucleo.EnTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		q := integracionbd.New(tx)
		tomado, err := q.TomarCandadoRelay(ctx, candadoRelay)
		if err != nil || !tomado {
			return err
		}
		filas, err := q.EventosPendientes(ctx, lote)
		if err != nil {
			return err
		}
		seguir = int32(len(filas)) == lote //nolint:gosec // lote ≤ int32
		for _, f := range filas {
			e := desdeFila(f)
			if errEntrega := r.entregar(base, e); errEntrega != nil {
				espera := r.espera(e.Intentos)
				log.WarnContext(ctx, "relay: evento no entregado", "evento", e.Evento, "id", e.ID,
					"intentos", e.Intentos+1, "reintento_en", espera, "err", errEntrega)
				msg := errEntrega.Error()
				if err := q.MarcarFallido(ctx, integracionbd.MarcarFallidoParams{
					ID: e.ID, UltimoError: &msg, ProximoIntento: time.Now().Add(espera),
				}); err != nil {
					return err
				}
				continue
			}
			if err := q.MarcarPublicado(ctx, e.ID); err != nil {
				return err
			}
			publicados++
		}
		return nil
	})
	return publicados, seguir && err == nil, err
}

func (r *Relay) entregar(ctx context.Context, e Evento) error {
	sctx := contexto.ConRequestID(ctx, e.RequestID)
	sctx = contexto.Sistema(sctx, contexto.CanalWorker)
	var errs []string
	for _, s := range r.Bus.para(e.Evento) {
		if err := llamar(sctx, s, e); err != nil {
			errs = append(errs, s.nombre+": "+err.Error())
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

// llamar aísla el pánico de un suscriptor para que no tumbe el lote.
func llamar(ctx context.Context, s suscripcion, e Evento) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("pánico: %v", rec)
		}
	}()
	return s.fn(ctx, e)
}

func (r *Relay) espera(intentos int32) time.Duration {
	maxima := r.EsperaMaxima
	if maxima <= 0 {
		maxima = time.Hour
	}
	espera := 5 * time.Second
	for i := int32(0); i < intentos && espera < maxima; i++ {
		espera *= 2
	}
	return min(espera, maxima)
}

// ArgsRelay es el job de River que corre el relay.
type ArgsRelay struct{}

// Kind implementa river.JobArgs.
func (ArgsRelay) Kind() string { return "integracion.relay_outbox" }

// InsertOpts: único por segundo (River) y, dentro, el advisory lock del relay.
func (ArgsRelay) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       ColaIntegracion,
		MaxAttempts: 3,
		UniqueOpts:  river.UniqueOpts{ByPeriod: time.Second},
	}
}

// ColaIntegracion es la cola de River de este módulo.
const ColaIntegracion = "integracion"

// TrabajadorRelay es el worker de River del relay.
type TrabajadorRelay struct {
	river.WorkerDefaults[ArgsRelay]
	Relay *Relay
}

// Work implementa river.Worker.
func (t *TrabajadorRelay) Work(ctx context.Context, _ *river.Job[ArgsRelay]) error {
	_, err := t.Relay.Ejecutar(ctx)
	return err
}

// Timeout del job.
func (t *TrabajadorRelay) Timeout(*river.Job[ArgsRelay]) time.Duration { return 5 * time.Minute }

// TrabajoPeriodicoRelay corre el relay cada intervalo aunque no llegue un NOTIFY (River solo lo
// agenda desde el líder: una instancia).
func TrabajoPeriodicoRelay(intervalo time.Duration) *river.PeriodicJob {
	return river.NewPeriodicJob(river.PeriodicInterval(intervalo),
		func() (river.JobArgs, *river.InsertOpts) { return ArgsRelay{}, nil },
		&river.PeriodicJobOpts{RunOnStart: true})
}
