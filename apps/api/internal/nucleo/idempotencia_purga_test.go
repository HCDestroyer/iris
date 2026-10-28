package nucleo_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
	"github.com/HCDestroyer/iris/apps/api/migraciones"
)

func TestMain(m *testing.M) { os.Exit(pruebas.Correr(m)) }

func insertarClave(t *testing.T, clave string, formato int, expira string) {
	t.Helper()
	if _, err := pruebas.Pool(t).Exec(context.Background(), `INSERT INTO nucleo.idempotencia
		(actor, clave, metodo, ruta, hash_peticion, hash_formato, expira_en)
		VALUES ('usuario:1', $1, 'POST', '/api/v1/x', '\x00', $2, now() + $3::interval)`, clave, formato, expira); err != nil {
		t.Fatal(err)
	}
}

func contar(t *testing.T, filtro string) int {
	t.Helper()
	var n int
	if err := pruebas.Pool(t).QueryRow(context.Background(), `SELECT count(*) FROM nucleo.idempotencia WHERE `+filtro).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// correrJob encola el job de la purga en un cliente de River con solo ese worker y espera a
// que termine (como lo corre cmd/worker).
func correrJob(t *testing.T) {
	t.Helper()
	pool := pruebas.Pool(t)
	workers := river.NewWorkers()
	nucleo.RegistrarPurgaIdempotencia(workers, pool)
	cliente, err := river.NewClient(riverpgxv5.New(pool), &river.Config{
		Schema: migraciones.EsquemaRiver, Workers: workers,
		Queues: map[string]river.QueueConfig{river.QueueDefault: {MaxWorkers: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancelar := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancelar()
	eventos, desuscribir := cliente.Subscribe(river.EventKindJobCompleted, river.EventKindJobFailed)
	defer desuscribir()
	if err := cliente.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cliente.Stop(context.Background()) }()
	// Sin la unicidad por período (el periódico la trae): cada llamada de la prueba corre.
	if _, err := cliente.Insert(ctx, nucleo.ArgsPurgaIdempotencia{}, &river.InsertOpts{UniqueOpts: river.UniqueOpts{}}); err != nil {
		t.Fatal(err)
	}
	for {
		select {
		case ev := <-eventos:
			if ev.Job.Kind != (nucleo.ArgsPurgaIdempotencia{}).Kind() {
				continue
			}
			if ev.Kind != river.EventKindJobCompleted {
				t.Fatalf("el job falló: %v", ev.Job.Errors)
			}
			return
		case <-ctx.Done():
			t.Fatal("el job no terminó")
		}
	}
}

// El job borra todo lo vencido y deja lo vigente; las filas de formato 0 (SHA-256 plano, de
// antes del HMAC) desaparecen cuando vencen (como mucho 24 h + el intervalo del job).
func TestPurgaIdempotencia(t *testing.T) {
	pool := pruebas.Pool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `DELETE FROM nucleo.idempotencia`); err != nil {
		t.Fatal(err)
	}
	insertarClave(t, "vencida-vieja", 0, "-1 minute")
	insertarClave(t, "vencida-hmac", 1, "-1 hour")
	insertarClave(t, "vigente-vieja", 0, "23 hours")
	insertarClave(t, "vigente-hmac", 1, "24 hours")

	correrJob(t)
	if n := contar(t, "expira_en < now()"); n != 0 {
		t.Fatalf("quedan %d vencidas", n)
	}
	if n := contar(t, "clave IN ('vigente-vieja', 'vigente-hmac')"); n != 2 {
		t.Fatalf("la purga borró vigentes: quedan %d", n)
	}

	// Pasan 24 h: la vieja vigente también vence y la purga se la lleva; no queda formato 0.
	if _, err := pool.Exec(ctx, `UPDATE nucleo.idempotencia SET expira_en = expira_en - interval '24 hours'`); err != nil {
		t.Fatal(err)
	}
	insertarClave(t, "nueva-hmac", 1, "24 hours")
	if n, err := nucleo.PurgarIdempotencia(ctx, pool); err != nil || n != 2 {
		t.Fatalf("purga tras 24 h: %d %v", n, err)
	}
	if n := contar(t, "hash_formato = 0"); n != 0 {
		t.Fatalf("quedan %d filas de SHA-256 plano", n)
	}
	if n := contar(t, "true"); n != 1 {
		t.Fatalf("solo debe quedar la vigente nueva: %d", n)
	}
}

// El periódico existe, corre al arrancar y es único por período.
func TestTrabajoPeriodicoPurgaIdempotencia(t *testing.T) {
	if nucleo.TrabajoPeriodicoPurgaIdempotencia(0) == nil {
		t.Fatal("sin trabajo periódico")
	}
	if op := (nucleo.ArgsPurgaIdempotencia{}).InsertOpts(); op.UniqueOpts.ByPeriod != 30*time.Minute {
		t.Fatalf("único por período: %+v", op.UniqueOpts)
	}
}
