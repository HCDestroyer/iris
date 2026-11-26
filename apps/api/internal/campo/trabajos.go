package campo

import (
	"context"
	"log/slog"
	"time"

	"github.com/riverqueue/river"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/pendientes"
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
		river.AddWorker(w, &TrabajadorActivaciones{BD: bd, Modulo: m})
	}
}

// ArgsConsultarActivaciones: las activaciones en proceso sin aviso de Argos en 10 min se consultan
// (GET /aprovisionamientos/:id, contrato 0.9.0 §12.2). Procesa todo lo pendiente hasta ahora.
type ArgsConsultarActivaciones struct{}

// Kind implementa river.JobArgs.
func (ArgsConsultarActivaciones) Kind() string { return "campo.consultar_activaciones" }

// InsertOpts: único por minuto (la corrida es idempotente).
func (ArgsConsultarActivaciones) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 3, UniqueOpts: river.UniqueOpts{ByPeriod: time.Minute}}
}

// TrabajadorActivaciones corre la consulta como actor sistema (canal cron).
type TrabajadorActivaciones struct {
	river.WorkerDefaults[ArgsConsultarActivaciones]
	BD     nucleo.BD
	Modulo *Modulo
}

// Work implementa river.Worker.
func (w *TrabajadorActivaciones) Work(ctx context.Context, _ *river.Job[ArgsConsultarActivaciones]) error {
	ctx = nucleo.ConBD(contexto.Sistema(ctx, contexto.CanalCron), w.BD)
	_, err := w.Modulo.Activacion.Consultar(ctx)
	return err
}

// TrabajoPeriodicoActivaciones agenda la consulta (cada 2 min por defecto).
func TrabajoPeriodicoActivaciones(intervalo time.Duration) *river.PeriodicJob {
	if intervalo <= 0 {
		intervalo = 2 * time.Minute
	}
	return river.NewPeriodicJob(river.PeriodicInterval(intervalo),
		func() (river.JobArgs, *river.InsertOpts) { return ArgsConsultarActivaciones{}, nil }, nil)
}

// SuscribirActivaciones conecta la activación con la cola de pendientes (el 202 de aprovisionar o su
// rechazo) y con los webhooks de Argos (argos.instalacion_resuelta). Idempotente: un intento resuelto
// no cambia (cmd/worker).
func (m *Modulo) SuscribirActivaciones(bus *integracion.Bus, bd nucleo.BD, log *slog.Logger) {
	pendiente := func(ctx context.Context, e integracion.Evento) error {
		return m.resolverDesdePendiente(nucleo.ConBD(ctx, bd), e.Evento, e.Payload)
	}
	for _, ev := range []string{pendientes.EventoHecho, pendientes.EventoFallido, pendientes.EventoResueltoManual} {
		bus.Suscribir("campo.activacion_pendiente", ev, pendiente)
	}
	bus.Suscribir("campo.activacion_webhook", argos.EventoInstalacionResuelta, func(ctx context.Context, e integracion.Evento) error {
		err := m.resolverDesdeWebhook(nucleo.ConBD(ctx, bd), e.Payload)
		if err != nil {
			log.WarnContext(ctx, "campo: no se pudo aplicar el resultado de la activación", "evento", e.ID, "err", err)
		}
		return err
	})
}
