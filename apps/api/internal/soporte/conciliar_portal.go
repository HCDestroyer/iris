package soporte

import (
	"context"
	"slices"
	"time"

	"github.com/riverqueue/river"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	soportebd "github.com/HCDestroyer/iris/apps/api/internal/soporte/bd"
)

// Conciliación de los tickets del portal de Argos creados sin poder confirmar el cliente (contrato
// 0.9.0 §11.2): cuando Argos vuelve, Iris lo lee; si el cliente era de la empresa, el ticket queda
// normal; si no existe o es de otra empresa, queda marcado para revisión de una persona
// (cliente_revision = no_coincide) con una nota interna. Si Argos sigue sin responder, se reintenta
// en la corrida siguiente.

// ConciliadorClientes concilia un lote de tickets pendientes (job soporte.conciliar_clientes).
type ConciliadorClientes struct {
	m        *Modulo
	resolver *auditoria.Caso[entradaConciliar, bool]
}

type entradaConciliar struct {
	TicketID  int64 `json:"ticket_id"`
	ClienteID int64 `json:"cliente_id"`
	Coincide  bool  `json:"coincide"`
}

func (m *Modulo) nuevoConciliarClientes() *ConciliadorClientes {
	c := &ConciliadorClientes{m: m}
	c.resolver = auditoria.Nuevo(auditoria.Caso[entradaConciliar, bool]{
		Accion: "portal_argos.conciliar_cliente", Acceso: auditoria.Autenticado,
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaConciliar) (bool, error) {
			t.FijarEntidad("ticket", in.TicketID)
			q := soportebd.New(t)
			var revision *string
			if !in.Coincide {
				revision = ptr("no_coincide")
			}
			n, err := q.ResolverClienteRevision(ctx, soportebd.ResolverClienteRevisionParams{ID: in.TicketID, ClienteRevision: revision})
			if err != nil || n == 0 || in.Coincide {
				return n > 0, err
			}
			f, err := q.TicketPorID(ctx, in.TicketID)
			if err != nil {
				return false, err
			}
			t.FijarEmpresa(deref(f.EmpresaID))
			_, err = mensajeSistema(ctx, q, f, false, "Revisar: Argos dice que el cliente de este ticket (abierto desde el portal de Argos "+
				"mientras Argos no respondía) no es de esta empresa o no existe.")
			return true, err
		},
	})
	return c
}

// Ejecutar concilia hasta limite tickets. Devuelve cuántos resolvió.
func (c *ConciliadorClientes) Ejecutar(ctx context.Context, limite int32) (int, error) {
	if c.m.cfg.PortalArgos == nil {
		return 0, nil
	}
	bd := nucleo.BDDe(ctx)
	if bd == nil {
		return 0, nucleo.ErrSinBD
	}
	filas, err := soportebd.New(bd).TicketsClientePorConciliar(ctx, limite)
	if err != nil {
		return 0, err
	}
	hechos := 0
	for _, f := range filas {
		v, err := c.m.cfg.PortalArgos.ValidarCliente(ctx, *f.ClienteID)
		if err != nil {
			return hechos, err
		}
		if !v.Validado {
			continue // Argos sigue sin responder: la próxima corrida
		}
		coincide := v.Existe && (v.SinServicios || slices.Contains(v.Empresas, deref(f.EmpresaID)))
		if ok, err := c.resolver.Ejecutar(ctx, entradaConciliar{TicketID: f.ID, ClienteID: *f.ClienteID, Coincide: coincide}); err != nil {
			return hechos, err
		} else if ok {
			hechos++
		}
	}
	return hechos, nil
}

// ArgsConciliarClientes es el job periódico de la conciliación.
type ArgsConciliarClientes struct{}

// Kind implementa river.JobArgs.
func (ArgsConciliarClientes) Kind() string { return "soporte.conciliar_clientes_portal" }

// InsertOpts: único por periodo.
func (ArgsConciliarClientes) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 3, UniqueOpts: river.UniqueOpts{ByPeriod: 5 * time.Minute}}
}

// TrabajadorConciliar corre la conciliación como actor sistema (canal cron).
type TrabajadorConciliar struct {
	river.WorkerDefaults[ArgsConciliarClientes]
	BD     nucleo.BD
	Modulo *Modulo
}

// Work implementa river.Worker.
func (w *TrabajadorConciliar) Work(ctx context.Context, _ *river.Job[ArgsConciliarClientes]) error {
	ctx = nucleo.ConBD(contexto.Sistema(ctx, contexto.CanalCron), w.BD)
	_, err := w.Modulo.ConciliarClientes.Ejecutar(ctx, 50)
	return err
}

// TrabajoPeriodicoConciliar agenda la conciliación cada 5 minutos.
func TrabajoPeriodicoConciliar() *river.PeriodicJob {
	return river.NewPeriodicJob(river.PeriodicInterval(5*time.Minute),
		func() (river.JobArgs, *river.InsertOpts) { return ArgsConciliarClientes{}, nil }, nil)
}
