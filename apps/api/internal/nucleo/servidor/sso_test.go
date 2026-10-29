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
	"strings"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/servidor"
)

// Autologin desde Argos («Ver en Iris»): ticket de 60 s y un solo uso emitido solo con el token
// del conector activo; vínculo explícito tras un login posterior al canje; MFA como el login.

const rutaTicketsSSO = "/api/v1/integraciones/argosisp/sso/tickets"

type entornoSSO struct {
	*entorno
	argos      *cliente // Bearer = el token entregado al conector activo
	otroToken  *cliente // otro token del mismo usuario de integración, también con sso.emitir
	conectorID int64
}

func nuevoEntornoSSO(t *testing.T) *entornoSSO {
	t.Helper()
	pool := pruebas.Pool(t)
	clave := make([]byte, 32)
	_, _ = rand.Read(clave)
	cif, err := nucleo.NuevoCifrador(base64.StdEncoding.EncodeToString(clave))
	if err != nil {
		t.Fatal(err)
	}
	mod := identidad.Nuevo(identidad.Config{Cifrador: cif, BaseURL: "https://iris.prueba"})
	h, _ := servidor.Nuevo(servidor.Deps{BD: pool, Identidad: mod, Version: "prueba", Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	e := &entornoSSO{entorno: &entorno{t: t, srv: httptest.NewServer(h), mod: mod}}
	t.Cleanup(e.srv.Close)

	ctx := pruebas.ContextoSistema(pool)
	u, err := mod.CrearUsuario.Ejecutar(ctx, identidad.EntradaCrearUsuario{
		Nombre: "Argos", Correo: pruebas.Correo("argos"), Tipo: "integracion",
		Roles: []identidad.AsignacionRol{{Rol: "integracion_argos"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	token := func() (*cliente, int64) {
		tk, err := mod.CrearToken.Ejecutar(ctx, identidad.EntradaCrearToken{UsuarioID: u.ID, Nombre: "sso " + pruebas.Unico(), Permisos: []nucleo.Permiso{nucleo.PermSSOEmitir}})
		if err != nil {
			t.Fatal(err)
		}
		c := e.cliente()
		c.bearer = tk.Valor
		return c, tk.Token.ID
	}
	var entregado int64
	e.argos, entregado = token()
	e.otroToken, _ = token()
	e.ejecutar(`UPDATE integracion.conectores SET estado = 'apagado' WHERE sistema = 'argosisp'`)
	// Sin conector activo no se emite.
	e.emitir(e.argos, "u-1", "ticket", "5").problema(t, 409, "conector_apagado")
	if err := pool.QueryRow(context.Background(), `INSERT INTO integracion.conectores
		(sistema, base_url, origen, token_cifrado, secreto_webhook_cifrado, estado, token_entregado_id)
		VALUES ('argosisp', 'https://argos.prueba', 'enlace_remoto', '\x00', '\x00', 'activo', $1) RETURNING id`, entregado).Scan(&e.conectorID); err != nil {
		t.Fatal(err)
	}
	return e
}

func (e *entornoSSO) ejecutar(sql string, args ...any) {
	e.t.Helper()
	if _, err := pruebas.Pool(e.t).Exec(context.Background(), sql, args...); err != nil {
		e.t.Fatal(err)
	}
}

func (e *entornoSSO) contar(sql string, args ...any) int {
	e.t.Helper()
	var n int
	if err := pruebas.Pool(e.t).QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

func (e *entornoSSO) emitir(c *cliente, externo, tipo, id string) respuesta {
	e.t.Helper()
	return c.pedir(http.MethodPost, rutaTicketsSSO, map[string]any{
		"usuario": map[string]any{"id_externo": externo, "correo": "ana@isp.gt", "nombre": "Ana López"},
		"destino": map[string]any{"tipo": tipo, "id_externo": id},
	})
}

// ticket emite con el token del conector y devuelve lo que va después de #.
func (e *entornoSSO) ticket(externo, tipo, id string) string {
	e.t.Helper()
	r := e.emitir(e.argos, externo, tipo, id)
	url, _ := r.cuerpo["url"].(string)
	base, ticket, ok := strings.Cut(url, "#")
	if r.status != 201 || !ok || base != "https://iris.prueba/sso/entrar" || len(ticket) < 40 {
		e.t.Fatalf("emitir: %d %s", r.status, r.crudo)
	}
	return ticket
}

func canjear(c *cliente, ticket string) respuesta {
	c.t.Helper()
	r := c.pedir(http.MethodPost, "/api/v1/sso/canjear", map[string]any{"ticket": ticket})
	if s, ok := r.cuerpo["sesion"].(map[string]any); ok {
		c.csrf, _ = s["csrfToken"].(string)
	}
	return r
}

func vincular(c *cliente) respuesta {
	c.t.Helper()
	return c.pedir(http.MethodPost, "/api/v1/sso/vincular", nil)
}

func TestSSOEmitirSoloConElTokenDelConectorActivo(t *testing.T) {
	e := nuevoEntornoSSO(t)
	// Otro token con sso.emitir, o una sesión con todos los permisos: 403.
	e.emitir(e.otroToken, "u-1", "ticket", "5").problema(t, 403, "prohibido")
	_, correo := e.usuario("admin")
	admin := e.cliente()
	admin.entrar(correo)
	e.emitir(admin, "u-1", "ticket", "5").problema(t, 403, "prohibido")
	// Destinos: solo ticket, orden y prospecto, con id de Iris.
	e.emitir(e.argos, "u-1", "cliente", "5").problema(t, 422, "validacion")
	e.emitir(e.argos, "u-1", "ticket", "abc").problema(t, 422, "validacion")
	e.emitir(e.argos, "", "ticket", "5").problema(t, 422, "validacion")
	// correo y nombre pueden venir null (Argos los manda así si no los tiene).
	r := e.argos.pedir(http.MethodPost, rutaTicketsSSO, map[string]any{
		"usuario": map[string]any{"id_externo": "u-2", "correo": nil, "nombre": nil},
		"destino": map[string]any{"tipo": "prospecto", "id_externo": "9"},
	})
	if r.status != 201 {
		t.Fatalf("correo y nombre null: %d %s", r.status, r.crudo)
	}
	ticket := e.ticket("u-1", "orden", "55")
	// Solo se guarda el SHA-256, con 60 s, atado al conector y al token que lo pidió.
	if e.contar(`SELECT count(*) FROM nucleo.sso_tickets WHERE ticket_hash = $1 AND conector_id = $2
		AND expira_en BETWEEN now() + interval '50 seconds' AND now() + interval '61 seconds'
		AND emitido_token_id = (SELECT token_entregado_id FROM integracion.conectores WHERE id = $2)`, identidad.Hash256(ticket), e.conectorID) != 1 {
		t.Fatal("ticket guardado como SHA-256 con 60 s")
	}
	if e.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'sso.emitir' AND resultado = 'ok' AND entrada::text LIKE '%u-1%'`) < 1 {
		t.Fatal("emitir se audita")
	}
	if e.contar(`SELECT count(*) FROM auditoria.acciones a WHERE a::text LIKE '%' || $1 || '%'`, ticket) != 0 {
		t.Fatal("el ticket no queda en la auditoría")
	}
}

func TestSSOVincularYEntrar(t *testing.T) {
	e := nuevoEntornoSSO(t)
	ana, correo := e.usuario("lectura")

	// Sin vínculo: vincular, con cookie solo para /api/v1/sso.
	nav := e.cliente()
	nav.entrar(correo) // una sesión abierta antes del canje no sirve para vincular
	ticket := e.ticket("u-17", "orden", "55")
	r := canjear(nav, ticket)
	destino, _ := r.cuerpo["destino"].(map[string]any)
	externo, _ := r.cuerpo["externo"].(map[string]any)
	if r.status != 200 || r.cuerpo["estado"] != "vincular" || destino["ruta"] != "/ordenes/55" || externo["correo"] != "ana@isp.gt" || r.cuerpo["sesion"] != nil {
		t.Fatalf("canje sin vínculo: %d %s", r.status, r.crudo)
	}
	if !strings.Contains(r.h.Get("Set-Cookie"), identidad.CookieVinculacionSSO+"=") || !strings.Contains(r.h.Get("Set-Cookie"), "Path=/api/v1/sso") ||
		!strings.Contains(r.h.Get("Set-Cookie"), "HttpOnly") {
		t.Fatalf("cookie de vinculación: %v", r.h.Values("Set-Cookie"))
	}
	// Un solo uso.
	canjear(e.cliente(), ticket).problema(t, 401, "ticket_invalido")
	vincular(nav).problema(t, 422, "reautenticacion_requerida")
	// Sin sesión: 401; con otra cookie de vinculación (otro navegador sin canje): 422.
	vincular(e.cliente()).problema(t, 401, "no_autenticado")
	otro := e.cliente()
	otro.entrar(correo)
	vincular(otro).problema(t, 422, "vinculacion_invalida")
	// Tras iniciar sesión de nuevo, vincula.
	time.Sleep(10 * time.Millisecond)
	nav.entrar(correo)
	r = vincular(nav)
	vinculo, _ := r.cuerpo["vinculo"].(map[string]any)
	if r.status != 200 || vinculo["usuarioExterno"] != "u-17" || vinculo["sistema"] != "argosisp" {
		t.Fatalf("vincular: %d %s", r.status, r.crudo)
	}
	vinculoID := int64(vinculo["id"].(float64))
	if !strings.Contains(r.h.Get("Set-Cookie"), "Max-Age=0") {
		t.Fatal("vincular borra la cookie")
	}
	vincular(nav).problema(t, 422, "vinculacion_invalida")
	if e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'usuario.sso_vinculado' AND (payload->>'usuario_id')::bigint = $1
		AND payload->>'usuario_externo' = 'u-17'`, ana) != 1 {
		t.Fatal("falta usuario.sso_vinculado")
	}

	// Con vínculo: entra directo, sin CSRF, aunque el navegador ya tuviera otra sesión.
	nuevo := e.cliente()
	r = canjear(nuevo, e.ticket("u-17", "prospecto", "9"))
	destino, _ = r.cuerpo["destino"].(map[string]any)
	if r.status != 200 || r.cuerpo["estado"] != "sesion" || destino["ruta"] != "/comercial/9" {
		t.Fatalf("canje con vínculo: %d %s", r.status, r.crudo)
	}
	if y := nuevo.pedir(http.MethodGet, "/api/v1/yo", nil); y.status != 200 || y.cuerpo["usuario"].(map[string]any)["id"] != float64(ana) {
		t.Fatalf("yo tras el autologin: %d %s", y.status, y.crudo)
	}
	nav.csrf = ""
	if r := canjear(nav, e.ticket("u-17", "ticket", "3")); r.status != 200 || r.cuerpo["estado"] != "sesion" {
		t.Fatalf("canje con sesión previa (sin CSRF): %d %s", r.status, r.crudo)
	}

	// Una cuenta de Iris se vincula con una sola de ese Argos.
	tercero := e.cliente()
	if r := canjear(tercero, e.ticket("u-99", "ticket", "3")); r.cuerpo["estado"] != "vincular" {
		t.Fatalf("u-99: %s", r.crudo)
	}
	time.Sleep(10 * time.Millisecond)
	tercero.entrar(correo)
	vincular(tercero).problema(t, 409, "vinculo_existente")
	// Una cuenta de Argos ya vinculada no se vincula con otro usuario.
	_, correoBeto := e.usuario()
	e.ejecutar(`UPDATE nucleo.sso_tickets SET vinculado_en = NULL, usuario_externo = 'u-17' WHERE usuario_externo = 'u-99'`)
	tercero.entrar(correoBeto)
	vincular(tercero).problema(t, 409, "cuenta_externa_vinculada")

	// Ver y quitar vínculos: los propios sin permiso; los de otro, con usuarios.ver / usuarios.editar.
	propia := e.cliente()
	propia.entrar(correo)
	if r := propia.pedir(http.MethodGet, fmt.Sprintf("/api/v1/usuarios/%d/vinculos-sso", ana), nil); r.status != 200 || len(r.cuerpo["data"].([]any)) != 1 {
		t.Fatalf("ver propios: %d %s", r.status, r.crudo)
	}
	beto := e.cliente()
	beto.entrar(correoBeto)
	beto.pedir(http.MethodGet, fmt.Sprintf("/api/v1/usuarios/%d/vinculos-sso", ana), nil).problema(t, 403, "prohibido")
	beto.pedir(http.MethodPost, fmt.Sprintf("/api/v1/usuarios/%d/vinculos-sso/%d/borrar", ana, vinculoID), map[string]any{}).problema(t, 403, "prohibido")
	_, correoAdmin := e.usuario("admin")
	admin := e.cliente()
	admin.entrar(correoAdmin)
	if r := admin.pedir(http.MethodGet, fmt.Sprintf("/api/v1/usuarios/%d/vinculos-sso", ana), nil); r.status != 200 || len(r.cuerpo["data"].([]any)) != 1 {
		t.Fatalf("admin ve: %d %s", r.status, r.crudo)
	}
	if r := admin.pedir(http.MethodPost, fmt.Sprintf("/api/v1/usuarios/%d/vinculos-sso/%d/borrar", ana, vinculoID), map[string]any{"motivo": "cuenta compartida"}); r.status != 200 {
		t.Fatalf("admin borra: %d %s", r.status, r.crudo)
	}
	if e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'usuario.sso_desvinculado' AND (payload->>'vinculo_id')::bigint = $1`, vinculoID) != 1 {
		t.Fatal("falta usuario.sso_desvinculado")
	}
	propia.pedir(http.MethodPost, fmt.Sprintf("/api/v1/usuarios/%d/vinculos-sso/%d/borrar", ana, vinculoID), map[string]any{}).problema(t, 404, "no_encontrado")
	// Sin vínculo, el siguiente vuelve a pedir vincular.
	if r := canjear(e.cliente(), e.ticket("u-17", "ticket", "3")); r.cuerpo["estado"] != "vincular" {
		t.Fatalf("tras quitar el vínculo: %s", r.crudo)
	}
}

func TestSSOConMFAVencidoYConectorApagado(t *testing.T) {
	e := nuevoEntornoSSO(t)
	id, correo := e.usuario("lectura")
	conMFA(t, e.entorno, correo)
	e.ejecutar(`INSERT INTO nucleo.sso_vinculos (conector_id, usuario_externo, usuario_id) VALUES ($1, 'u-5', $2)`, e.conectorID, id)
	// Con MFA, la sesión queda pendiente como en el login.
	nav := e.cliente()
	r := canjear(nav, e.ticket("u-5", "ticket", "8"))
	sesion, _ := r.cuerpo["sesion"].(map[string]any)
	if r.status != 200 || r.cuerpo["estado"] != "mfa" || sesion["mfaRequerido"] != true || sesion["usuario"] != nil {
		t.Fatalf("canje con MFA: %d %s", r.status, r.crudo)
	}
	nav.pedir(http.MethodGet, "/api/v1/yo", nil).problema(t, 401, "no_autenticado")

	// Vencido: 401, sin fila de auditoría (anónimo).
	ticket := e.ticket("u-5", "ticket", "8")
	e.ejecutar(`UPDATE nucleo.sso_tickets SET expira_en = now() - interval '1 second' WHERE ticket_hash = $1`, identidad.Hash256(ticket))
	antes := e.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'sso.canjear'`)
	canjear(e.cliente(), ticket).problema(t, 401, "ticket_invalido")
	canjear(e.cliente(), "no-es-un-ticket-valido-0123456789").problema(t, 401, "ticket_invalido")
	if e.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'sso.canjear'`) != antes {
		t.Fatal("los canjes fallidos anónimos no se auditan")
	}

	// Cuenta vinculada desactivada: 403 (auditado).
	e.ejecutar(`UPDATE nucleo.usuarios SET activo = false WHERE id = $1`, id)
	canjear(e.cliente(), e.ticket("u-5", "ticket", "8")).problema(t, 403, "prohibido")
	e.ejecutar(`UPDATE nucleo.usuarios SET activo = true WHERE id = $1`, id)

	// Conector apagado: no se emite ni se canjea lo emitido antes.
	pendiente := e.ticket("u-5", "ticket", "8")
	e.ejecutar(`UPDATE integracion.conectores SET estado = 'apagado' WHERE id = $1`, e.conectorID)
	e.emitir(e.argos, "u-5", "ticket", "8").problema(t, 409, "conector_apagado")
	canjear(e.cliente(), pendiente).problema(t, 409, "conector_apagado")

	// La purga borra los tickets vencidos (los vínculos quedan).
	e.ejecutar(`UPDATE nucleo.sso_tickets SET expira_en = now() - interval '40 days'`)
	res, err := identidad.PurgarPortal(context.Background(), pruebas.Pool(t), time.Now(), identidad.RetencionPortal, 100)
	if err != nil || res.TicketsSSO < 3 || e.contar(`SELECT count(*) FROM nucleo.sso_tickets WHERE conector_id = $1`, e.conectorID) != 0 ||
		e.contar(`SELECT count(*) FROM nucleo.sso_vinculos WHERE conector_id = $1`, e.conectorID) != 1 {
		t.Fatalf("purga: %+v %v", res, err)
	}
}
