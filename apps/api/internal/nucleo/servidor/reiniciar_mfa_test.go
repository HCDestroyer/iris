package servidor_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

const temporal = "temporal-entregada-en-mano-42"

// conMFA entra con el usuario, configura y activa su MFA y devuelve la sesión (ya completa),
// el secreto y la versión del usuario tras activarlo.
func conMFA(t *testing.T, e *entorno, correo string) (*cliente, string, int) {
	t.Helper()
	c := e.cliente()
	if r := c.entrar(correo); r.status != 200 {
		t.Fatalf("entrar: %d %s", r.status, r.crudo)
	}
	r := c.pedir(http.MethodPost, "/api/v1/yo/mfa", nil)
	secreto, _ := r.cuerpo["secreto"].(string)
	if r.status != 200 || secreto == "" {
		t.Fatalf("configurar: %d %s", r.status, r.crudo)
	}
	codigo, _ := totp.GenerateCode(secreto, time.Now())
	r = c.pedir(http.MethodPost, "/api/v1/yo/mfa/activar", map[string]any{"codigo": codigo})
	if r.status != 200 || r.cuerpo["mfaActivo"] != true {
		t.Fatalf("activar: %d %s", r.status, r.crudo)
	}
	return c, secreto, int(r.cuerpo["version"].(float64))
}

func reiniciarMFA(c *cliente, id int64, version int, extra ...any) respuesta {
	cuerpo := map[string]any{"version": version, "motivo": "perdió el teléfono", "contrasenaTemporal": temporal}
	for i := 0; i+1 < len(extra); i += 2 {
		cuerpo[extra[i].(string)] = extra[i+1]
	}
	return c.pedir(http.MethodPost, fmt.Sprintf("/api/v1/usuarios/%d/reiniciar-mfa", id), cuerpo)
}

// tokenPara crea un token iris_pat_ del usuario directo en la base y devuelve un cliente Bearer.
func tokenPara(t *testing.T, e *entorno, usuarioID int64) (*cliente, int64) {
	t.Helper()
	valor, err := identidad.NuevoTokenAPI()
	if err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := pruebas.Pool(t).QueryRow(context.Background(), `INSERT INTO nucleo.tokens_api (usuario_id, nombre, prefijo, hash)
		VALUES ($1, $2, $3, $4) RETURNING id`, usuarioID, "script "+pruebas.Unico(), valor[:13], identidad.Hash256(valor)).Scan(&id); err != nil {
		t.Fatal(err)
	}
	c := e.cliente()
	c.bearer = valor
	return c, id
}

// entrarCon entra con una contraseña dada (sin MFA) y devuelve la respuesta.
func entrarCon(c *cliente, correo, clave string) respuesta {
	r := c.pedir(http.MethodPost, "/api/v1/sesion", map[string]any{"correo": correo, "contrasena": clave})
	if r.status == http.StatusOK {
		c.csrf, _ = r.cuerpo["csrfToken"].(string)
	}
	return r
}

func TestReiniciarMFA(t *testing.T) {
	e := nuevoEntorno(t)
	pool := pruebas.Pool(t)
	ctx := context.Background()
	idAdmin, correoAdmin := e.usuario("admin")
	_, correoSoporte := e.usuario("soporte")
	idU, correoU := e.usuario("lectura")

	sesionU, _, version := conMFA(t, e, correoU)
	// Otra sesión a medio entrar (pendiente del código): también se cierra.
	pendiente := e.cliente()
	if r := pendiente.entrar(correoU); r.cuerpo["mfaRequerido"] != true {
		t.Fatalf("login con MFA: %d %s", r.status, r.crudo)
	}
	bot, idToken := tokenPara(t, e, idU)
	if r := bot.pedir(http.MethodGet, "/api/v1/yo", nil); r.status != 200 {
		t.Fatalf("token antes: %d %s", r.status, r.crudo)
	}

	// Sin el permiso: 403 (soporte tiene usuarios.ver, no usuarios.reiniciar_mfa).
	sop := e.cliente()
	sop.entrar(correoSoporte)
	reiniciarMFA(sop, idU, version).problema(t, 403, "prohibido")

	admin, _, versionAdmin := conMFA(t, e, correoAdmin)
	// El propio: no por esta vía.
	reiniciarMFA(admin, idAdmin, versionAdmin).problema(t, 422, "reinicio_mfa_propio")
	// Versión vieja: 409; inexistente: 404; sin motivo: 422.
	reiniciarMFA(admin, idU, version-1).problema(t, 409, "conflicto_version")
	reiniciarMFA(admin, 999999999, 1).problema(t, 404, "no_encontrado")
	admin.pedir(http.MethodPost, fmt.Sprintf("/api/v1/usuarios/%d/reiniciar-mfa", idU), map[string]any{"version": version, "contrasenaTemporal": temporal}).problema(t, 422, "validacion")
	// Contraseña temporal: misma política (12+) y distinta de la actual.
	reiniciarMFA(admin, idU, version, "contrasenaTemporal", "corta").problema(t, 422, "validacion")
	reiniciarMFA(admin, idU, version, "contrasenaTemporal", contrasena).problema(t, 422, "validacion")

	r := reiniciarMFA(admin, idU, version)
	if r.status != 200 || r.cuerpo["mfaActivo"] != false || r.cuerpo["cambioContrasenaRequerido"] != true || int(r.cuerpo["version"].(float64)) != version+1 {
		t.Fatalf("reiniciar: %d %s", r.status, r.crudo)
	}
	// Los mismos contadores que el evento: la sesión completa y la pendiente, y su token.
	if r.cuerpo["sesionesCerradas"] != float64(2) || r.cuerpo["tokensRevocados"] != float64(1) || r.cuerpo["id"] != float64(idU) {
		t.Fatalf("contadores: %s", r.crudo)
	}
	// Sus sesiones (completa y pendiente) y sus tokens quedaron cerrados.
	sesionU.pedir(http.MethodGet, "/api/v1/yo", nil).problema(t, 401, "no_autenticado")
	pendiente.pedir(http.MethodPost, "/api/v1/sesion/mfa", map[string]any{"codigo": "123456"}).problema(t, 401, "no_autenticado")
	bot.pedir(http.MethodGet, "/api/v1/yo", nil).problema(t, 401, "no_autenticado")
	var abiertas int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM nucleo.sesiones WHERE usuario_id = $1 AND revocada_en IS NULL`, idU).Scan(&abiertas); err != nil || abiertas != 0 {
		t.Fatalf("sesiones abiertas: %d %v", abiertas, err)
	}
	var secretoNulo bool
	if err := pool.QueryRow(ctx, `SELECT totp_secreto IS NULL AND NOT totp_activo AND totp_ultimo_paso = 0 AND debe_cambiar_contrasena
		FROM nucleo.usuarios WHERE id = $1`, idU).Scan(&secretoNulo); err != nil || !secretoNulo {
		t.Fatalf("secreto borrado y cambio pendiente: %v %v", secretoNulo, err)
	}

	// Escenario del hallazgo: quien solo tiene la contraseña vieja ya no entra.
	entrarCon(e.cliente(), correoU, contrasena).problema(t, 401, "credenciales_invalidas")

	// Con la temporal entra, pero hasta cambiarla solo puede cambiarla, ver /yo o salir.
	nueva := e.cliente()
	if r := entrarCon(nueva, correoU, temporal); r.status != 200 || r.cuerpo["mfaRequerido"] != false ||
		r.cuerpo["usuario"].(map[string]any)["cambioContrasenaRequerido"] != true {
		t.Fatalf("entrar con la temporal: %d %s", r.status, r.crudo)
	}
	if r := nueva.pedir(http.MethodGet, "/api/v1/yo", nil); r.status != 200 || r.cuerpo["usuario"].(map[string]any)["cambioContrasenaRequerido"] != true {
		t.Fatalf("yo con la temporal: %d %s", r.status, r.crudo)
	}
	nueva.pedir(http.MethodPost, "/api/v1/yo/mfa", nil).problema(t, 403, "cambio_contrasena_requerido")
	nueva.pedir(http.MethodGet, "/api/v1/usuarios", nil).problema(t, 403, "cambio_contrasena_requerido")
	nueva.pedir(http.MethodPost, "/api/v1/tokens", map[string]any{"nombre": "x"}).problema(t, 403, "cambio_contrasena_requerido")
	// Cambiarla: exige la actual (la temporal), 12+ y distinta.
	nueva.pedir(http.MethodPost, identidad.RutaCambiarContrasena, map[string]any{"actual": "no-es-la-actual-123", "nueva": "nueva-del-usuario-777"}).problema(t, 422, "validacion")
	nueva.pedir(http.MethodPost, identidad.RutaCambiarContrasena, map[string]any{"actual": temporal, "nueva": "corta"}).problema(t, 422, "validacion")
	nueva.pedir(http.MethodPost, identidad.RutaCambiarContrasena, map[string]any{"actual": temporal, "nueva": temporal}).problema(t, 422, "validacion")
	otra := e.cliente() // otra sesión con la temporal: se cierra al cambiarla
	entrarCon(otra, correoU, temporal)
	if r := nueva.pedir(http.MethodPost, identidad.RutaCambiarContrasena, map[string]any{"actual": temporal, "nueva": "nueva-del-usuario-777"}); r.status != 200 || r.cuerpo["cambioContrasenaRequerido"] != nil {
		t.Fatalf("cambiar contraseña: %d %s", r.status, r.crudo)
	}
	otra.pedir(http.MethodGet, "/api/v1/yo", nil).problema(t, 401, "no_autenticado")
	// Ya puede configurar su MFA; la temporal no sirve más.
	if r := nueva.pedir(http.MethodPost, "/api/v1/yo/mfa", nil); r.status != 200 {
		t.Fatalf("configurar de nuevo: %d %s", r.status, r.crudo)
	}
	entrarCon(e.cliente(), correoU, temporal).problema(t, 401, "credenciales_invalidas")
	// La sesión del admin sigue: solo se cierran las del objetivo.
	if r := admin.pedir(http.MethodGet, "/api/v1/yo", nil); r.status != 200 {
		t.Fatalf("sesión del admin: %d %s", r.status, r.crudo)
	}

	// Auditoría (sin la temporal) y eventos por outbox.
	var entrada string
	if err := pool.QueryRow(ctx, `SELECT entrada::text FROM auditoria.acciones
		WHERE accion = 'usuarios.reiniciar_mfa' AND entidad = 'usuario' AND entidad_id = $1 AND resultado = 'ok'`, fmt.Sprint(idU)).Scan(&entrada); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(entrada, "perdió el teléfono") || !strings.Contains(entrada, `"sesiones_cerradas": 2`) ||
		!strings.Contains(entrada, fmt.Sprintf(`"tokens_revocados": [%d]`, idToken)) || strings.Contains(entrada, temporal) || !strings.Contains(entrada, `"contrasenaTemporal": "***"`) {
		t.Fatalf("entrada auditada: %s", entrada)
	}
	var fugas int
	_ = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM auditoria.acciones WHERE entrada::text LIKE '%' || $1 || '%')
		+ (SELECT count(*) FROM auditoria.cambios WHERE antes::text LIKE '%' || $1 || '%' OR despues::text LIKE '%' || $1 || '%')
		+ (SELECT count(*) FROM integracion.outbox WHERE payload::text LIKE '%' || $1 || '%')`, temporal).Scan(&fugas)
	if fugas != 0 {
		t.Fatalf("la contraseña temporal quedó en auditoría o eventos (%d)", fugas)
	}
	var payload []byte
	if err := pool.QueryRow(ctx, `SELECT payload FROM integracion.outbox WHERE evento = 'usuario.mfa_reiniciado' AND payload->>'usuario_id' = $1`, fmt.Sprint(idU)).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	var p map[string]any
	_ = json.Unmarshal(payload, &p)
	if p["motivo"] != "perdió el teléfono" || p["sesiones_cerradas"] != float64(2) || p["tokens_revocados"] != float64(1) {
		t.Fatalf("payload: %s", payload)
	}
	var revocado int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM integracion.outbox WHERE evento = 'token.revocado' AND payload->>'token_id' = $1
		AND payload->>'usuario_id' = $2 AND payload->>'motivo' = 'mfa_reiniciado'`, fmt.Sprint(idToken), fmt.Sprint(idU)).Scan(&revocado)
	if revocado != 1 {
		t.Fatalf("token.revocado: %d", revocado)
	}
	// Los rechazos también quedan auditados (sin escribir eventos).
	var errores, eventos int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM auditoria.acciones WHERE accion = 'usuarios.reiniciar_mfa' AND resultado = 'error'`).Scan(&errores)
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM integracion.outbox WHERE evento = 'usuario.mfa_reiniciado' AND payload->>'usuario_id' IN ($1, $2)`, fmt.Sprint(idU), fmt.Sprint(idAdmin)).Scan(&eventos)
	if errores < 4 || eventos != 1 { // 403, propio, versión y 404 (la validación de huma no llega al caso)
		t.Fatalf("errores auditados %d, eventos %d", errores, eventos)
	}

	// revocarTokens: false conserva los tokens; un MFA a medio configurar también se reinicia.
	bot2, _ := tokenPara(t, e, idU)
	var v int
	_ = pool.QueryRow(ctx, `SELECT version FROM nucleo.usuarios WHERE id = $1`, idU).Scan(&v)
	// La sesión con la contraseña nueva se cierra; el token sigue (0 revocados).
	if r := reiniciarMFA(admin, idU, v, "revocarTokens", false, "contrasenaTemporal", "otra-temporal-larga-99"); r.status != 200 ||
		r.cuerpo["sesionesCerradas"] != float64(1) || r.cuerpo["tokensRevocados"] != float64(0) {
		t.Fatalf("reiniciar a medio configurar: %d %s", r.status, r.crudo)
	}
	if r := bot2.pedir(http.MethodGet, "/api/v1/yo", nil); r.status != 200 {
		t.Fatalf("con revocarTokens false el token sigue: %d %s", r.status, r.crudo)
	}
	reiniciarMFA(admin, idU, v+1).problema(t, 422, "mfa_no_configurado")
}

// Alcance por empresa: un token del admin limitado (sin empresas.todas) solo reinicia a
// usuarios cuyas empresas son todas suyas; a los de otra empresa los ve como inexistentes (404,
// sin revelar su versión), igual al desactivar o editar.
func TestReiniciarMFARespetaEmpresas(t *testing.T) {
	e := entornoEmpresas(t)
	nuevo := empresasAB(t, e)
	_, correoAdmin := nuevo("admin", 101)
	_, correoA := nuevo("lectura", 101)
	idB, correoB := nuevo("lectura", 102)
	idA := idDe(t, correoA)
	_, _, versionA := conMFA(t, e, correoA)
	_, _, versionB := conMFA(t, e, correoB)

	admin := e.cliente()
	admin.entrar(correoAdmin)
	bot := tokenDe(t, e, admin, "usuarios.reiniciar_mfa", "usuarios.desactivar", "usuarios.editar", "usuarios.ver", "roles.ver", "permisos.ver",
		"clientes.ver", "empresas.ver", "tickets.ver", "ordenes.ver", "agenda.ver", "rutas.ver", "prospectos.ver") // los del rol lectura

	// Fuera de su alcance: 404 con cualquier versión (la correcta, otra o una inexistente), como
	// un id que no existe.
	for _, v := range []int{versionB, versionB + 7} {
		reiniciarMFA(bot, idB, v).problema(t, 404, "no_encontrado")
		bot.pedir(http.MethodPost, fmt.Sprintf("/api/v1/usuarios/%d/desactivar", idB), map[string]any{"version": v, "motivo": "x"}).problema(t, 404, "no_encontrado")
		bot.pedir(http.MethodPost, fmt.Sprintf("/api/v1/usuarios/%d/editar", idB), map[string]any{"version": v, "nombre": "x"}).problema(t, 404, "no_encontrado")
	}
	reiniciarMFA(bot, 999999999, 1).problema(t, 404, "no_encontrado")
	// En su alcance, la versión vieja sí es 409.
	reiniciarMFA(bot, idA, versionA+7).problema(t, 409, "conflicto_version")
	if r := reiniciarMFA(bot, idA, versionA); r.status != 200 || r.cuerpo["mfaActivo"] != false {
		t.Fatalf("reiniciar en su empresa: %d %s", r.status, r.crudo)
	}
	// Con la sesión del admin global, también el de la otra empresa.
	if r := reiniciarMFA(admin, idB, versionB); r.status != 200 {
		t.Fatalf("admin global: %d %s", r.status, r.crudo)
	}
}

func idDe(t *testing.T, correo string) int64 {
	t.Helper()
	var id int64
	if err := pruebas.Pool(t).QueryRow(context.Background(), `SELECT id FROM nucleo.usuarios WHERE correo = $1`, correo).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}
