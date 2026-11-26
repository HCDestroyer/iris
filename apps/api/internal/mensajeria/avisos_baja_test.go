package mensajeria_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

// §6.4: una baja que el cliente da mientras su aviso espera (horario de silencio) manda sobre el
// aviso: al terminar el silencio no sale, falla una sola vez (rechazado_por_canal, detalle baja) y
// Argos recibe un único webhook final.
func TestAvisoEnColaNoSaleTrasLaBaja(t *testing.T) {
	e := nuevoEntorno(t, nil)
	// 22:00 en Guatemala: el aviso queda en cola hasta las 07:00.
	*e.reloj = time.Date(2026, 11, 6, 4, 0, 0, 0, time.UTC)
	c := e.cliente(1, "5512-3344")
	e.consentir(c, "acepta", e.reloj.Add(-time.Hour))
	r := e.avisar(fmt.Sprintf("argos:aviso:factura_por_vencer:%d", c), avisoPorVencer(c, "2026-11-20"))
	e.exigir(r, http.StatusAccepted, "estado", "en_cola")
	id := r.cuerpo["aviso_id"].(string)
	if espera := e.enviar(id); espera <= 0 {
		t.Fatalf("en silencio debía esperar: %v", espera)
	}
	// El cliente pide la baja durante la noche.
	e.exigir(e.consentir(c, "baja", e.reloj.Add(time.Minute)), http.StatusCreated, "estado_vigente", "baja")

	// 07:01: no sale; falla con un solo aviso.fallido.
	*e.reloj = time.Date(2026, 11, 6, 13, 1, 0, 0, time.UTC)
	if espera := e.enviar(id); espera != 0 {
		t.Fatalf("espera: %v", espera)
	}
	e.enviar(id) // un segundo intento no hace nada
	e.publicar()
	e.publicar()
	if n := e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'aviso.enviado' AND payload->>'aviso_id' = $1`, id); n != 0 {
		t.Fatalf("salió después de la baja: %d", n)
	}
	w := e.receptor.de("aviso.fallido", "aviso_id", id)
	if len(w) != 1 {
		t.Fatalf("aviso.fallido: %d", len(w))
	}
	p := w[0].Cuerpo["payload"].(map[string]any)
	if p["motivo"] != "rechazado_por_canal" || p["detalle"] != "baja" || p["canal"] != "whatsapp" {
		t.Fatalf("payload: %v", p)
	}
	if len(e.receptor.de("aviso.entregado", "aviso_id", id)) != 0 {
		t.Fatal("un aviso fallido no se entrega")
	}
	e.exigir(e.pedir(http.MethodGet, "/api/v1/mensajeria/avisos/"+id, e.token, "", nil), http.StatusOK, "estado", "fallido", "detalle", "baja")

	// Sin baja, el mismo camino sale como siempre.
	c2 := e.cliente(1, "5512-3345")
	e.consentir(c2, "acepta", e.reloj.Add(-time.Hour))
	r = e.avisar(fmt.Sprintf("argos:aviso:factura_por_vencer:%d", c2), avisoPorVencer(c2, "2026-11-20"))
	e.exigir(r, http.StatusAccepted, "estado", "en_cola")
	e.enviar(r.cuerpo["aviso_id"].(string))
	e.exigir(e.pedir(http.MethodGet, "/api/v1/mensajeria/avisos/"+r.cuerpo["aviso_id"].(string), e.token, "", nil), http.StatusOK, "estado", "entregado")
}
