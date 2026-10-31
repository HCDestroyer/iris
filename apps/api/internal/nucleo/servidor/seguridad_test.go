package servidor_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// Regresión: un token con solo tokens.crear no puede emitir un token con más permisos (ni uno
// sin límite, que tendría todos los del usuario).
func TestTokenDesdeTokenNoEscala(t *testing.T) {
	e := nuevoEntorno(t)
	idAdmin, correoAdmin := e.usuario("admin")
	admin := e.cliente()
	admin.entrar(correoAdmin)

	// Un token limitado a usuarios.editar no cambia la contraseña de su propio usuario (con eso
	// entraría con la sesión completa).
	r := admin.pedir(http.MethodPost, "/api/v1/tokens", map[string]any{"nombre": "editar", "permisos": []string{"usuarios.editar"}})
	editor := e.cliente()
	editor.bearer = r.cuerpo["valor"].(string)
	editor.pedir(http.MethodPost, fmt.Sprintf("/api/v1/usuarios/%d/editar", idAdmin), map[string]any{"version": 1, "contrasena": "tomada-por-token-123"}).problema(t, 403, "prohibido")

	r = admin.pedir(http.MethodPost, "/api/v1/tokens", map[string]any{"nombre": "solo-crear", "permisos": []string{"tokens.crear"}})
	if r.status != 201 {
		t.Fatalf("crear token limitado: %d %s", r.status, r.crudo)
	}
	lim := e.cliente()
	lim.bearer = r.cuerpo["valor"].(string)

	lim.pedir(http.MethodPost, "/api/v1/tokens", map[string]any{"nombre": "sin-limite"}).problema(t, 403, "prohibido")
	lim.pedir(http.MethodPost, "/api/v1/tokens", map[string]any{"nombre": "mas", "permisos": []string{"usuarios.ver"}}).problema(t, 403, "prohibido")
	lim.pedir(http.MethodPost, "/api/v1/tokens", map[string]any{"nombre": "mas", "permisos": []string{"tokens.crear", "usuarios.editar"}}).problema(t, 403, "prohibido")

	// Un subconjunto de lo que tiene el token sí.
	r = lim.pedir(http.MethodPost, "/api/v1/tokens", map[string]any{"nombre": "igual", "permisos": []string{"tokens.crear"}})
	if r.status != 201 {
		t.Fatalf("token hijo con el mismo límite: %d %s", r.status, r.crudo)
	}
	hijo := e.cliente()
	hijo.bearer = r.cuerpo["valor"].(string)
	if yo := hijo.pedir(http.MethodGet, "/api/v1/yo", nil); fmt.Sprint(yo.cuerpo["permisos"]) != "[tokens.crear]" {
		t.Fatalf("permisos del hijo: %s", yo.crudo)
	}

	// Un token sin límite puede crear otro sin límite (mismos permisos efectivos).
	r = admin.pedir(http.MethodPost, "/api/v1/tokens", map[string]any{"nombre": "completo"})
	if r.status != 201 {
		t.Fatalf("token sin límite: %d %s", r.status, r.crudo)
	}
	completo := e.cliente()
	completo.bearer = r.cuerpo["valor"].(string)
	if r := completo.pedir(http.MethodPost, "/api/v1/tokens", map[string]any{"nombre": "otro-completo"}); r.status != 201 {
		t.Fatalf("token sin límite desde token sin límite: %d %s", r.status, r.crudo)
	}
}

// Regresión: un admin con alcance de zona no toma la cuenta de un admin global.
func TestAdminZonalNoTomaAdminGlobal(t *testing.T) {
	e := nuevoEntorno(t)
	idGlobal, correoGlobal := e.usuario("admin")
	zona := int64(5)
	correoZ := pruebas.Correo("zonal")
	if _, err := e.mod.CrearUsuario.Ejecutar(pruebas.ContextoSistema(pruebas.Pool(t)), identidad.EntradaCrearUsuario{
		Nombre: "Zonal", Correo: correoZ, Contrasena: contrasena, Roles: []identidad.AsignacionRol{{Rol: "admin", ZonaID: &zona}},
	}); err != nil {
		t.Fatal(err)
	}
	z := e.cliente()
	z.entrar(correoZ)
	ruta := fmt.Sprintf("/api/v1/usuarios/%d/editar", idGlobal)

	nueva := "tomada-por-zonal-123"
	z.pedir(http.MethodPost, ruta, map[string]any{"version": 1, "contrasena": nueva}).problema(t, 403, "prohibido")
	e.cliente().pedir(http.MethodPost, "/api/v1/sesion", map[string]any{"correo": correoGlobal, "contrasena": nueva}).problema(t, 401, "credenciales_invalidas")
	z.pedir(http.MethodPost, ruta, map[string]any{"version": 1, "correo": pruebas.Correo("robado")}).problema(t, 403, "prohibido")
	z.pedir(http.MethodPost, ruta, map[string]any{"version": 1, "roles": []any{}}).problema(t, 403, "prohibido")
	z.pedir(http.MethodPost, fmt.Sprintf("/api/v1/usuarios/%d/desactivar", idGlobal), map[string]any{"version": 1, "motivo": "x"}).problema(t, 403, "prohibido")
	if r := e.cliente().entrar(correoGlobal); r.status != 200 {
		t.Fatalf("el admin global sigue entrando: %d %s", r.status, r.crudo)
	}

	// El admin global sí puede (tiene sin límite todo lo del objetivo).
	idLectura, _ := e.usuario("lectura")
	g := e.cliente()
	g.entrar(correoGlobal)
	if r := g.pedir(http.MethodPost, fmt.Sprintf("/api/v1/usuarios/%d/editar", idLectura), map[string]any{"version": 1, "contrasena": nueva, "roles": []any{}}); r.status != 200 {
		t.Fatalf("admin global edita a lectura: %d %s", r.status, r.crudo)
	}
}

// Regresión: los tokens de otro usuario solo se emiten para usuarios de integración, y quien
// los emite debe tener sin límite todo lo que el token podrá hacer.
func TestTokenParaOtroSoloIntegracion(t *testing.T) {
	e := nuevoEntorno(t)
	_, correoAdmin := e.usuario("admin")
	idPersona, _ := e.usuario("lectura")
	admin := e.cliente()
	admin.entrar(correoAdmin)

	admin.pedir(http.MethodPost, "/api/v1/tokens", map[string]any{"nombre": "suplantar", "usuarioId": idPersona}).problema(t, 422, "token_para_persona")

	// Dar el rol admin (permisos sensibles) exige haber confirmado la contraseña.
	if r := admin.pedir(http.MethodPost, "/api/v1/sesion/reautenticar", map[string]any{"contrasena": contrasena}); r.status != 200 {
		t.Fatalf("reautenticar: %d %s", r.status, r.crudo)
	}
	r := admin.pedir(http.MethodPost, "/api/v1/usuarios", map[string]any{"nombre": "Horus", "correo": pruebas.Correo("horus"), "tipo": "integracion", "roles": []map[string]any{{"rol": "integracion_horus"}}})
	if r.status != 201 {
		t.Fatalf("crear integración: %d %s", r.status, r.crudo)
	}
	integ := r.cuerpo["id"]

	// Un admin de zona no emite tokens de una integración con permisos globales.
	zona := int64(7)
	correoZ := pruebas.Correo("zonal")
	if _, err := e.mod.CrearUsuario.Ejecutar(pruebas.ContextoSistema(pruebas.Pool(t)), identidad.EntradaCrearUsuario{
		Nombre: "Zonal", Correo: correoZ, Contrasena: contrasena, Roles: []identidad.AsignacionRol{{Rol: "admin", ZonaID: &zona}},
	}); err != nil {
		t.Fatal(err)
	}
	z := e.cliente()
	z.entrar(correoZ)
	z.pedir(http.MethodPost, "/api/v1/tokens", map[string]any{"nombre": "horus", "usuarioId": integ}).problema(t, 403, "prohibido")

	if r := admin.pedir(http.MethodPost, "/api/v1/tokens", map[string]any{"nombre": "horus", "usuarioId": integ}); r.status != 201 {
		t.Fatalf("token de integración por el admin global: %d %s", r.status, r.crudo)
	}
}

// Regresión: 10 inicios de sesión fallidos bloquean el correo 15 min (429 con Retry-After,
// también con la contraseña correcta); el bloqueo queda auditado; un correo que no existe se
// comporta igual; entrar bien reinicia la cuenta.
func TestLoginSeBloqueaTrasFallos(t *testing.T) {
	e := nuevoEntorno(t)
	id, correo := e.usuario("lectura")
	c := e.cliente()
	malo := map[string]any{"correo": correo, "contrasena": "mala-contrasena-123"}
	for i := range 10 {
		r := c.pedir(http.MethodPost, "/api/v1/sesion", malo)
		if r.status != 401 {
			t.Fatalf("fallo %d: %d %s", i+1, r.status, r.crudo)
		}
	}
	r := c.entrar(correo)
	r.problema(t, 429, "demasiados_intentos")
	if r.h.Get("Retry-After") == "" {
		t.Fatal("falta Retry-After")
	}
	var bloqueos int
	if err := pruebas.Pool(t).QueryRow(context.Background(),
		`SELECT count(*) FROM auditoria.acciones WHERE accion = 'sesion.iniciar' AND entidad_id = $1
		   AND resultado = 'error' AND entrada->'auditoria' ? 'bloqueado_hasta'`, fmt.Sprint(id)).Scan(&bloqueos); err != nil || bloqueos != 1 {
		t.Fatalf("auditoría del bloqueo: %d %v", bloqueos, err)
	}

	// Un correo inexistente se bloquea igual: el 429 no revela si existe.
	nadie := map[string]any{"correo": pruebas.Correo("nadie"), "contrasena": "x"}
	for range 10 {
		c.pedir(http.MethodPost, "/api/v1/sesion", nadie).problema(t, 401, "credenciales_invalidas")
	}
	c.pedir(http.MethodPost, "/api/v1/sesion", nadie).problema(t, 429, "demasiados_intentos")

	// Vencido el bloqueo, entra; y entrar bien reinicia el contador.
	if _, err := pruebas.Pool(t).Exec(context.Background(),
		`UPDATE nucleo.intentos_login SET bloqueado_hasta = now() - interval '1 second' WHERE correo = $1`, correo); err != nil {
		t.Fatal(err)
	}
	for range 9 {
		c.pedir(http.MethodPost, "/api/v1/sesion", malo).problema(t, 401, "credenciales_invalidas")
	}
	if r := c.entrar(correo); r.status != 200 {
		t.Fatalf("tras el bloqueo: %d %s", r.status, r.crudo)
	}
	for range 9 {
		c.pedir(http.MethodPost, "/api/v1/sesion", malo).problema(t, 401, "credenciales_invalidas")
	}
	if r := e.cliente().entrar(correo); r.status != 200 {
		t.Fatalf("el login exitoso debía reiniciar el contador: %d %s", r.status, r.crudo)
	}
}

// Regresión: 5 códigos MFA inválidos revocan la sesión pendiente; ni un código válido la revive.
func TestMFASeRevocaTrasFallos(t *testing.T) {
	e := nuevoEntorno(t)
	_, correo := e.usuario("lectura")
	c := e.cliente()
	c.entrar(correo)
	secreto, _ := c.pedir(http.MethodPost, "/api/v1/yo/mfa", nil).cuerpo["secreto"].(string)
	ahora, _ := totp.GenerateCode(secreto, time.Now())
	if r := c.pedir(http.MethodPost, "/api/v1/yo/mfa/activar", map[string]any{"codigo": ahora}); r.status != 200 {
		t.Fatalf("activar: %d %s", r.status, r.crudo)
	}

	p := e.cliente()
	if r := p.entrar(correo); r.cuerpo["mfaRequerido"] != true {
		t.Fatalf("login con MFA: %s", r.crudo)
	}
	p.csrf = p.cookie(identidad.CookieCSRF).Value
	for range 4 {
		p.pedir(http.MethodPost, "/api/v1/sesion/mfa", map[string]any{"codigo": "000000"}).problema(t, 401, "codigo_invalido")
	}
	p.pedir(http.MethodPost, "/api/v1/sesion/mfa", map[string]any{"codigo": "000000"}).problema(t, 401, "mfa_bloqueado")
	siguiente, _ := totp.GenerateCode(secreto, time.Now().Add(30*time.Second))
	p.pedir(http.MethodPost, "/api/v1/sesion/mfa", map[string]any{"codigo": siguiente}).problema(t, 401, "no_autenticado")

	// Con un login nuevo el contador empieza de cero.
	n := e.cliente()
	n.entrar(correo)
	n.csrf = n.cookie(identidad.CookieCSRF).Value
	if r := n.pedir(http.MethodPost, "/api/v1/sesion/mfa", map[string]any{"codigo": siguiente}); r.status != 200 {
		t.Fatalf("MFA tras volver a entrar: %d %s", r.status, r.crudo)
	}
}

// Regresión: el valor de un token y el secreto TOTP no quedan en nucleo.idempotencia; repetir la
// clave responde 409 idempotencia_no_repetible. Las claves de los tokens son por token.
func TestIdempotenciaNoGuardaSecretos(t *testing.T) {
	e := nuevoEntorno(t)
	_, correoAdmin := e.usuario("admin")
	admin := e.cliente()
	admin.entrar(correoAdmin)
	pool := pruebas.Pool(t)
	ctx := context.Background()
	guardado := func(clave string) (estado string, respuesta []byte) {
		t.Helper()
		if err := pool.QueryRow(ctx, `SELECT estado, respuesta FROM nucleo.idempotencia WHERE clave = $1`, clave).Scan(&estado, &respuesta); err != nil {
			t.Fatal(err)
		}
		return estado, respuesta
	}

	nombre := "idem-" + pruebas.Unico()
	clave := "tok-" + pruebas.Unico()
	r := admin.pedir(http.MethodPost, "/api/v1/tokens", map[string]any{"nombre": nombre}, nucleo.CabeceraIdempotencia, clave)
	if r.status != 201 {
		t.Fatalf("crear token: %d %s", r.status, r.crudo)
	}
	if estado, resp := guardado(clave); estado != "no_repetible" || resp != nil || strings.Contains(string(resp), r.cuerpo["valor"].(string)) {
		t.Fatalf("idempotencia guardó el token: %s %q", estado, resp)
	}
	admin.pedir(http.MethodPost, "/api/v1/tokens", map[string]any{"nombre": nombre}, nucleo.CabeceraIdempotencia, clave).problema(t, 409, "idempotencia_no_repetible")
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM nucleo.tokens_api WHERE nombre = $1`, nombre).Scan(&n); err != nil || n != 1 {
		t.Fatalf("la repetición no crea otro token: %d %v", n, err)
	}

	claveMFA := "mfa-" + pruebas.Unico()
	r = admin.pedir(http.MethodPost, "/api/v1/yo/mfa", nil, nucleo.CabeceraIdempotencia, claveMFA)
	if r.status != 200 || r.cuerpo["secreto"] == "" {
		t.Fatalf("configurar MFA: %d %s", r.status, r.crudo)
	}
	if estado, resp := guardado(claveMFA); estado != "no_repetible" || resp != nil {
		t.Fatalf("idempotencia guardó el secreto TOTP: %s %q", estado, resp)
	}
	admin.pedir(http.MethodPost, "/api/v1/yo/mfa", nil, nucleo.CabeceraIdempotencia, claveMFA).problema(t, 409, "idempotencia_no_repetible")

	// Un error no lleva secreto: se guarda y se repite igual.
	idPersona, _ := e.usuario("lectura")
	claveError := "err-" + pruebas.Unico()
	admin.pedir(http.MethodPost, "/api/v1/tokens", map[string]any{"nombre": "x", "usuarioId": idPersona}, nucleo.CabeceraIdempotencia, claveError).problema(t, 422, "token_para_persona")
	rep := admin.pedir(http.MethodPost, "/api/v1/tokens", map[string]any{"nombre": "x", "usuarioId": idPersona}, nucleo.CabeceraIdempotencia, claveError)
	rep.problema(t, 422, "token_para_persona")
	if rep.h.Get(nucleo.CabeceraRepetida) != "true" {
		t.Fatal("el error se repite desde idempotencia")
	}

	// Dos tokens del mismo usuario no comparten claves.
	tokens := make([]*cliente, 2)
	for i := range tokens {
		r := admin.pedir(http.MethodPost, "/api/v1/tokens", map[string]any{"nombre": fmt.Sprintf("api-%d", i)})
		tokens[i] = e.cliente()
		tokens[i].bearer = r.cuerpo["valor"].(string)
	}
	compartida := "comp-" + pruebas.Unico()
	for i, c := range tokens {
		r := c.pedir(http.MethodPost, "/api/v1/usuarios", map[string]any{"nombre": "Por token", "correo": pruebas.Correo(fmt.Sprint("tk", i)), "contrasena": contrasena}, nucleo.CabeceraIdempotencia, compartida)
		if r.status != 201 || r.h.Get(nucleo.CabeceraRepetida) != "" {
			t.Fatalf("token %d con la misma clave: %d %s", i, r.status, r.crudo)
		}
	}
	if err := pool.QueryRow(ctx, `SELECT count(DISTINCT actor) FROM nucleo.idempotencia WHERE clave = $1 AND actor LIKE 'token:%'`, compartida).Scan(&n); err != nil || n != 2 {
		t.Fatalf("claves por token: %d %v", n, err)
	}
}

// Regresión: una clave que quedó en_proceso (el proceso cayó antes de guardar la respuesta) se
// retoma pasado el tiempo de reclamo; antes, 409 idempotencia_en_proceso.
func TestIdempotenciaRetomaClaveAbandonada(t *testing.T) {
	e := nuevoEntorno(t)
	idAdmin, correoAdmin := e.usuario("admin")
	admin := e.cliente()
	admin.entrar(correoAdmin)
	pool := pruebas.Pool(t)
	ctx := context.Background()

	cuerpo := map[string]any{"nombre": "Retomado", "correo": pruebas.Correo("ret"), "contrasena": contrasena}
	crudo, _ := json.Marshal(cuerpo)
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "%s\n%s\n", http.MethodPost, "/api/v1/usuarios")
	h.Write(crudo)
	clave := "aband-" + pruebas.Unico()
	if _, err := pool.Exec(ctx, `INSERT INTO nucleo.idempotencia (actor, clave, metodo, ruta, hash_peticion, hash_formato, expira_en, updated_at)
		VALUES ($1, $2, 'POST', '/api/v1/usuarios', $3, 0, now() + interval '1 day', now() - interval '10 seconds')`,
		fmt.Sprintf("usuario:%d", idAdmin), clave, h.Sum(nil)); err != nil {
		t.Fatal(err)
	}
	admin.pedir(http.MethodPost, "/api/v1/usuarios", cuerpo, nucleo.CabeceraIdempotencia, clave).problema(t, 409, "idempotencia_en_proceso")

	if _, err := pool.Exec(ctx, `UPDATE nucleo.idempotencia SET updated_at = now() - interval '3 minutes' WHERE clave = $1`, clave); err != nil {
		t.Fatal(err)
	}
	// Otra petición con la misma clave no la retoma.
	otro := map[string]any{"nombre": "Otro", "correo": pruebas.Correo("ret"), "contrasena": contrasena}
	admin.pedir(http.MethodPost, "/api/v1/usuarios", otro, nucleo.CabeceraIdempotencia, clave).problema(t, 422, "idempotencia_reutilizada")

	r := admin.pedir(http.MethodPost, "/api/v1/usuarios", cuerpo, nucleo.CabeceraIdempotencia, clave)
	if r.status != 201 || r.h.Get(nucleo.CabeceraRepetida) != "" {
		t.Fatalf("retomar la clave: %d %s", r.status, r.crudo)
	}
	rep := admin.pedir(http.MethodPost, "/api/v1/usuarios", cuerpo, nucleo.CabeceraIdempotencia, clave)
	if rep.status != 201 || rep.h.Get(nucleo.CabeceraRepetida) != "true" || !bytes.Equal(rep.crudo, r.crudo) {
		t.Fatalf("tras retomarla, se repite: %d %s", rep.status, rep.crudo)
	}
}
