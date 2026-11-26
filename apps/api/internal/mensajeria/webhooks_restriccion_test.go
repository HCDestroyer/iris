package mensajeria_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/HCDestroyer/iris/apps/api/internal/mensajeria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// Cada evento que va a Argos cabe en webhooks_salida_evento_check después de todas las migraciones.
// Las que redefinen la restricción (cuadrilla, vínculos de técnicos, portal de Argos) tienen que unir
// sus listas: si una pisa a otra, el relay no puede programar ese webhook.
func TestRestriccionWebhooksAceptaTodosLosEventos(t *testing.T) {
	pool := pruebas.Pool(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, ev := range mensajeria.EventosWebhook {
		if _, err := tx.Exec(ctx, `SAVEPOINT ev`); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO mensajeria.webhooks_salida (id, evento, empresa_id, payload, ocurrido_en, proximo_intento)
			VALUES ($1, $2, 1, '{}', now(), $3)`, uuid.Must(uuid.NewV7()), ev, time.Now()); err != nil {
			t.Errorf("%s: %v", ev, err)
		}
		if _, err := tx.Exec(ctx, `ROLLBACK TO SAVEPOINT ev`); err != nil {
			t.Fatal(err)
		}
	}
}
