package main

import (
	"strings"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/config"
)

// `iris integracion crear` dice si el token vence y cuándo, y que se revoca con un canje nuevo
// (contrato con Argos 0.8.0 §1.5).
func TestTextoVencimientoCLI(t *testing.T) {
	if s := textoVencimientoCLI(nil); !strings.Contains(s, "no vence") || !strings.Contains(s, "canje nuevo") {
		t.Fatalf("sin vencimiento: %q", s)
	}
	v := time.Date(2027, 2, 17, 15, 4, 0, 0, time.UTC)
	if s := textoVencimientoCLI(&v); !strings.Contains(s, "2027-02-17 15:04 UTC") {
		t.Fatalf("con vencimiento: %q", s)
	}
}

func TestIntegracionCrearExpiresInFueraDeRango(t *testing.T) {
	for _, dias := range []string{"-1", "366"} {
		err := crearIntegracion(t.Context(), config.Config{}, []string{"--sistema", "argosisp", "--expires-in", dias})
		if err == nil || !strings.Contains(err.Error(), "--expires-in") {
			t.Fatalf("--expires-in %s: %v", dias, err)
		}
	}
}

// Contrato 0.15.0 §13.3: el modo simulado (declara todos los códigos) no arranca la API en producción.
func TestCapacidadesMensajeriaSinSimuladoEnProduccion(t *testing.T) {
	if _, err := capacidadesMensajeria(true, "https://iris.ejemplo.gt"); err == nil {
		t.Fatal("simulado con IRIS_BASE_URL https pública debía fallar")
	}
	for _, c := range []struct {
		simulado bool
		base     string
	}{{false, "https://iris.ejemplo.gt"}, {true, "http://localhost:3000"}, {true, ""}} {
		if d, err := capacidadesMensajeria(c.simulado, c.base); err != nil || d == nil {
			t.Fatalf("%v %q: %v", c.simulado, c.base, err)
		}
	}
}
