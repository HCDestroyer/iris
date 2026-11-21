package argos_test

import (
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
)

// El nombre del servicio (A45) es texto del cliente: sin saltos ni controles, y sin caracteres de
// formato Unicode (U+202E invierte el texto en pantalla; U+200B lo esconde).
func TestNombreServicioSinFormato(t *testing.T) {
	for entrada, esperado := range map[string]string{
		"Casa\u202e anaM":         "Casa anaM",
		"Ca\u200bsa\u2066\u2069":  "Casa",
		"\ufeffNegocio\n  centro": "Negocio centro",
	} {
		if got := argos.NombreServicio(entrada); got != esperado {
			t.Errorf("NombreServicio(%q) = %q, se esperaba %q", entrada, got, esperado)
		}
	}
}
