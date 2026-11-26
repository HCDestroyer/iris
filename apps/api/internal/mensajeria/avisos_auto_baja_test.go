package mensajeria_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

// C2 y C12: con auto, una baja que llega después de encolar y antes del primer envío es causa del canal:
// el aviso queda bloqueado con canal_no_disponible (sin webhook final; Argos lo ve con GET y lo manda
// por sus canales). Con un canal explícito, falla como siempre (rechazado_por_canal, detalle baja).
func TestBajaAntesDelPrimerEnvioConAuto(t *testing.T) {
	e := nuevoEntorno(t, nil)
	e.usarContrato("0.15.0")
	// 22:00 en Guatemala: los avisos esperan al final del silencio.
	*e.reloj = time.Date(2026, 11, 6, 4, 0, 0, 0, time.UTC)
	pedir := func(preferido string) string {
		t.Helper()
		c := e.cliente(1, "5512-3344")
		e.consentir(c, "acepta", e.reloj.Add(-time.Hour))
		cuerpo := avisoPorVencer(c, "2026-11-20")
		cuerpo["canal_preferido"] = preferido
		r := e.avisar(fmt.Sprintf("argos:aviso:factura_por_vencer:%d", c), cuerpo)
		e.exigir(r, http.StatusAccepted, "estado", "en_cola")
		id := r.cuerpo["aviso_id"].(string)
		if espera := e.enviar(id); espera <= 0 {
			t.Fatalf("en silencio debía esperar: %v", espera)
		}
		e.exigir(e.consentir(c, "baja", e.reloj.Add(time.Minute)), http.StatusCreated, "estado_vigente", "baja")
		return id
	}
	auto := pedir("auto")
	wa := pedir("whatsapp")

	*e.reloj = time.Date(2026, 11, 6, 13, 1, 0, 0, time.UTC)
	e.enviar(auto)
	e.enviar(auto) // un segundo intento no hace nada
	e.enviar(wa)
	e.publicar()

	e.exigir(e.pedir(http.MethodGet, "/api/v1/mensajeria/avisos/"+auto, e.token, "", nil), http.StatusOK,
		"estado", "bloqueado", "motivo", "canal_no_disponible", "detalle", "whatsapp: baja", "canal", "<nil>")
	if n := len(e.receptor.de("aviso.fallido", "aviso_id", auto)) + len(e.receptor.de("aviso.entregado", "aviso_id", auto)); n != 0 {
		t.Fatalf("un bloqueado no manda webhook final: %d", n)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'aviso.bloqueado' AND payload->>'aviso_id' = $1
		AND payload->>'motivo' = 'canal_no_disponible'`, auto); n != 1 {
		t.Fatalf("aviso.bloqueado: %d", n)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'aviso.enviado' AND payload->>'aviso_id' = $1`, auto); n != 0 {
		t.Fatal("salió después de la baja")
	}
	if n := e.contar(`SELECT count(*) FROM auditoria.cambios WHERE entidad = 'aviso' AND entidad_id = $1 AND campo = 'estado' AND despues::text = '"bloqueado"'`, auto); n != 1 {
		t.Fatalf("auditoría del bloqueo: %d", n)
	}

	w := e.receptor.de("aviso.fallido", "aviso_id", wa)
	if len(w) != 1 {
		t.Fatalf("con whatsapp, un solo aviso.fallido: %d", len(w))
	}
	if p := w[0].Cuerpo["payload"].(map[string]any); p["motivo"] != "rechazado_por_canal" || p["detalle"] != "baja" {
		t.Fatalf("con whatsapp: %v", p)
	}
}
