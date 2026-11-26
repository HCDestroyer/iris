package notificaciones_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// Los 12 tipos de aviso de la campana después de todas las migraciones.
var tiposAviso = []string{"asistente_tope", "servidor_ia_caido", "servidor_ia_incompatible", "servidor_ia_umbral", "argos_certificado",
	"venta_revisada", "paso_titular", "contrato_sin_firma", "posible_falla_general", "verificacion_vencida",
	"argos_clave_reutilizada", "argos_carga_vinculos"}

// Revisión de la cadena de bodega (ALTO): 20261125220009 rehace avisos_tipo_check y no puede perder
// los tipos que agregó antes #285 (posible_falla_general, verificacion_vencida). Con una fila de
// posible_falla_general guardada, el Down y el Up de esa migración pasan y la fila queda; y con todas
// las migraciones aplicadas el CHECK tiene los 12 tipos. En una transacción que se revierte.
func TestMigracionVinculosConservaTiposDeAviso(t *testing.T) {
	crudo, err := os.ReadFile("../../migraciones/20261125220009_argos_vinculos_tecnicos.sql")
	if err != nil {
		t.Fatal(err)
	}
	partes := strings.SplitN(strings.SplitN(string(crudo), "-- +goose Up", 2)[1], "-- +goose Down", 2)
	up, down := partes[0], partes[1]
	x := nuevoEntornoAvisos(t, false)
	yo := x.persona([]int64{1}, "supervisor")
	ctx := context.Background()
	pool := pruebas.Pool(t)

	var def string
	if err := pool.QueryRow(ctx, `SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname = 'avisos_tipo_check'`).Scan(&def); err != nil {
		t.Fatal(err)
	}
	for _, tipo := range tiposAviso {
		if !strings.Contains(def, "'"+tipo+"'") {
			t.Errorf("con todas las migraciones falta %s: %s", tipo, def)
		}
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var id int64
	if err := tx.QueryRow(ctx, `INSERT INTO notificaciones.avisos (usuario_id, empresa_id, tipo, clave, nivel, titulo)
		VALUES ($1, 1, 'posible_falla_general', 'prueba-migracion-220009', 'peligro', 'Posible falla general') RETURNING id`, yo.id).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, down); err != nil {
		t.Fatalf("down con posible_falla_general: %v", err)
	}
	if _, err := tx.Exec(ctx, up); err != nil {
		t.Fatalf("up con posible_falla_general: %v", err)
	}
	var n int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM notificaciones.avisos WHERE id = $1`, id).Scan(&n); err != nil || n != 1 {
		t.Fatalf("la fila no quedó: %d %v", n, err)
	}
	if err := tx.QueryRow(ctx, `SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname = 'avisos_tipo_check'`).Scan(&def); err != nil {
		t.Fatal(err)
	}
	for _, tipo := range tiposAviso {
		if !strings.Contains(def, "'"+tipo+"'") {
			t.Errorf("tras el Up de 220009 falta %s: %s", tipo, def)
		}
	}
}
