package servidor_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	identidadbd "github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// «Revocar y eliminar» es una sola acción del servidor: una transacción, una fila de auditoría y
// los dos eventos. Sobre uno vigente exige además tokens.revocar.
func TestRevocarYEliminarToken(t *testing.T) {
	e := nuevoEntorno(t)
	_, correoAdmin := e.usuario("admin")
	admin := e.cliente()
	admin.entrar(correoAdmin)
	id, valor := tokenCon(t, admin, nil)
	ruta := fmt.Sprintf("/api/v1/tokens/%d/revocar-y-eliminar", id)

	// Un token con tokens.eliminar pero sin tokens.revocar: 403 sobre uno vigente, sin cambios.
	sinRevocar := tokenDe(t, e, admin, "tokens.ver", "tokens.eliminar")
	sinRevocar.pedir(http.MethodPost, ruta, map[string]any{}).problema(t, 403, "prohibido")
	if n := contarSQL(t, `SELECT count(*) FROM nucleo.tokens_api WHERE id = $1 AND revocado_en IS NULL AND eliminado_en IS NULL`, id); n != 1 {
		t.Fatal("el 403 no toca el token")
	}

	r := admin.pedir(http.MethodPost, ruta, map[string]any{"motivo": "ya no se usa"})
	if r.status != 200 || r.cuerpo["revocadoEn"] == nil || r.cuerpo["eliminadoEn"] == nil {
		t.Fatalf("revocar y eliminar: %d %s", r.status, r.crudo)
	}
	api := e.cliente()
	api.bearer = valor
	api.pedir(http.MethodGet, "/api/v1/yo", nil).problema(t, 401, "no_autenticado")
	if n := contarSQL(t, `SELECT count(*) FROM integracion.outbox WHERE evento = 'token.revocado' AND (payload->>'token_id')::bigint = $1 AND payload->>'motivo' = 'ya no se usa'`, id); n != 1 {
		t.Fatalf("token.revocado: %d", n)
	}
	if n := contarSQL(t, `SELECT count(*) FROM integracion.outbox WHERE evento = 'token.eliminado' AND (payload->>'token_id')::bigint = $1 AND payload->>'motivo' = 'manual'`, id); n != 1 {
		t.Fatalf("token.eliminado: %d", n)
	}
	if n := auditadas(t, "tokens.revocar_y_eliminar", fmt.Sprint(id)); n != 1 {
		t.Fatalf("auditoría: %d", n)
	}
	// Otra vez: 200 sin eventos nuevos.
	if r := admin.pedir(http.MethodPost, ruta, map[string]any{}); r.status != 200 {
		t.Fatalf("otra vez: %d %s", r.status, r.crudo)
	}
	if n := eventos(t, "token.eliminado", "token_id", id); n != 1 {
		t.Fatalf("token.eliminado tras repetir: %d", n)
	}
	// Uno ya revocado: basta tokens.eliminar.
	otro, _ := tokenCon(t, admin, nil)
	if r := admin.pedir(http.MethodPost, fmt.Sprintf("/api/v1/tokens/%d/revocar", otro), map[string]any{}); r.status != 200 {
		t.Fatalf("revocar: %d %s", r.status, r.crudo)
	}
	if r := sinRevocar.pedir(http.MethodPost, fmt.Sprintf("/api/v1/tokens/%d/revocar-y-eliminar", otro), map[string]any{}); r.status != 200 {
		t.Fatalf("revocado, solo con tokens.eliminar: %d %s", r.status, r.crudo)
	}
}

// M5: la base no deja eliminar un token que aún vale: el UPDATE exige revocado o vencido con su
// propia hora, y el CHECK exige que un eliminado esté revocado o tenga vencimiento.
func TestEliminarTokenVigenteEnLaBase(t *testing.T) {
	e := nuevoEntorno(t)
	_, correoAdmin := e.usuario("admin")
	admin := e.cliente()
	admin.entrar(correoAdmin)
	vigente, _ := tokenCon(t, admin, map[string]any{"expiraEn": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)})
	pool := pruebas.Pool(t)
	if _, err := identidadbd.New(pool).EliminarToken(context.Background(), identidadbd.EliminarTokenParams{ID: vigente}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("EliminarToken de uno vigente: %v", err)
	}
	sinVencimiento, _ := tokenCon(t, admin, nil)
	_, err := pool.Exec(context.Background(), `UPDATE nucleo.tokens_api SET eliminado_en = now(), hash = NULL WHERE id = $1`, sinVencimiento)
	if err == nil || !strings.Contains(err.Error(), "tokens_api_eliminado_inactivo") {
		t.Fatalf("CHECK de eliminado inactivo: %v", err)
	}
}
