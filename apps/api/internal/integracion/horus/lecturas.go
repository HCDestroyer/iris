package horus

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	argosbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/argos/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Lecturas de Horus (§1.1) para el ticket y la orden: salud del servicio del cliente y estado
// del router. Todavía sin ruta HTTP (no hay pantalla): son la base que usará el detalle. Como
// las de Argos, en dos fases sin transacción abierta durante la llamada (permiso y conector en
// un caso corto; la llamada; la fila de auditoría en otro). Permiso clientes.ver y empresa
// activa: el servicio o el router deben ser de la empresa activa según la instantánea de Argos
// (si no está en la instantánea, 404: no hay cómo saber de quién es).

// SaludServicio es el consumo y la salud del servicio de un cliente según Horus.
type SaludServicio struct {
	ServicioID    int64    `json:"servicioId" doc:"Id del servicio en Argos"`
	Estado        string   `json:"estado" doc:"Estado según Horus (en_linea, degradado, fuera_de_linea…)"`
	BajadaBps     *float64 `json:"bajadaBps" nullable:"true"`
	SubidaBps     *float64 `json:"subidaBps" nullable:"true"`
	LatenciaMs    *float64 `json:"latenciaMs" nullable:"true"`
	PerdidaPct    *float64 `json:"perdidaPct" nullable:"true"`
	ActualizadoEn *string  `json:"actualizadoEn" nullable:"true"`
}

// Router es el estado de un router según Horus.
type Router struct {
	RouterID int64   `json:"routerId" doc:"Id del router en Argos"`
	Nombre   string  `json:"nombre"`
	Estado   string  `json:"estado" doc:"caido | arriba (o el estado que informe Horus)"`
	Desde    *string `json:"desde" nullable:"true"`
}

// EntradaLecturaHorus es el id (de Argos) del servicio o el router.
type EntradaLecturaHorus struct {
	ID int64 `json:"id" minimum:"1"`
}

type entradaFinal[T any] struct {
	ID       int64 `json:"id"`
	valor    T
	errHorus error
}

// lectura es una lectura de Horus en dos fases.
type lectura[T any] struct {
	m      *Modulo
	previo *auditoria.Caso[EntradaLecturaHorus, conector.Leido]
	final  *auditoria.Caso[entradaFinal[T], T]
	leer   func(ctx context.Context, c *Cliente, id int64) (T, error)
}

// verificarEmpresa: con un usuario detrás, el recurso debe estar en la instantánea de Argos y
// ser de la empresa activa (mismo 404 que inexistente).
type verificarEmpresa func(ctx context.Context, q *argosbd.Queries, id int64) (empresa int64, err error)

func nuevaLectura[T any](m *Modulo, accion, entidad string, verificar verificarEmpresa, leer func(context.Context, *Cliente, int64) (T, error)) *lectura[T] {
	l := &lectura[T]{m: m, leer: leer}
	l.previo = auditoria.Nuevo(auditoria.Caso[EntradaLecturaHorus, conector.Leido]{
		Accion: accion, Permiso: nucleo.PermClientesVer, AuditarExito: soloLecturas[conector.Leido],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaLecturaHorus) (conector.Leido, error) {
			c, err := m.conexion.Activo(ctx, t)
			if err != nil {
				return c, err
			}
			if e := contexto.EmpresaDe(ctx); contexto.ActorDe(ctx).EsUsuario() {
				empresa, err := verificar(ctx, argosbd.New(t), in.ID)
				if errors.Is(err, pgx.ErrNoRows) || (err == nil && empresa != e) {
					return c, nucleo.NoEncontrado("No existe")
				}
				if err != nil {
					return c, err
				}
			}
			return c, nil
		},
	})
	l.final = auditoria.Nuevo(auditoria.Caso[entradaFinal[T], T]{
		Accion: accion, Permiso: nucleo.PermClientesVer,
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaFinal[T]) (T, error) {
			t.FijarEntidad(entidad, in.ID)
			if in.errHorus != nil {
				var cero T
				return cero, conector.Problema(in.errHorus)
			}
			return in.valor, nil
		},
	})
	return l
}

// Ejecutar lee de Horus.
func (l *lectura[T]) Ejecutar(ctx context.Context, in EntradaLecturaHorus) (T, error) {
	c, err := l.previo.Ejecutar(ctx, in)
	if err != nil {
		var cero T
		return cero, err
	}
	f := entradaFinal[T]{ID: in.ID}
	f.valor, f.errHorus = l.leer(ctx, l.m.clienteDe(c), in.ID)
	return l.final.Ejecutar(ctx, f)
}

// ConsultarSaludServicio lee de Horus el consumo y la salud de un servicio (id de Argos).
type ConsultarSaludServicio = lectura[SaludServicio]

// ConsultarRouter lee de Horus el estado de un router (id de Argos).
type ConsultarRouter = lectura[Router]

func nuevoConsultarSaludServicio(m *Modulo) *ConsultarSaludServicio {
	return nuevaLectura(m, "horus.salud_servicio", "servicio_argos",
		func(ctx context.Context, q *argosbd.Queries, id int64) (int64, error) {
			s, err := q.ServicioPorID(ctx, id)
			return s.EmpresaID, err
		},
		func(ctx context.Context, c *Cliente, id int64) (SaludServicio, error) {
			h, err := c.SaludServicio(ctx, id)
			if err != nil {
				return SaludServicio{}, err
			}
			s := SaludServicio{
				ServicioID: id, Estado: h.Estado, BajadaBps: h.BajadaBps.Float(), SubidaBps: h.SubidaBps.Float(),
				LatenciaMs: h.LatenciaMs.Float(), PerdidaPct: h.PerdidaPct.Float(),
			}
			if t, err := time.Parse(time.RFC3339Nano, h.ActualizadoEn); err == nil {
				v := t.UTC().Format(time.RFC3339)
				s.ActualizadoEn = &v
			}
			return s, nil
		})
}

func nuevoConsultarRouter(m *Modulo) *ConsultarRouter {
	return nuevaLectura(m, "horus.router", "router_argos",
		func(ctx context.Context, q *argosbd.Queries, id int64) (int64, error) {
			r, err := q.RouterPorID(ctx, id)
			return r.EmpresaID, err
		},
		func(ctx context.Context, c *Cliente, id int64) (Router, error) {
			h, err := c.Router(ctx, id)
			if err != nil {
				return Router{}, err
			}
			r := Router{RouterID: id, Nombre: h.Nombre, Estado: h.Estado}
			if e := estadoDe(h.Estado); e != "" {
				r.Estado = e
			}
			if h.Desde != "" {
				r.Desde = &h.Desde
			}
			return r, nil
		})
}
