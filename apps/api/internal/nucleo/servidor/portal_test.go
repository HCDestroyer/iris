package servidor_test

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/servidor"
	"github.com/HCDestroyer/iris/apps/api/internal/soporte"
)

// Portal de soporte: el cliente de Argos entra con un ticket de un solo uso y queda como actor
// cliente, aislado del panel y de las otras empresas.

// sondaCliente es un caso de uso del portal (como los que escribirá I2) que devuelve la empresa
// y el canal con que corrió.
var sondaCliente = auditoria.Nuevo(auditoria.Caso[struct{}, string]{
	Accion: "sonda.portal", Acceso: auditoria.Cliente,
	AuditarExito: func(string) bool { return false },
	Logica: func(ctx context.Context, _ *auditoria.Tx, _ struct{}) (string, error) {
		a := contexto.ActorDe(ctx)
		return string(contexto.CanalDe(ctx)) + ":" + itoa(contexto.EmpresaDe(ctx)) + ":" + itoa(a.ClienteID), nil
	},
})

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

type salidaTexto struct {
	Body struct {
		Valor string `json:"valor"`
	}
}

// entornoPortal: empresas 1 y 2 registradas, soporte montado (para probar que el cliente no
// llega a /tickets) y sondas en /api/v1/portal/prueba/…: una del portal (Acceso Cliente) y una
// operativa (ConPermiso).
func entornoPortal(t *testing.T, cfg identidad.Config) *entorno {
	t.Helper()
	pool := pruebas.Pool(t)
	limpiar := func() {
		for _, sql := range []string{
			"DELETE FROM nucleo.portal_sesiones", "DELETE FROM nucleo.portal_accesos",
			"DELETE FROM integracion.argos_clientes WHERE id = 88",
			"DELETE FROM nucleo.usuarios_empresas", "DELETE FROM nucleo.empresas",
		} {
			if _, err := pool.Exec(context.Background(), sql); err != nil {
				t.Fatal(err)
			}
		}
	}
	limpiar()
	t.Cleanup(limpiar) // las demás pruebas del paquete corren en modo sin conector
	clave := make([]byte, 32)
	_, _ = rand.Read(clave)
	cif, err := nucleo.NuevoCifrador(base64.StdEncoding.EncodeToString(clave))
	if err != nil {
		t.Fatal(err)
	}
	cfg.Cifrador = cif
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://iris.prueba"
	}
	mod := identidad.Nuevo(cfg)
	ctx := pruebas.ContextoSistema(pool)
	for id, nombre := range map[int64]string{1: "Uno", 2: "Dos"} {
		if _, err := mod.CrearEmpresa.Ejecutar(ctx, identidad.EntradaCrearEmpresa{ID: id, Nombre: nombre}); err != nil {
			t.Fatal(err)
		}
	}
	sondaRuta := func(id, ruta string, fn func(context.Context) (string, error)) func(huma.API) {
		return func(api huma.API) {
			huma.Register(api, huma.Operation{OperationID: id, Method: http.MethodGet, Path: ruta},
				func(ctx context.Context, _ *struct{}) (*salidaTexto, error) {
					v, err := fn(ctx)
					if err != nil {
						return nil, err
					}
					s := &salidaTexto{}
					s.Body.Valor = v
					return s, nil
				})
		}
	}
	h, _ := servidor.Nuevo(servidor.Deps{
		BD: pool, Identidad: mod, Version: "prueba", Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Modulos: []func(huma.API){
			soporte.Nuevo(soporte.Config{}).Registrar,
			sondaRuta("sondaPortal", "/api/v1/portal/prueba/cliente", func(ctx context.Context) (string, error) {
				return sondaCliente.Ejecutar(ctx, struct{}{})
			}),
			sondaRuta("sondaPanelEnPortal", "/api/v1/portal/prueba/panel", func(ctx context.Context) (string, error) {
				id, err := sonda.Ejecutar(ctx, struct{}{})
				return itoa(id), err
			}),
		},
	})
	e := &entorno{t: t, srv: httptest.NewServer(h), mod: mod}
	t.Cleanup(e.srv.Close)
	return e
}

// argos crea un usuario de integración «Argos» (rol integracion_argos, empresa 1) y un cliente
// HTTP con un token suyo limitado a los permisos dados.
func (e *entorno) argos(permisos ...nucleo.Permiso) *cliente {
	e.t.Helper()
	ctx := pruebas.ContextoSistema(pruebas.Pool(e.t))
	u, err := e.mod.CrearUsuario.Ejecutar(ctx, identidad.EntradaCrearUsuario{
		Nombre: "Argos", Correo: pruebas.Correo("argos"), Tipo: "integracion",
		Roles: []identidad.AsignacionRol{{Rol: "integracion_argos"}}, Empresas: []int64{1},
	})
	if err != nil {
		e.t.Fatal(err)
	}
	tk, err := e.mod.CrearToken.Ejecutar(ctx, identidad.EntradaCrearToken{UsuarioID: u.ID, Nombre: "portal", Permisos: permisos})
	if err != nil {
		e.t.Fatal(err)
	}
	c := e.cliente()
	c.bearer = tk.Valor
	return c
}

// emitir pide un acceso al portal y devuelve el ticket (lo que sigue a #).
func emitir(t *testing.T, argos *cliente, clienteID, empresaID int64) string {
	t.Helper()
	r := argos.pedir(http.MethodPost, "/api/v1/portal/tickets-acceso", map[string]any{"clienteId": clienteID, "empresaId": empresaID, "nombre": "Ana Pérez"})
	if r.status != http.StatusCreated {
		t.Fatalf("emitir: %d %s", r.status, r.crudo)
	}
	url, _ := r.cuerpo["url"].(string)
	base, ticket, ok := strings.Cut(url, "#")
	if !ok || base != "https://iris.prueba/soporte/entrar" || len(ticket) < 40 {
		t.Fatalf("url de acceso inesperada: %q", url)
	}
	return ticket
}

// entrarPortal canjea el ticket con un navegador nuevo; guarda el CSRF del portal.
func (e *entorno) entrarPortal(ticket string) (*cliente, respuesta) {
	e.t.Helper()
	c := e.cliente()
	r := c.pedir(http.MethodPost, "/api/v1/portal/sesion", map[string]any{"ticket": ticket})
	if r.status == http.StatusOK {
		c.csrf, _ = r.cuerpo["csrfToken"].(string)
	}
	return c, r
}

func TestPortalEmitirYEntrar(t *testing.T) {
	e := entornoPortal(t, identidad.Config{CookieSegura: false})
	argos := e.argos(nucleo.PermPortalEmitir)

	// Emitir: con permiso, solo para sus empresas; sin permiso, 403.
	ticket := emitir(t, argos, 77, 1)
	argos.pedir(http.MethodPost, "/api/v1/portal/tickets-acceso", map[string]any{"clienteId": 77, "empresaId": 2}).problema(t, 403, "empresa_no_permitida")
	argos.pedir(http.MethodPost, "/api/v1/portal/tickets-acceso", map[string]any{"clienteId": 77, "empresaId": 1}, identidad.CabeceraEmpresa, "2").
		problema(t, 403, "empresa_no_permitida")
	e.argos(nucleo.PermTicketsVer).pedir(http.MethodPost, "/api/v1/portal/tickets-acceso", map[string]any{"clienteId": 77, "empresaId": 1}).
		problema(t, 403, "prohibido")
	e.cliente().pedir(http.MethodPost, "/api/v1/portal/tickets-acceso", map[string]any{"clienteId": 77, "empresaId": 1}).
		problema(t, 401, "no_autenticado")
	// Si Iris ya conoce al cliente (instantánea de Argos) y es de otra empresa, 422.
	if _, err := pruebas.Pool(t).Exec(context.Background(),
		`INSERT INTO integracion.argos_clientes (id, empresa_id, nombre, sincronizado_en) VALUES (88, 2, 'Otro', now())`); err != nil {
		t.Fatal(err)
	}
	argos.pedir(http.MethodPost, "/api/v1/portal/tickets-acceso", map[string]any{"clienteId": 88, "empresaId": 1}).problema(t, 422, "empresa_no_coincide")
	// El ticket no se guarda en claro.
	var enClaro int
	if err := pruebas.Pool(t).QueryRow(context.Background(),
		`SELECT count(*) FROM nucleo.portal_accesos WHERE encode(ticket_hash, 'escape') LIKE '%' || $1 || '%'`, ticket).Scan(&enClaro); err != nil || enClaro != 0 {
		t.Fatalf("ticket en claro: %d %v", enClaro, err)
	}

	// Canje: cookie propia HttpOnly + SameSite=Lax y CSRF de doble envío.
	portal, r := e.entrarPortal(ticket)
	if r.status != 200 || portal.csrf == "" {
		t.Fatalf("entrar: %d %s", r.status, r.crudo)
	}
	cli := r.cuerpo["cliente"].(map[string]any)
	if cli["clienteId"] != float64(77) || cli["empresaId"] != float64(1) || cli["nombre"] != "Ana Pérez" {
		t.Fatalf("cliente de la sesión: %v", cli)
	}
	var setCookie string
	for _, v := range r.h.Values("Set-Cookie") {
		if strings.HasPrefix(v, identidad.CookiePortal+"=") {
			setCookie = v
		}
	}
	if !strings.Contains(setCookie, "HttpOnly") || !strings.Contains(setCookie, "SameSite=Lax") || !strings.Contains(setCookie, "Path=/") {
		t.Fatalf("cookie del portal: %s", setCookie)
	}
	if k := portal.cookie(identidad.CookiePortal); k == nil || strings.Contains(string(r.crudo), k.Value) {
		t.Fatal("falta la cookie iris_portal o su valor salió en el cuerpo")
	}
	if k := portal.cookie(identidad.CookiePortalCSRF); k == nil || k.Value != portal.csrf {
		t.Fatal("falta la cookie iris_portal_csrf")
	}
	if portal.cookie(identidad.CookieSesion) != nil {
		t.Fatal("el portal no debe abrir una sesión del panel")
	}
	// Auditado con el actor cliente (actor_id = id del cliente en Argos) y su empresa.
	var actor, canal string
	var actorID, empresa int64
	if err := pruebas.Pool(t).QueryRow(context.Background(),
		`SELECT actor_tipo, actor_id, empresa_id, canal FROM auditoria.acciones WHERE accion = 'portal.entrar' ORDER BY id DESC LIMIT 1`).
		Scan(&actor, &actorID, &empresa, &canal); err != nil || actor != "cliente" || actorID != 77 || empresa != 1 {
		t.Fatalf("auditoría del canje: %s %d %d %s %v", actor, actorID, empresa, canal, err)
	}

	// Un solo uso: el mismo ticket ya no entra; uno vencido tampoco.
	_, r = e.entrarPortal(ticket)
	r.problema(t, 401, "ticket_invalido")
	vencido := emitir(t, argos, 77, 1)
	if _, err := pruebas.Pool(t).Exec(context.Background(), `UPDATE nucleo.portal_accesos SET expira_en = now() - interval '1 second' WHERE usado_en IS NULL`); err != nil {
		t.Fatal(err)
	}
	_, r = e.entrarPortal(vencido)
	r.problema(t, 401, "ticket_invalido")

	// La sesión del portal.
	r = portal.pedir(http.MethodGet, "/api/v1/portal/sesion", nil)
	if r.status != 200 || r.cuerpo["cliente"].(map[string]any)["clienteId"] != float64(77) {
		t.Fatalf("sesión del portal: %d %s", r.status, r.crudo)
	}
	// Un caso del portal corre con canal portal y la empresa de la sesión.
	r = portal.pedir(http.MethodGet, "/api/v1/portal/prueba/cliente", nil)
	if r.status != 200 || r.cuerpo["valor"] != "portal:1:77" {
		t.Fatalf("sonda del portal: %d %s", r.status, r.crudo)
	}
	if r := portal.pedir(http.MethodGet, "/api/v1/portal/prueba/cliente", nil, identidad.CabeceraEmpresa, "1"); r.status != 200 {
		t.Fatalf("con su propia empresa: %d %s", r.status, r.crudo)
	}

	// Cerrar: exige el CSRF del portal.
	sinCSRF := *portal
	sinCSRF.csrf = ""
	sinCSRF.pedir(http.MethodDelete, "/api/v1/portal/sesion", nil).problema(t, 403, "csrf_invalido")
	otro := *portal
	otro.csrf = "no-es-el-csrf-del-portal"
	otro.pedir(http.MethodDelete, "/api/v1/portal/sesion", nil).problema(t, 403, "csrf_invalido")
	if r := portal.pedir(http.MethodDelete, "/api/v1/portal/sesion", nil); r.status != http.StatusNoContent {
		t.Fatalf("cerrar: %d %s", r.status, r.crudo)
	}
	portal.pedir(http.MethodGet, "/api/v1/portal/sesion", nil).problema(t, 401, "no_autenticado")
}

func TestPortalCookieSegura(t *testing.T) {
	e := entornoPortal(t, identidad.Config{CookieSegura: true})
	argos := e.argos(nucleo.PermPortalEmitir)
	_, r := e.entrarPortal(emitir(t, argos, 77, 1))
	if r.status != 200 {
		t.Fatalf("entrar: %d %s", r.status, r.crudo)
	}
	for _, v := range r.h.Values("Set-Cookie") {
		if strings.HasPrefix(v, "iris_portal") && !strings.Contains(v, "Secure") {
			t.Fatalf("cookie sin Secure: %s", v)
		}
	}
}

// El actor cliente no llega al panel: ni a /tickets, /usuarios o /yo (la cookie del portal no
// cuenta fuera de /api/v1/portal/), ni a una acción del panel montada bajo /portal (403), ni a
// otra empresa.
func TestPortalAislamientoDelActorCliente(t *testing.T) {
	e := entornoPortal(t, identidad.Config{CookieSegura: false})
	portal, r := e.entrarPortal(emitir(t, e.argos(nucleo.PermPortalEmitir), 77, 1))
	if r.status != 200 {
		t.Fatalf("entrar: %d %s", r.status, r.crudo)
	}
	for _, ruta := range []string{"/api/v1/tickets", "/api/v1/usuarios", "/api/v1/yo", "/api/v1/empresas", "/api/v1/tokens"} {
		portal.pedir(http.MethodGet, ruta, nil).problema(t, 401, "no_autenticado")
	}
	portal.pedir(http.MethodPost, "/api/v1/tickets", map[string]any{"asunto": "Desde el portal"}).problema(t, 401, "no_autenticado")
	portal.pedir(http.MethodGet, "/api/v1/portal/prueba/panel", nil).problema(t, 403, "prohibido")
	portal.pedir(http.MethodGet, "/api/v1/portal/prueba/cliente", nil, identidad.CabeceraEmpresa, "2").problema(t, 403, "empresa_no_permitida")
	portal.pedir(http.MethodGet, "/api/v1/portal/sesion", nil, identidad.CabeceraEmpresa, "x").problema(t, 403, "empresa_no_permitida")

	// Directo a los casos de uso: el actor cliente solo pasa por Acceso Cliente.
	ctx := pruebas.ContextoCliente(pruebas.Pool(t), 77, 1)
	if _, err := soporte.Nuevo(soporte.Config{}).ListarTickets.Ejecutar(ctx, soporte.EntradaListarTickets{}); !esProblema(err, "prohibido") {
		t.Fatalf("listar tickets como cliente: %v", err)
	}
	if _, err := e.mod.ObtenerYo.Ejecutar(ctx, struct{}{}); !esProblema(err, "prohibido") {
		t.Fatalf("yo como cliente (Autenticado): %v", err)
	}
	if _, err := e.mod.ListarUsuarios.Ejecutar(ctx, identidad.EntradaListarUsuarios{}); !esProblema(err, "prohibido") {
		t.Fatalf("usuarios como cliente: %v", err)
	}
	// Una persona del panel no usa los casos del portal: su sesión no cuenta en /api/v1/portal/
	// y, directo al caso de uso, 403.
	_, correo := e.usuario("admin")
	panel := e.cliente()
	if r := panel.entrar(correo); r.status != 200 {
		t.Fatalf("login: %d %s", r.status, r.crudo)
	}
	panel.pedir(http.MethodGet, "/api/v1/portal/sesion", nil).problema(t, 401, "no_autenticado")
	panel.pedir(http.MethodGet, "/api/v1/portal/prueba/cliente", nil).problema(t, 401, "no_autenticado")
	if _, err := sondaCliente.Ejecutar(pruebas.ContextoUsuarioEnEmpresa(pruebas.Pool(t), 1, 1, nucleo.PermTicketsVer), struct{}{}); !esProblema(err, "prohibido") {
		t.Fatalf("sonda del portal con un usuario: %v", err)
	}
	if _, err := sondaCliente.Ejecutar(nucleo.ConBD(context.Background(), pruebas.Pool(t)), struct{}{}); !esProblema(err, "no_autenticado") {
		t.Fatalf("sonda del portal anónima: %v", err)
	}
	// Con las dos cookies en el mismo navegador (sin el CSRF del panel), el portal entra; en
	// /api/v1/portal/ cuenta la del portal y en el panel la del panel.
	ambos := e.cliente()
	ambos.http.Jar = panel.http.Jar
	_, r = ambos.entrarPortalCon(emitir(t, e.argos(nucleo.PermPortalEmitir), 78, 1))
	if r.status != 200 {
		t.Fatalf("entrar con sesión del panel: %d %s", r.status, r.crudo)
	}
	if r := ambos.pedir(http.MethodGet, "/api/v1/portal/prueba/cliente", nil); r.status != 200 || r.cuerpo["valor"] != "portal:1:78" {
		t.Fatalf("portal con las dos cookies: %d %s", r.status, r.crudo)
	}
	if r := ambos.pedir(http.MethodGet, "/api/v1/yo", nil); r.status != 200 || r.cuerpo["actor"] != "usuario" {
		t.Fatalf("panel con las dos cookies: %d %s", r.status, r.crudo)
	}
}

func (c *cliente) entrarPortalCon(ticket string) (*cliente, respuesta) {
	c.t.Helper()
	r := c.pedir(http.MethodPost, "/api/v1/portal/sesion", map[string]any{"ticket": ticket})
	return c, r
}

// Sin límite por IP: muchos canjes inválidos (todos desde la misma IP, como detrás del proxy
// del panel) no impiden un canje válido después; los rechazos no se auditan, se cuentan.
func TestPortalCanjesInvalidosNoBloquean(t *testing.T) {
	e := entornoPortal(t, identidad.Config{CookieSegura: false})
	argos := e.argos(nucleo.PermPortalEmitir)
	contar := func() int {
		var n int
		if err := pruebas.Pool(t).QueryRow(context.Background(),
			`SELECT count(*) FROM auditoria.acciones WHERE accion = 'portal.entrar' AND resultado = 'error'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	antes, rechazos := contar(), e.mod.RechazosPortalSinAuditar()
	for i := range 60 {
		_, r := e.entrarPortal(fmt.Sprintf("ticket-inventado-%02d-0123456789abcdef", i))
		r.problema(t, 401, "ticket_invalido")
	}
	if contar() != antes || e.mod.RechazosPortalSinAuditar()-rechazos != 60 {
		t.Fatalf("auditados %d, contados %d", contar()-antes, e.mod.RechazosPortalSinAuditar()-rechazos)
	}
	if _, r := e.entrarPortal(emitir(t, argos, 77, 1)); r.status != 200 {
		t.Fatalf("canje válido tras 60 inválidos: %d %s", r.status, r.crudo)
	}
}

// Freno global (muy alto en producción): pasado el tope de fallos en la ventana, los canjes
// fallidos responden 429; un ticket válido entra igual (regresión de la revisión: antes el tope
// cortaba antes del canje y unas pocas peticiones basura bloqueaban a todos).
func TestPortalTopeGlobal(t *testing.T) {
	e := entornoPortal(t, identidad.Config{CookieSegura: false, PortalTope: 3, PortalVentanaTope: time.Minute})
	argos := e.argos(nucleo.PermPortalEmitir)
	for range 3 {
		_, r := e.entrarPortal("ticket-inventado-0123456789abcdef")
		r.problema(t, 401, "ticket_invalido")
	}
	_, r := e.entrarPortal("ticket-inventado-0123456789abcdef")
	r.problema(t, 429, "demasiados_intentos")
	if r.h.Get("Retry-After") == "" {
		t.Fatal("falta Retry-After")
	}
	if _, r := e.entrarPortal(emitir(t, argos, 77, 1)); r.status != 200 {
		t.Fatalf("ticket válido con el tope excedido: %d %s", r.status, r.crudo)
	}
}

// iris portal acceso (consola): el actor sistema emite para una empresa registrada.
func TestPortalEmitirDesdeConsola(t *testing.T) {
	e := entornoPortal(t, identidad.Config{CookieSegura: false})
	ctx := pruebas.ContextoSistema(pruebas.Pool(t))
	a, err := e.mod.EmitirAccesoPortal.Ejecutar(ctx, identidad.EntradaEmitirAccesoPortal{ClienteID: 5, EmpresaID: 2})
	if err != nil || !strings.HasPrefix(a.URL, "https://iris.prueba/soporte/entrar#") {
		t.Fatalf("consola: %+v %v", a, err)
	}
	if _, err := e.mod.EmitirAccesoPortal.Ejecutar(ctx, identidad.EntradaEmitirAccesoPortal{ClienteID: 5, EmpresaID: 999}); !esProblema(err, "empresa_no_registrada") {
		t.Fatalf("empresa no registrada: %v", err)
	}
	_, ticket, _ := strings.Cut(a.URL, "#")
	if _, r := e.entrarPortal(ticket); r.status != 200 || r.cuerpo["cliente"].(map[string]any)["empresaId"] != float64(2) {
		t.Fatalf("entrar con el acceso de consola: %d %s", r.status, r.crudo)
	}
}

// iris integracion crear: usuario de integración con el rol base de su sistema, sus empresas y
// un token, sin pasar por admin.
func TestPrepararIntegracion(t *testing.T) {
	e := entornoPortal(t, identidad.Config{CookieSegura: false})
	ctx := pruebas.ContextoSistema(pruebas.Pool(t))
	conToken := func(valor string) *cliente {
		c := e.cliente()
		c.bearer = valor
		return c
	}
	correo := pruebas.Correo("argos")
	a, err := e.mod.PrepararIntegracion(ctx, identidad.EntradaPrepararIntegracion{Sistema: "argosisp", Empresas: []int64{1}, Correo: correo})
	if err != nil || !a.Creado || a.Usuario.Tipo != "integracion" || len(a.Usuario.Roles) != 1 || a.Usuario.Roles[0].Rol != "integracion_argos" ||
		!strings.HasPrefix(a.Token.Valor, "iris_pat_") {
		t.Fatalf("argos: %+v %v", a, err)
	}
	argos := conToken(a.Token.Valor)
	emitir(t, argos, 77, 1)
	argos.pedir(http.MethodPost, "/api/v1/portal/tickets-acceso", map[string]any{"clienteId": 77, "empresaId": 2}).problema(t, 403, "empresa_no_permitida")
	argos.pedir(http.MethodGet, "/api/v1/usuarios", nil).problema(t, 403, "prohibido")
	if r := argos.pedir(http.MethodGet, "/api/v1/tickets", nil); r.status != 200 {
		t.Fatalf("tickets.ver: %d %s", r.status, r.crudo)
	}
	// Otra vez con el mismo correo: reutiliza el usuario, suma la empresa y emite otro token.
	b, err := e.mod.PrepararIntegracion(ctx, identidad.EntradaPrepararIntegracion{Sistema: "argosisp", Empresas: []int64{1, 2}, Correo: correo})
	if err != nil || b.Creado || b.Usuario.ID != a.Usuario.ID || len(b.Usuario.Empresas) != 2 || b.Token.Valor == a.Token.Valor {
		t.Fatalf("reutilizar: %+v %v", b, err)
	}
	emitir(t, conToken(b.Token.Valor), 77, 2)

	// Horus: tickets.ver y tickets.crear, sin portal.emitir.
	h, err := e.mod.PrepararIntegracion(ctx, identidad.EntradaPrepararIntegracion{Sistema: "horus", Empresas: []int64{1}, Correo: pruebas.Correo("horus")})
	if err != nil || h.Usuario.Roles[0].Rol != "integracion_horus" {
		t.Fatalf("horus: %+v %v", h, err)
	}
	conToken(h.Token.Valor).pedir(http.MethodPost, "/api/v1/portal/tickets-acceso", map[string]any{"clienteId": 77, "empresaId": 1}).problema(t, 403, "prohibido")

	// Un correo de persona no se convierte en integración; sistema desconocido, 422.
	_, persona := e.usuario("lectura")
	if _, err := e.mod.PrepararIntegracion(ctx, identidad.EntradaPrepararIntegracion{Sistema: "horus", Empresas: []int64{1}, Correo: persona}); !esProblema(err, "usuario_no_es_integracion") {
		t.Fatalf("persona: %v", err)
	}
	if _, err := e.mod.PrepararIntegracion(ctx, identidad.EntradaPrepararIntegracion{Sistema: "otro"}); !esProblema(err, "validacion") {
		t.Fatalf("sistema: %v", err)
	}
	// Regresión (revisión): con empresas registradas, --empresa es obligatorio.
	if _, err := e.mod.PrepararIntegracion(ctx, identidad.EntradaPrepararIntegracion{Sistema: "horus", Correo: pruebas.Correo("sin")}); !esProblema(err, "validacion") {
		t.Fatalf("sin empresas: %v", err)
	}
	// Regresión (revisión): no se reutiliza (ni se le emite token) una integración con más roles
	// que el de su sistema.
	ancho := pruebas.Correo("ancho")
	if _, err := e.mod.CrearUsuario.Ejecutar(ctx, identidad.EntradaCrearUsuario{
		Nombre: "Horus ancho", Correo: ancho, Tipo: "integracion", Empresas: []int64{1},
		Roles: []identidad.AsignacionRol{{Rol: "integracion_horus"}, {Rol: "lectura"}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.mod.PrepararIntegracion(ctx, identidad.EntradaPrepararIntegracion{Sistema: "horus", Empresas: []int64{1}, Correo: ancho}); !esProblema(err, "usuario_no_es_integracion") {
		t.Fatalf("roles de más: %v", err)
	}
}

// Regresión (revisión): una sesión del panel no puede declarar X-Iris-Canal: portal; ese canal
// es solo del actor cliente. campo sí vale.
func TestSesionDelPanelNoDeclaraCanalPortal(t *testing.T) {
	e := nuevoEntorno(t)
	canalDeCierre := func(canal string) string {
		t.Helper()
		_, correo := e.usuario("lectura")
		c := e.cliente()
		if r := c.entrar(correo); r.status != 200 {
			t.Fatalf("login: %d %s", r.status, r.crudo)
		}
		r := c.pedir(http.MethodDelete, "/api/v1/sesion", nil, identidad.CabeceraCanal, canal)
		if r.status != http.StatusNoContent {
			t.Fatalf("cerrar: %d %s", r.status, r.crudo)
		}
		var v string
		if err := pruebas.Pool(t).QueryRow(context.Background(),
			`SELECT canal FROM auditoria.acciones WHERE accion = 'sesion.cerrar' AND request_id = $1`, r.h.Get(servidor.CabeceraRequestID)).Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	if c := canalDeCierre("portal"); c != "panel" {
		t.Fatalf("una sesión del panel declaró el canal %s", c)
	}
	if c := canalDeCierre("campo"); c != "campo" {
		t.Fatalf("campo: %s", c)
	}
}

// Regresión (revisión): si Iris no conoce al cliente, al emitir se lee en Argos (debe existir y
// ser de la empresa); si no se pudo validar, se emite y queda cliente_validado=false en la
// auditoría. Con instantánea no se pregunta a Argos.
func TestPortalValidaClienteEnArgos(t *testing.T) {
	llamadas := 0
	argosDice := map[int64]identidad.ValidacionCliente{
		90: {Validado: true, Existe: true, Empresas: []int64{1}, ConServicios: true},
		91: {Validado: true, Existe: true, Empresas: []int64{2}, ConServicios: true},
		92: {Validado: true},
		93: {Motivo: "conector_argos_apagado"},
	}
	e := entornoPortal(t, identidad.Config{CookieSegura: false, ValidarCliente: func(_ context.Context, id int64) (identidad.ValidacionCliente, error) {
		llamadas++
		return argosDice[id], nil
	}})
	argos := e.argos(nucleo.PermPortalEmitir)
	validacion := func() string {
		var v string
		if err := pruebas.Pool(t).QueryRow(context.Background(),
			`SELECT coalesce(entrada->'auditoria'->>'cliente_validado', '') || ':' || coalesce(entrada->'auditoria'->>'validacion', '')
			   FROM auditoria.acciones WHERE accion = 'portal.emitir' AND resultado = 'ok' ORDER BY id DESC LIMIT 1`).Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	emitir(t, argos, 90, 1)
	if v := validacion(); v != "true:argos" {
		t.Fatalf("validado en Argos: %s", v)
	}
	argos.pedir(http.MethodPost, "/api/v1/portal/tickets-acceso", map[string]any{"clienteId": 91, "empresaId": 1}).problema(t, 422, "empresa_no_coincide")
	argos.pedir(http.MethodPost, "/api/v1/portal/tickets-acceso", map[string]any{"clienteId": 92, "empresaId": 1}).problema(t, 422, "cliente_no_existe")
	emitir(t, argos, 93, 1)
	if v := validacion(); v != "false:conector_argos_apagado" {
		t.Fatalf("sin validar: %s", v)
	}
	// Con instantánea: no se pregunta a Argos.
	if _, err := pruebas.Pool(t).Exec(context.Background(),
		`INSERT INTO integracion.argos_clientes (id, empresa_id, nombre, sincronizado_en) VALUES (88, 1, 'Conocido', now())`); err != nil {
		t.Fatal(err)
	}
	antes := llamadas
	emitir(t, argos, 88, 1)
	if llamadas != antes || validacion() != "true:instantanea" {
		t.Fatalf("con instantánea: llamadas %d → %d, %s", antes, llamadas, validacion())
	}
}
