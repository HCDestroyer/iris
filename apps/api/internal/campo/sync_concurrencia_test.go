package campo_test

import (
	"context"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/campo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// Un choque con otra transacción (Postgres corta un deadlock: 409 concurrencia) no es un rechazo:
// no se guarda, lo que sigue de esa orden en el lote tampoco se aplica, y el mismo lote vuelve a
// mandarse y se aplica. Antes quedaba «rechazada» y detenía la cola de la orden en el teléfono.
func TestLoteConcurrenciaSeReintenta(t *testing.T) {
	e := nuevoEntorno()
	d, tec := despacho(t), tecnico(t)
	cd, ct := como(t, d, "despacho"), como(t, tec, "tecnico")
	o := programada(t, e, cd, "visita", tec)
	ops := []campo.OperacionLote{
		op(campo.OpLlegar, o.ID, nil),
		op(campo.OpNotaAgregar, o.ID, map[string]any{"texto": "Llegué, el cliente no está"}),
	}

	// Otra transacción tiene el candado del feed del técnico; el lote toma la orden y lo espera;
	// después la otra pide la orden: deadlock. Postgres corta al que esperó primero (el lote).
	ctx := context.Background()
	pool := pruebas.Pool(t)
	otra, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = otra.Rollback(ctx) }()
	if _, err := otra.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended(concat('campo.cambios:', $1::bigint), 0))", tec); err != nil {
		t.Fatal(err)
	}
	resp := make(chan campo.RespuestaLote, 1)
	go func() {
		r, err := e.m.Lote(ct, campo.EntradaLote{DispositivoID: "tel-choque-" + pruebas.Unico(), Plataforma: "pwa", Operaciones: ops})
		if err != nil {
			t.Error(err)
		}
		resp <- r
	}()
	limite := time.Now().Add(15 * time.Second)
	for {
		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname = current_database()
			AND cardinality(pg_blocking_pids(pid)) > 0`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n >= 1 {
			break
		}
		if time.Now().After(limite) {
			t.Fatal("el lote no llegó a esperar el candado del técnico")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := otra.Exec(ctx, "SELECT 1 FROM campo.ordenes WHERE id = $1 FOR UPDATE", o.ID); err != nil {
		t.Fatalf("la otra transacción también fue cortada: %v", err)
	}
	r := <-resp
	if err := otra.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if len(r.Resultados) != 2 {
		t.Fatalf("resultados: %+v", r.Resultados)
	}
	for _, res := range r.Resultados {
		if res.Estado != "rechazada" || res.Error == nil || res.Error.Codigo() != "concurrencia" {
			t.Fatalf("concurrencia en las dos (la segunda no se aplica fuera de orden): %+v", res)
		}
	}
	if n := contar(t, "SELECT count(*) FROM campo.operaciones_sync WHERE id = ANY($1::uuid[])", []string{ops[0].ID, ops[1].ID}); n != 0 {
		t.Fatalf("un choque no se guarda como rechazo: %d", n)
	}

	// El teléfono reintenta con los mismos ids: se aplican.
	r = lote(t, e, ct, ops...)
	for _, res := range r.Resultados {
		if res.Estado != "aplicada" {
			t.Fatalf("reintento: %+v", res)
		}
	}
}
