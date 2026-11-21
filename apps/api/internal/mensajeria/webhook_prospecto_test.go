package mensajeria_test

import (
	"fmt"
	"testing"

	"github.com/google/uuid"
)

// Contrato con Argos 0.10.0 §6.6: prospecto.estado_cambiado sale a Argos solo de los prospectos
// registrados desde su panel (argos_panel), sin datos personales, y solo a un conector con contrato
// ≥ 0.10.0 (con uno anterior queda omitido).
func TestWebhookProspectoEstadoCambiado(t *testing.T) {
	e := nuevoEntorno(t, nil)
	emitir := func(prospecto int, origen string) {
		t.Helper()
		if _, err := e.bd.Exec(e.ctx, `INSERT INTO integracion.outbox (id, evento, empresa_id, payload) VALUES ($1, 'prospecto.estado_cambiado', 1, $2)`,
			uuid.Must(uuid.NewV7()), fmt.Sprintf(`{"prospecto_id": %d, "empresa_id": 1, "de": "nuevo", "a": "contactado", "motivo": "Ana López pidió otra hora", "origen": %q}`, prospecto, origen)); err != nil {
			t.Fatal(err)
		}
	}
	// Con el contrato del entorno (< 0.10.0): omitido.
	emitir(9101, "argos_panel")
	e.publicar()
	if len(e.receptor.de("prospecto.estado_cambiado", "prospecto_id", 9101)) != 0 {
		t.Fatal("no se envía a un conector con contrato < 0.10.0")
	}
	if n := e.contar(`SELECT count(*) FROM mensajeria.webhooks_salida WHERE evento = 'prospecto.estado_cambiado' AND estado = 'omitido'`); n != 1 {
		t.Fatalf("omitido: %d", n)
	}
	var antes *string
	if err := e.bd.QueryRow(e.ctx, `SELECT contrato_remoto FROM integracion.conectores WHERE id = $1`, e.conector).Scan(&antes); err != nil {
		t.Fatal(err)
	}
	if _, err := e.bd.Exec(e.ctx, `UPDATE integracion.conectores SET contrato_remoto = '0.10.0' WHERE id = $1`, e.conector); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = e.bd.Exec(e.ctx, `UPDATE integracion.conectores SET contrato_remoto = $2 WHERE id = $1`, e.conector, antes)
	})
	emitir(9102, "argos_panel")
	emitir(9103, "panel")
	e.publicar()
	w := e.receptor.de("prospecto.estado_cambiado", "prospecto_id", 9102)
	if len(w) != 1 {
		t.Fatalf("webhook: %v", w)
	}
	p := w[0].Cuerpo["payload"].(map[string]any)
	if len(p) != 5 || p["de"] != "nuevo" || p["a"] != "contactado" || fmt.Sprint(p["empresa_id"]) != "1" || p["motivo_codigo"] != nil {
		t.Fatalf("payload: %v", p)
	}
	if len(e.receptor.de("prospecto.estado_cambiado", "prospecto_id", 9103)) != 0 {
		t.Fatal("solo los prospectos registrados desde Argos")
	}
}
