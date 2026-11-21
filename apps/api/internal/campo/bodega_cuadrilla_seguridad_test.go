package campo_test

import (
	"context"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/campo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// ordenEnSitio: una orden de instalación del técnico, en sitio, con la bodega 9 de su ficha.
func ordenEnSitio(t *testing.T, e entorno, tec int64) (campo.Orden, context.Context) {
	t.Helper()
	d := despacho(t)
	cd, ct := como(t, d, "despacho"), como(t, tec, "tecnico")
	if _, err := pruebas.Pool(t).Exec(context.Background(), `UPDATE agenda.tecnicos SET bodega_id = 9 WHERE usuario_id = $1`, tec); err != nil {
		t.Fatal(err)
	}
	o := programada(t, e, cd, "instalacion", tec)
	esperar(t, lote(t, e, ct, op(campo.OpLlegar, o.ID, nil)), "aplicada")
	return o, ct
}

// M-268-1: una bodega personal del técnico, pero de otra empresa (vista en mi-bodega), no vale en
// una orden de otra empresa: 403 no_es_miembro. B-268-2: y no se pide su existencia a Argos.
func TestLoteBodegaPersonalDeOtraEmpresa(t *testing.T) {
	inv := &inventarioFalso{existencias: map[int64][]campo.Existencia{9: {{ProductoID: 77, Cantidad: 50}}}}
	e := nuevoEntorno(func(c *campo.Config) { c.Inventario = inv })
	tec := tecnico(t)
	otra := 92_000_000 + time.Now().UnixNano()%1_000_000
	if _, err := pruebas.Pool(t).Exec(context.Background(), `INSERT INTO integracion.argos_bodegas_tecnico (bodega_id, empresa_id, usuario_id)
		VALUES ($1, 77, $2)`, otra, tec); err != nil {
		t.Fatal(err)
	}
	inv.existencias[otra] = []campo.Existencia{{ProductoID: 77, Cantidad: 1}}
	o, ct := ordenEnSitio(t, e, tec)
	esperar(t, lote(t, e, ct, op(campo.OpMaterialUsar, o.ID, map[string]any{"productoId": 77, "cantidad": 1, "bodegaId": otra})),
		"rechazada:no_es_miembro")
	// Ni una bodega cualquiera: nunca se consulta su existencia en Argos.
	esperar(t, lote(t, e, ct, op(campo.OpMaterialUsar, o.ID, map[string]any{"productoId": 77, "cantidad": 1, "bodegaId": 555})),
		"rechazada:no_es_miembro")
	inv.mu.Lock()
	defer inv.mu.Unlock()
	if inv.lecturas[otra] != 0 || inv.lecturas[555] != 0 {
		t.Fatalf("no se leen existencias de bodegas que el técnico no puede usar: %v", inv.lecturas)
	}
}

// B-268-3: quitar al técnico de la cuadrilla y un material.usar con la bodega de la cuadrilla, al
// mismo tiempo: la lectura de la membresía toma la ficha FOR SHARE, espera al cambio y lo ve.
func TestLoteBodegaCuadrillaConcurrenteConQuitarMiembro(t *testing.T) {
	inv := &inventarioFalso{existencias: map[int64][]campo.Existencia{9: {{ProductoID: 77, Cantidad: 50}}}}
	e := nuevoEntorno(func(c *campo.Config) { c.Inventario = inv })
	ctx := context.Background()
	pool := pruebas.Pool(t)
	tec := tecnico(t)
	var cuadrilla int64
	if err := pool.QueryRow(ctx, `INSERT INTO agenda.cuadrillas (nombre) VALUES ($1) RETURNING id`, "Barrera "+pruebas.Unico()).Scan(&cuadrilla); err != nil {
		t.Fatal(err)
	}
	bodega := 93_000_000 + cuadrilla
	inv.existencias[bodega] = []campo.Existencia{{ProductoID: 77, Cantidad: 5}}
	if _, err := pool.Exec(ctx, `INSERT INTO integracion.argos_bodegas_cuadrilla (bodega_id, empresa_id, cuadrilla_id) VALUES ($1, 0, $2)`, bodega, cuadrilla); err != nil {
		t.Fatal(err)
	}
	o, ct := ordenEnSitio(t, e, tec)
	if _, err := pool.Exec(ctx, `UPDATE agenda.tecnicos SET cuadrilla_id = $2 WHERE usuario_id = $1`, tec, cuadrilla); err != nil {
		t.Fatal(err)
	}

	// Quitarlo de la cuadrilla está en curso (la ficha tomada, sin confirmar).
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `UPDATE agenda.tecnicos SET cuadrilla_id = NULL, version = version + 1 WHERE usuario_id = $1`, tec); err != nil {
		t.Fatal(err)
	}
	type resultado struct {
		r   campo.RespuestaLote
		err error
	}
	listo := make(chan resultado, 1)
	operacion := op(campo.OpMaterialUsar, o.ID, map[string]any{"productoId": 77, "cantidad": 1, "bodegaId": bodega})
	go func() {
		r, err := e.m.Lote(ct, campo.EntradaLote{DispositivoID: "tel-" + pruebas.Unico(), Plataforma: "pwa", Operaciones: []campo.OperacionLote{operacion}})
		listo <- resultado{r, err}
	}()
	// Barrera: el lote espera el candado de la ficha (no la leyó sin esperar).
	esperando := false
	for i := 0; i < 200 && !esperando; i++ {
		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE wait_event_type = 'Lock' AND query LIKE '%FOR SHARE OF t%'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		esperando = n > 0
		if !esperando {
			time.Sleep(20 * time.Millisecond)
		}
	}
	if !esperando {
		t.Fatal("el lote no esperó el candado de la ficha (FOR SHARE)")
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-listo:
		if r.err != nil {
			t.Fatal(r.err)
		}
		esperar(t, r.r, "rechazada:no_es_miembro")
	case <-time.After(30 * time.Second):
		t.Fatal("el lote no terminó")
	}
}
