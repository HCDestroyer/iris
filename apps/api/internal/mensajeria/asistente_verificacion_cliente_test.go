package mensajeria_test

import (
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// El tope del desafío de identidad es por cliente (y empresa) y día, sumando todas sus conversaciones
// (contratos/conversaciones.md §17.8.2): dos conversaciones del mismo cliente lo comparten y otro
// cliente no se ve afectado.

// otraConversacionDe abre otra conversación (otro número) y la deja asociada al mismo cliente que conv.
func (x *bandejaIA) otraConversacionDe(conv string) string {
	x.t.Helper()
	num := numeroCliente()
	// El segundo número es el teléfono fijo del mismo cliente en Argos: la conversación se le asocia sola.
	if _, err := x.bd.Exec(x.ctx, `UPDATE integracion.argos_clientes SET telefono = $2
		WHERE id = (SELECT cliente_id FROM mensajeria.conversaciones WHERE id = $1)`, conv, "+502 "+num[3:7]+"-"+num[7:]); err != nil {
		x.t.Fatal(err)
	}
	x.texto(num, "wamid.otra"+num, "Hola, ¿cuánto debo? Escribo desde otro número")
	otra := cid(x.conv(num))
	if n := x.contar(`SELECT count(*) FROM mensajeria.conversaciones d JOIN mensajeria.conversaciones o ON o.cliente_id = d.cliente_id
		WHERE o.id = $1 AND d.id = $2 AND o.id <> d.id`, conv, otra); n != 1 {
		x.t.Fatalf("la segunda conversación no quedó asociada al mismo cliente")
	}
	return otra
}

func (x *bandejaIA) verificacionesArgos() int {
	x.argos.mu.Lock()
	defer x.argos.mu.Unlock()
	return len(x.argos.verificados)
}

func TestVerificacionDeIdentidadPorCliente(t *testing.T) {
	const dpi = "8391726480101"
	x := nuevaBandejaIA(t)
	x.autonomo("siempre_primero", nil)
	x.argos.mu.Lock()
	x.argos.documento = dpi
	x.argos.mu.Unlock()

	convA1, clienteA := x.conClienteArgos()
	convA2 := x.otraConversacionDe(convA1)
	convB, clienteB := x.conClienteArgos()
	pedir := func(conv string) {
		t.Helper()
		*x.reloj = x.reloj.Add(21 * time.Second)
		x.ia.decir(`{"categoria": "Pagos y saldo", "confianza": 0.95}`)
		x.guion(dice("Para darte tu saldo, escríbeme tu DPI completo."))
		x.correr(conv)
	}
	intento := func(conv, sufijo, valor string) map[string]any {
		t.Helper()
		return x.turno(x.numeroDe(conv), "wamid.vc"+sufijo, "Es "+valor, "Pagos y saldo", llama("verificar_identidad", `{}`),
			dice("Ese dato no coincide. ¿Puedes revisarlo y escribirlo completo?"))
	}
	filas := func(cliente int64, resultado string) int {
		return x.contar(`SELECT count(*) FROM mensajeria.asistente_verificaciones WHERE cliente_id = $1 AND resultado = $2`, cliente, resultado)
	}
	pedir(convA1)
	pedir(convA2)
	pedir(convB)

	// Dos intentos fallidos en una conversación y el tercero en la otra: se agota el tope del cliente.
	intento(convA1, "a1", "1111111100101")
	c := intento(convA1, "a2", "2222222200101")
	if strings.HasPrefix(estadoAsistente(c), "espera_persona") {
		t.Fatalf("con dos intentos aún no pasa a una persona: %v", c["asistente"])
	}
	c = intento(convA2, "a3", "3333333300101")
	x.exigirPaso(c, "herramienta_fallo")
	if n := filas(clienteA, "no_coincide"); n != 3 || x.verificacionesArgos() != 3 {
		t.Fatalf("intentos del cliente A: %d filas, %d llamadas a Argos", n, x.verificacionesArgos())
	}
	if n := x.contar(`SELECT count(DISTINCT conversacion_id) FROM mensajeria.asistente_verificaciones WHERE cliente_id = $1`, clienteA); n != 2 {
		t.Fatalf("los intentos debían repartirse en las dos conversaciones: %d", n)
	}

	// Otro cliente no se ve afectado: falla una vez, le quedan intentos, y con el dato bueno verifica.
	c = intento(convB, "b1", "1111111100101")
	if strings.HasPrefix(estadoAsistente(c), "espera_persona") {
		t.Fatalf("el cliente B no debía pasar a una persona: %v", c["asistente"])
	}
	x.turno(x.numeroDe(convB), "wamid.vcb2", "Perdón, es "+dpi, "Pagos y saldo", llama("verificar_identidad", `{}`), dice("Listo, ya verifiqué tu identidad."))
	if v := x.verificacion(convB); v != "fuerte" {
		t.Fatalf("el cliente B debía quedar verificado: %v", v)
	}
	if filas(clienteB, "no_coincide") != 1 || filas(clienteB, "coincide") != 1 || filas(clienteA, "no_coincide") != 3 {
		t.Fatalf("filas: B %d/%d, A %d", filas(clienteB, "no_coincide"), filas(clienteB, "coincide"), filas(clienteA, "no_coincide"))
	}

	// Agotado el tope, ni con el dato bueno ni desde la otra conversación se vuelve a llamar a Argos.
	antes := x.verificacionesArgos()
	x.turno(x.numeroDe(convA1), "wamid.vca4", "Ahora sí, es "+dpi, "Pagos y saldo", llama("verificar_identidad", `{}`), dice("Un momento."))
	if x.verificacionesArgos() != antes || filas(clienteA, "coincide") != 0 || x.verificacion(convA1) != "numero" {
		t.Fatalf("con el tope agotado no se verifica: %d llamadas nuevas, verificación %v", x.verificacionesArgos()-antes, x.verificacion(convA1))
	}
	if n := x.contar(`SELECT count(*) FROM mensajeria.asistente_verificaciones WHERE cliente_id = $1`, clienteA); n != 3 {
		t.Fatalf("filas del cliente A tras agotar: %d", n)
	}
}

// Dos conversaciones del mismo cliente a la vez: la reserva es atómica y nunca pasan de tres intentos.
func TestVerificacionDeIdentidadReservaAtomica(t *testing.T) {
	x := nuevaBandejaIA(t)
	x.autonomo("siempre_primero", nil)
	conv1, cliente := x.conClienteArgos()
	conv2 := x.otraConversacionDe(conv1)
	_, otro := x.conClienteArgos()
	ids := []int64{}
	for _, c := range []string{conv1, conv2} {
		id, _ := strconv.ParseInt(c, 10, 64)
		ids = append(ids, id)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	reservas, errores := 0, []error{}
	for i := range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, err := x.m.ReservarVerificacion(x.ctx, 1, cliente, ids[i%2])
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errores = append(errores, err)
			} else if id != 0 {
				reservas++
			}
		}()
	}
	wg.Wait()
	if len(errores) != 0 || reservas != 3 {
		t.Fatalf("reservas simultáneas: %d (errores %v)", reservas, errores)
	}
	if n := x.contar(`SELECT count(*) FROM mensajeria.asistente_verificaciones WHERE cliente_id = $1 AND resultado = 'en_curso'`, cliente); n != 3 {
		t.Fatalf("reservas guardadas: %d", n)
	}
	// Otro cliente, y el mismo cliente en otra empresa, tienen su propio tope.
	for nombre, r := range map[string][2]int64{"otro cliente": {1, otro}, "otra empresa": {2, cliente}} {
		if id, err := x.m.ReservarVerificacion(x.ctx, r[0], r[1], ids[0]); err != nil || id == 0 {
			t.Fatalf("%s: reserva %d, %v", nombre, id, err)
		}
	}
	// Pasado el día, vuelve a haber intentos.
	*x.reloj = x.reloj.Add(24*time.Hour + time.Minute)
	if id, err := x.m.ReservarVerificacion(x.ctx, 1, cliente, ids[0]); err != nil || id == 0 {
		t.Fatalf("al día siguiente: %d, %v", id, err)
	}
}
