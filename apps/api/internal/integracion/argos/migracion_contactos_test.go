package argos_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// La migración de contactos de la atención (B22, B25) solo agrega columnas a la tabla del portal
// (#283): el Down las quita sin tocar las filas del portal (ni su quitado) y el Up las vuelve a poner
// con los valores seguros por defecto (permisos en false, relación «otro»). Todo en una transacción
// que se revierte.
func TestMigracionContactosAtencionUpDown(t *testing.T) {
	crudo, err := os.ReadFile("../../../migraciones/20261126101606_integracion_argos_contactos.sql")
	if err != nil {
		t.Fatal(err)
	}
	partes := strings.SplitN(string(crudo), "-- +goose Down", 2)
	up, down := partes[0], partes[1]
	ctx := context.Background()
	pool := pruebas.Pool(t)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	columnas := func() int {
		t.Helper()
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_schema = 'integracion'
			AND table_name = 'argos_contactos' AND column_name IN ('relacion', 'es_titular', 'pagar', 'avisos', 'persona_cliente_id')`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	tablas := func() int {
		t.Helper()
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM information_schema.tables WHERE table_schema = 'integracion'
			AND table_name IN ('argos_contacto_telefonos', 'argos_contactos_leidos')`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if columnas() != 5 || tablas() != 2 {
		t.Fatal("antes del Down están las columnas y tablas de la atención")
	}
	if _, err := tx.Exec(ctx, down); err != nil {
		t.Fatalf("down: %v", err)
	}
	if columnas() != 0 || tablas() != 0 {
		t.Fatal("el Down quita solo lo que agregó la migración")
	}
	// Una fila del portal, quitada, como la deja #283 sin la atención.
	if _, err := tx.Exec(ctx, `INSERT INTO integracion.argos_contactos (contacto_id, cliente_id, empresa_id, nombre, consultar, reportar, version, quitado)
		VALUES (987654321, 4242, 1, 'Portal', true, true, 5, true)`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, up); err != nil {
		t.Fatalf("up: %v", err)
	}
	if columnas() != 5 || tablas() != 2 {
		t.Fatal("el Up vuelve a poner columnas y tablas")
	}
	var n int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM integracion.argos_contactos WHERE contacto_id = 987654321 AND relacion = 'otro'
		AND NOT es_titular AND NOT pagar AND NOT avisos AND persona_cliente_id IS NULL AND consultar AND reportar AND quitado AND version = 5`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("la fila del portal queda con los valores seguros y su quitado: %d %v", n, err)
	}
	// Una relación desconocida no entra.
	if _, err := tx.Exec(ctx, "SAVEPOINT relacion"); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE integracion.argos_contactos SET relacion = 'jefe' WHERE contacto_id = 987654321`); err == nil {
		t.Fatal("relación fuera del contrato")
	}
	if _, err := tx.Exec(ctx, "ROLLBACK TO SAVEPOINT relacion"); err != nil {
		t.Fatal(err)
	}
}
