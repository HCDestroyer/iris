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
			id, err := x.m.ReservarVerificacion(x.ctx, 1, cliente, ids[i%2], true)
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
		if id, err := x.m.ReservarVerificacion(x.ctx, r[0], r[1], ids[0], true); err != nil || id == 0 {
			t.Fatalf("%s: reserva %d, %v", nombre, id, err)
		}
	}
	// Pasado el día, vuelve a haber intentos.
	*x.reloj = x.reloj.Add(24*time.Hour + time.Minute)
	if id, err := x.m.ReservarVerificacion(x.ctx, 1, cliente, ids[0], true); err != nil || id == 0 {
		t.Fatalf("al día siguiente: %d, %v", id, err)
	}
}

// asociadaAMano abre una conversación desde un número desconocido y la deja como la dejaría un agente
// que la asocia a mano al cliente de conv: con cliente, pero sin verificar que el número sea suyo.
func (x *bandejaIA) asociadaAMano(conv string) string {
	x.t.Helper()
	num := numeroCliente()
	x.texto(num, "wamid.mano"+num, "Hola, ¿cuánto debo de mi servicio?")
	otra := cid(x.conv(num))
	if _, err := x.bd.Exec(x.ctx, `UPDATE mensajeria.conversaciones d SET cliente_id = o.cliente_id, servicio_id = o.servicio_id, asociacion = 'manual',
		cliente_verificado = false FROM mensajeria.conversaciones o WHERE o.id = $1 AND d.id = $2`, conv, otra); err != nil {
		x.t.Fatal(err)
	}
	return otra
}

// Denegación dirigida: quien logra que un agente asocie su número al cliente de otro no le gasta los
// intentos al titular. Los intentos desde un número sin verificar van aparte y están acotados.
func TestVerificacionDeIdentidadNumeroSinVerificarNoGastaAlTitular(t *testing.T) {
	const dpi = "8391726480101"
	x := nuevaBandejaIA(t)
	x.autonomo("siempre_primero", nil)
	x.argos.mu.Lock()
	x.argos.documento = dpi
	x.argos.mu.Unlock()
	// Una empresa que deja que el asistente atienda «Pagos y saldo» también a números sin verificar
	// (sin la condición solo_verificados): ahí está el camino del atacante.
	previas := map[int64]string{}
	filas, err := x.bd.Query(x.ctx, `SELECT id, condicion FROM mensajeria.asistente_categorias WHERE nombre = 'Pagos y saldo' AND condicion IS NOT NULL`)
	if err != nil {
		t.Fatal(err)
	}
	for filas.Next() {
		var id int64
		var c string
		if err := filas.Scan(&id, &c); err != nil {
			t.Fatal(err)
		}
		previas[id] = c
	}
	filas.Close()
	if _, err := x.bd.Exec(x.ctx, `UPDATE mensajeria.asistente_categorias SET condicion = NULL WHERE nombre = 'Pagos y saldo'`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for id, c := range previas {
			_, _ = x.bd.Exec(x.ctx, `UPDATE mensajeria.asistente_categorias SET condicion = $2 WHERE id = $1`, id, c)
		}
	})
	titular, cliente := x.conClienteArgos()
	atacante := x.asociadaAMano(titular)
	otroAtacante := x.asociadaAMano(titular)
	idDe := func(c string) int64 { id, _ := strconv.ParseInt(c, 10, 64); return id }
	reservar := func(conv string, verificado bool) int64 {
		t.Helper()
		id, err := x.m.ReservarVerificacion(x.ctx, 1, cliente, idDe(conv), verificado)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	// El atacante falla dos veces por el asistente (camino real: la herramienta, con su número sin verificar).
	*x.reloj = x.reloj.Add(21 * time.Second)
	x.ia.decir(`{"categoria": "Pagos y saldo", "confianza": 0.95}`)
	x.guion(dice("Para darte tu saldo, escríbeme tu DPI completo."))
	x.correr(atacante)
	for i, malo := range []string{"1111111100101", "2222222200101"} {
		x.turno(x.numeroDe(atacante), "wamid.at"+strconv.Itoa(i), "Es "+malo, "Pagos y saldo", llama("verificar_identidad", `{}`),
			dice("Ese dato no coincide. ¿Puedes revisarlo y escribirlo completo?"))
	}
	sinVerificar := func() int {
		return x.contar(`SELECT count(*) FROM mensajeria.asistente_verificaciones WHERE cliente_id = $1 AND NOT numero_verificado`, cliente)
	}
	if n := sinVerificar(); n != 2 || x.verificacionesArgos() != 2 {
		t.Fatalf("intentos del atacante: %d filas sin verificar, %d llamadas a Argos", n, x.verificacionesArgos())
	}
	// Tercero en la misma media hora: no llega a Argos (2 por media hora), ni desde otro número sin verificar.
	if reservar(atacante, false) != 0 || reservar(otroAtacante, false) != 0 {
		t.Fatal("más de dos intentos sin verificar en media hora")
	}
	// El titular, desde su número verificado, conserva sus tres intentos: verifica con normalidad.
	*x.reloj = x.reloj.Add(21 * time.Second)
	x.ia.decir(`{"categoria": "Pagos y saldo", "confianza": 0.95}`)
	x.guion(dice("Para darte tu saldo, escríbeme tu DPI completo."))
	x.correr(titular)
	x.turno(x.numeroDe(titular), "wamid.tit1", "Es "+dpi, "Pagos y saldo", llama("verificar_identidad", `{}`), dice("Listo, ya verifiqué tu identidad."))
	if v := x.verificacion(titular); v != "fuerte" {
		t.Fatalf("el titular debía verificar: %v", v)
	}
	if n := x.contar(`SELECT count(*) FROM mensajeria.asistente_verificaciones WHERE cliente_id = $1 AND numero_verificado AND resultado = 'coincide'`, cliente); n != 1 {
		t.Fatalf("intento del titular: %d", n)
	}
	// Y le quedan los otros dos, ni uno menos.
	segundo, tercero, cuarto := reservar(titular, true), reservar(titular, true), reservar(titular, true)
	if segundo == 0 || tercero == 0 || cuarto != 0 {
		t.Fatalf("el titular debía tener exactamente tres intentos al día: %d %d %d", segundo, tercero, cuarto)
	}
	// Pasada la media hora el atacante tiene un intento más (3 por cliente y día sin verificar) y ahí se acaba,
	// también desde otro número sin verificar. Máximo absoluto: 6 por cliente y día.
	*x.reloj = x.reloj.Add(31 * time.Minute)
	if reservar(atacante, false) == 0 || reservar(atacante, false) != 0 || reservar(otroAtacante, false) != 0 {
		t.Fatal("tope por cliente y día de los números sin verificar")
	}
	if n := x.contar(`SELECT count(*) FROM mensajeria.asistente_verificaciones WHERE cliente_id = $1`, cliente); n != 6 || sinVerificar() != 3 {
		t.Fatalf("total del cliente: %d (sin verificar %d)", n, sinVerificar())
	}
}
