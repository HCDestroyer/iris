package mensajeria_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

// 0.15.0 §6.6: un aviso que respondió 202 termina siempre con un único webhook final. Una baja que
// llega después de encolar y antes del primer envío lo cierra igual con auto que con un canal
// explícito: aviso.fallido, rechazado_por_canal, detalle baja.
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
	avisos := map[string]string{"auto": pedir("auto"), "whatsapp": pedir("whatsapp")}

	*e.reloj = time.Date(2026, 11, 6, 13, 1, 0, 0, time.UTC)
	for _, id := range avisos {
		e.enviar(id)
		e.enviar(id) // un segundo intento no hace nada
	}
	e.publicar()
	e.publicar()

	for preferido, id := range avisos {
		e.exigir(e.pedir(http.MethodGet, "/api/v1/mensajeria/avisos/"+id, e.token, "", nil), http.StatusOK,
			"estado", "fallido", "motivo", "rechazado_por_canal", "detalle", "baja")
		w := e.receptor.de("aviso.fallido", "aviso_id", id)
		if len(w) != 1 {
			t.Fatalf("%s: un solo aviso.fallido: %d", preferido, len(w))
		}
		if p := w[0].Cuerpo["payload"].(map[string]any); p["motivo"] != "rechazado_por_canal" || p["detalle"] != "baja" || p["canal"] != "whatsapp" {
			t.Fatalf("%s: %v", preferido, p)
		}
		if n := len(e.receptor.de("aviso.entregado", "aviso_id", id)); n != 0 {
			t.Fatalf("%s: entregado después de la baja: %d", preferido, n)
		}
		if n := e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'aviso.enviado' AND payload->>'aviso_id' = $1`, id); n != 0 {
			t.Fatalf("%s: salió después de la baja", preferido)
		}
	}
}
