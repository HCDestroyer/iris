package servidor_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/servidor"
)

// La API reenvía /api/v1/mensajeria al proceso de mensajería tal cual (cabeceras, cuerpo, request
// id) y agrega la IP de quien conectó a X-Forwarded-For; sin destino, 503 problem+json. El resto de
// las rutas no se reenvía.
func TestDelegadosReenvianAlOtroProceso(t *testing.T) {
	var visto *http.Request
	var cuerpo string
	destino := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		visto = r.Clone(r.Context())
		b, _ := io.ReadAll(r.Body)
		cuerpo = string(b)
		w.Header().Set("Idempotent-Replayed", "true")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"aviso_id":"x"}`))
	}))
	defer destino.Close()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	proxy, err := servidor.ProxyHacia(destino.URL, "Mensajería", log)
	if err != nil {
		t.Fatal(err)
	}
	h, _ := servidor.Nuevo(servidor.Deps{Log: log, Delegados: map[string]http.Handler{"/api/v1/mensajeria": proxy}})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/mensajeria/avisos?x=1", strings.NewReader(`{"a":1}`))
	req.RemoteAddr = "172.28.10.10:5555"
	req.Header.Set("Authorization", "Bearer iris_pat_x")
	req.Header.Set("Idempotency-Key", "argos:aviso:x:1")
	req.Header.Set("X-Forwarded-For", "203.0.113.7")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusAccepted || w.Header().Get("Idempotent-Replayed") != "true" || w.Body.String() != `{"aviso_id":"x"}` {
		t.Fatalf("respuesta: %d %v %s", w.Code, w.Header(), w.Body.String())
	}
	if visto == nil || visto.URL.RequestURI() != "/api/v1/mensajeria/avisos?x=1" || cuerpo != `{"a":1}` {
		t.Fatalf("petición reenviada: %+v %q", visto, cuerpo)
	}
	if visto.Header.Get("Authorization") != "Bearer iris_pat_x" || visto.Header.Get("Idempotency-Key") != "argos:aviso:x:1" {
		t.Fatalf("cabeceras: %v", visto.Header)
	}
	if visto.Header.Get("X-Forwarded-For") != "203.0.113.7, 172.28.10.10" || visto.Header.Get("X-Request-Id") == "" ||
		visto.Header.Get("X-Request-Id") != w.Header().Get("X-Request-Id") {
		t.Fatalf("X-Forwarded-For / request id: %v", visto.Header)
	}

	// Otra ruta no se reenvía (la atiende la API: aquí, salud).
	visto = nil
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/mensajeriax", nil))
	if visto != nil || w.Code != http.StatusNotFound {
		t.Fatalf("/api/v1/mensajeriax no es del prefijo: %d", w.Code)
	}

	// Sin destino configurado: 503 servicio_no_disponible.
	sin, _ := servidor.ProxyHacia("", "Mensajería", log)
	h2, _ := servidor.Nuevo(servidor.Deps{Log: log, Delegados: map[string]http.Handler{"/api/v1/mensajeria": sin}})
	w = httptest.NewRecorder()
	h2.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/mensajeria/plantillas", nil))
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "servicio_no_disponible") ||
		w.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("sin destino: %d %s", w.Code, w.Body.String())
	}
	// Destino caído: 502.
	destino.Close()
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/mensajeria/plantillas", nil))
	if w.Code != http.StatusBadGateway {
		t.Fatalf("destino caído: %d", w.Code)
	}
}
