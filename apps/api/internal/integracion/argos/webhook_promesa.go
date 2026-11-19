package argos

import (
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
)

// Webhooks de promesas (contrato 0.6.0 §7.4; desde 0.8.0 con motivo_reconexion, §7.3): Argos avisa
// que una promesa quedó activa, se cumplió, se incumplió o se canceló. Aquí solo se lee el payload
// firmado y se emite el evento interno argos.promesa_cambiada con lo mínimo: nunca el monto. Lo
// consume mensajería para la reconexión temporal de la conversación (tipo reconexion_temporal).

// Eventos de promesas (§2.3).
const (
	EventoPromesaCreada     = "promesa.creada"
	EventoPromesaCumplida   = "promesa.cumplida"
	EventoPromesaIncumplida = "promesa.incumplida"
	EventoPromesaCancelada  = "promesa.cancelada"
	// EventoPromesaCambiada es el evento interno (outbox) que consume mensajería.
	EventoPromesaCambiada = "argos.promesa_cambiada"
)

// Motivos de la reconexión temporal en Argos (§7.3): con cuál se pidió.
const (
	MotivoReconexionFueraDeHorario = "fuera_de_horario"
	MotivoReconexionSinPersonal    = "sin_personal"
)

// estadoDePromesa: el estado en que queda la promesa con cada webhook.
var estadoDePromesa = map[string]string{
	EventoPromesaCreada: "activa", EventoPromesaCumplida: "cumplida", EventoPromesaIncumplida: "incumplida", EventoPromesaCancelada: "cancelada",
}

// promesaCambiada es el payload común de promesa.* (§7.4).
type promesaCambiada struct {
	PromesaID        int64      `json:"promesa_id"`
	EmpresaID        *int64     `json:"empresa_id"`
	ClienteID        *int64     `json:"cliente_id"`
	ServicioID       *int64     `json:"servicio_id"`
	Tipo             string     `json:"tipo"`
	Canal            string     `json:"canal"`
	ComprobanteID    *int64     `json:"comprobante_id"`
	Vence            *time.Time `json:"vence"`
	MotivoReconexion *string    `json:"motivo_reconexion"`
	Motivo           *string    `json:"motivo"`
	estado           string
}

// promesaDe lee el payload del webhook; omitido dice por qué se ignora. motivo_reconexion que no
// viene (Argos < 0.8.0) o con un valor desconocido queda null; el motivo de cancelada, de una lista
// cerrada (lo demás es «otro»: nunca texto libre de Argos).
func promesaDe(e conector.Entrante) (*promesaCambiada, string) {
	var p promesaCambiada
	if err := conector.Decodificar(e.Payload, &p); err != nil || p.PromesaID <= 0 {
		return nil, "payload_invalido"
	}
	p.estado = estadoDePromesa[e.Evento]
	if p.Tipo != "promesa" && p.Tipo != "reconexion_temporal" {
		return nil, "tipo_desconocido"
	}
	p.MotivoReconexion = MotivoReconexionValido(p.MotivoReconexion)
	if p.Tipo != "reconexion_temporal" {
		p.MotivoReconexion = nil
	}
	if p.estado == "cancelada" {
		m := "otro"
		if p.Motivo != nil && (*p.Motivo == "comprobante_rechazado" || *p.Motivo == "vencida_sin_revisar") {
			m = *p.Motivo
		}
		p.Motivo = &m
	} else {
		p.Motivo = nil
	}
	if p.Canal != "portal" && p.Canal != "ia" && p.Canal != "operador" && p.Canal != "supervisor" {
		p.Canal = ""
	}
	return &p, ""
}

// MotivoReconexionValido: el motivo_reconexion de Argos si es uno de la lista cerrada; nil si no
// viene o es otro valor (Iris lo trata como null, §4.1).
func MotivoReconexionValido(m *string) *string {
	if m == nil || (*m != MotivoReconexionFueraDeHorario && *m != MotivoReconexionSinPersonal) {
		return nil
	}
	v := *m
	return &v
}

// emitirPromesa deja el evento interno, sin monto.
func emitirPromesa(t *auditoria.Tx, e conector.Entrante, p promesaCambiada) error {
	if p.EmpresaID != nil && *p.EmpresaID != 0 {
		t.FijarEmpresa(*p.EmpresaID)
	}
	var vence *string
	if p.Vence != nil {
		v := p.Vence.UTC().Format(time.RFC3339Nano)
		vence = &v
	}
	var canal *string
	if p.Canal != "" {
		canal = &p.Canal
	}
	return t.Emitir(EventoPromesaCambiada, map[string]any{
		"promesa_id": p.PromesaID, "estado": p.estado, "tipo": p.Tipo, "empresa_id": p.EmpresaID, "cliente_id": p.ClienteID,
		"servicio_id": p.ServicioID, "comprobante_id": p.ComprobanteID, "canal": canal, "vence": vence,
		"motivo_reconexion": p.MotivoReconexion, "motivo": p.Motivo, "entrante_id": e.ID,
	})
}
