package notificaciones_test

import (
	"testing"

	"github.com/google/uuid"

	"github.com/HCDestroyer/iris/apps/api/internal/notificaciones"
)

// B20: la instalación cambió el plan o el titular de una venta → campana del vendedor, una vez
// por cotización nueva; sin vendedor, nada.
func TestAvisoVentaRevisada(t *testing.T) {
	x := nuevoEntornoAvisos(t, false)
	vendedor := x.persona([]int64{1})
	empresa := int64(1)
	in := notificaciones.EntradaAvisarVentaRevisada{EventoID: uuid.Must(uuid.NewV7()), ProspectoID: 41, EmpresaID: &empresa,
		VendedorID: &vendedor.id, OrdenID: 9, Motivo: "plan", CotizacionID: 70001}
	for i, quiero := range []int{1, 0} {
		r, err := x.notif.AvisarVentaRevisada.Ejecutar(x.ctx, in)
		if err != nil {
			t.Fatal(err)
		}
		if r.Avisos != quiero {
			t.Fatalf("vuelta %d: %d avisos, esperaba %d", i, r.Avisos, quiero)
		}
	}
	var n int
	if err := x.bd.QueryRow(x.ctx, `SELECT count(*) FROM notificaciones.avisos WHERE tipo = 'venta_revisada' AND usuario_id = $1
		AND enlace = '/comercial/41' AND titulo = 'La instalación cambió el plan de tu venta'`, vendedor.id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("avisos del vendedor: %d", n)
	}
	in.VendedorID, in.CotizacionID = nil, 70002
	if r, err := x.notif.AvisarVentaRevisada.Ejecutar(x.ctx, in); err != nil || r.Avisos != 0 {
		t.Fatalf("sin vendedor: %+v %v", r, err)
	}
}
