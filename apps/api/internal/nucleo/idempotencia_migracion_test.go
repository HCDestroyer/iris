package nucleo_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// hash_formato sin DEFAULT: un INSERT que la olvide falla (no guarda un HMAC marcado como
// SHA-256 plano). El Down restaura el DEFAULT 0 sin tocar filas; se prueba en una transacción
// que se revierte.
func TestIdempotenciaHashFormatoSinDefault(t *testing.T) {
	crudo, err := os.ReadFile("../../migraciones/20261029173046_nucleo_idempotencia_sin_default.sql")
	if err != nil {
		t.Fatal(err)
	}
	down := strings.SplitN(string(crudo), "-- +goose Down", 2)[1]

	ctx := context.Background()
	tx, err := pruebas.Pool(t).Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	insertar := func(clave string) error {
		_, err := tx.Exec(ctx, `INSERT INTO nucleo.idempotencia (actor, clave, metodo, ruta, hash_peticion, expira_en)
			VALUES ('usuario:1', $1, 'POST', '/api/v1/x', '\x00', now() + interval '1 hour')`, clave)
		return err
	}
	contar := func() (n int) {
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM nucleo.idempotencia`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if _, err := tx.Exec(ctx, `INSERT INTO nucleo.idempotencia (actor, clave, metodo, ruta, hash_peticion, hash_formato, expira_en)
		VALUES ('usuario:1', $1, 'POST', '/api/v1/x', '\x00', 1, now() + interval '1 hour')`, "con-formato-"+pruebas.Unico()); err != nil {
		t.Fatal(err)
	}

	if _, err := tx.Exec(ctx, `SAVEPOINT sin_formato`); err != nil {
		t.Fatal(err)
	}
	var pgErr *pgconn.PgError
	if err := insertar("sin-formato-" + pruebas.Unico()); !errors.As(err, &pgErr) || pgErr.Code != "23502" {
		t.Fatalf("sin hash_formato debe fallar por NOT NULL: %v", err)
	}
	if _, err := tx.Exec(ctx, `ROLLBACK TO SAVEPOINT sin_formato`); err != nil {
		t.Fatal(err)
	}

	antes := contar()
	if _, err := tx.Exec(ctx, down); err != nil {
		t.Fatalf("down: %v", err)
	}
	if despues := contar(); despues != antes {
		t.Fatalf("el Down borró filas: %d → %d", antes, despues)
	}
	clave := "down-" + pruebas.Unico()
	if err := insertar(clave); err != nil {
		t.Fatalf("con el DEFAULT restaurado: %v", err)
	}
	var formato int
	if err := tx.QueryRow(ctx, `SELECT hash_formato FROM nucleo.idempotencia WHERE clave = $1`, clave).Scan(&formato); err != nil || formato != 0 {
		t.Fatalf("DEFAULT restaurado: %d %v", formato, err)
	}
}
