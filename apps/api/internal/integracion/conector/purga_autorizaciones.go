package conector

import (
	"context"
	"log/slog"
	"time"

	"github.com/riverqueue/river"

	conectorbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/conector/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Purga de integracion.autorizaciones («Conectar con un botón», §1.4): una autorización
// abandonada (el administrador nunca volvió de Argos) no guarda su code_verifier cifrado después
// de vencer, y las vencidas hace más de un día se borran. Procesa todo lo pendiente hasta ahora.

// PurgarAutorizaciones olvida los code_verifier vencidos y borra las autorizaciones viejas.
func PurgarAutorizaciones(ctx context.Context, bd nucleo.BD) (olvidados, borradas int64, err error) {
	q := conectorbd.New(bd)
	if olvidados, err = q.OlvidarVerificadoresVencidos(ctx); err != nil {
		return 0, 0, err
	}
	borradas, err = q.BorrarAutorizacionesViejas(ctx)
	return olvidados, borradas, err
}

// ArgsPurgaAutorizaciones es el job periódico de la purga.
type ArgsPurgaAutorizaciones struct{}

// Kind implementa river.JobArgs.
func (ArgsPurgaAutorizaciones) Kind() string { return "integracion.autorizaciones_purga" }

// InsertOpts: único por periodo (el periódico de varios workers no se amontona).
func (ArgsPurgaAutorizaciones) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 3, UniqueOpts: river.UniqueOpts{ByPeriod: 5 * time.Minute}}
}

type trabajadorPurgaAutorizaciones struct {
	river.WorkerDefaults[ArgsPurgaAutorizaciones]
	bd nucleo.BD
}

func (w *trabajadorPurgaAutorizaciones) Work(ctx context.Context, _ *river.Job[ArgsPurgaAutorizaciones]) error {
	olvidados, borradas, err := PurgarAutorizaciones(ctx, w.bd)
	if err == nil && olvidados+borradas > 0 {
		slog.InfoContext(ctx, "conector: autorizaciones vencidas purgadas", "verificadores", olvidados, "borradas", borradas)
	}
	return err
}

// RegistrarPurgaAutorizaciones agrega el job de la purga al worker (cmd/worker).
func RegistrarPurgaAutorizaciones(workers *river.Workers, bd nucleo.BD) {
	river.AddWorker(workers, &trabajadorPurgaAutorizaciones{bd: bd})
}

// TrabajoPeriodicoPurgaAutorizaciones purga cada intervalo (10 min por defecto) y al arrancar.
func TrabajoPeriodicoPurgaAutorizaciones(intervalo time.Duration) *river.PeriodicJob {
	if intervalo <= 0 {
		intervalo = 10 * time.Minute
	}
	return river.NewPeriodicJob(river.PeriodicInterval(intervalo),
		func() (river.JobArgs, *river.InsertOpts) { return ArgsPurgaAutorizaciones{}, nil },
		&river.PeriodicJobOpts{RunOnStart: true})
}
