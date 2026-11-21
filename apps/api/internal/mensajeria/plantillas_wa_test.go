package mensajeria_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/mensajeria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// metaPlantillas es una Graph API falsa de plantillas que se porta como la de Meta: guarda las
// plantillas por cuenta, rechaza el nombre repetido en el mismo idioma (100/2388024), pagina con
// cursores, borra por hsm_id y responde los mensajes con un wamid. Nunca se llama a Meta real.
type metaPlantillas struct {
	srv *httptest.Server
	mu  sync.Mutex
	// por cuenta: las plantillas en el orden en que se crearon.
	cuentas   map[string][]map[string]any
	siguiente int
	// sinPermiso: /me/permissions no trae whatsapp_business_management (y la gestión responde 200).
	sinPermiso bool
	caida      bool
	creadas    []map[string]any
	editadas   map[string]map[string]any
	borradas   []string
	reanudadas []string
	envios     []map[string]any
	tokens     []string
	pagina     int
}

func nuevaMetaPlantillas(t *testing.T) *metaPlantillas {
	f := &metaPlantillas{cuentas: map[string][]map[string]any{}, siguiente: 1689556908129000, editadas: map[string]map[string]any{}, pagina: 2}
	f.srv = httptest.NewServer(http.HandlerFunc(f.servir))
	t.Cleanup(f.srv.Close)
	return f
}

func errorMeta(w http.ResponseWriter, status, codigo, sub int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": msg, "type": "OAuthException", "code": codigo,
		"error_subcode": sub, "error_user_msg": msg, "fbtrace_id": "x"}})
}

func (f *metaPlantillas) servir(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.caida {
		hj, _ := w.(http.Hijacker)
		c, _, _ := hj.Hijack()
		_ = c.Close()
		return
	}
	f.tokens = append(f.tokens, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	partes := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	responder := func(v any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
	switch {
	case len(partes) == 2 && partes[0] == "me" && partes[1] == "permissions":
		perms := []map[string]any{{"permission": "whatsapp_business_messaging", "status": "granted"}}
		if !f.sinPermiso {
			perms = append(perms, map[string]any{"permission": "whatsapp_business_management", "status": "granted"})
		}
		responder(map[string]any{"data": perms})
	case len(partes) == 2 && partes[1] == "messages" && r.Method == http.MethodPost:
		var c map[string]any
		_ = json.NewDecoder(r.Body).Decode(&c)
		f.envios = append(f.envios, c)
		responder(map[string]any{"messaging_product": "whatsapp", "messages": []map[string]any{{"id": fmt.Sprintf("wamid.%d", len(f.envios))}}})
	case len(partes) == 2 && partes[1] == "message_templates":
		if f.sinPermiso {
			errorMeta(w, http.StatusForbidden, 200, 0, "(#200) Permissions error")
			return
		}
		waba := partes[0]
		switch r.Method {
		case http.MethodGet:
			lista := f.cuentas[waba]
			desde := 0
			if a := r.URL.Query().Get("after"); a != "" {
				desde, _ = strconv.Atoi(a)
			}
			hasta := min(desde+f.pagina, len(lista))
			out := map[string]any{"data": lista[desde:hasta]}
			if hasta < len(lista) {
				out["paging"] = map[string]any{"cursors": map[string]any{"after": strconv.Itoa(hasta)}, "next": "https://graph.facebook.com/next"}
			}
			responder(out)
		case http.MethodPost:
			var c map[string]any
			_ = json.NewDecoder(r.Body).Decode(&c)
			for _, p := range f.cuentas[waba] {
				if p["name"] == c["name"] && p["language"] == c["language"] {
					errorMeta(w, http.StatusBadRequest, 100, 2388024, "Content in This Language Already Exists")
					return
				}
			}
			f.siguiente++
			id := strconv.Itoa(f.siguiente)
			p := map[string]any{"id": id, "name": c["name"], "language": c["language"], "status": "PENDING", "category": strings.ToUpper(fmt.Sprint(c["category"])),
				"components": c["components"], "parameter_format": strings.ToUpper(fmt.Sprint(c["parameter_format"]))}
			f.cuentas[waba] = append(f.cuentas[waba], p)
			f.creadas = append(f.creadas, c)
			responder(map[string]any{"id": id, "status": "PENDING", "category": p["category"]})
		case http.MethodDelete:
			q := r.URL.Query()
			for i, p := range f.cuentas[waba] {
				if p["name"] == q.Get("name") && p["id"] == q.Get("hsm_id") {
					f.cuentas[waba] = append(f.cuentas[waba][:i], f.cuentas[waba][i+1:]...)
					f.borradas = append(f.borradas, q.Get("hsm_id"))
					responder(map[string]any{"success": true})
					return
				}
			}
			errorMeta(w, http.StatusBadRequest, 100, 0, "Template not found")
		}
	case len(partes) == 2 && partes[1] == "unpause":
		f.reanudadas = append(f.reanudadas, partes[0])
		responder(map[string]any{"success": true})
	case len(partes) == 1 && r.Method == http.MethodPost:
		var c map[string]any
		_ = json.NewDecoder(r.Body).Decode(&c)
		f.editadas[partes[0]] = c
		responder(map[string]any{"success": true})
	default:
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":{"code":100,"message":"Unknown path"}}`)
	}
}

// fijar cambia el estado (y lo demás) de una plantilla en Meta.
func (f *metaPlantillas) fijar(waba, id string, campos map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, p := range f.cuentas[waba] {
		if p["id"] == id {
			for k, v := range campos {
				p[k] = v
			}
		}
	}
}

// agregar pone en Meta una plantilla creada fuera de Iris.
func (f *metaPlantillas) agregar(waba string, p map[string]any) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.siguiente++
	id := strconv.Itoa(f.siguiente)
	p["id"] = id
	f.cuentas[waba] = append(f.cuentas[waba], p)
	return id
}

func (f *metaPlantillas) quitar(waba, id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, p := range f.cuentas[waba] {
		if p["id"] == id {
			f.cuentas[waba] = append(f.cuentas[waba][:i], f.cuentas[waba][i+1:]...)
			return
		}
	}
}

// cuentaWA es una prueba con un número de WhatsApp con cuenta (WABA) y la Meta falsa.
type cuentaWA struct {
	*bandeja
	meta   *metaPlantillas
	admin  persona
	waba   string
	canal  int64
	pnidWA string
}

var sigWaba sync.Mutex

func nuevaCuentaWA(t *testing.T, plantillas map[string]any) *cuentaWA {
	t.Helper()
	f := nuevaMetaPlantillas(t)
	b := nuevaBandeja(t, func(c *mensajeria.Config) {
		c.MetaURL = f.srv.URL
		c.BaseURL = "https://iris.ejemplo.gt/"
	})
	sigWaba.Lock()
	waba := fmt.Sprint(time.Now().UnixNano() % 1e14)
	time.Sleep(time.Microsecond)
	sigWaba.Unlock()
	c := &cuentaWA{bandeja: b, meta: f, admin: b.persona([]int64{1}, rolBase("admin")), waba: waba}
	cuerpo := map[string]any{"wabaId": waba, "usos": []string{"avisos", "soporte"}, "avisosEmpresa": true}
	if plantillas != nil {
		cuerpo["plantillas"] = plantillas
	}
	c.admin.empresa = 1
	r := crearNumero(b, c.admin, cuerpo)
	b.exigir(r, http.StatusCreated)
	c.canal = int64(r.cuerpo["id"].(float64))
	c.pnidWA = r.cuerpo["phoneNumberId"].(string)
	return c
}

func (c *cuentaWA) webhookWA(cuerpo string) {
	c.t.Helper()
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("%s/canales/%d/webhook", rutaMsj, c.canal), strings.NewReader(cuerpo))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hub-Signature-256", mensajeria.FirmarMeta("app-secret-de-prueba-0123", []byte(cuerpo)))
	w := httptest.NewRecorder()
	c.h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		c.t.Fatalf("webhook: %d %s", w.Code, w.Body.String())
	}
	filas, err := c.bd.Query(c.ctx, `SELECT id FROM mensajeria.entrantes WHERE canal_id = $1 AND procesado_en IS NULL ORDER BY id`, c.canal)
	if err != nil {
		c.t.Fatal(err)
	}
	ids, err := pgx.CollectRows(filas, pgx.RowTo[int64])
	if err != nil {
		c.t.Fatal(err)
	}
	for _, id := range ids {
		if err := c.m.ProcesarEntrante(c.ctx, id); err != nil {
			c.t.Fatal(err)
		}
	}
}

func (c *cuentaWA) estadoWebhook(metaID, nombre, evento, extra string) {
	c.webhookWA(fmt.Sprintf(`{"object":"whatsapp_business_account","entry":[{"id":%q,"time":1751247548,"changes":[{"field":"message_template_status_update",`+
		`"value":{"event":%q,"message_template_id":%s,"message_template_name":%q,"message_template_language":"es","reason":"NONE"%s}}]}]}`,
		c.waba, evento, metaID, nombre, extra))
}

func (c *cuentaWA) plantilla(id any) map[string]any {
	c.t.Helper()
	r := c.api(c.admin, http.MethodGet, fmt.Sprintf("/plantillas-whatsapp/%v", id), nil)
	c.exigir(r, http.StatusOK)
	return r.cuerpo
}

func (c *cuentaWA) eventos(evento string) []map[string]any {
	c.t.Helper()
	filas, err := c.bd.Query(c.ctx, `SELECT payload FROM integracion.outbox WHERE evento = $1 AND payload->>'waba_id' = $2 ORDER BY id`, evento, c.waba)
	if err != nil {
		c.t.Fatal(err)
	}
	defer filas.Close()
	out := []map[string]any{}
	for filas.Next() {
		var p map[string]any
		if err := filas.Scan(&p); err != nil {
			c.t.Fatal(err)
		}
		out = append(out, p)
	}
	return out
}

func idPW(v map[string]any) int64 { return int64(v["id"].(float64)) }

// Reglas de Meta antes de enviar ---------------------------------------------------------------------

func reglasFallidas(t *testing.T, in mensajeria.EntradaReglasPW) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, r := range mensajeria.ReglasMeta(in) {
		if !r.Ok {
			out[r.Regla] = r.Mensaje
		}
	}
	return out
}

func TestPlantillasWAReglasMeta(t *testing.T) {
	dato := func(s string) *string { return &s }
	base := func() mensajeria.EntradaReglasPW {
		return mensajeria.EntradaReglasPW{Nombre: "recordatorio_pago", Idioma: "es", Categoria: mensajeria.CategoriaUtility,
			Definicion: mensajeria.DefinicionPWA{Cuerpo: "Hola {{cliente_nombre}}, tu factura de {{monto_con_moneda}} vence pronto. ¡Gracias!",
				Variables: []mensajeria.VariablePWA{{Nombre: "cliente_nombre", Dato: dato("cliente_nombre"), Ejemplo: "María López"},
					{Nombre: "monto_con_moneda", Dato: dato("monto_con_moneda"), Ejemplo: "Q 250.00"}}, Botones: []mensajeria.BotonPWA{}}}
	}
	if f := reglasFallidas(t, base()); len(f) != 0 {
		t.Fatalf("una plantilla válida falló: %v", f)
	}
	casos := []struct {
		nombre string
		cambia func(*mensajeria.EntradaReglasPW)
		regla  string
	}{
		{"nombre con mayúsculas", func(e *mensajeria.EntradaReglasPW) { e.Nombre = "Recordatorio" }, "nombre"},
		{"nombre de 513", func(e *mensajeria.EntradaReglasPW) { e.Nombre = strings.Repeat("a", 513) }, "nombre"},
		{"sin cuerpo", func(e *mensajeria.EntradaReglasPW) { e.Definicion.Cuerpo = "  "; e.Definicion.Variables = nil }, "cuerpo"},
		{"cuerpo de 1025", func(e *mensajeria.EntradaReglasPW) {
			e.Definicion.Cuerpo = "Hola " + strings.Repeat("x", 1020)
			e.Definicion.Variables = nil
		}, "largos"},
		{"termina con dato", func(e *mensajeria.EntradaReglasPW) { e.Definicion.Cuerpo = "Hola {{cliente_nombre}}, debes {{monto_con_moneda}}" }, "variables_bordes"},
		{"empieza con dato", func(e *mensajeria.EntradaReglasPW) { e.Definicion.Cuerpo = "{{cliente_nombre}}, debes {{monto_con_moneda}}." }, "variables_bordes"},
		{"datos pegados", func(e *mensajeria.EntradaReglasPW) { e.Definicion.Cuerpo = "Hola {{cliente_nombre}} {{monto_con_moneda}}." }, "variables_seguidas"},
		{"sin ejemplo", func(e *mensajeria.EntradaReglasPW) { e.Definicion.Variables[1].Ejemplo = "" }, "ejemplos"},
		{"dato desconocido", func(e *mensajeria.EntradaReglasPW) { e.Definicion.Variables[0].Dato = dato("saldo_secreto") }, "datos"},
		{"mezcla nombre y número", func(e *mensajeria.EntradaReglasPW) {
			e.Definicion.Cuerpo = "Hola {{1}}, debes {{monto_con_moneda}}."
			e.Definicion.Variables[0].Nombre = "1"
		}, "variables_formato"},
		{"posición fuera de orden", func(e *mensajeria.EntradaReglasPW) {
			e.Definicion.Cuerpo = "Hola {{2}}, debes {{1}}."
			e.Definicion.Variables = []mensajeria.VariablePWA{{Nombre: "1", Ejemplo: "a"}, {Nombre: "2", Ejemplo: "b"}}
		}, "variables_formato"},
		{"encabezado de 61", func(e *mensajeria.EntradaReglasPW) {
			e.Definicion.Encabezado = &mensajeria.EncabezadoPWA{Tipo: "texto", Texto: dato(strings.Repeat("x", 61))}
		}, "largos"},
		{"encabezado con dos datos", func(e *mensajeria.EntradaReglasPW) {
			e.Definicion.Encabezado = &mensajeria.EncabezadoPWA{Tipo: "texto", Texto: dato("Hola {{cliente_nombre}} y {{monto_con_moneda}}")}
		}, "encabezado"},
		{"imagen sin archivo", func(e *mensajeria.EntradaReglasPW) { e.Definicion.Encabezado = &mensajeria.EncabezadoPWA{Tipo: "imagen"} }, "encabezado"},
		{"pie con dato", func(e *mensajeria.EntradaReglasPW) { e.Definicion.Pie = dato("Gracias {{cliente_nombre}}") }, "pie"},
		{"pie de 61", func(e *mensajeria.EntradaReglasPW) { e.Definicion.Pie = dato(strings.Repeat("p", 61)) }, "largos"},
		{"botón de 26", func(e *mensajeria.EntradaReglasPW) {
			e.Definicion.Botones = []mensajeria.BotonPWA{{Tipo: "respuesta", Texto: strings.Repeat("b", 26)}}
		}, "largos"},
		{"11 botones", func(e *mensajeria.EntradaReglasPW) {
			for range 11 {
				e.Definicion.Botones = append(e.Definicion.Botones, mensajeria.BotonPWA{Tipo: "respuesta", Texto: "Sí"})
			}
		}, "botones"},
		{"3 de página", func(e *mensajeria.EntradaReglasPW) {
			for range 3 {
				e.Definicion.Botones = append(e.Definicion.Botones, mensajeria.BotonPWA{Tipo: "url", Texto: "Pagar", URL: dato("https://pagos.kns.gt")})
			}
		}, "botones"},
		{"2 de llamar", func(e *mensajeria.EntradaReglasPW) {
			for range 2 {
				e.Definicion.Botones = append(e.Definicion.Botones, mensajeria.BotonPWA{Tipo: "telefono", Texto: "Llamar", Telefono: dato("+50222001500")})
			}
		}, "botones"},
		{"respuestas separadas", func(e *mensajeria.EntradaReglasPW) {
			e.Definicion.Botones = []mensajeria.BotonPWA{{Tipo: "respuesta", Texto: "Sí"}, {Tipo: "url", Texto: "Pagar", URL: dato("https://pagos.kns.gt")},
				{Tipo: "respuesta", Texto: "No"}}
		}, "botones"},
		{"dato en medio de la URL", func(e *mensajeria.EntradaReglasPW) {
			e.Definicion.Botones = []mensajeria.BotonPWA{{Tipo: "url", Texto: "Pagar", URL: dato("https://pagos.kns.gt/{{cliente_codigo}}/x"), Ejemplo: dato("C1")}}
			e.Definicion.Variables = append(e.Definicion.Variables, mensajeria.VariablePWA{Nombre: "cliente_codigo", Dato: dato("cliente_codigo"), Ejemplo: "C1"})
		}, "botones"},
		{"URL de 2001", func(e *mensajeria.EntradaReglasPW) {
			e.Definicion.Botones = []mensajeria.BotonPWA{{Tipo: "url", Texto: "Pagar", URL: dato("https://pagos.kns.gt/" + strings.Repeat("a", 1990))}}
		}, "largos"},
		{"teléfono de 21", func(e *mensajeria.EntradaReglasPW) {
			e.Definicion.Botones = []mensajeria.BotonPWA{{Tipo: "telefono", Texto: "Llamar", Telefono: dato("+" + strings.Repeat("5", 20))}}
		}, "largos"},
		{"código de 21", func(e *mensajeria.EntradaReglasPW) {
			e.Definicion.Botones = []mensajeria.BotonPWA{{Tipo: "copiar_codigo", Ejemplo: dato(strings.Repeat("1", 21))}}
		}, "largos"},
		{"verificación con enlace", func(e *mensajeria.EntradaReglasPW) {
			e.Categoria = mensajeria.CategoriaAutenticacion
			e.Definicion = mensajeria.DefinicionPWA{Botones: []mensajeria.BotonPWA{{Tipo: "copiar_codigo", Ejemplo: dato("123456")},
				{Tipo: "url", Texto: "Entrar", URL: dato("https://kns.gt")}}}
		}, "autenticacion"},
		{"verificación con emoji", func(e *mensajeria.EntradaReglasPW) {
			e.Categoria = mensajeria.CategoriaAutenticacion
			e.Definicion = mensajeria.DefinicionPWA{Pie: dato("Gracias 😀"), Botones: []mensajeria.BotonPWA{{Tipo: "copiar_codigo", Ejemplo: dato("123456")}}}
		}, "autenticacion"},
		{"verificación con imagen", func(e *mensajeria.EntradaReglasPW) {
			e.Categoria = mensajeria.CategoriaAutenticacion
			e.Definicion = mensajeria.DefinicionPWA{Encabezado: &mensajeria.EncabezadoPWA{Tipo: "imagen"},
				Botones: []mensajeria.BotonPWA{{Tipo: "copiar_codigo", Ejemplo: dato("123456")}}}
		}, "autenticacion"},
	}
	for _, c := range casos {
		e := base()
		c.cambia(&e)
		if f := reglasFallidas(t, e); f[c.regla] == "" {
			t.Errorf("%s: esperaba que fallara %q; fallaron %v", c.nombre, c.regla, f)
		}
	}
	// Las sugeridas de fábrica cumplen todas las reglas.
	for _, s := range mensajeria.Sugeridas {
		if f := reglasFallidas(t, mensajeria.EntradaReglasPW{Nombre: s.Clave, Idioma: "es", Categoria: s.Categoria, Definicion: s.Definicion}); len(f) != 0 {
			t.Errorf("sugerida %s no cumple: %v", s.Clave, f)
		}
	}
	if n := mensajeria.NombreMetaDe("Técnico en camino, ¡ya!"); n != "tecnico_en_camino_ya" {
		t.Fatalf("NombreMetaDe: %q", n)
	}
}

// Crear, enviar, aprobar y que Argos lo use por código ----------------------------------------------

func TestPlantillasWASugeridaEnviarAprobarYUsarPorCodigo(t *testing.T) {
	c := nuevaCuentaWA(t, nil)
	// Antes: el aviso de corte no tiene plantilla en ningún canal para la empresa 1… salvo otros números
	// de otras pruebas; se mira solo esta cuenta por su número.
	r := c.api(c.admin, http.MethodGet, "/plantillas-whatsapp/sugeridas?wabaId="+c.waba, nil)
	c.exigir(r, http.StatusOK, "faltan", "7")

	r = c.apiClave(c.admin, http.MethodPost, "/plantillas-whatsapp/sugeridas/aviso_corte/crear", "sug-"+pruebas.Unico(),
		map[string]any{"wabaId": c.waba, "pie": "Kingnet Solution", "enviar": true})
	c.exigir(r, http.StatusCreated, "nombre", "aviso_corte", "estado", "PENDING", "codigo", "corte_por_falta_de_pago", "origen", "sugerida")
	if r.cuerpo["errorEnvio"] != nil {
		t.Fatalf("errorEnvio: %v", r.cuerpo["errorEnvio"])
	}
	id := idPW(r.cuerpo)
	metaID := r.cuerpo["metaId"].(string)
	// Lo que recibió Meta: nombre, categoría, variables con nombre y sus ejemplos (nunca datos reales).
	creada := c.meta.creadas[0]
	if creada["name"] != "aviso_corte" || creada["category"] != "UTILITY" || creada["parameter_format"] != "named" || creada["language"] != "es" {
		t.Fatalf("creada en Meta: %v", creada)
	}
	comps := cadena(creada["components"])
	for _, s := range []string{`"body_text_named_params"`, `"param_name":"cliente_nombre"`, `"example":"María López"`, `"type":"FOOTER"`, `"text":"Kingnet Solution"`} {
		if !strings.Contains(comps, s) {
			t.Fatalf("components sin %s: %s", s, comps)
		}
	}
	for _, tk := range c.meta.tokens {
		if tk != tokenPrueba {
			t.Fatalf("token a Meta: %q", tk)
		}
	}
	if len(c.eventos("plantilla_whatsapp.creada")) != 1 || len(c.eventos("plantilla_whatsapp.enviada")) != 1 {
		t.Fatal("faltan los eventos creada y enviada")
	}
	// Mientras está en revisión, Argos no la ve aprobada en esta cuenta.
	if c.contar(`SELECT count(*) FROM mensajeria.plantillas_whatsapp WHERE waba_id = $1 AND codigo = 'corte_por_falta_de_pago' AND estado = 'APPROVED'`, c.waba) != 0 {
		t.Fatal("aprobada antes de tiempo")
	}

	// Meta la aprueba (webhook en vivo).
	c.estadoWebhook(metaID, "aviso_corte", "APPROVED", `,"message_template_category":"UTILITY"`)
	p := c.plantilla(id)
	if p["estado"] != "APPROVED" || p["atender"] != nil {
		t.Fatalf("después de aprobar: %v", p)
	}
	ev := c.eventos("plantilla_whatsapp.estado_cambiado")
	if len(ev) != 1 || ev[0]["de"] != "PENDING" || ev[0]["a"] != "APPROVED" || ev[0]["origen"] != "webhook" {
		t.Fatalf("estado_cambiado: %v", ev)
	}
	hist := p["historial"].([]any)
	if len(hist) < 3 || hist[0].(map[string]any)["tipo"] != "estado" || hist[0].(map[string]any)["origen"] != "webhook" {
		t.Fatalf("historial: %v", hist)
	}

	// El aviso de Argos por código sale con esta plantilla y sus datos con nombre.
	aviso := c.avisoPorCodigo("corte_por_falta_de_pago", map[string]string{"monto": "250.00", "moneda": "GTQ"})
	if aviso == nil {
		t.Fatal("el aviso no salió por WhatsApp")
	}
	plantilla := aviso["template"].(map[string]any)
	if plantilla["name"] != "aviso_corte" {
		t.Fatalf("plantilla enviada: %v", plantilla)
	}
	cuerpo := cadena(plantilla["components"])
	for _, s := range []string{`"parameter_name":"monto_con_moneda"`, `"text":"Q 250.00"`, `"parameter_name":"cliente_nombre"`} {
		if !strings.Contains(cuerpo, s) {
			t.Fatalf("parámetros sin %s: %s", s, cuerpo)
		}
	}

	// Meta la pausa por calidad: deja de salir hasta que se reanude.
	c.estadoWebhook(metaID, "aviso_corte", "PAUSED", `,"other_info":{"title":"FIRST_PAUSE","description":"x"}`)
	p = c.plantilla(id)
	if p["estado"] != "PAUSED" || p["pausadaHasta"] == nil || p["atender"] != "pausada" {
		t.Fatalf("pausada: %v", p)
	}
	if c.contar(`SELECT count(*) FROM mensajeria.plantillas_whatsapp WHERE waba_id = $1 AND estado = 'APPROVED' AND codigo IS NOT NULL`, c.waba) != 0 {
		t.Fatal("una pausada sigue usable")
	}
	r = c.apiClave(c.admin, http.MethodPost, fmt.Sprintf("/plantillas-whatsapp/%d/reanudar", id), "rean-"+pruebas.Unico(), map[string]any{"version": p["version"]})
	c.exigir(r, http.StatusOK, "estado", "APPROVED")
	if len(c.meta.reanudadas) != 1 || c.meta.reanudadas[0] != metaID {
		t.Fatalf("unpause: %v", c.meta.reanudadas)
	}
}

// avisoPorCodigo pide un aviso como Argos para un cliente con contacto y devuelve lo que se mandó a
// Meta (nil si no salió).
func (c *cuentaWA) avisoPorCodigo(codigo string, vars map[string]string) map[string]any {
	c.t.Helper()
	cliente := nuevoClienteWA(c.bandeja)
	r := c.integracionAviso(cliente, codigo, vars)
	if r.status != http.StatusAccepted || r.cuerpo["estado"] != "en_cola" {
		c.t.Fatalf("aviso: %d %v", r.status, r.cuerpo)
	}
	antes := len(c.meta.envios)
	c.despacharAvisos()
	c.meta.mu.Lock()
	defer c.meta.mu.Unlock()
	if len(c.meta.envios) == antes {
		return nil
	}
	return c.meta.envios[len(c.meta.envios)-1]
}

// nuevoClienteWA: un cliente de Argos de la empresa 1 con móvil, permiso de contacto y su instantánea
// (nombre) para las variables.
func nuevoClienteWA(b *bandeja) int64 {
	b.t.Helper()
	c := b.cliente(1, "5512-3344")
	b.exigir(b.consentir(c, "acepta", b.reloj.Add(-time.Hour)), http.StatusCreated)
	if _, err := b.bd.Exec(b.ctx, `INSERT INTO integracion.argos_clientes (id, empresa_id, nombre, codigo, sincronizado_en) VALUES ($1, 1, 'María López', 'C-1', now())
		ON CONFLICT (id) DO UPDATE SET nombre = EXCLUDED.nombre`, c); err != nil {
		b.t.Fatal(err)
	}
	return c
}

func (c *cuentaWA) integracionAviso(cliente int64, codigo string, vars map[string]string) respuesta {
	return c.avisar(fmt.Sprintf("argos:aviso:%s:%d", codigo, cliente), map[string]any{"empresa_id": 1, "cliente_id": cliente, "plantilla": codigo,
		"variables": vars, "canal_preferido": "whatsapp", "prioridad": "normal"})
}

// despacharAvisos corre los envíos encolados del número.
func (c *cuentaWA) despacharAvisos() {
	c.t.Helper()
	filas, err := c.bd.Query(c.ctx, `SELECT id::text FROM mensajeria.avisos WHERE canal_id = $1 AND estado = 'en_cola' AND proveedor_id IS NULL`, c.canal)
	if err != nil {
		c.t.Fatal(err)
	}
	ids, err := pgx.CollectRows(filas, pgx.RowTo[string])
	if err != nil {
		c.t.Fatal(err)
	}
	for _, id := range ids {
		c.enviar(id)
	}
}
