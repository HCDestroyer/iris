package formato

import (
	"encoding/json"
	"os"
	"testing"
)

type casos struct {
	Telefonos []struct {
		Tipo     string `json:"tipo"`
		Texto    string `json:"texto"`
		Esperado []struct {
			Original string  `json:"original"`
			E164     *string `json:"e164"`
			Mostrar  string  `json:"mostrar"`
		} `json:"esperado"`
	} `json:"telefonos"`
	Documentos []struct {
		Tipo    string `json:"tipo"`
		Numero  string `json:"numero"`
		Mostrar string `json:"mostrar"`
	} `json:"documentos"`
}

func leerCasos(t *testing.T) casos {
	t.Helper()
	crudo, err := os.ReadFile("testdata/telefonos.json")
	if err != nil {
		t.Fatal(err)
	}
	var c casos
	if err := json.Unmarshal(crudo, &c); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestTelefonosCompartidos(t *testing.T) {
	for _, c := range leerCasos(t).Telefonos {
		obtenido := Telefonos(c.Tipo, c.Texto)
		if len(obtenido) != len(c.Esperado) {
			t.Fatalf("%q: %d números, se esperaban %d (%+v)", c.Texto, len(obtenido), len(c.Esperado), obtenido)
		}
		for i, e := range c.Esperado {
			o := obtenido[i]
			if o.Tipo != c.Tipo || o.Original != e.Original || o.Mostrar != e.Mostrar || (o.E164 == nil) != (e.E164 == nil) ||
				(o.E164 != nil && *o.E164 != *e.E164) {
				t.Errorf("%q[%d] = %+v (e164 %v), se esperaba %+v (e164 %v)", c.Texto, i, o, deref(o.E164), e, deref(e.E164))
			}
		}
	}
}

func TestDocumentosCompartidos(t *testing.T) {
	for _, c := range leerCasos(t).Documentos {
		if o := Documento(c.Tipo, c.Numero); o != c.Mostrar {
			t.Errorf("Documento(%q, %q) = %q, se esperaba %q", c.Tipo, c.Numero, o, c.Mostrar)
		}
	}
}

func TestPrincipal(t *testing.T) {
	tel := append(Telefonos("telefono", "2233-4455"), Telefonos("movil", "1801 / 5512-3344")...)
	if p := Principal(tel); p == nil || deref(p.E164) != "+50255123344" {
		t.Fatalf("el móvil interpretable va primero: %+v", p)
	}
	if p := Principal(Telefonos("telefono", "ext 12")); p == nil || p.Original != "ext 12" {
		t.Fatalf("sin ninguno interpretable, el primero tal cual: %+v", p)
	}
	if Principal(nil) != nil {
		t.Fatal("sin teléfonos, nil")
	}
}

func TestCoordenadas(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	lat, lon := Coordenadas(f(14.63491234567), f(-90.50690000001))
	if lat == nil || *lat != 14.634912 || *lon != -90.5069 {
		t.Fatalf("redondeo a 6 decimales: %v, %v", lat, lon)
	}
	for _, c := range [][2]*float64{{f(0), f(0)}, {f(91), f(0)}, {f(14), f(-181)}, {nil, f(1)}, {f(1), nil}} {
		if la, lo := Coordenadas(c[0], c[1]); la != nil || lo != nil {
			t.Errorf("%v,%v debía quedar sin coordenadas", deref(c[0]), deref(c[1]))
		}
	}
}

func deref[T any](p *T) any {
	if p == nil {
		return nil
	}
	return *p
}
