package mensajeria_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/mensajeria"
)

// Cuarta revisión de la #163: cinco bajos, una prueba por punto.

// 1. El nivel de verificación se recalcula en cada herramienta: si la fuerte vence a mitad del turno,
// la acción de dinero siguiente ya no la tiene.
func TestAutonomoRevision4VenceAMitadDeTurno(t *testing.T) {
	x := nuevaBandejaIA(t)
	x.autonomo("siempre_primero", map[string]any{"perfiles": []any{perfilCon(map[string]any{"promesa_pago": true}, "alta")}})
	x.cuenta(&argos.EstadoCuenta{Saldo: texto("350.75"), Moneda: "GTQ", FacturasVencidas: vencidas(2), MontoVencido: texto("350.75")})
	conv, cliente := x.conClienteArgos()
	num := x.numeroDe(conv)
	x.exigir(x.api(x.supervisor, http.MethodPost, "/conversaciones/"+conv+"/confirmar-cliente", map[string]any{"version": ver(x.conv(num))}), http.StatusOK)
	// Mientras «Argos» responde el saldo pasan dos horas: la verificación (60 min) vence.
	x.argos.mu.Lock()
	x.argos.alLeerCuenta = func() { *x.reloj = x.reloj.Add(2 * time.Hour) }
	antes := len(x.argos.claves)
	x.argos.mu.Unlock()
	*x.reloj = x.reloj.Add(21 * time.Second)
	x.ia.decir(`{"categoria": "Pagos y saldo", "confianza": 0.95}`)
	x.guion(llama("consultar_saldo", `{}`), llama("pedir_promesa", fmt.Sprintf(`{"servicio_id": %d, "fecha": "2026-11-20"}`, cliente*10)),
		dice("Necesito verificar tu identidad otra vez."))
	x.correr(conv)
	x.argos.mu.Lock()
	x.argos.alLeerCuenta = nil
	claves := len(x.argos.claves) - antes
	x.argos.mu.Unlock()
	a := strings.Join(x.acciones(conv), ",")
	if !strings.Contains(a, "dar_saldo:ok,promesa_pago:rechazada") || claves != 0 {
		t.Fatalf("la promesa pasó con la verificación vencida a mitad del turno: %s (%d claves)", a, claves)
	}
}

// 2. «Tu servicio está activo» solo si estado_del_servicio devolvió algún servicio activo.
func TestAutonomoRevision4ActivoSoloSiVieneActivo(t *testing.T) {
	x := nuevaBandejaIA(t)
	x.autonomo("siempre_primero", nil)
	conv, cliente := x.conClienteArgos()
	num := x.numeroDe(conv)
	x.turno(num, "wamid.r4b1", "¿Cómo está mi servicio?", "Planes y precios", llama("estado_del_servicio", `{}`), dice("Tu servicio está activo."))
	ms := x.delAsistente(conv)
	if len(ms) == 0 || !strings.Contains(textoDe(ms[len(ms)-1]), "Tu servicio está activo.") {
		t.Fatalf("con el servicio activo debía enviarse: %v (%v)", ms, x.acciones(conv))
	}
	if _, err := x.bd.Exec(x.ctx, `UPDATE integracion.argos_servicios SET estado = 'cortado' WHERE cliente_id = $1`, cliente); err != nil {
		t.Fatal(err)
	}
	c := x.turno(num, "wamid.r4b2", "¿Y ahora?", "Planes y precios", llama("estado_del_servicio", `{}`), dice("Tu servicio está activo."))
	x.exigirPaso(c, "sin_respuesta")
	if n := len(x.delAsistente(conv)); n != len(ms) {
		// Solo puede haber salido el texto fijo de «te paso con una persona».
		for _, m := range x.delAsistente(conv)[len(ms):] {
			if strings.Contains(textoDe(m), "activo") {
				t.Fatal("dijo que el servicio está activo con el servicio cortado")
			}
		}
	}
}

// 3. Lo que el cliente nombró como NIT o DPI se manda aunque parezca un teléfono, salvo que sea el
// número del chat. Un DPI nombrado con verificador inválido no gasta intento: se pide otra vez y a la
// segunda pasa a una persona.
func TestAutonomoRevision4DocumentoNombrado(t *testing.T) {
	const tel = "50255123344"
	for texto, esperado := range map[string]string{
		"mi nit es 44556677":      "44556677",
		"NIT 5566778":             "5566778",
		"mi nit es 55123344":      "",
		"mi dpi es 50255123344":   "",
		"mi dpi es 1234567800101": "",
		"44556677":                "",
		"mi dpi es 1234567890101": "1234567890101",
	} {
		if got := mensajeria.DocumentoCandidato(texto, tel); got != esperado {
			t.Errorf("candidato de %q = %q (esperaba %q)", texto, got, esperado)
		}
	}
	if !mensajeria.DocumentoNombradoInvalido("mi dpi es 1234567800101", tel) || mensajeria.DocumentoNombradoInvalido("44556677", tel) {
		t.Error("un DPI nombrado con verificador inválido debe marcarse")
	}
	x := nuevaBandejaIA(t)
	x.autonomo("siempre_primero", nil)
	conv, _ := x.conClienteArgos()
	num := x.numeroDe(conv)
	*x.reloj = x.reloj.Add(21 * time.Second)
	x.ia.decir(`{"categoria": "Pagos y saldo", "confianza": 0.95}`)
	x.guion(dice("Escríbeme tu DPI completo."))
	x.correr(conv)
	c := x.turno(num, "wamid.r4c1", "mi dpi es 1234567800101", "Pagos y saldo", llama("verificar_identidad", `{}`), dice("Ese DPI no parece completo. ¿Puedes revisarlo?"))
	if estadoAsistente(c) == "" || strings.HasPrefix(estadoAsistente(c), "espera_persona") {
		t.Fatalf("a la primera debía pedirlo otra vez: %v", c["asistente"])
	}
	c = x.turno(num, "wamid.r4c2", "mi dpi es 1234567810101", "Pagos y saldo", llama("verificar_identidad", `{}`), dice("No debería salir"))
	x.exigirPaso(c, "herramienta_fallo")
	x.argos.mu.Lock()
	llamadas := len(x.argos.verificados)
	x.argos.mu.Unlock()
	if n := x.contar(`SELECT count(*) FROM mensajeria.asistente_verificaciones WHERE conversacion_id = $1`, conv); n != 0 || llamadas != 0 {
		t.Fatalf("un DPI inválido gastó intentos: %d, %d llamadas a Argos", n, llamadas)
	}
}

// 4. Al reescribir el payload de Meta los números quedan como llegaron (UseNumber).
func TestAutonomoRevision4PayloadConservaNumeros(t *testing.T) {
	crudo := []byte(`{"entry":[{"id":12345678001014567890,"t":1.50,"changes":[{"value":{"messages":[{"id":"wamid.X","text":{"body":"mi nit es 44556677"}}]}}]}]}`)
	out := string(mensajeria.PayloadTapado(crudo, "wamid.X"))
	if !strings.Contains(out, "12345678001014567890") || !strings.Contains(out, "1.50") || !strings.Contains(out, "mi nit es ***") || strings.Contains(out, "44556677") {
		t.Fatalf("payload reescrito: %s", out)
	}
}

// 5. Si la empresa acorta verificacionFuerteMin, las verificaciones ya dadas también se acortan: la
// vigencia efectiva es el mínimo entre lo fijado al verificar y verificada + el ajuste actual.
func TestAutonomoRevision4VigenciaRecortada(t *testing.T) {
	x := nuevaBandejaIA(t)
	x.autonomo("siempre_primero", nil)
	conv, _ := x.conClienteArgos()
	num := x.numeroDe(conv)
	x.exigir(x.api(x.supervisor, http.MethodPost, "/conversaciones/"+conv+"/confirmar-cliente", map[string]any{"version": ver(x.conv(num))}), http.StatusOK)
	*x.reloj = x.reloj.Add(10 * time.Minute)
	if v := x.verificacion(conv); v != "fuerte" {
		t.Fatalf("a los 10 min de 60: %v", v)
	}
	x.autonomo("siempre_primero", map[string]any{"verificacionFuerteMin": 5})
	if v := x.verificacion(conv); v != "numero" {
		t.Fatalf("con el ajuste acortado a 5 min, a los 10: %v", v)
	}
	// Alargarlo no revive ni alarga una verificación ya dada.
	x.autonomo("siempre_primero", map[string]any{"verificacionFuerteMin": 1440})
	*x.reloj = x.reloj.Add(55 * time.Minute)
	if v := x.verificacion(conv); v != "numero" {
		t.Fatalf("a los 65 min, con el ajuste alargado: %v", v)
	}
}
