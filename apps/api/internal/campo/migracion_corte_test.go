package campo_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// Migración del corte manual (20261126160152, revisión de #297): el Down falla con un mensaje claro si
// ya hay datos de corte_catv o reconexion_catv, y sin datos baja y vuelve a subir. Cada caso en una
// transacción que se revierte.
func TestMigracionCorteManualUpDown(t *testing.T) {
	crudo, err := os.ReadFile("../../migraciones/20261126160152_campo_corte_manual.sql")
	if err != nil {
		t.Fatal(err)
	}
	partes := strings.SplitN(string(crudo), "-- +goose Down", 2)
	up, down := strings.TrimPrefix(partes[0], "-- +goose Up"), partes[1]
	ctx := context.Background()
	pool := pruebas.Pool(t)
	tiposCorte := func(tx pgx.Tx) int {
		t.Helper()
		var n int
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM campo.tipos_orden WHERE codigo IN ('corte_catv', 'reconexion_catv')").Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	// Con datos (una orden de corte abierta por Argos): el Down falla y dice qué hay.
	e := nuevoEntornoCorte(t)
	e.aviso("solicitado", solicitudCorte+41, 1, "corte", "8801", nil)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, down); err == nil || !strings.Contains(err.Error(), "campo_corte_manual down") || !strings.Contains(err.Error(), "órdenes") {
		_ = tx.Rollback(ctx)
		t.Fatalf("con datos el Down debe fallar con un mensaje claro: %v", err)
	}
	_ = tx.Rollback(ctx)

	// Sin datos: los de estos tipos se quitan dentro de la transacción; el Down baja y el Up vuelve a subir.
	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, sql := range []string{
		"DELETE FROM campo.cortes_manuales",
		`UPDATE campo.ordenes SET tipo_id = (SELECT id FROM campo.tipos_orden WHERE codigo = 'visita'), plantilla_id = NULL
			WHERE tipo_id IN (SELECT id FROM campo.tipos_orden WHERE codigo IN ('corte_catv', 'reconexion_catv'))`,
		`UPDATE agenda.citas SET tipo_id = (SELECT id FROM campo.tipos_orden WHERE codigo = 'visita')
			WHERE tipo_id IN (SELECT id FROM campo.tipos_orden WHERE codigo IN ('corte_catv', 'reconexion_catv'))`,
		"DELETE FROM agenda.capacidades WHERE tipo_id IN (SELECT id FROM campo.tipos_orden WHERE codigo IN ('corte_catv', 'reconexion_catv'))",
		`DELETE FROM campo.orden_checklist_historial WHERE item_id IN (SELECT i.id FROM campo.plantilla_items i
			JOIN campo.plantillas_checklist p ON p.id = i.plantilla_id WHERE p.tipo_codigo IN ('corte_catv', 'reconexion_catv'))`,
		`DELETE FROM campo.orden_checklist WHERE item_id IN (SELECT i.id FROM campo.plantilla_items i
			JOIN campo.plantillas_checklist p ON p.id = i.plantilla_id WHERE p.tipo_codigo IN ('corte_catv', 'reconexion_catv'))`,
		`UPDATE campo.orden_adjuntos SET item_id = NULL WHERE item_id IN (SELECT i.id FROM campo.plantilla_items i
			JOIN campo.plantillas_checklist p ON p.id = i.plantilla_id WHERE p.tipo_codigo IN ('corte_catv', 'reconexion_catv'))`,
		`DELETE FROM campo.orden_formularios WHERE plantilla_id IN (SELECT id FROM campo.plantillas_checklist WHERE tipo_codigo IN ('corte_catv', 'reconexion_catv'))`,
		`UPDATE campo.plantillas_checklist SET tipo_codigo = NULL WHERE tipo_codigo IN ('corte_catv', 'reconexion_catv')
			AND NOT (empresa_id IS NULL AND codigo IN ('corte_catv', 'reconexion_catv'))`,
	} {
		if _, err := tx.Exec(ctx, sql); err != nil {
			t.Fatalf("preparar sin datos: %v\n%s", err, sql)
		}
	}
	if _, err := tx.Exec(ctx, down); err != nil {
		t.Fatalf("down sin datos: %v", err)
	}
	if n := tiposCorte(tx); n != 0 {
		t.Fatalf("el Down quita los tipos: %d", n)
	}
	var tabla *string
	if err := tx.QueryRow(ctx, "SELECT to_regclass('campo.cortes_manuales')::text").Scan(&tabla); err != nil || tabla != nil {
		t.Fatalf("el Down quita la tabla: %v %v", tabla, err)
	}
	if _, err := tx.Exec(ctx, up); err != nil {
		t.Fatalf("up: %v", err)
	}
	if n := tiposCorte(tx); n != 2 {
		t.Fatalf("el Up crea los dos tipos: %d", n)
	}
	var items int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM campo.plantilla_items i JOIN campo.plantillas_checklist p ON p.version_actual_id = i.version_id
		WHERE p.empresa_id IS NULL AND p.codigo IN ('corte_catv', 'reconexion_catv') AND i.codigo = 'resultado' AND i.obligatorio`).Scan(&items); err != nil || items != 2 {
		t.Fatalf("el ítem resultado obligatorio en las dos plantillas: %d %v", items, err)
	}
}
