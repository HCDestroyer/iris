package campo_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/HCDestroyer/iris/apps/api/internal/campo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
	"github.com/HCDestroyer/iris/apps/api/internal/soporte"
)

// Cerrar con éxito una visita o instalación genera su encuesta de satisfacción (soporte) en la
// misma transacción; una parcial o de otro tipo, no.
func TestEncuestaAlCerrarOrden(t *testing.T) {
	sop := soporte.Nuevo(soporte.Config{})
	e := nuevoEntorno(func(c *campo.Config) { c.Encuestas = sop.GenerarEncuestaOrden })
	d, tec := despacho(t), tecnico(t)
	cd, ct := como(t, d, "despacho"), como(t, tec, "tecnico")
	cerrar := func(tipo, resultado string) int64 {
		t.Helper()
		o := programada(t, e, cd, tipo, tec)
		o = deber(e.m.Llegar.Ejecutar(ct, campo.EntradaPosicion{ID: o.ID, Version: o.Version}))(t)
		completarChecklist(t, e, ct, o.ID)
		firma := uuid.Must(uuid.NewV7())
		deber(e.m.CapturarFirma.Ejecutar(ct, campo.EntradaCapturarFirma{ID: o.ID, Nombre: "Cliente",
			SVG: `<svg xmlns="http://www.w3.org/2000/svg"><path d="M0 0L10 10"/></svg>`, AdjuntoID: &firma}))(t)
		o = deber(e.m.VerOrden.Ejecutar(cd, campo.EntradaVerOrden{ID: o.ID}))(t).Orden
		deber(e.m.CerrarOrden.Ejecutar(ct, campo.EntradaCerrarOrden{ID: o.ID, Version: o.Version, Resultado: resultado}))(t)
		return o.ID
	}
	encuestas := func(orden int64) (n int, numero, usuario string) {
		t.Helper()
		if err := pruebas.Pool(t).QueryRow(context.Background(),
			"SELECT count(*), coalesce(max(numero), ''), coalesce(max(usuario_id)::text, '') FROM soporte.encuestas WHERE orden_id = $1",
			orden).Scan(&n, &numero, &usuario); err != nil {
			t.Fatal(err)
		}
		return n, numero, usuario
	}
	visita := cerrar("visita", "")
	if n, numero, usuario := encuestas(visita); n != 1 || numero[:3] != "OT-" || usuario == "" {
		t.Fatalf("encuesta de la visita: %d %s %s", n, numero, usuario)
	}
	var evento string
	if err := pruebas.Pool(t).QueryRow(context.Background(),
		"SELECT payload->>'canal' FROM integracion.outbox WHERE evento = 'encuesta.enviada' AND payload->>'entidad' = 'orden' AND (payload->>'entidad_id')::bigint = $1",
		visita).Scan(&evento); err != nil || evento != "enlace" {
		t.Fatalf("encuesta.enviada: %q %v", evento, err)
	}
	if n, _, _ := encuestas(cerrar("visita", "parcial")); n != 0 {
		t.Fatal("una visita parcial no genera encuesta")
	}
	if n, _, _ := encuestas(cerrar("mantenimiento", "")); n != 0 {
		t.Fatal("un mantenimiento no genera encuesta")
	}
}
