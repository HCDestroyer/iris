package nucleo_test

import (
	"net/netip"
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/config"
)

func redes(t *testing.T, v string) []netip.Prefix {
	t.Helper()
	r, err := config.ParsearRedes(v)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestIPCliente(t *testing.T) {
	// El panel (Next) en la red del compose y un balanceador delante en 172.16.0.5.
	panel := redes(t, "10.0.0.0/8, 172.16.0.5")
	casos := []struct {
		nombre, remoto string
		xff            []string
		confiables     []netip.Prefix
		esperada       string
	}{
		// Sin proxy confiable, un X-Forwarded-For falso se ignora.
		{"sin proxies confiables se ignora XFF", "10.1.1.1:5000", []string{"203.0.113.7"}, nil, "10.1.1.1"},
		{"conexión no confiable ignora XFF", "198.51.100.9:5000", []string{"203.0.113.7"}, panel, "198.51.100.9"},
		{"cliente directo que falsifica una IP confiable", "198.51.100.9:5000", []string{"10.1.1.1"}, panel, "198.51.100.9"},
		// Con proxy confiable, la IP del cliente.
		{"desde el panel se toma XFF", "10.1.1.1:5000", []string{"203.0.113.7"}, panel, "203.0.113.7"},
		// Varios saltos: de derecha a izquierda, la primera no confiable.
		{"lo falsificado a la izquierda no cuenta", "10.1.1.1:5000", []string{"1.2.3.4, 203.0.113.7"}, panel, "203.0.113.7"},
		{"se saltan los proxies confiables", "10.1.1.1:5000", []string{"203.0.113.7, 172.16.0.5"}, panel, "203.0.113.7"},
		{"tres saltos con dos proxies", "10.1.1.1:5000", []string{"9.9.9.9, 203.0.113.7, 172.16.0.5, 10.3.3.3"}, panel, "203.0.113.7"},
		{"varias cabeceras", "10.1.1.1:5000", []string{"1.2.3.4", "203.0.113.8"}, panel, "203.0.113.8"},
		{"espacios y comas vacías", "10.1.1.1:5000", []string{" , 203.0.113.9 ,, "}, panel, "203.0.113.9"},
		{"IPv6 y puerto", "10.1.1.1:5000", []string{"[2001:db8::1]:443"}, panel, "2001:db8::1"},
		{"IPv4 mapeada en IPv6", "[::ffff:10.1.1.1]:5000", []string{"::ffff:203.0.113.7"}, panel, "203.0.113.7"},
		// Malformado: cae en la IP del socket (RemoteAddr).
		{"entrada mal formada corta", "10.1.1.1:5000", []string{"203.0.113.7, basura"}, panel, "10.1.1.1"},
		{"mal formada tras uno confiable: la del socket", "10.1.1.1:5000", []string{"basura, 10.2.2.2"}, panel, "10.1.1.1"},
		{"cabecera inyectada", "10.1.1.1:5000", []string{"203.0.113.7\r\nX-Otra: 1"}, panel, "10.1.1.1"},
		{"sin XFF queda el proxy", "10.1.1.1:5000", nil, panel, "10.1.1.1"},
		// Si todo XFF es confiable no se toma lo de más a la izquierda (lo pudo escribir el
		// cliente si es una IP de la red confiable).
		{"todos confiables: la del socket", "10.1.1.1:5000", []string{"10.2.2.2"}, panel, "10.1.1.1"},
		{"todos confiables, varios saltos: la del socket", "10.1.1.1:5000", []string{"10.9.9.9, 172.16.0.5"}, panel, "10.1.1.1"},
		{"RemoteAddr sin puerto", "10.1.1.1", []string{"203.0.113.7"}, panel, "203.0.113.7"},
	}
	for _, c := range casos {
		if got := nucleo.IPCliente(c.remoto, c.xff, c.confiables); got != c.esperada {
			t.Errorf("%s: %s, esperaba %s", c.nombre, got, c.esperada)
		}
	}
	if _, err := config.ParsearRedes("10.0.0.0/33"); err == nil {
		t.Error("CIDR inválido aceptado")
	}
	if _, err := config.ParsearRedes("no-es-ip"); err == nil {
		t.Error("IP inválida aceptada")
	}
}

func TestHostDe(t *testing.T) {
	for entrada, esperada := range map[string]string{
		"203.0.113.7:5000":     "203.0.113.7",
		"203.0.113.7":          "203.0.113.7",
		"[2001:db8::1]:443":    "2001:db8::1",
		"2001:db8::1":          "2001:db8::1",
		"[::ffff:10.1.1.1]:80": "10.1.1.1",
		"pipe":                 "pipe",
		"":                     "",
	} {
		if got := nucleo.HostDe(entrada); got != esperada {
			t.Errorf("HostDe(%q) = %q, esperaba %q", entrada, got, esperada)
		}
	}
}
