package horus_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/horus"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/servidor"
	"github.com/HCDestroyer/iris/apps/api/migraciones"
)

func TestMain(m *testing.M) { os.Exit(pruebas.Correr(m)) }

const contrasena = "contrasena-de-prueba-123"

var loopback = []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8"), netip.MustParsePrefix("::1/128")}

type entorno struct {
	t     *testing.T
	pool  *pgxpool.Pool
	horus *horusFalso
	argos *argosFalso
	am    *argos.Modulo
	mod   *horus.Modulo
	ident *identidad.Modulo
	api   *httptest.Server
}

// nuevoEntorno levanta Horus y Argos falsos y la API de Iris con los dos conectores. redesHorus
// son las redes internas permitidas para Horus (loopback en casi todas las pruebas; nil para
// probar la protección contra SSRF).
func nuevoEntorno(t *testing.T, redesHorus []netip.Prefix) *entorno {
	t.Helper()
	pool := pruebas.Pool(t)
	for _, sql := range []string{
		"UPDATE integracion.conectores SET estado = 'apagado'",
		"DELETE FROM integracion.argos_servicios", "DELETE FROM integracion.argos_clientes", "DELETE FROM integracion.argos_routers",
		"DELETE FROM nucleo.usuarios_empresas", "DELETE FROM nucleo.empresas WHERE id NOT IN (1, 2)",
		"INSERT INTO nucleo.empresas (id, nombre) VALUES (1, 'Empresa 1'), (2, 'Empresa 2') ON CONFLICT (id) DO NOTHING",
	} {
		if _, err := pool.Exec(context.Background(), sql); err != nil {
			t.Fatal(err)
		}
	}
	clave := make([]byte, 32)
	_, _ = rand.Read(clave)
	cif, err := nucleo.NuevoCifrador(base64.StdEncoding.EncodeToString(clave))
	if err != nil {
		t.Fatal(err)
	}
	cola, err := integracion.NuevoClienteInsercion(pool, migraciones.EsquemaRiver, nil)
	if err != nil {
		t.Fatal(err)
	}
	am := argos.Nuevo(argos.Config{
		Cifrador: cif, BaseURL: "https://iris.prueba", PermitirHTTP: true, RedesPermitidas: loopback,
		Espera: 5 * time.Millisecond, Timeout: 2 * time.Second, Encolar: integracion.Encolar(cola),
	})
	mod := horus.Nuevo(horus.Config{Argos: am, RedesPermitidas: redesHorus, Espera: 5 * time.Millisecond, Timeout: 2 * time.Second})
	ident := identidad.Nuevo(identidad.Config{Cifrador: cif})
	h, _ := servidor.Nuevo(servidor.Deps{
		BD: pool, Identidad: ident, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Modulos: []func(huma.API){am.Registrar, mod.Registrar},
	})
	api := httptest.NewServer(h)
	t.Cleanup(api.Close)
	return &entorno{t: t, pool: pool, horus: nuevoHorusFalso(t), argos: nuevoArgosFalso(t), am: am, mod: mod, ident: ident, api: api}
}

// Cliente HTTP con sesión ---------------------------------------------------------------------

type clienteHTTP struct {
	t    *testing.T
	base string
	http *http.Client
	csrf string
}

type respuesta struct {
	status int
	cuerpo map[string]any
	crudo  []byte
	h      http.Header
}

func (r respuesta) problema(t *testing.T, status int, tipo string) {
	t.Helper()
	if r.status != status || r.cuerpo["type"] != "urn:iris:problema:"+tipo {
		t.Fatalf("esperaba %d %s, llegó %d: %s", status, tipo, r.status, r.crudo)
	}
}

func (c *clienteHTTP) pedir(metodo, ruta string, cuerpo any, cabeceras ...string) respuesta {
	c.t.Helper()
	var lector io.Reader
	switch b := cuerpo.(type) {
	case nil:
	case []byte:
		lector = bytes.NewReader(b)
	default:
		j, _ := json.Marshal(b)
		lector = bytes.NewReader(j)
	}
	req, _ := http.NewRequest(metodo, c.base+ruta, lector)
	if cuerpo != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.csrf != "" {
		req.Header.Set(identidad.CabeceraCSRF, c.csrf)
	}
	for i := 0; i+1 < len(cabeceras); i += 2 {
		req.Header.Set(cabeceras[i], cabeceras[i+1])
	}
	res, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	crudo, _ := io.ReadAll(res.Body)
	r := respuesta{status: res.StatusCode, crudo: crudo, h: res.Header}
	_ = json.Unmarshal(crudo, &r.cuerpo)
	return r
}

func (e *entorno) anonimo() *clienteHTTP {
	jar, _ := cookiejar.New(nil)
	return &clienteHTTP{t: e.t, base: e.api.URL, http: &http.Client{Jar: jar}}
}

// usuario crea un usuario con esos roles (empresa 1) y devuelve un cliente con su sesión.
func (e *entorno) usuario(roles ...string) *clienteHTTP {
	e.t.Helper()
	correo := pruebas.Correo("u")
	asign := []identidad.AsignacionRol{}
	for _, r := range roles {
		asign = append(asign, identidad.AsignacionRol{Rol: r})
	}
	ctx := pruebas.ContextoSistema(e.pool)
	if _, err := e.ident.CrearUsuario.Ejecutar(ctx, identidad.EntradaCrearUsuario{
		Nombre: "Usuario " + correo, Correo: correo, Contrasena: contrasena, Roles: asign, Empresas: []int64{1},
	}); err != nil {
		e.t.Fatal(err)
	}
	c := e.anonimo()
	r := c.pedir(http.MethodPost, "/api/v1/sesion", map[string]any{"correo": correo, "contrasena": contrasena})
	if r.status != 200 {
		e.t.Fatalf("login: %d %s", r.status, r.crudo)
	}
	c.csrf, _ = r.cuerpo["csrfToken"].(string)
	return c
}

func (e *entorno) contar(sql string, args ...any) int {
	e.t.Helper()
	var n int
	if err := e.pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

func (e *entorno) texto(sql string, args ...any) string {
	e.t.Helper()
	var s string
	if err := e.pool.QueryRow(context.Background(), sql, args...).Scan(&s); err != nil {
		e.t.Fatal(err)
	}
	return s
}

func activar(t *testing.T, admin *clienteHTTP, id int64) {
	t.Helper()
	var version float64 = 1
	r := admin.pedir(http.MethodGet, "/api/v1/integraciones/conectores?limit=200", nil)
	for _, c := range r.cuerpo["data"].([]any) {
		if m := c.(map[string]any); int64(m["id"].(float64)) == id {
			version = m["version"].(float64)
		}
	}
	if a := admin.pedir(http.MethodPost, fmt.Sprintf("/api/v1/integraciones/conectores/%d/activar", id), map[string]any{"version": version}); a.status != 200 {
		t.Fatalf("activar %d: %d %s", id, a.status, a.crudo)
	}
}

// conectarHorus: un admin canjea el enlace de Horus y activa el conector. Devuelve el admin, el
// id y el secreto del webhook que Iris entregó a Horus.
func (e *entorno) conectarHorus() (*clienteHTTP, int64, string) {
	e.t.Helper()
	admin := e.usuario("admin")
	r := admin.pedir(http.MethodPost, "/api/v1/integraciones/horus/conectar", map[string]any{"enlace": e.horus.enlace()})
	if r.status != 201 {
		e.t.Fatalf("conectar Horus: %d %s", r.status, r.crudo)
	}
	id := int64(r.cuerpo["id"].(float64))
	activar(e.t, admin, id)
	var secreto string
	e.horus.con(func() { secreto, _ = e.horus.canjes[len(e.horus.canjes)-1]["webhook_secreto"].(string) })
	return admin, id, secreto
}

// conectarArgos: el conector Argos por el otro sentido (enlace de Iris que Argos canjea) y
// activo, apuntando al Argos falso.
func (e *entorno) conectarArgos(admin *clienteHTTP) int64 {
	e.t.Helper()
	r := admin.pedir(http.MethodPost, "/api/v1/integraciones/argosisp/conexion/enlaces", nil)
	if r.status != 201 {
		e.t.Fatalf("enlace Argos: %d %s", r.status, r.crudo)
	}
	_, codigo, _ := strings.Cut(r.cuerpo["enlace"].(string), "#")
	c := e.anonimo().pedir(http.MethodPost, "/api/v1/integraciones/argosisp/conexion/canjear", map[string]any{
		"codigo": codigo, "sistema": "argosisp", "url": e.argos.srv.URL, "token": tokenArgos, "empresas": []int64{1, 2},
	})
	if c.status != 200 {
		e.t.Fatalf("canje Argos: %d %s", c.status, c.crudo)
	}
	id := int64(c.cuerpo["conectorId"].(float64))
	activar(e.t, admin, id)
	return id
}

// enviar firma y manda un webhook como lo haría Horus.
func (e *entorno) enviar(conectorID int64, secreto, id, evento string, payload map[string]any, marca time.Time) respuesta {
	e.t.Helper()
	cuerpo, _ := json.Marshal(map[string]any{"id": id, "evento": evento, "ocurrido_en": "2026-10-28T10:00:00Z", "payload": payload})
	ts := strconv.FormatInt(marca.Unix(), 10)
	return e.anonimo().pedir(http.MethodPost, fmt.Sprintf("/api/v1/integraciones/horus/webhook/%d", conectorID), cuerpo,
		"X-Webhook-Id", id, "X-Webhook-Timestamp", ts, "X-Webhook-Signature", conector.FirmarWebhook(secreto, ts, cuerpo))
}

// procesar manda el webhook, lo procesa (como el job) y devuelve el resultado y el payload del
// evento interno que emitió (nil si ninguno).
func (e *entorno) procesar(conectorID int64, secreto, evento string, payload map[string]any) (horus.ResultadoEntrante, map[string]any) {
	e.t.Helper()
	r := e.enviar(conectorID, secreto, "wh-"+pruebas.Unico(), evento, payload, time.Now())
	if r.status != 200 {
		e.t.Fatalf("webhook %s: %d %s", evento, r.status, r.crudo)
	}
	ent := int64(r.cuerpo["entranteId"].(float64))
	res, err := e.mod.ProcesarEntrante.Ejecutar(pruebas.ContextoSistema(e.pool), ent)
	if err != nil {
		e.t.Fatalf("procesar %s: %v", evento, err)
	}
	return res, e.evento(ent)
}

// evento devuelve el payload del evento interno de un entrante (nil si no hay), y verifica que
// la empresa del sobre sea la del payload.
func (e *entorno) evento(entrante int64) map[string]any {
	e.t.Helper()
	filas, err := e.pool.Query(context.Background(),
		`SELECT evento, empresa_id, payload FROM integracion.outbox WHERE (payload->>'entrante_id')::bigint = $1 AND evento LIKE 'horus.%'`, entrante)
	if err != nil {
		e.t.Fatal(err)
	}
	defer filas.Close()
	var salida map[string]any
	n := 0
	for filas.Next() {
		var evento string
		var empresa *int64
		var payload []byte
		if err := filas.Scan(&evento, &empresa, &payload); err != nil {
			e.t.Fatal(err)
		}
		n++
		_ = json.Unmarshal(payload, &salida)
		salida["_evento"] = evento
		if p, _ := salida["empresa_id"].(float64); (empresa == nil) != (salida["empresa_id"] == nil) || (empresa != nil && *empresa != int64(p)) {
			e.t.Fatalf("empresa del sobre %v ≠ payload %v", empresa, salida["empresa_id"])
		}
	}
	if n > 1 {
		e.t.Fatalf("%d eventos para el entrante %d", n, entrante)
	}
	return salida
}

// sinSecretos falla si el token o el secreto aparecen en claro en la base.
func (e *entorno) sinSecretos(secretos ...string) {
	e.t.Helper()
	todo := e.texto(`SELECT coalesce(string_agg(entrada::text || ' ' || coalesce(error::text, ''), ' '), '') FROM auditoria.acciones`) +
		e.texto(`SELECT coalesce(string_agg(coalesce(antes::text, '') || ' ' || coalesce(despues::text, ''), ' '), '') FROM auditoria.cambios`) +
		e.texto(`SELECT coalesce(string_agg(payload::text, ' '), '') FROM integracion.outbox`) +
		e.texto(`SELECT coalesce(string_agg(encode(token_cifrado, 'escape') || encode(secreto_webhook_cifrado, 'escape'), ' '), '') FROM integracion.conectores`)
	for _, s := range secretos {
		if s != "" && strings.Contains(todo, s) {
			e.t.Fatalf("el secreto %q quedó en claro en la base", s[:8])
		}
	}
}

func esProblema(err error, codigo string) bool {
	p, _ := nucleo.AProblema(err)
	return p.Codigo() == codigo
}

// Conexión ---------------------------------------------------------------------------------

func TestConectarHorusConEnlace(t *testing.T) {
	e := nuevoEntorno(t, loopback)
	admin := e.usuario("admin")
	e.usuario("lectura").pedir(http.MethodPost, "/api/v1/integraciones/horus/conectar", map[string]any{"enlace": e.horus.enlace()}).problema(t, 403, "prohibido")

	r := admin.pedir(http.MethodPost, "/api/v1/integraciones/horus/conectar", map[string]any{"enlace": e.horus.enlace()})
	if r.status != 201 || r.cuerpo["sistema"] != "horus" || r.cuerpo["estado"] != "apagado" || r.cuerpo["origen"] != "enlace_remoto" || r.cuerpo["remotoId"] != "hf-77" {
		t.Fatalf("conectar: %d %s", r.status, r.crudo)
	}
	id := int64(r.cuerpo["id"].(float64))
	if r.cuerpo["webhookUrl"] != fmt.Sprintf("https://iris.prueba/api/v1/integraciones/horus/webhook/%d", id) || r.cuerpo["urlPublica"] != e.horus.srv.URL {
		t.Fatalf("urls: %s", r.crudo)
	}
	if len(r.cuerpo["empresas"].([]any)) != 0 || strings.Contains(string(r.crudo), tokenHorus) {
		t.Fatalf("Horus no registra empresas ni expone el token: %s", r.crudo)
	}
	var canje map[string]any
	e.horus.con(func() { canje = e.horus.canjes[len(e.horus.canjes)-1] })
	if canje["sistema"] != "iris" || canje["url"] != "https://iris.prueba" || canje["webhook_url"] != r.cuerpo["webhookUrl"] || canje["webhook_secreto"] == "" {
		t.Fatalf("canje en Horus: %v", canje)
	}
	e.sinSecretos(tokenHorus, canje["webhook_secreto"].(string))
	if e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'conector.creado' AND payload->>'sistema' = 'horus' AND (payload->>'conector_id')::bigint = $1`, id) != 1 {
		t.Fatal("falta conector.creado")
	}
	// El código ya se usó: Horus lo rechaza y no se filtra su detail.
	r = admin.pedir(http.MethodPost, "/api/v1/integraciones/horus/conectar", map[string]any{"enlace": e.horus.srv.URL + "/conectar#codigo-de-horus-0123456789"})
	r.problema(t, 502, "horus_rechazo")
	if strings.Contains(string(r.crudo), "no debe llegar") || !strings.Contains(string(r.crudo), "HTTP 422") {
		t.Fatalf("detail de Horus filtrado: %s", r.crudo)
	}
	// Un token que no es hf_pat_ no se acepta.
	e.horus.con(func() { e.horus.codigo, e.horus.tokenMalo = "codigo-de-horus-otra-vez-0123456789", true })
	admin.pedir(http.MethodPost, "/api/v1/integraciones/horus/conectar", map[string]any{"enlace": e.horus.enlace()}).problema(t, 502, "horus_rechazo")
	e.horus.con(func() { e.horus.tokenMalo = false })

	// Apagado hasta activar; uno activo por sistema; Argos y Horus activos a la vez.
	activar(t, admin, id)
	otro := admin.pedir(http.MethodPost, "/api/v1/integraciones/horus/conectar", map[string]any{"enlace": e.horus.enlace()})
	if otro.status != 201 {
		t.Fatalf("segundo conector: %d %s", otro.status, otro.crudo)
	}
	admin.pedir(http.MethodPost, fmt.Sprintf("/api/v1/integraciones/conectores/%d/activar", int64(otro.cuerpo["id"].(float64))), map[string]any{"version": 1}).
		problema(t, 409, "otro_conector_activo")
	e.conectarArgos(admin)
	if n := e.contar(`SELECT count(*) FROM integracion.conectores WHERE estado = 'activo'`); n != 2 {
		t.Fatalf("activos: %d", n)
	}
	// Activar Horus no encola la carga de catálogos de Argos (cada sistema con su gancho).
	if n := e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'conector.activado' AND payload->>'sistema' = 'horus'`); n < 1 {
		t.Fatal("falta conector.activado de Horus")
	}
}

func TestCanjeLocalHorus(t *testing.T) {
	e := nuevoEntorno(t, loopback)
	admin := e.usuario("admin")
	r := admin.pedir(http.MethodPost, "/api/v1/integraciones/horus/conexion/enlaces", nil)
	if r.status != 201 {
		t.Fatalf("enlace: %d %s", r.status, r.crudo)
	}
	_, codigo, _ := strings.Cut(r.cuerpo["enlace"].(string), "#")
	anon := e.anonimo()
	canje := func(cuerpo map[string]any) respuesta {
		return anon.pedir(http.MethodPost, "/api/v1/integraciones/horus/conexion/canjear", cuerpo)
	}
	base := map[string]any{"codigo": codigo, "sistema": "horus", "url": e.horus.srv.URL, "token": tokenHorus}
	conCampo := func(k string, v any) map[string]any {
		m := map[string]any{}
		for a, b := range base {
			m[a] = b
		}
		m[k] = v
		return m
	}
	canje(conCampo("sistema", "argosisp")).problema(t, 422, "validacion")
	canje(conCampo("token", tokenArgos)).problema(t, 422, "validacion")
	// Un enlace de Argos no sirve para canjear como Horus.
	ra := admin.pedir(http.MethodPost, "/api/v1/integraciones/argosisp/conexion/enlaces", nil)
	_, codigoArgos, _ := strings.Cut(ra.cuerpo["enlace"].(string), "#")
	canje(conCampo("codigo", codigoArgos)).problema(t, 401, "codigo_invalido")

	ok := canje(base)
	if ok.status != 200 || ok.cuerpo["sistema"] != "iris" || ok.cuerpo["estado"] != "apagado" || ok.cuerpo["webhookSecreto"] == "" {
		t.Fatalf("canje: %d %s", ok.status, ok.crudo)
	}
	id := int64(ok.cuerpo["conectorId"].(float64))
	if ok.cuerpo["webhookUrl"] != fmt.Sprintf("https://iris.prueba/api/v1/integraciones/horus/webhook/%d", id) {
		t.Fatalf("webhookUrl: %s", ok.crudo)
	}
	canje(base).problema(t, 401, "codigo_invalido")
	if e.texto(`SELECT origen || ':' || sistema FROM integracion.conectores WHERE id = $1`, id) != "enlace_local:horus" {
		t.Fatal("conector mal guardado")
	}
	e.sinSecretos(tokenHorus, ok.cuerpo["webhookSecreto"].(string))
}

// Webhook --------------------------------------------------------------------------------

func TestWebhookHorus(t *testing.T) {
	e := nuevoEntorno(t, loopback)
	admin, id, secreto := e.conectarHorus()
	ahora := time.Now()
	wid := "wh-" + pruebas.Unico()
	payload := map[string]any{"router_id": 9, "estado": "caido"}

	r := e.enviar(id, secreto, wid, horus.EventoRouterEstadoCambiado, payload, ahora)
	if r.status != 200 || r.cuerpo["duplicado"] != false {
		t.Fatalf("webhook: %d %s", r.status, r.crudo)
	}
	ent := int64(r.cuerpo["entranteId"].(float64))
	if n := e.contar(`SELECT count(*) FROM river.river_job WHERE kind = 'integracion.horus_procesar_entrante' AND (args->>'entrante_id')::bigint = $1`, ent); n != 1 {
		t.Fatalf("job encolado: %d", n)
	}
	// Reintento de Horus (mismo id firmado): 200 duplicado, una fila y un job.
	d := e.enviar(id, secreto, wid, horus.EventoRouterEstadoCambiado, payload, ahora)
	if d.status != 200 || d.cuerpo["duplicado"] != true || int64(d.cuerpo["entranteId"].(float64)) != ent {
		t.Fatalf("duplicado: %d %s", d.status, d.crudo)
	}
	if e.contar(`SELECT count(*) FROM river.river_job WHERE kind = 'integracion.horus_procesar_entrante' AND (args->>'entrante_id')::bigint = $1`, ent) != 1 {
		t.Fatal("el duplicado encoló otro job")
	}

	antes, rechazos := e.contar(`SELECT count(*) FROM auditoria.acciones`), e.am.RechazosSinAuditar()
	e.enviar(id, "otro-secreto", "wh-x1", horus.EventoRouterEstadoCambiado, payload, ahora).problema(t, 401, "firma_invalida")
	e.enviar(id, secreto, "wh-x2", horus.EventoRouterEstadoCambiado, payload, ahora.Add(-10*time.Minute)).problema(t, 401, "firma_vencida")
	e.enviar(id+1000, secreto, "wh-x3", horus.EventoRouterEstadoCambiado, payload, ahora).problema(t, 401, "firma_invalida")
	// Un conector de Argos por la ruta de Horus: mismo 401 (no revela qué existe).
	argosID := e.conectarArgos(admin)
	antes += e.contar(`SELECT count(*) FROM auditoria.acciones`) - antes
	e.enviar(argosID, secreto, "wh-x4", horus.EventoRouterEstadoCambiado, payload, ahora).problema(t, 401, "firma_invalida")
	if d := e.contar(`SELECT count(*) FROM auditoria.acciones`) - antes; d != 0 {
		t.Fatalf("los rechazos anónimos dejaron %d filas de auditoría", d)
	}
	if e.am.RechazosSinAuditar()-rechazos != 4 {
		t.Fatalf("rechazos contados: %d", e.am.RechazosSinAuditar()-rechazos)
	}

	// El id firmado del cuerpo manda: otra cabecera, 422.
	cuerpo, _ := json.Marshal(map[string]any{"id": "wh-cuerpo", "evento": horus.EventoRouterEstadoCambiado, "payload": payload})
	ts := strconv.FormatInt(ahora.Unix(), 10)
	e.anonimo().pedir(http.MethodPost, fmt.Sprintf("/api/v1/integraciones/horus/webhook/%d", id), cuerpo,
		"X-Webhook-Id", "wh-otra-cabecera", "X-Webhook-Timestamp", ts, "X-Webhook-Signature", conector.FirmarWebhook(secreto, ts, cuerpo)).
		problema(t, 422, "validacion")
	// Sin id en el cuerpo, 422.
	sinID, _ := json.Marshal(map[string]any{"evento": horus.EventoRouterEstadoCambiado, "payload": payload})
	e.anonimo().pedir(http.MethodPost, fmt.Sprintf("/api/v1/integraciones/horus/webhook/%d", id), sinID,
		"X-Webhook-Id", "wh-sin-id", "X-Webhook-Timestamp", ts, "X-Webhook-Signature", conector.FirmarWebhook(secreto, ts, sinID)).
		problema(t, 422, "validacion")
	// Más de 1 MiB: 413 antes de leer la firma.
	grande := []byte(`{"id":"wh-grande","evento":"x","payload":{"relleno":"` + strings.Repeat("a", 1<<20) + `"}}`)
	if r := e.anonimo().pedir(http.MethodPost, fmt.Sprintf("/api/v1/integraciones/horus/webhook/%d", id), grande,
		"X-Webhook-Id", "wh-grande", "X-Webhook-Timestamp", ts, "X-Webhook-Signature", conector.FirmarWebhook(secreto, ts, grande)); r.status != http.StatusRequestEntityTooLarge {
		t.Fatalf("cuerpo grande: %d %s", r.status, r.crudo)
	}

	// Apagado: 503 con Retry-After (Horus reintenta).
	ver := admin.pedir(http.MethodGet, "/api/v1/integraciones/conectores?limit=200", nil)
	var version float64
	for _, c := range ver.cuerpo["data"].([]any) {
		if m := c.(map[string]any); int64(m["id"].(float64)) == id {
			version = m["version"].(float64)
		}
	}
	if r := admin.pedir(http.MethodPost, fmt.Sprintf("/api/v1/integraciones/conectores/%d/desactivar", id), map[string]any{"version": version}); r.status != 200 {
		t.Fatalf("desactivar: %d %s", r.status, r.crudo)
	}
	r = e.enviar(id, secreto, "wh-apagado", horus.EventoRouterEstadoCambiado, payload, time.Now())
	r.problema(t, 503, "conector_apagado")
	if r.h.Get("Retry-After") == "" {
		t.Fatal("falta Retry-After")
	}
}

// Eventos internos -------------------------------------------------------------------------

func TestEventosInternosDeRouterEInfra(t *testing.T) {
	e := nuevoEntorno(t, loopback)
	_, id, secreto := e.conectarHorus()
	if _, err := e.pool.Exec(context.Background(),
		`INSERT INTO integracion.argos_routers (id, empresa_id, nombre, sincronizado_en) VALUES (9, 2, 'RB-Centro', now())`); err != nil {
		t.Fatal(err)
	}

	// Sin empresa en el payload: la del router en la instantánea de Argos.
	res, ev := e.procesar(id, secreto, horus.EventoRouterEstadoCambiado, map[string]any{"routerId": "9", "nombre": "RB-Centro", "estado": "caido"})
	if res.Resultado != "aplicado" || ev["_evento"] != horus.EventoRouterEstadoCambiado || ev["router_id"] != float64(9) ||
		ev["estado"] != "caido" || ev["empresa_id"] != float64(2) || ev["nombre"] != "RB-Centro" || ev["ocurrido_en"] != "2026-10-28T10:00:00Z" {
		t.Fatalf("router caído: %+v %v", res, ev)
	}
	// Idempotente: procesar otra vez no emite de nuevo.
	ent := int64(ev["entrante_id"].(float64))
	if r, err := e.mod.ProcesarEntrante.Ejecutar(pruebas.ContextoSistema(e.pool), ent); err != nil || r.Resultado != "ya_procesado" {
		t.Fatalf("segunda vez: %+v %v", r, err)
	}
	e.evento(ent) // falla si hay dos

	// Estado en inglés, router anidado y empresa del payload.
	_, ev = e.procesar(id, secreto, horus.EventoRouterEstadoCambiado, map[string]any{"router": map[string]any{"id": 9, "nombre": "RB"}, "estado": "UP", "empresa_id": 1})
	if ev["estado"] != "arriba" || ev["empresa_id"] != float64(1) || ev["nombre"] != "RB" {
		t.Fatalf("router arriba: %v", ev)
	}
	// Router desconocido sin empresa: empresa null.
	_, ev = e.procesar(id, secreto, horus.EventoRouterEstadoCambiado, map[string]any{"router_id": 404, "estado": "down"})
	if ev["empresa_id"] != nil || ev["router_id"] != float64(404) {
		t.Fatalf("router sin instantánea: %v", ev)
	}

	// Servicio de infraestructura.
	_, ev = e.procesar(id, secreto, horus.EventoServicioInfraCambiado, map[string]any{"infraId": "olt-3", "nombre": "OLT Norte", "tipo": "OLT", "estado": "caido", "router_id": 9})
	if ev["_evento"] != horus.EventoServicioInfraCambiado || ev["infra_id"] != "olt-3" || ev["tipo"] != "olt" || ev["router_id"] != float64(9) || ev["empresa_id"] != float64(2) {
		t.Fatalf("infra: %v", ev)
	}

	// Payload inválido o evento desconocido: «ignorado» con la nota, sin evento.
	for evento, p := range map[string]map[string]any{
		horus.EventoRouterEstadoCambiado:  {"estado": "caido"},
		horus.EventoServicioInfraCambiado: {"infra_id": "x", "estado": "raro"},
		horus.EventoHallazgoAbierto:       {"hallazgo_id": "h-1"},
		"horus.algo_nuevo":                {"x": 1},
	} {
		res, ev := e.procesar(id, secreto, evento, p)
		if res.Resultado != "ignorado" || ev != nil {
			t.Fatalf("%s: %+v %v", evento, res, ev)
		}
		if nota := e.texto(`SELECT coalesce(error, '') FROM integracion.entrantes WHERE id = $1`, res.EntranteID); nota == "" {
			t.Fatalf("%s sin nota", evento)
		}
	}
}

func TestHallazgosConClienteResueltoPorArgos(t *testing.T) {
	e := nuevoEntorno(t, loopback)
	admin, id, secreto := e.conectarHorus()
	// El router 9 es de la empresa 1 (instantánea de Argos): los hallazgos sin empresa_id toman
	// la de su router.
	if _, err := e.pool.Exec(context.Background(), `INSERT INTO integracion.argos_routers (id, empresa_id, nombre, sincronizado_en) VALUES (9, 1, 'RB', now())`); err != nil {
		t.Fatal(err)
	}
	hallazgo := func(extra map[string]any) map[string]any {
		p := map[string]any{"hallazgoId": "hz-" + pruebas.Unico(), "categoria": "Equipo_Infectado", "severidad": "ALTA",
			"resumen": "Tráfico hacia C2 conocido", "router_id": 9}
		for k, v := range extra {
			p[k] = v
		}
		return p
	}
	motivo := func(ev map[string]any) any { return ev["motivo_sin_cliente"] }

	// Sin conector Argos activo: sin cliente, con motivo.
	_, ev := e.procesar(id, secreto, horus.EventoHallazgoAbierto, hallazgo(map[string]any{"ip": "100.64.10.20"}))
	if ev["cliente_id"] != nil || motivo(ev) != horus.MotivoConectorArgosApagado || ev["ip"] != "100.64.10.20" || ev["categoria"] != "equipo_infectado" || ev["severidad"] != "alta" {
		t.Fatalf("sin Argos: %v", ev)
	}

	e.conectarArgos(admin)
	e.argos.con(func() {
		e.argos.ips["100.64.10.20"] = map[string]any{"clienteId": 501, "servicioId": 9001, "empresaId": 1}
		e.argos.ips["100.64.10.21"] = map[string]any{"servicio": map[string]any{"id": 9002, "cliente_id": 502, "empresa_id": 2}}
	})
	// Resuelto por Argos (A8), con la URL de Horus del mismo origen.
	_, ev = e.procesar(id, secreto, horus.EventoHallazgoAbierto, hallazgo(map[string]any{"ip": "100.64.10.20", "url": e.horus.srv.URL + "/hallazgos/hz-1"}))
	if ev["cliente_id"] != float64(501) || ev["servicio_id"] != float64(9001) || ev["empresa_id"] != float64(1) ||
		ev["fuente_cliente"] != "argos" || motivo(ev) != nil || ev["url_horus"] != e.horus.srv.URL+"/hallazgos/hz-1" || ev["router_id"] != float64(9) {
		t.Fatalf("resuelto: %v", ev)
	}
	// Sin empresa_id, la del router.
	_, ev = e.procesar(id, secreto, horus.EventoHallazgoResuelto, map[string]any{"hallazgo_id": 78, "router_id": 9})
	if ev["empresa_id"] != float64(1) {
		t.Fatalf("resuelto con router: %v", ev)
	}
	// Una URL de otro origen no se publica (el panel no enlaza a donde diga un webhook).
	_, ev = e.procesar(id, secreto, horus.EventoHallazgoAbierto, hallazgo(map[string]any{"ip": "100.64.10.21", "empresa_id": 2, "url": "https://malo.example/x"}))
	if ev["url_horus"] != nil || ev["cliente_id"] != float64(502) || ev["empresa_id"] != float64(2) {
		t.Fatalf("otro origen: %v", ev)
	}
	// Empresa del payload distinta de la del cliente: no se asigna.
	_, ev = e.procesar(id, secreto, horus.EventoHallazgoAbierto, hallazgo(map[string]any{"ip": "100.64.10.21", "empresa_id": 1}))
	if ev["cliente_id"] != nil || motivo(ev) != horus.MotivoOtraEmpresa || ev["empresa_id"] != float64(1) {
		t.Fatalf("otra empresa: %v", ev)
	}
	// Regresión (revisión): sin empresa_id, la del router acota la IP; una IP repetida en otra
	// empresa (CGNAT) no da el cliente ajeno.
	_, ev = e.procesar(id, secreto, horus.EventoHallazgoAbierto, hallazgo(map[string]any{"ip": "100.64.10.21"}))
	if ev["cliente_id"] != nil || motivo(ev) != horus.MotivoOtraEmpresa || ev["empresa_id"] != float64(1) {
		t.Fatalf("router de otra empresa: %v", ev)
	}
	// Sin empresa_id ni router conocido: no se busca la IP en todas las empresas.
	sinEmpresa := hallazgo(map[string]any{"ip": "100.64.10.20"})
	delete(sinEmpresa, "router_id")
	_, ev = e.procesar(id, secreto, horus.EventoHallazgoAbierto, sinEmpresa)
	if ev["cliente_id"] != nil || motivo(ev) != horus.MotivoEmpresaDesconocida || ev["empresa_id"] != nil {
		t.Fatalf("sin empresa: %v", ev)
	}
	_, ev = e.procesar(id, secreto, horus.EventoHallazgoAbierto, hallazgo(map[string]any{"ip": "100.64.10.20", "router_id": 404}))
	if ev["cliente_id"] != nil || motivo(ev) != horus.MotivoEmpresaDesconocida {
		t.Fatalf("router desconocido: %v", ev)
	}
	// IP sin cliente, sin IP, IP inválida.
	_, ev = e.procesar(id, secreto, horus.EventoHallazgoAbierto, hallazgo(map[string]any{"ip": "100.64.99.99"}))
	if ev["cliente_id"] != nil || motivo(ev) != horus.MotivoIPSinCliente || ev["empresa_id"] != float64(1) {
		t.Fatalf("ip sin cliente: %v", ev)
	}
	_, ev = e.procesar(id, secreto, horus.EventoHallazgoAbierto, hallazgo(nil))
	if motivo(ev) != horus.MotivoSinIP || ev["ip"] != nil {
		t.Fatalf("sin ip: %v", ev)
	}
	_, ev = e.procesar(id, secreto, horus.EventoHallazgoAbierto, hallazgo(map[string]any{"ip": "no-es-ip"}))
	if motivo(ev) != "ip_invalida" || ev["ip"] != nil {
		t.Fatalf("ip inválida: %v", ev)
	}
	// Argos caído: la instantánea si la hay; si no, sin cliente con motivo.
	e.argos.con(func() { e.argos.caido = true })
	_, ev = e.procesar(id, secreto, horus.EventoHallazgoAbierto, hallazgo(map[string]any{"ip": "100.64.10.20"}))
	if ev["cliente_id"] != nil || motivo(ev) != horus.MotivoArgosNoDisponible {
		t.Fatalf("Argos caído sin instantánea: %v", ev)
	}
	for _, sql := range []string{
		`INSERT INTO integracion.argos_clientes (id, empresa_id, nombre, sincronizado_en) VALUES (501, 1, 'Ana', now())`,
		`INSERT INTO integracion.argos_servicios (id, empresa_id, cliente_id, ip, sincronizado_en) VALUES (9001, 1, 501, '100.64.10.20', now())`,
	} {
		if _, err := e.pool.Exec(context.Background(), sql); err != nil {
			t.Fatal(err)
		}
	}
	_, ev = e.procesar(id, secreto, horus.EventoHallazgoAbierto, hallazgo(map[string]any{"ip": "100.64.10.20"}))
	if ev["cliente_id"] != float64(501) || ev["fuente_cliente"] != "instantanea" || ev["empresa_id"] != float64(1) {
		t.Fatalf("Argos caído con instantánea: %v", ev)
	}

	// Hallazgo resuelto.
	_, ev = e.procesar(id, secreto, horus.EventoHallazgoResuelto, map[string]any{"hallazgo_id": 77, "categoria": "equipo_infectado", "empresa_id": 1, "ip": "100.64.10.20"})
	if _, conIP := ev["ip"]; ev["_evento"] != horus.EventoHallazgoResuelto || ev["hallazgo_id"] != "77" || ev["empresa_id"] != float64(1) || conIP {
		t.Fatalf("resuelto: %v", ev)
	}
	// La auditoría del job anota el cliente resuelto (lectura de datos de Argos).
	if e.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'integraciones.procesar_webhook' AND entrada->'auditoria'->>'cliente_id' = '501'`) < 1 {
		t.Fatal("la auditoría del job no anota el cliente")
	}
	e.sinSecretos(tokenHorus, tokenArgos, secreto)
}

// El worker de River procesa el webhook y deja el evento en el outbox.
func TestWebhookHorusConRiver(t *testing.T) {
	e := nuevoEntorno(t, loopback)
	_, id, secreto := e.conectarHorus()
	relay := &integracion.Relay{BD: e.pool, Bus: integracion.NuevoBus()}
	cliente, err := integracion.NuevoClienteRiver(e.pool, relay, integracion.OpcionesWorker{
		Esquema: migraciones.EsquemaRiver, RelayIntervalo: time.Hour,
		Registrar: func(w *river.Workers) {
			e.am.RegistrarWorkers(w, e.pool)
			e.mod.RegistrarWorkers(w, e.pool)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancelar := context.WithCancel(context.Background())
	defer cancelar()
	if err := cliente.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() {
		parar, c := context.WithTimeout(context.Background(), 10*time.Second)
		defer c()
		_ = cliente.Stop(parar)
	}()
	r := e.enviar(id, secreto, "wh-river-"+pruebas.Unico(), horus.EventoRouterEstadoCambiado, map[string]any{"router_id": 12, "estado": "caido", "empresa_id": 1}, time.Now())
	if r.status != 200 {
		t.Fatalf("webhook: %d %s", r.status, r.crudo)
	}
	ent := int64(r.cuerpo["entranteId"].(float64))
	limite := time.Now().Add(20 * time.Second)
	for time.Now().Before(limite) && e.contar(`SELECT count(*) FROM integracion.entrantes WHERE id = $1 AND procesado_en IS NOT NULL`, ent) == 0 {
		time.Sleep(50 * time.Millisecond)
	}
	if ev := e.evento(ent); ev == nil || ev["router_id"] != float64(12) {
		t.Fatalf("River no procesó el webhook: %v", ev)
	}
}

// Lecturas, SSRF y enlaces -------------------------------------------------------------------

func TestLecturasDeHorus(t *testing.T) {
	e := nuevoEntorno(t, loopback)
	ctx := pruebas.ContextoUsuarioEnEmpresa(e.pool, 1, 1, nucleo.PermClientesVer)
	// Sin conector Horus activo: 503.
	if _, err := e.mod.ConsultarSaludServicio.Ejecutar(ctx, horus.EntradaLecturaHorus{ID: 9001}); !esProblema(err, "conector_apagado") {
		t.Fatalf("apagado: %v", err)
	}
	e.conectarHorus()
	for _, sql := range []string{
		`INSERT INTO integracion.argos_clientes (id, empresa_id, nombre, sincronizado_en) VALUES (501, 1, 'Ana', now()), (502, 2, 'Beto', now())`,
		`INSERT INTO integracion.argos_servicios (id, empresa_id, cliente_id, ip, sincronizado_en) VALUES (9001, 1, 501, '100.64.10.20', now()), (9002, 2, 502, '100.64.10.21', now())`,
		`INSERT INTO integracion.argos_routers (id, empresa_id, nombre, sincronizado_en) VALUES (9, 1, 'RB-Centro', now())`,
	} {
		if _, err := e.pool.Exec(context.Background(), sql); err != nil {
			t.Fatal(err)
		}
	}
	s, err := e.mod.ConsultarSaludServicio.Ejecutar(ctx, horus.EntradaLecturaHorus{ID: 9001})
	if err != nil || s.Estado != "en_linea" || s.BajadaBps == nil || *s.BajadaBps != 12500000 || s.LatenciaMs == nil || *s.LatenciaMs != 18.5 ||
		s.PerdidaPct != nil || s.ActualizadoEn == nil {
		t.Fatalf("salud: %+v %v", s, err)
	}
	// De otra empresa o desconocido: 404 (no se pregunta a Horus).
	for _, sid := range []int64{9002, 9999} {
		if _, err := e.mod.ConsultarSaludServicio.Ejecutar(ctx, horus.EntradaLecturaHorus{ID: sid}); !esProblema(err, "no_encontrado") {
			t.Fatalf("servicio %d: %v", sid, err)
		}
	}
	r, err := e.mod.ConsultarRouter.Ejecutar(ctx, horus.EntradaLecturaHorus{ID: 9})
	if err != nil || r.Estado != "caido" || r.Nombre != "RB-Centro" || r.Desde == nil {
		t.Fatalf("router: %+v %v", r, err)
	}
	// Sin el permiso en Horus: 502 horus_sin_permiso.
	e.horus.con(func() { e.horus.sinPermiso = true })
	if _, err := e.mod.ConsultarRouter.Ejecutar(ctx, horus.EntradaLecturaHorus{ID: 9}); !esProblema(err, "horus_sin_permiso") {
		t.Fatalf("sin permiso: %v", err)
	}
	// Sin clientes.ver: 403.
	if _, err := e.mod.ConsultarRouter.Ejecutar(pruebas.ContextoUsuarioEnEmpresa(e.pool, 1, 1, nucleo.PermTicketsVer), horus.EntradaLecturaHorus{ID: 9}); !esProblema(err, "prohibido") {
		t.Fatalf("sin permiso en Iris: %v", err)
	}
	if n := e.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'horus.salud_servicio' AND entidad_id = '9001' AND resultado = 'ok'`); n != 1 {
		t.Fatalf("lectura auditada: %d", n)
	}
}

// Anti-SSRF: sin IRIS_HORUS_REDES_PERMITIDAS, Horus no puede estar en una red interna: ni
// escrita como IP ni detrás de un nombre que resuelve a loopback (se corta al conectar).
func TestHorusNoLlamaARedesInternas(t *testing.T) {
	e := nuevoEntorno(t, nil)
	admin := e.usuario("admin")
	_, puerto, _ := strings.Cut(strings.TrimPrefix(e.horus.srv.URL, "http://"), ":")
	admin.pedir(http.MethodPost, "/api/v1/integraciones/horus/conectar", map[string]any{"enlace": "http://127.0.0.1:" + puerto + "/conectar#codigo-de-horus-0123456789"}).
		problema(t, 422, "validacion")
	admin.pedir(http.MethodPost, "/api/v1/integraciones/horus/conectar", map[string]any{"enlace": "http://localhost:" + puerto + "/conectar#codigo-de-horus-0123456789"}).
		problema(t, 422, "destino_no_permitido")
	var canjes int
	e.horus.con(func() { canjes = len(e.horus.canjes) })
	if canjes != 0 {
		t.Fatal("Iris llegó a la red interna")
	}
	// Un conector guardado con un nombre interno (canje local) tampoco lee.
	r := admin.pedir(http.MethodPost, "/api/v1/integraciones/horus/conexion/enlaces", nil)
	_, codigo, _ := strings.Cut(r.cuerpo["enlace"].(string), "#")
	c := e.anonimo().pedir(http.MethodPost, "/api/v1/integraciones/horus/conexion/canjear", map[string]any{
		"codigo": codigo, "sistema": "horus", "url": "http://localhost:" + puerto, "token": tokenHorus,
	})
	if c.status != 200 {
		t.Fatalf("canje: %d %s", c.status, c.crudo)
	}
	activar(t, admin, int64(c.cuerpo["conectorId"].(float64)))
	if _, err := e.pool.Exec(context.Background(), `INSERT INTO integracion.argos_routers (id, empresa_id, nombre, sincronizado_en) VALUES (9, 1, 'RB', now())`); err != nil {
		t.Fatal(err)
	}
	if _, err := e.mod.ConsultarRouter.Ejecutar(pruebas.ContextoUsuarioEnEmpresa(e.pool, 1, 1, nucleo.PermClientesVer), horus.EntradaLecturaHorus{ID: 9}); !esProblema(err, "destino_no_permitido") {
		t.Fatalf("lectura a red interna: %v", err)
	}
}

func TestEnlacesExternosYURLPublica(t *testing.T) {
	e := nuevoEntorno(t, loopback)
	lectura := e.usuario("lectura")
	if r := lectura.pedir(http.MethodGet, "/api/v1/integraciones/enlaces", nil); r.status != 200 || r.cuerpo["argosisp"] != nil || r.cuerpo["horus"] != nil {
		t.Fatalf("sin conectores: %d %s", r.status, r.crudo)
	}
	e.anonimo().pedir(http.MethodGet, "/api/v1/integraciones/enlaces", nil).problema(t, 401, "no_autenticado")
	// Sin clientes.ver no conoce los orígenes de Argos ni de Horus.
	e.usuario("integracion_horus").pedir(http.MethodGet, "/api/v1/integraciones/enlaces", nil).problema(t, 403, "prohibido")
	admin, id, _ := e.conectarHorus()
	e.conectarArgos(admin)
	r := lectura.pedir(http.MethodGet, "/api/v1/integraciones/enlaces", nil)
	h, _ := r.cuerpo["horus"].(map[string]any)
	a, _ := r.cuerpo["argosisp"].(map[string]any)
	if r.status != 200 || h["urlPublica"] != e.horus.srv.URL || a["urlPublica"] != e.argos.srv.URL || h["conectorId"] != float64(id) {
		t.Fatalf("enlaces: %d %s", r.status, r.crudo)
	}
	if strings.Contains(string(r.crudo), "hf_pat_") || strings.Contains(string(r.crudo), "webhook") {
		t.Fatalf("los enlaces exponen de más: %s", r.crudo)
	}

	// URL pública: solo con integraciones.conectar; origen http(s) limpio; null vuelve a la base.
	url := fmt.Sprintf("/api/v1/integraciones/conectores/%d/url-publica", id)
	version := func() float64 {
		lista := admin.pedir(http.MethodGet, "/api/v1/integraciones/conectores?limit=200", nil)
		for _, c := range lista.cuerpo["data"].([]any) {
			if m := c.(map[string]any); int64(m["id"].(float64)) == id {
				return m["version"].(float64)
			}
		}
		t.Fatal("no está el conector")
		return 0
	}
	lectura.pedir(http.MethodPost, url, map[string]any{"version": version(), "urlPublica": "https://horus.isp.gt"}).problema(t, 403, "prohibido")
	for _, malo := range []string{"javascript:alert(1)", "https://u:p@horus.isp.gt", "https://horus.isp.gt/?x=1", "horus.isp.gt"} {
		admin.pedir(http.MethodPost, url, map[string]any{"version": version(), "urlPublica": malo}).problema(t, 422, "validacion")
	}
	admin.pedir(http.MethodPost, url, map[string]any{"version": 99, "urlPublica": "https://horus.isp.gt"}).problema(t, 409, "conflicto_version")
	if r := admin.pedir(http.MethodPost, url, map[string]any{"version": version(), "urlPublica": "https://horus.isp.gt/panel/"}); r.status != 200 || r.cuerpo["urlPublica"] != "https://horus.isp.gt/panel" {
		t.Fatalf("configurar: %d %s", r.status, r.crudo)
	}
	if r := lectura.pedir(http.MethodGet, "/api/v1/integraciones/enlaces", nil); r.cuerpo["horus"].(map[string]any)["urlPublica"] != "https://horus.isp.gt/panel" {
		t.Fatalf("enlaces tras configurar: %s", r.crudo)
	}
	if e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'conector.actualizado' AND (payload->>'conector_id')::bigint = $1 AND payload->'campos' ? 'url_publica'`, id) != 1 {
		t.Fatal("falta conector.actualizado")
	}
	if r := admin.pedir(http.MethodPost, url, map[string]any{"version": version(), "urlPublica": nil}); r.status != 200 || r.cuerpo["urlPublica"] != e.horus.srv.URL {
		t.Fatalf("volver a la base: %d %s", r.status, r.crudo)
	}
}
