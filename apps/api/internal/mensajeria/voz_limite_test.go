package mensajeria_test

import (
	"bytes"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/mensajeria"
)

// El requisito «límite de mensajería» de las llamadas, contra el cliente Graph REAL de Iris y un
// Meta de mentira que responde como la documentación vigente (messaging-limits): el campo es
// whatsapp_business_manager_messaging_limit, del portafolio de negocio; messaging_limit_tier está
// deprecado. Primera prueba del PM contra Meta real, 2026-11-17: Iris leía el campo deprecado,
// marcaba «Falta» con un límite real de 2 000 y eso apagaba todas las llamadas.

func TestLimiteDeNivel(t *testing.T) {
	for nivel, quiero := range map[string]int64{"TIER_250": 250, "TIER_1K": 1000, "TIER_2K": 2000, "TIER_10K": 10000, "TIER_100K": 100000,
		"tier_2k": 2000, " TIER_2K ": 2000, "TIER_1M": 1000000, "TIER_UNLIMITED": mensajeria.LimiteIlimitado} {
		if n, ok := mensajeria.LimiteDeNivel(nivel); !ok || n != quiero {
			t.Fatalf("%q: %d %v (quería %d)", nivel, n, ok, quiero)
		}
	}
	// Lo que no se reconoce no se interpreta: ni «cumple» ni «falta».
	for _, nivel := range []string{"", "TIER_", "TIER_NUEVO", "2000", "TIER_0", "TIER_-5", "TIER_2KK", "UNLIMITED", "TIER_2 K", "MESSAGING_LIMIT_TIER_1"} {
		if n, ok := mensajeria.LimiteDeNivel(nivel); ok {
			t.Fatalf("%q no debía reconocerse: %d", nivel, n)
		}
	}
}

// requisito devuelve el requisito con ese código de una respuesta de llamadas del canal.
func requisito(t *testing.T, r respuesta, codigo string) map[string]any {
	t.Helper()
	for _, f := range r.cuerpo["requisitos"].(map[string]any)["requisitos"].([]any) {
		if f.(map[string]any)["codigo"] == codigo {
			return f.(map[string]any)
		}
	}
	t.Fatalf("sin el requisito %s: %s", codigo, crudo(r))
	return nil
}

func TestLlamadasLimiteDeMensajeriaContraMeta(t *testing.T) {
	// Sin doble de Graph: el cliente real, contra el Meta de mentira del entorno.
	x := &bandejaVoz{bandeja: nuevaBandeja(t, nil)}
	meta := func(habilitadas bool, nivel, viejo string) {
		x.meta.mu.Lock()
		x.meta.llamadasHabilitadas, x.meta.nivel, x.meta.nivelViejo, x.meta.campos = habilitadas, nivel, viejo, nil
		x.meta.mu.Unlock()
	}
	comprobar := func() respuesta {
		t.Helper()
		return x.exigir(x.api(x.supervisor, http.MethodPost, x.rutaCanal("/llamadas/comprobar"), nil), http.StatusOK)
	}
	cumple := func(r respuesta) bool { return r.cuerpo["requisitos"].(map[string]any)["cumple"] == true }
	apagar := func(version int) int {
		t.Helper()
		r := x.exigir(x.configurar(version, map[string]any{"llamadasActivas": false}), http.StatusOK)
		return int(r.cuerpo["version"].(float64))
	}
	version := func(r respuesta) int { return int(r.cuerpo["version"].(float64)) }

	// 1. El campo vigente con 2 000 (lo que tiene el PM): cumple. Y se pidió el campo vigente, no el deprecado.
	meta(true, "TIER_2K", "error")
	c := comprobar()
	if l := requisito(t, c, "limite_mensajeria"); l["cumple"] != true || l["aviso"] == true || l["sinComprobar"] == true || !cumple(c) {
		t.Fatalf("TIER_2K: %s", crudo(c))
	}
	x.meta.mu.Lock()
	campos := slices.Clone(x.meta.campos)
	x.meta.mu.Unlock()
	if len(campos) != 1 || campos[0] != "whatsapp_business_manager_messaging_limit" {
		t.Fatalf("campos pedidos a Meta: %v", campos)
	}
	// Se enciende, y una entrante suena (no se rechaza sin registro).
	r := x.exigir(x.configurar(version(c), nil), http.StatusOK)
	if r.cuerpo["llamadasActivas"] != true {
		t.Fatalf("encender con el límite en 2 000: %s", crudo(r))
	}
	x.recibe(x.agente)
	num, call := numeroCliente(), "wacid.limite"+numeroCliente()
	x.connect(num, call)
	if _, estado, _ := x.llamada(call); estado != "sonando" {
		t.Fatalf("con las llamadas encendidas la entrante debía sonar: %s", estado)
	}
	v := apagar(version(r))

	// 2. 250 y sin llamadas habilitadas en Meta: falta de verdad (lo afirma Meta) y no deja encender.
	meta(false, "TIER_250", "")
	c = comprobar()
	if l := requisito(t, c, "limite_mensajeria"); l["cumple"] != false || l["aviso"] == true || l["sinComprobar"] == true || cumple(c) {
		t.Fatalf("TIER_250 sin llamadas habilitadas: %s", crudo(c))
	}
	if r := x.exigir(x.configurar(version(c), nil), http.StatusUnprocessableEntity); !strings.Contains(crudo(r), "requisitos_sin_cumplir") || !strings.Contains(crudo(r), "limite_mensajeria") {
		t.Fatalf("encender con 250: %s", crudo(r))
	}
	_ = v
	// 250 pero Meta ya tiene las llamadas habilitadas (número de prueba, exento): aviso, no bloquea.
	meta(true, "TIER_250", "")
	c = comprobar()
	if l := requisito(t, c, "limite_mensajeria"); l["cumple"] != false || l["aviso"] != true || l["sinComprobar"] == true || !cumple(c) {
		t.Fatalf("TIER_250 con llamadas habilitadas en Meta: %s", crudo(c))
	}

	// 3. Meta no devuelve ninguno de los dos campos, o un literal que Iris no conoce: «sin comprobar».
	// Nunca «falta»; se puede encender y, si ya estaban encendidas, no se apagan.
	for nombre, f := range map[string]func(){
		"sin ningún campo":               func() { meta(true, "", "") },
		"el deprecado ya no existe":      func() { meta(true, "", "error") },
		"literal desconocido":            func() { meta(true, "TIER_NUEVO", "") },
		"literal desconocido en los dos": func() { meta(true, "ALGO", "OTRO") },
	} {
		f()
		c = comprobar()
		l := requisito(t, c, "limite_mensajeria")
		if l["cumple"] != false || l["sinComprobar"] != true || l["aviso"] != true || !cumple(c) || strings.Contains(l["detalle"].(string), "exige") {
			t.Fatalf("%s: debía quedar sin comprobar: %s", nombre, crudo(c))
		}
		r := x.exigir(x.configurar(version(c), nil), http.StatusOK)
		if r.cuerpo["llamadasActivas"] != true {
			t.Fatalf("%s: debía poder encenderse: %s", nombre, crudo(r))
		}
		// Comprobar otra vez con las llamadas encendidas: siguen encendidas.
		if c = comprobar(); c.cuerpo["llamadasActivas"] != true {
			t.Fatalf("%s: comprobar no debía apagar las llamadas: %s", nombre, crudo(c))
		}
		apagar(version(c))
	}

	// 4. Respaldo: si el campo vigente no viene pero el deprecado sí, vale el deprecado.
	meta(true, "", "TIER_10K")
	c = comprobar()
	if l := requisito(t, c, "limite_mensajeria"); l["cumple"] != true {
		t.Fatalf("respaldo con el campo deprecado: %s", crudo(c))
	}
	x.meta.mu.Lock()
	campos = slices.Clone(x.meta.campos)
	x.meta.mu.Unlock()
	if strings.Join(campos, ",") != "whatsapp_business_manager_messaging_limit,messaging_limit_tier" {
		t.Fatalf("orden de los campos pedidos: %v", campos)
	}

	// 5. Lo que tenía guardado el canal del PM: requisitos con «Falta» del Iris anterior y las llamadas
	// apagadas. Al desplegar basta con encender: Iris vuelve a comprobar sola con lo que dice Meta ahora.
	meta(true, "TIER_2K", "error")
	viejo := func() {
		x.sql(`UPDATE mensajeria.voz_ajustes_canal SET llamadas_activas = false, requisitos = $2::jsonb WHERE canal_id = $1`, x.canal.ID,
			`{"cumple": false, "requisitos": [{"codigo": "conexion_api", "cumple": true, "detalle": "x", "aviso": false},
			{"codigo": "limite_mensajeria", "cumple": false, "detalle": "WhatsApp exige un límite de al menos 2 000 destinatarios por día", "aviso": false},
			{"codigo": "calling_habilitado", "cumple": true, "detalle": "x", "aviso": false}]}`)
	}
	viejo()
	g := x.exigir(x.api(x.supervisor, http.MethodGet, x.rutaCanal("/llamadas"), nil), http.StatusOK)
	r = x.exigir(x.configurar(version(g), nil), http.StatusOK)
	if r.cuerpo["llamadasActivas"] != true || requisito(t, r, "limite_mensajeria")["cumple"] != true {
		t.Fatalf("encender con una comprobación guardada vieja: %s", crudo(r))
	}
	// Y si de verdad no cumple, el error trae los requisitos de ahora.
	viejo()
	meta(false, "TIER_250", "")
	g = x.exigir(x.api(x.supervisor, http.MethodGet, x.rutaCanal("/llamadas"), nil), http.StatusOK)
	if r := x.exigir(x.configurar(version(g), nil), http.StatusUnprocessableEntity); !strings.Contains(crudo(r), "requisitos_sin_cumplir") || !strings.Contains(crudo(r), "calling_habilitado") {
		t.Fatalf("encender sin cumplir de verdad: %s", crudo(r))
	}
}

// Meta devuelve un literal que Iris no entiende: queda un WARN con el literal acotado (solo letras,
// dígitos y guion bajo, 40 como mucho), para enterarnos si cambia el formato.
func TestLlamadasLimiteDesconocidoDejaRastro(t *testing.T) {
	x := &bandejaVoz{bandeja: nuevaBandeja(t, nil)}
	var registro bytes.Buffer
	anterior := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&registro, nil)))
	t.Cleanup(func() { slog.SetDefault(anterior) })
	x.meta.mu.Lock()
	x.meta.llamadasHabilitadas, x.meta.nivel = true, `NIVEL_<script>alert(1)</script> "x" `+strings.Repeat("A", 200)
	x.meta.mu.Unlock()
	c := x.exigir(x.api(x.supervisor, http.MethodPost, x.rutaCanal("/llamadas/comprobar"), nil), http.StatusOK)
	if requisito(t, c, "limite_mensajeria")["sinComprobar"] != true {
		t.Fatalf("debía quedar sin comprobar: %s", crudo(c))
	}
	salida := registro.String()
	quiero := "NIVEL_scriptalert1scriptx" + strings.Repeat("A", 15) // 40 caracteres
	if !strings.Contains(salida, "level=WARN") || !strings.Contains(salida, "valor="+quiero+" ") && !strings.HasSuffix(strings.TrimSpace(salida), "valor="+quiero) {
		t.Fatalf("WARN con el literal acotado (%s): %s", quiero, salida)
	}
	if strings.Contains(salida, "<script>") || strings.Contains(salida, strings.Repeat("A", 41)) {
		t.Fatalf("el literal no quedó acotado: %s", salida)
	}
	// Un literal conocido no deja nada.
	registro.Reset()
	x.meta.mu.Lock()
	x.meta.nivel = "TIER_2K"
	x.meta.mu.Unlock()
	x.exigir(x.api(x.supervisor, http.MethodPost, x.rutaCanal("/llamadas/comprobar"), nil), http.StatusOK)
	if strings.Contains(registro.String(), "no reconoce") {
		t.Fatalf("un literal conocido no debía avisar: %s", registro.String())
	}
}

// Encender con una comprobación guardada que no cumple vuelve a comprobar y reintenta, pero si otra
// persona guardó los ajustes entre los dos intentos no se pisa su cambio: 409.
func TestLlamadasEncenderNoPisaOtroCambio(t *testing.T) {
	x := &bandejaVoz{bandeja: nuevaBandeja(t, nil)}
	x.meta.mu.Lock()
	x.meta.llamadasHabilitadas, x.meta.nivel = false, "TIER_250"
	x.meta.mu.Unlock()
	c := x.exigir(x.api(x.supervisor, http.MethodPost, x.rutaCanal("/llamadas/comprobar"), nil), http.StatusOK)
	version := int(c.cuerpo["version"].(float64))
	// Meta ya cumple, pero lo guardado dice que no. Mientras Iris vuelve a comprobar, otra persona
	// guarda los ajustes del número.
	x.meta.mu.Lock()
	x.meta.llamadasHabilitadas, x.meta.nivel = true, "TIER_2K"
	x.meta.alLeerAjustes = func() {
		x.sql(`UPDATE mensajeria.voz_ajustes_canal SET segundos_sonando = 40, version = version + 1 WHERE canal_id = $1`, x.canal.ID)
	}
	x.meta.mu.Unlock()
	r := x.exigir(x.configurar(version, nil), http.StatusConflict)
	if !strings.Contains(crudo(r), "conflicto_version") {
		t.Fatalf("debía responder el conflicto de versión: %s", crudo(r))
	}
	g := x.exigir(x.api(x.supervisor, http.MethodGet, x.rutaCanal("/llamadas"), nil), http.StatusOK)
	if g.cuerpo["llamadasActivas"] != false || g.cuerpo["segundosSonando"] != float64(40) {
		t.Fatalf("el cambio de la otra persona debía quedar intacto y las llamadas sin encender: %s", crudo(g))
	}
	// Sin nadie en medio, el mismo camino enciende.
	x.meta.mu.Lock()
	x.meta.alLeerAjustes = nil
	x.meta.mu.Unlock()
	x.sql(`UPDATE mensajeria.voz_ajustes_canal SET requisitos = jsonb_set(requisitos, '{cumple}', 'false') WHERE canal_id = $1`, x.canal.ID)
	g = x.exigir(x.api(x.supervisor, http.MethodGet, x.rutaCanal("/llamadas"), nil), http.StatusOK)
	if r := x.exigir(x.configurar(int(g.cuerpo["version"].(float64)), nil), http.StatusOK); r.cuerpo["llamadasActivas"] != true {
		t.Fatalf("sin nadie en medio debía encender: %s", crudo(r))
	}
}
