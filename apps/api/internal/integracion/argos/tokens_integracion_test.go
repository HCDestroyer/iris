package argos_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// Cerrar las sesiones con revocarTokens o reiniciar el MFA del usuario de integración no corta
// el conector activo: su iris_pat_ se conserva (tokensConservados) y Argos sigue leyendo; los
// demás tokens del usuario sí se revocan.
func TestRevocarTokensDeIntegracionConservaElDelConector(t *testing.T) {
	e := nuevoEntorno(t)
	admin, id, _ := e.conectado()
	pat := e.patDelUltimoCanje()
	var integ, tokenConector int64
	if err := e.pool.QueryRow(t.Context(), `SELECT t.usuario_id, t.id FROM integracion.conectores c JOIN nucleo.tokens_api t ON t.id = c.token_entregado_id
		WHERE c.id = $1`, id).Scan(&integ, &tokenConector); err != nil {
		t.Fatal(err)
	}
	// Otro token del mismo usuario de integración (no es del conector): ese sí se revoca.
	otro := admin.pedir(http.MethodPost, "/api/v1/tokens", map[string]any{"nombre": "script", "usuarioId": integ})
	if otro.status != 201 {
		t.Fatalf("otro token: %d %s", otro.status, otro.crudo)
	}
	otroID := int64(otro.cuerpo["token"].(map[string]any)["id"].(float64))
	// Uno vencido y eliminado (sin revocar): revocar los del usuario no lo toca.
	viejo := admin.pedir(http.MethodPost, "/api/v1/tokens", map[string]any{"nombre": "viejo", "usuarioId": integ})
	if viejo.status != 201 {
		t.Fatalf("token viejo: %d %s", viejo.status, viejo.crudo)
	}
	viejoID := int64(viejo.cuerpo["token"].(map[string]any)["id"].(float64))
	if _, err := e.pool.Exec(t.Context(), `UPDATE nucleo.tokens_api SET expira_en = now() - interval '1 minute' WHERE id = $1`, viejoID); err != nil {
		t.Fatal(err)
	}
	if r := admin.pedir(http.MethodPost, fmt.Sprintf("/api/v1/tokens/%d/eliminar", viejoID), map[string]any{}); r.status != 200 {
		t.Fatalf("eliminar el viejo: %d %s", r.status, r.crudo)
	}

	// El token_portal del canje (0.9.0 §1.2, hijo del iris_pat_) también es del conector: se conserva.
	var portal int64
	if err := e.pool.QueryRow(t.Context(), `SELECT id FROM nucleo.tokens_api WHERE token_padre_id = $1`, tokenConector).Scan(&portal); err != nil {
		t.Fatalf("token_portal del canje: %v", err)
	}
	conservados := float64(2)

	r := admin.pedir(http.MethodPost, fmt.Sprintf("/api/v1/usuarios/%d/cerrar-sesiones", integ), map[string]any{"revocarTokens": true})
	if r.status != 200 || r.cuerpo["tokensRevocados"] != float64(1) || r.cuerpo["tokensConservados"] != conservados {
		t.Fatalf("cerrar sesiones: %d %s", r.status, r.crudo)
	}
	if r := e.leerConPat(pat); r.status != 200 {
		t.Fatalf("el iris_pat_ del conector activo sigue valiendo: %d", r.status)
	}
	if e.contar(`SELECT count(*) FROM nucleo.tokens_api WHERE id = $1 AND revocado_en IS NOT NULL`, otroID) != 1 ||
		e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'token.revocado' AND (payload->>'token_id')::bigint = $1`, tokenConector) != 0 {
		t.Fatal("se revoca el otro token y no el del conector")
	}
	if e.contar(`SELECT count(*) FROM nucleo.tokens_api WHERE id = $1 AND revocado_en IS NULL`, portal) != 1 {
		t.Fatal("el token_portal del conector activo no se revoca")
	}
	if e.contar(`SELECT count(*) FROM nucleo.tokens_api WHERE id = $1 AND revocado_en IS NULL AND eliminado_en IS NOT NULL`, viejoID) != 1 {
		t.Fatal("un token eliminado no se revoca")
	}

	// Reiniciar el MFA (con revocarTokens por omisión): igual, el del conector se conserva.
	if _, err := e.pool.Exec(t.Context(), `UPDATE nucleo.usuarios SET totp_activo = true, totp_secreto = '\x00'::bytea WHERE id = $1`, integ); err != nil {
		t.Fatal(err)
	}
	var version int32
	if err := e.pool.QueryRow(t.Context(), `SELECT version FROM nucleo.usuarios WHERE id = $1`, integ).Scan(&version); err != nil {
		t.Fatal(err)
	}
	m := admin.pedir(http.MethodPost, fmt.Sprintf("/api/v1/usuarios/%d/reiniciar-mfa", integ), map[string]any{
		"version": version, "motivo": "prueba", "contrasenaTemporal": "temporal-larga-de-prueba-1",
	})
	if m.status != 200 || m.cuerpo["tokensConservados"] != conservados || m.cuerpo["tokensRevocados"] != float64(0) {
		t.Fatalf("reiniciar MFA: %d %s", m.status, m.crudo)
	}
	if r := e.leerConPat(pat); r.status != 200 {
		t.Fatalf("tras reiniciar el MFA el iris_pat_ del conector sigue valiendo: %d", r.status)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'usuario.mfa_reiniciado' AND (payload->>'usuario_id')::bigint = $1
		AND (payload->>'tokens_conservados')::int = 2 AND (payload->>'tokens_revocados')::int = 0`, integ); n != 1 {
		t.Fatalf("usuario.mfa_reiniciado con tokens_conservados: %d", n)
	}
}

// Revisión de seguridad de #299: un token_portal filtrado se revoca solo (sin tocar el iris_pat_ del
// conector) y el siguiente PUT …/contrato entrega otro; revocar todos los tokens del usuario de
// integración sigue conservando el token_portal vigente del conector activo.
func TestRevocarSoloElTokenPortal(t *testing.T) {
	e := nuevoEntorno(t)
	e.argos.con(func() { e.argos.version = "1.30.0" })
	admin, id, _ := e.conectado()
	var integ, pat int64
	if err := e.pool.QueryRow(t.Context(), `SELECT t.usuario_id, t.id FROM integracion.conectores c JOIN nucleo.tokens_api t ON t.id = c.token_entregado_id
		WHERE c.id = $1`, id).Scan(&integ, &pat); err != nil {
		t.Fatal(err)
	}
	portalVigente := func() int64 {
		t.Helper()
		var p int64
		if err := e.pool.QueryRow(t.Context(), `SELECT id FROM nucleo.tokens_api WHERE token_padre_id = $1 AND revocado_en IS NULL AND eliminado_en IS NULL`,
			pat).Scan(&p); err != nil {
			t.Fatalf("token_portal vigente: %v", err)
		}
		return p
	}
	portal := portalVigente()

	// Revocarlo solo funciona y luego se puede eliminar; el iris_pat_ del conector sigue vigente.
	if r := admin.pedir(http.MethodPost, fmt.Sprintf("/api/v1/tokens/%d/revocar", portal), map[string]any{}); r.status != 200 {
		t.Fatalf("revocar el token_portal: %d %s", r.status, r.crudo)
	}
	if r := admin.pedir(http.MethodPost, fmt.Sprintf("/api/v1/tokens/%d/eliminar", portal), map[string]any{}); r.status != 200 {
		t.Fatalf("eliminar el token_portal revocado: %d %s", r.status, r.crudo)
	}
	if e.contar(`SELECT count(*) FROM nucleo.tokens_api WHERE id = $1 AND revocado_en IS NULL`, pat) != 1 {
		t.Fatal("el iris_pat_ del conector sigue vigente")
	}

	// El siguiente PUT …/contrato entrega uno nuevo.
	var antes int
	e.argos.con(func() { antes = e.argos.portales })
	if _, err := e.mod.InformarContrato.Ejecutar(pruebas.ContextoSistema(e.pool), id); err != nil {
		t.Fatal(err)
	}
	var despues int
	e.argos.con(func() { despues = e.argos.portales })
	nuevo := portalVigente()
	if despues != antes+1 || nuevo == portal {
		t.Fatalf("PUT con token_portal nuevo: %d → %d, %d → %d", antes, despues, portal, nuevo)
	}

	// Revocar todos los del usuario conserva el iris_pat_ y el token_portal nuevo.
	if r := admin.pedir(http.MethodPost, fmt.Sprintf("/api/v1/usuarios/%d/cerrar-sesiones", integ), map[string]any{"revocarTokens": true}); r.status != 200 {
		t.Fatalf("cerrar sesiones: %d %s", r.status, r.crudo)
	}
	if e.contar(`SELECT count(*) FROM nucleo.tokens_api WHERE id = ANY($1) AND revocado_en IS NULL`, []int64{pat, nuevo}) != 2 {
		t.Fatal("revocar los del usuario conserva el iris_pat_ y el token_portal del conector")
	}
}
