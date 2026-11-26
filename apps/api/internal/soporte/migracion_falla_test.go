package soporte_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
	"github.com/HCDestroyer/iris/apps/api/internal/soporte"
)

// El Down de la falla general borra la categoría caidas_repetidas que agregó el Up, y falla si algún
// ticket la usa (nunca deja huérfanos). Cada caso en una transacción que se revierte.
func TestMigracionFallaGeneralDownCategoria(t *testing.T) {
	crudo, err := os.ReadFile("../../migraciones/20261125213013_soporte_falla_general.sql")
	if err != nil {
		t.Fatal(err)
	}
	down := strings.SplitN(string(crudo), "-- +goose Down", 2)[1]
	ctx := context.Background()
	pool := pruebas.Pool(t)
	// Sin tickets de esa categoría: el Down la borra.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, "UPDATE soporte.tickets SET categoria_id = NULL WHERE categoria_id = (SELECT id FROM soporte.categorias WHERE empresa_id IS NULL AND codigo = $1)",
		soporte.CategoriaCaidas); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, down); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("down: %v", err)
	}
	var n int
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM soporte.categorias WHERE empresa_id IS NULL AND codigo = $1", soporte.CategoriaCaidas).Scan(&n); err != nil || n != 0 {
		_ = tx.Rollback(ctx)
		t.Fatalf("la categoría queda: %d %v", n, err)
	}
	_ = tx.Rollback(ctx)

	// Con un ticket que la usa: el Down falla y lo dice.
	m, _ := nuevoModulo(time.Now())
	yo := agente(t)
	tk := crear(t, m, como(t, yo), soporte.EntradaCrearTicket{})
	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "UPDATE soporte.tickets SET categoria_id = (SELECT id FROM soporte.categorias WHERE empresa_id IS NULL AND codigo = $2) WHERE id = $1",
		tk.ID, soporte.CategoriaCaidas); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, down); err == nil || !strings.Contains(err.Error(), "caidas_repetidas") {
		t.Fatalf("con tickets de la categoría el Down debe fallar: %v", err)
	}
}
