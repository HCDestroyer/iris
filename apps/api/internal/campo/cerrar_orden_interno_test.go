package campo

import (
	"testing"

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
		if m["cantidad"] != q.cantidad || *(m["registrado_por"].(*int64)) != q.actor || m["desde_app"] != q.app {
			t.Fatalf("línea %d: %+v", i, m)
		}
	}
}
