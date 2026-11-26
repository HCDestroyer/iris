package mensajeria_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Contrato 0.15.0 §6.6 y §13.2 (B34): corte_manual.estado_cambiado va a Argos solo con contrato ≥
// 0.15.0 y con los campos del contrato (nunca la orden interna ni el servicio).
func TestWebhookCorteManualSoloContrato0150(t *testing.T) {
	e := nuevoEntorno(t, nil)
	// El relay de la base es uno solo (candado): si otro lo tiene, se espera a que el evento salga.
	publicar := func(id string) {
		t.Helper()
		for range 50 {
			e.publicar()
			if e.contar(`SELECT count(*) FROM integracion.outbox WHERE id = $1 AND publicado_en IS NOT NULL`, id) == 1 {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatal("el evento no se publicó")
	}
	emitirDe := func(solicitud int64, version int, empresa, conector int64) string {
		t.Helper()
		id := uuid.Must(uuid.NewV7())
		crudo, _ := json.Marshal(map[string]any{"solicitud_id": solicitud, "empresa_id": empresa, "version": version, "estado": "en_curso",
			"motivo_codigo": nil, "referencia": "OT-000123", "referencia_tipo": "orden", "ocurrido_en": "2026-11-26T15:00:00Z",
			"orden_id": 123, "servicio_id": 9500, "accion": "corte", "conector_id": conector})
		if _, err := e.bd.Exec(e.ctx, `INSERT INTO integracion.outbox (id, evento, empresa_id, payload) VALUES ($1, 'corte_manual.estado_cambiado', $3, $2)`,
			id, crudo, empresa); err != nil {
			t.Fatal(err)
		}
		return id.String()
	}
	emitir := func(solicitud int64, version int) string { return emitirDe(solicitud, version, 1, e.conector) }

	viejo := emitir(8101, 1)
	publicar(viejo)
	if len(e.receptor.de("corte_manual.estado_cambiado", "solicitud_id", 8101)) != 0 {
		t.Fatal("no va a un Argos con contrato < 0.15.0")
	}
	if n := e.contar(`SELECT count(*) FROM mensajeria.webhooks_salida WHERE id = $1 AND estado = 'omitido'`, viejo); n != 1 {
		t.Fatalf("omitido: %d", n)
	}

	if _, err := e.bd.Exec(e.ctx, `UPDATE integracion.conectores SET contrato_remoto = '0.15.0' WHERE id = $1`, e.conector); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = e.bd.Exec(e.ctx, `UPDATE integracion.conectores SET contrato_remoto = '0.6.0' WHERE id = $1`, e.conector)
	})
	// Alcance del conector: la empresa 1.
	var empresas []int64
	var global *bool
	if err := e.bd.QueryRow(e.ctx, `SELECT empresas, alcance_global FROM integracion.conectores WHERE id = $1`, e.conector).Scan(&empresas, &global); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = e.bd.Exec(e.ctx, `UPDATE integracion.conectores SET empresas = $2, alcance_global = $3 WHERE id = $1`, e.conector, empresas, global)
	})
	if _, err := e.bd.Exec(e.ctx, `UPDATE integracion.conectores SET empresas = '{1}', alcance_global = false WHERE id = $1`, e.conector); err != nil {
		t.Fatal(err)
	}
	publicar(emitir(8102, 1))
	ws := e.receptor.de("corte_manual.estado_cambiado", "solicitud_id", 8102)
	if len(ws) != 1 {
		t.Fatalf("webhooks: %d", len(ws))
	}
	p := ws[0].Cuerpo["payload"].(map[string]any)
	for _, c := range []string{"orden_id", "servicio_id", "accion"} {
		if _, hay := p[c]; hay {
			t.Errorf("%s no viaja a Argos: %v", c, p)
		}
	}
	if p["estado"] != "en_curso" || fmt.Sprint(p["version"]) != "1" || p["referencia"] != "OT-000123" || p["referencia_tipo"] != "orden" {
		t.Fatalf("payload: %v", p)
	}
	if _, hay := p["motivo_codigo"]; !hay {
		t.Fatal("motivo_codigo va siempre (null salvo no_realizada)")
	}

	for _, c := range []string{"orden_id", "servicio_id", "accion", "conector_id"} {
		if _, hay := p[c]; hay {
			t.Errorf("%s no viaja a Argos", c)
		}
	}

	// Revisión de #297: solo al conector que pidió el corte y solo en las empresas de su alcance.
	otro := emitirDe(8103, 1, 1, e.conector+1000)
	publicar(otro)
	ajena := emitirDe(8104, 1, 2, e.conector)
	publicar(ajena)
	if len(e.receptor.de("corte_manual.estado_cambiado", "solicitud_id", 8103))+len(e.receptor.de("corte_manual.estado_cambiado", "solicitud_id", 8104)) != 0 {
		t.Fatal("ni a otro conector ni fuera del alcance")
	}
	if n := e.contar(`SELECT count(*) FROM mensajeria.webhooks_salida WHERE id = $1 AND estado = 'omitido' AND ultimo_error LIKE 'otro_conector%'`, otro); n != 1 {
		t.Fatalf("otro conector: %d", n)
	}
	if n := e.contar(`SELECT count(*) FROM mensajeria.webhooks_salida WHERE id = $1 AND estado = 'omitido' AND ultimo_error LIKE 'empresa_no_permitida%'`, ajena); n != 1 {
		t.Fatalf("fuera del alcance: %d", n)
	}
}
