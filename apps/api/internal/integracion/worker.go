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
	// Periodicos: trabajos periódicos de otros módulos (p. ej. catálogos de Argos).
	Periodicos []*river.PeriodicJob
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
		PeriodicJobs: append([]*river.PeriodicJob{TrabajoPeriodicoRelay(op.RelayIntervalo)}, op.Periodicos...),
	})
}

// NuevoClienteInsercion arma un cliente de River que solo inserta jobs (la API los encola en la
// transacción del caso de uso con InsertTx; los corre cmd/worker).
func NuevoClienteInsercion(pool *pgxpool.Pool, esquema string, log *slog.Logger) (*river.Client[pgx.Tx], error) {
	return river.NewClient(riverpgxv5.New(pool), &river.Config{Schema: esquema, Logger: log})
}

// Encolar devuelve la función con que un caso de uso encola un job en su transacción.
func Encolar(cliente *river.Client[pgx.Tx]) func(ctx context.Context, tx pgx.Tx, args river.JobArgs) error {
	return func(ctx context.Context, tx pgx.Tx, args river.JobArgs) error {
		_, err := cliente.InsertTx(ctx, tx, args, nil)
		return err
	}
}

// EscucharOutbox hace LISTEN iris_outbox y, por cada aviso (un COMMIT con eventos), encola el
// relay; el advisory lock del relay garantiza uno a la vez. Se reconecta solo; termina cuando
// ctx se cancela.
//
// Un NOTIFY emitido sin nadie escuchando (al arrancar, o mientras se reconecta) se pierde: por
// eso, tras cada LISTEN encola un relay que barre lo que quedó pendiente.
func EscucharOutbox(ctx context.Context, pool *pgxpool.Pool, cliente *river.Client[pgx.Tx], log *slog.Logger) {
	escucharOutbox(ctx, pool, cliente, log, nil)
}

// escucharOutbox es EscucharOutbox con listo, que se llama tras cada LISTEN y su barrido
// encolado con el pid de la conexión que escucha (solo pruebas).
func escucharOutbox(ctx context.Context, pool *pgxpool.Pool, cliente *river.Client[pgx.Tx], log *slog.Logger, listo func(pid uint32)) {
	if log == nil {
		log = slog.Default()
	}
	espera := time.Second
	for ctx.Err() == nil {
		err := escuchar(ctx, pool, cliente, listo)
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

func escuchar(ctx context.Context, pool *pgxpool.Pool, cliente *river.Client[pgx.Tx], listo func(pid uint32)) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	// La conexión no vuelve al pool: seguiría escuchando y su último query («LISTEN …») se
	// confundiría con el de un listener vivo.
	defer func() { _ = conn.Hijack().Close(context.Background()) }()
	if _, err := conn.Exec(ctx, "LISTEN "+CanalNotificacion); err != nil {
		return err
	}

	// Los avisos que llegan mientras se encola un job se agrupan en el siguiente: cualquiera de
	// ellos llegó tras su COMMIT, y el job se inserta después de recibirlo.
	pid := conn.Conn().PgConn().PID()
	aviso := make(chan struct{}, 1)
	aviso <- struct{}{} // barrido de lo que quedó pendiente sin nadie escuchando
	ectx, cancelar := context.WithCancel(ctx)
	defer cancelar()
	errEncolar := make(chan error, 1)
	go func() {
		primero := true
		for {
			select {
			case <-ectx.Done():
				return
			case <-aviso:
			}
			if _, err := cliente.Insert(ectx, ArgsRelay{}, nil); err != nil {
				errEncolar <- err
				cancelar()
				return
			}
			if primero && listo != nil {
				listo(pid)
			}
			primero = false
		}
	}()

	for {
		if _, err := conn.Conn().WaitForNotification(ectx); err != nil {
			select {
			case e := <-errEncolar:
				return e
			default:
				return err
			}
		}
		select {
		case aviso <- struct{}{}:
		default: // ya hay uno pendiente de encolar
		}
	}
}
