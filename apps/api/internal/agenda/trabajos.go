package agenda

import (
	"context"
	"time"

	"github.com/riverqueue/river"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// ArgsEspejoZonas es el job que copia el catálogo de zonas de Argos (integracion.argos_zonas,
// que refresca el conector) a agenda.zonas.
type ArgsEspejoZonas struct{}

// Kind implementa river.JobArgs.
func (ArgsEspejoZonas) Kind() string { return "agenda.espejo_zonas" }

// InsertOpts: único por periodo; la corrida es idempotente.
func (ArgsEspejoZonas) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 3, UniqueOpts: river.UniqueOpts{ByPeriod: 5 * time.Minute}}
}

// TrabajadorEspejoZonas corre el espejo como actor sistema (canal cron).
type TrabajadorEspejoZonas struct {
	river.WorkerDefaults[ArgsEspejoZonas]
	BD     nucleo.BD
	Modulo *Modulo
}

// Work implementa river.Worker.
func (w *TrabajadorEspejoZonas) Work(ctx context.Context, _ *river.Job[ArgsEspejoZonas]) error {
	ctx = nucleo.ConBD(contexto.Sistema(ctx, contexto.CanalCron), w.BD)
	_, err := w.Modulo.SincronizarZonas.Ejecutar(ctx, struct{}{})
	return err
}

// TrabajoPeriodicoZonas agenda el espejo cada intervalo (10 min por defecto) y al arrancar.
func TrabajoPeriodicoZonas(intervalo time.Duration) *river.PeriodicJob {
	if intervalo <= 0 {
		intervalo = 10 * time.Minute
	}
	return river.NewPeriodicJob(river.PeriodicInterval(intervalo),
		func() (river.JobArgs, *river.InsertOpts) { return ArgsEspejoZonas{}, nil },
		&river.PeriodicJobOpts{RunOnStart: true})
}

// RegistrarTrabajos agrega los workers de agenda (integracion.OpcionesWorker.Registrar).
func (m *Modulo) RegistrarTrabajos(bd nucleo.BD) func(*river.Workers) {
	return func(w *river.Workers) {
		river.AddWorker(w, &TrabajadorEspejoZonas{BD: bd, Modulo: m})
	}
}
