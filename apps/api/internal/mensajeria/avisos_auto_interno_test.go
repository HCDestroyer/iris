package mensajeria

import (
	"slices"
	"testing"
	"time"
)

// Una plantilla de la cuenta con mapa de datos que pide una variable opcional (0.15.0) que el aviso no
// trae no puede enviarlo: nunca se manda vacía (Meta rechaza un parámetro vacío).
func TestFaltaDatoOpcional(t *testing.T) {
	conDatos := func(codigo string, datos ...string) *resuelto {
		pm := PlantillaMeta{Nombre: "x", Idioma: "es", Nombrados: true}
		for _, d := range datos {
			pm.Parametros = append(pm.Parametros, ParametroPWA{Componente: "cuerpo", Nombre: d, Dato: d})
		}
		return &resuelto{a: &whatsappCloud{avisos: map[string]PlantillaMeta{codigo: pm}}}
	}
	factura, _ := PlantillaPorCodigo("factura_por_vencer")
	debito, _ := PlantillaPorCodigo("debito_fallido")
	base := map[string]string{"monto": "1.00", "moneda": "GTQ", "vence_en": "2026-11-10"}
	casos := []struct {
		nombre string
		r      *resuelto
		p      Plantilla
		v      map[string]string
		falta  bool
	}{
		{"factura sin concepto en la plantilla", conDatos("factura_por_vencer", "monto_con_moneda", "fecha_corte"), factura, base, false},
		{"factura que pide concepto sin él", conDatos("factura_por_vencer", "concepto", "monto_con_moneda"), factura, base, true},
		{"factura que pide concepto con él", conDatos("factura_por_vencer", "concepto"), factura,
			map[string]string{"monto": "1.00", "moneda": "GTQ", "vence_en": "2026-11-10", "concepto": "TV"}, false},
		{"débito que pide la fecha del reintento sin ella", conDatos("debito_fallido", "fecha_reintento"), debito,
			map[string]string{"monto": "1.00", "moneda": "GTQ"}, true},
		{"débito que pide reintenta_el sin ella", conDatos("debito_fallido", "reintenta_el"), debito,
			map[string]string{"monto": "1.00", "moneda": "GTQ"}, true},
		{"débito sin reintento en la plantilla", conDatos("debito_fallido", "monto_con_moneda"), debito,
			map[string]string{"monto": "1.00", "moneda": "GTQ"}, false},
		{"simulado", &resuelto{a: simulado{}}, debito, map[string]string{"monto": "1.00", "moneda": "GTQ"}, false},
	}
	for _, c := range casos {
		if got := faltaDatoOpcional(c.r, c.p, c.v); got != c.falta {
			t.Errorf("%s: %v", c.nombre, got)
		}
	}
	// Los derivados en llano: la fecha de corte y la del reintento.
	d := datosDerivados(debito, map[string]string{"monto": "1.00", "moneda": "GTQ", "reintenta_el": "2026-12-01"})
	if d["fecha_reintento"] != "martes 1 de diciembre" || d["monto_con_moneda"] != "Q 1.00" {
		t.Fatalf("derivados del débito: %v", d)
	}
	corte, _ := PlantillaPorCodigo("corte_proximo")
	if d := datosDerivados(corte, map[string]string{"monto": "1.00", "moneda": "GTQ", "fecha_corte": "2026-11-28"}); d["fecha_corte"] != "sábado 28 de noviembre" {
		t.Fatalf("fecha de corte en llano: %v", d)
	}
	if !slices.Contains(datosDeCodigo(debito), "fecha_reintento") || !slices.Contains(datosDeCodigo(factura), "concepto") {
		t.Fatal("datos de los códigos nuevos")
	}
	// Por posición, solo las obligatorias.
	if p := factura.Parametros(map[string]string{"monto": "1.00", "moneda": "GTQ", "vence_en": "2026-11-10", "concepto": "TV"}); len(p) != 3 {
		t.Fatalf("parámetros por posición: %v", p)
	}
	// Vencimiento: una de Iris sin caducidad no vence; una de Argos sin variable, a las 72 h.
	ahora := time.Date(2026, 11, 5, 16, 0, 0, 0, time.UTC)
	boleta, _ := PlantillaPorCodigo("boleta_recibida")
	if boleta.venceEn(nil, time.UTC, ahora) != nil {
		t.Fatal("una plantilla de Iris sin caducidad no vence")
	}
	if v := debito.venceEn(map[string]string{}, time.UTC, ahora); v == nil || !v.Equal(ahora.Add(72*time.Hour)) {
		t.Fatalf("débito sin reintento: %v", v)
	}
}
