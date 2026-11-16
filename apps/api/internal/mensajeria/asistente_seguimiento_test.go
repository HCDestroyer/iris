package mensajeria_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
)

// Seguimiento de la fase 3b: lo que el panel pidió a I1 (.claude/integration/i3.md).

// identidadEnHilo: los detalles de los mensajes de sistema identidad_verificada de la conversación.
func (x *bandejaIA) identidadEnHilo(conv string) []string {
	x.t.Helper()
	filas, err := x.bd.Query(x.ctx, `SELECT coalesce(evento->>'detalle', '') || '/' || coalesce(evento->>'actorNombre', '') FROM mensajeria.conversacion_mensajes
		WHERE conversacion_id = $1 AND evento->>'tipo' = 'identidad_verificada' ORDER BY seq`, conv)
	if err != nil {
		x.t.Fatal(err)
	}
	defer filas.Close()
	var out []string
	for filas.Next() {
		var s string
		if err := filas.Scan(&s); err != nil {
			x.t.Fatal(err)
		}
		out = append(out, s)
	}
	return out
}

// 1. contacto.verificacionVenceEn: la vigencia efectiva (el mínimo), solo con el nivel fuerte.
func TestSeguimientoVerificacionVenceEn(t *testing.T) {
	x := nuevaBandejaIA(t)
	x.autonomo("siempre_primero", nil)
	conv, _ := x.conClienteArgos()
	num := x.numeroDe(conv)
	vence := func() any {
		return x.api(x.supervisor, http.MethodGet, "/conversaciones/"+conv, nil).cuerpo["contacto"].(map[string]any)["verificacionVenceEn"]
	}
	if v := vence(); v != nil {
		t.Fatalf("con nivel numero: %v", v)
	}
	verificada := *x.reloj
	x.exigir(x.api(x.supervisor, http.MethodPost, "/conversaciones/"+conv+"/confirmar-cliente", map[string]any{"version": ver(x.conv(num))}), http.StatusOK)
	hora := func(v any) time.Time {
		t.Helper()
		s, _ := v.(string)
		h, err := time.Parse(time.RFC3339Nano, s)
		if err != nil {
			t.Fatalf("verificacionVenceEn = %v", v)
		}
		return h
	}
	if h := hora(vence()); !h.Equal(verificada.Add(60 * time.Minute)) {
		t.Fatalf("vence a los 60 min: %v (verificada %v)", h, verificada)
	}
	// También en la lista.
	if h := hora(x.conv(num)["contacto"].(map[string]any)["verificacionVenceEn"]); !h.Equal(verificada.Add(60 * time.Minute)) {
		t.Fatalf("en la lista: %v", h)
	}
	// La empresa acorta el ajuste: la fecha es la vigencia efectiva (el mínimo).
	x.autonomo("siempre_primero", map[string]any{"verificacionFuerteMin": 30})
	if h := hora(vence()); !h.Equal(verificada.Add(30 * time.Minute)) {
		t.Fatalf("con el ajuste en 30 min: %v", h)
	}
	*x.reloj = x.reloj.Add(31 * time.Minute)
	if v := vence(); v != nil {
		t.Fatalf("vencida (nivel numero): %v", v)
	}
}

// 2. La verificación de identidad queda en el hilo como mensaje de sistema, con un detalle de lista
// cerrada y sin el documento; la confirmación de un supervisor también, con su nombre.
func TestSeguimientoIdentidadEnElHilo(t *testing.T) {
	x := nuevaBandejaIA(t)
	x.autonomo("siempre_primero", nil)
	x.argos.mu.Lock()
	x.argos.documento = "1234567890101"
	x.argos.mu.Unlock()
	pedir := func() (string, string) {
		conv, _ := x.conClienteArgos()
		*x.reloj = x.reloj.Add(21 * time.Second)
		x.ia.decir(`{"categoria": "Pagos y saldo", "confianza": 0.95}`)
		x.guion(dice("Escríbeme tu DPI completo."))
		x.correr(conv)
		return conv, x.numeroDe(conv)
	}
	conv, num := pedir()
	x.turno(num, "wamid.sg1", "2222222200101", "Pagos y saldo", llama("verificar_identidad", `{}`), dice("Ese dato no coincide. ¿Puedes revisarlo?"))
	c := x.turno(num, "wamid.sg2", "1234567890101", "Pagos y saldo", llama("verificar_identidad", `{}`), dice("Listo, ya verifiqué tu identidad."))
	if d := fmt.Sprint(x.identidadEnHilo(conv)); d != "[no_coincide/Asistente de IA coincide/Asistente de IA]" {
		t.Fatalf("mensajes de identidad en el hilo: %s", d)
	}
	if a := c["actividadAsistente"].(map[string]any); a["tipo"] != "respondio" && a["tipo"] != "identidad_verificada" {
		t.Fatalf("actividad: %v", a)
	}
	// El panel los recibe como mensajes de sistema; ninguno trae el documento ni texto libre.
	for _, m := range x.mensajes(x.supervisor, conv) {
		if e, _ := m["evento"].(map[string]any); e != nil && e["tipo"] == "identidad_verificada" {
			if s := fmt.Sprint(m); strings.Contains(s, "1234567890101") || strings.Contains(s, "2222222200101") {
				t.Fatalf("el mensaje de sistema trae el documento: %s", s)
			}
		}
	}
	for _, dato := range []string{"1234567890101", "2222222200101"} {
		if n := x.contar(`SELECT count(*) FROM auditoria.acciones WHERE entrada::text LIKE '%' || $1 || '%'`, dato); n != 0 {
			t.Fatalf("%s quedó en la auditoría", dato)
		}
	}
	// Argos limita los intentos (429) o no puede verificar (422 / 503).
	for status, detalle := range map[int]string{http.StatusTooManyRequests: "demasiados_intentos", http.StatusUnprocessableEntity: "no_disponible",
		http.StatusServiceUnavailable: "no_disponible"} {
		conv2, num2 := pedir()
		x.argos.mu.Lock()
		x.argos.errVerificar = &conector.ErrorRemoto{Status: status, Tipo: "urn:argos:problema:x"}
		x.argos.mu.Unlock()
		x.turno(num2, fmt.Sprintf("wamid.sg3%d", status), "1234567890101", "Pagos y saldo", llama("verificar_identidad", `{}`), dice("No debería salir"))
		x.argos.mu.Lock()
		x.argos.errVerificar = nil
		x.argos.mu.Unlock()
		if d := fmt.Sprint(x.identidadEnHilo(conv2)); d != "["+detalle+"/Asistente de IA]" {
			t.Fatalf("status %d: %s", status, d)
		}
	}
	// La confirmación de un supervisor: detalle «supervisor» con su nombre.
	conv3, num3 := pedir()
	x.exigir(x.api(x.supervisor, http.MethodPost, "/conversaciones/"+conv3+"/confirmar-cliente", map[string]any{"version": ver(x.conv(num3))}), http.StatusOK)
	d := x.identidadEnHilo(conv3)
	if len(d) != 1 || !strings.HasPrefix(d[0], "supervisor/") || d[0] == "supervisor/" || d[0] == "supervisor/Asistente de IA" {
		t.Fatalf("confirmación de un supervisor en el hilo: %v", d)
	}
}

// 3. El motivo del relevo llega como dato (asistente.motivoPaso), de lista cerrada.
func TestSeguimientoMotivoDelRelevo(t *testing.T) {
	x := nuevaBandejaIA(t)
	x.autonomo("siempre_primero", nil)
	num := numeroCliente()
	c := x.turno(num, "wamid.sm1", "¿Tienen fibra?", "Planes y precios", dice("Sí tenemos."))
	if a := c["asistente"].(map[string]any); a["estado"] != "atendiendo" || a["motivoPaso"] != nil {
		t.Fatalf("atendiendo, sin motivo: %v", a)
	}
	c = x.turno(num, "wamid.sm2", "Quiero hablar con una persona", "Planes y precios", dice("No debería salir"))
	if a := c["asistente"].(map[string]any); a["estado"] != "espera_persona" || a["motivoPaso"] != "cliente_lo_pidio" {
		t.Fatalf("relevo: %v", a)
	}
	if a := x.api(x.supervisor, http.MethodGet, "/conversaciones/"+cid(c), nil).cuerpo["asistente"].(map[string]any); a["motivoPaso"] != "cliente_lo_pidio" {
		t.Fatalf("en el detalle: %v", a)
	}
	// Otra conversación, otro motivo.
	n2 := numeroCliente()
	x.turno(n2, "wamid.sm3", "¿Tienen fibra?", "Planes y precios", dice("Sí tenemos."))
	c2 := x.turno(n2, "wamid.sm4", "¿Y con quién hablo?", "Planes y precios", dice("Soy Carlos, asesor del equipo de soporte"))
	if a := c2["asistente"].(map[string]any); a["motivoPaso"] != "sin_respuesta" {
		t.Fatalf("relevo por un mensaje que no se pudo enviar: %v", a)
	}
	// Una persona la toma: el asistente queda detenido y el motivo ya no aplica.
	x.exigir(x.api(x.agente, http.MethodPost, "/conversaciones/"+cid(c)+"/tomar", map[string]any{"version": ver(x.conv(num))}), http.StatusOK)
	if a := x.conv(num)["asistente"].(map[string]any); a["estado"] != "detenido" || a["motivoPaso"] != nil {
		t.Fatalf("tomada por una persona: %v", a)
	}
}

// El motivo del relevo nunca sale de un periodo anterior ni de otra empresa, y el relevo sin turno
// (apagaron el modo autónomo mientras atendía) deja su propio motivo.
func TestSeguimientoMotivoDelRelevoNoQuedaObsoleto(t *testing.T) {
	x := nuevaBandejaIA(t)
	x.autonomo("siempre_primero", nil)
	motivo := func(conv string) string {
		var m *string
		if err := x.bd.QueryRow(x.ctx, `SELECT asistente_motivo_paso FROM mensajeria.conversaciones WHERE id = $1`, conv).Scan(&m); err != nil {
			t.Fatal(err)
		}
		if m == nil {
			return ""
		}
		return *m
	}
	// Relevo, resolver y reabrir: en el periodo nuevo no queda el motivo del anterior.
	num := numeroCliente()
	x.turno(num, "wamid.so1", "¿Tienen fibra?", "Planes y precios", dice("Sí tenemos."))
	c := x.turno(num, "wamid.so2", "Quiero hablar con una persona", "Planes y precios", dice("No debería salir"))
	conv := cid(c)
	if motivo(conv) != "cliente_lo_pidio" {
		t.Fatalf("motivo guardado: %q", motivo(conv))
	}
	x.exigir(x.api(x.supervisor, http.MethodPost, "/conversaciones/"+conv+"/resolver", map[string]any{"version": ver(x.conv(num))}), http.StatusOK)
	if motivo(conv) != "" || x.conv(num)["asistente"] != nil {
		t.Fatalf("resuelta: motivo %q, asistente %v", motivo(conv), x.conv(num)["asistente"])
	}
	*x.reloj = x.reloj.Add(time.Hour)
	c = x.turno(num, "wamid.so3", "Hola otra vez, ¿tienen fibra?", "Planes y precios", dice("Sí tenemos."))
	if a := c["asistente"].(map[string]any); a["estado"] != "atendiendo" || a["motivoPaso"] != nil || motivo(conv) != "" {
		t.Fatalf("reabierta: %v (motivo %q)", a, motivo(conv))
	}
	// En el periodo nuevo, el motivo es el del relevo nuevo.
	c = x.turno(num, "wamid.so4", "¿Y con quién hablo?", "Planes y precios", dice("Soy Carlos, asesor del equipo de soporte"))
	if a := c["asistente"].(map[string]any); a["motivoPaso"] != "sin_respuesta" {
		t.Fatalf("relevo del periodo nuevo: %v", a)
	}
	// La conversación pasa a otra empresa (número prestado): el motivo de la anterior no viaja.
	if _, err := x.bd.Exec(x.ctx, `UPDATE mensajeria.conversaciones SET empresa_id = 2, categoria_id = NULL WHERE id = $1`, conv); err != nil {
		t.Fatal(err)
	}
	if motivo(conv) != "" || x.contar(`SELECT count(*) FROM mensajeria.conversaciones WHERE id = $1 AND asistente_estado IS NULL`, conv) != 1 {
		t.Fatalf("al pasar a otra empresa quedó el motivo %q", motivo(conv))
	}
	// Una persona la toma: el motivo se borra.
	n2 := numeroCliente()
	x.turno(n2, "wamid.so5", "¿Tienen fibra?", "Planes y precios", dice("Sí tenemos."))
	c2 := x.turno(n2, "wamid.so6", "Quiero hablar con una persona", "Planes y precios", dice("No debería salir"))
	x.exigir(x.api(x.agente, http.MethodPost, "/conversaciones/"+cid(c2)+"/tomar", map[string]any{"version": ver(x.conv(n2))}), http.StatusOK)
	if motivo(cid(c2)) != "" {
		t.Fatalf("tomada por una persona: %q", motivo(cid(c2)))
	}
	// Apagan el modo autónomo mientras el asistente atendía: al siguiente mensaje la conversación
	// vuelve al camino normal, con su motivo de lista cerrada (no hay turno que lo registre).
	n3 := numeroCliente()
	c3 := x.turno(n3, "wamid.so7", "¿Tienen fibra?", "Planes y precios", dice("Sí tenemos."))
	x.autonomo("nunca", nil)
	x.texto(n3, "wamid.so8", "¿Y cuánto cuesta?")
	if a := x.conv(n3)["asistente"].(map[string]any); a["estado"] != "espera_persona" || a["motivoPaso"] != "asistente_apagado" {
		t.Fatalf("modo autónomo apagado mientras atendía: %v", a)
	}
	_ = c3
}
