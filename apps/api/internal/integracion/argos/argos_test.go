package argos_test

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

	"github.com/HCDestroyer/iris/apps/api/internal/integracion"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/servidor"
	"github.com/HCDestroyer/iris/apps/api/migraciones"
)

func TestMain(m *testing.M) { os.Exit(pruebas.Correr(m)) }

const contrasena = "contrasena-de-prueba-123"

type entorno struct {
	t     *testing.T
	pool  *pgxpool.Pool
	argos *argosFalso
	mod   *argos.Modulo
	ident *identidad.Modulo
	api   *httptest.Server
	cfg   argos.Config
}

func nuevoCifrador(t *testing.T) *nucleo.Cifrador {
	clave := make([]byte, 32)
	_, _ = rand.Read(clave)
	c, err := nucleo.NuevoCifrador(base64.StdEncoding.EncodeToString(clave))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// nuevoEntorno levanta Argos falso y la API de Iris con el conector. Apaga los conectores y
// vacía las instantáneas de pruebas anteriores (la base es compartida dentro del paquete).
func nuevoEntorno(t *testing.T) *entorno {
	t.Helper()
	pool := pruebas.Pool(t)
	for _, sql := range []string{
		"UPDATE integracion.conectores SET estado = 'apagado'",
		"DELETE FROM integracion.argos_servicios", "DELETE FROM integracion.argos_clientes",
		"DELETE FROM integracion.argos_zonas", "DELETE FROM integracion.argos_routers",
		// Multiempresa: solo la empresa 1 (la que el Argos falso informa en el canje).
		"DELETE FROM nucleo.usuarios_empresas WHERE empresa_id <> 1", "DELETE FROM nucleo.empresas WHERE id <> 1",
		"INSERT INTO nucleo.empresas (id, nombre) VALUES (1, 'Empresa 1') ON CONFLICT (id) DO NOTHING",
	} {
		if _, err := pool.Exec(context.Background(), sql); err != nil {
			t.Fatal(err)
		}
	}
	fa := nuevoArgosFalso(t)
	cif := nuevoCifrador(t)
	cola, err := integracion.NuevoClienteInsercion(pool, migraciones.EsquemaRiver, nil)
	if err != nil {
		t.Fatal(err)
	}
	cfg := argos.Config{
		Cifrador: cif, BaseURL: "https://iris.prueba", PermitirHTTP: true,
		// El Argos falso escucha en loopback: en producción esa red está bloqueada (red.go).
		RedesPermitidas: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8"), netip.MustParsePrefix("::1/128")},
		Espera:          5 * time.Millisecond, Encolar: integracion.Encolar(cola), Timeout: 2 * time.Second,
	}
	mod := argos.Nuevo(cfg)
	ident := identidad.Nuevo(identidad.Config{Cifrador: cif})
	// Como cmd/iris: el canje entrega el iris_pat_ del usuario de integración (contrato §1.2).
	mod.Nucleo().FijarTokens(ident)
	h, _ := servidor.Nuevo(servidor.Deps{
		BD: pool, Identidad: ident, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Modulos: []func(huma.API){mod.Registrar},
	})
	api := httptest.NewServer(h)
	t.Cleanup(api.Close)
	return &entorno{t: t, pool: pool, argos: fa, mod: mod, ident: ident, api: api, cfg: cfg}
}

// usuario crea un usuario con esos roles, asignado a la empresa 1, y devuelve un cliente HTTP
// con su sesión.
func (e *entorno) usuario(roles ...string) *clienteHTTP {
	e.t.Helper()
	return e.usuarioEn([]int64{1}, roles...)
}

// usuarioEn es usuario con las empresas dadas.
func (e *entorno) usuarioEn(empresas []int64, roles ...string) *clienteHTTP {
	e.t.Helper()
	correo := pruebas.Correo("u")
	asign := []identidad.AsignacionRol{}
	for _, r := range roles {
		asign = append(asign, identidad.AsignacionRol{Rol: r})
	}
	ctx := pruebas.ContextoSistema(e.pool)
	u, err := e.ident.CrearUsuario.Ejecutar(ctx, identidad.EntradaCrearUsuario{
		Nombre: "Usuario " + correo, Correo: correo, Contrasena: contrasena, Roles: asign,
	})
	if err != nil {
		e.t.Fatal(err)
	}
	for _, emp := range empresas {
		if _, err := e.ident.AsignarEmpresaUsuario.Ejecutar(ctx, identidad.EntradaEmpresaUsuario{UsuarioID: u.ID, EmpresaID: emp}); err != nil {
			e.t.Fatal(err)
		}
	}
	c := e.anonimo()
	r := c.pedir(http.MethodPost, "/api/v1/sesion", map[string]any{"correo": correo, "contrasena": contrasena})
	if r.status != 200 {
		e.t.Fatalf("login: %d %s", r.status, r.crudo)
	}
	c.csrf, _ = r.cuerpo["csrfToken"].(string)
	return c
}

func (e *entorno) anonimo() *clienteHTTP {
	jar, _ := cookiejar.New(nil)
	return &clienteHTTP{t: e.t, base: e.api.URL, http: &http.Client{Jar: jar}}
}

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

// conectado: un admin conecta Iris con el enlace de Argos y lo activa. Devuelve el admin, el
// id del conector y el secreto del webhook que Iris entregó a Argos.
func (e *entorno) conectado() (*clienteHTTP, int64, string) {
	e.t.Helper()
	admin := e.usuario("admin")
	r := admin.pedir(http.MethodPost, "/api/v1/integraciones/argosisp/conectar", map[string]any{"enlace": e.argos.enlace()})
	if r.status != 201 {
		e.t.Fatalf("conectar: %d %s", r.status, r.crudo)
	}
	id := int64(r.cuerpo["id"].(float64))
	if a := admin.pedir(http.MethodPost, fmt.Sprintf("/api/v1/integraciones/conectores/%d/activar", id), map[string]any{"version": 1}); a.status != 200 {
		e.t.Fatalf("activar: %d %s", a.status, a.crudo)
	}
	var secreto string
	e.argos.con(func() { secreto = secretoDelCanje(e.argos.canjes[len(e.argos.canjes)-1]) })
	return admin, id, secreto
}

func (e *entorno) consultarTexto(sql string, args ...any) string {
	e.t.Helper()
	var s string
	if err := e.pool.QueryRow(context.Background(), sql, args...).Scan(&s); err != nil {
		e.t.Fatal(err)
	}
	return s
}

func (e *entorno) contar(sql string, args ...any) int {
	e.t.Helper()
	var n int
	if err := e.pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

// sinSecretos falla si el token o el secreto aparecen en claro en la auditoría o el outbox.
func (e *entorno) sinSecretos(secretos ...string) {
	e.t.Helper()
	todo := e.consultarTexto(`SELECT coalesce(string_agg(entrada::text || ' ' || coalesce(error::text, ''), ' '), '') FROM auditoria.acciones`) +
		e.consultarTexto(`SELECT coalesce(string_agg(coalesce(antes::text, '') || ' ' || coalesce(despues::text, ''), ' '), '') FROM auditoria.cambios`) +
		e.consultarTexto(`SELECT coalesce(string_agg(payload::text, ' '), '') FROM integracion.outbox`) +
		e.consultarTexto(`SELECT coalesce(string_agg(encode(token_cifrado, 'escape') || encode(secreto_webhook_cifrado, 'escape') || base_url, ' '), '') FROM integracion.conectores`)
	for _, s := range secretos {
		if s != "" && strings.Contains(todo, s) {
			e.t.Fatalf("el secreto %q quedó en claro en la base", s[:8])
		}
	}
}

// Enlace de conexión -----------------------------------------------------------------------

func TestConectarConEnlaceDeArgos(t *testing.T) {
	e := nuevoEntorno(t)
	admin := e.usuario("admin")
	lectura := e.usuario("lectura")

	lectura.pedir(http.MethodPost, "/api/v1/integraciones/argosisp/conectar", map[string]any{"enlace": e.argos.enlace()}).problema(t, 403, "prohibido")
	admin.pedir(http.MethodPost, "/api/v1/integraciones/argosisp/conectar", map[string]any{"enlace": "https://argos.prueba/otra-cosa"}).problema(t, 422, "validacion")

	r := admin.pedir(http.MethodPost, "/api/v1/integraciones/argosisp/conectar", map[string]any{"enlace": e.argos.enlace()})
	if r.status != 201 || r.cuerpo["estado"] != "apagado" || r.cuerpo["origen"] != "enlace_remoto" || r.cuerpo["remotoId"] != "77" {
		t.Fatalf("conectar: %d %s", r.status, r.crudo)
	}
	id := int64(r.cuerpo["id"].(float64))
	if strings.Contains(string(r.crudo), tokenArgos) || strings.Contains(string(r.crudo), "secreto") {
		t.Fatalf("la respuesta expone el token o el secreto: %s", r.crudo)
	}
	if r.cuerpo["webhookUrl"] != fmt.Sprintf("https://iris.prueba/api/v1/integraciones/argosisp/webhook/%d", id) {
		t.Fatalf("webhookUrl: %v", r.cuerpo["webhookUrl"])
	}
	var canje map[string]any
	e.argos.con(func() { canje = e.argos.canjes[len(e.argos.canjes)-1] })
	// Cuerpo del contrato (Argos ↔ Horus §1.4, como valida Argos 1.3.0): base_url, el iris_pat_
	// que Iris emite para Argos (§1.2) y su receptor con el secreto.
	secreto := secretoDelCanje(canje)
	wh, _ := canje["webhook"].(map[string]any)
	tokenIris, _ := canje["token"].(string)
	if canje["sistema"] != "iris" || canje["base_url"] != "https://iris.prueba" || wh["url"] != r.cuerpo["webhookUrl"] || len(secreto) < 40 ||
		!strings.HasPrefix(tokenIris, "iris_pat_") {
		t.Fatalf("canje enviado a Argos: %v", canje)
	}
	// El token es del usuario de integración de Argos (rol integracion_argos), vigente.
	if n := e.contar(`SELECT count(*) FROM nucleo.tokens_api t JOIN nucleo.usuarios u ON u.id = t.usuario_id
		JOIN nucleo.usuarios_roles ur ON ur.usuario_id = u.id JOIN nucleo.roles r ON r.id = ur.rol_id
		WHERE t.hash = $1 AND t.revocado_en IS NULL AND u.tipo = 'integracion' AND r.codigo = 'integracion_argos'`, identidad.Hash256(tokenIris)); n != 1 {
		t.Fatalf("el iris_pat_ del canje no es del usuario de integración de Argos: %d", n)
	}

	// El enlace es de un solo uso: Argos rechaza el segundo canje, y el iris_pat_ que Iris emitió
	// para ese canje queda revocado.
	admin.pedir(http.MethodPost, "/api/v1/integraciones/argosisp/conectar", map[string]any{"enlace": strings.Replace(e.argos.enlace(), "usado-", "", 1)}).problema(t, 502, "argos_rechazo")
	var rechazado string
	e.argos.con(func() { rechazado, _ = e.argos.canjes[len(e.argos.canjes)-1]["token"].(string) })
	if rechazado == "" || rechazado == tokenIris ||
		e.contar(`SELECT count(*) FROM nucleo.tokens_api WHERE hash = $1 AND revocado_en IS NOT NULL`, identidad.Hash256(rechazado)) != 1 {
		t.Fatal("el token de un canje rechazado debe quedar revocado")
	}
	e.sinSecretos(tokenIris)

	// Token y secreto cifrados: nunca en claro en la base, la auditoría ni el outbox; el
	// código del enlace tampoco queda en la auditoría.
	e.sinSecretos(tokenArgos, secreto, "codigo-de-argos-0123456789")
	if n := e.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'integraciones.conectar' AND entrada->>'enlace' = '***'`); n < 1 {
		t.Fatal("el enlace debía auditarse enmascarado")
	}
	if n := e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'conector.creado' AND payload->>'conector_id' = $1`, strconv.FormatInt(id, 10)); n != 1 {
		t.Fatalf("conector.creado: %d", n)
	}

	// Lista sin secretos, para quien tiene integraciones.ver.
	l := admin.pedir(http.MethodGet, "/api/v1/integraciones/conectores", nil)
	if l.status != 200 || strings.Contains(string(l.crudo), tokenArgos) {
		t.Fatalf("listar: %d %s", l.status, l.crudo)
	}
	lectura.pedir(http.MethodGet, "/api/v1/integraciones/conectores", nil).problema(t, 403, "prohibido")

	// Activar exige la versión y el permiso; luego no se activa otro del mismo sistema.
	ruta := fmt.Sprintf("/api/v1/integraciones/conectores/%d/activar", id)
	lectura.pedir(http.MethodPost, ruta, map[string]any{"version": 1}).problema(t, 403, "prohibido")
	admin.pedir(http.MethodPost, ruta, map[string]any{"version": 9}).problema(t, 409, "conflicto_version")
	// El periódico de la carga corre al arrancar el worker (con el conector apagado no hace nada):
	// uno del mismo minuto no puede tapar la carga de la activación (e2e con Argos 1.3.0).
	cola, err := integracion.NuevoClienteInsercion(e.pool, migraciones.EsquemaRiver, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cola.Insert(context.Background(), argos.ArgsSincronizarCatalogos{}, nil); err != nil {
		t.Fatal(err)
	}
	if a := admin.pedir(http.MethodPost, ruta, map[string]any{"version": 1}); a.status != 200 || a.cuerpo["estado"] != "activo" {
		t.Fatalf("activar: %d %s", a.status, a.crudo)
	}
	if n := e.contar(`SELECT count(*) FROM river.river_job WHERE kind = 'integracion.argos_sincronizar_catalogos'
		AND args->>'motivo' = 'activacion' AND created_at > now() - interval '1 minute'`); n != 1 {
		t.Fatalf("activar debía encolar la carga de zonas y routers aunque el periódico corriera en el mismo minuto: %d", n)
	}
	des := fmt.Sprintf("/api/v1/integraciones/conectores/%d/desactivar", id)
	if d := admin.pedir(http.MethodPost, des, map[string]any{"version": 2, "motivo": "prueba"}); d.status != 200 || d.cuerpo["estado"] != "apagado" {
		t.Fatalf("desactivar: %d %s", d.status, d.crudo)
	}
	admin.pedir(http.MethodPost, des, map[string]any{"version": 3}).problema(t, 422, "conector_inactivo")
}

func TestConectarExigeHTTPSYClave(t *testing.T) {
	e := nuevoEntorno(t)
	ctx := pruebas.ContextoUsuario(e.pool, 0, nucleo.PermIntegracionesConectar)
	estricto := e.cfg
	estricto.PermitirHTTP = false
	if _, err := argos.Nuevo(estricto).Conectar.Ejecutar(ctx, argos.EntradaConectar{Enlace: e.argos.enlace()}); !esProblema(err, "validacion") {
		t.Fatalf("http:// sin PermitirHTTP debía ser 422: %v", err)
	}
	sinClave := e.cfg
	sinClave.Cifrador = nil
	if _, err := argos.Nuevo(sinClave).Conectar.Ejecutar(ctx, argos.EntradaConectar{Enlace: e.argos.enlace()}); !esProblema(err, "cifrado_no_disponible") {
		t.Fatalf("sin IRIS_CLAVE_CIFRADO debía ser 422: %v", err)
	}
}

func esProblema(err error, codigo string) bool {
	p, _ := nucleo.AProblema(err)
	return p != nil && p.Codigo() == codigo
}

func TestCanjeDesdeArgos(t *testing.T) {
	e := nuevoEntorno(t)
	admin := e.usuario("admin")
	r := admin.pedir(http.MethodPost, "/api/v1/integraciones/argosisp/conexion/enlaces", nil, "Idempotency-Key", "enlace-1")
	if r.status != 201 {
		t.Fatalf("generar enlace: %d %s", r.status, r.crudo)
	}
	enlace := r.cuerpo["enlace"].(string)
	base, codigo, ok := strings.Cut(enlace, "/conectar#")
	if !ok || base != "https://iris.prueba" || len(codigo) < 40 {
		t.Fatalf("enlace: %s", enlace)
	}
	// La respuesta lleva un secreto: no se guarda para Idempotency-Key.
	admin.pedir(http.MethodPost, "/api/v1/integraciones/argosisp/conexion/enlaces", nil, "Idempotency-Key", "enlace-1").problema(t, 409, "idempotencia_no_repetible")

	anon := e.anonimo()
	canje := map[string]any{"codigo": codigo, "sistema": "argosisp", "url": e.argos.srv.URL, "token": tokenArgos, "conexionId": "c-9", "empresas": []int{1, 2}}
	anon.pedir(http.MethodPost, "/api/v1/integraciones/argosisp/conexion/canjear", map[string]any{
		"codigo": codigo, "sistema": "argosisp", "url": e.argos.srv.URL, "token": "sin-prefijo-1234",
	}).problema(t, 422, "validacion")
	c := anon.pedir(http.MethodPost, "/api/v1/integraciones/argosisp/conexion/canjear", canje)
	if c.status != 200 || c.cuerpo["sistema"] != "iris" || c.cuerpo["estado"] != "apagado" {
		t.Fatalf("canjear: %d %s", c.status, c.crudo)
	}
	secreto := c.cuerpo["webhookSecreto"].(string)
	id := int64(c.cuerpo["conectorId"].(float64))
	if c.cuerpo["webhookUrl"] != fmt.Sprintf("https://iris.prueba/api/v1/integraciones/argosisp/webhook/%d", id) || len(secreto) < 40 {
		t.Fatalf("canje: %s", c.crudo)
	}
	// Un solo uso.
	anon.pedir(http.MethodPost, "/api/v1/integraciones/argosisp/conexion/canjear", canje).problema(t, 401, "codigo_invalido")
	// Vencido.
	r = admin.pedir(http.MethodPost, "/api/v1/integraciones/argosisp/conexion/enlaces", nil)
	_, viejo, _ := strings.Cut(r.cuerpo["enlace"].(string), "#")
	if _, err := e.pool.Exec(context.Background(), `UPDATE integracion.codigos_conexion SET expira_en = now() - interval '1 minute' WHERE usado_en IS NULL`); err != nil {
		t.Fatal(err)
	}
	canje["codigo"] = viejo
	anon.pedir(http.MethodPost, "/api/v1/integraciones/argosisp/conexion/canjear", canje).problema(t, 401, "codigo_invalido")

	e.sinSecretos(tokenArgos, secreto, codigo)
	if n := e.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'integraciones.canjear' AND resultado = 'ok' AND actor_tipo = 'integracion' AND entrada->>'codigo' = '***' AND entrada->>'token' = '***'`); n != 1 {
		t.Fatalf("auditoría del canje: %d", n)
	}

	// El token canjeado sirve: al activar, Iris lee de Argos con él.
	if a := admin.pedir(http.MethodPost, fmt.Sprintf("/api/v1/integraciones/conectores/%d/activar", id), map[string]any{"version": 1}); a.status != 200 {
		t.Fatalf("activar: %d %s", a.status, a.crudo)
	}
	e.argos.con(func() { e.argos.clientes[5] = map[string]any{"id": 5, "empresaId": 1, "nombre": "Ana Canje"} })
	// El canje registró las empresas que cubre el token: el admin (todas) ahora elige.
	if n := e.contar(`SELECT count(*) FROM nucleo.empresas WHERE id = 2 AND origen = 'argos' AND sincronizado_en IS NOT NULL`); n != 1 {
		t.Fatalf("empresas del canje: %d", n)
	}
	admin.pedir(http.MethodGet, "/api/v1/clientes?q=ana", nil).problema(t, 422, "empresa_requerida")
	if b := admin.pedir(http.MethodGet, "/api/v1/clientes?q=ana", nil, "X-Iris-Empresa", "1"); b.status != 200 || !strings.Contains(string(b.crudo), "Ana Canje") {
		t.Fatalf("buscar con el token canjeado: %d %s", b.status, b.crudo)
	}
}

// Lecturas -------------------------------------------------------------------------------------

func (e *entorno) sembrarCliente(id int64, nombre string) {
	e.argos.con(func() {
		e.argos.clientes[id] = map[string]any{
			"id": id, "empresaId": 1, "codigo": "C-" + strconv.FormatInt(id, 10), "nombre": nombre,
			"telefono": "5555-0000", "direccion": "Zona 1", "latitud": "14.6349", "longitud": -90.5069,
			"zona": map[string]any{"id": 3, "nombre": "Centro"}, "estado": "activo",
		}
		e.argos.servicios[id] = []map[string]any{{
			"id": id * 10, "empresa_id": 1, "cliente_id": id, "codigo": "S-1",
			"plan": map[string]any{"id": 8, "nombre": "Fibra 100"}, "router_id": 4, "router_nombre": "RT-Centro",
			"zona_id": 3, "zona_nombre": "Centro", "ip": "10.0.0." + strconv.FormatInt(id, 10), "estado": "activo",
		}}
		e.argos.estados[id] = map[string]any{"saldo": "150.50", "moneda": "GTQ", "facturas_vencidas": []any{map[string]any{"id": 1}, map[string]any{"id": 2}}}
	})
}

func TestPermisosYConectorApagado(t *testing.T) {
	e := nuevoEntorno(t)
	sinRoles := e.usuario()
	lectura := e.usuario("lectura")
	sinRoles.pedir(http.MethodGet, "/api/v1/clientes?q=ana", nil).problema(t, 403, "prohibido")
	sinRoles.pedir(http.MethodGet, "/api/v1/clientes/1", nil).problema(t, 403, "prohibido")
	e.anonimo().pedir(http.MethodGet, "/api/v1/clientes/1", nil).problema(t, 401, "no_autenticado")
	// Sin conector activo: problema claro para que el panel trabaje con datos locales.
	lectura.pedir(http.MethodGet, "/api/v1/clientes?q=ana", nil).problema(t, 503, "conector_apagado")
	lectura.pedir(http.MethodGet, "/api/v1/clientes/1", nil).problema(t, 503, "conector_apagado")
	lectura.pedir(http.MethodGet, "/api/v1/clientes/resolver-ip?ip=10.0.0.1", nil).problema(t, 503, "conector_apagado")
}

func TestFichaInstantaneaYStale(t *testing.T) {
	e := nuevoEntorno(t)
	_, _, _ = e.conectado()
	soporte := e.usuario("soporte")
	lectura := e.usuario("lectura")
	e.sembrarCliente(41, "Ana López")

	// Búsqueda en vivo (no guarda instantánea).
	b := lectura.pedir(http.MethodGet, "/api/v1/clientes?q=ana%20l", nil)
	if b.status != 200 || b.cuerpo["stale"] != false || len(b.cuerpo["data"].([]any)) != 1 {
		t.Fatalf("buscar: %d %s", b.status, b.crudo)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.argos_clientes`); n != 0 {
		t.Fatal("la búsqueda no debía guardar instantáneas")
	}

	// Ficha: lee de Argos y guarda la instantánea.
	f := soporte.pedir(http.MethodGet, "/api/v1/clientes/41", nil)
	if f.status != 200 || f.cuerpo["stale"] != false {
		t.Fatalf("ficha: %d %s", f.status, f.crudo)
	}
	cli := f.cuerpo["cliente"].(map[string]any)
	srv := f.cuerpo["servicios"].([]any)[0].(map[string]any)
	ec := f.cuerpo["estadoCuenta"].(map[string]any)
	if cli["nombre"] != "Ana López" || cli["empresaId"] != float64(1) || cli["zonaId"] != float64(3) || cli["lat"] != 14.6349 ||
		srv["planNombre"] != "Fibra 100" || srv["planId"] != float64(8) || srv["routerNombre"] != "RT-Centro" ||
		ec["saldo"] != "150.50" || ec["facturasVencidas"] != float64(2) {
		t.Fatalf("ficha mal leída: %s", f.crudo)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.argos_servicios WHERE cliente_id = 41 AND empresa_id = 1`); n != 1 {
		t.Fatal("la instantánea del servicio no quedó guardada")
	}
	// Lectura auditada (datos personales), con empresa y entidad.
	if n := e.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'clientes.ver' AND resultado = 'ok' AND entidad = 'cliente_argos' AND entidad_id = '41' AND empresa_id = 1`); n != 1 {
		t.Fatalf("auditoría de la ficha: %d", n)
	}
	if n := e.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'clientes.buscar' AND resultado = 'ok' AND entrada->>'q' = 'ana l'`); n != 1 {
		t.Fatalf("auditoría de la búsqueda: %d", n)
	}
	// Sin clientes.estado_cuenta no hay estado de cuenta.
	fl := lectura.pedir(http.MethodGet, "/api/v1/clientes/41", nil)
	if fl.status != 200 || fl.cuerpo["estadoCuenta"] != nil || fl.cuerpo["estadoCuentaError"] != "sin_permiso" {
		t.Fatalf("ficha sin permiso de estado de cuenta: %s", fl.crudo)
	}

	// Instantánea fresca: no llama a Argos.
	antes := e.argos.llamadasA("/api/v1/clientes/41")
	lectura.pedir(http.MethodGet, "/api/v1/clientes/41", nil)
	if e.argos.llamadasA("/api/v1/clientes/41") != antes {
		t.Fatal("con la instantánea fresca no debía llamar a Argos")
	}

	// Vieja (> 24 h) y Argos caído: devuelve la última instantánea marcada stale.
	if _, err := e.pool.Exec(context.Background(), `UPDATE integracion.argos_clientes SET sincronizado_en = now() - interval '25 hours'`); err != nil {
		t.Fatal(err)
	}
	e.argos.con(func() { e.argos.caido = true })
	s := soporte.pedir(http.MethodGet, "/api/v1/clientes/41", nil)
	if s.status != 200 || s.cuerpo["stale"] != true || s.cuerpo["cliente"].(map[string]any)["nombre"] != "Ana López" ||
		s.cuerpo["estadoCuentaError"] != "argos_no_disponible" || s.cuerpo["estadoCuenta"] != nil {
		t.Fatalf("stale: %d %s", s.status, s.crudo)
	}
	// La búsqueda cae a la instantánea local.
	bl := lectura.pedir(http.MethodGet, "/api/v1/clientes?q=L%C3%B3pez", nil)
	if bl.status != 200 || bl.cuerpo["stale"] != true || len(bl.cuerpo["data"].([]any)) != 1 {
		t.Fatalf("búsqueda local: %d %s", bl.status, bl.crudo)
	}
	// Sin instantánea y sin Argos: 503, nunca una ficha vacía.
	lectura.pedir(http.MethodGet, "/api/v1/clientes/99", nil).problema(t, 503, "argos_no_disponible")

	// Argos vuelve: se refresca y deja de ser stale.
	e.argos.con(func() {
		e.argos.caido = false
		e.argos.clientes[41]["nombre"] = "Ana López de Pérez"
	})
	n := lectura.pedir(http.MethodGet, "/api/v1/clientes/41", nil)
	if n.status != 200 || n.cuerpo["stale"] != false || n.cuerpo["cliente"].(map[string]any)["nombre"] != "Ana López de Pérez" {
		t.Fatalf("refresco: %s", n.crudo)
	}

	// Argos ya no lo tiene: 404 y se borra la instantánea.
	if _, err := e.pool.Exec(context.Background(), `UPDATE integracion.argos_clientes SET sincronizado_en = now() - interval '25 hours'`); err != nil {
		t.Fatal(err)
	}
	e.argos.con(func() { delete(e.argos.clientes, 41) })
	lectura.pedir(http.MethodGet, "/api/v1/clientes/41", nil).problema(t, 404, "no_encontrado")
	if n := e.contar(`SELECT count(*) FROM integracion.argos_clientes WHERE id = 41`); n != 0 {
		t.Fatal("la instantánea de un cliente borrado en Argos debía eliminarse")
	}

	// Token rechazado por Argos: 502 claro.
	e.sembrarCliente(42, "Beto")
	e.argos.con(func() { e.argos.sinPermiso = true })
	lectura.pedir(http.MethodGet, "/api/v1/clientes/42", nil).problema(t, 502, "argos_sin_permiso")
}

func TestReintentosDelCliente(t *testing.T) {
	e := nuevoEntorno(t)
	_, _, _ = e.conectado()
	lectura := e.usuario("lectura")
	e.sembrarCliente(51, "Carla")
	e.argos.con(func() { e.argos.fallarUna = 1 })
	if r := lectura.pedir(http.MethodGet, "/api/v1/clientes?q=carla", nil); r.status != 200 || r.cuerpo["stale"] != false {
		t.Fatalf("debía reintentar tras un 503: %d %s", r.status, r.crudo)
	}
	if n := e.argos.llamadasA("/api/v1/clientes"); n != 2 {
		t.Fatalf("llamadas: %d (esperaba 2)", n)
	}
}

func TestResolverIP(t *testing.T) {
	e := nuevoEntorno(t)
	_, _, _ = e.conectado()
	lectura := e.usuario("lectura")
	e.argos.con(func() { e.argos.ips["10.0.0.7"] = map[string]any{"cliente_id": 7, "servicio_id": 70, "empresa_id": 1} })

	r := lectura.pedir(http.MethodGet, "/api/v1/clientes/resolver-ip?ip=10.0.0.7", nil)
	if r.status != 200 || r.cuerpo["clienteId"] != float64(7) || r.cuerpo["servicioId"] != float64(70) || r.cuerpo["fuente"] != "argos" {
		t.Fatalf("A8: %d %s", r.status, r.crudo)
	}
	// Sin A8 en Argos: respaldo GET /ip/{ip}.
	e.argos.con(func() { e.argos.sinA8 = true })
	r = lectura.pedir(http.MethodGet, "/api/v1/clientes/resolver-ip?ip=10.0.0.7", nil)
	if r.status != 200 || r.cuerpo["clienteId"] != float64(7) || r.cuerpo["servicioId"] != float64(70) || r.cuerpo["empresaId"] != float64(1) {
		t.Fatalf("respaldo: %d %s", r.status, r.crudo)
	}
	if e.argos.llamadasA("/api/v1/ip/10.0.0.7") != 1 {
		t.Fatal("debía usar GET /ip/{ip}")
	}
	lectura.pedir(http.MethodGet, "/api/v1/clientes/resolver-ip?ip=10.9.9.9", nil).problema(t, 404, "no_encontrado")
	lectura.pedir(http.MethodGet, "/api/v1/clientes/resolver-ip?ip=no-es-ip", nil).problema(t, 422, "validacion")

	// Argos caído: usa la instantánea de servicios.
	e.sembrarCliente(8, "Dora")
	if f := lectura.pedir(http.MethodGet, "/api/v1/clientes/8", nil); f.status != 200 {
		t.Fatalf("ficha: %s", f.crudo)
	}
	e.argos.con(func() { e.argos.caido = true })
	r = lectura.pedir(http.MethodGet, "/api/v1/clientes/resolver-ip?ip=10.0.0.8", nil)
	if r.status != 200 || r.cuerpo["clienteId"] != float64(8) || r.cuerpo["stale"] != true || r.cuerpo["fuente"] != "instantanea" {
		t.Fatalf("stale: %d %s", r.status, r.crudo)
	}
	if n := e.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'clientes.resolver_ip' AND resultado = 'ok'`); n < 3 {
		t.Fatalf("auditoría de resolver IP: %d", n)
	}
}

func TestSincronizarCatalogos(t *testing.T) {
	e := nuevoEntorno(t)
	_, _, _ = e.conectado()
	e.argos.con(func() {
		for i := 1; i <= 3; i++ {
			e.argos.zonas = append(e.argos.zonas, map[string]any{"id": i, "empresaId": 1, "nombre": fmt.Sprintf("Zona %d", i)})
		}
		e.argos.routers = []map[string]any{{"id": 9, "empresa_id": 1, "nombre": "RT-9", "zona_id": 2, "lat": "14.1", "lon": "-90.2"}}
	})
	ctx := pruebas.ContextoSistema(e.pool)
	r, err := e.mod.SincronizarCatalogos.Ejecutar(ctx, struct{}{})
	if err != nil || r.Zonas != 3 || r.Routers != 1 {
		t.Fatalf("sincronizar: %+v %v", r, err)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.argos_routers WHERE id = 9 AND lat = 14.1 AND zona_id = 2`); n != 1 {
		t.Fatal("router mal guardado")
	}
	// Lo que Argos ya no devuelve se borra.
	e.argos.con(func() { e.argos.zonas = e.argos.zonas[:1] })
	if _, err := e.mod.SincronizarCatalogos.Ejecutar(ctx, struct{}{}); err != nil {
		t.Fatal(err)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.argos_zonas`); n != 1 {
		t.Fatalf("zonas: %d", n)
	}
	// Argos caído: error (River reintenta) y la instantánea queda como estaba.
	e.argos.con(func() { e.argos.caido = true })
	if _, err := e.mod.SincronizarCatalogos.Ejecutar(ctx, struct{}{}); err == nil {
		t.Fatal("debía fallar con Argos caído")
	}
	if n := e.contar(`SELECT count(*) FROM integracion.argos_zonas`); n != 1 {
		t.Fatal("un fallo no debía borrar la instantánea")
	}
}

// secretoDelCanje es webhook.secreto del cuerpo que Iris mandó al canjear.
func secretoDelCanje(canje map[string]any) string {
	wh, _ := canje["webhook"].(map[string]any)
	secreto, _ := wh["secreto"].(string)
	return secreto
}
