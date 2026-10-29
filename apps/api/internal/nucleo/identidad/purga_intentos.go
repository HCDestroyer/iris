package identidad

import (
	"context"
	"log/slog"
	"time"

	"github.com/riverqueue/river"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	identidadbd "github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad/bd"
)

// Purga de nucleo.intentos_login. Cada correo que falla un login (exista o no) y cada
// `usuario:<id>` que falla el cambio de contraseña deja una fila que solo se borra al acertar:
// sin la purga, la tabla crece sin límite con los correos de cualquier fuerza bruta. Una fila
// cuyo último fallo es anterior a la ventana y sin bloqueo vigente ya no cuenta para nada, así
// que borrarla no cambia ningún límite.

const (
	// LoginVentanaPorDefecto: los fallos se cuentan dentro de esta ventana (Config.LoginVentana).
	LoginVentanaPorDefecto = 15 * time.Minute
	// LoginBloqueoPorDefecto: duración del bloqueo al llegar al máximo (Config.LoginBloqueo).
	LoginBloqueoPorDefecto = 15 * time.Minute
	// LotePurga: filas por sentencia DELETE de las purgas (transacciones cortas, sin frenar logins).
	LotePurga = 1000
)

// PurgarIntentosLogin borra, por lotes de `lote`, las filas cuyo último fallo es anterior a
// `ahora - antiguedad` y sin bloqueo vigente. El corte se fija al empezar («todo lo pendiente
// hasta ahora»): lo que falle mientras corre queda para la próxima vez y el bucle termina.
// Devuelve cuántas borró.
func PurgarIntentosLogin(ctx context.Context, bd nucleo.BD, ahora time.Time, antiguedad time.Duration, lote int32) (int64, error) {
	hasta := ahora.Add(-antiguedad)
	q := identidadbd.New(bd)
	return porLotes(ctx, lote, func(lote int32) (int64, error) {
		return q.PurgarIntentosLogin(ctx, identidadbd.PurgarIntentosLoginParams{Hasta: hasta, Lote: lote})
	})
}

// porLotes repite borrar (cada llamada, una sentencia y su propia transacción corta) hasta que
// borra menos de un lote. Devuelve el total.
func porLotes(ctx context.Context, lote int32, borrar func(int32) (int64, error)) (int64, error) {
	if lote <= 0 {
		lote = LotePurga
	}
	var total int64
	for {
		n, err := borrar(lote)
		total += n
		if err != nil || n < int64(lote) {
			return total, err
		}
		if err := ctx.Err(); err != nil {
			return total, err
		}
	}
}

// AntiguedadPurgaIntentos es la edad a partir de la cual una fila se purga: la mayor entre la
// ventana de conteo y el bloqueo (con los valores por defecto, 15 min).
func AntiguedadPurgaIntentos(ventana, bloqueo time.Duration) time.Duration {
	if ventana <= 0 {
		ventana = LoginVentanaPorDefecto
	}
	if bloqueo <= 0 {
		bloqueo = LoginBloqueoPorDefecto
	}
	return max(ventana, bloqueo)
}

// ArgsPurgaIntentosLogin es el job periódico de la purga.
type ArgsPurgaIntentosLogin struct{}

// Kind implementa river.JobArgs.
func (ArgsPurgaIntentosLogin) Kind() string { return "nucleo.intentos_login_purga" }

// InsertOpts: único por media hora (el periódico de varios workers no se amontona).
func (ArgsPurgaIntentosLogin) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 3, UniqueOpts: river.UniqueOpts{ByPeriod: 30 * time.Minute}}
}

type trabajadorPurgaIntentos struct {
	river.WorkerDefaults[ArgsPurgaIntentosLogin]
	bd         nucleo.BD
	antiguedad time.Duration
}

func (w *trabajadorPurgaIntentos) Work(ctx context.Context, _ *river.Job[ArgsPurgaIntentosLogin]) error {
	n, err := PurgarIntentosLogin(ctx, w.bd, time.Now(), w.antiguedad, LotePurga)
	if n > 0 {
		slog.InfoContext(ctx, "identidad: intentos de login vencidos purgados", "filas", n)
	}
	return err
}

// RegistrarPurgaIntentos agrega el job al cliente de River del worker. ventana y bloqueo son
// los de Config (0 = los valores por defecto).
func RegistrarPurgaIntentos(workers *river.Workers, bd nucleo.BD, ventana, bloqueo time.Duration) {
	river.AddWorker(workers, &trabajadorPurgaIntentos{bd: bd, antiguedad: AntiguedadPurgaIntentos(ventana, bloqueo)})
}

// TrabajoPeriodicoPurgaIntentos purga cada intervalo (1 h por defecto) y al arrancar.
func TrabajoPeriodicoPurgaIntentos(intervalo time.Duration) *river.PeriodicJob {
	if intervalo <= 0 {
		intervalo = time.Hour
	}
	return river.NewPeriodicJob(river.PeriodicInterval(intervalo),
		func() (river.JobArgs, *river.InsertOpts) { return ArgsPurgaIntentosLogin{}, nil },
		&river.PeriodicJobOpts{RunOnStart: true})
}
