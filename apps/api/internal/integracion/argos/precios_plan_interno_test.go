package argos

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/riverqueue/river"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
)

// «Lista demasiado larga» (más páginas de las que Todas lee) es definitiva: el job de precios del
// plan no se reintenta en bucle. Argos caído sí se reintenta.
func TestPreciosPlanListaLargaEsDefinitiva(t *testing.T) {
	larga := conector.NuevoErrorRemoto(Remoto, http.StatusBadGateway, "Lista demasiado larga", fmt.Errorf("%w: más de 50 páginas en /servicios", conector.ErrListaDemasiadoLarga))
	var cancelado *river.JobCancelError
	if !errors.As(errorDePrecios(larga), &cancelado) {
		t.Fatal("la lista demasiado larga debía cancelar el job (sin reintentos)")
	}
	caido := conector.NuevoErrorRemoto(Remoto, http.StatusServiceUnavailable, "Argos no disponible", errors.New("503"))
	if errors.As(errorDePrecios(caido), &cancelado) {
		t.Fatal("Argos caído se reintenta")
	}
}

func TestPrecioValido(t *testing.T) {
	for entrada, esperado := range map[string]string{"250": "250.00", "250.5": "250.50", "0": "0.00", "9999999999.99": "9999999999.99"} {
		if p, ok := precioValido(entrada); !ok || p != esperado {
			t.Errorf("%q: %q %v", entrada, p, ok)
		}
	}
	for _, malo := range []string{"NaN", "Inf", "-Inf", "-0.01", "10000000000", "1e400", "abc", ""} {
		if _, ok := precioValido(malo); ok {
			t.Errorf("%q debía ser inválido", malo)
		}
	}
}

// Una IP, un prefijo o un trozo de IP en la búsqueda (oráculo de red, §2.4); lo demás no.
func TestPareceIP(t *testing.T) {
	for _, q := range []string{"100.64.12.34", "100.64.12.0/24", "100.64", "100.64.12.", "2001:db8::1", "2001:db8::/48", "fe80::"} {
		if !pareceIP(q) {
			t.Errorf("%q debía parecer IP", q)
		}
	}
	for _, q := range []string{"María", "C-0503", "5512 3344", "5512-3344", "1234567-8", "zona 1", "S-4410"} {
		if pareceIP(q) {
			t.Errorf("%q no es una IP", q)
		}
	}
}
