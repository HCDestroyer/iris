package nucleo_test

import (
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

func TestEnmascararRuta(t *testing.T) {
	nucleo.RegistrarRutaConSecreto("/api/v1/prueba/{a}/fijo/{b}")
	nucleo.RegistrarRutaConSecreto("/api/v1/prueba/{a}/fijo/{b}") // idempotente
	for ruta, esperada := range map[string]string{
		"/api/v1/encuestas/publica/abc123":           "/api/v1/encuestas/publica/***",
		"/api/v1/encuestas/publica/abc123/responder": "/api/v1/encuestas/publica/***/responder",
		"/api/v1/encuestas/publica/":                 "/api/v1/encuestas/publica/",
		"/api/v1/encuestas/publica":                  "/api/v1/encuestas/publica",
		"/api/v1/encuestas/42":                       "/api/v1/encuestas/42",
		"/api/v1/tickets/42":                         "/api/v1/tickets/42",
		"/API/V1/encuestas/PUBLICA/abc":              "/API/V1/encuestas/PUBLICA/***",
		"/api//v1/encuestas/publica/abc/":            "/api//v1/encuestas/publica/***/",
		"/api/v1/encuestas/publica/abc?token=x":      "/api/v1/encuestas/publica/***",
		"/api/v1/prueba/uno/fijo/dos/tres":           "/api/v1/prueba/***/fijo/***/tres",
		"/api/v1/prueba/uno/otro/dos":                "/api/v1/prueba/uno/otro/dos",
	} {
		if got := nucleo.EnmascararRuta(ruta); got != esperada {
			t.Errorf("%s → %s (esperaba %s)", ruta, got, esperada)
		}
	}
}
