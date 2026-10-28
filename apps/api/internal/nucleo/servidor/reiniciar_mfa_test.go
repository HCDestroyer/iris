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

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

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

func reiniciarMFA(c *cliente, id int64, version int) respuesta {
	return c.pedir(http.MethodPost, fmt.Sprintf("/api/v1/usuarios/%d/reiniciar-mfa", id), map[string]any{"version": version, "motivo": "perdió el teléfono"})
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
	admin.pedir(http.MethodPost, fmt.Sprintf("/api/v1/usuarios/%d/reiniciar-mfa", idU), map[string]any{"version": version}).problema(t, 422, "validacion")

	r := reiniciarMFA(admin, idU, version)
	if r.status != 200 || r.cuerpo["mfaActivo"] != false || int(r.cuerpo["version"].(float64)) != version+1 {
		t.Fatalf("reiniciar: %d %s", r.status, r.crudo)
	}
	// Sus sesiones (completa y pendiente) quedaron cerradas.
	sesionU.pedir(http.MethodGet, "/api/v1/yo", nil).problema(t, 401, "no_autenticado")
	pendiente.pedir(http.MethodPost, "/api/v1/sesion/mfa", map[string]any{"codigo": "123456"}).problema(t, 401, "no_autenticado")
	var abiertas int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM nucleo.sesiones WHERE usuario_id = $1 AND revocada_en IS NULL`, idU).Scan(&abiertas); err != nil || abiertas != 0 {
		t.Fatalf("sesiones abiertas: %d %v", abiertas, err)
	}
	var secretoNulo bool
	if err := pool.QueryRow(ctx, `SELECT totp_secreto IS NULL AND NOT totp_activo AND totp_ultimo_paso = 0 FROM nucleo.usuarios WHERE id = $1`, idU).Scan(&secretoNulo); err != nil || !secretoNulo {
		t.Fatalf("secreto borrado: %v %v", secretoNulo, err)
	}
	// Entra solo con la contraseña y puede volver a configurar el MFA.
	nueva := e.cliente()
	if r := nueva.entrar(correoU); r.status != 200 || r.cuerpo["mfaRequerido"] != false {
		t.Fatalf("entrar tras el reinicio: %d %s", r.status, r.crudo)
	}
	if r := nueva.pedir(http.MethodPost, "/api/v1/yo/mfa", nil); r.status != 200 {
		t.Fatalf("configurar de nuevo: %d %s", r.status, r.crudo)
	}
	// La sesión del admin sigue: solo se cierran las del objetivo.
	if r := admin.pedir(http.MethodGet, "/api/v1/yo", nil); r.status != 200 {
		t.Fatalf("sesión del admin: %d %s", r.status, r.crudo)
	}

	// Auditoría (sin secretos) y evento por outbox.
	var entrada, resultado string
	if err := pool.QueryRow(ctx, `SELECT entrada::text, resultado FROM auditoria.acciones
		WHERE accion = 'usuarios.reiniciar_mfa' AND entidad = 'usuario' AND entidad_id = $1 AND resultado = 'ok'`, fmt.Sprint(idU)).Scan(&entrada, &resultado); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(entrada, "perdió el teléfono") || !strings.Contains(entrada, `"sesiones_cerradas": 2`) {
		t.Fatalf("entrada auditada: %s", entrada)
	}
	// El cambio de totp_activo queda registrado (enmascarado, como todo lo de TOTP).
	var antes, despues string
	if err := pool.QueryRow(ctx, `SELECT c.antes::text, c.despues::text FROM auditoria.cambios c JOIN auditoria.acciones a ON a.id = c.accion_id
		WHERE a.accion = 'usuarios.reiniciar_mfa' AND c.entidad_id = $1 AND c.campo = 'totp_activo'`, fmt.Sprint(idU)).Scan(&antes, &despues); err != nil || antes != `"***"` || despues != `"***"` {
		t.Fatalf("cambio totp_activo: %s → %s %v", antes, despues, err)
	}
	var payload []byte
	if err := pool.QueryRow(ctx, `SELECT payload FROM integracion.outbox WHERE evento = 'usuario.mfa_reiniciado' AND payload->>'usuario_id' = $1`, fmt.Sprint(idU)).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	var p map[string]any
	_ = json.Unmarshal(payload, &p)
	if p["motivo"] != "perdió el teléfono" || p["sesiones_cerradas"] != float64(2) {
		t.Fatalf("payload: %s", payload)
	}
	// Los rechazos también quedan auditados (sin escribir eventos).
	var errores, eventos int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM auditoria.acciones WHERE accion = 'usuarios.reiniciar_mfa' AND resultado = 'error'`).Scan(&errores)
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM integracion.outbox WHERE evento = 'usuario.mfa_reiniciado' AND payload->>'usuario_id' IN ($1, $2)`, fmt.Sprint(idU), fmt.Sprint(idAdmin)).Scan(&eventos)
	if errores < 4 || eventos != 1 { // 403, propio, versión y 404 (la validación de huma no llega al caso)
		t.Fatalf("errores auditados %d, eventos %d", errores, eventos)
	}

	// Un MFA a medio configurar (secreto sin activar) también se reinicia; sin MFA, 422.
	if r := reiniciarMFA(admin, idU, version+1); r.status != 200 {
		t.Fatalf("reiniciar a medio configurar: %d %s", r.status, r.crudo)
	}
	reiniciarMFA(admin, idU, version+2).problema(t, 422, "mfa_no_configurado")
}

// Alcance por empresa: un token del admin limitado (sin empresas.todas) solo reinicia a
// usuarios cuyas empresas son todas suyas, como editar o desactivar.
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
	bot := tokenDe(t, e, admin, "usuarios.reiniciar_mfa", "usuarios.ver", "roles.ver", "permisos.ver",
		"clientes.ver", "empresas.ver", "tickets.ver", "ordenes.ver", "agenda.ver") // los del rol lectura

	reiniciarMFA(bot, idB, versionB).problema(t, 403, "prohibido")
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
