package comercial_test

import (
	"context"
	"net/http"
	"slices"
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	argosbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/argos/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// Contrato 0.15.0 §4.6 y §13.3 (bloque C): capacidades.mensajeria[] solo a un Argos ≥ 0.15.0, con las
// empresas del alcance del conector (todas las registradas con alcance global).
func TestCapacidadesMensajeria0150(t *testing.T) {
	p := nuevoPortalArgos(t)
	bd := pruebas.Pool(t)
	ctx := context.Background()
	var antes struct {
		contrato *string
		empresas []int64
		global   *bool
	}
	if err := bd.QueryRow(ctx, `SELECT contrato_remoto, empresas, alcance_global FROM integracion.conectores WHERE id = $1`, p.e.conID).
		Scan(&antes.contrato, &antes.empresas, &antes.global); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = bd.Exec(ctx, `UPDATE integracion.conectores SET contrato_remoto = $2, empresas = $3, alcance_global = $4 WHERE id = $1`,
			p.e.conID, antes.contrato, antes.empresas, antes.global)
	})
	fijar := func(contrato string, empresas []int64, global bool) {
		t.Helper()
		if _, err := bd.Exec(ctx, `UPDATE integracion.conectores SET contrato_remoto = $2, empresas = $3, alcance_global = $4 WHERE id = $1`,
			p.e.conID, contrato, empresas, global); err != nil {
			t.Fatal(err)
		}
	}
	var pedidas []int64
	p.e.am.FijarCapacidadesMensajeria(func(_ context.Context, _ argosbd.DBTX, empresas []int64) ([]argos.CapacidadMensajeria, error) {
		pedidas = empresas
		out := []argos.CapacidadMensajeria{}
		for _, e := range empresas {
			out = append(out, argos.CapacidadMensajeria{EmpresaID: e, EntregaAuto: e == 1,
				Canales: []argos.CanalMensajeriaCap{{Canal: "whatsapp", Codigos: []string{"corte_proximo"}}}})
		}
		return out, nil
	})
	c := &clienteHTTP{t: t, base: p.base, http: http.DefaultClient}
	leer := func() map[string]any {
		t.Helper()
		pedidas = nil
		r := c.pedir(http.MethodGet, argos.RutaCapacidades, nil, "Authorization", "Bearer "+p.panel).ok(t, http.StatusOK)
		return r["capacidades"].(map[string]any)
	}

	// Argos < 0.15.0: sin la marca (Argos entiende entrega_auto false para todas).
	fijar("0.14.3", []int64{1}, false)
	bloques := func(caps map[string]any, encendidos bool) {
		t.Helper()
		if caps["tipo_servicio"] != encendidos || caps["corte_manual"] != encendidos || (caps["mensajeria"] != nil) != encendidos {
			t.Fatalf("bloques de la 0.15.0 (esperaba %v): %v", encendidos, caps)
		}
	}
	if caps := leer(); caps["mensajeria"] != nil || pedidas != nil {
		t.Fatalf("mensajeria con Argos 0.14.3: %v", caps)
	}
	bloques(leer(), false)
	// Argos 0.15.0, pero el contrato acordado (el que Iris informó y Argos aceptó) es 0.14.3: sin la marca.
	fijar("0.15.0", []int64{1}, false)
	if _, err := bd.Exec(ctx, `UPDATE integracion.conectores SET contrato_informado = '0.14.3' WHERE id = $1`, p.e.conID); err != nil {
		t.Fatal(err)
	}
	if caps := leer(); caps["mensajeria"] != nil || pedidas != nil {
		t.Fatalf("mensajeria con contrato acordado 0.14.3: %v", caps)
	}
	bloques(leer(), false)
	if _, err := bd.Exec(ctx, `UPDATE integracion.conectores SET contrato_informado = NULL WHERE id = $1`, p.e.conID); err != nil {
		t.Fatal(err)
	}
	// Argos 0.15.0: solo las empresas del alcance del conector.
	fijar("0.15.0", []int64{1, 2}, false)
	caps := leer()
	// Con Iris 0.15.0 (ContratoIris) y el contrato acordado 0.15.0: los tres bloques encendidos.
	bloques(caps, true)
	filas, ok := caps["mensajeria"].([]any)
	if !ok || len(filas) != 2 || !slices.Equal(pedidas, []int64{1, 2}) {
		t.Fatalf("mensajeria con Argos 0.15.0: %v (pedidas %v)", caps, pedidas)
	}
	f := filas[0].(map[string]any)
	if f["empresa_id"] != float64(1) || f["entrega_auto"] != true || len(f["canales"].([]any)) != 1 {
		t.Fatalf("fila: %v", f)
	}
	// Con alcance global, todas las empresas registradas en Iris.
	fijar("0.15.0", []int64{}, true)
	leer()
	var todas []int64
	filasE, err := bd.Query(ctx, `SELECT id FROM nucleo.empresas ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	for filasE.Next() {
		var id int64
		if err := filasE.Scan(&id); err != nil {
			t.Fatal(err)
		}
		todas = append(todas, id)
	}
	if !slices.Equal(pedidas, todas) {
		t.Fatalf("alcance global: %v (esperaba %v)", pedidas, todas)
	}
	// Una relectura sigue trayéndola.
	if _, hay := leer()["mensajeria"]; !hay {
		t.Fatal("capacidades sin mensajeria con Argos 0.15.0")
	}
}
