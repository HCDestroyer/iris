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
// una vez (reintentos, caída del relay entre la entrega y su COMMIT). Si devuelve error, el evento se
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
// publicado_en. Procesa todo lo pendiente, un evento por transacción corta (FOR UPDATE SKIP
// LOCKED): lo entregado queda confirmado aunque el relay se corte después. Un advisory lock,
// tomado en una transacción aparte que dura toda la corrida, garantiza un solo relay a la vez.
type Relay struct {
	BD  nucleo.BD
	Bus *Bus
	Log *slog.Logger
	// EsperaMaxima acota el reintento exponencial (1 h por defecto).
	EsperaMaxima time.Duration
	// MaxIntentos: tras este número de entregas fallidas el evento queda fallido (fallido_en)
	// y no se reintenta más; se registra como error en el log. 20 por defecto.
	MaxIntentos int32
}

// Ejecutar corre el relay hasta vaciar lo pendiente. Si otro relay tiene el candado, sale sin
// hacer nada. Devuelve cuántos eventos publicó.
func (r *Relay) Ejecutar(ctx context.Context) (int, error) {
	ctx = nucleo.ConBD(ctx, r.BD)
	base := ctx // sin transacción: cada evento y cada suscriptor abren la suya
	publicados := 0
	err := nucleo.EnTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		tomado, err := integracionbd.New(tx).TomarCandadoRelay(ctx, candadoRelay)
		if err != nil || !tomado {
			return err
		}
		for base.Err() == nil {
			hubo, publicado, err := r.uno(base)
			if publicado {
				publicados++
			}
			if err != nil || !hubo {
				return err
			}
		}
		return nil
	})
	return publicados, err
}

// uno toma un evento pendiente, lo entrega y registra el resultado en su propia transacción.
// La marca se escribe aunque ctx se cancele durante la entrega (el fallo cuenta como intento).
func (r *Relay) uno(ctx context.Context) (hubo, publicado bool, err error) {
	log := r.Log
	if log == nil {
		log = slog.Default()
	}
	maxIntentos := r.MaxIntentos
	if maxIntentos <= 0 {
		maxIntentos = 20
	}
	err = nucleo.EnTx(context.WithoutCancel(ctx), func(tctx context.Context, tx pgx.Tx) error {
		q := integracionbd.New(tx)
		f, err := q.TomarEventoPendiente(tctx)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		hubo = true
		e := desdeFila(f)
		errEntrega := r.entregar(ctx, e)
		if errEntrega == nil {
			publicado = true
			return q.MarcarPublicado(tctx, e.ID)
		}
		msg := errEntrega.Error()
		if e.Intentos+1 >= maxIntentos {
			log.ErrorContext(ctx, "relay: evento fallido tras agotar sus intentos; no se reintenta", "evento", e.Evento,
				"id", e.ID, "intentos", e.Intentos+1, "err", errEntrega)
			return q.MarcarAgotado(tctx, integracionbd.MarcarAgotadoParams{ID: e.ID, UltimoError: &msg})
		}
		espera := r.espera(e.Intentos)
		log.WarnContext(ctx, "relay: evento no entregado", "evento", e.Evento, "id", e.ID,
			"intentos", e.Intentos+1, "reintento_en", espera, "err", errEntrega)
		return q.MarcarFallido(tctx, integracionbd.MarcarFallidoParams{
			ID: e.ID, UltimoError: &msg, ProximoIntento: time.Now().Add(espera),
		})
	})
	return hubo, publicado, err
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
