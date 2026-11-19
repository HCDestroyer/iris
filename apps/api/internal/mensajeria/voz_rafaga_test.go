package mensajeria_test

import (
	"fmt"
	"testing"
	"time"
)

// Ráfaga con ventana fija (fallo de 0.29.0 con Meta real): los reintentos que la ráfaga rechaza no la
// renuevan y las salientes hacia el número no cuentan. Pasados 10 minutos desde la primera de las tres,
// la siguiente vuelve a sonar.
func TestRafagaVentanaFija(t *testing.T) {
	x := nuevaBandejaVoz(t)
	x.limitesVoz(1000, 100000)
	x.recibe(x.agente)
	num, conv := x.convConPermiso()
	// (b) Tres salientes al mismo número: no cuentan para la ráfaga.
	for i := range 3 {
		_, call := x.saliente(x.supervisor, conv, nil)
		x.webhookSaliente(num, call, "terminate", `,"status":"FAILED"`)
		x.pasa(time.Second, x.agente)
		if _, estado, _ := x.llamada(call); estado == "marcando" {
			t.Fatalf("saliente %d sigue marcando", i+1)
		}
	}
	entrante := func(n int) (string, string) {
		call := fmt.Sprintf("wacid.fija%d%s", n, num)
		x.connect(num, call)
		_, estado, motivo := x.llamada(call)
		if estado == "sonando" {
			x.webhook(x.eventoLlamada(num, call, "terminate", "", 0))
		}
		return estado, motivo
	}
	for i := range 3 {
		if estado, motivo := entrante(i); estado != "sonando" {
			t.Fatalf("entrante %d (las salientes no cuentan): %s %s", i+1, estado, motivo)
		}
		x.pasa(time.Second, x.agente)
	}
	// (c) La cuarta dentro de la ventana se rechaza, y los reintentos de los 9 minutos siguientes también.
	for i := 3; i < 8; i++ {
		if estado, motivo := entrante(i); estado != "rechazada" || motivo != "limite" {
			t.Fatalf("reintento %d dentro de la ventana: %s %s", i+1, estado, motivo)
		}
		x.pasa(2*time.Minute, x.agente)
	}
	// (a) Al minuto 11 de la primera, aunque hubo reintentos hasta el minuto 9, vuelve a sonar.
	x.pasa(time.Minute, x.agente)
	if estado, motivo := entrante(8); estado != "sonando" {
		t.Fatalf("pasados 10 minutos de la primera debía sonar: %s %s", estado, motivo)
	}
}
