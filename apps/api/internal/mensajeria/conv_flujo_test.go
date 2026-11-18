package mensajeria_test

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/mensajeria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
)

// El flujo de la bandeja (GET /conversaciones/eventos) sobrevive el WriteTimeout y el ReadTimeout
// del proceso de mensajería (aquí 1 s; en producción 60 s) con sus latidos, y se cierra solo al
// llegar a ChatDuracionMax (el panel se reconecta), igual que el del chat.
func TestConversacionesSSESobrevivePlazosYSeCierraAlMaximo(t *testing.T) {
	b := nuevaBandeja(t, func(c *mensajeria.Config) {
		c.ChatLatido = 200 * time.Millisecond
		c.ChatDuracionMax = 2500 * time.Millisecond
	})
	srv := httptest.NewUnstartedServer(b.h)
	srv.Config.WriteTimeout, srv.Config.ReadTimeout = time.Second, time.Second
	srv.Start()
	defer srv.Close()
	ctx, cancelar := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelar()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+rutaMsj+"/conversaciones/eventos", nil)
	req.AddCookie(&http.Cookie{Name: identidad.CookieSesion, Value: b.supervisor.cookie})
	req.Header.Set("Accept", "text/event-stream")
	inicio := time.Now()
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("flujo: %d", res.StatusCode)
	}
	var ultimoLatido time.Duration
	lector := bufio.NewScanner(res.Body)
	for lector.Scan() {
		if strings.HasPrefix(lector.Text(), ": latido") {
			ultimoLatido = time.Since(inicio)
		}
	}
	dur := time.Since(inicio)
	if ultimoLatido < 1500*time.Millisecond {
		t.Fatalf("el último latido llegó a los %v (cerró a los %v): el flujo no pasó el WriteTimeout de 1 s", ultimoLatido, dur)
	}
	if dur < 2*time.Second || dur > 4*time.Second {
		t.Fatalf("el flujo duró %v (esperaba ~2,5 s, ChatDuracionMax)", dur)
	}
}
