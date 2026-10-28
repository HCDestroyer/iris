package nucleo

import (
	"context"
	"log/slog"
	"time"

	"github.com/riverqueue/river"

	nucleobd "github.com/HCDestroyer/iris/apps/api/internal/nucleo/bd"
)

// Purga de nucleo.idempotencia: borra todo lo vencido hasta ahora (expira_en < now()). Sin ella
// la tabla crece sin límite y las filas de formato 0 (SHA-256 plano del cuerpo, de antes del
// HMAC) se quedarían para siempre: con el job, desaparecen como mucho 24 h (la vigencia) más el
// intervalo del job después de migrar.

// PurgarIdempotencia borra las claves de idempotencia vencidas y devuelve cuántas.
func PurgarIdempotencia(ctx context.Context, bd BD) (int64, error) {
	return nucleobd.New(bd).PurgarIdempotencia(ctx)
}

// ArgsPurgaIdempotencia es el job periódico de la purga.
type ArgsPurgaIdempotencia struct{}

// Kind implementa river.JobArgs.
func (ArgsPurgaIdempotencia) Kind() string { return "nucleo.idempotencia_purga" }

// InsertOpts: único por media hora (el periódico de varios workers no se amontona).
func (ArgsPurgaIdempotencia) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 3, UniqueOpts: river.UniqueOpts{ByPeriod: 30 * time.Minute}}
}

type trabajadorPurgaIdempotencia struct {
	river.WorkerDefaults[ArgsPurgaIdempotencia]
	bd BD
}

func (w *trabajadorPurgaIdempotencia) Work(ctx context.Context, _ *river.Job[ArgsPurgaIdempotencia]) error {
	n, err := PurgarIdempotencia(ctx, w.bd)
	if err == nil && n > 0 {
		slog.InfoContext(ctx, "idempotencia: claves vencidas purgadas", "filas", n)
	}
	return err
}

// RegistrarPurgaIdempotencia agrega el job de la purga al cliente de River del worker.
func RegistrarPurgaIdempotencia(workers *river.Workers, bd BD) {
	river.AddWorker(workers, &trabajadorPurgaIdempotencia{bd: bd})
}

// TrabajoPeriodicoPurgaIdempotencia purga cada intervalo (1 h por defecto) y al arrancar.
func TrabajoPeriodicoPurgaIdempotencia(intervalo time.Duration) *river.PeriodicJob {
	if intervalo <= 0 {
		intervalo = time.Hour
	}
	return river.NewPeriodicJob(river.PeriodicInterval(intervalo),
		func() (river.JobArgs, *river.InsertOpts) { return ArgsPurgaIdempotencia{}, nil },
		&river.PeriodicJobOpts{RunOnStart: true})
}
