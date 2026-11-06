package mensajeria_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// Horarios, fuera de horario y manuales (contratos/conversaciones.md §10–§11). El reloj de las
// pruebas es el jueves 5 de noviembre de 2026, 10:00 en Guatemala (16:00 UTC).

func (b *bandeja) horario(franjas []map[string]any, feriados []map[string]any) map[string]any {
	b.t.Helper()
	r := b.api(b.supervisor, http.MethodPost, "/horarios", map[string]any{"nombre": "Horario " + pruebas.Unico(), "franjas": franjas, "feriados": feriados})
	b.exigir(r, http.StatusCreated)
	return r.cuerpo
}

// configurarNumero fija horario y fuera de horario del número de la prueba.
func (b *bandeja) configurarNumero(horario any, fuera map[string]any) respuesta {
	b.t.Helper()
	l := b.api(b.supervisor, http.MethodGet, "/conversaciones/canales", nil)
	b.exigir(l, http.StatusOK)
	var version any
	for _, x := range l.cuerpo["data"].([]any) {
		c := x.(map[string]any)
		if int64(c["id"].(float64)) == b.canal.ID {
			version = c["version"]
		}
	}
	return b.api(b.supervisor, http.MethodPost, fmt.Sprintf("/canales/%d/configurar", b.canal.ID),
		map[string]any{"version": version, "usos": []string{"soporte"}, "avisosEmpresa": false, "colaId": b.cola, "horarioId": horario, "fueraDeHorario": fuera})
}

func todosLosDias(inicio, fin string) []map[string]any {
	return []map[string]any{{"dias": []int{1, 2, 3, 4, 5, 6, 7}, "inicio": inicio, "fin": fin}}
}

func TestHorarioEstadoYValidacion(t *testing.T) {
	b := nuevaBandeja(t, nil)
	h := b.horario([]map[string]any{{"dias": []int{1, 2, 3, 4, 5}, "inicio": "06:00", "fin": "22:00", "perfil": "solo_sugerir"},
		{"dias": []int{6}, "inicio": "08:00", "fin": "12:00"}}, []map[string]any{{"fecha": "2026-11-06", "nombre": "Feriado", "franjas": []any{}}})
	id := fmt.Sprint(int64(h["id"].(float64)))
	estado := func(en string) map[string]any {
		r := b.api(b.agente, http.MethodGet, "/horarios/"+id+"/estado?en="+en, nil)
		b.exigir(r, http.StatusOK)
		return r.cuerpo
	}
	// Jueves 10:00 Guatemala: abierto, con el perfil de la franja (fase 2), cierra a las 22:00.
	e := estado("2026-11-05T16:00:00Z")
	if e["abierto"] != true || e["franja"].(map[string]any)["perfil"] != "solo_sugerir" || e["proximoCambio"] != "2026-11-06T04:00:00Z" {
		t.Fatalf("jueves: %v", e)
	}
	// Jueves 23:00: cerrado; el viernes es feriado, abre el sábado 08:00 (14:00 UTC).
	if e := estado("2026-11-06T05:00:00Z"); e["abierto"] != false || e["proximoCambio"] != "2026-11-07T14:00:00Z" {
		t.Fatalf("feriado: %v", e)
	}
	// Validación: franjas que se solapan, zona inválida, fin antes de inicio.
	for _, cuerpo := range []map[string]any{
		{"nombre": "x" + pruebas.Unico(), "franjas": []map[string]any{{"dias": []int{1}, "inicio": "08:00", "fin": "12:00"}, {"dias": []int{1}, "inicio": "11:00", "fin": "13:00"}}},
		{"nombre": "y" + pruebas.Unico(), "zona": "Marte/Olimpo", "franjas": todosLosDias("08:00", "12:00")},
		{"nombre": "z" + pruebas.Unico(), "franjas": todosLosDias("12:00", "08:00")},
	} {
		b.exigir(b.api(b.supervisor, http.MethodPost, "/horarios", cuerpo), http.StatusUnprocessableEntity)
	}
	// Soporte no configura horarios; versión vieja: 409.
	b.exigir(b.api(b.agente, http.MethodPost, "/horarios", map[string]any{"nombre": "n", "franjas": []any{}}), http.StatusForbidden)
	b.exigir(b.api(b.supervisor, http.MethodPost, "/horarios/"+id+"/actualizar", map[string]any{"version": 99, "nombre": "otro", "franjas": []any{}}),
		http.StatusConflict)
	// El número usa el horario; asistente activo es de la fase 2.
	b.exigir(b.configurarNumero(h["id"], map[string]any{"responder": false, "asistente": map[string]any{"activo": true}}), http.StatusUnprocessableEntity,
		"type", "urn:iris:problema:disponible_en_fase_2")
	b.exigir(b.configurarNumero(h["id"], map[string]any{"responder": false}), http.StatusOK)
}

func TestFueraDeHorarioUnaVezPorVentana(t *testing.T) {
	b := nuevaBandeja(t, nil)
	// Abierto de 12:00 a 13:00 Guatemala: a las 10:00 está cerrado.
	h := b.horario(todosLosDias("12:00", "13:00"), nil)
	b.exigir(b.configurarNumero(h["id"], map[string]any{"responder": true, "mensaje": "Gracias por escribir. Te respondemos al abrir."}), http.StatusOK)
	num := numeroCliente()
	automaticas := func() int {
		c := b.conv(num)
		n := 0
		for _, m := range b.mensajes(b.supervisor, cid(c)) {
			if m["direccion"] == "saliente" && m["autor"] == nil {
				n++
			}
		}
		return n
	}
	b.texto(num, "wamid.k1", "Hola")
	b.texto(num, "wamid.k2", "¿Hay alguien?")
	b.texto(num, "wamid.k3", "Sigo sin internet")
	if n := automaticas(); n != 1 {
		t.Fatalf("respuestas automáticas en la misma ventana: %d", n)
	}
	c := b.conv(num)
	// El SLA (60 min) empieza al abrir: 12:00 + 1 h = 13:00 Guatemala = 19:00 UTC.
	if v := c["sla"].(map[string]any)["venceEn"]; v != "2026-11-05T19:00:00Z" {
		t.Fatalf("SLA con horario: %v", c["sla"])
	}
	// En horario no contesta sola.
	*b.reloj = time.Date(2026, 11, 5, 18, 30, 0, 0, time.UTC)
	b.texto(num, "wamid.k4", "¿Ya abrieron?")
	if n := automaticas(); n != 1 {
		t.Fatalf("en horario: %d", n)
	}
	// Otra ventana cerrada (14:00 Guatemala): vuelve a contestar una vez.
	*b.reloj = time.Date(2026, 11, 5, 20, 0, 0, 0, time.UTC)
	b.texto(num, "wamid.k5", "Hola de nuevo")
	b.texto(num, "wamid.k6", "Hola de nuevo 2")
	if n := automaticas(); n != 2 {
		t.Fatalf("otra ventana: %d", n)
	}
	if n := b.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'conversacion.fuera_de_horario' AND payload->>'conversacion_id' = $1`, cid(c)); n != 2 {
		t.Fatalf("conversacion.fuera_de_horario: %d", n)
	}
	// Un cliente con baja vigente no recibe la respuesta automática.
	otro := numeroCliente()
	b.texto(otro, "wamid.k7", "Hola")
	co := b.conv(otro)
	cliente := b.cliente(1, "5512-0003")
	if _, err := b.bd.Exec(b.ctx, `UPDATE mensajeria.conversaciones SET cliente_id = $2, fuera_horario_hasta = NULL WHERE id = $1`, cid(co), cliente); err != nil {
		t.Fatal(err)
	}
	b.exigir(b.consentir(cliente, "baja", b.reloj.Add(-time.Minute)), http.StatusCreated)
	b.texto(otro, "wamid.k8", "Hola otra vez")
	for _, m := range b.mensajes(b.supervisor, cid(co)) {
		if m["direccion"] == "saliente" && m["texto"] != nil && strings.Contains(m["texto"].(string), "Gracias") && m["seq"].(float64) > 3 {
			t.Fatal("respondió a un cliente con baja")
		}
	}
	// Quien escribe BAJA como primer mensaje fuera de horario tampoco recibe la respuesta automática.
	tercero := numeroCliente()
	b.texto(tercero, "wamid.k9", "BAJA")
	for _, m := range b.mensajes(b.supervisor, cid(b.conv(tercero))) {
		if m["direccion"] == "saliente" {
			t.Fatalf("respondió a quien pidió baja: %v", m)
		}
	}
}

func TestGuardiaPorEtiquetaYFallaMasiva(t *testing.T) {
	b := nuevaBandeja(t, nil)
	h := b.horario(todosLosDias("12:00", "13:00"), nil)
	et := b.api(b.supervisor, http.MethodPost, "/etiquetas", map[string]any{"nombre": "Empresarial " + pruebas.Unico()})
	b.exigir(et, http.StatusCreated)
	guardia := b.persona([]int64{1}, rolBase("soporte"))
	b.exigir(b.configurarNumero(h["id"], map[string]any{"responder": false,
		"guardia": map[string]any{"activa": true, "usuarioId": guardia.id, "etiquetaIds": []any{et.cuerpo["id"]}, "fallaMasiva": true}}), http.StatusOK)
	num := numeroCliente()
	b.texto(num, "wamid.l1", "Hola")
	c := b.conv(num)
	b.exigir(b.api(guardia, http.MethodGet, "/conversaciones/"+cid(c), nil), http.StatusNotFound)
	b.exigir(b.api(b.agente, http.MethodPost, "/conversaciones/"+cid(c)+"/etiquetar", map[string]any{"version": ver(c), "etiquetaIds": []any{et.cuerpo["id"]}}), http.StatusOK)
	b.texto(num, "wamid.l2", "Urgente, se cayó la empresa")
	b.texto(num, "wamid.l3", "Urgente 2")
	b.exigir(b.api(guardia, http.MethodGet, "/conversaciones/"+cid(c), nil), http.StatusOK)
	if n := b.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'conversacion.guardia_avisada' AND payload->>'conversacion_id' = $1`, cid(c)); n != 1 {
		t.Fatalf("guardia una vez por ventana: %d", n)
	}
	r := b.api(guardia, http.MethodGet, "/conversaciones/cambios?cursor=0&limit=200", nil)
	b.exigir(r, http.StatusOK)
	if !strings.Contains(fmt.Sprint(r.cuerpo["data"]), "tipo:guardia") {
		t.Fatalf("evento guardia en el flujo: %v", r.cuerpo)
	}
	// Falla masiva: el router del servicio del cliente está caído según Horus.
	otro := numeroCliente()
	cliente := siguienteCliente.Add(1)
	router := siguienteCliente.Add(1)
	if _, err := b.bd.Exec(b.ctx, `INSERT INTO integracion.argos_clientes (id, empresa_id, nombre, sincronizado_en, empresas) VALUES ($1, 1, 'Fábrica', now(), '{1}')`, cliente); err != nil {
		t.Fatal(err)
	}
	if _, err := b.bd.Exec(b.ctx, `INSERT INTO integracion.argos_servicios (id, empresa_id, cliente_id, router_id, sincronizado_en) VALUES ($1, 1, $2, $3, now())`,
		cliente*10, cliente, router); err != nil {
		t.Fatal(err)
	}
	if _, err := b.bd.Exec(b.ctx, `INSERT INTO soporte.horus_estados (clave, empresa_id, estado, ocurrido_en, entrante_id) VALUES ($1, 1, 'caido', now(), 1)`,
		fmt.Sprintf("router:1:%d", router)); err != nil {
		t.Fatal(err)
	}
	b.texto(otro, "wamid.l4", "Hola")
	co := b.conv(otro)
	if _, err := b.bd.Exec(b.ctx, `UPDATE mensajeria.conversaciones SET cliente_id = $2 WHERE id = $1`, cid(co), cliente); err != nil {
		t.Fatal(err)
	}
	b.texto(otro, "wamid.l5", "No hay internet")
	if p := b.ultimoEvento("conversacion.guardia_avisada", "conversacion_id", cid(co)); p["motivo"] != "falla_masiva" {
		t.Fatalf("falla masiva: %v", p)
	}
}

func TestManualesBusquedaSugerenciasYEnvio(t *testing.T) {
	b := nuevaBandeja(t, nil)
	crear := func(cuerpo map[string]any) map[string]any {
		r := b.api(b.supervisor, http.MethodPost, "/manuales", cuerpo)
		b.exigir(r, http.StatusCreated)
		return r.cuerpo
	}
	etiqueta := "Sin servicio " + pruebas.Unico()
	diag := crear(map[string]any{"titulo": "Sin servicio en fibra: diagnóstico", "resumen": "Revisar luces de la ONU",
		"cuerpo": "Revisar luces de la ONU, falla en la zona, potencia y reinicio guiado.", "etiquetas": []string{etiqueta}, "visibilidad": "interna"})
	onu := crear(map[string]any{"titulo": "Reiniciar la ONU ZTE F670L", "cuerpo": "Apagar 30 segundos.\n\nRevisar la luz PON <b>verde</b>.",
		"equipo": "ZTE F670L", "visibilidad": "cliente"})
	visita := crear(map[string]any{"titulo": "Cuándo agendar visita", "cuerpo": "Si la luz LOS sigue roja después del reinicio, agenda una visita técnica.",
		"visibilidad": "interna"})
	// Búsqueda en español (raíces): «reinicios» encuentra «reinicio».
	r := b.api(b.agente, http.MethodGet, "/manuales?q=reinicios", nil)
	b.exigir(r, http.StatusOK)
	if len(r.cuerpo["data"].([]any)) < 2 {
		t.Fatalf("búsqueda: %v", r.cuerpo)
	}
	b.exigir(b.api(b.agente, http.MethodPost, "/manuales", map[string]any{"titulo": "x", "cuerpo": "y", "visibilidad": "interna"}), http.StatusForbidden)

	// Conversación con la etiqueta, el equipo en Argos y un ticket abierto «visita».
	num := numeroCliente()
	b.texto(num, "wamid.m1", "Hola")
	c := b.conv(num)
	et := b.api(b.supervisor, http.MethodPost, "/etiquetas", map[string]any{"nombre": etiqueta})
	b.exigir(et, http.StatusCreated)
	b.exigir(b.api(b.agente, http.MethodPost, "/conversaciones/"+cid(c)+"/etiquetar", map[string]any{"version": ver(c), "etiquetaIds": []any{et.cuerpo["id"]}}), http.StatusOK)
	cliente := siguienteCliente.Add(1)
	if _, err := b.bd.Exec(b.ctx, `INSERT INTO integracion.argos_clientes (id, empresa_id, nombre, sincronizado_en, empresas) VALUES ($1, 1, 'Carlos', now(), '{1}')`, cliente); err != nil {
		t.Fatal(err)
	}
	if _, err := b.bd.Exec(b.ctx, `INSERT INTO integracion.argos_servicios (id, empresa_id, cliente_id, sincronizado_en) VALUES ($1, 1, $2, now())`, cliente*10, cliente); err != nil {
		t.Fatal(err)
	}
	if _, err := b.bd.Exec(b.ctx, `UPDATE mensajeria.conversaciones SET cliente_id = $2, servicio_id = $3 WHERE id = $1`, cid(c), cliente, cliente*10); err != nil {
		t.Fatal(err)
	}
	b.argos.modelos = []string{"ZTE F670L"}
	b.exigir(b.api(b.agente, http.MethodPost, "/conversaciones/"+cid(c)+"/crear-ticket", map[string]any{"asunto": "Agendar visita técnica"}), http.StatusCreated)
	s := b.api(b.agente, http.MethodGet, "/conversaciones/"+cid(c)+"/manuales-sugeridos", nil)
	b.exigir(s, http.StatusOK)
	motivos := map[float64]string{}
	for _, x := range s.cuerpo["data"].([]any) {
		m := x.(map[string]any)
		motivos[m["id"].(float64)] = fmt.Sprint(m["motivos"].([]any)[0].(map[string]any)["tipo"])
	}
	if motivos[diag["id"].(float64)] != "etiqueta" || motivos[onu["id"].(float64)] != "equipo" || motivos[visita["id"].(float64)] != "ticket_abierto" {
		t.Fatalf("sugerencias con su motivo: %v", s.cuerpo)
	}

	// Enviar: interno no; para el cliente, por enlace público.
	ruta := "/conversaciones/" + cid(c) + "/enviar-manual"
	b.exigir(b.api(b.agente, http.MethodPost, ruta, map[string]any{"id": nuevoID(), "manualId": diag["id"], "modo": "enlace"}), http.StatusUnprocessableEntity,
		"type", "urn:iris:problema:manual_interno")
	env := b.api(b.agente, http.MethodPost, ruta, map[string]any{"id": nuevoID(), "manualId": onu["id"], "modo": "enlace"})
	b.exigir(env, http.StatusCreated, "direccion", "saliente")
	texto := env.cuerpo["texto"].(string)
	i := strings.Index(texto, "/api/v1/mensajeria/publico/manuales/")
	if i < 0 {
		t.Fatalf("enlace: %q", texto)
	}
	publico := func(ruta string) (int, string) {
		w := httptest.NewRecorder()
		b.h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, ruta, nil))
		cuerpo, _ := io.ReadAll(w.Body)
		return w.Code, string(cuerpo)
	}
	code, html := publico(texto[i:])
	if code != http.StatusOK || !strings.Contains(html, "Reiniciar la ONU") || strings.Contains(html, "<b>") || !strings.Contains(html, "&lt;b&gt;") {
		t.Fatalf("página pública: %d %s", code, html)
	}
	// Revocar un enlace: lo ve la conversación (sin token); solo supervisión revoca; luego 410.
	env2 := b.api(b.agente, http.MethodPost, ruta, map[string]any{"id": nuevoID(), "manualId": onu["id"], "modo": "enlace"})
	b.exigir(env2, http.StatusCreated)
	texto2 := env2.cuerpo["texto"].(string)
	texto2 = texto2[strings.Index(texto2, "/api/v1/mensajeria/publico/manuales/"):]
	le := b.api(b.agente, http.MethodGet, "/conversaciones/"+cid(c)+"/manual-enlaces", nil)
	b.exigir(le, http.StatusOK)
	enlaces := le.cuerpo["data"].([]any)
	if len(enlaces) != 2 || strings.Contains(fmt.Sprint(le.cuerpo), texto2[len("/api/v1/mensajeria/publico/manuales/"):]) {
		t.Fatalf("enlaces de la conversación: %v", le.cuerpo)
	}
	ultimo := fmt.Sprint(int64(enlaces[0].(map[string]any)["id"].(float64)))
	b.exigir(b.api(b.agente, http.MethodPost, "/manual-enlaces/"+ultimo+"/revocar", nil), http.StatusForbidden)
	b.exigir(b.api(b.supervisor, http.MethodPost, "/manual-enlaces/"+ultimo+"/revocar", nil), http.StatusOK)
	b.exigir(b.api(b.supervisor, http.MethodPost, "/manual-enlaces/"+ultimo+"/revocar", nil), http.StatusOK)
	if code, _ := publico(texto2); code != http.StatusGone {
		t.Fatalf("enlace revocado: %d", code)
	}
	if code, _ := publico(texto[i:]); code != http.StatusOK {
		t.Fatalf("el otro enlace sigue: %d", code)
	}
	if b.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'manual.enlace_revocado' AND payload->>'enlace_id' = $1`, ultimo) != 1 {
		t.Fatal("manual.enlace_revocado")
	}
	ajeno := b.persona([]int64{2}, rolBase("supervisor"))
	b.exigir(b.api(ajeno, http.MethodPost, "/manual-enlaces/"+ultimo+"/revocar", nil), http.StatusNotFound)
	// Al desactivarlo, el enlace deja de valer.
	b.exigir(b.api(b.supervisor, http.MethodPost, fmt.Sprintf("/manuales/%d/desactivar", int64(onu["id"].(float64))), nil), http.StatusOK)
	if code, _ := publico(texto[i:]); code != http.StatusGone {
		t.Fatalf("enlace de un manual desactivado: %d", code)
	}
	if code, _ := publico("/api/v1/mensajeria/publico/manuales/" + strings.Repeat("x", 43)); code != http.StatusGone {
		t.Fatalf("token desconocido: %d", code)
	}
}
