package mensajeria_test

import (
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/mensajeria"
)

// contadorRevalidaciones cuenta las revalidaciones de los flujos SSE por usuario y por dónde
// ("pagina": después de leer una página que mueve el cursor; "latido").
type contadorRevalidaciones struct {
	mu    sync.Mutex
	veces map[int64]map[string]int
}

func contarRevalidaciones(m *mensajeria.Modulo) *contadorRevalidaciones {
	c := &contadorRevalidaciones{veces: map[int64]map[string]int{}}
	m.FijarAlRevalidarFlujo(func(usuario int64, donde string) {
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.veces[usuario] == nil {
			c.veces[usuario] = map[string]int{}
		}
		c.veces[usuario][donde]++
	})
	return c
}

func (c *contadorRevalidaciones) de(usuario int64) map[string]int {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := map[string]int{}
	for k, v := range c.veces[usuario] {
		out[k] = v
	}
	return out
}

func (c *contadorRevalidaciones) total(usuario int64) int {
	n := 0
	for _, v := range c.de(usuario) {
		n += v
	}
	return n
}

// esperarTotal espera a que el flujo de usuario lleve n revalidaciones y comprueba que no haya más.
func (c *contadorRevalidaciones) esperarTotal(t *testing.T, usuario int64, n int) {
	t.Helper()
	limite := time.Now().Add(5 * time.Second)
	for c.total(usuario) < n && time.Now().Before(limite) {
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(150 * time.Millisecond) // una revalidación de más llegaría aquí
	if got := c.total(usuario); got != n {
		t.Fatalf("revalidaciones: %d %v (esperaba %d)", got, c.de(usuario), n)
	}
}

// sinMensajes falla si el flujo entregó un mensaje (lo que se envía en estas pruebas no le toca).
func sinMensajes(t *testing.T, flujo <-chan eventoSSE) {
	t.Helper()
	for {
		select {
		case ev, ok := <-flujo:
			if !ok {
				t.Fatal("el flujo se cerró")
			}
			if ev.tipo != "latido" {
				t.Fatalf("entregó %s fuera de su alcance: %v", ev.tipo, ev.datos)
			}
		default:
			return
		}
	}
}

// Carga del flujo del chat: en el caso común (un evento de la empresa que el usuario no ve, que
// mueve el cursor) cada despertar revalida UNA vez, después de leer y antes de avanzar el cursor; sin
// revalidación antes de leer. Cada latido revalida una vez (y la lectura que sigue, sin eventos, no).
func TestChatSSERevalidaUnaVezPorDespertarYPorLatido(t *testing.T) {
	t.Run("despertar", func(t *testing.T) {
		e := nuevoEntorno(t, func(c *mensajeria.Config) { c.ChatLatido = time.Hour })
		cuenta := contarRevalidaciones(e.m)
		url, ctx := e.servirSSE(t)
		q := e.equipo(1)
		a := e.persona([]int64{1}, rolBase("tecnico"))
		e.ficha(a.id, q.cuadrilla1)
		c := idConv(e.chat(a, http.MethodPost, fmt.Sprintf("/cuadrillas/%d/abrir", q.cuadrilla1), "", nil))
		// Solo lectura: no ve la cuadrilla, pero es de la misma empresa (cada mensaje lo despierta).
		ajeno := e.persona([]int64{1}, rolBase("lectura"))
		flujo := abrirSSE(t, ctx, url, ajeno, "")
		// Abrir: la primera lectura parte de la autorización de apertura y no mueve el cursor.
		cuenta.esperarTotal(t, ajeno.id, 0)
		for i := 1; i <= 3; i++ {
			e.exigir(e.enviarChat(a, c, nuevoID(), fmt.Sprintf("no le toca %d", i)), http.StatusCreated)
			cuenta.esperarTotal(t, ajeno.id, i)
			if v := cuenta.de(ajeno.id); v["pagina"] != i || v["latido"] != 0 {
				t.Fatalf("despertar %d: %v (esperaba una revalidación después de leer)", i, v)
			}
		}
		sinMensajes(t, flujo)
	})
	t.Run("latido", func(t *testing.T) {
		e := nuevoEntorno(t, func(c *mensajeria.Config) { c.ChatLatido = 150 * time.Millisecond })
		cuenta := contarRevalidaciones(e.m)
		url, ctx := e.servirSSE(t)
		p := e.persona([]int64{1}, rolBase("tecnico"))
		flujo := abrirSSE(t, ctx, url, p, "")
		latidos := 0
		for latidos < 4 {
			if ev := esperarLatido(t, flujo); ev.tipo == "latido" {
				latidos++
			}
		}
		// La revalidación del latido va antes de escribirlo: a lo más una más si el siguiente
		// latido ya revalidó y aún no se escribió.
		v := cuenta.de(p.id)
		if v["pagina"] != 0 || v["latido"] < latidos || v["latido"] > latidos+1 {
			t.Fatalf("con %d latidos: %v (esperaba una revalidación por latido)", latidos, v)
		}
	})
}

// Lo mismo en el flujo de la bandeja (un mensaje de un cliente en una conversación que un agente de
// soporte sin supervisar no ve).
func TestConversacionesSSERevalidaUnaVezPorDespertarYPorLatido(t *testing.T) {
	t.Run("despertar", func(t *testing.T) {
		b := nuevaBandeja(t, func(c *mensajeria.Config) { c.ChatLatido = time.Hour })
		cuenta := contarRevalidaciones(b.m)
		url, ctx := b.servirSSE(t)
		url += rutaMsj + "/conversaciones/eventos"
		p := b.persona([]int64{1}, rolBase("soporte"))
		flujo := abrirSSEEn(t, ctx, url, p, "")
		cuenta.esperarTotal(t, p.id, 0)
		for i := 1; i <= 3; i++ {
			b.texto(numeroCliente(), fmt.Sprintf("wamid.carga.%d", i), "no le toca")
			cuenta.esperarTotal(t, p.id, i)
			if v := cuenta.de(p.id); v["pagina"] != i || v["latido"] != 0 {
				t.Fatalf("despertar %d: %v (esperaba una revalidación después de leer)", i, v)
			}
		}
		sinMensajes(t, flujo)
	})
	t.Run("latido", func(t *testing.T) {
		b := nuevaBandeja(t, func(c *mensajeria.Config) { c.ChatLatido = 150 * time.Millisecond })
		cuenta := contarRevalidaciones(b.m)
		url, ctx := b.servirSSE(t)
		url += rutaMsj + "/conversaciones/eventos"
		p := b.persona([]int64{1}, rolBase("soporte"))
		flujo := abrirSSEEn(t, ctx, url, p, "")
		latidos := 0
		for latidos < 4 {
			if ev := esperarLatido(t, flujo); ev.tipo == "latido" {
				latidos++
			}
		}
		v := cuenta.de(p.id)
		if v["pagina"] != 0 || v["latido"] < latidos || v["latido"] > latidos+1 {
			t.Fatalf("con %d latidos: %v (esperaba una revalidación por latido)", latidos, v)
		}
	})
}

// esperarLatido devuelve el siguiente evento del flujo (falla si se cierra o tarda).
func esperarLatido(t *testing.T, flujo <-chan eventoSSE) eventoSSE {
	t.Helper()
	select {
	case ev, ok := <-flujo:
		if !ok {
			t.Fatal("el flujo se cerró")
		}
		return ev
	case <-time.After(5 * time.Second):
		t.Fatal("sin latidos")
	}
	return eventoSSE{}
}

// Sin la revalidación antes de leer, una credencial revocada entre dos despertares sin eventos
// visibles se corta igual: en la siguiente página que mueve el cursor (aunque no traiga nada que el
// usuario vea) o, si no llega ninguna, en el siguiente latido. Nunca se entrega nada.
func TestChatSSERevocadaEntreDespertaresSeCorta(t *testing.T) {
	sqlRevocar := `UPDATE nucleo.tokens_api SET revocado_en = now() WHERE usuario_id = $1`
	t.Run("pagina", func(t *testing.T) {
		// Sin latidos en la prueba: solo la página puede cortarlo.
		e := nuevoEntorno(t, func(c *mensajeria.Config) { c.ChatLatido = time.Hour })
		cuenta := contarRevalidaciones(e.m)
		url, ctx := e.servirSSE(t)
		q := e.equipo(1)
		a := e.persona([]int64{1}, rolBase("tecnico"))
		e.ficha(a.id, q.cuadrilla1)
		c := idConv(e.chat(a, http.MethodPost, fmt.Sprintf("/cuadrillas/%d/abrir", q.cuadrilla1), "", nil))
		ajeno := e.persona([]int64{1}, rolBase("lectura"))
		flujo := abrirSSE(t, ctx, url, ajeno, "")
		e.exigir(e.enviarChat(a, c, nuevoID(), "primer despertar"), http.StatusCreated)
		cuenta.esperarTotal(t, ajeno.id, 1)
		if _, err := e.bd.Exec(e.ctx, sqlRevocar, ajeno.id); err != nil {
			t.Fatal(err)
		}
		e.exigir(e.enviarChat(a, c, nuevoID(), "segundo despertar"), http.StatusCreated)
		cerrado(t, flujo)
		if v := cuenta.de(ajeno.id); v["pagina"] != 2 || v["latido"] != 0 {
			t.Fatalf("revalidaciones: %v (esperaba que lo cortara la segunda página)", v)
		}
	})
	t.Run("latido", func(t *testing.T) {
		e := nuevoEntorno(t, func(c *mensajeria.Config) { c.ChatLatido = 300 * time.Millisecond })
		cuenta := contarRevalidaciones(e.m)
		url, ctx := e.servirSSE(t)
		q := e.equipo(1)
		a := e.persona([]int64{1}, rolBase("tecnico"))
		e.ficha(a.id, q.cuadrilla1)
		c := idConv(e.chat(a, http.MethodPost, fmt.Sprintf("/cuadrillas/%d/abrir", q.cuadrilla1), "", nil))
		ajeno := e.persona([]int64{1}, rolBase("lectura"))
		flujo := abrirSSE(t, ctx, url, ajeno, "")
		e.exigir(e.enviarChat(a, c, nuevoID(), "despertar"), http.StatusCreated)
		limite := time.Now().Add(5 * time.Second)
		for cuenta.de(ajeno.id)["pagina"] < 1 && time.Now().Before(limite) {
			time.Sleep(10 * time.Millisecond)
		}
		if _, err := e.bd.Exec(e.ctx, sqlRevocar, ajeno.id); err != nil {
			t.Fatal(err)
		}
		antes := cuenta.de(ajeno.id)["latido"]
		cerrado(t, flujo)
		if v := cuenta.de(ajeno.id); v["pagina"] != 1 || v["latido"] < antes+1 {
			t.Fatalf("revalidaciones: %v (esperaba que lo cortara el siguiente latido)", v)
		}
	})
}

// Bandeja con despertares sin parar: aunque el select elija sub.c una y otra vez y el latido no
// llegue, el flujo toca sus receptores arriba del ciclo cada ChatLatido (25 s) del reloj del módulo,
// así que pasados más de FlujoVivo (40 s) el agente sigue vivo y la entrante le suena. Solo avanza el
// reloj del módulo: el latido real (25 s de verdad) no llega durante la prueba.
func TestConversacionesSSEDespertaresSeguidosSiguenTocandoReceptores(t *testing.T) {
	x := nuevaBandejaVoz(t)
	cuenta := contarRevalidaciones(x.m)
	x.recibe(x.agente)
	url, ctx := x.servirSSE(t)
	flujo := abrirSSEEn(t, ctx, url+rutaMsj+"/conversaciones/eventos", x.sesion(x.agente), "")
	vistoEn := func() time.Time {
		t.Helper()
		var v time.Time
		if err := x.bd.QueryRow(x.ctx, `SELECT max(visto_en) FROM mensajeria.llamada_receptores WHERE usuario_id = $1 AND recibe`,
			x.agente.id).Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	inicio := *x.reloj
	// Seis despertares, uno cada 10 s del reloj del módulo (60 s en total, sin latidos).
	for i := 1; i <= 6; i++ {
		x.pasa(10 * time.Second)
		antes := cuenta.total(x.agente.id)
		x.texto(numeroCliente(), fmt.Sprintf("wamid.despertar.%d", i), "despierta el flujo")
		limite := time.Now().Add(5 * time.Second)
		for cuenta.total(x.agente.id) == antes && time.Now().Before(limite) {
			time.Sleep(10 * time.Millisecond)
		}
		// Tras el despertar, el último toque nunca queda más viejo que ChatLatido (25 s).
		limite = time.Now().Add(5 * time.Second)
		for x.reloj.Sub(vistoEn()) >= 25*time.Second && time.Now().Before(limite) {
			time.Sleep(10 * time.Millisecond)
		}
		if hueco := x.reloj.Sub(vistoEn()); hueco >= 25*time.Second {
			t.Fatalf("despertar %d (%v desde el inicio): último toque hace %v", i, x.reloj.Sub(inicio), hueco)
		}
	}
	// A lo más un toque extra por cada 25 s del reloj: a los 30 s y a los 60 s.
	if v := cuenta.de(x.agente.id); v["toque"] < 1 || v["toque"] > 2 {
		t.Fatalf("revalidaciones para tocar: %v (esperaba 1 o 2 en 60 s)", v)
	}
	// Una entrante ahora (60 s después de abrir, sin latidos): el agente es candidato y le suena.
	num, call := numeroCliente(), "wacid.despertares"+numeroCliente()
	x.connect(num, call)
	id, estado, _ := x.llamada(call)
	if estado != "sonando" {
		t.Fatalf("la entrante quedó %s", estado)
	}
	if n := x.contar(`SELECT count(*) FROM mensajeria.llamada_candidatos WHERE llamada_id = $1::uuid AND usuario_id = $2`, id, x.agente.id); n != 1 {
		t.Fatalf("el agente no es candidato de la entrante: %d", n)
	}
	select {
	case _, ok := <-flujo:
		if !ok {
			t.Fatal("el flujo se cerró")
		}
	default:
	}
}
