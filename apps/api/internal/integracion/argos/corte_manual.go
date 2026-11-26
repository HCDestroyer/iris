package argos

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	argosbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/argos/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	conectorbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/conector/bd"
)

// Corte manual de respaldo (contrato argosisp.md 0.15.0 §13.2, A54 / B34). Cuando SmartOLT no puede
// cortar o reconectar el cable (CATV), Argos avisa a Iris con servicio.corte_manual_solicitado y
// servicio.corte_manual_cancelado. El receptor ya los guardó en integracion.entrantes (deduplicados
// por X-Webhook-Id); aquí se valida el payload y se emite el evento interno argos.corte_manual, que
// campo aplica (orden nueva, cancelación, deduplicación por solicitud_id y version, B3 y B4).
//
// B9: con la empresa fuera del alcance del conector o un servicio que Iris no conoce (ni Argos lo
// confirma de esa empresa), el evento sale con `descarte` y campo guarda la solicitud sin abrir nada.
// El webhook ya se respondió 2xx al recibirlo.

// Webhooks de Argos del corte manual (§2.3).
const (
	EventoCorteManualSolicitado = "servicio.corte_manual_solicitado"
	EventoCorteManualCancelado  = "servicio.corte_manual_cancelado"
)

// EventoInternoCorteManual: el corte manual validado, para campo (contratos/eventos.md).
const EventoInternoCorteManual = "argos.corte_manual"

// ContratoCorteManual: el bloque B del contrato 0.15.0.
const ContratoCorteManual = ContratoTipoServicio

// Acciones y causas del corte manual (§13.2).
const (
	CorteAccionCorte      = "corte"
	CorteAccionReconexion = "reconexion"
)

// Descartes de una solicitud que no abre nada (B9).
const (
	DescarteFueraDeAlcance      = "fuera_de_alcance"
	DescarteServicioDesconocido = "servicio_desconocido"
)

// Lista cerrada de causas (§13.2). Una causa que Argos agregue después se guarda como «otra».
var causasCorte = map[string]bool{"sin_onu": true, "instancia_no_operativa": true, "smartolt_fallo": true, "catv_no_soportado": true}

var reCodigoCorte = regexp.MustCompile(`^[a-z][a-z0-9_]{0,19}$`)

// SoportaCorteManual: el contrato incluye el corte manual (0.15.0).
func SoportaCorteManual(contrato *string) bool {
	return conector.VersionAlMenos(contrato, ContratoCorteManual)
}

type payloadCorte struct {
	SolicitudID  int64      `json:"solicitud_id"`
	Version      int64      `json:"version"`
	SolicitadaEn *time.Time `json:"solicitada_en"`
	ServicioID   int64      `json:"servicio_id"`
	EmpresaID    *int64     `json:"empresa_id"`
	ClienteID    *int64     `json:"cliente_id"`
	Componente   string     `json:"componente"`
	Accion       string     `json:"accion"`
	Causa        string     `json:"causa"`
}

// eventoCorte es el corte validado (solo ids y códigos: sin datos personales).
type eventoCorte struct {
	empresa int64
	payload map[string]any
}

// leerCorte valida un servicio.corte_manual_*: omitido ≠ "" = se ignora (payload inválido); un error
// de Argos (al confirmar un servicio que Iris no conoce) reintenta el job.
func (p *ProcesarEntrante) leerCorte(ctx context.Context, bd conector.DBTX, e conector.Entrante) (*eventoCorte, string, error) {
	var c payloadCorte
	if err := conector.Decodificar(e.Payload, &c); err != nil || c.SolicitudID <= 0 || c.Version <= 0 || c.Version > 1<<31-1 ||
		c.ServicioID <= 0 || c.EmpresaID == nil || *c.EmpresaID <= 0 || (c.Accion != CorteAccionCorte && c.Accion != CorteAccionReconexion) {
		return nil, "payload_invalido", nil
	}
	componente := strings.TrimSpace(c.Componente)
	if componente == "" {
		componente = TipoCable
	}
	if !reCodigoCorte.MatchString(componente) {
		return nil, "payload_invalido", nil
	}
	causa := strings.TrimSpace(c.Causa)
	if !causasCorte[causa] {
		causa = "otra"
	}
	evento := "solicitado"
	if e.Evento == EventoCorteManualCancelado {
		evento = "cancelado"
	}
	payload := map[string]any{
		"evento": evento, "solicitud_id": c.SolicitudID, "version": c.Version, "solicitada_en": c.SolicitadaEn,
		"servicio_id": c.ServicioID, "empresa_id": *c.EmpresaID, "cliente_id": c.ClienteID, "componente": componente,
		"accion": c.Accion, "causa": causa, "conector_id": e.ConectorID, "entrante_id": e.ID, "descarte": nil,
	}
	con, err := conectorbd.New(bd).ConectorPorID(ctx, e.ConectorID)
	if err != nil {
		return nil, "", err
	}
	switch {
	case !empresaEnAlcance(con, *c.EmpresaID):
		payload["descarte"] = DescarteFueraDeAlcance
	case evento == "solicitado":
		// Solo el solicitado necesita el servicio (una cancelación de algo desconocido solo se guarda).
		var cli *Cliente
		lecturas := 0
		ok, err := p.servicioDeEmpresa(ctx, argosbd.New(bd), &cli, e.ConectorID, c.ServicioID, *c.EmpresaID, bd, &lecturas)
		if err != nil {
			return nil, "", err
		}
		if !ok {
			payload["descarte"] = DescarteServicioDesconocido
		}
	}
	return &eventoCorte{empresa: *c.EmpresaID, payload: payload}, "", nil
}

// emitirCorte deja argos.corte_manual en el outbox (campo lo aplica en su transacción).
func emitirCorte(t *auditoria.Tx, c eventoCorte) error {
	t.FijarEmpresa(c.empresa)
	t.Anotar(map[string]any{"corte_manual": c.payload["evento"], "solicitud_id": c.payload["solicitud_id"], "descarte": c.payload["descarte"]})
	return t.Emitir(EventoInternoCorteManual, c.payload)
}
