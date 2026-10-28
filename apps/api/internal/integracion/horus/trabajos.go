package horus

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

// Jobs de River del conector Horus (corren en cmd/worker).

// ArgsProcesarEntrante procesa un webhook de Horus guardado en integracion.entrantes.
type ArgsProcesarEntrante struct {
	EntranteID int64 `json:"entrante_id"`
}

// Kind implementa river.JobArgs.
func (ArgsProcesarEntrante) Kind() string { return "integracion.horus_procesar_entrante" }

// InsertOpts: un job por entrante (único por argumentos); River reintenta con su espera
// exponencial si falla la base.
func (ArgsProcesarEntrante) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: integracion.ColaIntegracion, MaxAttempts: 12, UniqueOpts: river.UniqueOpts{ByArgs: true}}
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

// RegistrarWorkers agrega los workers del conector al cliente de River del worker.
func (m *Modulo) RegistrarWorkers(workers *river.Workers, bd nucleo.BD) {
	river.AddWorker(workers, &trabajadorEntrante{m: m, bd: bd})
}
