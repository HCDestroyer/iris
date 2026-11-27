package conector

import "testing"

func TestVersionMenorYContratoAcordado(t *testing.T) {
	p := func(s string) *string { return &s }
	for _, c := range []struct{ a, b, menor string }{
		{"0.14.3", "0.8.1", "0.8.1"}, {"0.8.1", "0.14.3", "0.8.1"}, {"0.9", "0.9.0", "0.9"}, {"1.0.0", "0.99.99", "0.99.99"},
		{"0.14.3-rc1", "0.14.2", "0.14.2"}, {"x", "0.9.0", "0.9.0"}, {"0.9.0", "x", "0.9.0"}, {"x", "y", "x"},
	} {
		if got := VersionMenor(c.a, c.b); got != c.menor {
			t.Errorf("VersionMenor(%q, %q) = %q, esperaba %q", c.a, c.b, got, c.menor)
		}
	}
	for _, c := range []struct {
		remoto, informado *string
		acordado          *string
	}{
		{nil, p("0.14.3"), nil},                 // sin el de Argos: falla cerrado
		{p("0.14.3"), nil, p("0.14.3")},         // sin informe guardado: el de Argos
		{p("0.14.3"), p("0.8.1"), p("0.8.1")},   // Argos todavía cree que Iris habla 0.8.1
		{p("0.14.2"), p("0.14.3"), p("0.14.2")}, // Argos habla menos que lo informado
		{p("raro"), p("0.14.3"), p("raro")},     // un remoto ilegible no se vuelve válido
		{p("0.14.3"), p("raro"), p("0.14.3")},   // un informado ilegible no cuenta
		{p("0.14.3"), p("0.14.3"), p("0.14.3")}, // empate
	} {
		got := ContratoAcordado(c.remoto, c.informado)
		if (got == nil) != (c.acordado == nil) || (got != nil && *got != *c.acordado) {
			t.Errorf("ContratoAcordado(%v, %v) = %v, esperaba %v", deref(c.remoto), deref(c.informado), deref(got), deref(c.acordado))
		}
	}
	if VersionAlMenos(ContratoAcordado(p("0.14.3"), p("0.8.1")), "0.9.0") {
		t.Error("con 0.8.1 informado, lo de 0.9.0 no se enciende aunque Argos hable 0.14.3")
	}
}

func deref(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}
