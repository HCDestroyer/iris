package campo

import (
	"testing"
	"time"

	campobd "github.com/HCDestroyer/iris/apps/api/internal/campo/bd"
)

// M-268-1: al cerrar, el material se suma por producto, bodega y quién lo registró: dos técnicos, o
// la app y el panel, con el mismo producto y bodega no se juntan en una línea a nombre de uno.
func TestPayloadCerradaNoJuntaActores(t *testing.T) {
	a, b, bodega := int64(1), int64(2), int64(9)
	d := OrdenDetalle{Materiales: []MaterialOrden{
		{ProductoID: 77, Cantidad: 1, BodegaID: &bodega, registradoPor: &a, desdeApp: true},
		{ProductoID: 77, Cantidad: 2, BodegaID: &bodega, registradoPor: &a, desdeApp: true},
		{ProductoID: 77, Cantidad: 4, BodegaID: &bodega, registradoPor: &b, desdeApp: true},
		{ProductoID: 77, Cantidad: 8, BodegaID: &bodega, registradoPor: &a},
	}}
	materiales, _ := payloadCerrada(campobd.CampoOrden{}, "visita", d)["materiales"].([]map[string]any)
	if len(materiales) != 3 {
		t.Fatalf("una línea por actor y origen: %+v", materiales)
	}
	quiero := []struct {
		cantidad float64
		actor    int64
		app      bool
	}{{3, a, true}, {4, b, true}, {8, a, false}}
	for i, q := range quiero {
		m := materiales[i]
		if m["cantidad"] != q.cantidad || *(m["usuario_id"].(*int64)) != q.actor || m["desde_app"] != q.app {
			t.Fatalf("línea %d: %+v", i, m)
		}
	}
}

// M-7 (0.14.0 §3.10): plazoDelActor. Las 12 h cuentan desde lo primero que pase: que le quiten la
// orden (a su nombre o a su cuadrilla) o que lo quiten de la cuadrilla, aunque siga a su nombre.
func TestPlazoDelActor(t *testing.T) {
	t0 := time.Date(2026, 11, 21, 15, 0, 0, 0, time.UTC)
	h := func(n int) time.Time { return t0.Add(time.Duration(n) * time.Hour) }
	asig := func(en time.Time, tecnico, cuadrilla int64) campobd.AsignacionesDeOrdenRow {
		return campobd.AsignacionesDeOrdenRow{OcurridoEn: en, TraeTecnico: true, TecnicoID: tecnico, TraeCuadrilla: true, CuadrillaID: cuadrilla}
	}
	ptrT := func(x time.Time) *time.Time { return &x }
	const yo, otro, c1, c2 = int64(1), int64(2), int64(10), int64(20)
	miembroHasta := func(hasta *time.Time) []campobd.MembresiasDeRow {
		return []campobd.MembresiasDeRow{{CuadrillaID: c1, Desde: h(-10), Hasta: hasta}}
	}
	casos := []struct {
		nombre     string
		filas      []campobd.AsignacionesDeOrdenRow
		membresias []campobd.MembresiasDeRow
		tenia      bool
		quitado    *time.Time
	}{
		{"a su nombre, sigue", []campobd.AsignacionesDeOrdenRow{asig(h(0), yo, 0)}, nil, true, nil},
		{"a su nombre, reasignada", []campobd.AsignacionesDeOrdenRow{asig(h(0), yo, 0), asig(h(3), otro, 0)}, nil, true, ptrT(h(3))},
		{"por la cuadrilla, sigue", []campobd.AsignacionesDeOrdenRow{asig(h(0), 0, c1)}, miembroHasta(nil), true, nil},
		{"por la cuadrilla, lo quitaron", []campobd.AsignacionesDeOrdenRow{asig(h(0), 0, c1)}, miembroHasta(ptrT(h(2))), true, ptrT(h(2))},
		{"por la cuadrilla, la orden pasó a otra", []campobd.AsignacionesDeOrdenRow{asig(h(0), 0, c1), asig(h(4), 0, c2)}, miembroHasta(nil), true, ptrT(h(4))},
		{"a su nombre y de la cuadrilla, lo quitaron de ella", []campobd.AsignacionesDeOrdenRow{asig(h(0), yo, c1)}, miembroHasta(ptrT(h(2))), true, ptrT(h(2))},
		{"ni a su nombre ni miembro", []campobd.AsignacionesDeOrdenRow{asig(h(0), otro, c2)}, miembroHasta(nil), false, nil},
	}
	for _, c := range casos {
		tenia, quitado := plazoDelActor(c.filas, c.membresias, yo, h(1))
		if tenia != c.tenia || (quitado == nil) != (c.quitado == nil) || (quitado != nil && !quitado.Equal(*c.quitado)) {
			t.Errorf("%s: %v %v", c.nombre, tenia, quitado)
		}
	}
}
