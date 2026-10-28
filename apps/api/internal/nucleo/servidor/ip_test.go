package servidor_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/config"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/servidor"
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
	panel := redes(t, "10.0.0.0/8, 172.16.0.5")
	casos := []struct {
		nombre, remoto string
		xff            []string
		confiables     []netip.Prefix
		esperada       string
	}{
		{"sin proxies confiables se ignora XFF", "10.1.1.1:5000", []string{"203.0.113.7"}, nil, "10.1.1.1"},
		{"conexión no confiable ignora XFF", "198.51.100.9:5000", []string{"203.0.113.7"}, panel, "198.51.100.9"},
		{"desde el panel se toma XFF", "10.1.1.1:5000", []string{"203.0.113.7"}, panel, "203.0.113.7"},
		{"lo falsificado a la izquierda no cuenta", "10.1.1.1:5000", []string{"1.2.3.4, 203.0.113.7"}, panel, "203.0.113.7"},
		{"se saltan los proxies confiables", "10.1.1.1:5000", []string{"203.0.113.7, 172.16.0.5"}, panel, "203.0.113.7"},
		{"varias cabeceras", "10.1.1.1:5000", []string{"1.2.3.4", "203.0.113.8"}, panel, "203.0.113.8"},
		{"IPv6 y puerto", "10.1.1.1:5000", []string{"[2001:db8::1]:443"}, panel, "2001:db8::1"},
		{"entrada mal formada corta", "10.1.1.1:5000", []string{"203.0.113.7, basura"}, panel, "10.1.1.1"},
		{"sin XFF queda el proxy", "10.1.1.1:5000", nil, panel, "10.1.1.1"},
		// Regresión (revisión, hallazgo 8): si todo XFF es confiable no se toma lo de más a la
		// izquierda (lo pudo escribir el cliente si es una IP de la red confiable).
		{"todos confiables: la del socket", "10.1.1.1:5000", []string{"10.2.2.2"}, panel, "10.1.1.1"},
		{"todos confiables, varios saltos: la del socket", "10.1.1.1:5000", []string{"10.9.9.9, 172.16.0.5"}, panel, "10.1.1.1"},
		{"mal formada tras uno confiable: la del socket", "10.1.1.1:5000", []string{"basura, 10.2.2.2"}, panel, "10.1.1.1"},
	}
	for _, c := range casos {
		if got := servidor.IPCliente(c.remoto, c.xff, c.confiables); got != c.esperada {
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

// La IP de la sesión es la del cliente solo si la conexión viene de un proxy confiable.
func TestXForwardedForEnSesion(t *testing.T) {
	pool := pruebas.Pool(t)
	mod := identidad.Nuevo(identidad.Config{CookieSegura: false})
	for _, c := range []struct {
		nombre    string
		confiable string
		esperada  string
	}{
		{"proxy confiable", "127.0.0.0/8", "203.0.113.7"},
		{"sin configurar", "", "127.0.0.1"},
		{"otra red", "10.0.0.0/8", "127.0.0.1"},
	} {
		h, _ := servidor.Nuevo(servidor.Deps{
			BD: pool, Identidad: mod, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
			ProxiesConfiables: redes(t, c.confiable),
		})
		srv := httptest.NewServer(h)
		e := &entorno{t: t, srv: srv, mod: mod}
		_, correo := e.usuario("lectura")
		cl := e.cliente()
		r := cl.pedir(http.MethodPost, "/api/v1/sesion", map[string]any{"correo": correo, "contrasena": contrasena},
			"X-Forwarded-For", "1.2.3.4, 203.0.113.7")
		srv.Close()
		if r.status != 200 {
			t.Fatalf("%s: login %d %s", c.nombre, r.status, r.crudo)
		}
		var ip string
		if err := pool.QueryRow(context.Background(),
			`SELECT s.ip FROM nucleo.sesiones s JOIN nucleo.usuarios u ON u.id = s.usuario_id WHERE u.correo = $1`, correo).Scan(&ip); err != nil {
			t.Fatal(err)
		}
		if ip != c.esperada {
			t.Errorf("%s: ip de la sesión %s, esperaba %s", c.nombre, ip, c.esperada)
		}
	}
}
