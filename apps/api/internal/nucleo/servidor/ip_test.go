package servidor_test

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
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

// registroIP guarda el log de acceso de la prueba (el handler escribe desde la petición).
type registroIP struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (r *registroIP) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.b.Write(p)
}

func (r *registroIP) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.b.String()
}

// entradaSondaIP hace lo mismo que la encuesta pública y el login: Resolve toma la IP con
// nucleo.HostDe(RemoteAddr).
type entradaSondaIP struct {
	ip string
}

func (e *entradaSondaIP) Resolve(ctx huma.Context) []error {
	e.ip = nucleo.HostDe(ctx.RemoteAddr())
	return nil
}

// La IP que ven los handlers (como la encuesta) y el log de acceso es la del cliente solo detrás
// de un proxy confiable; sin él, un X-Forwarded-For falso se ignora.
func TestIPClienteEnHandlersYLog(t *testing.T) {
	sonda := func(api huma.API) {
		huma.Register(api, huma.Operation{OperationID: "sondaIP", Method: http.MethodGet, Path: "/api/v1/salud/sonda-ip"},
			func(_ context.Context, in *entradaSondaIP) (*struct{ Body string }, error) {
				return &struct{ Body string }{Body: in.ip}, nil
			})
	}
	for _, c := range []struct {
		nombre, confiable, xff, esperada string
	}{
		{"proxy confiable", "127.0.0.0/8", "1.2.3.4, 203.0.113.7", "203.0.113.7"},
		{"proxy confiable, varios saltos", "127.0.0.0/8, 10.0.0.0/8", "1.2.3.4, 203.0.113.7, 10.0.0.9", "203.0.113.7"},
		{"proxy confiable, malformado", "127.0.0.0/8", "203.0.113.7, no-es-ip", "127.0.0.1"},
		{"sin configurar: XFF falso ignorado", "", "203.0.113.7", "127.0.0.1"},
		{"otra red: XFF falso ignorado", "10.0.0.0/8", "203.0.113.7", "127.0.0.1"},
	} {
		log := &registroIP{}
		h, _ := servidor.Nuevo(servidor.Deps{
			BD: pruebas.Pool(t), Identidad: identidad.Nuevo(identidad.Config{}), Log: slog.New(slog.NewJSONHandler(log, nil)),
			ProxiesConfiables: redes(t, c.confiable), Modulos: []func(huma.API){sonda},
		})
		req := httptest.NewRequest(http.MethodGet, "/api/v1/salud/sonda-ip", nil)
		req.RemoteAddr = "127.0.0.1:40000"
		req.Header.Set("X-Forwarded-For", c.xff)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `"`+c.esperada+`"` {
			t.Errorf("%s: el handler vio %d %s, esperaba %s", c.nombre, rec.Code, rec.Body.String(), c.esperada)
		}
		if !strings.Contains(log.String(), `"ip":"`+c.esperada+`"`) {
			t.Errorf("%s: log de acceso %s, esperaba ip %s", c.nombre, log.String(), c.esperada)
		}
	}
}
