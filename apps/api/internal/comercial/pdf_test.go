package comercial

import (
	"bytes"
	"testing"
	"time"
)

// Mismos datos, mismos bytes: volver a generar un PDF no cambia su SHA-256 (el alta del adjunto
// es idempotente).
func TestPDFDeterminista(t *testing.T) {
	m := Nuevo(Config{})
	creado := time.Date(2026, 10, 28, 15, 0, 0, 0, time.UTC)
	d := datosCotizacionPDF{Numero: "COT-000001", Moneda: "GTQ", Revision: 1, Creada: creado, VigenteHasta: creado.Add(48 * time.Hour),
		Cliente: clientePDF{Nombre: "Ana López Ñandú", Direccion: "Zona 1"}, TotalMensual: "250.00", TotalUnico: "0.00",
		Lineas: []LineaCotizacion{{Tipo: "plan", Descripcion: "Fibra 100", Cantidad: "1.00", PrecioUnitario: "250.00", Total: "250.00", Recurrente: true}}}
	a, err := m.pdfCotizacion(d)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := m.pdfCotizacion(d)
	if !bytes.Equal(a, b) || !bytes.HasPrefix(a, []byte("%PDF-")) {
		t.Fatal("el PDF de la cotización no es determinista")
	}
	c := datosContratoPDF{Creado: creado, Contenido: ContenidoContrato{Numero: "CTR-000001", Fecha: creado, Lineas: d.Lineas, Clausulas: clausulasEstandar(10)},
		Firma: &firmaPDF{Nombre: "Ana", En: creado, SVG: `<svg width="100" height="50"><path d="M 1 1 L 50 40 Q 60 10 90 30"/></svg>`}}
	x, err := m.pdfContrato(c)
	if err != nil {
		t.Fatal(err)
	}
	y, _ := m.pdfContrato(c)
	if !bytes.Equal(x, y) {
		t.Fatal("el PDF del contrato no es determinista")
	}
}

func TestMontos(t *testing.T) {
	for entrada, c := range map[string]int64{"0": 0, "1": 100, "1.5": 150, "150.50": 15050, "999999999999.99": 99_999_999_999_999} {
		v, err := leerMonto("x", entrada, maxCentavos)
		if err != nil || v != c {
			t.Fatalf("%s → %d %v", entrada, v, err)
		}
		if centavos(aNumeric(c)) != c || aTexto(c) == "" {
			t.Fatalf("ida y vuelta de %d", c)
		}
	}
	for _, malo := range []string{"", "-1", "+1", "1.", "1.234", "a", ".5", "1e3"} {
		if _, err := leerMonto("x", malo, maxCentavos); err == nil {
			t.Fatalf("%q debería fallar", malo)
		}
	}
	if multiplicar(250, 15050) != 37625 || multiplicar(333, 1) != 3 || aTexto(-5025) != "-50.25" {
		t.Fatal("aritmética de montos")
	}
	if d := Haversine(14.6, -90.5, 14.6+100.0/111_195, -90.5); d < 99.5 || d > 100.5 {
		t.Fatalf("haversine: %f", d)
	}
}
