package mensajeria

import "testing"

// Bajo 3 de la verificación de #287: una plantilla de Meta de avisos que lleva el nombre, el código o
// el plan del titular no sale a contactos (copias_motivo = sin_plantilla_contacto).
func TestPlantillaConDatosTitular(t *testing.T) {
	w := &whatsappCloud{avisos: map[string]PlantillaMeta{
		"factura_vencida":    {Nombre: "fv", Parametros: []ParametroPWA{{Dato: "cliente_nombre"}, {Dato: "monto"}}},
		"factura_por_vencer": {Nombre: "fpv", Parametros: []ParametroPWA{{Dato: "monto"}, {Dato: "vence_en"}}},
	}}
	r := &resuelto{a: w}
	if !plantillaConDatosTitular(r, "factura_vencida") {
		t.Fatal("con cliente_nombre no hay copias")
	}
	if plantillaConDatosTitular(r, "factura_por_vencer") {
		t.Fatal("una plantilla neutra sí sale a contactos")
	}
	if plantillaConDatosTitular(&resuelto{a: simulado{}}, "factura_vencida") {
		t.Fatal("el canal simulado no tiene plantillas de Meta")
	}
}
