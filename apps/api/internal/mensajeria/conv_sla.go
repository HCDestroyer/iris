package mensajeria

import (
	"context"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
)

// nuevoVencerSLA marca las conversaciones abiertas con el SLA vencido (una vez por espera) y emite
// conversacion.sla_vencido; el flujo lo ve como conversacion_actualizada. Lo corre el mantenimiento.
func nuevoVencerSLA(m *Modulo) *auditoria.Caso[struct{}, int] {
	return auditoria.Nuevo(auditoria.Caso[struct{}, int]{
		Accion: "conversaciones.vencer_sla", Acceso: auditoria.Autenticado, AuditarExito: func(n int) bool { return n > 0 },
		Logica: func(ctx context.Context, t *auditoria.Tx, _ struct{}) (int, error) {
			q := mensajeriabd.New(t)
			ids, err := q.SLAVencidosSinAvisar(ctx, m.ahora())
			if err != nil {
				return 0, err
			}
			n := 0
			for _, id := range ids {
				c, err := q.ConversacionClienteParaActualizar(ctx, id)
				if err != nil {
					return n, err
				}
				if c.SlaAvisado || c.SlaVenceEn == nil || c.SlaVenceEn.After(m.ahora()) {
					continue
				}
				c.SlaAvisado = true
				if c, err = guardarConv(ctx, q, c, false); err != nil {
					return n, err
				}
				if err := m.emitirEventoConv(ctx, t, c, EventoConvActualizada, nil, nil); err != nil {
					return n, err
				}
				if err := t.EmitirEnEmpresa("conversacion.sla_vencido", c.EmpresaID, map[string]any{"conversacion_id": c.ID, "empresa_id": c.EmpresaID,
					"cola_id": c.ColaID, "asignado_a": c.AsignadoA, "vence_en": c.SlaVenceEn, "espera_desde": c.EsperaDesde}); err != nil {
					return n, err
				}
				n++
			}
			t.Anotar(map[string]any{"vencidas": n})
			return n, nil
		},
	})
}
