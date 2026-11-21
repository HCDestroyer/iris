package servidor_test

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

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

// rutasFlujo: los flujos SSE que la API reenvía a la mensajería (la lista cerrada
// rutasFlujoSSE de proxy.go). Literales a propósito: si una sale de la lista, su prueba falla.
var rutasFlujo = []string{
	"/api/v1/mensajeria/chat/eventos",
	"/api/v1/mensajeria/conversaciones/eventos", // la bandeja: antes se cortaba a los 60 s
}

// Los flujos SSE (el del chat y el de la bandeja) pasan por el proxy sin búfer (cada evento llega
// al escribirse) y sobreviven el WriteTimeout y el ReadTimeout de la API (aquí 1 s).
func TestProxyFlujoSSESinBuferNiPlazo(t *testing.T) {
	defer servidor.FijarPlazosSSE(5*time.Second, 10*time.Second)()
	for _, ruta := range rutasFlujo {
		t.Run(ruta, func(t *testing.T) { probarFlujoVivo(t, ruta) })
	}
	get := func(metodo, ruta string) *http.Request {
		r, _ := http.NewRequest(metodo, "http://api"+ruta, nil)
		return r
	}
	for _, ruta := range rutasFlujo {
		if !servidor.EsFlujoSSE(get(http.MethodGet, ruta)) {
			t.Fatalf("EsFlujoSSE(GET %s) = false", ruta)
		}
		for _, no := range []*http.Request{get(http.MethodPost, ruta), get(http.MethodHead, ruta), get(http.MethodGet, ruta+"/x"),
			get(http.MethodGet, ruta+"/"), get(http.MethodGet, strings.TrimSuffix(ruta, "/eventos"))} {
			if servidor.EsFlujoSSE(no) {
				t.Fatalf("EsFlujoSSE(%s %s): solo GET a la ruta exacta", no.Method, no.URL.Path)
			}
		}
	}
	// Escapes: Path llega decodificado y el destino enruta por RawPath; nunca heredan la exención.
	for _, ruta := range []string{"/api/v1/mensajeria/plantillas", "/api/v1/mensajeria/eventos",
		"/api/v1/mensajeria/conversaciones/1/eventos", "/api/v1/mensajeria/avisos/eventos",
		"/api/v1/mensajeria/conversaciones/%65ventos", "/api/v1/mensajeria/chat/%65ventos",
		"/api/v1/mensajeria%2Fconversaciones/eventos", "/api/v1/mensajeria/chat%2Feventos"} {
		if servidor.EsFlujoSSE(get(http.MethodGet, ruta)) {
			t.Fatalf("EsFlujoSSE(GET %s): no está en la lista cerrada", ruta)
		}
	}
	// Con cuerpo (Content-Length o chunked) no: la exención se decide antes de autenticar.
	for _, ruta := range rutasFlujo {
		conLargo, _ := http.NewRequest(http.MethodGet, "http://api"+ruta, strings.NewReader("x"))
		chunked := get(http.MethodGet, ruta)
		chunked.ContentLength, chunked.TransferEncoding = -1, []string{"chunked"}
		if servidor.EsFlujoSSE(conLargo) || servidor.EsFlujoSSE(chunked) {
			t.Fatalf("EsFlujoSSE(GET %s con cuerpo): solo sin cuerpo", ruta)
		}
	}
}

// probarFlujoVivo: tres eventos por ruta, el tercero después del WriteTimeout de la API.
func probarFlujoVivo(t *testing.T, ruta string) {
	siguiente := make(chan struct{})
	url := apiConDestino(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		rc := http.NewResponseController(w)
		_ = rc.Flush() // como el chat y la bandeja: las cabeceras salen enseguida (retry: …)
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
	lineas := leerIDs(t, url+ruta)
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
				t.Fatalf("evento %d: %q (abierto %v): se cortó el flujo", i, l, ok)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("el evento %d no llegó: el proxy lo retuvo en un búfer", i)
		}
	}
}

// El plazo del flujo nunca es cero: el de escritura se renueva con cada evento (más corto que el
// WriteTimeout de la API, el flujo sigue mientras haya eventos) y hay un máximo absoluto, en las
// dos rutas (el cliente se reconecta).
func TestProxyFlujoSSEPlazoMovilYMaximo(t *testing.T) {
	defer servidor.FijarPlazosSSE(400*time.Millisecond, 2*time.Second)()
	for _, ruta := range rutasFlujo {
		t.Run(ruta, func(t *testing.T) {
			n, dur := cuantosHasta(t, leerIDs(t, apiConDestino(t, destinoCadaTanto(150*time.Millisecond))+ruta))
			if dur < 1700*time.Millisecond || dur > 3500*time.Millisecond || n < 9 {
				t.Fatalf("el flujo duró %v con %d eventos (esperaba ~2 s, el máximo)", dur, n)
			}
		})
	}
}

// Un flujo exento cuyo destino calla (sin eventos ni latidos) vive más que los plazos de la API
// (1 s) y lo cierra el máximo absoluto, no antes ni nunca. (Sin la exención, un destino callado no
// escribe y el WriteTimeout no llega a cortar: el flujo quedaba abierto hasta que el destino
// cerrara; con ella, el tope de lectura lo cierra.)
func TestProxyFlujoSSESilencioHastaElMaximo(t *testing.T) {
	defer servidor.FijarPlazosSSE(400*time.Millisecond, 2*time.Second)()
	for _, ruta := range rutasFlujo {
		t.Run(ruta, func(t *testing.T) {
			_, dur := cuantosHasta(t, leerIDs(t, apiConDestino(t, destinoCadaTanto(time.Minute))+ruta))
			if dur < 1700*time.Millisecond || dur > 3500*time.Millisecond {
				t.Fatalf("un flujo sin escrituras duró %v (esperaba ~2 s, el máximo)", dur)
			}
		})
	}
}

// El plazo móvil corta a quien deja de leer: el destino escribe sin parar, el cliente no lee, la
// escritura de la API se bloquea y vence su plazo (400 ms) mucho antes del máximo (10 s).
func TestProxyFlujoSSEClienteQueNoLeeSeCorta(t *testing.T) {
	defer servidor.FijarPlazosSSE(400*time.Millisecond, 10*time.Second)()
	for _, ruta := range rutasFlujo {
		t.Run(ruta, func(t *testing.T) {
			fin := make(chan struct{})
			relleno := strings.Repeat("x", 32*1024)
			api := apiConDestino(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer close(fin)
				w.Header().Set("Content-Type", "text/event-stream")
				rc := http.NewResponseController(w)
				for r.Context().Err() == nil {
					if _, err := fmt.Fprintf(w, "data: %s\n\n", relleno); err != nil || rc.Flush() != nil {
						return
					}
				}
			}))
			c := pedirCrudo(t, api, "GET "+ruta+" HTTP/1.1\r\nHost: api\r\nAccept: text/event-stream\r\n\r\n")
			if tcp, ok := c.(*net.TCPConn); ok {
				_ = tcp.SetReadBuffer(4096)
			}
			inicio := time.Now()
			select {
			case <-fin:
				if d := time.Since(inicio); d > 5*time.Second {
					t.Fatalf("el flujo de un cliente que no lee duró %v", d)
				}
			case <-time.After(8 * time.Second):
				t.Fatal("el flujo de un cliente que no lee no se cortó: el plazo móvil no rige")
			}
		})
	}
}

// Un GET a la ruta del flujo con cuerpo chunked lento no obtiene la exención: lo cortan los plazos
// de la API (1 s), no el máximo (10 s). Así nadie sin sesión retiene la conexión 31 min.
func TestProxyFlujoSSEConCuerpoConservaPlazos(t *testing.T) {
	defer servidor.FijarPlazosSSE(5*time.Second, 10*time.Second)()
	for _, ruta := range rutasFlujo {
		t.Run(ruta, func(t *testing.T) {
			c := pedirCrudo(t, apiConDestino(t, destinoCadaTanto(150*time.Millisecond)),
				"GET "+ruta+" HTTP/1.1\r\nHost: api\r\nAccept: text/event-stream\r\nTransfer-Encoding: chunked\r\n\r\n1\r\na\r\n")
			inicio := time.Now()
			go func() { // el cuerpo, un byte cada 300 ms (nunca termina)
				for c.SetWriteDeadline(time.Now().Add(time.Second)) == nil {
					time.Sleep(300 * time.Millisecond)
					if _, err := c.Write([]byte("1\r\na\r\n")); err != nil {
						return
					}
				}
			}()
			_ = c.SetReadDeadline(time.Now().Add(8 * time.Second))
			_, _ = io.Copy(io.Discard, c)
			if d := time.Since(inicio); d > 2500*time.Millisecond {
				t.Fatalf("un GET con cuerpo lento a %s duró %v: obtuvo la exención", ruta, d)
			}
		})
	}
}

// pedirCrudo abre una conexión TCP a la API y escribe la petición tal cual.
func pedirCrudo(t *testing.T, api, peticion string) net.Conn {
	t.Helper()
	c, err := net.Dial("tcp", strings.TrimPrefix(api, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if _, err := io.WriteString(c, peticion); err != nil {
		t.Fatal(err)
	}
	return c
}

// Otra ruta delegada con Accept: text/event-stream conserva los plazos de la API (1 s).
func TestProxyOtraRutaConservaPlazos(t *testing.T) {
	_, dur := cuantosHasta(t, leerIDs(t, apiConDestino(t, destinoCadaTanto(150*time.Millisecond))+"/api/v1/mensajeria/plantillas"))
	if dur > 1700*time.Millisecond {
		t.Fatalf("otra ruta mantuvo el flujo %v: perdió el WriteTimeout de la API", dur)
	}
}

// El WebSocket del teléfono de Iris pasa por el proxy (upgrade) y sobrevive el ReadTimeout y el
// WriteTimeout de la API (aquí 1 s): al secuestrar la conexión, net/http borra sus plazos.
func TestProxyTelefonoSIPSinPlazos(t *testing.T) {
	destino := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{"sip"}, InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer func() { _ = c.CloseNow() }()
		for {
			tipo, b, err := c.Read(r.Context())
			if err != nil {
				return
			}
			if err := c.Write(r.Context(), tipo, b); err != nil {
				return
			}
		}
	})
	base := apiConDestino(t, destino)
	eco := func(ruta string) error {
		ctx, cancelar := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelar()
		c, resp, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(base, "http")+ruta, &websocket.DialOptions{Subprotocols: []string{"sip"}})
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		if err != nil {
			return err
		}
		defer func() { _ = c.CloseNow() }()
		time.Sleep(1500 * time.Millisecond) // pasa el ReadTimeout y el WriteTimeout de la API
		if err := c.Write(ctx, websocket.MessageText, []byte("OPTIONS")); err != nil {
			return err
		}
		_, b, err := c.Read(ctx)
		if err == nil && string(b) != "OPTIONS" {
			return fmt.Errorf("eco: %q", b)
		}
		return err
	}
	if err := eco(servidor.RutaTelefonoSIP); err != nil {
		t.Fatalf("el teléfono no sobrevivió los plazos de la API: %v", err)
	}
}
