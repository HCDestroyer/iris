package mensajeria_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/mensajeria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// Conexión con Meta, WhatsApp por QR (coexistencia) y préstamo de números
// (contratos/conversaciones.md §15): pruebas contra Postgres real y una Graph API falsa.

const (
	appIDQR      = "1298475561023344"
	appSecretQR  = "app-secret-de-la-instalacion-0123"
	configIDQR   = "889900112233"
	tokenNegocio = "EAAQR-token-del-negocio-0123456789"
)

// grafoQR es la Graph API para el registro con QR: canje, número, suscripción, smb_app_data, app.
type grafoQR struct {
	srv      *httptest.Server
	mu       sync.Mutex
	llamadas []string
	cuerpos  map[string][]string
	// Respuestas (status, cuerpo) por paso; vacías = las de éxito.
	canje, numero, suscribir, sync, app, wabaNums [2]any
	enAppNegocio                                  bool
}

func nuevoGrafoQR(t *testing.T) *grafoQR {
	g := &grafoQR{cuerpos: map[string][]string{}, enAppNegocio: true}
	g.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		defer g.mu.Unlock()
		ruta := strings.TrimPrefix(r.URL.Path, "/v21.0")
		crudo, _ := io.ReadAll(r.Body)
		clave := r.Method + " " + ruta
		g.llamadas = append(g.llamadas, clave)
		g.cuerpos[clave] = append(g.cuerpos[clave], string(crudo))
		responder := func(def [2]any, status int, cuerpo string) {
			if def[0] != nil {
				status, cuerpo = def[0].(int), def[1].(string)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = io.WriteString(w, cuerpo)
		}
		partes := strings.Split(strings.Trim(ruta, "/"), "/")
		switch {
		case ruta == "/oauth/access_token":
			// GET con los parámetros en la consulta, como lo documenta Meta.
			q := r.URL.Query()
			if r.Method != http.MethodGet || q.Get("client_secret") != appSecretQR || q.Get("client_id") != appIDQR || q.Get("code") == "" {
				responder([2]any{}, 400, `{"error":{"code":101,"type":"OAuthException"}}`)
				return
			}
			responder(g.canje, 200, `{"access_token":"`+tokenNegocio+`","token_type":"bearer"}`)
		case len(partes) == 2 && partes[1] == "phone_numbers":
			// Las pruebas usan wabaId = "9" + pnid[1:]: la WABA tiene esos números.
			ids := []string{}
			for d := 0; d <= 9; d++ {
				ids = append(ids, `{"id":"`+fmt.Sprint(d)+partes[0][1:]+`"}`)
			}
			responder(g.wabaNums, 200, `{"data":[`+strings.Join(ids, ",")+`]}`)
		case len(partes) == 2 && partes[1] == "subscribed_apps":
			responder(g.suscribir, 200, `{"success":true}`)
		case len(partes) == 2 && partes[1] == "smb_app_data":
			responder(g.sync, 200, `{"request_id":"req-`+fmt.Sprint(len(g.llamadas))+`"}`)
		case len(partes) == 2 && partes[1] == "messages":
			responder([2]any{}, 200, `{"messages":[{"id":"wamid.`+uuid.NewString()+`"}]}`)
		case len(partes) == 1 && r.URL.Query().Get("fields") == "id,name":
			// El token de la app en access_token, como lo documenta Meta.
			if r.URL.Query().Get("access_token") != appIDQR+"|"+appSecretQR {
				responder([2]any{}, 400, `{"error":{"code":190,"type":"OAuthException","message":"Invalid token `+r.URL.Query().Get("access_token")+`"}}`)
				return
			}
			responder(g.app, 200, `{"id":"`+partes[0]+`","name":"Red Sur Iris"}`)
		case len(partes) == 1 && r.Method == http.MethodGet:
			biz := "true"
			if !g.enAppNegocio {
				biz = "false"
			}
			responder(g.numero, 200, `{"id":"`+partes[0]+`","display_phone_number":"+502 5512 3344","verified_name":"Red Sur Ventas","is_on_biz_app":`+biz+`,"platform_type":"CLOUD_API"}`)
		default:
			responder([2]any{}, 404, `{"error":{"code":100}}`)
		}
	}))
	t.Cleanup(g.srv.Close)
	return g
}

func (g *grafoQR) cuenta(prefijo, sufijo string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	n := 0
	for _, l := range g.llamadas {
		if strings.HasPrefix(l, prefijo) && strings.HasSuffix(l, sufijo) {
			n++
		}
	}
	return n
}

func (g *grafoQR) responder(paso *[2]any, status int, cuerpo string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	*paso = [2]any{status, cuerpo}
}

func (g *grafoQR) exito(paso *[2]any) {
	g.mu.Lock()
	defer g.mu.Unlock()
	*paso = [2]any{}
}

// entornoQR: la bandeja (empresa 1) con la Graph falsa, una URL pública y el admin.
func entornoQR(t *testing.T, ajustar func(*mensajeria.Config)) (*bandeja, *grafoQR, persona) {
	t.Helper()
	g := nuevoGrafoQR(t)
	b := nuevaBandeja(t, func(c *mensajeria.Config) {
		c.MetaURL = g.srv.URL + "/v21.0"
		c.BaseURL = "https://iris.ejemplo.gt/"
		if ajustar != nil {
			ajustar(c)
		}
	})
	// La conexión es una por instalación y los números QR de otras pruebas no cuentan.
	if _, err := b.bd.Exec(b.ctx, `DELETE FROM mensajeria.conexion_meta`); err != nil {
		t.Fatal(err)
	}
	if _, err := b.bd.Exec(b.ctx, `UPDATE mensajeria.canales SET activo = false WHERE tipo_conexion = 'qr'`); err != nil {
		t.Fatal(err)
	}
	admin := b.persona([]int64{1}, rolBase("admin"))
	admin.empresa = 1
	return b, g, admin
}

// conectarMeta guarda la conexión propia y devuelve el verify token del webhook de la app.
func conectarMeta(b *bandeja, admin persona) string {
	b.t.Helper()
	r := b.api(admin, http.MethodPost, "/conexion-meta/actualizar", map[string]any{"version": 0, "modo": "propia", "appId": appIDQR,
		"appSecret": appSecretQR, "configId": configIDQR})
	b.exigir(r, http.StatusOK, "modo", "propia", "qrDisponible", "true", "estado", "sin_probar")
	vt, _ := r.cuerpo["verifyTokenGenerado"].(string)
	if !strings.HasPrefix(vt, "vt_") {
		b.t.Fatalf("verify token de la app: %v", r.cuerpo)
	}
	return vt
}

func pnidNuevo() string { return fmt.Sprint(time.Now().UnixNano() % 1e13) }

func conectarQR(b *bandeja, admin persona, pnid, codigo, clave string) respuesta {
	b.t.Helper()
	return b.apiClave(admin, http.MethodPost, "/canales/qr", clave, map[string]any{"code": codigo, "wabaId": "9" + pnid[1:], "phoneNumberId": pnid,
		"nombre": "Ventas tienda " + pnid, "usos": []string{"ventas"}})
}

// webhookApp manda un cuerpo firmado al webhook de la app y procesa los entrantes nuevos.
func (b *bandeja) webhookApp(cuerpo string) respuesta {
	b.t.Helper()
	req := httptest.NewRequest(http.MethodPost, rutaMsj+"/meta/webhook", strings.NewReader(cuerpo))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hub-Signature-256", mensajeria.FirmarMeta(appSecretQR, []byte(cuerpo)))
	w := httptest.NewRecorder()
	b.h.ServeHTTP(w, req)
	r := respuesta{status: w.Code}
	_ = json.Unmarshal(w.Body.Bytes(), &r.cuerpo)
	b.procesarPendientes()
	return r
}

func (b *bandeja) procesarPendientes() {
	b.t.Helper()
	filas, err := b.bd.Query(b.ctx, `SELECT id FROM mensajeria.entrantes WHERE procesado_en IS NULL ORDER BY id`)
	if err != nil {
		b.t.Fatal(err)
	}
	ids, err := pgx.CollectRows(filas, pgx.RowTo[int64])
	if err != nil {
		b.t.Fatal(err)
	}
	for _, id := range ids {
		if err := b.m.ProcesarEntrante(b.ctx, id); err != nil {
			b.t.Fatal(err)
		}
	}
}

func (b *bandeja) contarOutbox(evento, campo, valor string) int {
	b.t.Helper()
	return b.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = $1 AND payload->>`+campo+` = $2`, evento, valor)
}

func sinSecretos(t *testing.T, donde string, secretos ...string) {
	t.Helper()
	for _, s := range secretos {
		if s != "" && strings.Contains(donde, s) {
			t.Fatalf("aparece un secreto (%q…)", s[:min(6, len(s))])
		}
	}
}

// Conexión con Meta -------------------------------------------------------------------------------

// Solo el admin cambia la conexión; los secretos nunca salen; probar valida app ID y secreto.
func TestConexionMetaSoloAdminYSinSecretos(t *testing.T) {
	b, _, admin := entornoQR(t, nil)
	// Sin configurar.
	r := b.api(admin, http.MethodGet, "/conexion-meta", nil)
	b.exigir(r, http.StatusOK, "modo", "sin_configurar", "qrDisponible", "false", "motivoQr", "sin_configurar")
	if r.cuerpo["webhookUrl"] != "https://iris.ejemplo.gt/api/v1/mensajeria/meta/webhook" {
		t.Fatalf("webhookUrl: %v", r.cuerpo["webhookUrl"])
	}
	b.exigir(b.api(admin, http.MethodPost, "/conexion-meta/probar", nil), http.StatusUnprocessableEntity)
	// Supervisor (sin mensajeria.conexion_meta): 403 al leer y al cambiar.
	b.exigir(b.api(b.supervisor, http.MethodGet, "/conexion-meta", nil), http.StatusForbidden)
	b.exigir(b.api(b.supervisor, http.MethodPost, "/conexion-meta/actualizar", map[string]any{"version": 0, "modo": "propia", "appId": appIDQR,
		"appSecret": appSecretQR, "configId": configIDQR}), http.StatusForbidden)
	// Un token de API (aunque sea del admin): 404.
	tk := admin
	tk.cookie = ""
	b.exigir(b.api(tk, http.MethodGet, "/conexion-meta", nil), http.StatusNotFound)

	// Propia: la primera vez el secreto es obligatorio.
	b.exigir(b.api(admin, http.MethodPost, "/conexion-meta/actualizar", map[string]any{"version": 0, "modo": "propia", "appId": appIDQR,
		"configId": configIDQR}), http.StatusUnprocessableEntity)
	r = b.apiClave(admin, http.MethodPost, "/conexion-meta/actualizar", "conexion-"+pruebas.Unico(), map[string]any{"version": 0, "modo": "propia",
		"appId": appIDQR, "appSecret": appSecretQR, "configId": configIDQR})
	b.exigir(r, http.StatusOK, "modo", "propia", "estado", "sin_probar", "qrDisponible", "true", "graphVersion", "v21.0")
	vt := r.cuerpo["verifyTokenGenerado"].(string)
	if r.cuerpo["appSecretGuardadoEn"] == nil || r.cuerpo["verifyTokenGuardadoEn"] == nil {
		t.Fatalf("guardadoEn: %v", r.cuerpo)
	}
	// Versión vieja: 409.
	b.exigir(b.api(admin, http.MethodPost, "/conexion-meta/actualizar", map[string]any{"version": 0, "modo": "propia", "appId": appIDQR,
		"configId": configIDQR}), http.StatusConflict)
	ver := b.api(admin, http.MethodGet, "/conexion-meta", nil)
	b.exigir(ver, http.StatusOK, "version", "1")
	if _, ok := ver.cuerpo["verifyTokenGenerado"]; ok {
		t.Fatal("el GET trae el verify token")
	}
	// Ningún secreto en respuestas, auditoría, outbox ni idempotencia.
	sinSecretos(t, cadena(ver.cuerpo), appSecretQR, vt)
	var aud, out, idem string
	if err := b.bd.QueryRow(b.ctx, `SELECT coalesce(string_agg(entrada::text || coalesce(resultado::text, ''), ''), '') FROM auditoria.acciones WHERE accion LIKE 'mensajeria.%conexion%'`).Scan(&aud); err != nil {
		t.Fatal(err)
	}
	if err := b.bd.QueryRow(b.ctx, `SELECT coalesce(string_agg(payload::text, ''), '') FROM integracion.outbox WHERE evento LIKE 'meta.%'`).Scan(&out); err != nil {
		t.Fatal(err)
	}
	if err := b.bd.QueryRow(b.ctx, `SELECT coalesce(string_agg(encode(respuesta, 'escape'), ''), '') FROM nucleo.idempotencia`).Scan(&idem); err != nil {
		t.Fatal(err)
	}
	sinSecretos(t, aud+out+idem, appSecretQR, vt)
	if n := b.contar(`SELECT count(*) FROM auditoria.cambios WHERE entidad = 'conexion_meta' AND campo = 'app_secret' AND despues = '"***"'::jsonb`); n == 0 {
		t.Fatal("la auditoría no marca el app secret como ***")
	}
	if n := b.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'meta.conexion_cambiada' AND payload->>'modo' = 'propia'`); n == 0 {
		t.Fatal("sin meta.conexion_cambiada")
	}

	// Probar: Meta acepta la app.
	p := b.api(admin, http.MethodPost, "/conexion-meta/probar", nil)
	b.exigir(p, http.StatusOK, "estado", "lista")
	comp := p.cuerpo["comprobaciones"].([]any)
	if len(comp) != 4 || comp[0].(map[string]any)["estado"] != "ok" || comp[2].(map[string]any)["estado"] != "advertencia" ||
		comp[3].(map[string]any)["estado"] != "sin_verificar" {
		t.Fatalf("comprobaciones: %v", comp)
	}
	if b.contarOutbox("meta.conexion_probada", "'estado'", "lista") == 0 {
		t.Fatal("sin meta.conexion_probada")
	}
	// Otro secreto: Meta no lo acepta (y el mensaje de Meta, que repite el token, no se copia).
	r = b.api(admin, http.MethodPost, "/conexion-meta/actualizar", map[string]any{"version": 1, "modo": "propia", "appId": appIDQR,
		"appSecret": "otro-secreto-que-no-sirve-01", "configId": configIDQR})
	b.exigir(r, http.StatusOK, "estado", "sin_probar")
	p = b.api(admin, http.MethodPost, "/conexion-meta/probar", nil)
	b.exigir(p, http.StatusOK, "estado", "error")
	sinSecretos(t, cadena(p.cuerpo), "otro-secreto-que-no-sirve-01")

	// Webhook de la app: Meta verifica con el verify token de la conexión.
	w := httptest.NewRecorder()
	b.h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, rutaMsj+"/meta/webhook?hub.mode=subscribe&hub.verify_token="+vt+"&hub.challenge=4242", nil))
	if w.Code != http.StatusOK || w.Body.String() != "4242" {
		t.Fatalf("verificación: %d %q", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	b.h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, rutaMsj+"/meta/webhook?hub.mode=subscribe&hub.verify_token=otro-0123456789abcdef&hub.challenge=1", nil))
	if w.Code != http.StatusForbidden {
		t.Fatalf("verify token ajeno: %d", w.Code)
	}
	if v := b.api(admin, http.MethodGet, "/conexion-meta", nil); v.cuerpo["webhookVerificadoEn"] == nil {
		t.Fatalf("verificación sin registrar: %v", v.cuerpo)
	}
	// Firma con otro secreto: 401.
	req := httptest.NewRequest(http.MethodPost, rutaMsj+"/meta/webhook", strings.NewReader(`{"entry":[]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hub-Signature-256", mensajeria.FirmarMeta("no-es-el-secreto-0123", []byte(`{"entry":[]}`)))
	w = httptest.NewRecorder()
	b.h.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("firma ajena: %d", w.Code)
	}

	// Datos para la ventana de Meta (quien configura números): nunca el secreto.
	reg := b.api(b.supervisor, http.MethodGet, "/conexion-meta/registro", nil)
	b.exigir(reg, http.StatusForbidden) // el supervisor no tiene mensajeria.configurar
	reg = b.api(admin, http.MethodGet, "/conexion-meta/registro", nil)
	b.exigir(reg, http.StatusOK, "disponible", "true", "appId", appIDQR, "configId", configIDQR, "featureType", "whatsapp_business_app_onboarding",
		"sessionInfoVersion", "3")
	sinSecretos(t, cadena(reg.cuerpo), appSecretQR, "otro-secreto-que-no-sirve-01")

	// Compartida: se guarda la clave, el estado dice que falta el servicio central y el QR no va.
	r = b.api(admin, http.MethodPost, "/conexion-meta/actualizar", map[string]any{"version": 2, "modo": "compartida", "claveEnlace": "clave-de-enlace-horus-0123456"})
	b.exigir(r, http.StatusOK, "modo", "compartida", "estado", "sin_servicio_central", "qrDisponible", "false", "motivoQr", "servicio_central_no_disponible",
		"servicioUrl", mensajeria.ServicioCentralPorDefecto)
	sinSecretos(t, cadena(r.cuerpo), "clave-de-enlace-horus-0123456")
	b.exigir(b.api(admin, http.MethodPost, "/conexion-meta/probar", nil), http.StatusOK, "estado", "sin_servicio_central")
	q := b.api(admin, http.MethodPost, "/canales/qr", map[string]any{"code": "AQB-" + pruebas.Unico(), "wabaId": "1", "phoneNumberId": pnidNuevo(), "nombre": "X"})
	b.exigir(q, http.StatusUnprocessableEntity)
	if !strings.HasSuffix(fmt.Sprint(q.cuerpo["type"]), "servicio_central_no_disponible") {
		t.Fatalf("QR en compartida: %v", q.cuerpo)
	}
	b.exigir(b.api(admin, http.MethodGet, "/conexion-meta/registro", nil), http.StatusOK, "disponible", "false", "motivo", "servicio_central_no_disponible")
}

// Iris en la nube: la conexión la fija el entorno y no se cambia.
func TestConexionMetaSaaS(t *testing.T) {
	b, g, admin := entornoQR(t, func(c *mensajeria.Config) {
		c.Meta = mensajeria.MetaEntorno{Modo: "saas", AppID: appIDQR, AppSecret: appSecretQR, ConfigID: configIDQR, VerifyToken: "vt-del-entorno-0123456789"}
	})
	r := b.api(admin, http.MethodGet, "/conexion-meta", nil)
	b.exigir(r, http.StatusOK, "modo", "saas", "gestionada", "true", "estado", "gestionada", "qrDisponible", "true")
	sinSecretos(t, cadena(r.cuerpo), appSecretQR, "vt-del-entorno-0123456789")
	r = b.api(admin, http.MethodPost, "/conexion-meta/actualizar", map[string]any{"version": 0, "modo": "propia", "appId": appIDQR, "appSecret": appSecretQR,
		"configId": configIDQR})
	b.exigir(r, http.StatusUnprocessableEntity)
	if !strings.HasSuffix(fmt.Sprint(r.cuerpo["type"]), "conexion_gestionada") {
		t.Fatalf("saas: %v", r.cuerpo)
	}
	b.exigir(b.api(admin, http.MethodPost, "/conexion-meta/probar", nil), http.StatusOK)
	// El QR funciona con la app del entorno.
	b.exigir(conectarQR(b, admin, pnidNuevo(), "AQB-saas-"+pruebas.Unico(), ""), http.StatusCreated, "tipoConexion", "qr")
	if g.cuenta("GET /oauth/access_token", "") != 1 {
		t.Fatalf("canjes: %v", g.llamadas)
	}
}

// QR -----------------------------------------------------------------------------------------------

// Conectar con QR: el código se canjea una sola vez, nunca /register, y nada guarda el código ni el
// token.
func TestCanalQRConectaYCanjeaUnaVez(t *testing.T) {
	b, g, admin := entornoQR(t, nil)
	// Sin conexión: 422.
	r := conectarQR(b, admin, pnidNuevo(), "AQB-"+pruebas.Unico(), "")
	b.exigir(r, http.StatusUnprocessableEntity)
	if !strings.HasSuffix(fmt.Sprint(r.cuerpo["type"]), "conexion_meta_sin_configurar") {
		t.Fatalf("sin conexión: %v", r.cuerpo)
	}
	conectarMeta(b, admin)
	// Ids que no son números: 422 sin llamar a Meta.
	b.exigir(b.api(admin, http.MethodPost, "/canales/qr", map[string]any{"code": "AQB-x", "wabaId": "12a", "phoneNumberId": "1", "nombre": "X"}),
		http.StatusUnprocessableEntity)
	if len(g.llamadas) != 0 {
		t.Fatalf("llamó a Meta con ids inválidos: %v", g.llamadas)
	}

	pnid, codigo, clave := pnidNuevo(), "AQB-codigo-"+pruebas.Unico(), "qr-"+pruebas.Unico()
	r = conectarQR(b, admin, pnid, codigo, clave)
	b.exigir(r, http.StatusCreated, "tipoConexion", "qr", "coexistencia", "true", "numero", "+50255123344", "phoneNumberId", pnid, "conexion", "sin_probar")
	id := fmt.Sprint(r.cuerpo["id"])
	sync := r.cuerpo["sincronizacion"].(map[string]any)
	if sync["contactos"].(map[string]any)["estado"] != "sin_pedir" || sync["mensajes"].(map[string]any)["estado"] != "sin_pedir" || sync["plazo"] == nil {
		t.Fatalf("sincronizacion: %v", sync)
	}
	if r.cuerpo["appSecretGuardadoEn"] != nil || r.cuerpo["tokenGuardadoEn"] == nil || r.cuerpo["conectadoEn"] == nil {
		t.Fatalf("credenciales del QR: %v", r.cuerpo)
	}
	if g.cuenta("GET /oauth/access_token", "") != 1 || g.cuenta("POST /", "/subscribed_apps") != 1 || g.cuenta("POST /", "/register") != 0 {
		t.Fatalf("llamadas a Meta: %v", g.llamadas)
	}
	// El primer número QR usa el webhook de la app (sin override).
	if s := g.cuerpos["POST /9"+pnid[1:]+"/subscribed_apps"]; len(s) != 1 || strings.Contains(s[0], "override_callback_uri") {
		t.Fatalf("suscripción: %v", s)
	}
	// La misma Idempotency-Key: la misma respuesta, sin otro canje.
	r2 := conectarQR(b, admin, pnid, codigo, clave)
	b.exigir(r2, http.StatusCreated, "id", id)
	// El mismo código con otra clave: codigo_vencido sin llamar a Meta.
	r3 := conectarQR(b, admin, pnid, codigo, "otra-"+pruebas.Unico())
	b.exigir(r3, http.StatusUnprocessableEntity)
	if !strings.HasSuffix(fmt.Sprint(r3.cuerpo["type"]), "codigo_vencido") {
		t.Fatalf("código repetido: %v", r3.cuerpo)
	}
	if g.cuenta("GET /oauth/access_token", "") != 1 {
		t.Fatalf("canjeó dos veces: %v", g.llamadas)
	}
	// Nada guarda el código ni el token en claro.
	var aud, out, idem string
	if err := b.bd.QueryRow(b.ctx, `SELECT coalesce(string_agg(entrada::text || coalesce(resultado::text, ''), ''), '') FROM auditoria.acciones WHERE accion LIKE 'mensajeria.conectar_qr%'`).Scan(&aud); err != nil {
		t.Fatal(err)
	}
	if err := b.bd.QueryRow(b.ctx, `SELECT coalesce(string_agg(payload::text, ''), '') FROM integracion.outbox WHERE payload->>'canal_id' = $1`, id).Scan(&out); err != nil {
		t.Fatal(err)
	}
	if err := b.bd.QueryRow(b.ctx, `SELECT coalesce(string_agg(encode(respuesta, 'escape'), ''), '') FROM nucleo.idempotencia`).Scan(&idem); err != nil {
		t.Fatal(err)
	}
	sinSecretos(t, aud+out+idem+cadena(r.cuerpo), codigo, tokenNegocio, appSecretQR)
	if b.contarOutbox("canal_mensajeria.creado", "'tipo_conexion'", "qr") == 0 {
		t.Fatal("sin canal_mensajeria.creado con tipo_conexion qr")
	}

	// Un segundo número QR: con override_callback_uri a su propio webhook (≤ 200) y su verify token.
	pnid2 := pnidNuevo()
	r = conectarQR(b, admin, pnid2, "AQB-dos-"+pruebas.Unico(), "")
	b.exigir(r, http.StatusCreated)
	s := g.cuerpos["POST /9"+pnid2[1:]+"/subscribed_apps"]
	if len(s) != 1 || !strings.Contains(s[0], `"override_callback_uri":"https://iris.ejemplo.gt/api/v1/mensajeria/canales/`+fmt.Sprint(r.cuerpo["id"])+`/webhook"`) ||
		!strings.Contains(s[0], `"verify_token":"vt_`) {
		t.Fatalf("override: %v", s)
	}
	// Un número QR no cambia su token ni su phone number ID por /actualizar.
	c := b.api(admin, http.MethodGet, "/canales/"+id, nil)
	b.exigir(b.api(admin, http.MethodPost, "/canales/"+id+"/actualizar", map[string]any{"version": c.cuerpo["version"], "nombre": "Otro", "numero": c.cuerpo["numero"],
		"phoneNumberId": pnid, "token": "EAAG-otro-token-0123456789abcdef", "plantillas": map[string]any{}}), http.StatusUnprocessableEntity)
	b.exigir(b.api(admin, http.MethodPost, "/canales/"+id+"/actualizar", map[string]any{"version": c.cuerpo["version"], "nombre": "Otro nombre", "numero": c.cuerpo["numero"],
		"phoneNumberId": pnid, "plantillas": map[string]any{}}), http.StatusOK, "nombre", "Otro nombre")
	// El mismo phone number ID en otra empresa: 409 canal_existente.
	admin2 := b.persona([]int64{1, 2}, rolBase("admin"))
	b.exigir(b.api(admin2, http.MethodPost, "/canales/qr", map[string]any{"code": "AQB-otra-" + pruebas.Unico(), "wabaId": "1", "phoneNumberId": pnid,
		"nombre": "X", "empresaId": 2}), http.StatusConflict)
}

// Cada error de Meta se traduce a su código.
func TestCanalQRErroresDeMeta(t *testing.T) {
	b, g, admin := entornoQR(t, nil)
	conectarMeta(b, admin)
	casos := []struct {
		nombre string
		paso   *[2]any
		status int
		cuerpo string
		codigo string
		http   int
	}{
		{"canje vencido", &g.canje, 400, `{"error":{"code":100,"error_subcode":36007,"type":"OAuthException"}}`, "codigo_vencido", 422},
		{"canje sin permiso", &g.canje, 400, `{"error":{"code":10,"type":"OAuthException"}}`, "app_no_aprobada", 422},
		{"número de otro", &g.numero, 400, `{"error":{"code":100,"error_subcode":33,"type":"GraphMethodException"}}`, "numero_no_elegible", 422},
		{"suscripción sin permiso", &g.suscribir, 403, `{"error":{"code":200,"type":"OAuthException"}}`, "app_no_aprobada", 422},
		{"Meta caída", &g.suscribir, 500, `{"error":{"code":2,"type":"OAuthException","fbtrace_id":"AbX7kq2M9","message":"token EAAQR filtrado"}}`, "error_meta", 502},
	}
	for _, c := range casos {
		g.responder(c.paso, c.status, c.cuerpo)
		r := conectarQR(b, admin, pnidNuevo(), "AQB-"+pruebas.Unico(), "")
		g.exito(c.paso)
		if r.status != c.http || !strings.HasSuffix(fmt.Sprint(r.cuerpo["type"]), c.codigo) {
			t.Fatalf("%s: %d %v", c.nombre, r.status, r.cuerpo)
		}
		sinSecretos(t, cadena(r.cuerpo), tokenNegocio, "EAAQR filtrado")
		if c.codigo == "error_meta" && !strings.Contains(fmt.Sprint(r.cuerpo["detail"]), "Código 2 · OAuthException · fbtrace AbX7kq2M9") {
			t.Fatalf("detalle de error_meta: %v", r.cuerpo)
		}
	}
	// Un número que no está en la app WhatsApp Business.
	g.mu.Lock()
	g.enAppNegocio = false
	g.mu.Unlock()
	r := conectarQR(b, admin, pnidNuevo(), "AQB-"+pruebas.Unico(), "")
	b.exigir(r, http.StatusUnprocessableEntity)
	if !strings.HasSuffix(fmt.Sprint(r.cuerpo["type"]), "numero_no_elegible") {
		t.Fatalf("no elegible: %v", r.cuerpo)
	}
	// Ninguno quedó guardado y los fallos quedan auditados.
	if n := b.contar(`SELECT count(*) FROM mensajeria.canales WHERE tipo_conexion = 'qr' AND activo`); n != 0 {
		t.Fatalf("quedaron %d números", n)
	}
	if n := b.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'mensajeria.conectar_qr_fallido'`); n < len(casos) {
		t.Fatalf("fallos auditados: %d", n)
	}
	if g.cuenta("POST /", "/register") != 0 {
		t.Fatal("llamó /register")
	}
}

// Traer contactos e historial: una vez cada uno y dentro de 24 h.
func TestCanalQRSincronizar(t *testing.T) {
	b, g, admin := entornoQR(t, nil)
	conectarMeta(b, admin)
	r := conectarQR(b, admin, pnidNuevo(), "AQB-"+pruebas.Unico(), "")
	b.exigir(r, http.StatusCreated)
	id := fmt.Sprint(r.cuerpo["id"])
	ruta := "/canales/" + id + "/sincronizar"
	b.exigir(b.api(admin, http.MethodPost, ruta, map[string]any{}), http.StatusUnprocessableEntity)
	// Un número de API: no_es_coexistencia.
	nc := b.api(admin, http.MethodPost, fmt.Sprintf("/canales/%d/sincronizar", b.canal.ID), map[string]any{"contactos": true})
	if nc.status != http.StatusUnprocessableEntity || !strings.HasSuffix(fmt.Sprint(nc.cuerpo["type"]), "no_es_coexistencia") {
		t.Fatalf("API: %v", nc.cuerpo)
	}
	// El historial falla en Meta: 502 y queda fallido (se puede pedir otra vez).
	g.responder(&g.sync, 500, `{"error":{"code":1}}`)
	f := b.api(admin, http.MethodPost, ruta, map[string]any{"mensajes": true})
	g.exito(&g.sync)
	if f.status != http.StatusBadGateway {
		t.Fatalf("falla de Meta: %d %v", f.status, f.cuerpo)
	}
	v := b.api(admin, http.MethodGet, "/canales/"+id, nil)
	if e := v.cuerpo["sincronizacion"].(map[string]any)["mensajes"].(map[string]any); e["estado"] != "fallido" || e["detalle"] == nil {
		t.Fatalf("fallido: %v", e)
	}
	s := b.api(admin, http.MethodPost, ruta, map[string]any{"contactos": true, "mensajes": true})
	b.exigir(s, http.StatusOK)
	sync := s.cuerpo["sincronizacion"].(map[string]any)
	if sync["contactos"].(map[string]any)["estado"] != "pedido" || sync["mensajes"].(map[string]any)["estado"] != "pedido" {
		t.Fatalf("pedido: %v", sync)
	}
	cuerpos := strings.Join(g.cuerpos["POST /"+fmt.Sprint(r.cuerpo["phoneNumberId"])+"/smb_app_data"], "|")
	if !strings.Contains(cuerpos, `"sync_type":"smb_app_state_sync"`) || !strings.Contains(cuerpos, `"sync_type":"history"`) {
		t.Fatalf("smb_app_data: %s", cuerpos)
	}
	// Ya pedido: 422.
	y := b.api(admin, http.MethodPost, ruta, map[string]any{"contactos": true})
	if y.status != http.StatusUnprocessableEntity || !strings.HasSuffix(fmt.Sprint(y.cuerpo["type"]), "sincronizacion_ya_pedida") {
		t.Fatalf("ya pedida: %v", y.cuerpo)
	}
	// Pasadas 24 h: vencida.
	r = conectarQR(b, admin, pnidNuevo(), "AQB-"+pruebas.Unico(), "")
	b.exigir(r, http.StatusCreated)
	*b.reloj = b.reloj.Add(25 * time.Hour)
	vz := b.api(admin, http.MethodPost, fmt.Sprintf("/canales/%v/sincronizar", r.cuerpo["id"]), map[string]any{"contactos": true})
	if vz.status != http.StatusUnprocessableEntity || !strings.HasSuffix(fmt.Sprint(vz.cuerpo["type"]), "sincronizacion_vencida") {
		t.Fatalf("vencida: %v", vz.cuerpo)
	}
}

// Ecos, historial, contactos y desconexión por el webhook de la app: idempotentes y sin bot, avisos
// ni encuestas.
func TestCoexistenciaWebhooks(t *testing.T) {
	b, _, admin := entornoQR(t, nil)
	conectarMeta(b, admin)
	pnid := pnidNuevo()
	r := conectarQR(b, admin, pnid, "AQB-"+pruebas.Unico(), "")
	b.exigir(r, http.StatusCreated)
	id := int64(r.cuerpo["id"].(float64))
	waba := "9" + pnid[1:]
	b.exigir(b.api(admin, http.MethodPost, fmt.Sprintf("/canales/%d/sincronizar", id), map[string]any{"contactos": true, "mensajes": true}), http.StatusOK)
	jobsAntes := len(*b.jobs)

	// Eco: mandado desde el teléfono a un cliente nuevo.
	cliente := numeroCliente()
	eco := fmt.Sprintf(`{"object":"whatsapp_business_account","entry":[{"id":%q,"changes":[{"field":"smb_message_echoes","value":{"messaging_product":"whatsapp",`+
		`"metadata":{"display_phone_number":"50255123344","phone_number_id":%q},"message_echoes":[{"from":"50255123344","to":%q,"id":"wamid.eco-%s",`+
		`"timestamp":"%d","type":"text","text":{"body":"Hola, le escribo desde la tienda"}}]}}]}]}`, waba, pnid, cliente, pruebas.Unico(), b.reloj.Unix())
	b.exigir(b.webhookApp(eco), http.StatusOK)
	b.webhookApp(eco) // repetido: no duplica
	var conv int64
	var estado string
	var asignado *int64
	if err := b.bd.QueryRow(b.ctx, `SELECT id, estado, asignado_a FROM mensajeria.conversaciones WHERE canal_id = $1 AND numero = $2`, id, "+"+cliente).
		Scan(&conv, &estado, &asignado); err != nil {
		t.Fatal(err)
	}
	if estado != "esperando_cliente" || asignado != nil {
		t.Fatalf("eco: estado %s asignado %v", estado, asignado)
	}
	if n := b.contar(`SELECT count(*) FROM mensajeria.conversacion_mensajes WHERE conversacion_id = $1 AND origen = 'telefono' AND direccion = 'saliente' AND autor_id IS NULL`, conv); n != 1 {
		t.Fatalf("mensajes del teléfono: %d", n)
	}
	ms := b.mensajes(b.supervisor, fmt.Sprint(conv))
	if ms[len(ms)-1]["origen"] != "telefono" || ms[len(ms)-1]["autor"] != nil {
		t.Fatalf("mensaje del teléfono: %v", ms[len(ms)-1])
	}
	if b.contarOutbox("conversacion.mensaje_enviado", "'conversacion_id'", fmt.Sprint(conv)) != 1 ||
		b.contarOutbox("conversacion.creada", "'conversacion_id'", fmt.Sprint(conv)) != 1 {
		t.Fatal("outbox del eco")
	}
	if b.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'conversacion.creada' AND payload->>'conversacion_id' = $1 AND payload->>'origen' = 'telefono'`, fmt.Sprint(conv)) != 1 {
		t.Fatal("conversacion.creada sin origen telefono")
	}
	if v := b.api(admin, http.MethodGet, fmt.Sprintf("/canales/%d", id), nil); v.cuerpo["ultimoEcoEn"] == nil {
		t.Fatalf("ultimoEcoEn: %v", v.cuerpo)
	}

	// Historial: entra resuelto, idempotente, sin eventos de conversación.
	viejo := numeroCliente()
	hist := fmt.Sprintf(`{"object":"whatsapp_business_account","entry":[{"id":%q,"changes":[{"field":"history","value":{"messaging_product":"whatsapp",`+
		`"metadata":{"display_phone_number":"50255123344","phone_number_id":%q},"history":[{"metadata":{"phase":0,"chunk_order":1,"progress":100},`+
		`"threads":[{"id":%q,"context":{"wa_id":%q},"messages":[`+
		`{"from":%q,"id":"wamid.h1-%[6]s","timestamp":"%[7]d","type":"text","text":{"body":"¿Tienen fibra en zona 10?"},"history_context":{"status":"READ"}},`+
		`{"from":"50255123344","to":%[3]q,"id":"wamid.h2-%[6]s","timestamp":"%[8]d","type":"text","text":{"body":"Sí, desde Q199"},"history_context":{"status":"DELIVERED"}},`+
		`{"from":%[3]q,"id":"wamid.h3-%[6]s","timestamp":"%[9]d","type":"image","image":{"id":"m1","caption":"foto"},"history_context":{"status":"READ"}}`+
		`]}]}]}}]}]}`, waba, pnid, viejo, viejo, viejo, pruebas.Unico(), b.reloj.Add(-72*time.Hour).Unix(), b.reloj.Add(-71*time.Hour).Unix(), b.reloj.Add(-70*time.Hour).Unix())
	b.exigir(b.webhookApp(hist), http.StatusOK)
	b.webhookApp(strings.Replace(hist, `"chunk_order":1`, `"chunk_order":1 `, 1)) // otro cuerpo, mismos mensajes
	var hconv int64
	if err := b.bd.QueryRow(b.ctx, `SELECT id, estado FROM mensajeria.conversaciones WHERE canal_id = $1 AND numero = $2`, id, "+"+viejo).Scan(&hconv, &estado); err != nil {
		t.Fatal(err)
	}
	if estado != "resuelta" {
		t.Fatalf("historial: %s", estado)
	}
	if n := b.contar(`SELECT count(*) FROM mensajeria.conversacion_mensajes WHERE conversacion_id = $1 AND origen = 'historial'`, hconv); n != 3 {
		t.Fatalf("mensajes del historial: %d", n)
	}
	if n := b.contar(`SELECT count(*) FROM mensajeria.conversacion_mensajes WHERE conversacion_id = $1 AND origen = 'historial' AND direccion = 'saliente'`, hconv); n != 1 {
		t.Fatalf("salientes del historial: %d", n)
	}
	if n := b.contar(`SELECT count(*) FROM integracion.outbox WHERE payload->>'conversacion_id' = $1`, fmt.Sprint(hconv)); n != 0 {
		t.Fatalf("el historial emitió %d eventos", n)
	}
	if n := b.contar(`SELECT count(*) FROM mensajeria.conv_eventos WHERE conversacion_id = $1`, hconv); n != 0 {
		t.Fatalf("el historial llenó el feed: %d", n)
	}

	// Contactos: uno es cliente de Argos (solo se cuenta), otro se crea y luego se renombra.
	argosTel := numeroCliente()
	clienteArgos := siguienteCliente.Add(1)
	if _, err := b.bd.Exec(b.ctx, `INSERT INTO integracion.argos_clientes (id, empresa_id, nombre, movil, codigo, sincronizado_en, empresas)
		VALUES ($1, 1, 'Carlos Argos', $2, 'C-1', now(), '{1}')`, clienteArgos, argosTel); err != nil {
		t.Fatal(err)
	}
	nuevo := numeroCliente()
	contactos := func(nombre string) string {
		return fmt.Sprintf(`{"object":"whatsapp_business_account","entry":[{"id":%q,"changes":[{"field":"smb_app_state_sync","value":{"messaging_product":"whatsapp",`+
			`"metadata":{"display_phone_number":"50255123344","phone_number_id":%q},"state_sync":[`+
			`{"type":"contact","contact":{"full_name":"Otro nombre","first_name":"Otro","phone_number":%q},"action":"add","metadata":{"timestamp":"1"}},`+
			`{"type":"contact","contact":{"full_name":%q,"first_name":"Ana","phone_number":%q},"action":"add","metadata":{"timestamp":"1"}}]}}]}]}`,
			waba, pnid, argosTel, nombre, nuevo)
	}
	b.webhookApp(contactos("Ana Proveedora"))
	if n := b.contar(`SELECT count(*) FROM mensajeria.contactos_iris WHERE telefono = $1 AND nombre = 'Ana Proveedora'`, "+"+nuevo); n != 1 {
		t.Fatalf("contacto de Iris: %d", n)
	}
	if n := b.contar(`SELECT count(*) FROM mensajeria.contactos_iris WHERE telefono = $1`, "+"+argosTel); n != 0 {
		t.Fatal("creó un contacto de Iris para un cliente de Argos")
	}
	b.webhookApp(contactos("Ana López"))
	if n := b.contar(`SELECT count(*) FROM mensajeria.contactos_iris WHERE telefono = $1 AND nombre = 'Ana López'`, "+"+nuevo); n != 1 {
		t.Fatal("no renombró el contacto")
	}
	if n := b.contar(`SELECT count(*) FROM integracion.argos_clientes WHERE id = $1 AND nombre = 'Carlos Argos'`, clienteArgos); n != 1 {
		t.Fatal("tocó la instantánea de Argos")
	}
	v := b.api(admin, http.MethodGet, fmt.Sprintf("/canales/%d", id), nil)
	sync := v.cuerpo["sincronizacion"].(map[string]any)
	if c := sync["contactos"].(map[string]any); c["estado"] != "listo" || c["asociados"].(float64) < 1 {
		t.Fatalf("contactos: %v", c)
	}
	if mm := sync["mensajes"].(map[string]any); mm["estado"] != "listo" || mm["progreso"].(float64) != 100 || mm["mensajes"].(float64) != 3 {
		t.Fatalf("mensajes: %v", mm)
	}
	// Ni bot, ni avisos, ni encuestas: ningún job nuevo salvo la descarga del medio del eco.
	for _, j := range (*b.jobs)[jobsAntes:] {
		if j.Kind() != "mensajeria.descargar_medio" && j.Kind() != "mensajeria.procesar_entrante" {
			t.Fatalf("job inesperado: %s", j.Kind())
		}
	}
	if n := b.contar(`SELECT count(*) FROM integracion.outbox WHERE evento IN ('conversacion.mensaje_recibido', 'conversacion.bot_atendio', 'conversacion.aviso_sin_atender', 'aviso.creado') AND payload->>'canal_id' = $1`, fmt.Sprint(id)); n != 0 {
		t.Fatalf("eventos de bot o avisos: %d", n)
	}

	// account_update: desconectado por inactividad del teléfono; luego reconectado.
	baja := fmt.Sprintf(`{"object":"whatsapp_business_account","entry":[{"id":"2949482758682047","time":1748477359,"changes":[{"field":"account_update",`+
		`"value":{"event":"PARTNER_REMOVED","waba_info":{"waba_id":%q,"owner_business_id":"2329417887457253"},`+
		`"disconnection_info":{"reason":"PRIMARY_INACTIVITY","initiated_by":"SYSTEM"}}}]}]}`, waba)
	b.exigir(b.webhookApp(baja), http.StatusOK)
	v = b.api(admin, http.MethodGet, fmt.Sprintf("/canales/%d", id), nil)
	b.exigir(v, http.StatusOK, "conexion", "desconectado", "desconectadoMotivo", "primary_inactivity")
	if b.contarOutbox("canal_mensajeria.desconectado", "'canal_id'", fmt.Sprint(id)) != 1 {
		t.Fatal("sin canal_mensajeria.desconectado")
	}
	b.webhookApp(strings.Replace(baja, `"time":1748477359`, `"time":1748477360`, 1)) // repetido con otro cuerpo: una sola vez
	if b.contarOutbox("canal_mensajeria.desconectado", "'canal_id'", fmt.Sprint(id)) != 1 {
		t.Fatal("desconectó dos veces")
	}
	// «Reconectar»: el mismo QR otra vez reconecta el número (200, mismo id).
	re := b.api(admin, http.MethodPost, "/canales/qr", map[string]any{"code": "AQB-re-" + pruebas.Unico(), "wabaId": waba, "phoneNumberId": pnid, "nombre": "Ventas"})
	b.exigir(re, http.StatusOK, "id", fmt.Sprint(id), "conexion", "sin_probar")
	if re.cuerpo["desconectadoEn"] != nil {
		t.Fatalf("reconectar: %v", re.cuerpo)
	}
	b.exigir(b.webhookApp(fmt.Sprintf(`{"object":"whatsapp_business_account","entry":[{"id":%q,"changes":[{"field":"account_update","value":{"event":"ACCOUNT_OFFBOARDED"}}]}]}`, waba)), http.StatusOK)
	b.exigir(b.api(admin, http.MethodGet, fmt.Sprintf("/canales/%d", id), nil), http.StatusOK, "conexion", "desconectado", "desconectadoMotivo", "account_offboarded")
	b.webhookApp(fmt.Sprintf(`{"object":"whatsapp_business_account","entry":[{"id":%q,"changes":[{"field":"account_update","value":{"event":"ACCOUNT_RECONNECTED"}}]}]}`, waba))
	v = b.api(admin, http.MethodGet, fmt.Sprintf("/canales/%d", id), nil)
	if v.cuerpo["desconectadoEn"] != nil || b.contarOutbox("canal_mensajeria.reconectado", "'canal_id'", fmt.Sprint(id)) != 2 {
		t.Fatalf("reconectado: %v", v.cuerpo)
	}
	// Una entry de un número que Iris no tiene se descarta.
	b.exigir(b.webhookApp(`{"object":"whatsapp_business_account","entry":[{"id":"1","changes":[{"field":"messages","value":{"metadata":{"phone_number_id":"424242"}}}]}]}`),
		http.StatusOK)
}

// Préstamos -----------------------------------------------------------------------------------------

// prestamo: el número de la bandeja (empresa 1) compartido con la empresa 2 y su gente.
type prestamo struct {
	*bandeja
	sup2  persona // supervisor de la empresa 2
	ag2   persona // agente de la empresa 2 (miembro de su cola)
	cola2 int64
}

func nuevoPrestamo(t *testing.T, usos ...string) *prestamo {
	t.Helper()
	b := nuevaBandeja(t, nil)
	p := &prestamo{bandeja: b, sup2: b.persona([]int64{2}, rolBase("supervisor")), ag2: b.persona([]int64{2}, rolBase("soporte"))}
	r := b.api(p.sup2, http.MethodPost, "/colas", map[string]any{"nombre": "Soporte Norte " + pruebas.Unico()})
	b.exigir(r, http.StatusCreated)
	p.cola2 = int64(r.cuerpo["id"].(float64))
	b.exigir(b.api(p.sup2, http.MethodPost, fmt.Sprintf("/colas/%d/miembros", p.cola2), map[string]any{"version": r.cuerpo["version"], "usuarioIds": []int64{p.ag2.id}}),
		http.StatusOK)
	if len(usos) == 0 {
		usos = []string{"soporte", "avisos"}
	}
	pr := b.api(b.supervisor, http.MethodPost, fmt.Sprintf("/canales/%d/prestamos", b.canal.ID), map[string]any{"empresaId": 2, "usos": usos, "colaId": p.cola2})
	b.exigir(pr, http.StatusOK, "estado", "activo", "empresaId", "2")
	return p
}

func (p *prestamo) convDe(numero string) (int64, int64, string) {
	p.t.Helper()
	var id, empresa int64
	var estado string
	if err := p.bd.QueryRow(p.ctx, `SELECT id, empresa_id, estado FROM mensajeria.conversaciones WHERE canal_id = $1 AND numero = $2 ORDER BY id DESC LIMIT 1`,
		p.canal.ID, "+"+numero).Scan(&id, &empresa, &estado); err != nil {
		p.t.Fatalf("conversación de %s: %v", numero, err)
	}
	return id, empresa, estado
}

func (p *prestamo) clienteArgos(empresa int64, numero string) {
	p.t.Helper()
	if _, err := p.bd.Exec(p.ctx, `INSERT INTO integracion.argos_clientes (id, empresa_id, nombre, movil, codigo, sincronizado_en, empresas)
		VALUES ($1, $2, $4, $3, 'C', now(), $5)`, siguienteCliente.Add(1), empresa, numero, "Cliente "+numero, []int64{empresa}); err != nil {
		p.t.Fatal(err)
	}
}

// Cada caso de la regla de a qué empresa va un mensaje.
func TestPrestamoRuteo(t *testing.T) {
	p := nuevoPrestamo(t)
	motivo := func(conv int64) string {
		o := p.outbox("conversacion.empresa_asignada", fmt.Sprint(conv))
		if len(o) == 0 {
			return ""
		}
		return fmt.Sprint(o[len(o)-1]["motivo"])
	}
	// Sin coincidencia: la dueña.
	n1 := numeroCliente()
	p.texto(n1, "wamid.r1-"+pruebas.Unico(), "Hola")
	c, e, _ := p.convDe(n1)
	if e != 1 || motivo(c) != "sin_coincidencia" {
		t.Fatalf("sin coincidencia: empresa %d motivo %s", e, motivo(c))
	}
	// Cliente de la empresa 2: va a su cola.
	n2 := numeroCliente()
	p.clienteArgos(2, n2)
	p.texto(n2, "wamid.r2-"+pruebas.Unico(), "Sin internet")
	c, e, _ = p.convDe(n2)
	var cola int64
	if err := p.bd.QueryRow(p.ctx, `SELECT cola_id FROM mensajeria.conversaciones WHERE id = $1`, c).Scan(&cola); err != nil {
		t.Fatal(err)
	}
	if e != 2 || cola != p.cola2 || motivo(c) != "cliente_de_empresa" {
		t.Fatalf("cliente de la 2: empresa %d cola %d motivo %s", e, cola, motivo(c))
	}
	// Abierta: el siguiente mensaje sigue ahí sin otro evento.
	p.texto(n2, "wamid.r3-"+pruebas.Unico(), "¿Me ayudan?")
	if c2, e2, _ := p.convDe(n2); c2 != c || e2 != 2 || len(p.outbox("conversacion.empresa_asignada", fmt.Sprint(c))) != 1 {
		t.Fatalf("abierta: %d %d", c2, e2)
	}
	// Contacto de Iris de la empresa 2.
	n3 := numeroCliente()
	if _, err := p.bd.Exec(p.ctx, `INSERT INTO mensajeria.contactos_iris (empresa_id, nombre, tipo, telefono) VALUES (2, 'Proveedor', 'proveedor', $1)`, "+"+n3); err != nil {
		t.Fatal(err)
	}
	p.texto(n3, "wamid.r4-"+pruebas.Unico(), "Hola")
	if _, e, _ := p.convDe(n3); e != 2 {
		t.Fatalf("contacto de Iris de la 2: empresa %d", e)
	}
	// Cliente de las dos: la dueña.
	n4 := numeroCliente()
	p.clienteArgos(1, n4)
	p.clienteArgos(2, n4)
	p.texto(n4, "wamid.r5-"+pruebas.Unico(), "Hola")
	if c, e, _ := p.convDe(n4); e != 1 || motivo(c) != "varias_coincidencias" {
		t.Fatalf("varias: empresa %d motivo %s", e, motivo(c))
	}
	// La empresa 2 le mandó un aviso por este número en las últimas 24 h: es suya, aunque el
	// cliente sea de la 1.
	n5 := numeroCliente()
	p.clienteArgos(1, n5)
	if _, err := p.bd.Exec(p.ctx, `INSERT INTO mensajeria.avisos (id, empresa_id, cliente_id, plantilla, canal_preferido, prioridad, origen, estado, canal, canal_id,
		destino, enviado_en) VALUES ($1, 2, 77, 'bienvenida', 'whatsapp', 'normal', 'argos', 'entregado', 'whatsapp', $2, $3, $4)`,
		uuid.New(), p.canal.ID, "+"+n5, p.reloj.Add(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	p.texto(n5, "wamid.r6-"+pruebas.Unico(), "Recibí su aviso")
	if c, e, _ := p.convDe(n5); e != 2 || motivo(c) != "iniciada_por_empresa" {
		t.Fatalf("iniciada: empresa %d motivo %s", e, motivo(c))
	}
	// Un préstamo solo para avisos no recibe por coincidencia de cliente.
	r := p.api(p.supervisor, http.MethodGet, fmt.Sprintf("/canales/%d/prestamos", p.canal.ID), nil)
	ver := r.cuerpo["data"].([]any)[0].(map[string]any)["version"]
	p.exigir(p.api(p.supervisor, http.MethodPost, fmt.Sprintf("/canales/%d/prestamos", p.canal.ID), map[string]any{"empresaId": 2, "usos": []string{"avisos"},
		"colaId": p.cola2, "version": ver}), http.StatusOK)
	n6 := numeroCliente()
	p.clienteArgos(2, n6)
	p.texto(n6, "wamid.r7-"+pruebas.Unico(), "Hola")
	if _, e, _ := p.convDe(n6); e != 1 {
		t.Fatalf("solo avisos: empresa %d", e)
	}
}

// Una empresa nunca ve las conversaciones de la otra por el número compartido (lista, búsqueda,
// detalle, contadores y cambios), salvo las que se le pasan.
func TestPrestamoVisibilidad(t *testing.T) {
	p := nuevoPrestamo(t)
	duena, otra := numeroCliente(), numeroCliente()
	p.clienteArgos(2, otra)
	cursor1 := p.api(p.supervisor, http.MethodGet, "/conversaciones/cambios", nil).cuerpo["nextCursor"]
	cursor2 := p.api(p.sup2, http.MethodGet, "/conversaciones/cambios", nil).cuerpo["nextCursor"]
	p.texto(duena, "wamid.v1-"+pruebas.Unico(), "Soy de Red Sur")
	p.texto(otra, "wamid.v2-"+pruebas.Unico(), "Soy de Cable Norte")
	c1, _, _ := p.convDe(duena)
	c2, e2, _ := p.convDe(otra)
	if e2 != 2 {
		t.Fatal("la conversación no fue a la empresa 2")
	}
	ids := func(per persona, ruta string) map[int64]bool {
		r := p.api(per, http.MethodGet, ruta, nil)
		p.exigir(r, http.StatusOK)
		out := map[int64]bool{}
		for _, x := range r.cuerpo["data"].([]any) {
			m := x.(map[string]any)
			if id, ok := m["id"]; ok && m["conversacionId"] == nil {
				out[int64(id.(float64))] = true
			} else if cid, ok := m["conversacionId"]; ok {
				out[int64(cid.(float64))] = true
			}
		}
		return out
	}
	for _, caso := range []struct {
		per     persona
		propia  int64
		ajena   int64
		numAjen string
	}{{p.supervisor, c1, c2, otra}, {p.sup2, c2, c1, duena}, {p.ag2, c2, c1, duena}} {
		l := ids(caso.per, "/conversaciones?limit=100")
		if !l[caso.propia] || l[caso.ajena] {
			t.Fatalf("lista: %v (propia %d, ajena %d)", l, caso.propia, caso.ajena)
		}
		if b := ids(caso.per, "/conversaciones?limit=100&q="+caso.numAjen); b[caso.ajena] {
			t.Fatal("la búsqueda trae una conversación de la otra empresa")
		}
		p.exigir(p.api(caso.per, http.MethodGet, fmt.Sprintf("/conversaciones/%d", caso.ajena), nil), http.StatusNotFound)
		p.exigir(p.api(caso.per, http.MethodGet, fmt.Sprintf("/conversaciones/%d/mensajes", caso.ajena), nil), http.StatusNotFound)
		p.exigir(p.api(caso.per, http.MethodPost, fmt.Sprintf("/conversaciones/%d/tomar", caso.ajena), map[string]any{"version": 1}), http.StatusNotFound)
	}
	cont := p.api(p.sup2, http.MethodGet, "/conversaciones/contadores", nil)
	for _, pc := range cont.cuerpo["porCanal"].([]any) {
		if m := pc.(map[string]any); int64(m["id"].(float64)) == p.canal.ID && m["total"].(float64) != 1 {
			t.Fatalf("contadores de la 2: %v", cont.cuerpo)
		}
	}
	if cs := ids(p.supervisor, "/conversaciones/cambios?limit=200&cursor="+fmt.Sprint(cursor1)); cs[c2] || !cs[c1] {
		t.Fatalf("cambios de la dueña: %v", cs)
	}
	if cs := ids(p.sup2, "/conversaciones/cambios?limit=200&cursor="+fmt.Sprint(cursor2)); cs[c1] || !cs[c2] {
		t.Fatalf("cambios de la 2: %v", cs)
	}

	// Antes de pasarla, la dueña anota algo interno (con un adjunto) y el cliente mandó una foto.
	nota := p.api(p.supervisor, http.MethodPost, fmt.Sprintf("/conversaciones/%d/notas", c1), map[string]any{"id": uuid.Must(uuid.NewV7()).String(),
		"texto": "Dato interno de Red Sur", "menciones": []int64{p.agente.id}})
	p.exigir(nota, http.StatusCreated)
	adjCliente, adjNota := uuid.New(), uuid.New()
	for _, a := range []uuid.UUID{adjCliente, adjNota} {
		if _, err := p.bd.Exec(p.ctx, `INSERT INTO nucleo.adjuntos (id, empresa_id, entidad, entidad_id, tipo, mime, tamano, sha256, recibido, estado, almacenamiento, clave)
			VALUES ($1, 1, 'conversacion', $2, 'foto', 'image/jpeg', 10, repeat('a', 64), 10, 'completo', 'local', $3)`, a, fmt.Sprint(c1), "prueba/"+a.String()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := p.bd.Exec(p.ctx, `UPDATE mensajeria.conversacion_mensajes SET adjunto_id = $2 WHERE conversacion_id = $1 AND direccion = 'entrante'`, c1, adjCliente); err != nil {
		t.Fatal(err)
	}
	if _, err := p.bd.Exec(p.ctx, `UPDATE mensajeria.conversacion_mensajes SET adjunto_id = $2 WHERE id = $1`, nota.cuerpo["id"], adjNota); err != nil {
		t.Fatal(err)
	}
	// La dueña pasa la suya a la empresa 2: desde ahí solo la ve la 2 (con los mensajes del cliente).
	d := p.api(p.supervisor, http.MethodGet, fmt.Sprintf("/conversaciones/%d", c1), nil)
	pe := p.api(p.supervisor, http.MethodPost, fmt.Sprintf("/conversaciones/%d/pasar-empresa", c1), map[string]any{"version": d.cuerpo["version"], "empresaId": 2,
		"nota": "Es cliente de Cable Norte"})
	p.exigir(pe, http.StatusOK, "soloLectura", "true")
	p.exigir(p.api(p.supervisor, http.MethodGet, fmt.Sprintf("/conversaciones/%d", c1), nil), http.StatusNotFound)
	v := p.api(p.sup2, http.MethodGet, fmt.Sprintf("/conversaciones/%d", c1), nil)
	p.exigir(v, http.StatusOK, "soloLectura", "<nil>")
	if v.cuerpo["cola"].(map[string]any)["id"].(float64) != float64(p.cola2) || v.cuerpo["asignado"] != nil {
		t.Fatalf("pasada: %v", v.cuerpo)
	}
	// Ve los mensajes del cliente, la nota de quien la pasó y «Pasada por Empresa 1»; nunca las
	// notas internas previas ni los eventos de sistema de la dueña.
	ms := p.mensajes(p.sup2, fmt.Sprint(c1))
	pasada := eventosSistema(ms, "pasada_a_empresa")
	if len(pasada) != 1 || pasada[0]["detalle"] != "Empresa 1" || pasada[0]["actorNombre"] != nil {
		t.Fatalf("pasada_a_empresa: %v", ms)
	}
	textos := cadena(ms)
	if strings.Contains(textos, "Dato interno de Red Sur") || len(eventosSistema(ms, "conversacion_creada")) != 0 ||
		!strings.Contains(textos, "Soy de Red Sur") || !strings.Contains(textos, "Es cliente de Cable Norte") {
		t.Fatalf("mensajes pasados: %v", ms)
	}
	if n := p.contar(`SELECT count(*) FROM mensajeria.conversacion_mensajes WHERE conversacion_id = $1 AND direccion = 'nota' AND empresa_id = 1`, c1); n != 1 {
		t.Fatalf("la nota de la dueña debía quedarse con la dueña: %d", n)
	}
	// El adjunto del cliente pasa con su mensaje; el de la nota de la dueña, no.
	var eCliente, eNota int64
	if err := p.bd.QueryRow(p.ctx, `SELECT (SELECT empresa_id FROM nucleo.adjuntos WHERE id = $1), (SELECT empresa_id FROM nucleo.adjuntos WHERE id = $2)`,
		adjCliente, adjNota).Scan(&eCliente, &eNota); err != nil {
		t.Fatal(err)
	}
	if eCliente != 2 || eNota != 1 {
		t.Fatalf("adjuntos: cliente %d nota %d", eCliente, eNota)
	}
	if n := p.contar(`SELECT count(*) FROM mensajeria.conversacion_participantes WHERE conversacion_id = $1`, c1); n != 0 {
		t.Fatal("las menciones de la dueña pasaron")
	}
	if o := p.outbox("conversacion.empresa_asignada", fmt.Sprint(c1)); o[len(o)-1]["motivo"] != "pasada" {
		t.Fatalf("empresa_asignada: %v", o)
	}
	// Solo la dueña pasa, y solo a quien usa el número.
	v2 := p.api(p.sup2, http.MethodGet, fmt.Sprintf("/conversaciones/%d", c2), nil)
	p.exigir(p.api(p.sup2, http.MethodPost, fmt.Sprintf("/conversaciones/%d/pasar-empresa", c2), map[string]any{"version": v2.cuerpo["version"], "empresaId": 1}),
		http.StatusUnprocessableEntity)
	n := numeroCliente()
	p.texto(n, "wamid.v3-"+pruebas.Unico(), "Hola")
	c3, _, _ := p.convDe(n)
	d = p.api(p.supervisor, http.MethodGet, fmt.Sprintf("/conversaciones/%d", c3), nil)
	p.exigir(p.api(p.supervisor, http.MethodPost, fmt.Sprintf("/conversaciones/%d/pasar-empresa", c3), map[string]any{"version": d.cuerpo["version"], "empresaId": 99}),
		http.StatusUnprocessableEntity)
}

// Revocar: lo abierto se resuelve y queda de solo lectura para la empresa 2; lo nuevo llega a la
// dueña; sus avisos en cola fallan.
func TestPrestamoRevocar(t *testing.T) {
	p := nuevoPrestamo(t)
	n := numeroCliente()
	p.clienteArgos(2, n)
	p.texto(n, "wamid.x1-"+pruebas.Unico(), "Hola")
	c, e, _ := p.convDe(n)
	if e != 2 {
		t.Fatal("no fue a la 2")
	}
	aviso := uuid.New()
	if _, err := p.bd.Exec(p.ctx, `INSERT INTO mensajeria.avisos (id, empresa_id, cliente_id, plantilla, canal_preferido, prioridad, origen, estado, canal, canal_id, destino)
		VALUES ($1, 2, 77, 'bienvenida', 'whatsapp', 'normal', 'argos', 'en_cola', 'whatsapp', $2, '+50255550001')`, aviso, p.canal.ID); err != nil {
		t.Fatal(err)
	}
	l := p.api(p.supervisor, http.MethodGet, fmt.Sprintf("/canales/%d/prestamos", p.canal.ID), nil)
	pr := l.cuerpo["data"].([]any)[0].(map[string]any)
	if pr["conversacionesAbiertas"].(float64) != 1 || pr["avisosProgramados"].(float64) != 1 {
		t.Fatalf("préstamo: %v", pr)
	}
	// Solo la dueña revoca, con motivo.
	p.exigir(p.api(p.sup2, http.MethodPost, fmt.Sprintf("/canales/%d/prestamos/2/revocar", p.canal.ID), map[string]any{"version": pr["version"], "motivo": "Ya no"}),
		http.StatusNotFound)
	p.exigir(p.api(p.supervisor, http.MethodPost, fmt.Sprintf("/canales/%d/prestamos/2/revocar", p.canal.ID), map[string]any{"version": pr["version"], "motivo": "No"}),
		http.StatusUnprocessableEntity)
	r := p.api(p.supervisor, http.MethodPost, fmt.Sprintf("/canales/%d/prestamos/2/revocar", p.canal.ID), map[string]any{"version": pr["version"],
		"motivo": "Cable Norte tiene su propio número"})
	p.exigir(r, http.StatusOK, "estado", "revocado")
	if _, _, estado := p.convDe(n); estado != "resuelta" {
		t.Fatalf("revocada: %s", estado)
	}
	v := p.api(p.sup2, http.MethodGet, fmt.Sprintf("/conversaciones/%d", c), nil)
	p.exigir(v, http.StatusOK, "soloLectura", "true")
	p.exigir(p.api(p.sup2, http.MethodPost, fmt.Sprintf("/conversaciones/%d/reabrir", c), map[string]any{"version": v.cuerpo["version"]}), http.StatusUnprocessableEntity)
	re := p.api(p.sup2, http.MethodPost, fmt.Sprintf("/conversaciones/%d/notas", c), map[string]any{"id": uuid.Must(uuid.NewV7()).String(), "texto": "nota", "menciones": []int64{}})
	if re.status != http.StatusUnprocessableEntity || !strings.HasSuffix(fmt.Sprint(re.cuerpo["type"]), "conversacion_solo_lectura") {
		t.Fatalf("nota en solo lectura: %v", re.cuerpo)
	}
	p.exigir(p.api(p.sup2, http.MethodPost, fmt.Sprintf("/conversaciones/%d/mensajes", c), map[string]any{"id": uuid.Must(uuid.NewV7()).String(), "texto": "hola"}),
		http.StatusUnprocessableEntity)
	var estado string
	if err := p.bd.QueryRow(p.ctx, `SELECT estado FROM mensajeria.avisos WHERE id = $1`, aviso).Scan(&estado); err != nil || estado != "fallido" {
		t.Fatalf("aviso en cola: %s %v", estado, err)
	}
	if p.contarOutbox("canal_mensajeria.prestamo_revocado", "'canal_id'", fmt.Sprint(p.canal.ID)) != 1 {
		t.Fatal("sin canal_mensajeria.prestamo_revocado")
	}
	// El cliente vuelve a escribir: llega a la dueña.
	p.texto(n, "wamid.x2-"+pruebas.Unico(), "¿Siguen ahí?")
	if _, e, _ := p.convDe(n); e != 1 {
		t.Fatalf("después de revocar: empresa %d", e)
	}
	// La 2 ya no ve el número.
	for _, x := range p.api(p.sup2, http.MethodGet, "/conversaciones/canales", nil).cuerpo["data"].([]any) {
		if int64(x.(map[string]any)["id"].(float64)) == p.canal.ID {
			t.Fatal("la 2 sigue viendo el número")
		}
	}
}

// Quien usa el número: lo ve sin credenciales, solo con sus usos; avisos solo con el uso avisos.
func TestPrestamoUsos(t *testing.T) {
	p := nuevoPrestamo(t, "avisos")
	admin2 := p.persona([]int64{2}, rolBase("admin"))
	admin2.empresa = 2
	// Lista de números de la 2: prestado por la empresa 1, sin datos de Meta.
	l := p.api(admin2, http.MethodGet, "/canales", nil)
	p.exigir(l, http.StatusOK)
	var visto map[string]any
	for _, x := range l.cuerpo["data"].([]any) {
		if m := x.(map[string]any); int64(m["id"].(float64)) == p.canal.ID {
			visto = m
		}
	}
	if visto == nil || visto["prestadoPor"].(map[string]any)["empresaId"].(float64) != 1 || visto["phoneNumberId"] != "" || visto["webhookUrl"] != "" ||
		len(visto["plantillas"].(map[string]any)) != 0 || fmt.Sprint(visto["usos"]) != "[avisos]" {
		t.Fatalf("número prestado: %v", visto)
	}
	p.exigir(p.api(admin2, http.MethodGet, fmt.Sprintf("/canales/%d", p.canal.ID), nil), http.StatusOK, "phoneNumberId", "", "empresaId", "2")
	// Quien lo usa no lo presta, no ve sus préstamos ni configura su uso.
	p.exigir(p.api(p.sup2, http.MethodPost, fmt.Sprintf("/canales/%d/prestamos", p.canal.ID), map[string]any{"empresaId": 1, "usos": []string{"avisos"}}),
		http.StatusNotFound)
	p.exigir(p.api(p.sup2, http.MethodGet, fmt.Sprintf("/canales/%d/prestamos", p.canal.ID), nil), http.StatusNotFound)
	p.exigir(p.api(p.sup2, http.MethodPost, fmt.Sprintf("/canales/%d/configurar", p.canal.ID), map[string]any{"version": 1, "usos": []string{"avisos"}, "avisosEmpresa": false, "colaId": nil}),
		http.StatusNotFound)
	// La dueña lo ve compartido con 1.
	if v := p.api(p.supervisor, http.MethodGet, "/conversaciones/canales", nil); v.status != http.StatusOK {
		t.Fatal(v.cuerpo)
	}
	admin1 := p.persona([]int64{1}, rolBase("admin"))
	admin1.empresa = 1
	p.exigir(p.api(admin1, http.MethodGet, fmt.Sprintf("/canales/%d", p.canal.ID), nil), http.StatusOK, "compartidoCon", "1")

	// Solo avisos: no inicia ni ve plantillas del número.
	nums := p.api(p.sup2, http.MethodGet, "/conversaciones/canales", nil)
	for _, x := range nums.cuerpo["data"].([]any) {
		if m := x.(map[string]any); int64(m["id"].(float64)) == p.canal.ID && (m["iniciar"] != false || m["prestadoPor"] == nil) {
			t.Fatalf("número en la bandeja de la 2: %v", m)
		}
	}
	p.exigir(p.api(p.sup2, http.MethodGet, fmt.Sprintf("/conversaciones/canales/%d/plantillas", p.canal.ID), nil), http.StatusNotFound)
	ini := p.api(p.sup2, http.MethodPost, "/conversaciones/iniciar", map[string]any{"id": uuid.Must(uuid.NewV7()).String(), "canalId": p.canal.ID, "contactoIrisId": 1,
		"plantilla": map[string]any{"codigo": "bienvenida", "variables": map[string]any{}}})
	if ini.status != http.StatusUnprocessableEntity || !strings.HasSuffix(fmt.Sprint(ini.cuerpo["type"]), "uso_no_permitido") {
		t.Fatalf("iniciar solo avisos: %d %v", ini.status, ini.cuerpo)
	}

	// Número de avisos de la empresa 2: con el uso avisos sí; los avisos de su Argos salen por él.
	pv := visto["prestamo"].(map[string]any)["version"]
	c := p.api(p.sup2, http.MethodPost, fmt.Sprintf("/canales/%d/prestamo/configurar", p.canal.ID), map[string]any{"version": pv, "avisosEmpresa": true})
	p.exigir(c, http.StatusOK, "avisosEmpresa", "true")
	cliente := p.cliente(2, "+50255443322")
	cuando := p.reloj.Add(-time.Hour)
	p.exigir(p.pedir(http.MethodPost, rutaMsj+"/consentimientos", p.entorno.otro, fmt.Sprintf("argos:consentimiento:2:%d:whatsapp:%s:acepta", cliente, cuando.Format(time.RFC3339)),
		map[string]any{"empresa_id": 2, "cliente_id": cliente, "canal": "whatsapp", "estado": "acepta", "origen": "argos_panel", "ocurrido_en": cuando}), http.StatusCreated)
	a := p.pedir(http.MethodPost, rutaMsj+"/avisos", p.entorno.otro, "argos:aviso:bienvenida:"+pruebas.Unico(), map[string]any{"empresa_id": 2, "cliente_id": cliente,
		"servicio_id": cliente * 10, "plantilla": "bienvenida", "variables": map[string]any{"plan": "Fibra 50"}, "canal_preferido": "whatsapp", "prioridad": "normal"})
	p.exigir(a, http.StatusAccepted, "estado", "en_cola")
	var canal *int64
	if err := p.bd.QueryRow(p.ctx, `SELECT canal_id FROM mensajeria.avisos WHERE id = $1`, a.cuerpo["aviso_id"]).Scan(&canal); err != nil || canal == nil || *canal != p.canal.ID {
		t.Fatalf("aviso de la 2 por el número prestado: %v %v", canal, err)
	}
	// Sin el uso avisos: no puede marcarlo.
	pr := p.api(p.supervisor, http.MethodGet, fmt.Sprintf("/canales/%d/prestamos", p.canal.ID), nil).cuerpo["data"].([]any)[0].(map[string]any)
	p.exigir(p.api(p.supervisor, http.MethodPost, fmt.Sprintf("/canales/%d/prestamos", p.canal.ID), map[string]any{"empresaId": 2, "usos": []string{"soporte"},
		"colaId": p.cola2, "version": pr["version"]}), http.StatusOK, "avisosEmpresa", "false")
	v := p.api(p.sup2, http.MethodGet, "/conversaciones/canales", nil)
	for _, x := range v.cuerpo["data"].([]any) {
		if m := x.(map[string]any); int64(m["id"].(float64)) == p.canal.ID {
			pv = m["version"]
		}
	}
	u := p.api(p.sup2, http.MethodPost, fmt.Sprintf("/canales/%d/prestamo/configurar", p.canal.ID), map[string]any{"version": pv, "avisosEmpresa": true})
	if u.status != http.StatusUnprocessableEntity || !strings.HasSuffix(fmt.Sprint(u.cuerpo["type"]), "uso_no_permitido") {
		t.Fatalf("avisos sin el uso: %v", u.cuerpo)
	}
	// Prestar a la misma empresa dueña o a una que no existe: 422.
	p.exigir(p.api(p.supervisor, http.MethodPost, fmt.Sprintf("/canales/%d/prestamos", p.canal.ID), map[string]any{"empresaId": 1, "usos": []string{"soporte"}}),
		http.StatusUnprocessableEntity)
	p.exigir(p.api(p.supervisor, http.MethodPost, fmt.Sprintf("/canales/%d/prestamos", p.canal.ID), map[string]any{"empresaId": 9999, "usos": []string{"soporte"}}),
		http.StatusUnprocessableEntity)
	// Una cola de otra empresa: 422.
	pr = p.api(p.supervisor, http.MethodGet, fmt.Sprintf("/canales/%d/prestamos", p.canal.ID), nil).cuerpo["data"].([]any)[0].(map[string]any)
	p.exigir(p.api(p.supervisor, http.MethodPost, fmt.Sprintf("/canales/%d/prestamos", p.canal.ID), map[string]any{"empresaId": 2, "usos": []string{"soporte"},
		"colaId": p.cola, "version": pr["version"]}), http.StatusUnprocessableEntity)
	// Opciones: la empresa 2 con su cola.
	o := p.api(p.supervisor, http.MethodGet, fmt.Sprintf("/canales/%d/prestamos/opciones", p.canal.ID), nil)
	p.exigir(o, http.StatusOK)
	if !strings.Contains(cadena(o.cuerpo), fmt.Sprintf(`"id":%d`, p.cola2)) {
		t.Fatalf("opciones: %v", o.cuerpo)
	}
	// Con soporte: la plantilla en su conversación sí pasa la regla de usos (y en una de solo
	// avisos no).
	n := numeroCliente()
	p.clienteArgos(2, n)
	p.texto(n, "wamid.u1-"+pruebas.Unico(), "Hola")
	cid, e, _ := p.convDe(n)
	if e != 2 {
		t.Fatal("no fue a la 2")
	}
	pr = p.api(p.supervisor, http.MethodGet, fmt.Sprintf("/canales/%d/prestamos", p.canal.ID), nil).cuerpo["data"].([]any)[0].(map[string]any)
	p.exigir(p.api(p.supervisor, http.MethodPost, fmt.Sprintf("/canales/%d/prestamos", p.canal.ID), map[string]any{"empresaId": 2, "usos": []string{"avisos"},
		"colaId": p.cola2, "version": pr["version"]}), http.StatusOK)
	pl := p.api(p.sup2, http.MethodPost, fmt.Sprintf("/conversaciones/%d/mensajes", cid), map[string]any{"id": uuid.Must(uuid.NewV7()).String(),
		"plantilla": map[string]any{"codigo": "bienvenida", "variables": map[string]any{"nombre": "Ana"}}})
	if pl.status != http.StatusUnprocessableEntity || !strings.HasSuffix(fmt.Sprint(pl.cuerpo["type"]), "uso_no_permitido") {
		t.Fatalf("plantilla solo avisos: %d %v", pl.status, pl.cuerpo)
	}
	// Dentro de la ventana, el texto sí sale (es su conversación).
	p.exigir(p.api(p.sup2, http.MethodPost, fmt.Sprintf("/conversaciones/%d/mensajes", cid), map[string]any{"id": uuid.Must(uuid.NewV7()).String(), "texto": "Hola"}),
		http.StatusCreated)
}

// Pasar a otra empresa y prestar: los errores que faltan (conversación que ya existe, con vínculos,
// número inactivo, versión vieja).
func TestPrestamoErrores(t *testing.T) {
	p := nuevoPrestamo(t)
	n := numeroCliente()
	p.texto(n, "wamid.e1-"+pruebas.Unico(), "Hola")
	c1, e, _ := p.convDe(n)
	if e != 1 {
		t.Fatal("no fue a la dueña")
	}
	d := p.api(p.supervisor, http.MethodGet, fmt.Sprintf("/conversaciones/%d", c1), nil)
	p.exigir(p.api(p.supervisor, http.MethodPost, fmt.Sprintf("/conversaciones/%d/resolver", c1), map[string]any{"version": d.cuerpo["version"]}), http.StatusOK)
	// El cliente resulta ser de la 2: lo siguiente va allá.
	p.clienteArgos(2, n)
	p.texto(n, "wamid.e2-"+pruebas.Unico(), "Otra vez")
	if _, e, _ := p.convDe(n); e != 2 {
		t.Fatalf("segunda conversación: empresa %d", e)
	}
	// Pasar la de la dueña: la 2 ya tiene una de ese teléfono.
	d = p.api(p.supervisor, http.MethodGet, fmt.Sprintf("/conversaciones/%d", c1), nil)
	r := p.api(p.supervisor, http.MethodPost, fmt.Sprintf("/conversaciones/%d/pasar-empresa", c1), map[string]any{"version": d.cuerpo["version"], "empresaId": 2})
	if r.status != http.StatusConflict || !strings.HasSuffix(fmt.Sprint(r.cuerpo["type"]), "conversacion_existente") {
		t.Fatalf("conversación existente: %d %v", r.status, r.cuerpo)
	}
	// Con vínculos (un ticket de la dueña): no se pasa.
	n2 := numeroCliente()
	p.texto(n2, "wamid.e3-"+pruebas.Unico(), "Hola")
	c2, _, _ := p.convDe(n2)
	if _, err := p.bd.Exec(p.ctx, `INSERT INTO mensajeria.conversacion_vinculos (conversacion_id, tipo, entidad_id, referencia) VALUES ($1, 'ticket', '1', 'TK-000001')`, c2); err != nil {
		t.Fatal(err)
	}
	d = p.api(p.supervisor, http.MethodGet, fmt.Sprintf("/conversaciones/%d", c2), nil)
	r = p.api(p.supervisor, http.MethodPost, fmt.Sprintf("/conversaciones/%d/pasar-empresa", c2), map[string]any{"version": d.cuerpo["version"], "empresaId": 2})
	if r.status != http.StatusUnprocessableEntity || !strings.HasSuffix(fmt.Sprint(r.cuerpo["type"]), "conversacion_con_vinculos") {
		t.Fatalf("con vínculos: %d %v", r.status, r.cuerpo)
	}
	// Un número inactivo no se presta.
	admin := p.persona([]int64{1}, rolBase("admin"))
	p.exigir(p.api(admin, http.MethodPost, fmt.Sprintf("/canales/%d/desactivar", p.canal.ID), nil), http.StatusOK)
	pr := p.api(p.supervisor, http.MethodGet, fmt.Sprintf("/canales/%d/prestamos", p.canal.ID), nil).cuerpo["data"].([]any)[0].(map[string]any)
	r = p.api(p.supervisor, http.MethodPost, fmt.Sprintf("/canales/%d/prestamos", p.canal.ID), map[string]any{"empresaId": 2, "usos": []string{"soporte"},
		"version": pr["version"]})
	if r.status != http.StatusUnprocessableEntity || !strings.HasSuffix(fmt.Sprint(r.cuerpo["type"]), "canal_inactivo") {
		t.Fatalf("inactivo: %d %v", r.status, r.cuerpo)
	}
	// Versión vieja del préstamo: 409.
	p.exigir(p.api(admin, http.MethodPost, fmt.Sprintf("/canales/%d/activar", p.canal.ID), nil), http.StatusOK)
	p.exigir(p.api(p.supervisor, http.MethodPost, fmt.Sprintf("/canales/%d/prestamos", p.canal.ID), map[string]any{"empresaId": 2, "usos": []string{"soporte"},
		"version": 0}), http.StatusConflict)
}
