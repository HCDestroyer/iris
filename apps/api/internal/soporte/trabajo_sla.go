package soporte

import (
	"context"
	"time"

	"github.com/riverqueue/river"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// ArgsRevisarSLA es el job de River del cron de SLA.
type ArgsRevisarSLA struct{}

// Kind implementa river.JobArgs.
func (ArgsRevisarSLA) Kind() string { return "soporte.revisar_sla" }

// InsertOpts: único por minuto; la corrida además es idempotente por ticket y umbral.
func (ArgsRevisarSLA) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 3, UniqueOpts: river.UniqueOpts{ByPeriod: time.Minute}}
}

// TrabajadorSLA corre soporte.revisar_sla como actor sistema (canal cron).
type TrabajadorSLA struct {
	river.WorkerDefaults[ArgsRevisarSLA]
	BD     nucleo.BD
	Modulo *Modulo
}

// Work implementa river.Worker.
func (w *TrabajadorSLA) Work(ctx context.Context, _ *river.Job[ArgsRevisarSLA]) error {
	ctx = nucleo.ConBD(contexto.Sistema(ctx, contexto.CanalCron), w.BD)
	_, err := w.Modulo.RevisarSLA.Ejecutar(ctx, EntradaRevisarSLA{})
	return err
}

// Timeout del job.
func (w *TrabajadorSLA) Timeout(*river.Job[ArgsRevisarSLA]) time.Duration { return 5 * time.Minute }

// TrabajoPeriodicoSLA agenda la revisión cada intervalo (1 min por defecto); River solo la
// agenda desde el líder.
func TrabajoPeriodicoSLA(intervalo time.Duration) *river.PeriodicJob {
	if intervalo <= 0 {
		intervalo = time.Minute
	}
	return river.NewPeriodicJob(river.PeriodicInterval(intervalo),
		func() (river.JobArgs, *river.InsertOpts) { return ArgsRevisarSLA{}, nil },
		&river.PeriodicJobOpts{RunOnStart: true})
}

// RegistrarTrabajos agrega los workers del SLA y del correo (para
// integracion.OpcionesWorker.Registrar).
func (m *Modulo) RegistrarTrabajos(bd nucleo.BD) func(*river.Workers) {
	return func(w *river.Workers) {
		river.AddWorker(w, &TrabajadorSLA{BD: bd, Modulo: m})
		river.AddWorker(w, &TrabajadorProgramarBuzones{BD: bd, Modulo: m})
		river.AddWorker(w, &TrabajadorLeerBuzon{BD: bd, Modulo: m})
		river.AddWorker(w, &TrabajadorProcesarCorreos{BD: bd, Modulo: m})
		river.AddWorker(w, &TrabajadorEnviarCorreo{BD: bd, Modulo: m})
	}
}
