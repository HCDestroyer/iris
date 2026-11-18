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
			ColaRelay:          {MaxWorkers: 1}, // el advisory lock ya deja correr uno a la vez
		},
		Workers:      workers,
		PeriodicJobs: append([]*river.PeriodicJob{TrabajoPeriodicoRelay(op.RelayIntervalo, pool, op.Esquema)}, op.Periodicos...),
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
//
// No encola si ya hay un relay disponible (available) en la cola: aún no empezó, así que su
// barrido es posterior al COMMIT avisado. La consulta va a river_job (esquema de River) y no a
// un indicador en memoria para que valga también con el relay encolado por otra réplica. Un
// relay en curso no cuenta: pudo hacer ya su último barrido.
func EscucharOutbox(ctx context.Context, pool *pgxpool.Pool, cliente *river.Client[pgx.Tx], esquema string, log *slog.Logger) {
	escucharOutbox(ctx, pool, cliente, esquema, log, ganchosEscucha{})
}

// ganchosEscucha son avisos para las pruebas: listo tras cada LISTEN y su barrido (con el pid
// de la conexión que escucha); antesDeReconectar al perder el LISTEN, antes de esperar.
type ganchosEscucha struct {
	listo             func(pid uint32)
	antesDeReconectar func(espera time.Duration)
}

func escucharOutbox(ctx context.Context, pool *pgxpool.Pool, cliente *river.Client[pgx.Tx], esquema string, log *slog.Logger, g ganchosEscucha) {
	if log == nil {
		log = slog.Default()
	}
	espera := time.Second
	for ctx.Err() == nil {
		escucho, err := escuchar(ctx, pool, cliente, esquema, log, g.listo)
		if ctx.Err() != nil {
			return
		}
		if escucho {
			espera = time.Second // la espera crece solo con fallos seguidos
		}
		log.WarnContext(ctx, "outbox: se perdió el LISTEN; reintentando", "err", err, "espera", espera)
		if g.antesDeReconectar != nil {
			g.antesDeReconectar(espera)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(espera):
		}
		espera = min(espera*2, 30*time.Second)
	}
}

// escuchar hace LISTEN y encola un relay por aviso hasta perder la conexión. escucho es true
// si el LISTEN llegó a hacerse.
func escuchar(ctx context.Context, pool *pgxpool.Pool, cliente *river.Client[pgx.Tx], esquema string, log *slog.Logger,
	listo func(pid uint32),
) (escucho bool, err error) {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return false, err
	}
	// La conexión no vuelve al pool: seguiría escuchando y su último query («LISTEN …») se
	// confundiría con el de un listener vivo.
	defer func() {
		cctx, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		_ = conn.Hijack().Close(cctx)
	}()
	if _, err := conn.Exec(ctx, "LISTEN "+CanalNotificacion); err != nil {
		return false, err
	}

	// Los avisos que llegan mientras se encola un job se agrupan en el siguiente: cualquiera de
	// ellos llegó tras su COMMIT, y el job se inserta (o se ve disponible) después de recibirlo.
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
			if err := encolarRelay(ectx, pool, cliente, esquema, log); err != nil {
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
				return true, e
			default:
				return true, err
			}
		}
		select {
		case aviso <- struct{}{}:
		default: // ya hay uno pendiente de encolar
		}
	}
}

// encolarRelay encola un relay salvo que ya haya uno disponible sin empezar en su cola. Si la
// comprobación falla, encola igual (un job de más es inofensivo); solo el error del Insert corta
// el LISTEN.
func encolarRelay(ctx context.Context, pool *pgxpool.Pool, cliente *river.Client[pgx.Tx], esquema string, log *slog.Logger) error {
	hay, err := hayRelayDisponible(ctx, pool, esquema)
	if err != nil {
		log.WarnContext(ctx, "outbox: no se pudo ver si hay un relay disponible; se encola igual", "err", err)
	} else if hay {
		return nil
	}
	_, err = cliente.Insert(ctx, ArgsRelay{}, nil)
	return err
}

// hayRelayDisponible dice si hay un relay en estado available en ColaRelay. Consulta propia (no
// JobList de River): JobList ordena por id con parámetros ANY, y con plan genérico recorre la PK
// de river_job entera (76 ms con 500 000 filas completed); esta usa el índice por estado y cola.
// river_job no está en las migraciones goose, así que no pasa por sqlc.
func hayRelayDisponible(ctx context.Context, pool *pgxpool.Pool, esquema string) (bool, error) {
	var hay bool
	err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM `+pgx.Identifier{esquema, "river_job"}.Sanitize()+`
		WHERE state = 'available' AND queue = '`+ColaRelay+`' AND kind = $1)`, ArgsRelay{}.Kind()).Scan(&hay)
	return hay, err
}
