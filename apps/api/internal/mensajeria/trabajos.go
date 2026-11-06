package mensajeria

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// ctxSistema prepara el contexto de un job: actor sistema, canal worker y la base.
func ctxSistema(ctx context.Context, bd nucleo.BD) context.Context {
	return nucleo.ConBD(contexto.Sistema(ctx, contexto.CanalWorker), bd)
}

// RegistrarWorkers suma los workers de la mensajería (cola Cola) al cliente de River del proceso.
func (m *Modulo) RegistrarWorkers(w *river.Workers, bd nucleo.BD) {
	river.AddWorker(w, &trabajadorEnvio{m: m, bd: bd})
	river.AddWorker(w, &trabajadorEntrante{m: m, bd: bd})
	river.AddWorker(w, &trabajadorDespacho{m: m, bd: bd})
	river.AddWorker(w, &trabajadorMantenimiento{m: m, bd: bd})
	river.AddWorker(w, &trabajadorMensaje{m: m, bd: bd})
	river.AddWorker(w, &trabajadorMedio{m: m, bd: bd})
}

// ArgsMantenimiento: avisos sin acuse (agotado), webhooks de Meta que quedaron sin procesar y la
// purga del feed del chat (ChatRetencion) y de las fotos del chat nunca enviadas
// (ChatFotosSinEnviar).
type ArgsMantenimiento struct{}

// Kind implementa river.JobArgs.
func (ArgsMantenimiento) Kind() string { return "mensajeria.mantenimiento" }

// InsertOpts: uno por minuto.
func (ArgsMantenimiento) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: Cola, MaxAttempts: 3, UniqueOpts: river.UniqueOpts{ByPeriod: time.Minute}}
}

type trabajadorMantenimiento struct {
	river.WorkerDefaults[ArgsMantenimiento]
	m  *Modulo
	bd nucleo.BD
}

func (w *trabajadorMantenimiento) Work(ctx context.Context, _ *river.Job[ArgsMantenimiento]) error {
	ctx = ctxSistema(ctx, w.bd)
	if _, err := w.m.vencerAvisos.Ejecutar(ctx, struct{}{}); err != nil {
		return err
	}
	if err := w.m.purgarFeedChat(ctx, w.bd); err != nil {
		return err
	}
	if err := w.m.purgarFotosSinEnviar(ctx, w.bd); err != nil {
		return err
	}
	if err := w.m.purgarFeedConv(ctx, w.bd); err != nil {
		return err
	}
	if _, err := w.m.vencerSLA.Ejecutar(ctx, struct{}{}); err != nil {
		return err
	}
	// Bandeja 1.1: disponibilidad (sesión, horario, inactividad) y temporizadores del bot. Un caso
	// por fila: un error en una queda en el log y no corta el mantenimiento.
	w.m.vencerDisponibilidades(ctx, w.bd)
	w.m.dispararBots(ctx, w.bd)
	ids, err := mensajeriabd.New(w.bd).EntrantesPendientes(ctx, w.m.ahora().Add(-5*time.Minute))
	if err != nil {
		return err
	}
	for _, id := range ids {
		if _, err := w.m.procesarMeta.Ejecutar(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

// Reloj encola el despacho de webhooks (cada 10 s) y el mantenimiento (cada minuto) hasta que ctx
// termina. En lugar de trabajos periódicos de River (que solo encola el líder, y el líder puede ser
// el worker de la API, que no conoce estos jobs), cada proceso de mensajería los encola únicos por
// periodo: con varias réplicas, igual corre uno.
func Reloj(ctx context.Context, cliente *river.Client[pgx.Tx], log *slog.Logger) {
	encolar := func(args river.JobArgs) {
		if _, err := cliente.Insert(ctx, args, nil); err != nil && ctx.Err() == nil {
			log.WarnContext(ctx, "mensajeria: encolar trabajo del reloj", "trabajo", args.Kind(), "err", err)
		}
	}
	encolar(ArgsDespacharWebhooks{})
	encolar(ArgsMantenimiento{})
	despacho, mant := time.NewTicker(10*time.Second), time.NewTicker(time.Minute)
	defer despacho.Stop()
	defer mant.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-despacho.C:
			encolar(ArgsDespacharWebhooks{})
		case <-mant.C:
			encolar(ArgsMantenimiento{})
		}
	}
}
