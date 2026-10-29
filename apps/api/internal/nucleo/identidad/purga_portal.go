package identidad

import (
	"context"
	"log/slog"
	"time"

	"github.com/riverqueue/river"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	identidadbd "github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad/bd"
)

// Purga del portal de soporte: cada entrada desde Argos deja un ticket de acceso (60 s) y una
// sesión (2 h) que nunca se borraban. Se conservan RetencionPortal después de vencer o de
// revocarse (IP y navegador de la sesión sirven para revisar un incidente); luego se borran.

// RetencionPortal: cuánto se guarda una sesión o un ticket del portal ya vencidos.
const RetencionPortal = 30 * 24 * time.Hour

// PurgaPortal es lo que borró PurgarPortal.
type PurgaPortal struct {
	Sesiones int64
	Accesos  int64
}

// PurgarPortal borra por lotes las sesiones del portal vencidas o revocadas antes de
// `ahora - retencion` y luego los tickets de acceso vencidos antes de esa fecha que ya no tienen
// sesiones. El corte se fija al empezar («todo lo pendiente hasta ahora»).
func PurgarPortal(ctx context.Context, bd nucleo.BD, ahora time.Time, retencion time.Duration, lote int32) (PurgaPortal, error) {
	hasta := ahora.Add(-retencion)
	q := identidadbd.New(bd)
	var res PurgaPortal
	var err error
	res.Sesiones, err = porLotes(ctx, lote, func(lote int32) (int64, error) {
		return q.PurgarSesionesPortal(ctx, identidadbd.PurgarSesionesPortalParams{Hasta: hasta, Lote: lote})
	})
	if err != nil {
		return res, err
	}
	res.Accesos, err = porLotes(ctx, lote, func(lote int32) (int64, error) {
		return q.PurgarAccesosPortal(ctx, identidadbd.PurgarAccesosPortalParams{Hasta: hasta, Lote: lote})
	})
	return res, err
}

// ArgsPurgaPortal es el job periódico de la purga del portal.
type ArgsPurgaPortal struct{}

// Kind implementa river.JobArgs.
func (ArgsPurgaPortal) Kind() string { return "nucleo.portal_purga" }

// InsertOpts: único por media hora (el periódico de varios workers no se amontona).
func (ArgsPurgaPortal) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 3, UniqueOpts: river.UniqueOpts{ByPeriod: 30 * time.Minute}}
}

type trabajadorPurgaPortal struct {
	river.WorkerDefaults[ArgsPurgaPortal]
	bd nucleo.BD
}

func (w *trabajadorPurgaPortal) Work(ctx context.Context, _ *river.Job[ArgsPurgaPortal]) error {
	res, err := PurgarPortal(ctx, w.bd, time.Now(), RetencionPortal, LotePurga)
	if res.Sesiones > 0 || res.Accesos > 0 {
		slog.InfoContext(ctx, "identidad: sesiones y accesos del portal vencidos purgados", "sesiones", res.Sesiones, "accesos", res.Accesos)
	}
	return err
}

// RegistrarPurgaPortal agrega el job al cliente de River del worker.
func RegistrarPurgaPortal(workers *river.Workers, bd nucleo.BD) {
	river.AddWorker(workers, &trabajadorPurgaPortal{bd: bd})
}

// TrabajoPeriodicoPurgaPortal purga cada intervalo (1 h por defecto) y al arrancar.
func TrabajoPeriodicoPurgaPortal(intervalo time.Duration) *river.PeriodicJob {
	if intervalo <= 0 {
		intervalo = time.Hour
	}
	return river.NewPeriodicJob(river.PeriodicInterval(intervalo),
		func() (river.JobArgs, *river.InsertOpts) { return ArgsPurgaPortal{}, nil },
		&river.PeriodicJobOpts{RunOnStart: true})
}
