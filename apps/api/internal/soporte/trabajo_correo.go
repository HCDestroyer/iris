package soporte

import (
	"context"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Jobs de River del correo. Todos corren como actor sistema (canal worker) y llaman casos de
// uso; la lógica vive en correo_*.go.

// unicoMientrasPendiente: un solo job por argumentos mientras no haya terminado (no cuenta los
// completados, así el siguiente minuto vuelve a encolar).
var unicoMientrasPendiente = river.UniqueOpts{ByArgs: true, ByState: []rivertype.JobState{
	rivertype.JobStateAvailable, rivertype.JobStatePending, rivertype.JobStateRunning,
	rivertype.JobStateRetryable, rivertype.JobStateScheduled,
}}

// ArgsProgramarBuzones: cron que encola la lectura de cada buzón activo.
type ArgsProgramarBuzones struct{}

// Kind implementa river.JobArgs.
func (ArgsProgramarBuzones) Kind() string { return "soporte.programar_buzones" }

// InsertOpts: único por minuto.
func (ArgsProgramarBuzones) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 3, UniqueOpts: river.UniqueOpts{ByPeriod: time.Minute}}
}

// ArgsLeerBuzon lee un buzón (único por buzón: nunca dos lecturas del mismo a la vez).
type ArgsLeerBuzon struct {
	BuzonID int64 `json:"buzon_id"`
}

// Kind implementa river.JobArgs.
func (ArgsLeerBuzon) Kind() string { return "soporte.leer_buzon" }

// InsertOpts: único por buzón mientras esté pendiente o corriendo; pocos reintentos (el cron
// lo vuelve a encolar cada minuto).
func (ArgsLeerBuzon) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: integracion.ColaIntegracion, MaxAttempts: 3, UniqueOpts: unicoMientrasPendiente}
}

// ArgsProcesarCorreos convierte en tickets los correos pendientes de un buzón, en orden.
type ArgsProcesarCorreos struct {
	BuzonID int64 `json:"buzon_id"`
}

// Kind implementa river.JobArgs.
func (ArgsProcesarCorreos) Kind() string { return "soporte.procesar_correos" }

// InsertOpts: único por buzón mientras esté pendiente o corriendo.
func (ArgsProcesarCorreos) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 10, UniqueOpts: unicoMientrasPendiente}
}

// ArgsEnviarCorreo envía una respuesta por SMTP.
type ArgsEnviarCorreo struct {
	SalienteID int64 `json:"saliente_id"`
}

// Kind implementa river.JobArgs.
func (ArgsEnviarCorreo) Kind() string { return "soporte.enviar_correo" }

// InsertOpts: único por correo saliente; River reintenta con su espera exponencial.
func (ArgsEnviarCorreo) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: integracion.ColaIntegracion, MaxAttempts: 12, UniqueOpts: unicoMientrasPendiente}
}

func ctxWorker(ctx context.Context, bd nucleo.BD) context.Context {
	return nucleo.ConBD(contexto.Sistema(ctx, contexto.CanalWorker), bd)
}

// TrabajadorProgramarBuzones corre soporte.programar_buzones.
type TrabajadorProgramarBuzones struct {
	river.WorkerDefaults[ArgsProgramarBuzones]
	BD     nucleo.BD
	Modulo *Modulo
}

// Work implementa river.Worker.
func (w *TrabajadorProgramarBuzones) Work(ctx context.Context, _ *river.Job[ArgsProgramarBuzones]) error {
	_, err := w.Modulo.ProgramarLecturas.Ejecutar(nucleo.ConBD(contexto.Sistema(ctx, contexto.CanalCron), w.BD), EntradaProgramarLecturas{})
	return err
}

// TrabajadorLeerBuzon corre soporte.leer_buzon.
type TrabajadorLeerBuzon struct {
	river.WorkerDefaults[ArgsLeerBuzon]
	BD     nucleo.BD
	Modulo *Modulo
}

// Work implementa river.Worker.
func (w *TrabajadorLeerBuzon) Work(ctx context.Context, j *river.Job[ArgsLeerBuzon]) error {
	_, err := w.Modulo.LeerBuzon(ctxWorker(ctx, w.BD), j.Args.BuzonID)
	return err
}

// Timeout del job.
func (w *TrabajadorLeerBuzon) Timeout(*river.Job[ArgsLeerBuzon]) time.Duration {
	return 10 * time.Minute
}

// TrabajadorProcesarCorreos corre soporte.procesar_correos.
type TrabajadorProcesarCorreos struct {
	river.WorkerDefaults[ArgsProcesarCorreos]
	BD     nucleo.BD
	Modulo *Modulo
}

// Work implementa river.Worker.
func (w *TrabajadorProcesarCorreos) Work(ctx context.Context, j *river.Job[ArgsProcesarCorreos]) error {
	_, err := w.Modulo.ProcesarCorreos(ctxWorker(ctx, w.BD), j.Args.BuzonID)
	return err
}

// Timeout del job.
func (w *TrabajadorProcesarCorreos) Timeout(*river.Job[ArgsProcesarCorreos]) time.Duration {
	return 10 * time.Minute
}

// TrabajadorEnviarCorreo corre soporte.enviar_correo.
type TrabajadorEnviarCorreo struct {
	river.WorkerDefaults[ArgsEnviarCorreo]
	BD     nucleo.BD
	Modulo *Modulo
}

// Work implementa river.Worker.
func (w *TrabajadorEnviarCorreo) Work(ctx context.Context, j *river.Job[ArgsEnviarCorreo]) error {
	return w.Modulo.EnviarCorreo(ctxWorker(ctx, w.BD), j.Args.SalienteID)
}

// TrabajoPeriodicoBuzones agenda la lectura de los buzones cada intervalo (1 min por defecto).
func TrabajoPeriodicoBuzones(intervalo time.Duration) *river.PeriodicJob {
	if intervalo <= 0 {
		intervalo = time.Minute
	}
	return river.NewPeriodicJob(river.PeriodicInterval(intervalo),
		func() (river.JobArgs, *river.InsertOpts) { return ArgsProgramarBuzones{}, nil },
		&river.PeriodicJobOpts{RunOnStart: true})
}

// SuscribirCorreo registra el consumidor de ticket.respondido (salida por correo).
func (m *Modulo) SuscribirCorreo(bus *integracion.Bus, bd nucleo.BD) {
	bus.Suscribir("soporte.correo_saliente", "ticket.respondido", func(ctx context.Context, e integracion.Evento) error {
		return m.AlResponderTicket(nucleo.ConBD(ctx, bd), e)
	})
}
