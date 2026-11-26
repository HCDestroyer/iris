package pendientes_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// pendientes_actor_check (enlaces de Wi-Fi, 20261126002325): el Down no borra ni cambia los pendientes
// con actor asistente_ia (también los usa B31, #282) y deja la constraint con asistente_ia; el Up vuelve
// a correr encima sin problema. En una transacción que se revierte.
func TestMigracionEnlacesWifiActorAsistente(t *testing.T) {
	crudo, err := os.ReadFile("../../../migraciones/20261126002325_mensajeria_enlaces_wifi.sql")
	if err != nil {
		t.Fatal(err)
	}
	partes := strings.SplitN(strings.SplitN(string(crudo), "-- +goose Up", 2)[1], "-- +goose Down", 2)
	up, down := partes[0], partes[1]
	ctx := context.Background()
	tx, err := pruebas.Pool(t).Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	insertar := func(clave, actor string) error {
		_, err := tx.Exec(ctx, `INSERT INTO integracion.pendientes (empresa_id, sistema, operacion, idempotency_key, entidad, entidad_id, metodo,
			ruta, payload, depende_de, datos_incompletos, estado, proximo_intento, actor)
			VALUES (1, 'argosisp', 'consumo', $1, 'orden', '1', 'POST', '/x', '{}', '{}', false, 'pendiente', now(), $2)`, clave, actor)
		return err
	}
	clave := "iris:prueba:actor-asistente:" + nuevaOrden()
	if err := insertar(clave, "asistente_ia"); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, down); err != nil {
		t.Fatalf("down: %v", err)
	}
	var estado, actor string
	if err := tx.QueryRow(ctx, "SELECT estado, actor FROM integracion.pendientes WHERE idempotency_key = $1", clave).Scan(&estado, &actor); err != nil {
		t.Fatalf("el Down no borra el pendiente del asistente: %v", err)
	}
	if estado != "pendiente" || actor != "asistente_ia" {
		t.Fatalf("el Down no lo cambia: %s %s", estado, actor)
	}
	if _, err := tx.Exec(ctx, "SAVEPOINT s"); err != nil {
		t.Fatal(err)
	}
	if err := insertar(clave+":otro", "otro_actor"); err == nil {
		t.Fatal("la constraint sigue validando el actor")
	}
	if _, err := tx.Exec(ctx, "ROLLBACK TO SAVEPOINT s"); err != nil {
		t.Fatal(err)
	}
	if err := insertar(clave+":2", "asistente_ia"); err != nil {
		t.Fatalf("tras el Down, asistente_ia sigue valiendo: %v", err)
	}
	if _, err := tx.Exec(ctx, up); err != nil {
		t.Fatalf("up de nuevo: %v", err)
	}
	if err := insertar(clave+":3", "usuario:7"); err != nil {
		t.Fatal(err)
	}
}
