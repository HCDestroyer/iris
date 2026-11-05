package servidor_test

import (
	"bufio"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

// apiConDestino arma la API (WriteTimeout y ReadTimeout de 1 s) que delega /api/v1/mensajeria en
// destino.
func apiConDestino(t *testing.T, destino http.Handler) string {
	t.Helper()
	d := httptest.NewServer(destino)
	t.Cleanup(d.Close)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	proxy, err := servidor.ProxyHacia(d.URL, "Mensajería", log)
	if err != nil {
		t.Fatal(err)
	}
	h, _ := servidor.Nuevo(servidor.Deps{Log: log, Delegados: map[string]http.Handler{"/api/v1/mensajeria": proxy}})
	api := httptest.NewUnstartedServer(h)
	api.Config.WriteTimeout, api.Config.ReadTimeout = time.Second, time.Second
	api.Start()
	t.Cleanup(api.Close)
	return api.URL
}

// leerIDs abre el flujo y manda cada línea «id: …» por el canal (se cierra al terminar el flujo).
func leerIDs(t *testing.T, url string) <-chan string {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Accept", "text/event-stream")
	res, err := http.DefaultClient.Do(req) //nolint:bodyclose // lo cierra la goroutine que lee
	if err != nil {
		t.Fatal(err)
	}
	lineas := make(chan string, 100)
	go func() {
		defer func() { _ = res.Body.Close() }()
		s := bufio.NewScanner(res.Body)
		for s.Scan() {
			if strings.HasPrefix(s.Text(), "id: ") {
				lineas <- s.Text()
			}
		}
		close(lineas)
	}()
	return lineas
}

// destinoCadaTanto manda un evento cada intervalo hasta que se corta la petición.
func destinoCadaTanto(intervalo time.Duration) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		rc := http.NewResponseController(w)
		_ = rc.Flush()
		for i := 1; ; i++ {
			select {
			case <-time.After(intervalo):
			case <-r.Context().Done():
				return
			}
			_, _ = fmt.Fprintf(w, "id: %d\nevent: mensaje_nuevo\ndata: {}\n\n", i)
			if rc.Flush() != nil {
				return
			}
		}
	})
}

// cuantosHasta cuenta los eventos hasta que el flujo se cierra (o falla a los 6 s).
func cuantosHasta(t *testing.T, lineas <-chan string) (int, time.Duration) {
	t.Helper()
	inicio := time.Now()
	n := 0
	limite := time.After(6 * time.Second)
	for {
		select {
		case _, ok := <-lineas:
			if !ok {
				return n, time.Since(inicio)
			}
			n++
		case <-limite:
			t.Fatalf("el flujo no se cerró (%d eventos)", n)
		}
	}
}

// El flujo SSE del chat (GET /api/v1/mensajeria/chat/eventos) pasa por el proxy sin búfer (cada
// evento llega al escribirse) y sobrevive el WriteTimeout y el ReadTimeout de la API (aquí 1 s).
func TestProxyFlujoSSESinBuferNiPlazo(t *testing.T) {
	defer servidor.FijarPlazosSSE(5*time.Second, 10*time.Second)()
	siguiente := make(chan struct{})
	url := apiConDestino(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		rc := http.NewResponseController(w)
		_ = rc.Flush() // como el chat: las cabeceras salen enseguida (retry: …)
		for i := 1; i <= 3; i++ {
			select {
			case <-siguiente:
			case <-r.Context().Done():
				return
			}
			_, _ = fmt.Fprintf(w, "id: %d\nevent: mensaje_nuevo\ndata: {}\n\n", i)
			_ = rc.Flush()
		}
	}))
	lineas := leerIDs(t, url+servidor.RutaFlujoSSE)
	for i := 1; i <= 3; i++ {
		if i == 3 {
			time.Sleep(1500 * time.Millisecond) // pasa el WriteTimeout y el ReadTimeout de la API
		}
		select {
		case siguiente <- struct{}{}:
		case <-time.After(3 * time.Second):
			t.Fatalf("el destino ya no espera el evento %d: se cortó el flujo", i)
		}
		select {
		case l, ok := <-lineas:
			if !ok || l != fmt.Sprintf("id: %d", i) {
				t.Fatalf("evento %d: %q (abierto %v)", i, l, ok)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("el evento %d no llegó: el proxy lo retuvo en un búfer", i)
		}
	}
	get := func(metodo, ruta string) *http.Request {
		r, _ := http.NewRequest(metodo, "http://api"+ruta, nil)
		return r
	}
	if !servidor.EsFlujoSSE(get(http.MethodGet, servidor.RutaFlujoSSE)) || servidor.EsFlujoSSE(get(http.MethodPost, servidor.RutaFlujoSSE)) ||
		servidor.EsFlujoSSE(get(http.MethodGet, "/api/v1/mensajeria/plantillas")) || servidor.EsFlujoSSE(get(http.MethodGet, servidor.RutaFlujoSSE+"/x")) {
		t.Fatal("EsFlujoSSE: solo GET a la ruta exacta")
	}
}

// El plazo del flujo nunca es cero: el de escritura se renueva con cada evento (más corto que el
// WriteTimeout de la API, el flujo sigue mientras haya eventos) y hay un máximo absoluto.
func TestProxyFlujoSSEPlazoMovilYMaximo(t *testing.T) {
	defer servidor.FijarPlazosSSE(400*time.Millisecond, 2*time.Second)()
	n, dur := cuantosHasta(t, leerIDs(t, apiConDestino(t, destinoCadaTanto(150*time.Millisecond))+servidor.RutaFlujoSSE))
	if dur < 1700*time.Millisecond || dur > 3500*time.Millisecond || n < 9 {
		t.Fatalf("el flujo duró %v con %d eventos (esperaba ~2 s, el máximo)", dur, n)
	}
}

// Otra ruta delegada con Accept: text/event-stream conserva los plazos de la API (1 s).
func TestProxyOtraRutaConservaPlazos(t *testing.T) {
	_, dur := cuantosHasta(t, leerIDs(t, apiConDestino(t, destinoCadaTanto(150*time.Millisecond))+"/api/v1/mensajeria/plantillas"))
	if dur > 1700*time.Millisecond {
		t.Fatalf("otra ruta mantuvo el flujo %v: perdió el WriteTimeout de la API", dur)
	}
}
