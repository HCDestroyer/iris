package notificaciones

import (
	"context"
	"time"

	"github.com/riverqueue/river"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	notificacionesbd "github.com/HCDestroyer/iris/apps/api/internal/notificaciones/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// TrabajadorEnviar es el worker de River de un envío.
type TrabajadorEnviar struct {
	river.WorkerDefaults[ArgsEnviar]
	BD     nucleo.BD
	Modulo *Modulo
}

// Work implementa river.Worker. 429/5xx devuelven error (River reintenta con NextRetry); en el
// último intento el envío queda fallido («agotado») y el job termina.
func (w *TrabajadorEnviar) Work(ctx context.Context, job *river.Job[ArgsEnviar]) error {
	ctx = nucleo.ConBD(contexto.Sistema(ctx, contexto.CanalWorker), w.BD)
	return w.Modulo.Enviar(ctx, job.Args.EnvioID, job.Attempt >= job.MaxAttempts)
}

// NextRetry: espera exponencial (15 s, 30 s, 1 min, … hasta 30 min).
func (w *TrabajadorEnviar) NextRetry(job *river.Job[ArgsEnviar]) time.Time {
	return time.Now().Add(EsperaReintento(job.Attempt))
}

// EsperaReintento tras el intento n (1, 2, …).
func EsperaReintento(n int) time.Duration {
	espera := 15 * time.Second
	for i := 1; i < n && espera < 30*time.Minute; i++ {
		espera *= 2
	}
	return min(espera, 30*time.Minute)
}

// Timeout del job.
func (w *TrabajadorEnviar) Timeout(*river.Job[ArgsEnviar]) time.Duration { return time.Minute }

// ArgsPurga es la purga periódica: dispositivos cuya sesión o token ya no vale y envíos viejos.
type ArgsPurga struct{}

// Kind implementa river.JobArgs.
func (ArgsPurga) Kind() string { return "notificaciones.purga" }

// InsertOpts: único por periodo.
func (ArgsPurga) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 3, UniqueOpts: river.UniqueOpts{ByPeriod: 30 * time.Minute}}
}

// TrabajadorPurga corre la purga como sistema (canal cron).
type TrabajadorPurga struct {
	river.WorkerDefaults[ArgsPurga]
	BD     nucleo.BD
	Modulo *Modulo
}

// Work implementa river.Worker.
func (w *TrabajadorPurga) Work(ctx context.Context, _ *river.Job[ArgsPurga]) error {
	ctx = nucleo.ConBD(contexto.Sistema(ctx, contexto.CanalCron), w.BD)
	_, err := w.Modulo.PurgarCaducos.Ejecutar(ctx, struct{}{})
	return err
}

// TrabajoPeriodicoPurga agenda la purga (cada hora por defecto).
func TrabajoPeriodicoPurga(intervalo time.Duration) *river.PeriodicJob {
	if intervalo <= 0 {
		intervalo = time.Hour
	}
	return river.NewPeriodicJob(river.PeriodicInterval(intervalo),
		func() (river.JobArgs, *river.InsertOpts) { return ArgsPurga{}, nil },
		&river.PeriodicJobOpts{RunOnStart: true})
}

// RegistrarTrabajos agrega los workers (integracion.OpcionesWorker.Registrar).
func (m *Modulo) RegistrarTrabajos(bd nucleo.BD) func(*river.Workers) {
	return func(w *river.Workers) {
		river.AddWorker(w, &TrabajadorEnviar{BD: bd, Modulo: m})
		river.AddWorker(w, &TrabajadorPurga{BD: bd, Modulo: m})
	}
}

// ResultadoPurga: cuántos dispositivos se quitaron y cuántos envíos se borraron.
type ResultadoPurga struct {
	Dispositivos int   `json:"dispositivos"`
	Envios       int64 `json:"envios"`
}

// porLote de la purga de dispositivos.
const porLote = 500

func (m *Modulo) nuevoPurgarCaducos() *auditoria.Caso[struct{}, ResultadoPurga] {
	return auditoria.Nuevo(auditoria.Caso[struct{}, ResultadoPurga]{
		Accion:       "notificaciones.purgar",
		Acceso:       auditoria.Autenticado,
		AuditarExito: func(r ResultadoPurga) bool { return r.Dispositivos > 0 },
		Logica: func(ctx context.Context, t *auditoria.Tx, _ struct{}) (ResultadoPurga, error) {
			q := notificacionesbd.New(t)
			ds, err := q.DispositivosSinCredencial(ctx, porLote)
			if err != nil {
				return ResultadoPurga{}, err
			}
			r := ResultadoPurga{}
			for _, d := range ds {
				if _, err := q.BorrarDispositivo(ctx, d.ID); err != nil && !esSinFilas(err) {
					return r, err
				}
				if err := emitirQuitado(t, d, QuitadoSesionCerrada); err != nil {
					return r, err
				}
				r.Dispositivos++
			}
			if r.Dispositivos > 0 {
				t.Anotar(map[string]any{"dispositivos_quitados": r.Dispositivos})
			}
			n, err := q.PurgarEnvios(ctx, m.ahora().Add(-m.cfg.RetencionEnvios))
			if err != nil {
				return r, err
			}
			r.Envios = n
			return r, nil
		},
	})
}
