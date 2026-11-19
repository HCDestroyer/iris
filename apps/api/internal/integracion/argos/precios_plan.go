package argos

import (
	"context"
	"errors"
	"time"

	"github.com/riverqueue/river"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion"
	argosbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/argos/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Precio del plan (contrato 0.8.0 §2.3 y §2.4): catalogo.cambiado de un plan con `precio` en
// campos[] encola este job; relee GET /servicios?plan_id= (paginado) y refresca precio_mensual y
// moneda de los servicios de ese plan que Iris ya tiene (no crea instantáneas nuevas).

// ArgsPreciosPlan relee los precios de los servicios de un plan.
type ArgsPreciosPlan struct {
	ConectorID int64 `json:"conector_id"`
	PlanID     int64 `json:"plan_id"`
}

// Kind implementa river.JobArgs.
func (ArgsPreciosPlan) Kind() string { return "integracion.argos_precios_plan" }

// InsertOpts: único por (conector, plan) mientras esté pendiente o corriendo: una ráfaga de avisos
// del mismo plan relee la lista una sola vez.
func (ArgsPreciosPlan) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: integracion.ColaIntegracion, MaxAttempts: 8, UniqueOpts: river.UniqueOpts{ByArgs: true}}
}

// ResultadoPreciosPlan dice cuántos servicios cambiaron.
type ResultadoPreciosPlan struct {
	Servicios  int `json:"servicios"`
	Cambiados  int `json:"cambiados"`
	Invalidos  int `json:"invalidos"`
	Releidos   int `json:"releidos"`
	SinLocales bool `json:"sin_locales"`
}

type entradaPrecios struct {
	ArgsPreciosPlan
	precios   []precioServicio
	invalidos int
	releidos  int
}

// RefrescarPreciosPlan es el caso de uso del job.
type RefrescarPreciosPlan struct {
	m       *Modulo
	guardar *auditoria.Caso[entradaPrecios, ResultadoPreciosPlan]
}

func nuevoRefrescarPreciosPlan(m *Modulo) *RefrescarPreciosPlan {
	r := &RefrescarPreciosPlan{m: m}
	r.guardar = auditoria.Nuevo(auditoria.Caso[entradaPrecios, ResultadoPreciosPlan]{
		Accion: "integraciones.refrescar_precios_plan", Acceso: auditoria.Autenticado,
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaPrecios) (ResultadoPreciosPlan, error) {
			t.FijarEmpresa(0)
			t.FijarEntidad("plan_argos", in.PlanID)
			q := argosbd.New(t)
			res := ResultadoPreciosPlan{Servicios: len(in.precios), Invalidos: in.invalidos, Releidos: in.releidos}
			for _, pr := range in.precios {
				n, err := q.CambiarPrecioServicio(ctx, argosbd.CambiarPrecioServicioParams{ID: pr.id, PlanID: in.PlanID, PrecioMensual: pr.precio, Moneda: pr.moneda})
				if err != nil {
					return res, err
				}
				res.Cambiados += int(n)
			}
			t.Anotar(map[string]any{"servicios": res.Servicios, "cambiados": res.Cambiados, "invalidos": res.Invalidos})
			return res, nil
		},
	})
	return r
}

// Ejecutar relee la lista en Argos (sin transacción abierta) y guarda en una transacción corta.
func (r *RefrescarPreciosPlan) Ejecutar(ctx context.Context, args ArgsPreciosPlan) (ResultadoPreciosPlan, error) {
	bd := nucleo.BDDe(ctx)
	if bd == nil {
		return ResultadoPreciosPlan{}, nucleo.ErrSinBD
	}
	locales, err := argosbd.New(bd).ServiciosDePlan(ctx, args.PlanID)
	if err != nil {
		return ResultadoPreciosPlan{}, err
	}
	if len(locales) == 0 {
		return ResultadoPreciosPlan{SinLocales: true}, nil
	}
	conocidos := make(map[int64]bool, len(locales))
	for _, l := range locales {
		conocidos[l.ID] = true
	}
	c, err := r.m.conexion.PorID(ctx, bd, args.ConectorID)
	if err != nil {
		return ResultadoPreciosPlan{}, err
	}
	leidos, err := r.m.clienteDe(c).ServiciosDePlan(ctx, args.PlanID)
	if err != nil {
		return ResultadoPreciosPlan{}, err
	}
	in := entradaPrecios{ArgsPreciosPlan: args, precios: []precioServicio{}, releidos: len(leidos)}
	for _, s := range leidos {
		if !conocidos[s.ID] {
			continue
		}
		v := AServicio(s, nil)
		if v.PrecioMensual == nil && s.PrecioMensual != "" {
			// Precio inválido (NaN, infinito, negativo o fuera de numeric(12,2)): queda sin precio.
			in.invalidos++
		}
		in.precios = append(in.precios, precioServicio{id: s.ID, precio: v.PrecioMensual, moneda: v.Moneda})
	}
	return r.guardar.Ejecutar(ctx, in)
}

type trabajadorPreciosPlan struct {
	river.WorkerDefaults[ArgsPreciosPlan]
	m  *Modulo
	bd nucleo.BD
}

func (w *trabajadorPreciosPlan) Work(ctx context.Context, job *river.Job[ArgsPreciosPlan]) error {
	ctx = nucleo.ConBD(contexto.Sistema(ctx, contexto.CanalWorker), w.bd)
	_, err := w.m.RefrescarPreciosPlan.Ejecutar(ctx, job.Args)
	if errors.Is(err, conector.ErrListaDemasiadoLarga) {
		// Definitivo: reintentar lee las mismas páginas otra vez. Los precios se refrescan al abrir
		// cada ficha (más de 24 h) o con servicio.config_cambiada.
		return river.JobCancel(err)
	}
	return err
}

func (w *trabajadorPreciosPlan) Timeout(*river.Job[ArgsPreciosPlan]) time.Duration {
	return 5 * time.Minute
}
