package servidor_test

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/servidor"
)

func TestMain(m *testing.M) { os.Exit(pruebas.Correr(m)) }

const contrasena = "contrasena-de-prueba-123"

type entorno struct {
	t   *testing.T
	srv *httptest.Server
	mod *identidad.Modulo
}

func nuevoEntorno(t *testing.T) *entorno {
	t.Helper()
	clave := make([]byte, 32)
	_, _ = rand.Read(clave)
	cifrador, err := nucleo.NuevoCifrador(base64.StdEncoding.EncodeToString(clave))
	if err != nil {
		t.Fatal(err)
	}
	mod := identidad.Nuevo(identidad.Config{Cifrador: cifrador, CookieSegura: false})
	h, _ := servidor.Nuevo(servidor.Deps{
		BD: pruebas.Pool(t), Identidad: mod, Version: "prueba",
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &entorno{t: t, srv: srv, mod: mod}
}

// usuario crea un usuario con los roles dados (como el sistema) y devuelve su id y correo.
func (e *entorno) usuario(roles ...string) (int64, string) {
	e.t.Helper()
	correo := pruebas.Correo("u")
	asign := []identidad.AsignacionRol{}
	for _, r := range roles {
		asign = append(asign, identidad.AsignacionRol{Rol: r})
	}
	u, err := e.mod.CrearUsuario.Ejecutar(pruebas.ContextoSistema(pruebas.Pool(e.t)), identidad.EntradaCrearUsuario{
		Nombre: "Usuario " + correo, Correo: correo, Contrasena: contrasena, Roles: asign,
	})
	if err != nil {
		e.t.Fatal(err)
	}
	return u.ID, correo
}

type cliente struct {
	t      *testing.T
	base   string
	http   *http.Client
	csrf   string
	bearer string
}

func (e *entorno) cliente() *cliente {
	jar, _ := cookiejar.New(nil)
	return &cliente{t: e.t, base: e.srv.URL, http: &http.Client{Jar: jar}}
}

type respuesta struct {
	status int
	cuerpo map[string]any
	crudo  []byte
	h      http.Header
}

func (r respuesta) problema(t *testing.T, status int, tipo string) {
	t.Helper()
	if r.status != status {
		t.Fatalf("status %d, esperaba %d: %s", r.status, status, r.crudo)
	}
	if ct := r.h.Get("Content-Type"); !strings.HasPrefix(ct, "application/problem+json") {
		t.Fatalf("Content-Type %q, esperaba application/problem+json", ct)
	}
	if r.cuerpo["type"] != "urn:iris:problema:"+tipo || r.cuerpo["status"] != float64(status) || r.cuerpo["title"] == "" {
		t.Fatalf("problema inesperado: %s", r.crudo)
	}
}

func (c *cliente) pedir(metodo, ruta string, cuerpo any, cabeceras ...string) respuesta {
	c.t.Helper()
	var lector io.Reader
	if cuerpo != nil {
		b, _ := json.Marshal(cuerpo)
		lector = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(metodo, c.base+ruta, lector)
	if cuerpo != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.csrf != "" {
		req.Header.Set(identidad.CabeceraCSRF, c.csrf)
	}
	if c.bearer != "" {
		req.Header.Set("Authorization", "Bearer "+c.bearer)
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

func (c *cliente) entrar(correo string) respuesta {
	c.t.Helper()
	r := c.pedir(http.MethodPost, "/api/v1/sesion", map[string]any{"correo": correo, "contrasena": contrasena})
	if r.status == http.StatusOK {
		c.csrf, _ = r.cuerpo["csrfToken"].(string)
	}
	return r
}

func (c *cliente) cookie(nombre string) *http.Cookie {
	u, _ := url.Parse(c.base)
	for _, k := range c.http.Jar.Cookies(u) {
		if k.Name == nombre {
			return k
		}
	}
	return nil
}

func TestSaludListoY404(t *testing.T) {
	e := nuevoEntorno(t)
	c := e.cliente()
	if r := c.pedir(http.MethodGet, "/api/v1/salud", nil); r.status != 200 || r.cuerpo["estado"] != "ok" {
		t.Fatalf("salud: %d %s", r.status, r.crudo)
	}
	r := c.pedir(http.MethodGet, "/api/v1/listo", nil)
	if r.status != 200 || r.h.Get(servidor.CabeceraRequestID) == "" {
		t.Fatalf("listo: %d %s", r.status, r.crudo)
	}
	c.pedir(http.MethodGet, "/api/v1/no-existe", nil).problema(t, 404, "no_encontrado")
}

func TestLoginCSRFYPermisoDenegado(t *testing.T) {
	e := nuevoEntorno(t)
	_, correo := e.usuario("soporte") // usuarios.ver y tickets.*; sin usuarios.crear
	c := e.cliente()

	r := c.pedir(http.MethodPost, "/api/v1/sesion", map[string]any{"correo": correo, "contrasena": "mala-contrasena-123"})
	r.problema(t, 401, "credenciales_invalidas")
	c.pedir(http.MethodPost, "/api/v1/sesion", map[string]any{"correo": "nadie@prueba.iris.gt", "contrasena": "x"}).problema(t, 401, "credenciales_invalidas")
	c.pedir(http.MethodGet, "/api/v1/yo", nil).problema(t, 401, "no_autenticado")

	r = c.entrar(correo)
	if r.status != 200 || r.cuerpo["mfaRequerido"] != false || c.csrf == "" {
		t.Fatalf("login: %d %s", r.status, r.crudo)
	}
	sesion := c.cookie(identidad.CookieSesion)
	if sesion == nil || c.cookie(identidad.CookieCSRF).Value != c.csrf {
		t.Fatal("faltan las cookies iris_sesion e iris_csrf")
	}
	if strings.Contains(string(r.crudo), sesion.Value) {
		t.Fatal("el valor de la cookie de sesión no debe ir en el cuerpo")
	}
	var setCookie string
	for _, v := range r.h.Values("Set-Cookie") {
		if strings.HasPrefix(v, identidad.CookieSesion+"=") {
			setCookie = v
		}
	}
	if !strings.Contains(setCookie, "HttpOnly") || !strings.Contains(setCookie, "SameSite=Lax") {
		t.Fatalf("cookie de sesión sin HttpOnly/SameSite=Lax: %s", setCookie)
	}

	yo := c.pedir(http.MethodGet, "/api/v1/yo", nil)
	permisosYo := fmt.Sprint(yo.cuerpo["permisos"])
	if yo.status != 200 || yo.cuerpo["actor"] != "usuario" || strings.Contains(permisosYo, "usuarios.crear") ||
		!strings.Contains(permisosYo, "usuarios.ver") || !strings.Contains(permisosYo, "clientes.ver") ||
		!strings.Contains(permisosYo, "clientes.estado_cuenta") || !strings.Contains(permisosYo, "tickets.ver") {
		t.Fatalf("yo: %d %s", yo.status, yo.crudo)
	}
	if r := c.pedir(http.MethodGet, "/api/v1/usuarios?limit=1", nil); r.status != 200 {
		t.Fatalf("listar con usuarios.ver: %d %s", r.status, r.crudo)
	}

	nuevo := map[string]any{"nombre": "Otro", "correo": pruebas.Correo("otro"), "contrasena": contrasena}
	guardado := c.csrf
	c.csrf = ""
	c.pedir(http.MethodPost, "/api/v1/usuarios", nuevo).problema(t, 403, "csrf_invalido")
	c.csrf = "otro-valor"
	c.pedir(http.MethodPost, "/api/v1/usuarios", nuevo).problema(t, 403, "csrf_invalido")
	c.csrf = guardado
	r = c.pedir(http.MethodPost, "/api/v1/usuarios", nuevo)
	r.problema(t, 403, "prohibido")
	if !strings.Contains(r.cuerpo["detail"].(string), "usuarios.crear") {
		t.Fatalf("el detalle nombra el permiso: %s", r.crudo)
	}

	if r := c.pedir(http.MethodDelete, "/api/v1/sesion", nil); r.status != 204 {
		t.Fatalf("cerrar sesión: %d %s", r.status, r.crudo)
	}
	c.pedir(http.MethodGet, "/api/v1/yo", nil).problema(t, 401, "no_autenticado")
}

func TestValidacionDeEntrada(t *testing.T) {
	e := nuevoEntorno(t)
	_, correo := e.usuario("admin")
	c := e.cliente()
	c.entrar(correo)
	r := c.pedir(http.MethodPost, "/api/v1/usuarios", map[string]any{"nombre": "", "correo": "no-es-correo"})
	r.problema(t, 422, "validacion")
	errs, _ := r.cuerpo["errors"].([]any)
	if len(errs) == 0 {
		t.Fatalf("faltan errors[]: %s", r.crudo)
	}
	campos := []string{}
	for _, x := range errs {
		campos = append(campos, x.(map[string]any)["field"].(string))
	}
	if !strings.Contains(strings.Join(campos, ","), "correo") {
		t.Fatalf("errors[] debe nombrar correo: %v", campos)
	}
	c.pedir(http.MethodPost, "/api/v1/usuarios", map[string]any{"nombre": "Corta", "correo": pruebas.Correo("c"), "contrasena": "corta"}).problema(t, 422, "validacion")
}

func TestMFA(t *testing.T) {
	e := nuevoEntorno(t)
	_, correo := e.usuario("lectura")
	c := e.cliente()
	c.entrar(correo)

	r := c.pedir(http.MethodPost, "/api/v1/yo/mfa", nil)
	secreto, _ := r.cuerpo["secreto"].(string)
	if r.status != 200 || secreto == "" || !strings.HasPrefix(r.cuerpo["url"].(string), "otpauth://") {
		t.Fatalf("configurar: %d %s", r.status, r.crudo)
	}
	c.pedir(http.MethodPost, "/api/v1/yo/mfa/activar", map[string]any{"codigo": "000000"}).problema(t, 422, "codigo_invalido")
	ahora, _ := totp.GenerateCode(secreto, time.Now())
	if r := c.pedir(http.MethodPost, "/api/v1/yo/mfa/activar", map[string]any{"codigo": ahora}); r.status != 200 || r.cuerpo["mfaActivo"] != true {
		t.Fatalf("activar: %d %s", r.status, r.crudo)
	}
	c.pedir(http.MethodDelete, "/api/v1/sesion", nil)

	// Con MFA, la sesión queda pendiente y no sirve hasta el código.
	c2 := e.cliente()
	r = c2.entrar(correo)
	if r.status != 200 || r.cuerpo["mfaRequerido"] != true || r.cuerpo["usuario"] != nil {
		t.Fatalf("login con MFA: %d %s", r.status, r.crudo)
	}
	pendiente := c2.cookie(identidad.CookieSesion).Value
	c2.pedir(http.MethodGet, "/api/v1/yo", nil).problema(t, 401, "no_autenticado")
	c2.pedir(http.MethodPost, "/api/v1/sesion/mfa", map[string]any{"codigo": "000000"}).problema(t, 401, "codigo_invalido")
	c2.pedir(http.MethodPost, "/api/v1/sesion/mfa", map[string]any{"codigo": ahora}).problema(t, 401, "codigo_invalido") // ya usado al activar

	siguiente, _ := totp.GenerateCode(secreto, time.Now().Add(30*time.Second))
	r = c2.pedir(http.MethodPost, "/api/v1/sesion/mfa", map[string]any{"codigo": siguiente})
	if r.status != 200 || r.cuerpo["mfaRequerido"] != false || r.cuerpo["usuario"] == nil {
		t.Fatalf("verificar MFA: %d %s", r.status, r.crudo)
	}
	c2.csrf = r.cuerpo["csrfToken"].(string)
	if c2.cookie(identidad.CookieSesion).Value == pendiente {
		t.Fatal("al completar el MFA la sesión cambia de valor")
	}
	if r := c2.pedir(http.MethodGet, "/api/v1/yo", nil); r.status != 200 {
		t.Fatalf("yo tras MFA: %d %s", r.status, r.crudo)
	}

	// El mismo código no sirve dos veces.
	c3 := e.cliente()
	c3.entrar(correo)
	c3.pedir(http.MethodPost, "/api/v1/sesion/mfa", map[string]any{"codigo": siguiente}).problema(t, 401, "codigo_invalido")
}

func TestTokens(t *testing.T) {
	e := nuevoEntorno(t)
	_, correoAdmin := e.usuario("admin")
	admin := e.cliente()
	admin.entrar(correoAdmin)

	r := admin.pedir(http.MethodPost, "/api/v1/tokens", map[string]any{"nombre": "lectura", "permisos": []string{"usuarios.ver"}})
	if r.status != 201 {
		t.Fatalf("crear token: %d %s", r.status, r.crudo)
	}
	valor := r.cuerpo["valor"].(string)
	token := r.cuerpo["token"].(map[string]any)
	if !strings.HasPrefix(valor, identidad.PrefijoToken) || len(valor) != len(identidad.PrefijoToken)+43 || token["prefijo"] != valor[:13] {
		t.Fatalf("formato del token: %s %v", valor, token["prefijo"])
	}
	id := int64(token["id"].(float64))

	api := e.cliente()
	api.bearer = valor
	if r := api.pedir(http.MethodGet, "/api/v1/usuarios?limit=1", nil); r.status != 200 {
		t.Fatalf("Bearer con usuarios.ver: %d %s", r.status, r.crudo)
	}
	yo := api.pedir(http.MethodGet, "/api/v1/yo", nil)
	if yo.cuerpo["actor"] != "token" || fmt.Sprint(yo.cuerpo["permisos"]) != "[usuarios.ver]" || yo.cuerpo["tokenId"] != float64(id) {
		t.Fatalf("yo con token (el admin queda limitado a los permisos del token): %s", yo.crudo)
	}
	// Bearer no necesita CSRF, pero sí el permiso.
	api.pedir(http.MethodPost, "/api/v1/usuarios", map[string]any{"nombre": "X", "correo": pruebas.Correo("x"), "contrasena": contrasena}).problema(t, 403, "prohibido")

	malo := e.cliente()
	malo.bearer = identidad.PrefijoToken + "noexiste"
	malo.pedir(http.MethodGet, "/api/v1/yo", nil).problema(t, 401, "no_autenticado")

	lista := admin.pedir(http.MethodGet, "/api/v1/tokens", nil)
	if lista.status != 200 || strings.Contains(string(lista.crudo), valor) || strings.Contains(string(lista.crudo), "hash") {
		t.Fatalf("la lista nunca muestra el valor ni el hash: %s", lista.crudo)
	}

	ruta := fmt.Sprintf("/api/v1/tokens/%d/revocar", id)
	if r := admin.pedir(http.MethodPost, ruta, map[string]any{"motivo": "prueba"}); r.status != 200 || r.cuerpo["revocadoEn"] == nil {
		t.Fatalf("revocar: %d %s", r.status, r.crudo)
	}
	admin.pedir(http.MethodPost, ruta, map[string]any{}).problema(t, 422, "token_revocado")
	api.pedir(http.MethodGet, "/api/v1/yo", nil).problema(t, 401, "no_autenticado")

	// Token de un usuario de integración → actor integracion.
	r = admin.pedir(http.MethodPost, "/api/v1/usuarios", map[string]any{"nombre": "Argos", "correo": pruebas.Correo("argos"), "tipo": "integracion", "roles": []map[string]any{{"rol": "lectura"}}})
	if r.status != 201 {
		t.Fatalf("crear integración: %d %s", r.status, r.crudo)
	}
	integ := admin.pedir(http.MethodPost, "/api/v1/tokens", map[string]any{"nombre": "argos", "usuarioId": r.cuerpo["id"]})
	if integ.status != 201 {
		t.Fatalf("token de integración: %d %s", integ.status, integ.crudo)
	}
	argos := e.cliente()
	argos.bearer = integ.cuerpo["valor"].(string)
	if yo := argos.pedir(http.MethodGet, "/api/v1/yo", nil); yo.cuerpo["actor"] != "integracion" {
		t.Fatalf("actor de integración: %s", yo.crudo)
	}
}

func TestIdempotencia(t *testing.T) {
	e := nuevoEntorno(t)
	_, correoAdmin := e.usuario("admin")
	c := e.cliente()
	c.entrar(correoAdmin)

	correo := pruebas.Correo("idem")
	cuerpo := map[string]any{"nombre": "Idem", "correo": correo, "contrasena": contrasena}
	clave := "clave-" + pruebas.Unico()

	r1 := c.pedir(http.MethodPost, "/api/v1/usuarios", cuerpo, nucleo.CabeceraIdempotencia, clave)
	if r1.status != 201 || r1.h.Get(nucleo.CabeceraRepetida) != "" {
		t.Fatalf("primera: %d %s", r1.status, r1.crudo)
	}
	r2 := c.pedir(http.MethodPost, "/api/v1/usuarios", cuerpo, nucleo.CabeceraIdempotencia, clave)
	if r2.status != 201 || !bytes.Equal(r1.crudo, r2.crudo) || r2.h.Get(nucleo.CabeceraRepetida) != "true" {
		t.Fatalf("repetida: %d %s (repetida=%q)", r2.status, r2.crudo, r2.h.Get(nucleo.CabeceraRepetida))
	}
	lista := c.pedir(http.MethodGet, "/api/v1/usuarios?q="+url.QueryEscape(correo), nil)
	if n := len(lista.cuerpo["data"].([]any)); n != 1 {
		t.Fatalf("la repetición no crea otro usuario: %d", n)
	}

	otro := map[string]any{"nombre": "Idem 2", "correo": pruebas.Correo("idem"), "contrasena": contrasena}
	c.pedir(http.MethodPost, "/api/v1/usuarios", otro, nucleo.CabeceraIdempotencia, clave).problema(t, 422, "idempotencia_reutilizada")

	// Sin clave (o con otra) la operación corre de nuevo: aquí choca con el correo.
	c.pedir(http.MethodPost, "/api/v1/usuarios", cuerpo, nucleo.CabeceraIdempotencia, "otra-"+clave).problema(t, 409, "correo_en_uso")

	// Las claves son por actor: otro usuario puede usar la misma.
	_, correo2 := e.usuario("admin")
	c2 := e.cliente()
	c2.entrar(correo2)
	r := c2.pedir(http.MethodPost, "/api/v1/usuarios", otro, nucleo.CabeceraIdempotencia, clave)
	if r.status != 201 || r.h.Get(nucleo.CabeceraRepetida) != "" {
		t.Fatalf("otro actor, misma clave: %d %s", r.status, r.crudo)
	}
}

func TestVersionConflicto(t *testing.T) {
	e := nuevoEntorno(t)
	_, correoAdmin := e.usuario("admin")
	id, _ := e.usuario("lectura")
	c := e.cliente()
	c.entrar(correoAdmin)

	ruta := fmt.Sprintf("/api/v1/usuarios/%d/editar", id)
	r := c.pedir(http.MethodPost, ruta, map[string]any{"version": 1, "nombre": "Nuevo nombre", "roles": []map[string]any{{"rol": "soporte", "zonaId": 3}}})
	if r.status != 200 || r.cuerpo["version"] != float64(2) || r.cuerpo["nombre"] != "Nuevo nombre" {
		t.Fatalf("editar: %d %s", r.status, r.crudo)
	}
	roles := r.cuerpo["roles"].([]any)
	if len(roles) != 1 || roles[0].(map[string]any)["zonaId"] != float64(3) {
		t.Fatalf("roles con alcance: %s", r.crudo)
	}
	c.pedir(http.MethodPost, ruta, map[string]any{"version": 1, "nombre": "Otra"}).problema(t, 409, "conflicto_version")
	c.pedir(http.MethodPost, "/api/v1/usuarios/999999/editar", map[string]any{"version": 1}).problema(t, 404, "no_encontrado")
	c.pedir(http.MethodPost, ruta, map[string]any{"version": 2, "roles": []map[string]any{{"rol": "soporte", "zonaId": 3, "cuadrillaId": 4}}}).problema(t, 422, "validacion")
}

func TestDesactivar(t *testing.T) {
	e := nuevoEntorno(t)
	idAdmin, correoAdmin := e.usuario("admin")
	id, correo := e.usuario("lectura")
	admin := e.cliente()
	admin.entrar(correoAdmin)
	victima := e.cliente()
	victima.entrar(correo)

	admin.pedir(http.MethodPost, fmt.Sprintf("/api/v1/usuarios/%d/desactivar", idAdmin), map[string]any{"version": 1, "motivo": "x"}).problema(t, 422, "autodesactivacion")
	r := admin.pedir(http.MethodPost, fmt.Sprintf("/api/v1/usuarios/%d/desactivar", id), map[string]any{"version": 1, "motivo": "salió de la empresa"})
	if r.status != 200 || r.cuerpo["activo"] != false {
		t.Fatalf("desactivar: %d %s", r.status, r.crudo)
	}
	victima.pedir(http.MethodGet, "/api/v1/yo", nil).problema(t, 401, "no_autenticado")
	victima.entrar(correo).problema(t, 401, "credenciales_invalidas")
}

func TestPaginacion(t *testing.T) {
	e := nuevoEntorno(t)
	_, correoAdmin := e.usuario("admin")
	marca := "pag" + pruebas.Unico()
	for i := range 5 {
		_, err := e.mod.CrearUsuario.Ejecutar(pruebas.ContextoSistema(pruebas.Pool(t)), identidad.EntradaCrearUsuario{
			Nombre: fmt.Sprintf("%s %d", marca, i), Correo: pruebas.Correo(marca), Contrasena: contrasena,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	c := e.cliente()
	c.entrar(correoAdmin)

	vistos := []string{}
	cursor := ""
	for paginas := 0; ; paginas++ {
		ruta := "/api/v1/usuarios?limit=2&q=" + marca
		if cursor != "" {
			ruta += "&cursor=" + url.QueryEscape(cursor)
		}
		r := c.pedir(http.MethodGet, ruta, nil)
		if r.status != 200 {
			t.Fatalf("página: %d %s", r.status, r.crudo)
		}
		for _, u := range r.cuerpo["data"].([]any) {
			vistos = append(vistos, u.(map[string]any)["nombre"].(string))
		}
		siguiente, existe := r.cuerpo["nextCursor"]
		if !existe {
			t.Fatalf("nextCursor siempre está presente (null al final): %s", r.crudo)
		}
		if siguiente == nil {
			if paginas != 2 {
				t.Fatalf("esperaba 3 páginas, fueron %d", paginas+1)
			}
			break
		}
		cursor = siguiente.(string)
	}
	if len(vistos) != 5 || vistos[0] != marca+" 0" || vistos[4] != marca+" 4" {
		t.Fatalf("recorrido: %v", vistos)
	}

	r := c.pedir(http.MethodGet, "/api/v1/usuarios?cursor=basura!", nil)
	r.problema(t, 422, "validacion")
	if !strings.Contains(string(r.crudo), `"field":"cursor"`) {
		t.Fatalf("el error nombra cursor: %s", r.crudo)
	}
	r = c.pedir(http.MethodGet, "/api/v1/usuarios?limit=500", nil)
	r.problema(t, 422, "validacion")
	if !strings.Contains(string(r.crudo), `"field":"limit"`) {
		t.Fatalf("el error nombra limit: %s", r.crudo)
	}
	if r := c.pedir(http.MethodGet, "/api/v1/permisos", nil); r.status != 200 || r.cuerpo["nextCursor"] != nil || len(r.cuerpo["data"].([]any)) != len(nucleo.Catalogo) {
		t.Fatalf("permisos: %d %s", r.status, r.crudo)
	}
	// Los roles base; los propios que crean otras pruebas del paquete no cuentan.
	r = c.pedir(http.MethodGet, "/api/v1/roles", nil)
	base := 0
	for _, x := range r.cuerpo["data"].([]any) {
		if x.(map[string]any)["sistema"] == true {
			base++
		}
	}
	if r.status != 200 || base != len(nucleo.RolesBase) {
		t.Fatalf("roles: %d %s", r.status, r.crudo)
	}
}
