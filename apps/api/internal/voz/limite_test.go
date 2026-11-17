package voz

import (
	"testing"
	"time"
)

// Una IP que insiste en /enlazar agota su cubo, no el de las demás: nadie bloquea el enlace legítimo.
func TestLimiteDeEnlacePorIP(t *testing.T) {
	l := nuevoLimiteEnlace()
	ahora := time.Now()
	pasaron := 0
	for range 50 {
		if l.permitir("203.0.113.1", ahora) {
			pasaron++
		}
	}
	if pasaron != 3 {
		t.Fatalf("la IP que insiste pasó %d veces", pasaron)
	}
	if !l.permitir("198.51.100.7", ahora) {
		t.Fatal("otra IP quedó bloqueada por la primera")
	}
	// Muchas IP distintas a la vez tampoco bloquean a una nueva (el global es holgado).
	for i := range 60 {
		l.permitir(time.Duration(i).String(), ahora)
	}
	if !l.permitir("192.0.2.9", ahora) {
		t.Fatal("varias IP bloquearon a una nueva")
	}
	if l.permitir("203.0.113.1", ahora.Add(time.Second)) || !l.permitir("203.0.113.1", ahora.Add(3*time.Second)) {
		t.Fatal("el cubo por IP debía reponer una ficha cada 2 s")
	}
}

// Rotación: cada credencial anterior tiene su vencimiento y una vencida no revive.
func TestAplicarCredencial(t *testing.T) {
	ahora := time.Now()
	viejo, nuevo := []byte("viejo"), []byte("nuevo")
	e := Estado{Token: nuevo, TokenAnterior: viejo, TokenAnteriorVence: ahora.Add(-time.Minute), HuellaCliente: "h1"}
	aplicarCredencial(&e, PeticionCredencial{HuellaCliente: "h2"}, time.Hour, ahora)
	if e.TokenAnterior != nil || e.HuellaClienteAnterior != "h1" || !vigente(e.HuellaAnteriorVence, ahora) {
		t.Fatalf("rotar la huella: %+v", e)
	}
	e = Estado{Token: nuevo, HuellaCliente: "h2", HuellaClienteAnterior: "h1", HuellaAnteriorVence: ahora.Add(-time.Minute)}
	aplicarCredencial(&e, PeticionCredencial{Token: []byte("otro")}, time.Hour, ahora)
	if e.HuellaClienteAnterior != "" || string(e.TokenAnterior) != "nuevo" || !vigente(e.TokenAnteriorVence, ahora) {
		t.Fatalf("rotar el token: %+v", e)
	}
	// Convivencia 0 sin nada nuevo: todo lo anterior deja de valer.
	e = Estado{Token: nuevo, TokenAnterior: viejo, TokenAnteriorVence: ahora.Add(time.Hour), HuellaCliente: "h2",
		HuellaClienteAnterior: "h1", HuellaAnteriorVence: ahora.Add(time.Hour)}
	aplicarCredencial(&e, PeticionCredencial{}, 0, ahora)
	if e.TokenAnterior != nil || e.HuellaClienteAnterior != "" || !e.TokenAnteriorVence.IsZero() || !e.HuellaAnteriorVence.IsZero() {
		t.Fatalf("revocar: %+v", e)
	}
}
