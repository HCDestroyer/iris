package mensajeria_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/mensajeria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// graphFalsa es la Graph API para GET /{phone_number_id}: responde lo que diga la prueba. En el
// cuerpo, «%TOKEN%» se cambia por el token recibido (Meta a veces lo repite en el mensaje de error).
type graphFalsa struct {
	srv     *httptest.Server
	mu      sync.Mutex
	status  int
	cuerpo  string
	caida   bool
	tokens  []string
	rutas   []string
	consult int
}

func nuevaGraph(t *testing.T) *graphFalsa {
	g := &graphFalsa{status: http.StatusOK}
	g.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		g.mu.Lock()
		defer g.mu.Unlock()
		g.consult++
		token := strings.TrimPrefix(req.Header.Get("Authorization"), "Bearer ")
		g.tokens = append(g.tokens, token)
		g.rutas = append(g.rutas, req.URL.RequestURI())
		if g.caida {
			hj, ok := w.(http.Hijacker)
			if ok {
				c, _, _ := hj.Hijack()
				_ = c.Close()
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(g.status)
		_, _ = fmt.Fprint(w, strings.ReplaceAll(g.cuerpo, "%TOKEN%", token))
	}))
	t.Cleanup(g.srv.Close)
	return g
}

func (g *graphFalsa) responder(status int, cuerpo string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.status, g.cuerpo, g.caida = status, cuerpo, false
}

const tokenPrueba = "EAAG-token-secreto-de-prueba-0123456789"

// numerosWA prepara la bandeja con la Graph falsa y una URL pública.
func numerosWA(t *testing.T) (*bandeja, *graphFalsa, persona) {
	t.Helper()
	g := nuevaGraph(t)
	b := nuevaBandeja(t, func(c *mensajeria.Config) {
		c.MetaURL = g.srv.URL
		c.BaseURL = "https://iris.ejemplo.gt/"
	})
	admin := b.persona([]int64{1}, rolBase("admin"))
	return b, g, admin
}

func crearNumero(b *bandeja, admin persona, cuerpo map[string]any) respuesta {
	b.t.Helper()
	base := map[string]any{"empresaId": 1, "nombre": "Soporte técnico " + pruebas.Unico(), "numero": "+50222000040",
		"phoneNumberId": fmt.Sprint(time.Now().UnixNano() % 1e13), "token": tokenPrueba, "appSecret": "app-secret-de-prueba-0123", "usos": []string{"soporte"}}
	for k, v := range cuerpo {
		base[k] = v
	}
	return b.api(admin, http.MethodPost, "/canales", base)
}

func cadena(v any) string {
	crudo, _ := json.Marshal(v)
	return string(crudo)
}

// Alta sin verify token: Iris lo genera y lo devuelve una sola vez; la URL del webhook es completa;
// la verificación de Meta queda registrada y ninguna lectura trae credenciales.
func TestNumeroVerifyTokenGeneradoYWebhook(t *testing.T) {
	b, _, admin := numerosWA(t)
	r := b.apiClave(admin, http.MethodPost, "/canales", "alta-numero-"+pruebas.Unico(), map[string]any{"empresaId": 1, "nombre": "Ventas " + pruebas.Unico(),
		"numero": "+50222000031", "phoneNumberId": fmt.Sprint(time.Now().UnixNano() % 1e13), "token": tokenPrueba, "appSecret": "app-secret-de-prueba-0123",
		"usos": []string{"ventas"}})
	b.exigir(r, http.StatusCreated, "conexion", "sin_probar", "estado", "activo", "webhookUrlCompleta", "true")
	vt, _ := r.cuerpo["verifyTokenGenerado"].(string)
	if !strings.HasPrefix(vt, "vt_") || len(vt) != 35 {
		t.Fatalf("verify token generado: %q", vt)
	}
	id := int64(r.cuerpo["id"].(float64))
	if got := r.cuerpo["webhookUrl"]; got != fmt.Sprintf("https://iris.ejemplo.gt/api/v1/mensajeria/canales/%d/webhook", id) {
		t.Fatalf("webhookUrl: %v", got)
	}
	for _, campo := range []string{"tokenGuardadoEn", "appSecretGuardadoEn", "verifyTokenGuardadoEn"} {
		if r.cuerpo[campo] == nil {
			t.Fatalf("%s vacío: %v", campo, r.cuerpo)
		}
	}
	if r.cuerpo["webhookVerificadoEn"] != nil || r.cuerpo["ultimoMensajeEn"] != nil {
		t.Fatalf("webhook sin verificar: %v", r.cuerpo)
	}
	// La respuesta con el verify token no se guardó con la Idempotency-Key.
	if n := b.contar(`SELECT count(*) FROM nucleo.idempotencia WHERE position(convert_to($1, 'UTF8') in coalesce(respuesta, ''::bytea)) > 0`, vt); n != 0 {
		t.Fatal("el verify token quedó guardado en la idempotencia")
	}

	// Ninguna lectura vuelve a traer credenciales ni el verify token.
	ver := b.api(admin, http.MethodGet, fmt.Sprintf("/canales/%d", id), nil)
	b.exigir(ver, http.StatusOK)
	lista := b.api(admin, http.MethodGet, "/canales", nil)
	b.exigir(lista, http.StatusOK)
	for _, s := range []string{cadena(ver.cuerpo), cadena(lista.cuerpo)} {
		for _, secreto := range []string{vt, tokenPrueba, "app-secret-de-prueba-0123", "verifyTokenGenerado"} {
			if strings.Contains(s, secreto) {
				t.Fatalf("una lectura trae %q", secreto)
			}
		}
	}
	if n := b.contar(`SELECT count(*) FROM auditoria.cambios WHERE entidad = 'canal_mensajeria' AND entidad_id = $1 AND despues::text LIKE '%' || $2 || '%'`, fmt.Sprint(id), vt); n != 0 {
		t.Fatal("verify token en la auditoría")
	}

	// Meta verifica con el token generado: queda registrado.
	ruta := fmt.Sprintf("%s/canales/%d/webhook", rutaMsj, id)
	w := httptest.NewRecorder()
	b.h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, ruta+"?hub.mode=subscribe&hub.verify_token="+vt+"&hub.challenge=777", nil))
	if w.Code != http.StatusOK || w.Body.String() != "777" {
		t.Fatalf("verificación: %d %q", w.Code, w.Body.String())
	}
	ver = b.api(admin, http.MethodGet, fmt.Sprintf("/canales/%d", id), nil)
	if ver.cuerpo["webhookVerificadoEn"] == nil {
		t.Fatalf("verificación sin registrar: %v", ver.cuerpo)
	}
	// Una verificación con otro token no cuenta.
	var antes time.Time
	if err := b.bd.QueryRow(b.ctx, `SELECT webhook_verificado_en FROM mensajeria.canales WHERE id = $1`, id).Scan(&antes); err != nil {
		t.Fatal(err)
	}
	*b.reloj = b.reloj.Add(time.Hour)
	w = httptest.NewRecorder()
	b.h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, ruta+"?hub.mode=subscribe&hub.verify_token=otro-token-0123456789&hub.challenge=1", nil))
	if w.Code != http.StatusForbidden {
		t.Fatalf("verify token ajeno: %d", w.Code)
	}
	var despues time.Time
	if err := b.bd.QueryRow(b.ctx, `SELECT webhook_verificado_en FROM mensajeria.canales WHERE id = $1`, id).Scan(&despues); err != nil {
		t.Fatal(err)
	}
	if !despues.Equal(antes) {
		t.Fatal("una verificación fallida cambió webhook_verificado_en")
	}

	// «Generar otro»: nuevo token una vez, y Meta debe verificar otra vez.
	act := b.api(admin, http.MethodPost, fmt.Sprintf("/canales/%d/actualizar", id), map[string]any{"version": ver.cuerpo["version"],
		"nombre": ver.cuerpo["nombre"], "numero": ver.cuerpo["numero"], "phoneNumberId": ver.cuerpo["phoneNumberId"], "generarVerifyToken": true, "plantillas": map[string]any{}})
	b.exigir(act, http.StatusOK)
	nuevo, _ := act.cuerpo["verifyTokenGenerado"].(string)
	if !strings.HasPrefix(nuevo, "vt_") || nuevo == vt || act.cuerpo["webhookVerificadoEn"] != nil || act.cuerpo["tokenGuardadoEn"] != ver.cuerpo["tokenGuardadoEn"] {
		t.Fatalf("generar otro: %v", act.cuerpo)
	}
	if act.cuerpo["verifyTokenGuardadoEn"] == ver.cuerpo["verifyTokenGuardadoEn"] {
		t.Fatalf("verifyTokenGuardadoEn no cambió: %v", act.cuerpo)
	}
	// Generar y mandar uno a la vez: 422.
	b.exigir(b.api(admin, http.MethodPost, fmt.Sprintf("/canales/%d/actualizar", id), map[string]any{"version": act.cuerpo["version"],
		"nombre": ver.cuerpo["nombre"], "numero": ver.cuerpo["numero"], "phoneNumberId": ver.cuerpo["phoneNumberId"], "generarVerifyToken": true,
		"verifyToken": "verify-token-propio-0123", "plantillas": map[string]any{}}), http.StatusUnprocessableEntity)
	// Uno propio sigue valiendo al crear, y no se devuelve.
	propio := crearNumero(b, admin, map[string]any{"verifyToken": "verify-token-propio-0123"})
	b.exigir(propio, http.StatusCreated)
	if _, ok := propio.cuerpo["verifyTokenGenerado"]; ok {
		t.Fatalf("devolvió un verify token que mandó el cliente: %v", propio.cuerpo)
	}
	b.exigir(crearNumero(b, admin, map[string]any{"verifyToken": "corto"}), http.StatusUnprocessableEntity)
}

// Repetir la Idempotency-Key de un alta con verify token generado: 409, sin el token.
func TestNumeroAltaNoRepetible(t *testing.T) {
	b, _, admin := numerosWA(t)
	cuerpo := map[string]any{"empresaId": 1, "nombre": "Cobros " + pruebas.Unico(), "numero": "+50222000022",
		"phoneNumberId": fmt.Sprint(time.Now().UnixNano() % 1e13), "token": tokenPrueba, "appSecret": "app-secret-de-prueba-0123", "usos": []string{"cobros"}}
	clave := "alta-" + pruebas.Unico()
	r := b.apiClave(admin, http.MethodPost, "/canales", clave, cuerpo)
	b.exigir(r, http.StatusCreated)
	rep := b.apiClave(admin, http.MethodPost, "/canales", clave, cuerpo)
	b.exigir(rep, http.StatusConflict, "type", "urn:iris:problema:idempotencia_no_repetible")
	if strings.Contains(cadena(rep.cuerpo), r.cuerpo["verifyTokenGenerado"].(string)) {
		t.Fatal("la repetición trae el verify token")
	}
}

// Cada estado de la prueba, sin filtrar el token, con auditoría y evento.
func TestProbarNumero(t *testing.T) {
	b, g, admin := numerosWA(t)
	r := crearNumero(b, admin, nil)
	b.exigir(r, http.StatusCreated)
	id := int64(r.cuerpo["id"].(float64))
	pnid := r.cuerpo["phoneNumberId"].(string)
	version := r.cuerpo["version"]
	ruta := fmt.Sprintf("/canales/%d/probar", id)
	probar := func(status int, cuerpo string) respuesta {
		t.Helper()
		g.responder(status, cuerpo)
		p := b.api(admin, http.MethodPost, ruta, nil)
		b.exigir(p, http.StatusOK)
		if s := cadena(p.cuerpo); strings.Contains(s, tokenPrueba) || strings.Contains(s, "EAAG") {
			t.Fatalf("la respuesta trae el token: %s", s)
		}
		return p
	}
	ok := fmt.Sprintf(`{"id":%q,"display_phone_number":"+502 2200 0040","verified_name":"Red Sur Soporte","quality_rating":"GREEN"}`, pnid)

	// Conectado, pero Meta aún no verificó el webhook.
	p := probar(http.StatusOK, ok)
	b.exigir(p, http.StatusOK, "estado", "conectado", "conexion", "webhook_sin_verificar", "webhookVerificado", "false",
		"calidad", "alta", "nombreVerificado", "Red Sur Soporte", "numeroMeta", "+50222000040", "detalle", "<nil>")
	g.mu.Lock()
	if g.tokens[len(g.tokens)-1] != tokenPrueba || !strings.HasPrefix(g.rutas[len(g.rutas)-1], "/"+pnid+"?fields=display_phone_number,verified_name,quality_rating") {
		t.Fatalf("consulta a Meta: %v %v", g.rutas, len(g.tokens))
	}
	g.mu.Unlock()
	if n := b.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'canal_mensajeria.probado' AND payload->>'canal_id' = $1 AND payload->>'estado' = 'conectado' AND payload->>'empresa_id' = '1'`, fmt.Sprint(id)); n != 1 {
		t.Fatalf("eventos canal_mensajeria.probado: %d", n)
	}
	// Con el webhook verificado: conectado.
	if _, err := b.bd.Exec(b.ctx, `UPDATE mensajeria.canales SET webhook_verificado_en = now() WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	p = probar(http.StatusOK, ok)
	b.exigir(p, http.StatusOK, "estado", "conectado", "conexion", "conectado", "webhookVerificado", "true")

	casos := []struct {
		nombre, estado string
		status         int
		cuerpo         string
	}{
		{"token vencido", "token_invalido", http.StatusUnauthorized, `{"error":{"message":"Error validating access token: %TOKEN% expired","type":"OAuthException","code":190,"error_subcode":463}}`},
		{"token mal copiado", "token_invalido", http.StatusBadRequest, `{"error":{"message":"Malformed access token %TOKEN%","type":"OAuthException","code":190}}`},
		{"otro número", "numero_no_coincide", http.StatusOK, fmt.Sprintf(`{"id":%q,"display_phone_number":"+502 5555 1234","verified_name":"Otra","quality_rating":"YELLOW"}`, pnid)},
		{"otro id", "numero_no_coincide", http.StatusOK, `{"id":"999","display_phone_number":"+502 2200 0040"}`},
		{"id ajeno", "numero_no_coincide", http.StatusBadRequest, `{"error":{"message":"Unsupported get request. Object with ID does not exist (token %TOKEN%)","code":100,"error_subcode":33}}`},
		{"sin permiso 403", "sin_permiso", http.StatusForbidden, `{"error":{"message":"%TOKEN% no permission","code":200}}`},
		{"sin permiso 10", "sin_permiso", http.StatusBadRequest, `{"error":{"message":"Application does not have permission %TOKEN%","code":10}}`},
		{"Meta caída", "error_meta", http.StatusServiceUnavailable, `{"error":{"message":"Service temporarily unavailable %TOKEN%","code":2}}`},
		{"cuerpo raro", "error_meta", http.StatusOK, `<html>%TOKEN%</html>`},
		{"otro error", "error_meta", http.StatusBadRequest, `{"error":{"message":"Invalid parameter %TOKEN%","code":100}}`},
	}
	for _, c := range casos {
		p := probar(c.status, c.cuerpo)
		if p.cuerpo["estado"] != c.estado || p.cuerpo["conexion"] != c.estado {
			t.Fatalf("%s: %v", c.nombre, p.cuerpo)
		}
		d, _ := p.cuerpo["detalle"].(string)
		if d == "" || strings.Contains(d, tokenPrueba) {
			t.Fatalf("%s: detalle %q", c.nombre, d)
		}
	}
	// Sin respuesta de Meta.
	g.mu.Lock()
	g.caida = true
	g.mu.Unlock()
	p = b.api(admin, http.MethodPost, ruta, nil)
	b.exigir(p, http.StatusOK, "estado", "error_meta")

	// El resultado queda en el número (sin subir version) y en la lista.
	ver := b.api(admin, http.MethodGet, fmt.Sprintf("/canales/%d", id), nil)
	b.exigir(ver, http.StatusOK, "ultimoEstado", "error_meta", "conexion", "error_meta", "version", fmt.Sprint(version))
	if ver.cuerpo["ultimaPruebaEn"] == nil || ver.cuerpo["ultimoDetalle"] == nil {
		t.Fatalf("prueba sin guardar: %v", ver.cuerpo)
	}
	// Nada del token en la base, la auditoría ni el outbox.
	if n := b.contar(`SELECT count(*) FROM mensajeria.canales WHERE id = $1 AND position($2 in coalesce(ultimo_detalle, '')) > 0`, id, tokenPrueba); n != 0 {
		t.Fatal("token en el detalle guardado")
	}
	if n := b.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'mensajeria.probar_canal' AND resultado = 'ok' AND entidad_id = $1`, fmt.Sprint(id)); n != len(casos)+3 {
		t.Fatalf("pruebas auditadas: %d", n)
	}
	if n := b.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'mensajeria.probar_canal' AND (entrada::text LIKE '%' || $1 || '%' OR coalesce(error::text, '') LIKE '%' || $1 || '%')`, tokenPrueba); n != 0 {
		t.Fatal("token en la auditoría")
	}
	if n := b.contar(`SELECT count(*) FROM integracion.outbox WHERE payload::text LIKE '%' || $1 || '%'`, tokenPrueba); n != 0 {
		t.Fatal("token en el outbox")
	}

	// Otro token deja el número sin probar y cambia tokenGuardadoEn.
	*b.reloj = b.reloj.Add(time.Minute)
	act := b.api(admin, http.MethodPost, fmt.Sprintf("/canales/%d/actualizar", id), map[string]any{"version": ver.cuerpo["version"],
		"nombre": ver.cuerpo["nombre"], "numero": ver.cuerpo["numero"], "phoneNumberId": pnid, "token": "EAAG-otro-token-de-prueba-0123456789", "plantillas": map[string]any{}})
	b.exigir(act, http.StatusOK, "conexion", "sin_probar", "ultimoEstado", "<nil>", "ultimaPruebaEn", "<nil>")
	if act.cuerpo["tokenGuardadoEn"] == ver.cuerpo["tokenGuardadoEn"] || act.cuerpo["appSecretGuardadoEn"] != ver.cuerpo["appSecretGuardadoEn"] {
		t.Fatalf("guardadoEn: %v → %v", ver.cuerpo, act.cuerpo)
	}
	// Desactivado manda sobre la última prueba.
	des := b.api(admin, http.MethodPost, fmt.Sprintf("/canales/%d/desactivar", id), nil)
	b.exigir(des, http.StatusOK, "conexion", "desactivado", "estado", "inactivo")
}

// Probar es solo de personas con sesión y con mensajeria.configurar, y de su empresa.
func TestProbarNumeroSoloPersonas(t *testing.T) {
	b, g, admin := numerosWA(t)
	r := crearNumero(b, admin, nil)
	b.exigir(r, http.StatusCreated)
	ruta := fmt.Sprintf("/canales/%v/probar", r.cuerpo["id"])
	g.responder(http.StatusOK, `{}`)
	antes := g.consult
	// Token de API de un administrador: 404.
	tk := b.entorno.persona([]int64{1}, rolBase("admin"))
	b.exigir(b.api(tk, http.MethodPost, ruta, nil), http.StatusNotFound)
	// Integración: sin el permiso.
	integ := b.integracion("integracion_argos")
	if s := b.api(integ, http.MethodPost, ruta, nil).status; s != http.StatusNotFound && s != http.StatusForbidden {
		t.Fatalf("integración: %d", s)
	}
	// Sin mensajeria.configurar: 403.
	b.exigir(b.api(b.agente, http.MethodPost, ruta, nil), http.StatusForbidden)
	// Otra empresa: 404.
	codigo := fmt.Sprintf("numeros_wa_%d", time.Now().UnixNano()%1_000_000_000)
	if _, err := identChat.CrearRol.Ejecutar(b.ctx, identidad.EntradaCrearRol{Codigo: codigo, Nombre: "Números " + pruebas.Unico(),
		Permisos: []nucleo.Permiso{nucleo.PermMensajeriaConfigurar}}); err != nil {
		t.Fatal(err)
	}
	otra := b.persona([]int64{2}, rolBase(codigo))
	b.exigir(b.api(otra, http.MethodPost, ruta, nil), http.StatusNotFound)
	b.exigir(b.api(otra, http.MethodGet, fmt.Sprintf("/canales/%v", r.cuerpo["id"]), nil), http.StatusNotFound)
	b.exigir(b.api(admin, http.MethodPost, "/canales/999999999/probar", nil), http.StatusNotFound)
	if g.consult != antes {
		t.Fatal("se consultó a Meta sin autorización")
	}
}

// Un webhook firmado actualiza ultimoMensajeEn (con precisión de 1 minuto).
func TestNumeroUltimoMensaje(t *testing.T) {
	b, _, admin := numerosWA(t)
	ruta := fmt.Sprintf("/canales/%d", b.canal.ID)
	if v := b.api(admin, http.MethodGet, ruta, nil).cuerpo["ultimoMensajeEn"]; v != nil {
		t.Fatalf("ultimoMensajeEn inicial: %v", v)
	}
	b.texto(numeroCliente(), "wamid.um1", "Hola")
	v1 := b.api(admin, http.MethodGet, ruta, nil).cuerpo["ultimoMensajeEn"]
	if v1 == nil {
		t.Fatal("ultimoMensajeEn sin registrar")
	}
	*b.reloj = b.reloj.Add(30 * time.Second)
	b.texto(numeroCliente(), "wamid.um2", "Hola otra vez")
	if v := b.api(admin, http.MethodGet, ruta, nil).cuerpo["ultimoMensajeEn"]; v != v1 {
		t.Fatalf("antes del minuto cambió: %v → %v", v1, v)
	}
	*b.reloj = b.reloj.Add(2 * time.Minute)
	b.texto(numeroCliente(), "wamid.um3", "Y otra")
	if v := b.api(admin, http.MethodGet, ruta, nil).cuerpo["ultimoMensajeEn"]; v == v1 {
		t.Fatal("ultimoMensajeEn no avanzó")
	}
	// En la lista también.
	lista := b.api(admin, http.MethodGet, "/canales", nil)
	b.exigir(lista, http.StatusOK)
	for _, c := range lista.cuerpo["data"].([]any) {
		cm := c.(map[string]any)
		if int64(cm["id"].(float64)) == b.canal.ID && cm["ultimoMensajeEn"] == nil {
			t.Fatalf("la lista no trae ultimoMensajeEn: %v", cm)
		}
	}
}

// Varios números por empresa: crear otro número de avisos pasa la marca; un phone number ID activo
// repetido es canal_existente.
func TestNumerosVariosPorEmpresa(t *testing.T) {
	b, _, admin := numerosWA(t)
	a := crearNumero(b, admin, map[string]any{"usos": []string{"avisos", "soporte"}, "avisosEmpresa": true})
	b.exigir(a, http.StatusCreated, "avisosEmpresa", "true")
	c := crearNumero(b, admin, map[string]any{"usos": []string{"avisos"}, "avisosEmpresa": true})
	b.exigir(c, http.StatusCreated, "avisosEmpresa", "true")
	b.exigir(b.api(admin, http.MethodGet, fmt.Sprintf("/canales/%v", a.cuerpo["id"]), nil), http.StatusOK, "avisosEmpresa", "false", "estado", "activo")
	dup := crearNumero(b, admin, map[string]any{"phoneNumberId": c.cuerpo["phoneNumberId"]})
	b.exigir(dup, http.StatusConflict, "type", "urn:iris:problema:canal_existente")
	if d, _ := dup.cuerpo["detail"].(string); !strings.Contains(d, "phone number ID") {
		t.Fatalf("detalle de canal_existente: %v", dup.cuerpo)
	}
}
