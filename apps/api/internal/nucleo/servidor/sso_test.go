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
	"sync"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/servidor"
)

// Autologin desde Argos («Ver en Iris»): ticket de 60 s y un solo uso emitido solo con el token
// del conector activo; vínculo confirmado tras un login posterior al canje y con el mismo correo
// (si no, lo crea un admin); sesión sso con vida corta, MFA a los privilegiados y re-autenticación.

const rutaTicketsSSO = "/api/v1/integraciones/argosisp/sso/tickets"

// correoSinVerificar, delante de un correo, hace que Argos lo mande con correo_verificado false.
const correoSinVerificar = "sin-verificar:"

type entornoSSO struct {
	*entorno
	argos      *cliente // Bearer = el token entregado al conector activo
	otroToken  *cliente // otro token del mismo usuario de integración, también con sso.emitir
	conectorID int64
}

func nuevoEntornoSSO(t *testing.T, cfg identidad.Config) *entornoSSO {
	t.Helper()
	pool := pruebas.Pool(t)
	clave := make([]byte, 32)
	_, _ = rand.Read(clave)
	cif, err := nucleo.NuevoCifrador(base64.StdEncoding.EncodeToString(clave))
	if err != nil {
		t.Fatal(err)
	}
	cfg.Cifrador, cfg.BaseURL = cif, "https://iris.prueba"
	mod := identidad.Nuevo(cfg)
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
	e.emitir(e.argos, "u-1", "", "ticket", "5").problema(t, 409, "conector_apagado")
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

// emitir pide un ticket; con correo, Argos lo da por verificado (salvo correoSinVerificar).
func (e *entornoSSO) emitir(c *cliente, externo, correo, tipo, id string) respuesta {
	e.t.Helper()
	verificado := correo != "" && !strings.HasPrefix(correo, correoSinVerificar)
	correo = strings.TrimPrefix(correo, correoSinVerificar)
	return c.pedir(http.MethodPost, rutaTicketsSSO, map[string]any{
		"usuario": map[string]any{"id_externo": externo, "correo": correo, "correo_verificado": verificado, "nombre": "Ana López"},
		"destino": map[string]any{"tipo": tipo, "id_externo": id},
	})
}

// ticket emite con el token del conector y devuelve lo que va después de #.
func (e *entornoSSO) ticket(externo, correo, tipo, id string) string {
	e.t.Helper()
	r := e.emitir(e.argos, externo, correo, tipo, id)
	url, _ := r.cuerpo["url"].(string)
	base, ticket, ok := strings.Cut(url, "#")
	if r.status != 201 || !ok || base != "https://iris.prueba/sso/entrar" || len(ticket) < 40 {
		e.t.Fatalf("emitir: %d %s", r.status, r.crudo)
	}
	return ticket
}

// vincularCon vincula directo en la base (para las pruebas que no prueban la vinculación).
func (e *entornoSSO) vincularCon(externo string, usuario int64) {
	e.t.Helper()
	e.ejecutar(`INSERT INTO nucleo.sso_vinculos (conector_id, usuario_externo, usuario_id) VALUES ($1, $2, $3)`, e.conectorID, externo, usuario)
}

func canjear(c *cliente, ticket string, extra ...any) respuesta {
	c.t.Helper()
	cuerpo := map[string]any{"ticket": ticket}
	for i := 0; i+1 < len(extra); i += 2 {
		cuerpo[extra[i].(string)] = extra[i+1]
	}
	r := c.pedir(http.MethodPost, "/api/v1/sso/canjear", cuerpo)
	if s, ok := r.cuerpo["sesion"].(map[string]any); ok {
		c.csrf, _ = s["csrfToken"].(string)
	}
	return r
}

func vincular(c *cliente, externo string) respuesta {
	c.t.Helper()
	return c.pedir(http.MethodPost, "/api/v1/sso/vincular", map[string]any{"id_externo": externo})
}

func TestSSOEmitirSoloConElTokenDelConectorActivo(t *testing.T) {
	e := nuevoEntornoSSO(t, identidad.Config{})
	// Otro token con sso.emitir, o una sesión con todos los permisos: 403.
	e.emitir(e.otroToken, "u-1", "", "ticket", "5").problema(t, 403, "prohibido")
	_, correo := e.usuario("admin")
	admin := e.cliente()
	admin.entrar(correo)
	e.emitir(admin, "u-1", "", "ticket", "5").problema(t, 403, "prohibido")
	// Destinos: solo ticket, orden y prospecto, con id de Iris.
	e.emitir(e.argos, "u-1", "", "cliente", "5").problema(t, 422, "validacion")
	e.emitir(e.argos, "u-1", "", "ticket", "abc").problema(t, 422, "validacion")
	e.emitir(e.argos, "", "", "ticket", "5").problema(t, 422, "validacion")
	// correo y nombre pueden venir null (Argos los manda así si no los tiene).
	r := e.argos.pedir(http.MethodPost, rutaTicketsSSO, map[string]any{
		"usuario": map[string]any{"id_externo": "u-2", "correo": nil, "nombre": nil},
		"destino": map[string]any{"tipo": "prospecto", "id_externo": "9"},
	})
	if r.status != 201 {
		t.Fatalf("correo y nombre null: %d %s", r.status, r.crudo)
	}
	ticket := e.ticket("u-1", "", "orden", "55")
	// Solo se guarda el SHA-256, con 60 s (reloj de la base), atado al conector y al token.
	if e.contar(`SELECT count(*) FROM nucleo.sso_tickets WHERE ticket_hash = $1 AND conector_id = $2
		AND expira_en BETWEEN created_at + interval '59 seconds' AND created_at + interval '61 seconds'
		AND emitido_token_id = (SELECT token_entregado_id FROM integracion.conectores WHERE id = $2)`, identidad.Hash256(ticket), e.conectorID) != 1 {
		t.Fatal("ticket guardado como SHA-256 con 60 s")
	}
	if e.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'sso.emitir' AND resultado = 'ok' AND entrada::text LIKE '%u-1%'`) < 1 {
		t.Fatal("emitir se audita")
	}
	// Canjeado, el ticket queda como *** en la auditoría.
	nav := e.cliente()
	if r := canjear(nav, ticket); r.status != 200 {
		t.Fatalf("canje: %d %s", r.status, r.crudo)
	}
	if e.contar(`SELECT count(*) FROM auditoria.acciones a WHERE a::text LIKE '%' || $1 || '%'`, ticket) != 0 ||
		e.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'sso.canjear' AND entrada->>'ticket' = '***'`) < 1 {
		t.Fatal("el ticket no queda en claro en la auditoría")
	}
}

// Tope de emisión por conector (429 con Retry-After) y alerta por muchos id_externo distintos.
func TestSSOTopeYAlertaDeEmision(t *testing.T) {
	e := nuevoEntornoSSO(t, identidad.Config{SSOTopeEmision: 4, SSOAlertaExternos: 2})
	for i := range 4 {
		e.ticket(fmt.Sprintf("u-%d", i), "", "ticket", "1")
	}
	r := e.emitir(e.argos, "u-9", "", "ticket", "1")
	r.problema(t, 429, "demasiados_tickets")
	if s := r.h.Get("Retry-After"); s == "" || s == "0" {
		t.Fatalf("Retry-After: %q", s)
	}
	if e.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'sso.emitir' AND entrada->'auditoria' ? 'alerta_externos_distintos'`) < 1 {
		t.Fatal("la alerta queda anotada en la auditoría")
	}
}

func TestSSOVincularYEntrar(t *testing.T) {
	e := nuevoEntornoSSO(t, identidad.Config{})
	ana, correo := e.usuario("lectura")
	correoArgos := strings.ToUpper(correo) // mismo correo, otras mayúsculas

	// Sin vínculo: vincular, con cookie solo para /api/v1/sso.
	nav := e.cliente()
	nav.entrar(correo) // una sesión abierta antes del canje no sirve para vincular
	ticket := e.ticket("u-17", correoArgos, "orden", "55")
	r := canjear(nav, ticket)
	destino, _ := r.cuerpo["destino"].(map[string]any)
	externo, _ := r.cuerpo["externo"].(map[string]any)
	if r.status != 200 || r.cuerpo["estado"] != "vincular" || destino["ruta"] != "/ordenes/55" || externo["idExterno"] != "u-17" ||
		externo["correo"] != correoArgos || r.cuerpo["sesion"] != nil {
		t.Fatalf("canje sin vínculo: %d %s", r.status, r.crudo)
	}
	if c := r.h.Get("Set-Cookie"); !strings.Contains(c, identidad.CookieVinculacionSSO+"=") || !strings.Contains(c, "Path=/api/v1/sso") || !strings.Contains(c, "HttpOnly") {
		t.Fatalf("cookie de vinculación: %v", r.h.Values("Set-Cookie"))
	}
	// Con la sesión previa en el navegador, el actor del canje es anónimo.
	if e.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'sso.canjear' AND entrada->'auditoria'->>'resultado' = 'vincular'
		AND actor_tipo = 'anonimo' AND entrada->'auditoria'->>'usuario_externo' = 'u-17'`) != 1 {
		t.Fatal("canje sin vínculo con sesión previa: actor anónimo")
	}
	// Un solo uso.
	canjear(e.cliente(), ticket).problema(t, 401, "ticket_invalido")
	vincular(nav, "u-17").problema(t, 422, "sesion_anterior_al_canje")
	// Sin sesión: 401; sin cookie de vinculación (otro navegador): 422.
	vincular(e.cliente(), "u-17").problema(t, 401, "no_autenticado")
	otro := e.cliente()
	otro.entrar(correo)
	vincular(otro, "u-17").problema(t, 422, "vinculacion_invalida")
	// El mismo correo sin verificar por Argos no autovincula.
	sinVerificar := e.cliente()
	if r := canjear(sinVerificar, e.ticket("u-17", correoSinVerificar+correo, "ticket", "3")); r.cuerpo["estado"] != "vincular" ||
		r.cuerpo["externo"].(map[string]any)["correoVerificado"] != false {
		t.Fatalf("sin verificar: %s", r.crudo)
	}
	time.Sleep(10 * time.Millisecond)
	sinVerificar.entrar(correo)
	vincular(sinVerificar, "u-17").problema(t, 409, "vinculo_requiere_admin")
	// Tras iniciar sesión de nuevo, vincula solo la cuenta que se le mostró.
	time.Sleep(10 * time.Millisecond)
	nav.entrar(correo)
	vincular(nav, "u-99").problema(t, 422, "vinculacion_invalida")
	r = vincular(nav, "u-17")
	vinculo, _ := r.cuerpo["vinculo"].(map[string]any)
	if r.status != 200 || vinculo["usuarioExterno"] != "u-17" || vinculo["sistema"] != "argosisp" {
		t.Fatalf("vincular: %d %s", r.status, r.crudo)
	}
	vinculoID := int64(vinculo["id"].(float64))
	if !strings.Contains(r.h.Get("Set-Cookie"), "Max-Age=0") {
		t.Fatal("vincular borra la cookie")
	}
	vincular(nav, "u-17").problema(t, 422, "vinculacion_invalida")
	if e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'usuario.sso_vinculado' AND (payload->>'usuario_id')::bigint = $1
		AND payload->>'usuario_externo' = 'u-17' AND payload->>'origen' = 'usuario'`, ana) != 1 {
		t.Fatal("falta usuario.sso_vinculado")
	}

	// Con vínculo: entra con una sesión sso (8 h como máximo), sin CSRF.
	sso := e.cliente()
	r = canjear(sso, e.ticket("u-17", correoArgos, "prospecto", "9"))
	destino, _ = r.cuerpo["destino"].(map[string]any)
	if r.status != 200 || r.cuerpo["estado"] != "sesion" || destino["ruta"] != "/comercial/9" {
		t.Fatalf("canje con vínculo: %d %s", r.status, r.crudo)
	}
	if y := sso.pedir(http.MethodGet, "/api/v1/yo", nil); y.status != 200 || y.cuerpo["usuario"].(map[string]any)["id"] != float64(ana) {
		t.Fatalf("yo tras el autologin: %d %s", y.status, y.crudo)
	}
	if e.contar(`SELECT count(*) FROM nucleo.sesiones WHERE token_hash = $1 AND origen = 'sso'
		AND expira_en BETWEEN now() + interval '7 hours 59 minutes' AND now() + interval '8 hours 1 minute'`, identidad.Hash256(sso.cookie(identidad.CookieSesion).Value)) != 1 {
		t.Fatal("la sesión del autologin es de origen sso y dura 8 h")
	}
	// Mismo usuario: reutiliza la sesión.
	if r := canjear(sso, e.ticket("u-17", correoArgos, "ticket", "3")); r.status != 200 || r.cuerpo["estado"] != "sesion" || r.cuerpo["sesionReutilizada"] != true || r.cuerpo["sesion"] != nil {
		t.Fatalf("reutilizar: %d %s", r.status, r.crudo)
	}
	// Acciones sensibles: re-autenticación con contraseña; las acciones quedan marcadas sso.
	sso.pedir(http.MethodPost, "/api/v1/yo/mfa", nil).problema(t, 403, "reautenticacion_requerida")
	sso.pedir(http.MethodPost, "/api/v1/tokens", map[string]any{"nombre": "x"}).problema(t, 403, "reautenticacion_requerida")
	sso.pedir(http.MethodPost, "/api/v1/sesion/reautenticar", map[string]any{"contrasena": "otra-cosa-cualquiera"}).problema(t, 401, "credenciales_invalidas")
	if r := sso.pedir(http.MethodPost, "/api/v1/sesion/reautenticar", map[string]any{"contrasena": contrasena}); r.status != 200 {
		t.Fatalf("reautenticar: %d %s", r.status, r.crudo)
	}
	if r := sso.pedir(http.MethodPost, "/api/v1/yo/mfa", nil); r.status != 200 {
		t.Fatalf("mfa tras reautenticar: %d %s", r.status, r.crudo)
	}
	if e.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'mfa.configurar' AND actor_id = $1 AND entrada->'auditoria'->>'sesion_origen' = 'sso'`, ana) < 1 {
		t.Fatal("acción de una sesión sso anotada")
	}

	// Sesión de otro usuario en el navegador: confirmar antes de cambiarla (el ticket no se consume).
	beto, correoBeto := e.usuario()
	navBeto := e.cliente()
	navBeto.entrar(correoBeto)
	previa := navBeto.cookie(identidad.CookieSesion).Value
	tk := e.ticket("u-17", correoArgos, "ticket", "3")
	r = canjear(navBeto, tk)
	como, _ := r.cuerpo["entrarComo"].(map[string]any)
	if r.status != 200 || r.cuerpo["estado"] != "confirmar_cambio" || r.cuerpo["sesion"] != nil ||
		como["idExterno"] != "u-17" || como["nombreExterno"] != "Ana López" || !strings.HasPrefix(como["nombreIris"].(string), "Usuario ") {
		t.Fatalf("confirmar_cambio: %d %s", r.status, r.crudo)
	}
	if r := canjear(navBeto, tk, "confirmar", true); r.status != 200 || r.cuerpo["estado"] != "sesion" {
		t.Fatalf("canje confirmado: %d %s", r.status, r.crudo)
	}
	if e.contar(`SELECT count(*) FROM nucleo.sesiones WHERE token_hash = $1 AND revocada_en IS NOT NULL`, identidad.Hash256(previa)) != 1 {
		t.Fatal("la sesión del otro usuario se cierra al confirmar")
	}

	// Correo distinto: no se autovincula; lo vincula un administrador.
	canjeBeto := e.cliente()
	if r := canjear(canjeBeto, e.ticket("u-50", "otra@persona.gt", "ticket", "3")); r.cuerpo["estado"] != "vincular" {
		t.Fatalf("u-50: %s", r.crudo)
	}
	time.Sleep(10 * time.Millisecond)
	canjeBeto.entrar(correoBeto)
	vincular(canjeBeto, "u-50").problema(t, 409, "vinculo_requiere_admin")
	_, correoAdmin := e.usuario("admin")
	admin := e.cliente()
	admin.entrar(correoAdmin)
	betoVinculos := fmt.Sprintf("/api/v1/usuarios/%d/vinculos-sso", beto)
	canjeBeto.pedir(http.MethodPost, betoVinculos, map[string]any{"conector_id": e.conectorID, "id_externo": "u-50"}).problema(t, 403, "prohibido")
	admin.pedir(http.MethodPost, betoVinculos, map[string]any{"conector_id": e.conectorID, "id_externo": "u-17"}).problema(t, 409, "cuenta_externa_vinculada")
	if r := admin.pedir(http.MethodPost, betoVinculos, map[string]any{"conector_id": e.conectorID, "id_externo": "u-50"}); r.status != 201 {
		t.Fatalf("admin vincula: %d %s", r.status, r.crudo)
	}
	admin.pedir(http.MethodPost, betoVinculos, map[string]any{"conector_id": e.conectorID, "id_externo": "u-51"}).problema(t, 409, "vinculo_existente")
	if e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'usuario.sso_vinculado' AND (payload->>'usuario_id')::bigint = $1 AND payload->>'origen' = 'admin'`, beto) != 1 {
		t.Fatal("vínculo del admin con su evento")
	}

	// Ver y quitar vínculos: los propios sin permiso (con la sesión, no con un token); los de otro,
	// con usuarios.ver / usuarios.editar.
	propia := e.cliente()
	propia.entrar(correo)
	anaVinculos := fmt.Sprintf("/api/v1/usuarios/%d/vinculos-sso", ana)
	if r := propia.pedir(http.MethodGet, anaVinculos, nil); r.status != 200 || len(r.cuerpo["data"].([]any)) != 1 {
		t.Fatalf("ver propios: %d %s", r.status, r.crudo)
	}
	pat, _ := tokenPara(t, e.entorno, ana)
	pat.pedir(http.MethodPost, fmt.Sprintf("%s/%d/borrar", anaVinculos, vinculoID), map[string]any{}).problema(t, 403, "prohibido")
	canjeBeto.pedir(http.MethodGet, anaVinculos, nil).problema(t, 403, "prohibido")
	canjeBeto.pedir(http.MethodPost, fmt.Sprintf("%s/%d/borrar", anaVinculos, vinculoID), map[string]any{}).problema(t, 403, "prohibido")
	if r := admin.pedir(http.MethodGet, anaVinculos, nil); r.status != 200 || len(r.cuerpo["data"].([]any)) != 1 {
		t.Fatalf("admin ve: %d %s", r.status, r.crudo)
	}
	if r := admin.pedir(http.MethodPost, fmt.Sprintf("%s/%d/borrar", anaVinculos, vinculoID), map[string]any{"motivo": "cuenta compartida"}); r.status != 200 {
		t.Fatalf("admin borra: %d %s", r.status, r.crudo)
	}
	if e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'usuario.sso_desvinculado' AND (payload->>'vinculo_id')::bigint = $1`, vinculoID) != 1 {
		t.Fatal("falta usuario.sso_desvinculado")
	}
	propia.pedir(http.MethodPost, fmt.Sprintf("%s/%d/borrar", anaVinculos, vinculoID), map[string]any{}).problema(t, 404, "no_encontrado")
	// Sin vínculo, el siguiente vuelve a pedir vincular.
	if r := canjear(e.cliente(), e.ticket("u-17", correoArgos, "ticket", "3")); r.cuerpo["estado"] != "vincular" {
		t.Fatalf("tras quitar el vínculo: %s", r.crudo)
	}
}

// Barreras del login en el canje y vínculos que se borran al tomar la cuenta.
func TestSSOBarrerasYVinculosBorrados(t *testing.T) {
	e := nuevoEntornoSSO(t, identidad.Config{})
	ctx := pruebas.ContextoSistema(pruebas.Pool(t))

	// Privilegiado sin TOTP: 403 sso_requiere_mfa (auditado).
	admin, _ := e.usuario("admin")
	e.vincularCon("u-admin", admin)
	canjear(e.cliente(), e.ticket("u-admin", "", "ticket", "1")).problema(t, 403, "sso_requiere_mfa")

	// Bloqueada por fuerza bruta: 429 como el login.
	ana, correo := e.usuario("lectura")
	e.vincularCon("u-ana", ana)
	e.ejecutar(`INSERT INTO nucleo.intentos_login (correo, bloqueado_hasta) VALUES ($1, now() + interval '10 minutes')
		ON CONFLICT (correo) DO UPDATE SET bloqueado_hasta = EXCLUDED.bloqueado_hasta`, correo)
	canjear(e.cliente(), e.ticket("u-ana", "", "ticket", "1")).problema(t, 429, "demasiados_intentos")
	e.ejecutar(`DELETE FROM nucleo.intentos_login WHERE correo = $1`, correo)
	// Sin contraseña: 403.
	e.ejecutar(`UPDATE nucleo.usuarios SET contrasena_hash = NULL WHERE id = $1`, ana)
	canjear(e.cliente(), e.ticket("u-ana", "", "ticket", "1")).problema(t, 403, "prohibido")

	// Desactivar al usuario o cambiarle la contraseña (un admin) borra sus vínculos con su evento.
	beto, _ := e.usuario("lectura")
	e.vincularCon("u-beto", beto)
	u, err := e.mod.VerUsuario.Ejecutar(ctx, identidad.EntradaVerUsuario{ID: beto})
	if err != nil {
		t.Fatal(err)
	}
	nueva := "otra-contrasena-larga-123"
	if _, err := e.mod.EditarUsuario.Ejecutar(ctx, identidad.EntradaEditarUsuario{ID: beto, Version: u.Version, Contrasena: &nueva}); err != nil {
		t.Fatal(err)
	}
	carla, _ := e.usuario("lectura")
	e.vincularCon("u-carla", carla)
	u, err = e.mod.VerUsuario.Ejecutar(ctx, identidad.EntradaVerUsuario{ID: carla})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.mod.DesactivarUsuario.Ejecutar(ctx, identidad.EntradaDesactivarUsuario{ID: carla, Version: u.Version, Motivo: "baja"}); err != nil {
		t.Fatal(err)
	}
	dani, _ := e.usuario("lectura")
	e.vincularCon("u-dani", dani)
	u, err = e.mod.VerUsuario.Ejecutar(ctx, identidad.EntradaVerUsuario{ID: dani})
	if err != nil {
		t.Fatal(err)
	}
	otroCorreo := pruebas.Correo("nuevo")
	if _, err := e.mod.EditarUsuario.Ejecutar(ctx, identidad.EntradaEditarUsuario{ID: dani, Version: u.Version, Correo: &otroCorreo}); err != nil {
		t.Fatal(err)
	}
	for usuario, motivo := range map[int64]string{beto: "contrasena_reiniciada", carla: "usuario_desactivado", dani: "correo_cambiado"} {
		if e.contar(`SELECT count(*) FROM nucleo.sso_vinculos WHERE usuario_id = $1`, usuario) != 0 ||
			e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'usuario.sso_desvinculado' AND (payload->>'usuario_id')::bigint = $1
				AND payload->>'motivo' = $2`, usuario, motivo) != 1 {
			t.Fatalf("vínculos de %d borrados con motivo %s", usuario, motivo)
		}
	}
}

func TestSSOConMFAVencidoYConectorApagado(t *testing.T) {
	e := nuevoEntornoSSO(t, identidad.Config{})
	id, correo := e.usuario("lectura")
	conMFA(t, e.entorno, correo)
	e.vincularCon("u-5", id)
	// Con MFA, la sesión queda pendiente como en el login.
	nav := e.cliente()
	r := canjear(nav, e.ticket("u-5", "", "ticket", "8"))
	sesion, _ := r.cuerpo["sesion"].(map[string]any)
	if r.status != 200 || r.cuerpo["estado"] != "mfa" || sesion["mfaRequerido"] != true || sesion["usuario"] != nil {
		t.Fatalf("canje con MFA: %d %s", r.status, r.crudo)
	}
	nav.pedir(http.MethodGet, "/api/v1/yo", nil).problema(t, 401, "no_autenticado")

	// Vencido: 401, sin fila de auditoría (anónimo).
	ticket := e.ticket("u-5", "", "ticket", "8")
	e.ejecutar(`UPDATE nucleo.sso_tickets SET expira_en = now() - interval '1 second' WHERE ticket_hash = $1`, identidad.Hash256(ticket))
	antes := e.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'sso.canjear'`)
	rechazos := e.mod.RechazosSSOSinAuditar()
	canjear(e.cliente(), ticket).problema(t, 401, "ticket_invalido")
	canjear(e.cliente(), "no-es-un-ticket-valido-0123456789").problema(t, 401, "ticket_invalido")
	if e.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'sso.canjear'`) != antes || e.mod.RechazosSSOSinAuditar() != rechazos+2 {
		t.Fatal("los canjes fallidos anónimos no se auditan y se cuentan aparte")
	}

	// Cuenta vinculada desactivada: 403 (auditado).
	e.ejecutar(`UPDATE nucleo.usuarios SET activo = false WHERE id = $1`, id)
	canjear(e.cliente(), e.ticket("u-5", "", "ticket", "8")).problema(t, 403, "prohibido")
	e.ejecutar(`UPDATE nucleo.usuarios SET activo = true WHERE id = $1`, id)

	// Conector apagado: no se emite ni se canjea lo emitido antes.
	pendiente := e.ticket("u-5", "", "ticket", "8")
	e.ejecutar(`UPDATE integracion.conectores SET estado = 'apagado' WHERE id = $1`, e.conectorID)
	e.emitir(e.argos, "u-5", "", "ticket", "8").problema(t, 409, "conector_apagado")
	canjear(e.cliente(), pendiente).problema(t, 409, "conector_apagado")

	// La purga borra los tickets vencidos (los vínculos quedan).
	e.ejecutar(`UPDATE nucleo.sso_tickets SET expira_en = now() - interval '40 days'`)
	res, err := identidad.PurgarPortal(context.Background(), pruebas.Pool(t), time.Now(), identidad.RetencionPortal, 100)
	if err != nil || res.TicketsSSO < 3 || e.contar(`SELECT count(*) FROM nucleo.sso_tickets WHERE conector_id = $1`, e.conectorID) != 0 ||
		e.contar(`SELECT count(*) FROM nucleo.sso_vinculos WHERE conector_id = $1`, e.conectorID) != 1 {
		t.Fatalf("purga: %+v %v", res, err)
	}
}

// El tope de emisión aguanta pedidos simultáneos (la emisión del conector se serializa).
func TestSSOTopeConConcurrencia(t *testing.T) {
	e := nuevoEntornoSSO(t, identidad.Config{SSOTopeEmision: 5})
	const n = 12
	estados := make(chan int, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			estados <- e.emitir(e.argos, fmt.Sprintf("u-%d", i), "", "ticket", "1").status
		}()
	}
	wg.Wait()
	close(estados)
	cuenta := map[int]int{}
	for s := range estados {
		cuenta[s]++
	}
	if cuenta[201] != 5 || cuenta[429] != n-5 {
		t.Fatalf("emitidos con el tope de 5: %v", cuenta)
	}
}

// Los fallos de reautenticar desde una sesión del autologin no bloquean el login con contraseña.
func TestSSOReautenticarConContadorPropio(t *testing.T) {
	e := nuevoEntornoSSO(t, identidad.Config{LoginMaxFallos: 2})
	id, correo := e.usuario("lectura")
	e.vincularCon("u-r", id)
	sso := e.cliente()
	if r := canjear(sso, e.ticket("u-r", "", "ticket", "1")); r.cuerpo["estado"] != "sesion" {
		t.Fatalf("canje: %s", r.crudo)
	}
	for range 2 {
		sso.pedir(http.MethodPost, "/api/v1/sesion/reautenticar", map[string]any{"contrasena": "mala-mala-mala"}).problema(t, 401, "credenciales_invalidas")
	}
	sso.pedir(http.MethodPost, "/api/v1/sesion/reautenticar", map[string]any{"contrasena": contrasena}).problema(t, 429, "demasiados_intentos")
	if r := e.cliente().entrar(correo); r.status != 200 {
		t.Fatalf("el login con contraseña sigue: %d %s", r.status, r.crudo)
	}
}
