package mensajeria

import (
	"strings"
	"testing"
)

// La clave Wi-Fi escrita en el chat no queda en el historial (0.9.0 §12.4); un texto sin ella no cambia.
func TestTaparClaveWifi(t *testing.T) {
	for _, c := range []struct{ entra, sale string }{
		{"la clave de mi wifi es Secreta2026!", "la clave de mi wifi es ***"},
		{"Contraseña del WiFi: casa-perez-99", "Contraseña del WiFi: ***"},
		{"mi wifi, la contraseña nueva sea 'Gatito123'", "mi wifi, la contraseña nueva sea '***"},
		{"no tengo internet desde ayer", "no tengo internet desde ayer"},
		{"la clave es corta", "la clave es corta"},
	} {
		if got := TaparClaveWifi(c.entra); got != c.sale {
			t.Errorf("TaparClaveWifi(%q) = %q, esperaba %q", c.entra, got, c.sale)
		}
	}
}

// Variables de bienvenida_servicio (§12.5): sin enlaces, sin 4 dígitos seguidos, sin saltos de línea.
func TestValidarBienvenida(t *testing.T) {
	for _, c := range []struct {
		nombre, valor string
		ok            bool
	}{
		{"nombre", "Ana Pérez", true}, {"nombre", "Paga en www.estafa.gt", false}, {"nombre", "Cuenta 12345678", false},
		{"plan", "Fibra 100", true}, {"plan", "Plan\nsegundo", false}, {"nombre", strings.Repeat("a", 61), false},
		{"codigo_cliente", "CLI-0091", true}, {"codigo_cliente", "CLI 0091", false},
	} {
		if got := validarBienvenida(c.nombre, c.valor) == ""; got != c.ok {
			t.Errorf("validarBienvenida(%s, %q) = %v, esperaba %v", c.nombre, c.valor, got, c.ok)
		}
	}
	p, _ := PlantillaPorCodigo(PlantillaBienvenidaServicio)
	if txt := p.Render(map[string]string{"nombre": "Ana", "plan": "Fibra", "precio_mensual": "250.00", "moneda": "GTQ", "codigo_cliente": "C-1",
		"cuentas": "BI monetaria 1 a nombre de ISP"}); !strings.Contains(txt, "BI monetaria") || strings.Contains(txt, "{{") {
		t.Fatalf("render: %s", txt)
	}
	if _, ok := PlantillaPorCodigo(PlantillaBienvenidaSinCuentas); !ok {
		t.Fatal("falta la variante sin cuentas")
	}
	for _, c := range CodigosPlantillas() {
		if c == PlantillaBienvenidaSinCuentas {
			t.Fatal("la variante interna no se pide por código")
		}
	}
}

// El enlace solo lo pone el servidor, una vez, y solo si el turno lo creó.
func TestMarcaEnlaceWifi(t *testing.T) {
	tu := &turnoIA{}
	if _, ok := conEnlaceWifi(tu, "Entra a {{enlace_wifi}}"); ok {
		t.Fatal("sin enlace creado, la marca no se pone")
	}
	u := "https://iris.prueba/wifi/abc"
	tu.enlaceWifi = &u
	if s, ok := conEnlaceWifi(tu, "Entra a {{ enlace_wifi }}"); !ok || s != "Entra a "+u {
		t.Fatalf("marca: %q %v", s, ok)
	}
	if _, ok := conEnlaceWifi(tu, "{{enlace_wifi}} y {{enlace_wifi}}"); ok {
		t.Fatal("dos marcas: falla cerrado")
	}
}
