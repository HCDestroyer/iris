package argos

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	conectorbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/conector/bd"
)

// Webhooks de la 0.9.0 (§12): solo la lista blanca llega al evento interno (nunca el SSID, la clave
// ni credenciales), y una empresa fuera del alcance del conector se ignora.
func TestWebhooks090ListaBlanca(t *testing.T) {
	con := conectorbd.IntegracionConectore{Empresas: []int64{1}}
	entrante := func(evento, payload string) conector.Entrante {
		return conector.Entrante{ID: 9, Evento: evento, Payload: []byte(payload)}
	}
	ev, omit := de090(entrante(EventoInstalacionAprovisionada, `{"aprovisionamiento_id":7001,"servicio_id":"4410","empresa_id":1,"cliente_id":990,
		"origen_tipo":"iris_orden","origen_id":"9123","idempotency_key":"iris:aprovisionar:9123:1","servicio_estado":"activo",
		"equipo":{"tipo":"ont","serie":"ZTEG","mac":null,"unidad_id":5},"wifi":{"ssid":"Casa","clave":"secreta-123"},"senal":{"rx_dbm":-19}}`), con)
	if omit != "" || ev.evento != EventoInstalacionResuelta || ev.payload["estado"] != "listo" || ev.payload["aprovisionamiento_id"] != int64(7001) {
		t.Fatalf("aprovisionada: %+v %s", ev, omit)
	}
	crudo, _ := json.Marshal(ev.payload)
	for _, prohibido := range []string{"secreta", "Casa", "ZTEG", "ssid", "clave"} {
		if strings.Contains(string(crudo), prohibido) {
			t.Fatalf("el evento interno lleva %q: %s", prohibido, crudo)
		}
	}
	ev, _ = de090(entrante(EventoInstalacionFallida, `{"aprovisionamiento_id":7002,"servicio_id":4410,"empresa_id":1,"causa":"Rara Causa!",
		"reintentable":true,"detalle":"La ONT\nno aparece"}`), con)
	if ev.payload["causa"].(*string) == nil || *ev.payload["causa"].(*string) != "otro" || strings.Contains(*ev.payload["detalle"].(*string), "\n") {
		t.Fatalf("fallida: %+v", ev.payload)
	}
	if _, omit := de090(entrante(EventoInstalacionFallida, `{"aprovisionamiento_id":7003,"servicio_id":4410,"empresa_id":2}`), con); omit != "empresa_fuera_de_alcance" {
		t.Fatalf("empresa fuera del alcance: %s", omit)
	}
	if _, omit := de090(entrante(EventoWifiResuelto, `{"solicitud_id":8001,"servicio_id":4410,"empresa_id":1,"estado":"raro"}`), con); omit != "payload_invalido" {
		t.Fatalf("wifi con estado desconocido: %s", omit)
	}
	ev, _ = de090(entrante(EventoBienvenidaCambiada, `{"servicio_id":4410,"empresa_id":1,"canales":[{"canal":"whatsapp","estado":"enviada"},
		{"canal":"sms","estado":"enviada"},{"canal":"correo","estado":"x"}],"texto":"Hola, paga en la cuenta 123456"}`), con)
	if c := ev.payload["canales"].([]map[string]any); len(c) != 1 || c[0]["canal"] != "whatsapp" {
		t.Fatalf("canales: %+v", ev.payload["canales"])
	}
	if crudo, _ := json.Marshal(ev.payload); strings.Contains(string(crudo), "123456") {
		t.Fatalf("la bienvenida lleva el texto: %s", crudo)
	}
	if _, omit := de090(entrante(EventoFacturaEmitida, `{"factura_id":555,"empresa_id":1,"origen_tipo":"otro","origen_id":"9"}`), con); omit != "payload_invalido" {
		t.Fatalf("factura que no es de una orden de Iris: %s", omit)
	}
}
