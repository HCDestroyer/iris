package mensajeria_test

import (
	"fmt"
	"testing"

	"github.com/google/uuid"
)

// Contrato con Argos 0.9.0 §6.6 (B9): ticket.estado_cambiado, ticket.respondido y
// solicitud_servicio.estado_cambiado salen a Argos solo con contrato ≥ 0.9.0, solo de tickets con
// cliente y en el alcance del conector, y sin el cuerpo, notas ni motivos escritos por una persona.
func TestWebhooksPortalArgos(t *testing.T) {
	e := nuevoEntorno(t, nil)
	emitir := func(evento string, empresa int, payload string) {
		t.Helper()
		if _, err := e.bd.Exec(e.ctx, `INSERT INTO integracion.outbox (id, evento, empresa_id, payload) VALUES ($1, $2, $3, $4)`,
			uuid.Must(uuid.NewV7()), evento, empresa, payload); err != nil {
			t.Fatal(err)
		}
	}
	estado := func(ticket, cliente any, empresa int) string {
		return fmt.Sprintf(`{"ticket_id": %v, "numero": "TK-%06v", "empresa_id": %d, "cliente_id": %v, "servicio_id": null, "origen": "portal_argos",
			"de": "abierto", "a": "resuelto", "motivo": "Ana cambió el router"}`, ticket, ticket, empresa, cliente)
	}
	// Con el contrato del entorno (< 0.9.0): omitido.
	emitir("ticket.estado_cambiado", 1, estado(9201, 8850, 1))
	e.publicar()
	if len(e.receptor.de("ticket.estado_cambiado", "ticket_id", 9201)) != 0 {
		t.Fatal("no se envía a un conector con contrato < 0.9.0")
	}
	var antes *string
	if err := e.bd.QueryRow(e.ctx, `SELECT contrato_remoto FROM integracion.conectores WHERE id = $1`, e.conector).Scan(&antes); err != nil {
		t.Fatal(err)
	}
	if _, err := e.bd.Exec(e.ctx, `UPDATE integracion.conectores SET contrato_remoto = '0.9.0', empresas = '{1}', alcance_global = false WHERE id = $1`, e.conector); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = e.bd.Exec(e.ctx, `UPDATE integracion.conectores SET contrato_remoto = $2 WHERE id = $1`, e.conector, antes)
	})
	emitir("ticket.estado_cambiado", 1, estado(9202, 8850, 1))
	emitir("ticket.estado_cambiado", 1, estado(9203, "null", 1)) // sin cliente: no va
	emitir("ticket.estado_cambiado", 2, estado(9204, 8850, 2))   // fuera del alcance: omitido
	emitir("ticket.respondido", 1, `{"ticket_id": 9205, "mensaje_id": 77, "numero": "TK-009205", "empresa_id": 1, "cliente_id": 8850,
		"autor_tipo": "agente", "masivo": false, "aviso_por_correo": true}`)
	emitir("solicitud_servicio.estado_cambiado", 1, `{"solicitud_id": 731, "numero": "PR-000731", "empresa_id": 1, "cliente_id": 8850,
		"tipo": "traslado", "servicio_id": 4410, "de": "recibida", "a": "en_revision"}`)
	e.publicar()

	w := e.receptor.de("ticket.estado_cambiado", "ticket_id", 9202)
	if len(w) != 1 {
		t.Fatalf("webhook: %v", w)
	}
	p := w[0].Cuerpo["payload"].(map[string]any)
	if len(p) != 9 || p["puede_confirmar"] != true || p["motivo"] != nil || p["origen"] != "portal_argos" || fmt.Sprint(p["cliente_id"]) != "8850" {
		t.Fatalf("payload: %v", p)
	}
	if len(e.receptor.de("ticket.estado_cambiado", "ticket_id", 9203)) != 0 || len(e.receptor.de("ticket.estado_cambiado", "ticket_id", 9204)) != 0 {
		t.Fatal("sin cliente o fuera del alcance no se envía")
	}
	if n := e.contar(`SELECT count(*) FROM mensajeria.webhooks_salida WHERE evento = 'ticket.estado_cambiado' AND estado = 'omitido'
		AND ultimo_error LIKE '%alcance%'`); n != 1 {
		t.Fatalf("omitido por alcance: %d", n)
	}
	r := e.receptor.de("ticket.respondido", "ticket_id", 9205)
	if len(r) != 1 || r[0].Cuerpo["payload"].(map[string]any)["aviso_por_correo"] != true {
		t.Fatalf("respondido: %v", r)
	}
	s := e.receptor.de("solicitud_servicio.estado_cambiado", "solicitud_id", 731)
	if len(s) != 1 || s[0].Cuerpo["payload"].(map[string]any)["a"] != "en_revision" {
		t.Fatalf("solicitud: %v", s)
	}
}
