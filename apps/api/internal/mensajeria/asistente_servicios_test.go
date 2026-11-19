package mensajeria_test

import (
	"strings"
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/mensajeria"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
)

// Contrato argosisp.md 0.8.0 §2.4 y §10.2 puntos 7 y 8: el asistente distingue los servicios como
// «Servicio n · plan · zona», dice el precio del plan solo a un número registrado y no adivina entre
// dos servicios iguales.
func TestAsistenteServiciosEtiquetaYPrecio(t *testing.T) {
	svs := []mensajeriabd.ServiciosClienteAsistenteRow{
		{ID: 4410, Codigo: "S-4410", PlanNombre: "Fibra 50", ZonaNombre: "Chitá", Estado: "activo", PrecioMensual: "250.00", Moneda: "GTQ", Numero: 1},
		{ID: 4518, Codigo: "S-4518", PlanNombre: "Fibra 50", ZonaNombre: "Chitá", Estado: "suspendido", Numero: 2},
		{ID: 4702, Codigo: "S-4702", PlanNombre: "Negocio 30", ZonaNombre: "San Pedro", Estado: "pendiente_instalacion", PrecioMensual: "399.00", Moneda: "GTQ", Numero: 3},
	}
	registrado := mensajeria.DescribirServiciosIA(svs, true)
	for _, debe := range []string{
		"Servicio 1 · Fibra 50 · Chitá: estado activo.", "Su plan cuesta Q250.00 al mes", "no es el total de la factura",
		"Servicio 2 · Fibra 50 · Chitá: estado suspendido.", "Servicio 3 · Negocio 30 · San Pedro", "Su plan cuesta Q399.00 al mes",
		"Servicio 1 y Servicio 2 tienen el mismo plan y la misma zona", "pasa a una persona",
	} {
		if !strings.Contains(registrado, debe) {
			t.Fatalf("falta %q en:\n%s", debe, registrado)
		}
	}
	// Sin precio en Argos (anterior a 1.18.0): no se dice nada del precio de ese servicio.
	if linea := strings.Split(registrado, "\n")[1]; strings.Contains(linea, "cuesta") {
		t.Fatalf("el servicio sin precio no debía decir precio: %s", linea)
	}
	// Nunca ids ni códigos internos.
	for _, prohibido := range []string{"4410", "S-4410", "4518", "4702"} {
		if strings.Contains(registrado, prohibido) {
			t.Fatalf("el modelo vería %q:\n%s", prohibido, registrado)
		}
	}
	// Número no registrado: sin precios, y se le dice que no los dé.
	desconocido := mensajeria.DescribirServiciosIA(svs, false)
	if strings.Contains(desconocido, "Q250.00") || strings.Contains(desconocido, "Q399.00") || !strings.Contains(desconocido, "no digas precios") {
		t.Fatalf("a un número no registrado no se le dice el precio:\n%s", desconocido)
	}
}
