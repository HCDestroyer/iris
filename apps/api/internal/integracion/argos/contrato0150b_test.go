package argos_test

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
)

// Contrato argosisp.md 0.15.0 §13.2 (bloque B, B34): los webhooks servicio.corte_manual_* se guardan
// en integracion.entrantes y, validados, salen como argos.corte_manual para campo. B9: con la empresa
// fuera del alcance o el servicio desconocido, el evento lleva `descarte` (campo no abre nada).

func (e *entorno) corteEmitido(entrante int64) map[string]any {
	e.t.Helper()
	var b []byte
	if err := e.pool.QueryRow(context.Background(), `SELECT payload FROM integracion.outbox WHERE evento = 'argos.corte_manual'
		AND payload->>'entrante_id' = $1`, strconv.FormatInt(entrante, 10)).Scan(&b); err != nil {
		e.t.Fatalf("sin argos.corte_manual del entrante %d: %v", entrante, err)
	}
	var p map[string]any
	_ = json.Unmarshal(b, &p)
	return p
}

func TestCorteManualWebhooks(t *testing.T) {
	e := nuevoEntorno(t)
	_, id, secreto := e.conectado()
	e.sembrar080(950) // servicios 9500 y 9501 de la empresa 1
	e.usuario("soporte").pedir("GET", "/api/v1/clientes/950", nil)
	base := func(solicitud int64, servicio, empresa int64) map[string]any {
		return map[string]any{"solicitud_id": solicitud, "version": 1, "solicitada_en": "2026-11-26T15:00:00Z", "servicio_id": servicio,
			"empresa_id": empresa, "cliente_id": 950, "componente": "cable", "accion": "corte", "causa": "sin_onu",
			// Lo que no está en el contrato no viaja.
			"nombre": "María Pérez", "telefono": "5555-0101"}
	}

	r := e.procesarWebhook(id, secreto, argos.EventoCorteManualSolicitado, base(7001, 9500, 1))
	if r.Resultado != "aplicado" {
		t.Fatalf("solicitado: %+v", r)
	}
	p := e.corteEmitido(r.EntranteID)
	if p["evento"] != "solicitado" || p["solicitud_id"] != float64(7001) || p["accion"] != "corte" || p["causa"] != "sin_onu" ||
		p["descarte"] != nil || p["conector_id"] != float64(id) {
		t.Fatalf("argos.corte_manual: %v", p)
	}
	if _, ok := p["nombre"]; ok {
		t.Fatal("sin datos personales")
	}

	// B9: empresa fuera del alcance del conector.
	r = e.procesarWebhook(id, secreto, argos.EventoCorteManualSolicitado, base(7002, 9500, 99))
	if p := e.corteEmitido(r.EntranteID); p["descarte"] != argos.DescarteFueraDeAlcance {
		t.Fatalf("fuera del alcance: %v", p)
	}
	// B9: servicio que ni Iris ni Argos conocen (Argos responde 404).
	r = e.procesarWebhook(id, secreto, argos.EventoCorteManualSolicitado, base(7003, 123456, 1))
	if p := e.corteEmitido(r.EntranteID); p["descarte"] != argos.DescarteServicioDesconocido {
		t.Fatalf("servicio desconocido: %v", p)
	}
	// Cancelado: sale sin consultar el servicio.
	c := base(7001, 9500, 1)
	c["version"] = 2
	r = e.procesarWebhook(id, secreto, argos.EventoCorteManualCancelado, c)
	if p := e.corteEmitido(r.EntranteID); p["evento"] != "cancelado" || p["version"] != float64(2) {
		t.Fatalf("cancelado: %v", p)
	}
	// Una causa nueva de Argos se guarda como «otra»; un payload sin solicitud se ignora.
	n := base(7004, 9500, 1)
	n["causa"] = "<script>"
	r = e.procesarWebhook(id, secreto, argos.EventoCorteManualSolicitado, n)
	if p := e.corteEmitido(r.EntranteID); p["causa"] != "otra" {
		t.Fatalf("causa desconocida: %v", p)
	}
	sin := base(0, 9500, 1)
	if r := e.procesarWebhook(id, secreto, argos.EventoCorteManualSolicitado, sin); r.Resultado != "ignorado" || e.omitidoDe(r.EntranteID) != "payload_invalido" {
		t.Fatalf("payload inválido: %+v", r)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.entrantes WHERE evento LIKE 'servicio.corte_manual_%' AND conector_id = $1`, id); n != 6 {
		t.Fatalf("todos quedan en integracion.entrantes: %d", n)
	}
}

// capacidades.corte_manual: true solo con un Iris que habla la 0.15.0 (§4.6).
func TestCapacidadCorteManual(t *testing.T) {
	if c := argos.CapacidadesDeIris("0.15.0"); !c.CorteManual || !c.TipoServicio {
		t.Fatalf("con la 0.15.0: %+v", c)
	}
	if c := argos.CapacidadesDeIris("0.14.3"); c.CorteManual || c.TipoServicio {
		t.Fatalf("con la 0.14.3: %+v", c)
	}
}
