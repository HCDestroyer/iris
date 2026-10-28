package campo

import (
	"context"
	"time"

	"github.com/riverqueue/river"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// ArgsMantenerPosiciones: particiones de campo.posiciones (crea las próximas, purga las viejas).
type ArgsMantenerPosiciones struct{}

// Kind implementa river.JobArgs.
func (ArgsMantenerPosiciones) Kind() string { return "campo.mantener_posiciones" }

// InsertOpts: único por hora; la corrida es idempotente.
func (ArgsMantenerPosiciones) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 3, UniqueOpts: river.UniqueOpts{ByPeriod: time.Hour}}
}

// TrabajadorPosiciones corre el mantenimiento como actor sistema (canal cron).
type TrabajadorPosiciones struct {
	river.WorkerDefaults[ArgsMantenerPosiciones]
	BD     nucleo.BD
	Modulo *Modulo
}

// Work implementa river.Worker.
func (w *TrabajadorPosiciones) Work(ctx context.Context, _ *river.Job[ArgsMantenerPosiciones]) error {
	ctx = nucleo.ConBD(contexto.Sistema(ctx, contexto.CanalCron), w.BD)
	_, err := w.Modulo.MantenerPosiciones.Ejecutar(ctx, struct{}{})
	return err
}

// TrabajoPeriodicoPosiciones agenda el mantenimiento (cada 6 h por defecto) y al arrancar.
func TrabajoPeriodicoPosiciones(intervalo time.Duration) *river.PeriodicJob {
	if intervalo <= 0 {
		intervalo = 6 * time.Hour
	}
	return river.NewPeriodicJob(river.PeriodicInterval(intervalo),
		func() (river.JobArgs, *river.InsertOpts) { return ArgsMantenerPosiciones{}, nil },
		&river.PeriodicJobOpts{RunOnStart: true})
}

// RegistrarTrabajos agrega los workers de campo (integracion.OpcionesWorker.Registrar).
func (m *Modulo) RegistrarTrabajos(bd nucleo.BD) func(*river.Workers) {
	return func(w *river.Workers) {
		river.AddWorker(w, &TrabajadorPosiciones{BD: bd, Modulo: m})
	}
}
