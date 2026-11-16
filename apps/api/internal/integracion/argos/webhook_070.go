package argos

import (
	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
)

// Webhooks de comprobantes (contrato 0.6.0 §7.2; sin cambios de forma en 0.7.0): Argos avisa que un
// comprobante se aprobó o se rechazó, lo haya decidido caja en su panel o una persona desde Iris
// (§7.7). Aquí el webhook es solo un AVISO: se emite el evento interno argos.comprobante_resuelto
// con lo mínimo (qué comprobante, de qué empresa y qué dice Argos que pasó). Mensajería marca la
// boleta de ese comprobante y relee GET /comprobantes/:id antes de aplicar nada: el estado, el
// monto, el motivo, la causa y quién decidió salen de esa lectura, nunca de este payload.

// Eventos de comprobantes (§2.3).
const (
	EventoComprobanteAprobado  = "comprobante.aprobado"
	EventoComprobanteRechazado = "comprobante.rechazado"
	// EventoComprobanteResuelto es el evento interno (outbox) que consume mensajería.
	EventoComprobanteResuelto = "argos.comprobante_resuelto"
)

// comprobanteResuelto es el payload de comprobante.aprobado y comprobante.rechazado (§7.2).
type comprobanteResuelto struct {
	ComprobanteID  int64   `json:"comprobante_id"`
	EmpresaID      *int64  `json:"empresa_id"`
	ClienteID      *int64  `json:"cliente_id"`
	ServicioID     *int64  `json:"servicio_id"`
	Canal          string  `json:"canal"`
	PagoID         *int64  `json:"pago_id"`
	ServicioEstado *string `json:"servicio_estado"`
	estado         string
}

// comprobanteDe lee el payload del webhook; omitido dice por qué se ignora.
func comprobanteDe(e conector.Entrante) (*comprobanteResuelto, string) {
	var c comprobanteResuelto
	if err := conector.Decodificar(e.Payload, &c); err != nil || c.ComprobanteID <= 0 {
		return nil, "payload_invalido"
	}
	c.estado = ComprobanteAprobado
	if e.Evento == EventoComprobanteRechazado {
		c.estado = ComprobanteRechazado
	}
	return &c, ""
}

// emitirComprobante deja el evento interno. Sin monto, motivo ni causa: vienen sin verificar, nadie
// los usa y el motivo es texto libre de caja (lo que se le dice al cliente sale de releer Argos).
func emitirComprobante(t *auditoria.Tx, e conector.Entrante, c comprobanteResuelto) error {
	if c.EmpresaID != nil && *c.EmpresaID != 0 {
		t.FijarEmpresa(*c.EmpresaID)
	}
	return t.Emitir(EventoComprobanteResuelto, map[string]any{
		"comprobante_id": c.ComprobanteID, "estado": c.estado, "empresa_id": c.EmpresaID, "cliente_id": c.ClienteID, "servicio_id": c.ServicioID,
		"canal": c.Canal, "pago_id": c.PagoID, "servicio_estado": c.ServicioEstado, "entrante_id": e.ID,
	})
}
