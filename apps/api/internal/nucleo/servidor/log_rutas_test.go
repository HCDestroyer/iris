package servidor_test

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/servidor"
)

// bufer seguro para el log (el handler escribe desde la petición).
type bufer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *bufer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *bufer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

// El log de acceso no deja el token de la encuesta pública (I2, PR #41): ni con la ruta
// registrada (con su patrón de chi), ni en /responder, ni en variantes que el router no acepta.
func TestLogAccesoEnmascaraSecretos(t *testing.T) {
	log := &bufer{}
	// Las rutas de la encuesta como las publicará encuestas (aquí, de prueba y anónimas).
	encuesta := func(api huma.API) {
		huma.Register(api, huma.Operation{OperationID: "verEncuestaPrueba", Method: http.MethodGet, Path: "/api/v1/encuestas/publica/{token}"},
			func(context.Context, *struct {
				Token string `path:"token"`
			}) (*struct{}, error) {
				return &struct{}{}, nil
			})
		huma.Register(api, huma.Operation{OperationID: "responderEncuestaPrueba", Method: http.MethodPost, Path: "/api/v1/encuestas/publica/{token}/responder"},
			func(context.Context, *struct {
				Token string `path:"token"`
			}) (*struct{}, error) {
				return &struct{}{}, nil
			})
	}
	h, _ := servidor.Nuevo(servidor.Deps{
		BD: pruebas.Pool(t), Identidad: identidad.Nuevo(identidad.Config{}), Version: "prueba",
		Log: slog.New(slog.NewJSONHandler(log, nil)), Modulos: []func(huma.API){encuesta},
	})
	pedir := func(metodo, ruta string) {
		t.Helper()
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(metodo, ruta, strings.NewReader("{}")))
	}

	pedir(http.MethodGet, "/api/v1/encuestas/publica/abc123")
	salida := log.String()
	if strings.Contains(salida, "abc123") {
		t.Fatalf("el token quedó en el log: %s", salida)
	}
	if !strings.Contains(salida, `"ruta":"/api/v1/encuestas/publica/***"`) || !strings.Contains(salida, `"patron":"/api/v1/encuestas/publica/{token}"`) {
		t.Fatalf("log de acceso: %s", salida)
	}

	for _, ruta := range []string{
		"/api/v1/encuestas/publica/sec-responder-1/responder",
		"/api/v1/Encuestas/Publica/sec-mayus-2",          // otra caja: 404, pero tampoco se loguea
		"//api/v1//encuestas/publica/sec-barras-3/",      // segmentos vacíos
		"/api/v1/encuestas/publica/sec-escapado-4%2Fmas", // %2F no parte el segmento
		"/api/v1/encuestas/publica/sec-query-5?x=sec-query-6",
	} {
		pedir(http.MethodPost, ruta)
	}
	salida = log.String()
	for _, secreto := range []string{"sec-responder-1", "sec-mayus-2", "sec-barras-3", "sec-escapado-4", "%2Fmas", "sec-query-5", "sec-query-6"} {
		if strings.Contains(salida, secreto) {
			t.Fatalf("%q quedó en el log: %s", secreto, salida)
		}
	}
	if !strings.Contains(salida, `"ruta":"/api/v1/encuestas/publica/***/responder"`) {
		t.Fatalf("responder: %s", salida)
	}

	// Una ruta sin secreto se loguea tal cual, con su patrón.
	pedir(http.MethodGet, "/api/v1/salud")
	if !strings.Contains(log.String(), `"ruta":"/api/v1/salud"`) {
		t.Fatalf("salud: %s", log.String())
	}
}
