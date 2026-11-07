package mensajeria_test

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
)

// Chat del equipo en el panel (contratos/chat.md §2.1–§2.3, §3.2): menciones, contadores, miembros
// de un hilo de orden, solo lectura y «Nuevo mensaje».

func lista(r respuesta) []map[string]any {
	out := []map[string]any{}
	d, _ := r.cuerpo["data"].([]any)
	for _, x := range d {
		out = append(out, x.(map[string]any))
	}
	return out
}

func numero(v any) int64 {
	f, _ := v.(float64)
	return int64(f)
}

// resumenChat lee GET /chat/resumen al instante (sin el tope de frecuencia; ver
// TestChatResumenTopeDeFrecuencia).
func (e *entorno) resumenChat(p persona) map[string]any {
	e.t.Helper()
	e.m.VaciarResumenes()
	r := e.chat(p, http.MethodGet, "/resumen", "", nil)
	e.exigir(r, http.StatusOK)
	return r.cuerpo
}

func (e *entorno) cerrarOrden(orden int64, estado, resultado string) {
	e.t.Helper()
	if _, err := e.bd.Exec(e.ctx, `UPDATE campo.ordenes SET estado = $2, resultado = $3, finalizada_en = now(), motivo_falla = CASE WHEN $2 = 'fallida' THEN 'sin_acceso' END WHERE id = $1`, orden, estado, resultado); err != nil {
		e.t.Fatal(err)
	}
}

func TestChatMenciones(t *testing.T) {
	e := nuevoEntorno(t, nil)
	q := e.equipo(1)
	uno := []int64{1}
	ana := e.persona(uno, rolBase("tecnico"))
	beto := e.persona(uno, rolBase("tecnico"))
	fuera := e.persona(uno, rolBase("tecnico"))
	e.ficha(ana.id, q.cuadrilla1)
	e.ficha(beto.id, q.cuadrilla1)
	e.ficha(fuera.id, q.cuadrilla2)
	sup := e.persona(uno, identidad.AsignacionRol{Rol: "supervisor", ZonaID: &q.zona1})

	r := e.chat(ana, http.MethodPost, fmt.Sprintf("/cuadrillas/%d/abrir", q.cuadrilla1), "", nil)
	e.exigir(r, http.StatusCreated)
	c := idConv(r)
	enviar := func(p persona, id string, cuerpo map[string]any) respuesta {
		cuerpo["id"] = id
		return e.chat(p, http.MethodPost, "/conversaciones/"+c+"/mensajes", "", cuerpo)
	}

	// Validación: quien no está en la conversación, el autor y una mención sin texto.
	r = enviar(ana, nuevoID(), map[string]any{"texto": "@Fuera hola", "menciones": []int64{fuera.id}})
	e.exigir(r, http.StatusUnprocessableEntity, "type", "urn:iris:problema:mencion_invalida")
	if errs := r.cuerpo["errors"].([]any); len(errs) != 1 || errs[0].(map[string]any)["field"] != "menciones[0]" {
		t.Fatalf("errors: %v", r.cuerpo["errors"])
	}
	e.exigir(enviar(ana, nuevoID(), map[string]any{"texto": "@Ana yo", "menciones": []int64{ana.id}}), http.StatusUnprocessableEntity,
		"type", "urn:iris:problema:mencion_invalida")
	e.exigir(enviar(ana, nuevoID(), map[string]any{"ubicacion": map[string]any{"lat": 14.6, "lon": -90.5}, "menciones": []int64{beto.id}}),
		http.StatusUnprocessableEntity, "type", "urn:iris:problema:mencion_sin_texto")
	muchas := make([]int64, 21)
	for i := range muchas {
		muchas[i] = int64(i + 1)
	}
	e.exigir(enviar(ana, nuevoID(), map[string]any{"texto": "todos", "menciones": muchas}), http.StatusUnprocessableEntity)
	if n := e.contar(`SELECT count(*) FROM mensajeria.chat_mensajes WHERE conversacion_id = $1`, c); n != 0 {
		t.Fatalf("se guardaron mensajes inválidos: %d", n)
	}

	// Beto y la supervisión (por zona) se pueden mencionar; los repetidos cuentan una vez.
	id := nuevoID()
	cuerpo := map[string]any{"texto": "@Beto @Laura la NAP 14 tiene el puerto 3 quemado", "menciones": []int64{sup.id, beto.id, beto.id}}
	r = enviar(ana, id, cuerpo)
	e.exigir(r, http.StatusCreated)
	if ms := r.cuerpo["menciones"].([]any); len(ms) != 2 {
		t.Fatalf("menciones: %v", r.cuerpo["menciones"])
	}
	for _, p := range []persona{beto, sup} {
		ev := e.ultimoEvento("chat.mencion", "usuario_id", fmt.Sprint(p.id))
		if ev["mensaje_id"] != id || numero(ev["autor_id"]) != ana.id || fmt.Sprint(ev["conversacion_id"]) != c || numero(ev["empresa_id"]) != 1 {
			t.Fatalf("chat.mencion: %v", ev)
		}
		if _, hay := ev["texto"]; hay {
			t.Fatal("chat.mencion lleva el texto")
		}
	}
	if n := e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'chat.mencion' AND payload->>'mensaje_id' = $1`, id); n != 2 {
		t.Fatalf("chat.mencion: %d (uno por mencionado, nunca el autor)", n)
	}
	// Reenviar el mismo id: 200 sin otro evento; con otras menciones, 409.
	e.exigir(enviar(ana, id, cuerpo), http.StatusOK)
	e.exigir(enviar(ana, id, map[string]any{"texto": cuerpo["texto"], "menciones": []int64{beto.id}}), http.StatusConflict,
		"type", "urn:iris:problema:mensaje_id_reutilizado")
	if n := e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'chat.mencion' AND payload->>'mensaje_id' = $1`, id); n != 2 {
		t.Fatalf("un reenvío emitió otra mención: %d", n)
	}

	// Contadores: la mención cuenta como «hace falta actuar»; un mensaje sin mención en la cuadrilla, no.
	e.exigir(enviar(ana, nuevoID(), map[string]any{"texto": "voy para allá"}), http.StatusCreated)
	conv := e.conversacionesDe(beto)[c]
	if numero(conv["no_leidos"]) != 2 || numero(conv["menciones"]) != 1 || conv["solo_lectura"] != false {
		t.Fatalf("conversación de Beto: %v", conv)
	}
	res := e.resumenChat(beto)
	if numero(res["no_leidos"]) != 2 || numero(res["menciones"]) != 1 || numero(res["requiere_accion"]) != 1 || numero(res["conversaciones_no_leidas"]) != 1 {
		t.Fatalf("resumen de Beto: %v", res)
	}
	if cu := res["por_tipo"].(map[string]any)["cuadrilla"].(map[string]any); numero(cu["menciones"]) != 1 || numero(cu["requiere_accion"]) != 1 {
		t.Fatalf("resumen por tipo: %v", res["por_tipo"])
	}
	// Directa: cada no leído requiere acción.
	r = e.chat(ana, http.MethodPost, "/directas", "", map[string]any{"usuario_id": beto.id})
	e.exigir(r, http.StatusCreated)
	e.exigir(e.enviarChat(ana, idConv(r), nuevoID(), "¿me confirmas la dirección?"), http.StatusCreated)
	if res = e.resumenChat(beto); numero(res["requiere_accion"]) != 2 || numero(res["no_leidos"]) != 3 {
		t.Fatalf("resumen con directa: %v", res)
	}
	// El feed trae las menciones de la conversación.
	r = e.chat(beto, http.MethodGet, "/cambios?cursor=0&limit=200", "", nil)
	e.exigir(r, http.StatusOK)
	vista := false
	for _, ev := range lista(r) {
		if fmt.Sprint(numero(ev["conversacion_id"])) == c && ev["tipo"] == "mensaje_nuevo" {
			vista = true
			if numero(ev["menciones"]) != 1 {
				t.Fatalf("evento sin menciones: %v", ev)
			}
		}
	}
	if !vista {
		t.Fatal("el feed no trae los mensajes de la cuadrilla")
	}
	// Los mensajes traen sus menciones (con nombre); el autor no ve contadores de lo suyo.
	r = e.chat(beto, http.MethodGet, "/conversaciones/"+c+"/mensajes", "", nil)
	e.exigir(r, http.StatusOK)
	ms := lista(r)
	if len(ms) != 2 || len(ms[0]["menciones"].([]any)) != 2 || len(ms[1]["menciones"].([]any)) != 0 {
		t.Fatalf("mensajes: %v", ms)
	}
	if res = e.resumenChat(ana); numero(res["menciones"]) != 0 {
		t.Fatalf("resumen de Ana: %v", res)
	}
	// Leer hasta el mensaje: la mención deja de contar.
	e.exigir(e.chat(beto, http.MethodPost, "/conversaciones/"+c+"/leer", "", map[string]any{"hasta_mensaje_id": id}), http.StatusOK)
	if conv = e.conversacionesDe(beto)[c]; numero(conv["menciones"]) != 0 || numero(conv["no_leidos"]) != 1 {
		t.Fatalf("tras leer: %v", conv)
	}

	// Eliminar un mensaje con mención: la lápida no las guarda y dejan de contar.
	id2 := nuevoID()
	e.exigir(enviar(ana, id2, map[string]any{"texto": "@Beto otra", "menciones": []int64{beto.id}}), http.StatusCreated)
	if numero(e.conversacionesDe(beto)[c]["menciones"]) != 1 {
		t.Fatal("la segunda mención no cuenta")
	}
	r = e.chat(ana, http.MethodPost, "/mensajes/"+id2+"/eliminar", "", nil)
	e.exigir(r, http.StatusOK, "eliminado", "true")
	if len(r.cuerpo["menciones"].([]any)) != 0 {
		t.Fatalf("la lápida trae menciones: %v", r.cuerpo)
	}
	if n := e.contar(`SELECT count(*) FROM mensajeria.chat_menciones WHERE mensaje_id = $1`, id2); n != 0 {
		t.Fatalf("menciones de la lápida: %d", n)
	}
	if numero(e.conversacionesDe(beto)[c]["menciones"]) != 0 {
		t.Fatal("una mención eliminada sigue contando")
	}
}

func TestChatBuscarYNuevoMensaje(t *testing.T) {
	e := nuevoEntorno(t, nil)
	q := e.equipo(1)
	uno := []int64{1}
	ana := e.persona(uno, rolBase("tecnico"))
	e.ficha(ana.id, q.cuadrilla1)
	beto := e.persona(uno, rolBase("despacho"))
	ventas := e.persona(uno, rolBase("ventas")) // sin chat.ver
	sup := e.persona(uno, identidad.AsignacionRol{Rol: "supervisor", ZonaID: &q.zona2})

	r := e.chat(ana, http.MethodPost, fmt.Sprintf("/cuadrillas/%d/abrir", q.cuadrilla1), "", nil)
	e.exigir(r, http.StatusCreated)
	cc := idConv(r)
	nombreCuadrilla := r.cuerpo["titulo"].(string)
	r = e.chat(ana, http.MethodPost, "/directas", "", map[string]any{"usuario_id": beto.id})
	e.exigir(r, http.StatusCreated)
	cd := idConv(r)
	o := e.orden(1, q.zona1, q.cuadrilla1, ana.id)
	r = e.chat(ana, http.MethodPost, fmt.Sprintf("/ordenes/%d/abrir", o), "", nil)
	e.exigir(r, http.StatusCreated, "solo_lectura", "false")
	co := idConv(r)
	if r.cuerpo["orden_tipo"] == nil {
		t.Fatalf("hilo de orden sin orden_tipo: %v", r.cuerpo)
	}
	numeroOrden := r.cuerpo["titulo"].(string)

	buscar := func(qs string) map[string]bool {
		r := e.chat(ana, http.MethodGet, "/conversaciones?q="+url.QueryEscape(qs), "", nil)
		e.exigir(r, http.StatusOK)
		out := map[string]bool{}
		for _, c := range lista(r) {
			out[fmt.Sprint(numero(c["id"]))] = true
		}
		return out
	}
	if v := buscar(strings.ToUpper(nombreCuadrilla[:8])); !v[cc] || v[cd] || v[co] {
		t.Fatalf("q cuadrilla: %v", v)
	}
	nombreBeto := e.conversacionesDe(ana)[cd]["titulo"].(string)
	if v := buscar(nombreBeto); !v[cd] || v[cc] {
		t.Fatalf("q persona: %v", v)
	}
	if v := buscar(numeroOrden); !v[co] || len(v) != 1 {
		t.Fatalf("q orden: %v", v)
	}
	if v := buscar("%"); len(v) != 0 {
		t.Fatalf("q con %% no se escapa: %v", v)
	}

	// Personas: activas, de la empresa, con chat.ver, sin uno mismo; con la directa si existe.
	r = e.chat(ana, http.MethodGet, "/personas?limit=200", "", nil)
	e.exigir(r, http.StatusOK)
	vistos := map[int64]map[string]any{}
	for _, p := range lista(r) {
		vistos[numero(p["id"])] = p
	}
	if _, ok := vistos[ana.id]; ok {
		t.Fatal("personas incluye al usuario")
	}
	if _, ok := vistos[ventas.id]; ok {
		t.Fatal("personas incluye a quien no tiene chat.ver")
	}
	if p, ok := vistos[beto.id]; !ok || fmt.Sprint(numero(p["conversacion_id"])) != cd {
		t.Fatalf("Beto: %v", p)
	}
	if p := vistos[sup.id]; p == nil || p["conversacion_id"] != nil {
		t.Fatalf("supervisión sin directa: %v", p)
	}
	r = e.chat(ana, http.MethodGet, "/personas?q="+url.QueryEscape(nombreBeto), "", nil)
	if ps := lista(r); len(ps) != 1 || numero(ps[0]["id"]) != beto.id {
		t.Fatalf("personas q: %v", ps)
	}
	// Paginación por cursor.
	r = e.chat(ana, http.MethodGet, "/personas?limit=1", "", nil)
	e.exigir(r, http.StatusOK)
	if len(lista(r)) != 1 || r.cuerpo["nextCursor"] == nil {
		t.Fatalf("personas página 1: %v", r.cuerpo)
	}
	r2 := e.chat(ana, http.MethodGet, "/personas?limit=1&cursor="+url.QueryEscape(r.cuerpo["nextCursor"].(string)), "", nil)
	e.exigir(r2, http.StatusOK)
	if numero(lista(r2)[0]["id"]) == numero(lista(r)[0]["id"]) {
		t.Fatal("el cursor repite la persona")
	}
	e.exigir(e.chat(ventas, http.MethodGet, "/personas", "", nil), http.StatusForbidden)

	// Cuadrillas: el técnico ve la suya (ya existe); la supervisión por zona 2, la cuadrilla 2.
	r = e.chat(ana, http.MethodGet, "/cuadrillas", "", nil)
	e.exigir(r, http.StatusOK)
	if cs := lista(r); len(cs) != 1 || numero(cs[0]["id"]) != q.cuadrilla1 || fmt.Sprint(numero(cs[0]["conversacion_id"])) != cc ||
		numero(cs[0]["tecnicos"]) != 1 || len(cs[0]["zonas"].([]any)) != 1 {
		t.Fatalf("cuadrillas del técnico: %v", cs)
	}
	r = e.chat(sup, http.MethodGet, "/cuadrillas", "", nil)
	if cs := lista(r); len(cs) != 1 || numero(cs[0]["id"]) != q.cuadrilla2 || cs[0]["conversacion_id"] != nil {
		t.Fatalf("cuadrillas de la supervisión: %v", cs)
	}
}

func TestChatMiembrosDeOrden(t *testing.T) {
	e := nuevoEntorno(t, nil)
	q := e.equipo(1)
	uno := []int64{1}
	tec := e.persona(uno, rolBase("tecnico"))
	e.ficha(tec.id, q.cuadrilla2)
	despacho := e.persona(uno, rolBase("despacho"))
	sup := e.persona(uno, identidad.AsignacionRol{Rol: "supervisor", CuadrillaID: &q.cuadrilla2})
	supOtra := e.persona(uno, identidad.AsignacionRol{Rol: "supervisor", ZonaID: &q.zona1})
	soporte := e.persona(uno, rolBase("soporte"))
	ventas := e.persona(uno, rolBase("ventas"))
	otraEmpresa := e.persona([]int64{2}, rolBase("soporte"))

	o := e.orden(1, q.zona2, q.cuadrilla2, tec.id)
	// Sin hilo todavía: 404 para todos (GET no lo crea).
	e.exigir(e.chat(tec, http.MethodGet, fmt.Sprintf("/ordenes/%d/conversacion", o), "", nil), http.StatusNotFound)
	r := e.chat(sup, http.MethodPost, fmt.Sprintf("/ordenes/%d/abrir", o), "", nil)
	e.exigir(r, http.StatusCreated)
	c := idConv(r)
	// El técnico asignado entra solo (sin abrir); despacho no lo ve hasta unirse.
	e.exigir(e.chat(tec, http.MethodGet, fmt.Sprintf("/ordenes/%d/conversacion", o), "", nil), http.StatusOK, "id", c)
	e.exigir(e.chat(despacho, http.MethodGet, fmt.Sprintf("/ordenes/%d/conversacion", o), "", nil), http.StatusNotFound)
	e.exigir(e.chat(despacho, http.MethodPost, fmt.Sprintf("/ordenes/%d/abrir", o), "", nil), http.StatusCreated, "id", c)
	e.exigir(e.chat(despacho, http.MethodGet, fmt.Sprintf("/ordenes/%d/conversacion", o), "", nil), http.StatusOK, "id", c)

	motivos := func(p persona) map[int64]string {
		r := e.chat(p, http.MethodGet, "/conversaciones/"+c+"/miembros", "", nil)
		e.exigir(r, http.StatusOK)
		out := map[int64]string{}
		for _, m := range lista(r) {
			out[numero(m["usuario_id"])] = m["motivo"].(string)
		}
		return out
	}
	m := motivos(tec)
	if m[tec.id] != "tecnico" || m[despacho.id] != "despacho" || m[sup.id] != "supervision" {
		t.Fatalf("miembros: %v", m)
	}
	if _, ok := m[supOtra.id]; ok {
		t.Fatalf("la supervisión de otra zona es miembro: %v", m)
	}

	agregar := func(p persona, usuario int64) respuesta {
		return e.chat(p, http.MethodPost, "/conversaciones/"+c+"/miembros", "", map[string]any{"usuario_id": usuario})
	}
	// Sin chat.supervisar: 403. Supervisión que no ve el hilo: 404.
	e.exigir(agregar(despacho, soporte.id), http.StatusForbidden)
	e.exigir(agregar(tec, soporte.id), http.StatusForbidden)
	e.exigir(agregar(supOtra, soporte.id), http.StatusNotFound)
	// El agregado debe poder usar el chat y operar en la empresa.
	e.exigir(agregar(sup, ventas.id), http.StatusUnprocessableEntity, "type", "urn:iris:problema:usuario_no_disponible")
	e.exigir(agregar(sup, otraEmpresa.id), http.StatusUnprocessableEntity, "type", "urn:iris:problema:usuario_no_disponible")

	e.exigir(e.chat(soporte, http.MethodGet, "/conversaciones/"+c, "", nil), http.StatusNotFound)
	r = agregar(sup, soporte.id)
	e.exigir(r, http.StatusCreated)
	if m = motivos(sup); m[soporte.id] != "agregado" {
		t.Fatalf("tras agregar: %v", m)
	}
	ev := e.ultimoEvento("chat.miembro_agregado", "conversacion_id", c)
	if numero(ev["usuario_id"]) != soporte.id || numero(ev["agregado_por"]) != sup.id || numero(ev["orden_id"]) != o || numero(ev["empresa_id"]) != 1 {
		t.Fatalf("chat.miembro_agregado: %v", ev)
	}
	if n := e.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'chat.agregar_miembro' AND resultado = 'ok' AND entidad_id = $1`, c); n != 1 {
		t.Fatalf("auditoría: %d", n)
	}
	// El agregado lo ve, lo lista, puede escribir y mencionarse entre ellos.
	e.exigir(e.chat(soporte, http.MethodGet, "/conversaciones/"+c, "", nil), http.StatusOK)
	if _, ok := e.conversacionesDe(soporte)[c]; !ok {
		t.Fatal("el agregado no lista el hilo")
	}
	e.exigir(e.chat(soporte, http.MethodPost, "/conversaciones/"+c+"/mensajes", "", map[string]any{"id": nuevoID(), "texto": "@Tomás ¿qué potencia?",
		"menciones": []int64{tec.id}}), http.StatusCreated)
	// Repetir: 200 sin otro evento; agregar a quien ya lo ve (despacho): 200.
	e.exigir(agregar(sup, soporte.id), http.StatusOK)
	e.exigir(agregar(sup, despacho.id), http.StatusOK)
	if n := e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'chat.miembro_agregado' AND payload->>'conversacion_id' = $1`, c); n != 1 {
		t.Fatalf("chat.miembro_agregado: %d", n)
	}
	// Una directa no admite miembros.
	r = e.chat(sup, http.MethodPost, "/directas", "", map[string]any{"usuario_id": tec.id})
	e.exigir(r, http.StatusCreated)
	e.exigir(e.chat(sup, http.MethodPost, "/conversaciones/"+idConv(r)+"/miembros", "", map[string]any{"usuario_id": soporte.id}),
		http.StatusUnprocessableEntity, "type", "urn:iris:problema:tipo_no_admite_miembros")
	// Supervisión que ve el hilo por otra vía (unida desde despacho) pero sin alcance: 403.
	supDespacho := e.persona(uno, rolBase("despacho"), identidad.AsignacionRol{Rol: "supervisor", ZonaID: &q.zona1})
	e.exigir(e.chat(supDespacho, http.MethodPost, fmt.Sprintf("/ordenes/%d/abrir", o), "", nil), http.StatusCreated)
	e.exigir(e.chat(supDespacho, http.MethodPost, "/conversaciones/"+c+"/miembros", "", map[string]any{"usuario_id": ventas.id}),
		http.StatusForbidden, "type", "urn:iris:problema:fuera_de_alcance")
}

func TestChatSoloLecturaAlCerrarLaOrden(t *testing.T) {
	e := nuevoEntorno(t, nil)
	q := e.equipo(1)
	uno := []int64{1}
	tec := e.persona(uno, rolBase("tecnico"))
	sup := e.persona(uno, rolBase("supervisor"))
	soporte := e.persona(uno, rolBase("soporte"))
	for _, fin := range [][2]string{{"cerrada", "exitosa"}, {"cancelada", "cancelada"}, {"fallida", "fallida"}} {
		o := e.orden(1, q.zona1, q.cuadrilla1, tec.id)
		r := e.chat(tec, http.MethodPost, fmt.Sprintf("/ordenes/%d/abrir", o), "", nil)
		e.exigir(r, http.StatusCreated)
		c := idConv(r)
		previo := nuevoID()
		cuerpo := map[string]any{"id": previo, "texto": "Ya quedó, −19.8 dBm"}
		e.exigir(e.chat(tec, http.MethodPost, "/conversaciones/"+c+"/mensajes", "", cuerpo), http.StatusCreated)
		e.cerrarOrden(o, fin[0], fin[1])

		e.exigir(e.chat(tec, http.MethodGet, "/conversaciones/"+c, "", nil), http.StatusOK, "solo_lectura", "true")
		if e.conversacionesDe(sup)[c]["solo_lectura"] != true {
			t.Fatalf("%s: la lista no marca solo lectura", fin[0])
		}
		e.exigir(e.enviarChat(tec, c, nuevoID(), "una cosa más"), http.StatusUnprocessableEntity, "type", "urn:iris:problema:conversacion_cerrada")
		e.exigir(e.enviarChat(sup, c, nuevoID(), "gracias"), http.StatusUnprocessableEntity, "type", "urn:iris:problema:conversacion_cerrada")
		// La cola sin conexión reenvía lo que ya había llegado: 200.
		e.exigir(e.chat(tec, http.MethodPost, "/conversaciones/"+c+"/mensajes", "", cuerpo), http.StatusOK)
		// Leer sigue: mensajes, leído, miembros; y retirar lo propio dentro del plazo.
		r = e.chat(sup, http.MethodGet, "/conversaciones/"+c+"/mensajes", "", nil)
		e.exigir(r, http.StatusOK)
		e.exigir(e.chat(sup, http.MethodPost, "/conversaciones/"+c+"/leer", "", map[string]any{"hasta_mensaje_id": previo}), http.StatusOK)
		e.exigir(e.chat(sup, http.MethodGet, "/conversaciones/"+c+"/miembros", "", nil), http.StatusOK)
		e.exigir(e.chat(sup, http.MethodPost, "/conversaciones/"+c+"/miembros", "", map[string]any{"usuario_id": soporte.id}),
			http.StatusUnprocessableEntity, "type", "urn:iris:problema:conversacion_cerrada")
		e.exigir(e.chat(tec, http.MethodPost, "/mensajes/"+previo+"/eliminar", "", nil), http.StatusOK, "eliminado", "true")
		if n := e.contar(`SELECT count(*) FROM mensajeria.chat_mensajes WHERE conversacion_id = $1`, c); n != 1 {
			t.Fatalf("%s: mensajes %d", fin[0], n)
		}
	}
	// Reabrir la orden devuelve la escritura (se deriva al leer).
	o := e.orden(1, q.zona1, q.cuadrilla1, tec.id)
	r := e.chat(tec, http.MethodPost, fmt.Sprintf("/ordenes/%d/abrir", o), "", nil)
	c := idConv(r)
	e.cerrarOrden(o, "cerrada", "exitosa")
	if _, err := e.bd.Exec(e.ctx, `UPDATE campo.ordenes SET estado = 'en_sitio', resultado = NULL, finalizada_en = NULL WHERE id = $1`, o); err != nil {
		t.Fatal(err)
	}
	e.exigir(e.enviarChat(tec, c, nuevoID(), "reabierta"), http.StatusCreated)
}

func TestChatPanelAislamientoEmpresa(t *testing.T) {
	e := nuevoEntorno(t, nil)
	q1, q2 := e.equipo(1), e.equipo(2)
	tec1 := e.persona([]int64{1}, rolBase("tecnico"))
	e.ficha(tec1.id, q1.cuadrilla1)
	sup1 := e.persona([]int64{1}, rolBase("supervisor"))
	tec2 := e.persona([]int64{2}, rolBase("tecnico"))
	e.ficha(tec2.id, q2.cuadrilla1)
	sup2 := e.persona([]int64{2}, rolBase("supervisor"))

	o := e.orden(1, q1.zona1, q1.cuadrilla1, tec1.id)
	r := e.chat(tec1, http.MethodPost, fmt.Sprintf("/ordenes/%d/abrir", o), "", nil)
	e.exigir(r, http.StatusCreated)
	c := idConv(r)
	// Mencionar a alguien de otra empresa: 422.
	e.exigir(e.chat(tec1, http.MethodPost, "/conversaciones/"+c+"/mensajes", "", map[string]any{"id": nuevoID(), "texto": "@X", "menciones": []int64{sup2.id}}),
		http.StatusUnprocessableEntity, "type", "urn:iris:problema:mencion_invalida")
	e.exigir(e.chat(tec1, http.MethodPost, "/conversaciones/"+c+"/mensajes", "", map[string]any{"id": nuevoID(), "texto": "@Sup", "menciones": []int64{sup1.id}}),
		http.StatusCreated)
	// La supervisión de la empresa 2 no ve, no lista miembros, no agrega ni encuentra el hilo por la orden.
	e.exigir(e.chat(sup2, http.MethodGet, fmt.Sprintf("/ordenes/%d/conversacion", o), "", nil), http.StatusNotFound)
	e.exigir(e.chat(sup2, http.MethodGet, "/conversaciones/"+c+"/miembros", "", nil), http.StatusNotFound)
	e.exigir(e.chat(sup2, http.MethodPost, "/conversaciones/"+c+"/miembros", "", map[string]any{"usuario_id": tec2.id}), http.StatusNotFound)
	// Ni agregar desde la empresa 1 a alguien de la 2.
	e.exigir(e.chat(sup1, http.MethodPost, "/conversaciones/"+c+"/miembros", "", map[string]any{"usuario_id": tec2.id}),
		http.StatusUnprocessableEntity, "type", "urn:iris:problema:usuario_no_disponible")
	// Contadores, personas y cuadrillas solo de la empresa activa.
	if res := e.resumenChat(sup2); numero(res["no_leidos"]) != 0 || numero(res["menciones"]) != 0 {
		t.Fatalf("resumen de la empresa 2: %v", res)
	}
	if res := e.resumenChat(sup1); numero(res["menciones"]) != 1 {
		t.Fatalf("resumen de la empresa 1: %v", res)
	}
	r = e.chat(sup2, http.MethodGet, "/personas?limit=200", "", nil)
	e.exigir(r, http.StatusOK)
	for _, p := range lista(r) {
		if id := numero(p["id"]); id == tec1.id || id == sup1.id {
			t.Fatalf("personas de la empresa 2 incluye a %d", id)
		}
	}
	r = e.chat(sup2, http.MethodGet, "/cuadrillas", "", nil)
	e.exigir(r, http.StatusOK)
	for _, cu := range lista(r) {
		if id := numero(cu["id"]); id == q1.cuadrilla1 || id == q1.cuadrilla2 {
			t.Fatalf("cuadrillas de la empresa 2 incluye la %d", id)
		}
	}
	r = e.chat(sup2, http.MethodGet, "/conversaciones?q=OT", "", nil)
	e.exigir(r, http.StatusOK)
	for _, cv := range lista(r) {
		if fmt.Sprint(numero(cv["id"])) == c {
			t.Fatal("la búsqueda de la empresa 2 trae un hilo de la 1")
		}
	}
}

func TestChatResumenTopeDeFrecuencia(t *testing.T) {
	e := nuevoEntorno(t, nil)
	q := e.equipo(1)
	ana := e.persona([]int64{1}, rolBase("tecnico"))
	beto := e.persona([]int64{1}, rolBase("tecnico"))
	e.ficha(ana.id, q.cuadrilla1)
	e.ficha(beto.id, q.cuadrilla1)
	reloj := time.Date(2026, 11, 7, 15, 0, 0, 0, time.UTC)
	e.m.FijarRelojResumen(func() time.Time { return reloj })
	t.Cleanup(func() { e.m.FijarRelojResumen(time.Now) })
	e.m.VaciarResumenes()

	r := e.chat(ana, http.MethodPost, fmt.Sprintf("/cuadrillas/%d/abrir", q.cuadrilla1), "", nil)
	e.exigir(r, http.StatusCreated)
	c := idConv(r)
	e.exigir(e.enviarChat(ana, c, nuevoID(), "uno"), http.StatusCreated)

	leer := func(p persona) int64 {
		t.Helper()
		r := e.chat(p, http.MethodGet, "/resumen", "", nil)
		e.exigir(r, http.StatusOK)
		return numero(r.cuerpo["no_leidos"])
	}
	antes := e.m.ConsultasResumen()
	// Dos llamadas seguidas: una sola consulta.
	if n := leer(beto); n != 1 {
		t.Fatalf("no leídos: %d", n)
	}
	if n := leer(beto); n != 1 {
		t.Fatalf("no leídos (repetido): %d", n)
	}
	if d := e.m.ConsultasResumen() - antes; d != 1 {
		t.Fatalf("consultas: %d (esperaba 1)", d)
	}
	// Dentro de los 2 s, un mensaje nuevo de otro aún no se ve (tope de frecuencia)...
	e.exigir(e.enviarChat(ana, c, nuevoID(), "dos"), http.StatusCreated)
	reloj = reloj.Add(1900 * time.Millisecond)
	if n := leer(beto); n != 1 || e.m.ConsultasResumen()-antes != 1 {
		t.Fatalf("dentro del tope: %d no leídos, %d consultas", n, e.m.ConsultasResumen()-antes)
	}
	// ...y al vencer se recalcula.
	reloj = reloj.Add(200 * time.Millisecond)
	if n := leer(beto); n != 2 || e.m.ConsultasResumen()-antes != 2 {
		t.Fatalf("vencido: %d no leídos, %d consultas", n, e.m.ConsultasResumen()-antes)
	}
	// Cada usuario (y empresa) tiene el suyo.
	leer(ana)
	if d := e.m.ConsultasResumen() - antes; d != 3 {
		t.Fatalf("otro usuario: %d consultas", d)
	}
	// Marcar leído olvida el suyo: su contador baja enseguida.
	ms := lista(e.chat(beto, http.MethodGet, "/conversaciones/"+c+"/mensajes", "", nil))
	e.exigir(e.chat(beto, http.MethodPost, "/conversaciones/"+c+"/leer", "", map[string]any{"hasta_mensaje_id": ms[len(ms)-1]["id"]}), http.StatusOK)
	if n := leer(beto); n != 0 {
		t.Fatalf("tras leer: %d", n)
	}
}

func TestChatAgregarMiembroSinCambiosNoEmite(t *testing.T) {
	e := nuevoEntorno(t, nil)
	q := e.equipo(1)
	tec := e.persona([]int64{1}, rolBase("tecnico"))
	sup := e.persona([]int64{1}, rolBase("supervisor"))
	soporte := e.persona([]int64{1}, rolBase("soporte"))
	o := e.orden(1, q.zona1, q.cuadrilla1, tec.id)
	r := e.chat(sup, http.MethodPost, fmt.Sprintf("/ordenes/%d/abrir", o), "", nil)
	e.exigir(r, http.StatusCreated)
	c := idConv(r)
	// Una fila que no le deja ver el hilo y que AgregarMiembroChat no convierte (solo «unido» pasa a
	// «agregado»): el INSERT no afecta filas.
	if _, err := e.bd.Exec(e.ctx, `INSERT INTO mensajeria.chat_miembros (conversacion_id, usuario_id, origen) VALUES ($1, $2, 'directa')`, c, soporte.id); err != nil {
		t.Fatal(err)
	}
	e.exigir(e.chat(soporte, http.MethodGet, "/conversaciones/"+c, "", nil), http.StatusNotFound)
	e.exigir(e.chat(sup, http.MethodPost, "/conversaciones/"+c+"/miembros", "", map[string]any{"usuario_id": soporte.id}), http.StatusOK)
	if n := e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'chat.miembro_agregado' AND payload->>'conversacion_id' = $1`, c); n != 0 {
		t.Fatalf("chat.miembro_agregado sin cambios: %d", n)
	}
	if n := e.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'chat.agregar_miembro' AND resultado = 'ok' AND entidad_id = $1`, c); n != 0 {
		t.Fatalf("auditoría sin cambios: %d", n)
	}
	if n := e.contar(`SELECT count(*) FROM mensajeria.chat_eventos WHERE conversacion_id = $1 AND tipo = 'conversacion_actualizada'`, c); n != 1 {
		t.Fatalf("feed: %d (solo el de abrir)", n)
	}
}
