package adjuntos

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	adjuntosbd "github.com/HCDestroyer/iris/apps/api/internal/nucleo/adjuntos/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// MotivoAbandonado: subida sin terminar y sin actividad por más de Config.Abandono.
const MotivoAbandonado = "abandonado"

type entradaAbandonar struct {
	ID    uuid.UUID `json:"adjunto_id"`
	Antes time.Time `json:"sin_actividad_desde"`
}

// nuevoAbandonar rechaza un adjunto abandonado (si sigue sin actividad), borra sus partes y
// emite adjunto.rechazado con motivo «abandonado». Corre como sistema (worker).
func nuevoAbandonar(*Modulo) *auditoria.Caso[entradaAbandonar, bool] {
	return auditoria.Nuevo(auditoria.Caso[entradaAbandonar, bool]{
		Accion: "adjuntos.abandonar", Acceso: auditoria.Autenticado, AuditarExito: func(b bool) bool { return b },
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaAbandonar) (bool, error) {
			q := adjuntosbd.New(t)
			f, err := q.AbandonarAdjunto(ctx, adjuntosbd.AbandonarAdjuntoParams{ID: in.ID, Antes: in.Antes})
			if errors.Is(err, pgx.ErrNoRows) {
				return false, nil // terminó o tuvo actividad entretanto
			}
			if err != nil {
				return false, err
			}
			if err := q.BorrarPartes(ctx, in.ID); err != nil {
				return false, err
			}
			a := aAdjunto(f)
			t.FijarEntidad("adjunto", a.ID)
			t.FijarEmpresa(empresaDe(f.EmpresaID))
			t.RegistrarCambios("adjunto", a.ID, nil, map[string]any{"estado": a.Estado, "motivo_rechazo": MotivoAbandonado})
			return true, t.Emitir(EventoRechazado, eventoRechazado{eventoAdjunto: eventoDe(a), Motivo: MotivoAbandonado})
		},
	})
}

// PurgarAbandonados rechaza todos los adjuntos sin terminar cuya última actividad es anterior
// a ahora − Config.Abandono. Devuelve cuántos. ctx: actor sistema y el pool (nucleo.ConBD).
func (m *Modulo) PurgarAbandonados(ctx context.Context) (int, error) {
	antes := m.cfg.Reloj().Add(-m.cfg.Abandono)
	total := 0
	for range 50 {
		ids, err := adjuntosbd.New(nucleo.BDDe(ctx)).AdjuntosAbandonados(ctx, adjuntosbd.AdjuntosAbandonadosParams{Antes: antes, Limite: 200})
		if err != nil {
			return total, err
		}
		for _, id := range ids {
			hecho, err := m.abandonar.Ejecutar(ctx, entradaAbandonar{ID: id, Antes: antes})
			if err != nil {
				return total, err
			}
			if hecho {
				total++
			}
		}
		if len(ids) < 200 {
			break
		}
	}
	return total, nil
}

// ArgsAbandonados es el job periódico que purga las subidas abandonadas.
type ArgsAbandonados struct{}

// Kind implementa river.JobArgs.
func (ArgsAbandonados) Kind() string { return "nucleo.adjuntos_abandonados" }

// InsertOpts: único por media hora (el periódico de varios workers no se amontona).
func (ArgsAbandonados) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 3, UniqueOpts: river.UniqueOpts{ByPeriod: 30 * time.Minute}}
}

type trabajadorAbandonados struct {
	river.WorkerDefaults[ArgsAbandonados]
	m  *Modulo
	bd nucleo.BD
}

func (w *trabajadorAbandonados) Work(ctx context.Context, _ *river.Job[ArgsAbandonados]) error {
	ctx = nucleo.ConBD(contexto.Sistema(ctx, contexto.CanalWorker), w.bd)
	_, err := w.m.PurgarAbandonados(ctx)
	return err
}

// RegistrarWorkers agrega el job de abandonados al cliente de River del worker.
func (m *Modulo) RegistrarWorkers(workers *river.Workers, bd nucleo.BD) {
	river.AddWorker(workers, &trabajadorAbandonados{m: m, bd: bd})
}

// TrabajoPeriodico purga las subidas abandonadas cada intervalo (1 h por defecto).
func TrabajoPeriodico(intervalo time.Duration) *river.PeriodicJob {
	if intervalo <= 0 {
		intervalo = time.Hour
	}
	return river.NewPeriodicJob(river.PeriodicInterval(intervalo),
		func() (river.JobArgs, *river.InsertOpts) { return ArgsAbandonados{}, nil },
		&river.PeriodicJobOpts{RunOnStart: true})
}
