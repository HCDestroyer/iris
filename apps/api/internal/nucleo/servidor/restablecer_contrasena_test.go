package servidor_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

const temporalRestablecida = "restablecida-fuera-de-banda-77"

func restablecer(c *cliente, id int64, version int, extra ...any) respuesta {
	cuerpo := map[string]any{"version": version, "motivo": "olvidó su contraseña", "contrasenaTemporal": temporalRestablecida}
	for i := 0; i+1 < len(extra); i += 2 {
		cuerpo[extra[i].(string)] = extra[i+1]
	}
	return c.pedir(http.MethodPost, fmt.Sprintf("/api/v1/usuarios/%d/restablecer-contrasena", id), cuerpo)
}

func versionDe(t *testing.T, id int64) int {
	t.Helper()
	var v int
	if err := pruebas.Pool(t).QueryRow(context.Background(), `SELECT version FROM nucleo.usuarios WHERE id = $1`, id).Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestRestablecerContrasena(t *testing.T) {
	e := nuevoEntorno(t)
	pool := pruebas.Pool(t)
	ctx := context.Background()
	idAdmin, correoAdmin := e.usuario("admin")
	_, correoSoporte := e.usuario("soporte")
	idU, correoU := e.usuario("lectura")

	// Dos sesiones abiertas del usuario y un token: las sesiones se cierran, el token no.
	s1, s2 := e.cliente(), e.cliente()
	s1.entrar(correoU)
	s2.entrar(correoU)
	bot, _ := tokenPara(t, e, idU)
	version := versionDe(t, idU)

	// Sin el permiso: 403 (soporte tiene usuarios.ver, no usuarios.restablecer_contrasena).
	sop := e.cliente()
	sop.entrar(correoSoporte)
	restablecer(sop, idU, version).problema(t, 403, "prohibido")

	admin := e.cliente()
	admin.entrar(correoAdmin)
	// La propia: no por esta vía (422).
	restablecer(admin, idAdmin, versionDe(t, idAdmin)).problema(t, 422, "restablecer_propia")
	// Versión vieja: 409; inexistente: 404; sin motivo o temporal corta o igual a la actual: 422.
	restablecer(admin, idU, version+5).problema(t, 409, "conflicto_version")
	restablecer(admin, 999999999, 1).problema(t, 404, "no_encontrado")
	admin.pedir(http.MethodPost, fmt.Sprintf("/api/v1/usuarios/%d/restablecer-contrasena", idU), map[string]any{"version": version, "contrasenaTemporal": temporalRestablecida}).problema(t, 422, "validacion")
	restablecer(admin, idU, version, "contrasenaTemporal", "corta").problema(t, 422, "validacion")
	restablecer(admin, idU, version, "contrasenaTemporal", contrasena).problema(t, 422, "validacion")

	r := restablecer(admin, idU, version)
	if r.status != 200 || r.cuerpo["cambioContrasenaRequerido"] != true || int(r.cuerpo["version"].(float64)) != version+1 {
		t.Fatalf("restablecer: %d %s", r.status, r.crudo)
	}
	if r.cuerpo["sesionesCerradas"] != float64(2) || r.cuerpo["tokensRevocados"] != float64(0) || r.cuerpo["id"] != float64(idU) {
		t.Fatalf("contadores: %s", r.crudo)
	}
	s1.pedir(http.MethodGet, "/api/v1/yo", nil).problema(t, 401, "no_autenticado")
	s2.pedir(http.MethodGet, "/api/v1/yo", nil).problema(t, 401, "no_autenticado")
	// El token de API sigue (revocarTokens es false por omisión).
	if r := bot.pedir(http.MethodGet, "/api/v1/yo", nil); r.status != 200 {
		t.Fatalf("token tras restablecer: %d %s", r.status, r.crudo)
	}
	// La vieja ya no entra; la temporal entra pero exige cambiarla.
	entrarCon(e.cliente(), correoU, contrasena).problema(t, 401, "credenciales_invalidas")
	nueva := e.cliente()
	if r := entrarCon(nueva, correoU, temporalRestablecida); r.status != 200 ||
		r.cuerpo["usuario"].(map[string]any)["cambioContrasenaRequerido"] != true {
		t.Fatalf("entrar con la temporal: %d %s", r.status, r.crudo)
	}
	nueva.pedir(http.MethodGet, "/api/v1/usuarios", nil).problema(t, 403, "cambio_contrasena_requerido")
	// La sesión del admin sigue.
	if r := admin.pedir(http.MethodGet, "/api/v1/yo", nil); r.status != 200 {
		t.Fatalf("sesión del admin: %d %s", r.status, r.crudo)
	}

	// Auditoría (con la temporal como ***) y evento por outbox sin la contraseña.
	var entrada string
	if err := pool.QueryRow(ctx, `SELECT entrada::text FROM auditoria.acciones
		WHERE accion = 'usuarios.restablecer_contrasena' AND entidad = 'usuario' AND entidad_id = $1 AND resultado = 'ok'`, fmt.Sprint(idU)).Scan(&entrada); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(entrada, "olvidó su contraseña") || !strings.Contains(entrada, `"sesiones_cerradas": 2`) ||
		strings.Contains(entrada, temporalRestablecida) || !strings.Contains(entrada, `"contrasenaTemporal": "***"`) {
		t.Fatalf("entrada auditada: %s", entrada)
	}
	var fugas int
	_ = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM auditoria.acciones WHERE entrada::text LIKE '%' || $1 || '%')
		+ (SELECT count(*) FROM auditoria.cambios WHERE antes::text LIKE '%' || $1 || '%' OR despues::text LIKE '%' || $1 || '%')
		+ (SELECT count(*) FROM integracion.outbox WHERE payload::text LIKE '%' || $1 || '%')`, temporalRestablecida).Scan(&fugas)
	if fugas != 0 {
		t.Fatalf("la contraseña temporal quedó en auditoría o eventos (%d)", fugas)
	}
	var payload []byte
	if err := pool.QueryRow(ctx, `SELECT payload FROM integracion.outbox WHERE evento = 'usuario.contrasena_restablecida' AND payload->>'usuario_id' = $1`, fmt.Sprint(idU)).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	var p map[string]any
	_ = json.Unmarshal(payload, &p)
	if p["motivo"] != "olvidó su contraseña" || p["sesiones_cerradas"] != float64(2) || p["tokens_revocados"] != float64(0) {
		t.Fatalf("payload: %s", payload)
	}

	// Con revocarTokens: true, también sus tokens.
	if r := restablecer(admin, idU, version+1, "revocarTokens", true, "contrasenaTemporal", "otra-temporal-larga-99"); r.status != 200 || r.cuerpo["tokensRevocados"] != float64(1) {
		t.Fatalf("restablecer revocando tokens: %d %s", r.status, r.crudo)
	}
	bot.pedir(http.MethodGet, "/api/v1/yo", nil).problema(t, 401, "no_autenticado")

	// Sus vínculos de autologin desde Argos se borran (quien tome la cuenta no los hereda).
	var conector, vinculo int64
	if err := pool.QueryRow(ctx, `INSERT INTO integracion.conectores (sistema, base_url, origen, token_cifrado, secreto_webhook_cifrado, estado)
		VALUES ('argosisp', 'https://argos.prueba', 'enlace_remoto', '\x00', '\x00', 'apagado') RETURNING id`).Scan(&conector); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO nucleo.sso_vinculos (conector_id, usuario_externo, usuario_id) VALUES ($1, $2, $3) RETURNING id`,
		conector, "ext-"+pruebas.Unico(), idU).Scan(&vinculo); err != nil {
		t.Fatal(err)
	}
	if r := restablecer(admin, idU, version+2, "contrasenaTemporal", "tercera-temporal-larga-55"); r.status != 200 {
		t.Fatalf("restablecer con vínculo: %d %s", r.status, r.crudo)
	}
	var vinculos, desvinculados int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM nucleo.sso_vinculos WHERE usuario_id = $1`, idU).Scan(&vinculos)
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM integracion.outbox WHERE evento = 'usuario.sso_desvinculado' AND payload->>'vinculo_id' = $1
		AND payload->>'usuario_id' = $2 AND payload->>'motivo' = 'contrasena_reiniciada'`, fmt.Sprint(vinculo), fmt.Sprint(idU)).Scan(&desvinculados)
	if vinculos != 0 || desvinculados != 1 {
		t.Fatalf("vínculos SSO: quedan %d, eventos %d", vinculos, desvinculados)
	}

	// Sin superar al objetivo (sus permisos van más allá de los tuyos): 403.
	_, correoAcotado := e.usuario(rolCon(t, "usuarios.restablecer_contrasena", "usuarios.ver"))
	acotado := e.cliente()
	acotado.entrar(correoAcotado)
	restablecer(acotado, idAdmin, versionDe(t, idAdmin)).problema(t, 403, "prohibido")

	// Un usuario de integración no tiene contraseña: 422.
	var idBot int64
	if err := pool.QueryRow(ctx, `INSERT INTO nucleo.usuarios (nombre, correo, tipo) VALUES ('Bot', $1, 'integracion') RETURNING id`, pruebas.Correo("bot")).Scan(&idBot); err != nil {
		t.Fatal(err)
	}
	restablecer(admin, idBot, versionDe(t, idBot)).problema(t, 422, "usuario_integracion")
}

// Alcance por empresa: un token limitado (sin empresas.todas) ve a los de otra empresa como
// inexistentes (404, sin revelar su versión).
func TestRestablecerContrasenaRespetaEmpresas(t *testing.T) {
	e := entornoEmpresas(t)
	nuevo := empresasAB(t, e)
	_, correoAdmin := nuevo("admin", 101)
	idOtroAdmin, _ := nuevo("admin", 101) // con empresas.todas: solo lo administra un admin global
	idA, _ := nuevo("lectura", 101)
	idB, _ := nuevo("lectura", 102)
	versionA, versionB := versionDe(t, idA), versionDe(t, idB)

	admin := e.cliente()
	admin.entrar(correoAdmin)
	bot := tokenDe(t, e, admin, "usuarios.restablecer_contrasena", "usuarios.ver", "roles.ver", "permisos.ver",
		"clientes.ver", "empresas.ver", "tickets.ver", "ordenes.ver", "agenda.ver", "rutas.ver", "prospectos.ver", "reportes.ver", "chat.ver", "conversaciones.ver", "manuales.ver", "llamadas.ver") // los del rol lectura

	for _, v := range []int{versionB, versionB + 7} {
		restablecer(bot, idB, v).problema(t, 404, "no_encontrado")
	}
	// Acotado a una empresa contra un usuario con empresas.todas: 404, como si no existiera.
	restablecer(bot, idOtroAdmin, versionDe(t, idOtroAdmin)).problema(t, 404, "no_encontrado")
	restablecer(bot, idA, versionA+7).problema(t, 409, "conflicto_version")
	if r := restablecer(bot, idA, versionA); r.status != 200 || r.cuerpo["cambioContrasenaRequerido"] != true {
		t.Fatalf("restablecer en su empresa: %d %s", r.status, r.crudo)
	}
	if r := restablecer(admin, idB, versionB); r.status != 200 {
		t.Fatalf("admin global: %d %s", r.status, r.crudo)
	}
}
