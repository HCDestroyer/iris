package soporte

import (
	"context"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	soportebd "github.com/HCDestroyer/iris/apps/api/internal/soporte/bd"
)

// EntradaRevisarSLA es la entrada del cron de SLA.
type EntradaRevisarSLA struct {
	// Ahora: hasta cuándo revisar (cero = la hora del módulo).
	Ahora time.Time `json:"ahora"`
	// Lote: alertas por transacción (200 por defecto). Se repite hasta agotar lo pendiente.
	Lote int `json:"lote"`
}

// LoteSLAPorDefecto: alertas por transacción del cron.
const LoteSLAPorDefecto = 200

// ResultadoRevisarSLA cuenta las alertas emitidas en la corrida.
type ResultadoRevisarSLA struct {
	EnRiesgo int `json:"en_riesgo"`
	Vencidos int `json:"vencidos"`
	// mas: el lote salió lleno (puede quedar algo pendiente).
	mas bool
}

// Emitidas es el total de alertas de la corrida.
func (r ResultadoRevisarSLA) Emitidas() int { return r.EnRiesgo + r.Vencidos }

// RevisorSLA es el cron de SLA: procesa todo lo pendiente hasta ahora en lotes, cada uno en su
// propia transacción (caso de uso soporte.revisar_sla), y emite ticket.sla_en_riesgo (80 %) y
// ticket.sla_vencido una sola vez por ticket, reloj, umbral y vencimiento
// (soporte.ticket_sla_alertas; un plazo nuevo vuelve a alertar). Una corrida concurrente o
// repetida no duplica: la alerta se reclama con INSERT … ON CONFLICT DO NOTHING en la misma
// transacción que el evento. Si un lote falla, los anteriores ya quedaron confirmados y la
// siguiente corrida sigue desde ahí.
type RevisorSLA struct {
	lote *auditoria.Caso[EntradaRevisarSLA, ResultadoRevisarSLA]
	m    *Modulo
}

// Ejecutar corre lotes hasta que uno sale incompleto. Ahora se fija al empezar, así todos los
// lotes revisan hasta el mismo instante.
func (r *RevisorSLA) Ejecutar(ctx context.Context, in EntradaRevisarSLA) (ResultadoRevisarSLA, error) {
	if in.Ahora.IsZero() {
		in.Ahora = r.m.ahora()
	}
	var total ResultadoRevisarSLA
	for {
		res, err := r.lote.Ejecutar(ctx, in)
		total.EnRiesgo += res.EnRiesgo
		total.Vencidos += res.Vencidos
		if err != nil || !res.mas {
			return total, err
		}
	}
}

func (m *Modulo) nuevoRevisarSLA() *RevisorSLA {
	lote := auditoria.Nuevo(auditoria.Caso[EntradaRevisarSLA, ResultadoRevisarSLA]{
		Accion: "soporte.revisar_sla",
		Acceso: auditoria.Autenticado, // lo corre el actor sistema (cron)
		// Solo deja fila de auditoría cuando emitió algo.
		AuditarExito: func(r ResultadoRevisarSLA) bool { return r.Emitidas() > 0 },
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaRevisarSLA) (ResultadoRevisarSLA, error) {
			if t.Actor().Tipo != contexto.ActorSistema {
				return ResultadoRevisarSLA{}, nucleo.Prohibido("Solo el sistema revisa el SLA")
			}
			q := soportebd.New(t)
			lote := in.Lote
			if lote <= 0 {
				lote = LoteSLAPorDefecto
			}
			filas, err := q.AlertasSLAPendientes(ctx, soportebd.AlertasSLAPendientesParams{
				Ahora: in.Ahora, Limite: int32(lote), //nolint:gosec // lote acotado por quien llama
			})
			if err != nil {
				return ResultadoRevisarSLA{}, err
			}
			res := ResultadoRevisarSLA{mas: len(filas) == lote}
			for _, a := range filas {
				emitida, err := emitirAlerta(ctx, t, q, a)
				if err != nil {
					return ResultadoRevisarSLA{}, err
				}
				switch {
				case !emitida:
				case a.Umbral == "vencido":
					res.Vencidos++
				default:
					res.EnRiesgo++
				}
			}
			t.FijarEmpresa(0)
			if res.Emitidas() > 0 {
				t.Anotar(map[string]any{"en_riesgo": res.EnRiesgo, "vencidos": res.Vencidos, "hasta": in.Ahora, "lote": lote})
			}
			return res, nil
		},
	})
	return &RevisorSLA{lote: lote, m: m}
}

// emitirAlerta reclama la alerta y, si nadie la había emitido, deja el historial y el evento.
func emitirAlerta(ctx context.Context, t *auditoria.Tx, q *soportebd.Queries, a soportebd.AlertasSLAPendientesRow) (bool, error) {
	n, err := q.ReclamarAlertaSLA(ctx, soportebd.ReclamarAlertaSLAParams{
		TicketID: a.TicketID, Sla: a.Sla, Umbral: a.Umbral, VenceEn: a.VenceEn,
	})
	if err != nil || n == 0 {
		return false, err
	}
	tipo := "sla_" + a.Umbral // sla_en_riesgo | sla_vencido
	f := soportebd.SoporteTicket{ID: a.TicketID, EmpresaID: a.EmpresaID}
	if err := historial(ctx, t, q, f, tipo, nil, nil, nil, map[string]any{"sla": a.Sla, "vence_en": a.VenceEn}); err != nil {
		return false, err
	}
	t.FijarEmpresa(deref(a.EmpresaID))
	if err := t.Emitir("ticket."+tipo, map[string]any{"ticket_id": a.TicketID, "sla": a.Sla, "vence_en": a.VenceEn}); err != nil {
		return false, err
	}
	return true, nil
}
