package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/config"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

func TestMain(m *testing.M) { os.Exit(pruebas.Correr(m)) }

func puertoDe(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	_, puerto, err := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	return puerto
}

// iris salud: 0 con la API arriba (GET /api/v1/salud en 127.0.0.1:$PORT), 1 si está caída, no
// responde 200 o tarda más que el plazo.
func TestSalud(t *testing.T) {
	ctx := context.Background()
	arriba := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/salud" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"estado":"ok"}`))
	}))
	defer arriba.Close()
	t.Setenv("IRIS_PUERTO", "")
	t.Setenv("PORT", puertoDe(t, arriba))
	var salida bytes.Buffer
	if err := salud(ctx, nil, &salida); err != nil || strings.TrimSpace(salida.String()) != "ok" {
		t.Fatalf("arriba: %v %q", err, salida.String())
	}
	// Por la orden completa (sin IRIS_DB_URL: el healthcheck no carga la configuración).
	t.Setenv("IRIS_DB_URL", "")
	if err := correr([]string{"salud"}); err != nil {
		t.Fatalf("iris salud: %v", err)
	}

	caida := httptest.NewServer(http.NotFoundHandler())
	t.Setenv("PORT", puertoDe(t, caida))
	caida.Close()
	if err := correr([]string{"salud"}); err == nil {
		t.Fatal("con la API caída debía fallar")
	}

	mal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer mal.Close()
	if err := salud(ctx, []string{"--url", mal.URL + "/api/v1/salud"}, &salida); err == nil || !strings.Contains(err.Error(), "503") {
		t.Fatalf("503: %v", err)
	}

	lenta := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	defer lenta.Close()
	inicio := time.Now()
	if err := salud(ctx, []string{"--url", lenta.URL, "--timeout", "200ms"}, &salida); err == nil || time.Since(inicio) > 3*time.Second {
		t.Fatalf("plazo: %v en %s", err, time.Since(inicio))
	}
	if err := salud(ctx, []string{"de-mas"}, &salida); err == nil {
		t.Fatal("argumento de más")
	}
	t.Setenv("PORT", "no-es-puerto")
	if err := salud(ctx, nil, &salida); err == nil {
		t.Fatal("PORT inválido")
	}
}

func claveBase64(t *testing.T) string {
	t.Helper()
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(b)
}

// iris clave rotar por la consola: --simular cuenta sin escribir, la rotación re-cifra y la
// repetición no toca nada; sin la clave anterior, falla.
func TestClaveRotar(t *testing.T) {
	url, err := pruebas.URL()
	if err != nil {
		t.Fatal(err)
	}
	pool := pruebas.Pool(t)
	ctx := context.Background()
	vieja, nueva := claveBase64(t), claveBase64(t)
	t.Setenv("IRIS_DB_URL", url)
	t.Setenv("IRIS_CLAVE_CIFRADO", nueva)
	t.Setenv("IRIS_CLAVE_CIFRADO_ANTERIOR", "")

	if err := correr([]string{"clave", "rotar"}); err == nil || !strings.Contains(err.Error(), "IRIS_CLAVE_CIFRADO_ANTERIOR") {
		t.Fatalf("sin anterior: %v", err)
	}
	if err := correr([]string{"clave"}); err == nil || !strings.Contains(err.Error(), "uso: iris clave rotar") {
		t.Fatalf("sin suborden: %v", err)
	}
	t.Setenv("IRIS_CLAVE_CIFRADO_ANTERIOR", vieja)

	// Un TOTP cifrado con la vieja.
	var id int64
	if err := pool.QueryRow(ctx, `INSERT INTO nucleo.usuarios (nombre, correo) VALUES ('Consola', $1) RETURNING id`, pruebas.Correo("consola")).Scan(&id); err != nil {
		t.Fatal(err)
	}
	cViejo, _ := nucleo.NuevoCifrador(vieja)
	cNuevo, _ := nucleo.NuevoCifrador(nueva)
	cifrado, _ := cViejo.Cifrar([]byte("JBSWY3DPEHPK3PXP"), identidad.DatosAsociadosTOTP(id))
	if _, err := pool.Exec(ctx, `UPDATE nucleo.usuarios SET totp_secreto = $2 WHERE id = $1`, id, cifrado); err != nil {
		t.Fatal(err)
	}
	leer := func() []byte {
		var v []byte
		if err := pool.QueryRow(ctx, `SELECT totp_secreto FROM nucleo.usuarios WHERE id = $1`, id).Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}

	cfg, err := config.Cargar()
	if err != nil {
		t.Fatal(err)
	}
	var salida bytes.Buffer
	if err := rotarClave(ctx, cfg, []string{"--simular"}, &salida); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(salida.String(), "simulación") || !strings.Contains(salida.String(), ": 1\n") || !bytes.Equal(leer(), cifrado) {
		t.Fatalf("simular: %s", salida.String())
	}
	salida.Reset()
	if err := rotarClave(ctx, cfg, nil, &salida); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(salida.String(), "Re-cifrados: 1") {
		t.Fatalf("rotar: %s", salida.String())
	}
	if p, err := cNuevo.Descifrar(leer(), identidad.DatosAsociadosTOTP(id)); err != nil || string(p) != "JBSWY3DPEHPK3PXP" {
		t.Fatalf("tras rotar, con la nueva: %v", err)
	}
	salida.Reset()
	if err := rotarClave(ctx, cfg, nil, &salida); err != nil || !strings.Contains(salida.String(), "Re-cifrados: 0") {
		t.Fatalf("repetida: %v %s", err, salida.String())
	}
	if err := rotarClave(ctx, cfg, []string{"de-mas"}, &salida); err == nil {
		t.Fatal("argumento de más")
	}
}
