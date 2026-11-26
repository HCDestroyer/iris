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

// Revisión de seguridad: ningún enlace ajeno (cualquier dominio, también disfrazado con NFKC) ni
// separador Unicode pasa en nombre o plan; lo legítimo sigue pasando.
func TestValidarBienvenidaEnlacesYUnicode(t *testing.T) {
	malos := []string{
		"pagos-claro.ru", "bit.do/x", "evil.shop", "mi-pago.online", "pago\u2024com", "PAGO.COM", "pago．com", "https://x", "www.x",
		"Ana\u2028Pérez", "Ana\u2029Pérez", "Ana\u0085Pérez", "Ana\rPérez", "Ana\u200bPérez", "Ana\u202ePérez", "Ana\x07",
		"Ana    Pérez", "ana@correo", "ruta/abc",
		// IP, puerto y puntos ideográficos.
		"Ve a 203.0.113.5", "Ve a 203.0.113.5:8080", "host:8080", "evil\u3002com", "evil\uff61com",
		// El largo se mide también sobre el texto tal como llega (NFKC lo acorta: «e» + acento es «é»).
		strings.Repeat("e\u0301", 41),
	}
	for _, v := range malos {
		for _, campo := range []string{"nombre", "plan"} {
			if validarBienvenida(campo, v) == "" {
				t.Errorf("%s debía rechazar %q", campo, v)
			}
		}
	}
	for _, v := range []string{"Dr. Pérez", "Ñandú Martínez", "O'Brien", "Ana María"} {
		if msg := validarBienvenida("nombre", v); msg != "" {
			t.Errorf("nombre %q: %s", v, msg)
		}
	}
	for _, v := range []string{"Fibra 100 Mbps", "Plan Hogar 2.0", "Internet 10/5", "IPTV + Internet", "Fibra 100 Mbps (promo)"} {
		if msg := validarBienvenida("plan", v); msg != "" {
			t.Errorf("plan %q: %s", v, msg)
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

// M3 y bajo 7: con cambiar_wifi, «contraseña: …» se tapa aunque no nombre el Wi-Fi; la bienvenida no
// deja pasar 4 dígitos aunque vayan separados.
func TestClaveSueltaYDigitosSeparados(t *testing.T) {
	if got := TaparClaveWifiAmplio("contraseña: Perro2024!"); strings.Contains(got, "Perro2024") {
		t.Fatalf("amplio: %q", got)
	}
	if got := TaparClaveWifi("contraseña: Perro2024!"); got != "contraseña: Perro2024!" {
		t.Fatalf("sin cambiar_wifi no se tapa: %q", got)
	}
	for _, v := range []string{"Cuenta 1-2-3-4", "pague 12 34 56", "1.234.567"} {
		if validarBienvenida("nombre", v) == "" {
			t.Errorf("%q debía rechazarse", v)
		}
	}
}
