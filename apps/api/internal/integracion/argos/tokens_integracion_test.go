package argos_test

import (
	"fmt"
	"net/http"
	"testing"
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

	r := admin.pedir(http.MethodPost, fmt.Sprintf("/api/v1/usuarios/%d/cerrar-sesiones", integ), map[string]any{"revocarTokens": true})
	if r.status != 200 || r.cuerpo["tokensRevocados"] != float64(1) || r.cuerpo["tokensConservados"] != float64(1) {
		t.Fatalf("cerrar sesiones: %d %s", r.status, r.crudo)
	}
	if r := e.leerConPat(pat); r.status != 200 {
		t.Fatalf("el iris_pat_ del conector activo sigue valiendo: %d", r.status)
	}
	if e.contar(`SELECT count(*) FROM nucleo.tokens_api WHERE id = $1 AND revocado_en IS NOT NULL`, otroID) != 1 ||
		e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'token.revocado' AND (payload->>'token_id')::bigint = $1`, tokenConector) != 0 {
		t.Fatal("se revoca el otro token y no el del conector")
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
	if m.status != 200 || m.cuerpo["tokensConservados"] != float64(1) || m.cuerpo["tokensRevocados"] != float64(0) {
		t.Fatalf("reiniciar MFA: %d %s", m.status, m.crudo)
	}
	if r := e.leerConPat(pat); r.status != 200 {
		t.Fatalf("tras reiniciar el MFA el iris_pat_ del conector sigue valiendo: %d", r.status)
	}
}
