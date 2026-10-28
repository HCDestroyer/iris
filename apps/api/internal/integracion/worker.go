package integracion

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
)

// OpcionesWorker configura el cliente de River del worker.
type OpcionesWorker struct {
	Esquema        string        // schema de River ("river")
	RelayIntervalo time.Duration // corrida periódica del relay (5 s por defecto)
	Log            *slog.Logger
	// Registrar agrega los workers de otros módulos (I2) al mismo cliente.
	Registrar func(*river.Workers)
}

// NuevoClienteRiver arma el cliente de River con el relay del outbox y su corrida periódica.
func NuevoClienteRiver(pool *pgxpool.Pool, relay *Relay, op OpcionesWorker) (*river.Client[pgx.Tx], error) {
	if op.RelayIntervalo <= 0 {
		op.RelayIntervalo = 5 * time.Second
	}
	workers := river.NewWorkers()
	river.AddWorker(workers, &TrabajadorRelay{Relay: relay})
	if op.Registrar != nil {
		op.Registrar(workers)
	}
	return river.NewClient(riverpgxv5.New(pool), &river.Config{
		Schema: op.Esquema,
		Logger: op.Log,
		Queues: map[string]river.QueueConfig{
			river.QueueDefault: {MaxWorkers: 10},
			ColaIntegracion:    {MaxWorkers: 2},
		},
		Workers:      workers,
		PeriodicJobs: []*river.PeriodicJob{TrabajoPeriodicoRelay(op.RelayIntervalo)},
	})
}

// EscucharOutbox hace LISTEN iris_outbox y, por cada aviso (un COMMIT con eventos), encola el
// relay. River lo deduplica por segundo y el relay toma su advisory lock. Se reconecta solo;
// termina cuando ctx se cancela.
func EscucharOutbox(ctx context.Context, pool *pgxpool.Pool, cliente *river.Client[pgx.Tx], log *slog.Logger) {
	if log == nil {
		log = slog.Default()
	}
	espera := time.Second
	for ctx.Err() == nil {
		err := escuchar(ctx, pool, cliente)
		if ctx.Err() != nil {
			return
		}
		log.WarnContext(ctx, "outbox: se perdió el LISTEN; reintentando", "err", err, "espera", espera)
		select {
		case <-ctx.Done():
			return
		case <-time.After(espera):
		}
		espera = min(espera*2, 30*time.Second)
	}
}

func escuchar(ctx context.Context, pool *pgxpool.Pool, cliente *river.Client[pgx.Tx]) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "LISTEN "+CanalNotificacion); err != nil {
		return err
	}
	for {
		if _, err := conn.Conn().WaitForNotification(ctx); err != nil {
			return err
		}
		if _, err := cliente.Insert(ctx, ArgsRelay{}, nil); err != nil {
			return err
		}
	}
}
