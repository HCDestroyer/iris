package comercial

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	comercialbd "github.com/HCDestroyer/iris/apps/api/internal/comercial/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Orden de instalación cancelada o fallida (B20, revisión de #280): el contrato regenerado en
// borrador ya no se puede firmar en sitio, así que se anula. La revisión de la cotización queda sin
// aceptar y el contrato firmado de la venta sigue vivo. Se dispara con orden.estado_cambiada
// (a = cancelada o fallida); idempotente.

// ordenTerminal: estados de la orden sin firma en sitio posible.
func ordenTerminal(estado string) bool { return estado == "cancelada" || estado == "fallida" }

// EntradaOrdenTerminada: la orden que se canceló o falló.
type EntradaOrdenTerminada struct {
	OrdenID int64  `json:"orden_id"`
	Estado  string `json:"a"`
}

// ResultadoOrdenTerminada: el contrato regenerado anulado (nil si no había).
type ResultadoOrdenTerminada struct {
	ContratoID *int64 `json:"contrato_id"`
}

func (m *Modulo) nuevoOrdenTerminada() *auditoria.Caso[EntradaOrdenTerminada, ResultadoOrdenTerminada] {
	return auditoria.Nuevo(auditoria.Caso[EntradaOrdenTerminada, ResultadoOrdenTerminada]{
		Accion: "prospectos.revision_sin_firma", Acceso: auditoria.Autenticado,
		AuditarExito: func(r ResultadoOrdenTerminada) bool { return r.ContratoID != nil },
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaOrdenTerminada) (ResultadoOrdenTerminada, error) {
			r := ResultadoOrdenTerminada{}
			if err := exigirSistema(ctx); err != nil {
				return r, err
			}
			q := comercialbd.New(t)
			o, err := q.OrdenParaFirmarContrato(ctx, in.OrdenID)
			if errors.Is(err, pgx.ErrNoRows) {
				return r, nil
			}
			if err != nil {
				return r, err
			}
			// Con el estado leído de la orden, no el del evento (pudo reabrirse después).
			if !ordenTerminal(o.Estado) {
				return r, nil
			}
			id, err := q.ProspectoGanadoDeOrden(ctx, &in.OrdenID)
			if errors.Is(err, pgx.ErrNoRows) {
				return r, nil
			}
			if err != nil {
				return r, err
			}
			p, err := tomar(ctx, t, q, id, nucleo.PermProspectosGanar, nil)
			if err != nil {
				return r, err
			}
			c, err := q.ContratoPorFirmarEnOrden(ctx, p.ID)
			if errors.Is(err, pgx.ErrNoRows) {
				return r, nil
			}
			if err != nil {
				return r, err
			}
			if orden := ordenDeContrato(c); orden == nil || *orden != in.OrdenID {
				return r, nil
			}
			t.FijarEntidad("contrato", c.ID)
			t.FijarEmpresa(deref(p.EmpresaID))
			motivo := "La orden " + o.Numero + " quedó " + o.Estado + ": el contrato regenerado ya no se firma en sitio"
			if err := anular(ctx, t, q, c, p.ID, motivo); err != nil {
				return r, err
			}
			r.ContratoID = &c.ID
			return r, historial(ctx, t, q, p.ID, "contrato_anulado", nil, nil, &motivo, map[string]any{"contrato_id": c.ID, "orden_id": in.OrdenID})
		},
	})
}

// OrdenTerminada anula el contrato regenerado de una orden cancelada o fallida (actor sistema).
func (m *Modulo) OrdenTerminada(ctx context.Context, in EntradaOrdenTerminada) (ResultadoOrdenTerminada, error) {
	return m.ordenTerminada.Ejecutar(ctx, in)
}

// suscribirOrdenTerminada: orden.estado_cambiada a cancelada o fallida (cmd/worker, con
// SuscribirRevisionVenta).
func (m *Modulo) suscribirOrdenTerminada(bus *integracion.Bus, bd nucleo.BD, log *slog.Logger) {
	bus.Suscribir("comercial.orden_terminada", "orden.estado_cambiada", func(ctx context.Context, e integracion.Evento) error {
		var in EntradaOrdenTerminada
		if err := json.Unmarshal(e.Payload, &in); err != nil {
			log.ErrorContext(ctx, "comercial: orden.estado_cambiada ilegible", "evento", e.ID, "err", err)
			return nil
		}
		if in.OrdenID <= 0 || !ordenTerminal(in.Estado) {
			return nil
		}
		_, err := m.OrdenTerminada(nucleo.ConBD(ctx, bd), in)
		return err
	})
}
