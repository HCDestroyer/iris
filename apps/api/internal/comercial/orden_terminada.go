package comercial

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

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
//
// Orden reabierta (de cancelada o fallida a pendiente): Argos ya tiene el plan o el titular nuevo,
// así que el contrato se regenera en borrador para esa orden con la revisión vigente sin aceptar
// (con vigencia nueva si venció): la subida de precio vuelve a esperar la firma en sitio.
//
// Los dos leen el estado de la orden con candado compartido antes de tomar el prospecto (como
// RevisarVenta): no se cruzan con cancelar ni reabrir.

// ordenTerminal: estados de la orden sin firma en sitio posible.
func ordenTerminal(estado string) bool { return estado == "cancelada" || estado == "fallida" }

// EntradaOrdenTerminada: la orden que se canceló o falló.
type EntradaOrdenTerminada struct {
	OrdenID int64  `json:"orden_id"`
	Estado  string `json:"a"`
	// De: el estado anterior (orden.estado_cambiada); de cancelada o fallida a pendiente = reabierta.
	De string `json:"de,omitempty"`
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
			o, err := q.OrdenEstadoCompartido(ctx, in.OrdenID)
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

// ResultadoOrdenReabierta: el contrato regenerado de nuevo (nil si no hacía falta).
type ResultadoOrdenReabierta struct {
	ContratoID *int64 `json:"contrato_id"`
}

func (m *Modulo) nuevoOrdenReabierta() *auditoria.Caso[EntradaOrdenTerminada, ResultadoOrdenReabierta] {
	return auditoria.Nuevo(auditoria.Caso[EntradaOrdenTerminada, ResultadoOrdenReabierta]{
		Accion: "prospectos.revision_por_firmar", Acceso: auditoria.Autenticado,
		AuditarExito: func(r ResultadoOrdenReabierta) bool { return r.ContratoID != nil },
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaOrdenTerminada) (ResultadoOrdenReabierta, error) {
			r := ResultadoOrdenReabierta{}
			if err := exigirSistema(ctx); err != nil {
				return r, err
			}
			q := comercialbd.New(t)
			o, err := q.OrdenEstadoCompartido(ctx, in.OrdenID)
			if errors.Is(err, pgx.ErrNoRows) {
				return r, nil
			}
			if err != nil {
				return r, err
			}
			// Con el estado leído de la orden: sigue abierta (no cancelada, fallida ni cerrada).
			if ordenTerminal(o.Estado) || o.Estado == "cerrada" {
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
			// Ya hay uno por firmar (el evento llegó dos veces): nada.
			if _, err := q.ContratoPorFirmarEnOrden(ctx, p.ID); err == nil {
				return r, nil
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return r, err
			}
			rev, err := q.RevisionDeOrden(ctx, comercialbd.RevisionDeOrdenParams{ProspectoID: p.ID, OrdenID: in.OrdenID})
			if errors.Is(err, pgx.ErrNoRows) {
				return r, nil
			}
			if err != nil {
				return r, err
			}
			if !rev.VigenteHasta.After(m.ahora()) {
				antes := rev.VigenteHasta
				if rev, err = q.RenovarVigencia(ctx, comercialbd.RenovarVigenciaParams{
					VigenteHasta: m.ahora().Add(m.cfg.VigenciaCotizacion).UTC().Truncate(time.Microsecond), ID: rev.ID}); err != nil {
					return r, err
				}
				t.RegistrarCambios("cotizacion", rev.ID, map[string]any{"vigente_hasta": antes}, map[string]any{"vigente_hasta": rev.VigenteHasta})
			}
			var cliente clientePDF
			if err := json.Unmarshal(rev.Cliente, &cliente); err != nil {
				return r, err
			}
			t.FijarEmpresa(deref(p.EmpresaID))
			ct, err := m.regenerarContrato(ctx, t, q, p, rev, cliente, in.OrdenID)
			if err != nil {
				return r, err
			}
			r.ContratoID = &ct.ID
			return r, historial(ctx, t, q, p.ID, "contrato_regenerado", nil, nil, nil, map[string]any{
				"contrato_id": ct.ID, "cotizacion_id": rev.ID, "orden_id": in.OrdenID})
		},
	})
}

// OrdenReabierta regenera el contrato por firmar de una orden reabierta (actor sistema) y arma
// su PDF en borrador.
func (m *Modulo) OrdenReabierta(ctx context.Context, in EntradaOrdenTerminada) (ResultadoOrdenReabierta, error) {
	r, err := m.ordenReabierta.Ejecutar(ctx, in)
	if err != nil || r.ContratoID == nil {
		return r, err
	}
	if ct, err := m.VerContrato.Ejecutar(ctx, EntradaVerContrato{ID: *r.ContratoID}); err == nil {
		m.conPDFBorrador(ctx, ct)
	}
	return r, nil
}

// OrdenTerminada anula el contrato regenerado de una orden cancelada o fallida (actor sistema).
func (m *Modulo) OrdenTerminada(ctx context.Context, in EntradaOrdenTerminada) (ResultadoOrdenTerminada, error) {
	return m.ordenTerminada.Ejecutar(ctx, in)
}

// suscribirOrdenTerminada: orden.estado_cambiada a cancelada o fallida, o de vuelta a pendiente
// (reabierta) (cmd/worker, con
// SuscribirRevisionVenta).
func (m *Modulo) suscribirOrdenTerminada(bus *integracion.Bus, bd nucleo.BD, log *slog.Logger) {
	bus.Suscribir("comercial.orden_terminada", "orden.estado_cambiada", func(ctx context.Context, e integracion.Evento) error {
		var in EntradaOrdenTerminada
		if err := json.Unmarshal(e.Payload, &in); err != nil {
			log.ErrorContext(ctx, "comercial: orden.estado_cambiada ilegible", "evento", e.ID, "err", err)
			return nil
		}
		switch {
		case in.OrdenID <= 0:
			return nil
		case ordenTerminal(in.Estado):
			_, err := m.OrdenTerminada(nucleo.ConBD(ctx, bd), in)
			return err
		case in.Estado == "pendiente" && ordenTerminal(in.De):
			_, err := m.OrdenReabierta(nucleo.ConBD(ctx, bd), in)
			return err
		}
		return nil
	})
}
