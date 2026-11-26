package campo_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// El Down de 20261125220005 vuelve al índice único por (orden, acción, unidad): con dos intentos de
// recuperación de la misma unidad en una orden (que el Up permite) no falla, porque antes borra los
// no_recuperado. En una transacción que se revierte.
func TestMigracionEnviadoAlAplicarDownConIntentosRepetidos(t *testing.T) {
	crudo, err := os.ReadFile("../../migraciones/20261125220005_campo_equipos_enviado_al_aplicar.sql")
	if err != nil {
		t.Fatal(err)
	}
	down := strings.SplitN(string(crudo), "-- +goose Down", 2)[1]
	e := nuevoEntorno()
	o, _ := ordenEnSitio(t, e, tecnico(t))
	ctx := context.Background()
	tx, err := pruebas.Pool(t).Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for i := range 2 {
		if _, err := tx.Exec(ctx, `INSERT INTO campo.orden_equipos (id, orden_id, accion, unidad_id, motivo, lat, lon, ocurrido_en)
			VALUES (gen_random_uuid(), $1, 'no_recuperado', 7301, 'cliente_ausente', 14.6, -90.5, now() - make_interval(days => $2::int))`, o.ID, i); err != nil {
			t.Fatalf("intento %d: %v", i, err)
		}
	}
	if _, err := tx.Exec(ctx, down); err != nil {
		t.Fatalf("down: %v", err)
	}
	var n int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM campo.orden_equipos WHERE orden_id = $1 AND accion = 'no_recuperado'`, o.ID).Scan(&n); err != nil || n != 0 {
		t.Fatalf("quedan intentos: %d %v", n, err)
	}
}
