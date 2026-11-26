package campo_test

import (
	"context"
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/campo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// Contrato 0.11.0 §3.9 y §10.6 a/g (B26): desde la app, el bodegaId de un material es la bodega
// personal del técnico o la de su cuadrilla actual en la empresa de la orden; si no, 403
// no_es_miembro antes de mirar existencias, y al quitarlo de la cuadrilla sus lotes con esa bodega se
// cortan en ese momento. Nunca se cambia de bodega en silencio.
func TestLoteMaterialConBodegaDeCuadrilla(t *testing.T) {
	inv := &inventarioFalso{existencias: map[int64][]campo.Existencia{
		9:  {{ProductoID: 77, Cantidad: 50}},
		81: {{ProductoID: 77, Cantidad: 2}},
	}}
	e := nuevoEntorno(func(c *campo.Config) { c.Inventario = inv })
	ctx := context.Background()
	pool := pruebas.Pool(t)
	d, tec := despacho(t), tecnico(t)
	cd, ct := como(t, d, "despacho"), como(t, tec, "tecnico")
	var cuadrilla int64
	if err := pool.QueryRow(ctx, `INSERT INTO agenda.cuadrillas (nombre) VALUES ($1) RETURNING id`, "Bodega "+pruebas.Unico()).Scan(&cuadrilla); err != nil {
		t.Fatal(err)
	}
	bodega := 81_000_000 + cuadrilla // una bodega de Argos solo de esta prueba
	inv.existencias[bodega] = inv.existencias[81]
	if _, err := pool.Exec(ctx, `UPDATE agenda.tecnicos SET cuadrilla_id = $2, bodega_id = 9 WHERE usuario_id = $1`, tec, cuadrilla); err != nil {
		t.Fatal(err)
	}
	// Sin conector, la orden no tiene empresa (0).
	if _, err := pool.Exec(ctx, `INSERT INTO integracion.argos_bodegas_cuadrilla (bodega_id, empresa_id, cuadrilla_id) VALUES ($1, 0, $2)`, bodega, cuadrilla); err != nil {
		t.Fatal(err)
	}
	o := programada(t, e, cd, "instalacion", tec)
	esperar(t, lote(t, e, ct, op(campo.OpIniciarCamino, o.ID, map[string]any{"lat": 14.6, "lon": -90.5}), op(campo.OpLlegar, o.ID, nil)),
		"aplicada", "aplicada")

	// De la cuadrilla (la elegida en la app) y de la suya: aplicadas, con la existencia de esa bodega.
	r := lote(t, e, ct,
		op(campo.OpMaterialUsar, o.ID, map[string]any{"productoId": 77, "cantidad": 3, "bodegaId": bodega}),
		op(campo.OpMaterialUsar, o.ID, map[string]any{"productoId": 77, "cantidad": 1, "bodegaId": 9}))
	esperar(t, r, "aplicada", "aplicada")
	if a := r.Resultados[0].Aviso; a == nil || *a != campo.AvisoExistenciaInsuficiente {
		t.Fatalf("la existencia es la de la bodega elegida (2): %+v", r.Resultados[0])
	}
	det := deber(e.m.VerOrdenPropia.Ejecutar(ct, campo.EntradaVerOrden{ID: o.ID}))(t)
	if len(det.Materiales) != 2 || det.Materiales[0].BodegaID == nil || *det.Materiales[0].BodegaID != bodega ||
		det.Materiales[1].BodegaID == nil || *det.Materiales[1].BodegaID != 9 {
		t.Fatalf("cada material con su bodega: %+v", det.Materiales)
	}

	// Una bodega que no es suya ni de su cuadrilla: 403 no_es_miembro.
	esperar(t, lote(t, e, ct, op(campo.OpMaterialUsar, o.ID, map[string]any{"productoId": 77, "cantidad": 1, "bodegaId": 555})),
		"rechazada:no_es_miembro")

	// Lo quitan de la cuadrilla: desde ese momento, sus lotes con esa bodega se cortan.
	if _, err := pool.Exec(ctx, `UPDATE agenda.tecnicos SET cuadrilla_id = NULL WHERE usuario_id = $1`, tec); err != nil {
		t.Fatal(err)
	}
	r = lote(t, e, ct, op(campo.OpMaterialUsar, o.ID, map[string]any{"productoId": 77, "cantidad": 1, "bodegaId": bodega}))
	esperar(t, r, "rechazada:no_es_miembro")
	if r.Resultados[0].Error.Status != 403 {
		t.Fatalf("403: %+v", r.Resultados[0].Error)
	}
	// Su bodega personal sigue valiendo.
	esperar(t, lote(t, e, ct, op(campo.OpMaterialUsar, o.ID, map[string]any{"productoId": 77, "cantidad": 1, "bodegaId": 9})), "aplicada")
	det = deber(e.m.VerOrdenPropia.Ejecutar(ct, campo.EntradaVerOrden{ID: o.ID}))(t)
	if len(det.Materiales) != 3 {
		t.Fatalf("los rechazados no quedan: %d", len(det.Materiales))
	}
}
