package argos

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Jobs de River del conector (corren en cmd/worker).

// ArgsProcesarEntrante procesa un webhook guardado en integracion.entrantes.
type ArgsProcesarEntrante struct {
	EntranteID int64 `json:"entrante_id"`
}

// Kind implementa river.JobArgs.
func (ArgsProcesarEntrante) Kind() string { return "integracion.argos_procesar_entrante" }

// InsertOpts: un job por entrante (único por argumentos); River reintenta con su espera
// exponencial si Argos no responde.
func (ArgsProcesarEntrante) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: integracion.ColaIntegracion, MaxAttempts: 12, UniqueOpts: river.UniqueOpts{ByArgs: true}}
}

// ArgsSincronizarCatalogos refresca las instantáneas de zonas y routers.
type ArgsSincronizarCatalogos struct{}

// Kind implementa river.JobArgs.
func (ArgsSincronizarCatalogos) Kind() string { return "integracion.argos_sincronizar_catalogos" }

// InsertOpts: único por minuto.
func (ArgsSincronizarCatalogos) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: integracion.ColaIntegracion, MaxAttempts: 5, UniqueOpts: river.UniqueOpts{ByPeriod: time.Minute}}
}

type trabajadorEntrante struct {
	river.WorkerDefaults[ArgsProcesarEntrante]
	m  *Modulo
	bd nucleo.BD
}

func (w *trabajadorEntrante) Work(ctx context.Context, job *river.Job[ArgsProcesarEntrante]) error {
	ctx = nucleo.ConBD(contexto.Sistema(ctx, contexto.CanalWorker), w.bd)
	_, err := w.m.ProcesarEntrante.Ejecutar(ctx, job.Args.EntranteID)
	if errors.Is(err, pgx.ErrNoRows) {
		return river.JobCancel(err)
	}
	var p *nucleo.Problema
	if errors.As(err, &p) && p.Status < http.StatusInternalServerError && p.Status != http.StatusConflict {
		// Error de datos (4xx): reintentar no lo arregla.
		return river.JobCancel(err)
	}
	return err
}

func (w *trabajadorEntrante) Timeout(*river.Job[ArgsProcesarEntrante]) time.Duration {
	return 2 * time.Minute
}

type trabajadorCatalogos struct {
	river.WorkerDefaults[ArgsSincronizarCatalogos]
	m  *Modulo
	bd nucleo.BD
}

func (w *trabajadorCatalogos) Work(ctx context.Context, _ *river.Job[ArgsSincronizarCatalogos]) error {
	ctx = nucleo.ConBD(contexto.Sistema(ctx, contexto.CanalWorker), w.bd)
	_, err := w.m.SincronizarCatalogos.Ejecutar(ctx, struct{}{})
	return err
}

func (w *trabajadorCatalogos) Timeout(*river.Job[ArgsSincronizarCatalogos]) time.Duration {
	return 5 * time.Minute
}

// RegistrarWorkers agrega los workers del conector al cliente de River del worker.
func (m *Modulo) RegistrarWorkers(workers *river.Workers, bd nucleo.BD) {
	river.AddWorker(workers, &trabajadorEntrante{m: m, bd: bd})
	river.AddWorker(workers, &trabajadorCatalogos{m: m, bd: bd})
}

// TrabajosPeriodicos: refresco de zonas y routers cada intervalo (6 h por defecto).
func TrabajosPeriodicos(intervalo time.Duration) []*river.PeriodicJob {
	if intervalo <= 0 {
		intervalo = 6 * time.Hour
	}
	return []*river.PeriodicJob{river.NewPeriodicJob(river.PeriodicInterval(intervalo),
		func() (river.JobArgs, *river.InsertOpts) { return ArgsSincronizarCatalogos{}, nil },
		&river.PeriodicJobOpts{RunOnStart: true})}
}
