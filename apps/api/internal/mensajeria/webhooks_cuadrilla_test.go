package mensajeria_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/google/uuid"
)

// Contrato 0.11.0 §6.6 (B26): cuadrilla.creada y cuadrilla.actualizada van a Argos solo con contrato
// ≥ 0.11.0, con los campos del contrato (nada de campos[] ni zonas) y nunca sin empresa.
func TestWebhooksCuadrillaSoloContrato0110(t *testing.T) {
	e := nuevoEntorno(t, nil)
	emitir := func(evento string, payload map[string]any) string {
		t.Helper()
		id := uuid.Must(uuid.NewV7())
		crudo, _ := json.Marshal(payload)
		if _, err := e.bd.Exec(e.ctx, `INSERT INTO integracion.outbox (id, evento, empresa_id, payload) VALUES ($1, $2, $3, $4)`,
			id, evento, payload["empresa_id"], crudo); err != nil {
			t.Fatal(err)
		}
		return id.String()
	}
	cuadrilla := func(id int64, empresa any, version int) map[string]any {
		return map[string]any{
			"cuadrilla_id": id, "empresa_id": empresa, "version": version, "nombre": "Norte", "activa": true,
			"miembros":        []map[string]any{{"usuario_id": 7, "id_externo": "u-7"}, {"usuario_id": 8, "id_externo": nil}},
			"jefe_usuario_id": 7, "campos": []string{"miembros"}, "zonas": []int64{3},
		}
	}

	// Con 0.6.0 (el del entorno) no sale: queda omitido.
	viejo := emitir("cuadrilla.actualizada", cuadrilla(9001, 1, 2))
	e.publicar()
	if len(e.receptor.de("cuadrilla.actualizada", "cuadrilla_id", 9001)) != 0 {
		t.Fatal("cuadrilla.actualizada no va a un Argos con contrato < 0.11.0")
	}
	if n := e.contar(`SELECT count(*) FROM mensajeria.webhooks_salida WHERE id = $1 AND estado = 'omitido'`, viejo); n != 1 {
		t.Fatalf("omitido: %d", n)
	}

	if _, err := e.bd.Exec(e.ctx, `UPDATE integracion.conectores SET contrato_remoto = '0.11.0' WHERE id = $1`, e.conector); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = e.bd.Exec(e.ctx, `UPDATE integracion.conectores SET contrato_remoto = '0.6.0' WHERE id = $1`, e.conector)
	})
	emitir("cuadrilla.creada", cuadrilla(9002, 1, 1))
	emitir("cuadrilla.creada", cuadrilla(9002, 2, 1))
	// Sin empresa (modo sin conector) nunca va a Argos.
	emitir("cuadrilla.actualizada", cuadrilla(9003, nil, 4))
	e.publicar()
	ws := e.receptor.de("cuadrilla.creada", "cuadrilla_id", 9002)
	if len(ws) != 2 {
		t.Fatalf("un webhook por empresa: %d", len(ws))
	}
	for _, w := range ws {
		p := w.Cuerpo["payload"].(map[string]any)
		if _, hay := p["campos"]; hay {
			t.Fatalf("solo los campos del contrato: %v", p)
		}
		if _, hay := p["zonas"]; hay {
			t.Fatalf("solo los campos del contrato: %v", p)
		}
		ms := p["miembros"].([]any)
		if len(ms) != 2 || ms[1].(map[string]any)["id_externo"] != nil || fmt.Sprint(p["jefe_usuario_id"]) != "7" || fmt.Sprint(p["version"]) != "1" {
			t.Fatalf("payload de la cuadrilla: %v", p)
		}
	}
	if len(e.receptor.de("cuadrilla.actualizada", "cuadrilla_id", 9003)) != 0 {
		t.Fatal("una cuadrilla sin empresa no va a Argos")
	}
}
