package mensajeria_test

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// El asistente por conversación (contratos/conversaciones.md §17.11): qué dice la bandeja cuando una
// persona toma y suelta una conversación que el asistente atendía, y las acciones pausar y reanudar.

// atencion: «atiende/motivo» del campo atencionAsistente ("" si no viene).
func atencion(c map[string]any) string {
	a, _ := c["atencionAsistente"].(map[string]any)
	if a == nil {
		return ""
	}
	if a["motivo"] == nil {
		return fmt.Sprint(a["atiende"])
	}
	return fmt.Sprint(a["atiende"], "/", a["motivo"])
}

func tipoActividad(c map[string]any) string {
	a, _ := c["actividadAsistente"].(map[string]any)
	if a == nil {
		return ""
	}
	return fmt.Sprint(a["tipo"])
}

func (x *bandejaIA) accionAsistente(p persona, conv, accion string, cuerpo map[string]any) respuesta {
	x.t.Helper()
	return x.api(p, http.MethodPost, "/conversaciones/"+conv+"/asistente/"+accion, cuerpo)
}

// A1 (reporte del PM): el asistente atiende, una persona toma la conversación y después se quita la
// asignación. Antes, la fila de la bandeja seguía diciendo «Respondió al cliente» (la última
// actividad del asistente, de antes de que la persona la tomara) aunque el asistente ya no contesta:
// queda detenido hasta que la conversación se resuelva. Ahora esa línea se borra, la conversación
// dice por qué el asistente no atiende, y «reanudar» se la devuelve de verdad.
func TestAsistentePersonaTomaYSuelta(t *testing.T) {
	x := nuevaBandejaIA(t)
	x.autonomo("siempre_primero", nil)
	num := numeroCliente()
	c := x.turno(num, "wamid.s1", "¿Tienen fibra?", "Planes y precios", dice("Sí, tenemos fibra."))
	conv := cid(c)
	if estadoAsistente(c) != "atendiendo/1" || tipoActividad(c) != "respondio" || atencion(c) != "true" {
		t.Fatalf("el asistente atiende: %v %v %v", c["asistente"], c["actividadAsistente"], c["atencionAsistente"])
	}
	// Una persona la toma: el asistente se aparta y la bandeja ya no dice «Respondió al cliente».
	x.exigir(x.api(x.agente, http.MethodPost, "/conversaciones/"+conv+"/tomar", map[string]any{"version": ver(c)}), http.StatusOK)
	c = x.conv(num)
	if estadoAsistente(c) != "detenido/1" || tipoActividad(c) != "" || atencion(c) != "false/persona_asignada" {
		t.Fatalf("tomada por una persona: %v actividad %v atención %v", c["asistente"], c["actividadAsistente"], c["atencionAsistente"])
	}
	// La persona se quita la asignación: el asistente NO retoma solo (§17.8) y la conversación lo dice.
	x.exigir(x.api(x.supervisor, http.MethodPost, "/conversaciones/"+conv+"/asignar", map[string]any{"version": ver(c), "usuarioId": nil}), http.StatusOK)
	c = x.conv(num)
	a, _ := c["atencionAsistente"].(map[string]any)
	if c["asignado"] != nil || atencion(c) != "false/persona_presente" || a["reanudable"] != true || tipoActividad(c) != "" {
		t.Fatalf("sin asignar después de una persona: actividad %v atención %v", c["actividadAsistente"], c["atencionAsistente"])
	}
	// El cliente escribe otra vez: el asistente no contesta, y nada en la conversación dice que contestó.
	antes := x.autonomos()
	c = x.turno(num, "wamid.s2", "¿Y cuánto cuesta?", "Planes y precios", dice("No debería salir"))
	if x.autonomos() != antes || len(x.delAsistente(conv)) != 1 {
		t.Fatalf("detenido: no debía actuar (%d mensajes del asistente)", len(x.delAsistente(conv)))
	}
	if strings.HasPrefix(tipoActividad(c), "respondio") || atencion(c) != "false/persona_presente" || c["estado"] != "abierta" {
		t.Fatalf("la bandeja dice que el asistente respondió y no envió nada: %v %v", c["actividadAsistente"], c["atencionAsistente"])
	}
	for _, f := range x.lista(x.supervisor) {
		if k := f.(map[string]any); cid(k) == conv && (strings.HasPrefix(tipoActividad(k), "respondio") || atencion(k) != "false/persona_presente") {
			t.Fatalf("la lista dice otra cosa: %v %v", k["actividadAsistente"], k["atencionAsistente"])
		}
	}
	// «Reanudar»: una persona le devuelve la conversación. El asistente contesta de verdad el mensaje
	// pendiente (sin presentarse otra vez) y vuelve a atender.
	r := x.accionAsistente(x.agente, conv, "reanudar", map[string]any{"version": ver(c)})
	x.exigir(r, http.StatusOK)
	if atencion(r.cuerpo) != "true" || r.cuerpo["asistente"] != nil {
		t.Fatalf("reanudado: %v %v", r.cuerpo["asistente"], r.cuerpo["atencionAsistente"])
	}
	x.guion(dice("Depende del plan."))
	x.correr(conv)
	c = x.conv(num)
	ms := x.delAsistente(conv)
	if len(ms) != 2 || textoDe(ms[1]) != "Depende del plan." || estadoAsistente(c) != "atendiendo/2" || tipoActividad(c) != "respondio" || atencion(c) != "true" {
		t.Fatalf("después de reanudar contesta el pendiente: %v %v %v", ms, c["asistente"], c["atencionAsistente"])
	}
	if e := eventosSistema(x.mensajes(x.supervisor, conv), "asistente_reanudado"); len(e) != 1 || e[0]["actorNombre"] == nil || e[0]["detalle"] != "persona_presente" {
		t.Fatalf("el hilo dice quién lo reanudó: %v", e)
	}
	if x.contarOutbox("conversacion.asistente_reanudado", "'conversacion_id'", conv) != 1 {
		t.Fatal("falta conversacion.asistente_reanudado")
	}

	// Una persona escribe sin tomarla (el asistente nunca la atendió): también es «persona presente»,
	// y también se le puede devolver.
	n2 := numeroCliente()
	x.texto(n2, "wamid.s3", "Hola")
	conv2 := cid(x.conv(n2))
	x.exigir(x.api(x.supervisor, http.MethodPost, "/conversaciones/"+conv2+"/mensajes", map[string]any{"id": nuevoID(), "texto": "Te atiendo yo."}), http.StatusCreated)
	x.exigir(x.api(x.supervisor, http.MethodPost, "/conversaciones/"+conv2+"/asignar", map[string]any{"version": ver(x.conv(n2)), "usuarioId": nil}), http.StatusOK)
	if c = x.conv(n2); atencion(c) != "false/persona_presente" {
		t.Fatalf("una persona escribió: %v", c["atencionAsistente"])
	}
	x.exigir(x.accionAsistente(x.agente, conv2, "reanudar", map[string]any{"version": ver(c)}), http.StatusOK)
	*x.reloj = x.reloj.Add(time.Second)
	c = x.turno(n2, "wamid.s4", "¿Tienen fibra?", "Planes y precios", dice("Sí, tenemos fibra."))
	if len(x.delAsistente(conv2)) != 1 || estadoAsistente(c) != "atendiendo/1" {
		t.Fatalf("reanudado después de que una persona escribió: %v", c["asistente"])
	}
}

// A2: pausar y reanudar el asistente en una conversación, aunque nadie la tenga asignada.
func TestAsistentePausarYReanudar(t *testing.T) {
	x := nuevaBandejaIA(t)
	x.autonomo("siempre_primero", nil)
	num := numeroCliente()
	conv := x.llega(num, "wamid.z1", "¿Tienen fibra?", "Planes y precios")
	c := x.conv(num)
	if atencion(c) != "true" || c["asignado"] != nil {
		t.Fatalf("antes de pausar: %v", c["atencionAsistente"])
	}
	// Permiso propio, versión y conversación visible.
	x.exigir(x.accionAsistente(x.lectura, conv, "pausar", map[string]any{"version": ver(c)}), http.StatusForbidden)
	x.exigir(x.accionAsistente(x.agente, conv, "pausar", map[string]any{"version": ver(c) + 7}), http.StatusConflict, "type", "urn:iris:problema:conflicto_version")
	x.exigir(x.accionAsistente(x.agente, conv, "pausar", map[string]any{"version": ver(c), "motivo": "porque sí"}), http.StatusUnprocessableEntity)
	ajeno := x.persona([]int64{2}, rolBase("admin"))
	ajeno.empresa = 2
	x.exigir(x.accionAsistente(ajeno, conv, "pausar", map[string]any{"version": ver(c)}), http.StatusNotFound)
	// Pausa sin que nadie la tenga asignada.
	cuerpo := map[string]any{"version": ver(c), "motivo": "caso_delicado"}
	r := x.apiClave(x.agente, http.MethodPost, "/conversaciones/"+conv+"/asistente/pausar", "pausa-"+conv, cuerpo)
	x.exigir(r, http.StatusOK)
	// Idempotency-Key: la misma petición repetida devuelve la misma respuesta (no un 409 por la versión).
	if rep := x.apiClave(x.agente, http.MethodPost, "/conversaciones/"+conv+"/asistente/pausar", "pausa-"+conv, cuerpo); rep.status != http.StatusOK ||
		rep.h.Get("Idempotent-Replayed") != "true" || ver(rep.cuerpo) != ver(r.cuerpo) {
		t.Fatalf("Idempotency-Key repetida: %d %v", rep.status, rep.h)
	}
	a, _ := r.cuerpo["atencionAsistente"].(map[string]any)
	p, _ := a["pausa"].(map[string]any)
	if atencion(r.cuerpo) != "false/pausado" || a["reanudable"] != true || p == nil || p["motivo"] != "caso_delicado" || p["por"] == nil || p["en"] == nil || r.cuerpo["asignado"] != nil {
		t.Fatalf("pausado: %v", r.cuerpo["atencionAsistente"])
	}
	// Repetirla no cambia nada ni emite otro evento.
	x.exigir(x.accionAsistente(x.supervisor, conv, "pausar", map[string]any{"version": ver(r.cuerpo)}), http.StatusOK)
	if n := x.contarOutbox("conversacion.asistente_pausado", "'conversacion_id'", conv); n != 1 {
		t.Fatalf("conversacion.asistente_pausado: %d", n)
	}
	if n := x.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'conversacion.asistente_pausado' AND payload->>'conversacion_id' = $1
		AND payload->>'motivo' = 'caso_delicado' AND (payload->>'actor_id')::bigint = $2 AND payload->>'atendia' = 'false'`, conv, x.agente.id); n != 1 {
		t.Fatal("el evento lleva actor y motivo")
	}
	if n := x.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'conversaciones.pausar_asistente' AND entidad = 'conversacion' AND entidad_id = $1
		AND resultado = 'ok' AND actor_id = $2`, conv, x.agente.id); n != 1 {
		t.Fatalf("auditoría de la pausa: %d", n)
	}
	if e := eventosSistema(x.mensajes(x.supervisor, conv), "asistente_pausado"); len(e) != 1 || e[0]["actorNombre"] == nil || e[0]["detalle"] != "caso_delicado" {
		t.Fatalf("el hilo dice quién lo pausó: %v", e)
	}
	// En pausa el asistente no contesta: ni el mensaje pendiente ni los que lleguen.
	antes := x.autonomos()
	x.guion(dice("No debería salir"))
	x.correr(conv)
	x.turno(num, "wamid.z2", "¿Siguen ahí?", "Planes y precios", dice("No debería salir"))
	if x.autonomos() != antes || len(x.delAsistente(conv)) != 0 {
		t.Fatal("en pausa, pero el asistente actuó")
	}
	// Las sugerencias al agente siguen: no llegan al cliente.
	x.exigir(x.api(x.agente, http.MethodPost, "/conversaciones/"+conv+"/sugerencias", nil), http.StatusAccepted)
	// Reanudar: contesta el mensaje pendiente.
	c = x.conv(num)
	x.exigir(x.accionAsistente(x.lectura, conv, "reanudar", map[string]any{"version": ver(c)}), http.StatusForbidden)
	x.exigir(x.accionAsistente(x.agente, conv, "reanudar", map[string]any{"version": ver(c) + 7}), http.StatusConflict)
	r = x.accionAsistente(x.agente, conv, "reanudar", map[string]any{"version": ver(c)})
	x.exigir(r, http.StatusOK)
	if a, _ = r.cuerpo["atencionAsistente"].(map[string]any); atencion(r.cuerpo) != "true" || a["pausa"] != nil || a["reanudable"] != false {
		t.Fatalf("reanudado: %v", r.cuerpo["atencionAsistente"])
	}
	x.guion(dice("Aquí seguimos."))
	x.correr(conv)
	c = x.conv(num)
	if ms := x.delAsistente(conv); len(ms) != 1 || !strings.HasSuffix(textoDe(ms[0]), "Aquí seguimos.") || estadoAsistente(c) != "atendiendo/1" {
		t.Fatalf("reanudado: contesta el pendiente: %v %v", ms, c["asistente"])
	}
	if n := x.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'conversacion.asistente_reanudado' AND payload->>'conversacion_id' = $1
		AND payload->>'de' = 'pausado' AND (payload->>'actor_id')::bigint = $2`, conv, x.agente.id); n != 1 {
		t.Fatal("falta conversacion.asistente_reanudado con actor")
	}
	// Reanudar sin nada que reanudar: 200 y ningún evento más.
	x.exigir(x.accionAsistente(x.agente, conv, "reanudar", map[string]any{"version": ver(c)}), http.StatusOK)
	if n := x.contarOutbox("conversacion.asistente_reanudado", "'conversacion_id'", conv); n != 1 {
		t.Fatalf("conversacion.asistente_reanudado: %d", n)
	}

	// Pausar a mitad de un turno: lo que el modelo estaba escribiendo no se envía.
	x.llega(num, "wamid.z3", "¿Qué velocidad tiene?", "Planes y precios")
	x.guion(dice("No debería salir"))
	var una sync.Once
	x.ia.mu.Lock()
	x.ia.alAutonomo = func(int) {
		una.Do(func() {
			x.exigir(x.accionAsistente(x.agente, conv, "pausar", map[string]any{"version": ver(x.conv(num))}), http.StatusOK)
		})
	}
	x.ia.mu.Unlock()
	x.correr(conv)
	x.ia.mu.Lock()
	x.ia.alAutonomo = nil
	x.ia.mu.Unlock()
	c = x.conv(num)
	if len(x.delAsistente(conv)) != 1 || estadoAsistente(c) != "detenido/1" || atencion(c) != "false/pausado" || tipoActividad(c) != "" {
		t.Fatalf("pausado a mitad del turno: %v %v %v", c["asistente"], c["atencionAsistente"], c["actividadAsistente"])
	}
	// Ese mensaje quedó sin contestar: al reanudar, el asistente lo contesta (el turno descartado vuelve).
	x.exigir(x.accionAsistente(x.agente, conv, "reanudar", map[string]any{"version": ver(c)}), http.StatusOK)
	x.guion(dice("Hasta 300 megas."))
	x.correr(conv)
	if ms := x.delAsistente(conv); len(ms) != 2 || textoDe(ms[1]) != "Hasta 300 megas." {
		t.Fatalf("al reanudar contesta el mensaje que quedó a medias: %v", ms)
	}

	// La pausa es del periodo: al resolverse se quita y, al reabrirse, vuelve lo que diga la empresa.
	c = x.conv(num)
	x.exigir(x.accionAsistente(x.agente, conv, "pausar", map[string]any{"version": ver(c)}), http.StatusOK)
	x.exigir(x.api(x.supervisor, http.MethodPost, "/conversaciones/"+conv+"/resolver", map[string]any{"version": ver(x.conv(num))}), http.StatusOK)
	c = x.conv(num)
	if c["atencionAsistente"] != nil {
		t.Fatalf("resuelta: sin campo de atención: %v", c["atencionAsistente"])
	}
	x.exigir(x.accionAsistente(x.agente, conv, "pausar", map[string]any{"version": ver(c)}), http.StatusUnprocessableEntity, "type", "urn:iris:problema:conversacion_resuelta")
	x.exigir(x.accionAsistente(x.agente, conv, "reanudar", map[string]any{"version": ver(c)}), http.StatusUnprocessableEntity, "type", "urn:iris:problema:conversacion_resuelta")
	c = x.turno(num, "wamid.z4", "Hola de nuevo, ¿tienen fibra?", "Planes y precios", dice("Sí, tenemos fibra."))
	if a, _ = c["atencionAsistente"].(map[string]any); atencion(c) != "true" || a["pausa"] != nil || estadoAsistente(c) != "atendiendo/1" {
		t.Fatalf("reabierta: vuelve a atender sin la pausa: %v %v", c["asistente"], c["atencionAsistente"])
	}

	// Pausada y con una persona asignada: reanudar quita la pausa, y el asistente espera a que la suelte.
	x.exigir(x.accionAsistente(x.agente, conv, "pausar", map[string]any{"version": ver(c)}), http.StatusOK)
	x.exigir(x.api(x.agente, http.MethodPost, "/conversaciones/"+conv+"/tomar", map[string]any{"version": ver(x.conv(num))}), http.StatusOK)
	r = x.accionAsistente(x.agente, conv, "reanudar", map[string]any{"version": ver(x.conv(num))})
	x.exigir(r, http.StatusOK)
	if atencion(r.cuerpo) != "false/persona_asignada" {
		t.Fatalf("asignada: %v", r.cuerpo["atencionAsistente"])
	}
	x.exigir(x.api(x.supervisor, http.MethodPost, "/conversaciones/"+conv+"/asignar", map[string]any{"version": ver(r.cuerpo), "usuarioId": nil}), http.StatusOK)
	if c = x.conv(num); atencion(c) != "true" {
		t.Fatalf("la soltó después de reanudar: %v", c["atencionAsistente"])
	}

	// Sin el asistente atendiendo solo en la empresa, el campo no viene (no hay nada que pintar)…
	x.autonomo("nunca", nil)
	if c = x.conv(num); c["atencionAsistente"] != nil {
		t.Fatalf("modo nunca: %v", c["atencionAsistente"])
	}
	// …y con el tope del mes alcanzado dice «tope».
	x.autonomo("siempre_primero", map[string]any{"topeTokensMes": 5500})
	if _, err := x.bd.Exec(x.ctx, `UPDATE mensajeria.asistente_uso SET tokens_entrada = 6000 WHERE empresa_id = 1`); err != nil {
		t.Fatal(err)
	}
	if c = x.conv(num); atencion(c) != "false/tope" {
		t.Fatalf("tope: %v", c["atencionAsistente"])
	}
}

// Los otros motivos por los que el asistente no atiende: la categoría solo sugiere y el bot de menú.
func TestAsistenteAtencionMotivos(t *testing.T) {
	x := nuevaBandejaIA(t)
	x.botActivo(map[string]any{"activo": true, "atiende": "bot", "minutosSinConfirmacion": 30, "minutosSinDisponibles": 3, "saludo": "Hola, soy el bot", "menu": []any{}})
	x.autonomo("siempre_primero", nil)
	num := numeroCliente()
	conv := x.llega(num, "wamid.m1", "Quiero cambiar de plan", "Cambio de plan")
	x.guion(dice("No debería salir"))
	x.correr(conv)
	c := x.conv(num)
	a, _ := c["atencionAsistente"].(map[string]any)
	if atencion(c) != "false/categoria_sugiere" || a["reanudable"] != false {
		t.Fatalf("categoría en «sugiere»: %v", c["atencionAsistente"])
	}
	*x.reloj = x.reloj.Add(4 * time.Minute)
	x.disparar()
	if c = x.conv(num); atencion(c) != "false/bot" {
		t.Fatalf("la tomó el bot: %v", c["atencionAsistente"])
	}
}

// El hilo no dice «contestó el bot» si el bot no le pudo mandar nada al cliente (BAJA o ventana de
// 24 h cerrada): dice que no pudo.
func TestBotSinRespuestaNoDiceQueContesto(t *testing.T) {
	b := nuevaBandeja(t, nil)
	b.botActivo(map[string]any{"activo": true, "atiende": "bot", "minutosSinConfirmacion": 30, "minutosSinDisponibles": 3, "saludo": "Hola, soy el bot", "menu": []any{}})
	// Con saludo: sí contestó.
	num := numeroCliente()
	b.texto(num, "wamid.q1", "Hola")
	conv := cid(b.conv(num))
	*b.reloj = b.reloj.Add(4 * time.Minute)
	b.disparar()
	ms := b.mensajes(b.supervisor, conv)
	if len(automaticos(ms, "bot")) != 1 || len(eventosSistema(ms, "bot_atendio")) != 1 || len(eventosSistema(ms, "bot_sin_respuesta")) != 0 {
		t.Fatalf("el bot saludó: %v", ms)
	}
	// BAJA: el bot no le escribe, y el hilo no dice que contestó.
	baja := numeroCliente()
	b.texto(baja, "wamid.q2", "BAJA")
	cb := cid(b.conv(baja))
	*b.reloj = b.reloj.Add(4 * time.Minute)
	b.disparar()
	ms = b.mensajes(b.supervisor, cb)
	for _, m := range ms {
		if m["direccion"] == "saliente" {
			t.Fatalf("el bot contestó a una baja: %v", m)
		}
	}
	if e := eventosSistema(ms, "bot_sin_respuesta"); len(eventosSistema(ms, "bot_atendio")) != 0 || len(e) != 1 || e[0]["detalle"] != "sin_disponibles" {
		t.Fatalf("el hilo dice que el bot contestó y no envió nada: %v", ms)
	}
	if n := b.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'conversacion.bot_atendio' AND payload->>'conversacion_id' = $1
		AND payload->>'respondio' = 'false' AND payload->>'mensaje_id' IS NULL`, cb); n != 1 {
		t.Fatalf("conversacion.bot_atendio con respondio = false: %d", n)
	}
}
