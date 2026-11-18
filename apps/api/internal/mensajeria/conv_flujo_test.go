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

// El flujo de la bandeja revalida la sesión y el permiso en cada latido, como el del chat: cerrar la
// sesión, desactivar al usuario o quitarle conversaciones.ver lo cierra.
func TestConversacionesSSERevalidaLaSesion(t *testing.T) {
	b := nuevaBandeja(t, func(c *mensajeria.Config) { c.ChatLatido = 100 * time.Millisecond })
	url, ctx := b.servirSSE(t)
	url += rutaMsj + "/conversaciones/eventos"
	casos := []struct {
		nombre string
		cortar string // SQL con $1 = usuario
	}{
		{"sesión cerrada", `UPDATE nucleo.sesiones SET revocada_en = now() WHERE usuario_id = $1`},
		{"usuario desactivado", `UPDATE nucleo.usuarios SET activo = false WHERE id = $1`},
		{"sin conversaciones.ver", `DELETE FROM nucleo.usuarios_roles WHERE usuario_id = $1`},
	}
	for _, c := range casos {
		p := b.persona([]int64{1}, rolBase("soporte"))
		flujo := abrirSSEEn(t, ctx, url, p, "")
		esperar(t, flujo, "latido")
		if _, err := b.bd.Exec(b.ctx, c.cortar, p.id); err != nil {
			t.Fatal(err)
		}
		limite := time.After(5 * time.Second)
	leer:
		for {
			select {
			case _, ok := <-flujo:
				if !ok {
					break leer
				}
			case <-limite:
				t.Fatalf("%s: el flujo de la bandeja no se cerró", c.nombre)
			}
		}
	}
	// Sin conversaciones.ver no se abre (403); un técnico no ve la bandeja.
	if s := estadoSSEEn(t, ctx, url, b.tecnico); s != http.StatusForbidden {
		t.Fatalf("técnico sin conversaciones.ver: %d", s)
	}
}

// Topes de la bandeja: 5 flujos por usuario (el sexto, 429) y ChatSSEMax por proceso (503); al
// cerrarse se liberan. El difusor es otro que el del chat: sus 5 no gastan los del chat.
func TestConversacionesSSETopes(t *testing.T) {
	b := nuevaBandeja(t, func(c *mensajeria.Config) { c.ChatSSEMax = 7 })
	url, ctx := b.servirSSE(t)
	conv := url + rutaMsj + "/conversaciones/eventos"
	a, otro, tercero := b.persona([]int64{1}, rolBase("soporte")), b.persona([]int64{1}, rolBase("soporte")), b.persona([]int64{1}, rolBase("soporte"))
	fctx, cerrar := context.WithCancel(ctx)
	for i := range 5 {
		if s := estadoSSEEn(t, fctx, conv, a); s != http.StatusOK {
			t.Fatalf("flujo %d: %d", i+1, s)
		}
	}
	if s := estadoSSEEn(t, ctx, conv, a); s != http.StatusTooManyRequests {
		t.Fatalf("sexto flujo de la bandeja: %d", s)
	}
	// El chat de la misma persona lleva su propia cuenta.
	if s := estadoSSEEn(t, fctx, url+rutaChat+"/eventos", a); s != http.StatusOK {
		t.Fatalf("chat con 5 flujos de bandeja abiertos: %d", s)
	}
	for range 2 {
		if s := estadoSSEEn(t, fctx, conv, otro); s != http.StatusOK {
			t.Fatalf("flujo de otro: %d", s)
		}
	}
	if s := estadoSSEEn(t, ctx, conv, tercero); s != http.StatusServiceUnavailable {
		t.Fatalf("sobre el tope del proceso: %d", s)
	}
	cerrar()
	deadline := time.Now().Add(3 * time.Second)
	for estadoSSEEn(t, ctx, conv, tercero) != http.StatusOK {
		if time.Now().After(deadline) {
			t.Fatal("los lugares de la bandeja no se liberaron")
		}
		time.Sleep(50 * time.Millisecond)
	}
}
