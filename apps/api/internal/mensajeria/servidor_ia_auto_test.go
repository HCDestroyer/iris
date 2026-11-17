package mensajeria_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/voz/vozprueba"
)

// El asistente atiende solo con el servidor de IA de la instalación, de punta a punta: Iris → canal
// firmado → puerta real → Ollama falso, con herramientas.

// guionOllama hace del modelo detrás de la puerta: clasifica los mensajes y sigue un guion en los
// turnos autónomos.
type guionOllama struct {
	mu        sync.Mutex
	categoria string
	pasos     []pasoFalso
	autonomos []string
}

func chatOllama(msg map[string]any, fin string) []byte {
	b, _ := json.Marshal(map[string]any{"id": "chatcmpl-1", "object": "chat.completion", "model": "modelo-a:latest",
		"choices": []any{map[string]any{"index": 0, "message": msg, "finish_reason": fin}},
		"usage":   map[string]any{"prompt_tokens": 12, "completion_tokens": 7, "total_tokens": 19}})
	return b
}

func (g *guionOllama) responder(cuerpo []byte) []byte {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !esAutonomo(string(cuerpo)) {
		return chatOllama(map[string]any{"role": "assistant", "content": fmt.Sprintf(`{"categoria": %q, "confianza": 0.95}`, g.categoria)}, "stop")
	}
	g.autonomos = append(g.autonomos, string(cuerpo))
	if len(g.pasos) == 0 {
		return chatOllama(map[string]any{"role": "assistant", "content": "Sin guion."}, "stop")
	}
	paso := g.pasos[0]
	g.pasos = g.pasos[1:]
	msg, fin := map[string]any{"role": "assistant", "content": paso.texto}, "stop"
	if len(paso.llamadas) > 0 {
		var tc []any
		for i, l := range paso.llamadas {
			tc = append(tc, map[string]any{"id": fmt.Sprintf("call_%d_%d", len(g.autonomos), i), "type": "function",
				"function": map[string]any{"name": l.nombre, "arguments": l.args}})
		}
		msg["tool_calls"], fin = tc, "tool_calls"
	}
	return chatOllama(msg, fin)
}

func (g *guionOllama) fijar(ps ...pasoFalso) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.pasos = ps
}

func (g *guionOllama) pedidos() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.autonomos...)
}

func TestServidorIAModoAutonomoDePuntaAPunta(t *testing.T) {
	xi := nuevaBandejaIA(t)
	x := bancoIASobre(t, xi.bandeja, vozprueba.Opciones{})
	x.enlazar()
	x.activar()
	modelo := &guionOllama{categoria: "Planes y precios"}
	x.banco.Ollama.Cambiar(func(o *vozprueba.Ollama) { o.Responder = modelo.responder })
	// La empresa elige el servidor de IA (sin URL ni clave) y enciende el modo autónomo.
	xi.autonomo("siempre_primero", map[string]any{"proveedor": "servidor_ia", "url": "", "modelo": "modelo-a:latest"})

	// Turno 1: el modelo pide una herramienta (crear el ticket), Iris la ejecuta, le devuelve el
	// resultado por el mismo canal y el modelo responde al cliente.
	num := numeroCliente()
	xi.clienteDeNumero(num)
	conv := xi.llega(num, "wamid.sia1", "No me funciona el internet, abran un caso", "Planes y precios")
	modelo.fijar(llama("crear_ticket", `{"asunto": "Sin internet", "descripcion": "El cliente reporta que no navega", "prioridad": "media"}`),
		dice("Abrí un ticket para tu caso."))
	xi.correr(conv)
	ms := xi.delAsistente(conv)
	if len(ms) != 1 || !strings.Contains(textoDe(ms[0]), "Abrí un ticket para tu caso.") {
		t.Fatalf("respuesta del asistente por el servidor de IA: %v (acciones %v)", ms, xi.acciones(conv))
	}
	if a := strings.Join(xi.acciones(conv), ","); a != "crear_ticket:ok,responder:ok" {
		t.Fatalf("acciones del turno: %s", a)
	}
	if n := xi.contar(`SELECT count(*) FROM mensajeria.conversacion_vinculos WHERE conversacion_id = $1 AND tipo = 'ticket'`, conv); n != 1 {
		t.Fatalf("tickets: %d", n)
	}
	if e := estadoAsistente(xi.conv(num)); e != "atendiendo/1" {
		t.Fatalf("estado del asistente: %s", e)
	}
	// Los dos pedidos del turno pasaron por la puerta: el primero con las herramientas y el segundo
	// con el resultado de la herramienta. A Ollama no llegó ninguna cabecera de Iris.
	pedidos := modelo.pedidos()
	if len(pedidos) != 2 || !strings.Contains(pedidos[0], `"tools"`) || !strings.Contains(pedidos[0], `"crear_ticket"`) ||
		!strings.Contains(pedidos[1], `"role":"tool"`) || !strings.Contains(pedidos[1], `"tool_calls"`) {
		t.Fatalf("pedidos del turno autónomo: %d %s", len(pedidos), strings.Join(pedidos, "\n"))
	}
	for _, c := range x.banco.Ollama.Chats() {
		if c.Cabeceras.Get("Authorization") != "" {
			t.Fatal("a Ollama llegó una credencial")
		}
	}
	// Y quedó contado como uso del servidor de IA de la empresa (clasificar + dos pedidos).
	if n := xi.contar(`SELECT coalesce(sum(peticiones), 0)::int FROM mensajeria.servidor_ia_uso WHERE motor_id = $1 AND empresa_id = 1`, x.motor); n != 3 {
		t.Fatalf("peticiones contadas al servidor de IA: %d", n)
	}
	if n := xi.contar(`SELECT count(*) FROM mensajeria.servidor_ia_plazas`); n != 0 {
		t.Fatalf("plazas sin soltar: %d", n)
	}

	// N4, el modelo hostil: pide una herramienta que no existe y una de dinero que la empresa no
	// encendió (y sin identidad verificada). Ninguna se ejecuta: «herramienta_no_permitida», sin efecto.
	numH := numeroCliente()
	xi.clienteDeNumero(numH)
	convH := xi.llega(numH, "wamid.siah", "Hola, quiero pagar después", "Planes y precios")
	modelo.fijar(llama("borrar_cliente", `{"id": 1}`), llama("pedir_promesa", `{"servicio_id": 10, "fecha": "2026-11-20"}`),
		llama("consultar_saldo", `{}`), dice("No puedo hacer eso por aquí."))
	xi.correr(convH)
	// Las dos que no se ofrecieron ni se miran; el saldo sí se ofrece, pero sin identidad verificada se rechaza.
	if a := strings.Join(xi.acciones(convH), ","); a != "herramienta_no_permitida:rechazada,herramienta_no_permitida:rechazada,dar_saldo:rechazada,responder:ok" {
		t.Fatalf("herramientas del modelo hostil: %s", a)
	}
	if n := xi.contar(`SELECT count(*) FROM mensajeria.conversacion_vinculos WHERE conversacion_id = $1`, convH); n != 0 {
		t.Fatalf("una herramienta no permitida no debía tener efecto: %d vínculos", n)
	}
	if n := xi.contar(`SELECT count(*) FROM auditoria.acciones WHERE actor_tipo = 'asistente_ia' AND entidad_id = $1 AND resultado = 'ok'
		AND accion NOT IN ('conversaciones.responder', 'mensajeria.asistente_turno', 'mensajeria.asistente_accion')`, convH); n != 0 {
		t.Fatalf("el asistente ejecutó algo que no debía: %d acciones", n)
	}
	for _, m := range xi.delAsistente(convH) {
		if strings.Contains(textoDe(m), "Q ") {
			t.Fatalf("salió un monto sin verificar: %q", textoDe(m))
		}
	}
	pedidosAntes := len(modelo.pedidos())

	// Turno 2: el servidor se cae. La conversación no se queda colgada: pasa a una persona con
	// «sin_respuesta» y al cliente solo le llega el aviso de que lo atiende una persona.
	x.banco.Apagar()
	xi.texto(num, "wamid.sia2", "¿Y cuándo llega el técnico?")
	*xi.reloj = xi.reloj.Add(21 * time.Second)
	modelo.fijar(dice("NO DEBE SALIR"))
	xi.correr(conv)
	c := xi.api(xi.supervisor, http.MethodGet, "/conversaciones/"+conv, nil).cuerpo
	xi.exigirPaso(c, "sin_respuesta")
	ms = xi.delAsistente(conv)
	if len(ms) != 2 || !strings.Contains(textoDe(ms[1]), "Te paso con una persona") || strings.Contains(textoDe(ms[1]), "NO DEBE SALIR") {
		t.Fatalf("con el servidor caído solo debía salir el aviso del relevo: %v", ms)
	}
	if n := xi.contar(`SELECT count(*) FROM mensajeria.servidor_ia_plazas`); n != 0 {
		t.Fatalf("plazas sin soltar tras la caída: %d", n)
	}
	// El job repetido no cambia nada: ya la tiene una persona.
	xi.correr(conv)
	if xi.pasos(conv, "sin_respuesta") != 1 || len(xi.delAsistente(conv)) != 2 {
		t.Fatal("un job repetido no debía pasar la conversación dos veces")
	}
	// Con el servidor ya marcado como caído (tres latidos), una conversación nueva no se queda
	// esperando al asistente: falla al instante, sin llamar, y sigue su curso en la cola para una persona.
	for range 3 {
		x.latir()
	}
	if x.estado() != "no_disponible" {
		t.Fatalf("estado del servidor: %s", x.estado())
	}
	num2 := numeroCliente()
	conv2 := xi.llega(num2, "wamid.sia3", "Hola, ¿tienen planes de fibra?", "Planes y precios")
	if d := xi.correr(conv2); d != 0 {
		t.Fatalf("el job no debía quedarse reintentando: %s", d)
	}
	c2 := xi.api(xi.supervisor, http.MethodGet, "/conversaciones/"+conv2, nil).cuerpo
	if c2["asistente"] != nil || c2["cola"] == nil || c2["asignado"] != nil || c2["estado"] != "abierta" || len(xi.delAsistente(conv2)) != 0 {
		t.Fatalf("con el servidor caído la conversación nueva sigue en la cola: asistente %v, cola %v, estado %v", c2["asistente"], c2["cola"], c2["estado"])
	}
	if len(modelo.pedidos()) != pedidosAntes {
		t.Fatalf("con el servidor caído no debía llegar nada más al modelo: %d", len(modelo.pedidos()))
	}
}
