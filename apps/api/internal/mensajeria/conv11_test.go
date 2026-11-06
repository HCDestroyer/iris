package mensajeria_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// Bandeja 1.1 (contratos/conversaciones.md §14): supervisores por cola, sin cola, iniciar, pasar,
// disponibilidad y el bot. El reloj es el jueves 5 de noviembre de 2026, 10:00 en Guatemala.

func (b *bandeja) miembro(p persona) {
	b.t.Helper()
	if _, err := b.bd.Exec(b.ctx, `INSERT INTO mensajeria.cola_miembros (cola_id, usuario_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, b.cola, p.id); err != nil {
		b.t.Fatal(err)
	}
}

func (b *bandeja) disponible(p persona, estado string) map[string]any {
	b.t.Helper()
	g := b.api(p, http.MethodGet, "/conversaciones/disponibilidad", nil)
	b.exigir(g, http.StatusOK)
	r := b.api(p, http.MethodPost, "/conversaciones/disponibilidad", map[string]any{"estado": estado, "version": g.cuerpo["version"]})
	b.exigir(r, http.StatusOK, "estado", estado)
	return r.cuerpo
}

func (b *bandeja) colaActual(id int64) map[string]any {
	b.t.Helper()
	r := b.api(b.supervisor, http.MethodGet, "/colas", nil)
	b.exigir(r, http.StatusOK)
	for _, x := range r.cuerpo["data"].([]any) {
		if c := x.(map[string]any); int64(c["id"].(float64)) == id {
			return c
		}
	}
	b.t.Fatalf("cola %d", id)
	return nil
}

func (b *bandeja) outbox(evento, conv string) []map[string]any {
	b.t.Helper()
	filas, err := b.bd.Query(b.ctx, `SELECT payload FROM integracion.outbox WHERE evento = $1 AND payload->>'conversacion_id' = $2 ORDER BY id`, evento, conv)
	if err != nil {
		b.t.Fatal(err)
	}
	defer filas.Close()
	out := []map[string]any{}
	for filas.Next() {
		var p map[string]any
		if err := filas.Scan(&p); err != nil {
			b.t.Fatal(err)
		}
		out = append(out, p)
	}
	return out
}

func eventosSistema(ms []map[string]any, tipo string) []map[string]any {
	out := []map[string]any{}
	for _, m := range ms {
		if e, ok := m["evento"].(map[string]any); ok && e["tipo"] == tipo {
			out = append(out, e)
		}
	}
	return out
}

func automaticos(ms []map[string]any, cual string) []map[string]any {
	out := []map[string]any{}
	for _, m := range ms {
		if m["direccion"] == "saliente" && m["automatico"] == cual {
			out = append(out, m)
		}
	}
	return out
}

func (b *bandeja) lista(p persona) []any {
	b.t.Helper()
	r := b.api(p, http.MethodGet, "/conversaciones?limit=200", nil)
	b.exigir(r, http.StatusOK)
	return r.cuerpo["data"].([]any)
}

func tieneConv(xs []any, id string) bool {
	for _, x := range xs {
		if cid(x.(map[string]any)) == id {
			return true
		}
	}
	return false
}

func TestConvSinColaVisibleSoloSinAsignar(t *testing.T) {
	b := nuevaBandeja(t, nil)
	num := numeroCliente()
	b.texto(num, "wamid.s1", "Hola")
	c := b.conv(num)
	if _, err := b.bd.Exec(b.ctx, `UPDATE mensajeria.conversaciones SET cola_id = NULL WHERE id = $1`, cid(c)); err != nil {
		t.Fatal(err)
	}
	ruta := "/conversaciones/" + cid(c)
	// Sin cola y sin asignar: todo soporte la ve.
	b.exigir(b.api(b.otro, http.MethodGet, ruta, nil), http.StatusOK)
	if !tieneConv(b.lista(b.otro), cid(c)) {
		t.Fatal("sin asignar: no está en la lista de otro")
	}
	// La toma el agente: deja de verla quien no es supervisor ni mencionado.
	c = b.conv(num)
	b.exigir(b.api(b.agente, http.MethodPost, ruta+"/tomar", map[string]any{"version": ver(c)}), http.StatusOK)
	b.exigir(b.api(b.otro, http.MethodGet, ruta, nil), http.StatusNotFound)
	if tieneConv(b.lista(b.otro), cid(c)) {
		t.Fatal("asignada: sigue en la lista de otro")
	}
	r := b.api(b.otro, http.MethodGet, "/conversaciones/contadores", nil)
	b.exigir(r, http.StatusOK)
	b.exigir(b.api(b.agente, http.MethodGet, ruta, nil), http.StatusOK)
	b.exigir(b.api(b.supervisor, http.MethodGet, ruta, nil), http.StatusOK)
	// Mencionado: la vuelve a ver.
	b.exigir(b.api(b.agente, http.MethodPost, ruta+"/notas", map[string]any{"id": nuevoID(), "texto": "ayuda", "menciones": []int64{b.otro.id}}), http.StatusCreated)
	b.exigir(b.api(b.otro, http.MethodGet, ruta, nil), http.StatusOK)
	// Otra sin cola asignada a otro y sin mención: 404 también en las acciones.
	num2 := numeroCliente()
	b.texto(num2, "wamid.s2", "Hola")
	c2 := b.conv(num2)
	if _, err := b.bd.Exec(b.ctx, `UPDATE mensajeria.conversaciones SET cola_id = NULL, asignado_a = $2 WHERE id = $1`, cid(c2), b.agente.id); err != nil {
		t.Fatal(err)
	}
	b.exigir(b.api(b.otro, http.MethodPost, "/conversaciones/"+cid(c2)+"/mensajes", map[string]any{"id": nuevoID(), "texto": "x"}), http.StatusNotFound)
}

func TestConvSupervisorDeCola(t *testing.T) {
	b := nuevaBandeja(t, nil)
	num := numeroCliente()
	b.texto(num, "wamid.q1", "Hola")
	c := b.conv(num)
	ruta := "/conversaciones/" + cid(c)
	// otro (soporte, sin conversaciones.asignar ni supervisar) no es de la cola: no la ve ni la asigna.
	b.exigir(b.api(b.otro, http.MethodGet, ruta, nil), http.StatusNotFound)
	cola := b.colaActual(b.cola)
	b.exigir(b.api(b.supervisor, http.MethodPost, fmt.Sprintf("/colas/%d/supervisores", b.cola),
		map[string]any{"version": cola["version"], "usuarioIds": []int64{b.otro.id}, "asistenteIa": true}), http.StatusUnprocessableEntity,
		"type", "urn:iris:problema:no_disponible_aun")
	b.exigir(b.api(b.supervisor, http.MethodPost, fmt.Sprintf("/colas/%d/supervisores", b.cola),
		map[string]any{"version": cola["version"], "usuarioIds": []int64{b.tecnico.id}}), http.StatusUnprocessableEntity,
		"type", "urn:iris:problema:usuario_no_disponible")
	r := b.api(b.supervisor, http.MethodPost, fmt.Sprintf("/colas/%d/supervisores", b.cola), map[string]any{"version": cola["version"], "usuarioIds": []int64{b.otro.id}})
	b.exigir(r, http.StatusOK)
	if s := r.cuerpo["supervisores"].([]any); len(s) != 1 || int64(s[0].(map[string]any)["id"].(float64)) != b.otro.id {
		t.Fatalf("supervisores: %v", r.cuerpo)
	}
	if n := b.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'cola.actualizada' AND payload->>'cola_id' = $1 AND payload->'campos' ? 'supervisores'`,
		fmt.Sprint(b.cola)); n != 1 {
		t.Fatalf("cola.actualizada: %d", n)
	}
	// Ve todo lo de la cola, asigna sin conversaciones.asignar y pasa.
	b.exigir(b.api(b.otro, http.MethodGet, ruta, nil), http.StatusOK)
	if !tieneConv(b.lista(b.otro), cid(c)) {
		t.Fatal("el supervisor de la cola no la ve en la lista")
	}
	c = b.conv(num)
	b.exigir(b.api(b.otro, http.MethodPost, ruta+"/asignar", map[string]any{"version": ver(c), "usuarioId": b.agente.id}), http.StatusOK)
	compa := b.persona([]int64{1}, rolBase("soporte"))
	b.miembro(compa)
	c = b.conv(num)
	p := b.api(b.otro, http.MethodPost, ruta+"/pasar", map[string]any{"version": ver(c), "usuarioId": compa.id})
	b.exigir(p, http.StatusOK)
	// Otro soporte sin ser supervisor de la cola no asigna (403).
	ajeno := b.persona([]int64{1}, rolBase("soporte"))
	b.miembro(ajeno)
	c = b.conv(num)
	b.exigir(b.api(ajeno, http.MethodPost, ruta+"/asignar", map[string]any{"version": ver(c), "usuarioId": b.agente.id}), http.StatusForbidden)
	// El supervisor de la cola puede recibirla y está en los agentes de la cola.
	a := b.api(b.agente, http.MethodGet, fmt.Sprintf("/conversaciones/agentes?colaId=%d", b.cola), nil)
	b.exigir(a, http.StatusOK)
	encontrado := false
	for _, x := range a.cuerpo["data"].([]any) {
		g := x.(map[string]any)
		if int64(g["id"].(float64)) == b.otro.id {
			encontrado = true
			if len(g["colasSupervisadas"].([]any)) != 1 || g["disponibilidad"].(map[string]any)["estado"] != "no_disponible" {
				t.Fatalf("agente supervisor: %v", g)
			}
		}
		if int64(g["id"].(float64)) == compa.id && g["abiertas"].(float64) != 1 {
			t.Fatalf("abiertas de compa: %v", g)
		}
	}
	if !encontrado {
		t.Fatalf("el supervisor de la cola no está en los agentes: %v", a.cuerpo)
	}
	// Detalle: supervisores y equipo.
	d := b.conv(num)
	if len(d["supervisores"].([]any)) != 1 || len(d["equipo"].([]any)) < 3 {
		t.Fatalf("detalle: %v %v", d["supervisores"], d["equipo"])
	}
	// Quitarle la supervisión: deja de verla.
	cola = b.colaActual(b.cola)
	b.exigir(b.api(b.supervisor, http.MethodPost, fmt.Sprintf("/colas/%d/supervisores", b.cola), map[string]any{"version": cola["version"], "usuarioIds": []int64{}}),
		http.StatusOK)
	b.exigir(b.api(b.otro, http.MethodGet, ruta, nil), http.StatusNotFound)
}

func TestConvPasarANoDisponible(t *testing.T) {
	b := nuevaBandeja(t, nil)
	compa := b.persona([]int64{1}, rolBase("soporte"))
	b.miembro(compa)
	num := numeroCliente()
	b.texto(num, "wamid.p1", "Hola")
	c := b.conv(num)
	ruta := "/conversaciones/" + cid(c)
	b.exigir(b.api(b.agente, http.MethodPost, ruta+"/tomar", map[string]any{"version": ver(c)}), http.StatusOK)
	c = b.conv(num)
	// compa no la tiene: no la puede pasar.
	b.exigir(b.api(compa, http.MethodPost, ruta+"/pasar", map[string]any{"version": ver(c), "usuarioId": compa.id}), http.StatusForbidden,
		"type", "urn:iris:problema:no_es_tuya")
	b.exigir(b.api(b.agente, http.MethodPost, ruta+"/pasar", map[string]any{"version": ver(c), "usuarioId": b.agente.id}), http.StatusUnprocessableEntity,
		"type", "urn:iris:problema:mismo_agente")
	b.exigir(b.api(b.agente, http.MethodPost, ruta+"/pasar", map[string]any{"version": ver(c), "usuarioId": b.otro.id}), http.StatusUnprocessableEntity,
		"type", "urn:iris:problema:usuario_no_disponible")
	b.exigir(b.api(b.agente, http.MethodPost, ruta+"/pasar", map[string]any{"version": ver(c) + 5, "usuarioId": compa.id}), http.StatusConflict)
	// compa no está disponible: se pasa igual y la respuesta lo dice.
	r := b.api(b.agente, http.MethodPost, ruta+"/pasar", map[string]any{"version": ver(c), "usuarioId": compa.id, "nota": "Ya reinició el módem; falta agendar visita."})
	b.exigir(r, http.StatusOK, "destinoDisponible", "false")
	if r.cuerpo["conversacion"].(map[string]any)["asignado"].(map[string]any)["id"].(float64) != float64(compa.id) {
		t.Fatalf("asignado: %v", r.cuerpo["conversacion"])
	}
	ev := b.outbox("conversacion.asignada", cid(c))
	ultimo := ev[len(ev)-1]
	if ultimo["origen"] != "pasar" || ultimo["disponible"] != false || ultimo["nota_id"] == nil || ultimo["anterior_id"].(float64) != float64(b.agente.id) {
		t.Fatalf("conversacion.asignada: %v", ultimo)
	}
	ms := b.mensajes(b.supervisor, cid(c))
	if p := eventosSistema(ms, "pasada"); len(p) != 1 || p[0]["detalle"] != "no_disponible" {
		t.Fatalf("evento pasada: %v", p)
	}
	// La nota no queda en la auditoría.
	if n := b.contar(`SELECT count(*) FROM auditoria.acciones WHERE entrada::text LIKE '%reinició el módem%'`); n != 0 {
		t.Fatal("la nota quedó en la auditoría")
	}
	// Disponible: la respuesta lo dice.
	b.disponible(b.agente, "disponible")
	c = b.conv(num)
	r = b.api(compa, http.MethodPost, ruta+"/pasar", map[string]any{"version": ver(c), "usuarioId": b.agente.id})
	b.exigir(r, http.StatusOK, "destinoDisponible", "true")
	// El supervisor global pasa cualquiera.
	c = b.conv(num)
	b.exigir(b.api(b.supervisor, http.MethodPost, ruta+"/pasar", map[string]any{"version": ver(c), "usuarioId": compa.id}), http.StatusOK)
}

func TestConvIniciarConYSinPermiso(t *testing.T) {
	b := nuevaBandeja(t, nil)
	cliente := b.cliente(1, "5512-0101")
	numero := "+50255120101"
	pl := map[string]any{"codigo": "bienvenida", "variables": map[string]string{"plan": "Fibra 100"}}
	cuerpo := func(id string) map[string]any {
		return map[string]any{"id": id, "canalId": b.canal.ID, "clienteId": cliente, "plantilla": pl}
	}
	// Sin permiso de contacto registrado: 422 sin_permiso.
	b.exigir(b.api(b.agente, http.MethodPost, "/conversaciones/iniciar", cuerpo(nuevoID())), http.StatusUnprocessableEntity,
		"type", "urn:iris:problema:sin_permiso")
	// Quien no es de la cola del número no inicia desde él; lectura no tiene el permiso.
	b.exigir(b.api(b.otro, http.MethodPost, "/conversaciones/iniciar", cuerpo(nuevoID())), http.StatusUnprocessableEntity,
		"type", "urn:iris:problema:numero_no_permitido")
	b.exigir(b.api(b.lectura, http.MethodPost, "/conversaciones/iniciar", cuerpo(nuevoID())), http.StatusForbidden)
	b.exigir(b.consentir(cliente, "acepta", b.reloj.Add(-time.Hour)), http.StatusCreated)
	// Plantillas del número (de mis colas): 200; de otro: 404.
	pls := b.api(b.agente, http.MethodGet, fmt.Sprintf("/conversaciones/canales/%d/plantillas", b.canal.ID), nil)
	b.exigir(pls, http.StatusOK)
	if d := pls.cuerpo["data"].([]any); len(d) != 1 || d[0].(map[string]any)["categoria"] != "servicio" {
		t.Fatalf("plantillas del número: %v", pls.cuerpo)
	}
	b.exigir(b.api(b.otro, http.MethodGet, fmt.Sprintf("/conversaciones/canales/%d/plantillas", b.canal.ID), nil), http.StatusNotFound)
	// Con permiso: 201, conversación nueva asignada a quien envía en la cola del número.
	id := nuevoID()
	r := b.api(b.agente, http.MethodPost, "/conversaciones/iniciar", cuerpo(id))
	b.exigir(r, http.StatusCreated, "nueva", "true")
	conv := r.cuerpo["conversacion"].(map[string]any)
	if conv["estado"] != "esperando_cliente" || conv["asignado"].(map[string]any)["id"].(float64) != float64(b.agente.id) ||
		conv["cola"].(map[string]any)["id"].(float64) != float64(b.cola) || conv["numero"] != numero || conv["contacto"].(map[string]any)["verificado"] != true {
		t.Fatalf("conversación iniciada: %v", conv)
	}
	if m := r.cuerpo["mensaje"].(map[string]any); m["tipo"] != "plantilla" || m["estadoEntrega"] != "en_cola" {
		t.Fatalf("mensaje: %v", m)
	}
	// Repetir el mismo id: 200 con el mismo cuerpo; otro contenido: 409.
	r2 := b.api(b.agente, http.MethodPost, "/conversaciones/iniciar", cuerpo(id))
	b.exigir(r2, http.StatusOK)
	if r2.h.Get("Idempotent-Replayed") != "true" || r2.cuerpo["mensaje"].(map[string]any)["id"] != id {
		t.Fatalf("reenvío: %v %v", r2.h, r2.cuerpo)
	}
	otra := cuerpo(id)
	otra["plantilla"] = map[string]any{"codigo": "bienvenida", "variables": map[string]string{"plan": "Otro"}}
	b.exigir(b.api(b.agente, http.MethodPost, "/conversaciones/iniciar", otra), http.StatusConflict, "type", "urn:iris:problema:mensaje_id_reutilizado")
	convID := cid(conv)
	for _, ev := range []string{"conversacion.creada", "conversacion.iniciada", "conversacion.asignada", "conversacion.mensaje_enviado"} {
		if n := len(b.outbox(ev, convID)); n != 1 {
			t.Fatalf("%s: %d", ev, n)
		}
	}
	if o := b.outbox("conversacion.creada", convID)[0]; o["origen"] != "agente" {
		t.Fatalf("conversacion.creada: %v", o)
	}
	// Iniciar otra vez al mismo número reutiliza la conversación (no la duplica).
	r3 := b.api(b.agente, http.MethodPost, "/conversaciones/iniciar", cuerpo(nuevoID()))
	b.exigir(r3, http.StatusCreated, "nueva", "false")
	if cid(r3.cuerpo["conversacion"].(map[string]any)) != convID {
		t.Fatal("no reutilizó la conversación del número")
	}
	if n := b.contar(`SELECT count(*) FROM mensajeria.conversaciones WHERE canal_id = $1 AND numero = $2`, b.canal.ID, numero); n != 1 {
		t.Fatalf("conversaciones del número: %d", n)
	}
	// Otro agente de la cola: la tiene el agente → 409 conversacion_de_otro.
	compa := b.persona([]int64{1}, rolBase("soporte"))
	b.miembro(compa)
	b.exigir(b.api(compa, http.MethodPost, "/conversaciones/iniciar", cuerpo(nuevoID())), http.StatusConflict, "type", "urn:iris:problema:conversacion_de_otro")
	// Baja por número: nunca sale (aunque el cliente acepte).
	if _, err := b.bd.Exec(b.ctx, `INSERT INTO mensajeria.consentimientos_numero (empresa_id, telefono, estado, origen, ocurrido_en) VALUES (1, $1, 'baja', 'cliente', $2)`,
		numero, b.reloj.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	b.exigir(b.api(b.agente, http.MethodPost, "/conversaciones/iniciar", cuerpo(nuevoID())), http.StatusUnprocessableEntity, "type", "urn:iris:problema:baja")
	// Cliente sin teléfono: sin_contacto. Contacto de Iris sin permiso: sin_permiso; con acepta: 201.
	sinTel := b.cliente(1, "")
	b.exigir(b.consentir(sinTel, "acepta", b.reloj.Add(-time.Hour)), http.StatusCreated)
	b.exigir(b.api(b.agente, http.MethodPost, "/conversaciones/iniciar", map[string]any{"id": nuevoID(), "canalId": b.canal.ID, "clienteId": sinTel, "plantilla": pl}),
		http.StatusUnprocessableEntity, "type", "urn:iris:problema:sin_contacto")
	var contacto int64
	telIris := "+5025" + fmt.Sprintf("%07d", siguienteCliente.Add(1)%10_000_000)
	if err := b.bd.QueryRow(b.ctx, `INSERT INTO mensajeria.contactos_iris (empresa_id, nombre, tipo, telefono) VALUES (1, 'Ferretería Iris', 'proveedor', $1) RETURNING id`,
		telIris).Scan(&contacto); err != nil {
		t.Fatal(err)
	}
	ci := map[string]any{"id": nuevoID(), "canalId": b.canal.ID, "contactoIrisId": contacto, "plantilla": pl}
	b.exigir(b.api(b.agente, http.MethodPost, "/conversaciones/iniciar", ci), http.StatusUnprocessableEntity, "type", "urn:iris:problema:sin_permiso")
	b.exigir(b.api(b.supervisor, http.MethodPost, fmt.Sprintf("/contactos-iris/%d/consentimiento", contacto), map[string]any{"estado": "acepta"}), http.StatusOK)
	ci["id"] = nuevoID()
	b.exigir(b.api(b.agente, http.MethodPost, "/conversaciones/iniciar", ci), http.StatusCreated, "nueva", "true")
	// Las variables no van a la auditoría.
	if n := b.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'conversaciones.iniciar' AND entrada::text LIKE '%Fibra 100%'`); n != 0 {
		t.Fatal("las variables quedaron en la auditoría")
	}
}

func TestConvDestinatarios(t *testing.T) {
	b := nuevaBandeja(t, nil)
	nombre := "Zoila " + pruebas.Unico()
	acepta, baja, sinTel := siguienteCliente.Add(1), siguienteCliente.Add(1), siguienteCliente.Add(1)
	for _, x := range []struct {
		id    int64
		movil string
	}{{acepta, "5512-7001"}, {baja, "5512-7002"}, {sinTel, ""}} {
		if _, err := b.bd.Exec(b.ctx, `INSERT INTO integracion.argos_clientes (id, empresa_id, nombre, movil, codigo, sincronizado_en, empresas)
			VALUES ($1, 1, $2, $3, $4, now(), '{1}')`, x.id, nombre+fmt.Sprint(x.id), x.movil, fmt.Sprintf("C-%d", x.id)); err != nil {
			t.Fatal(err)
		}
		b.contactos.mu.Lock()
		b.contactos.c[x.id] = argos.ContactoArgos{Existe: true, Empresas: []int64{1}, Movil: x.movil}
		b.contactos.mu.Unlock()
	}
	b.exigir(b.consentir(acepta, "acepta", b.reloj.Add(-time.Hour)), http.StatusCreated)
	b.exigir(b.consentir(baja, "baja", b.reloj.Add(-time.Hour)), http.StatusCreated)
	if _, err := b.bd.Exec(b.ctx, `INSERT INTO mensajeria.contactos_iris (empresa_id, nombre, tipo, telefono) VALUES (1, $1, 'otro', '+50240218830')`, nombre+" Iris"); err != nil {
		t.Fatal(err)
	}
	r := b.api(b.agente, http.MethodGet, fmt.Sprintf("/conversaciones/destinatarios?q=%s&canalId=%d", strings.ReplaceAll(nombre, " ", "%20"), b.canal.ID), nil)
	b.exigir(r, http.StatusOK)
	d := r.cuerpo["data"].([]any)
	if len(d) != 4 {
		t.Fatalf("destinatarios: %v", d)
	}
	motivos := map[string]any{}
	for _, x := range d {
		g := x.(map[string]any)
		motivos[fmt.Sprint(g["tipo"], int64(g["id"].(float64)))] = g["motivo"]
		if g["tipo"] == "cliente" && int64(g["id"].(float64)) == acepta && (g["puede"] != true || g["numero"] != "+50255127001") {
			t.Fatalf("acepta: %v", g)
		}
	}
	if motivos[fmt.Sprint("cliente", baja)] != "baja" || motivos[fmt.Sprint("cliente", sinTel)] != "sin_contacto" {
		t.Fatalf("motivos: %v", motivos)
	}
	// El singular es coherente; sin el permiso de iniciar, 403.
	u := b.api(b.agente, http.MethodGet, fmt.Sprintf("/conversaciones/destinatario?clienteId=%d", baja), nil)
	b.exigir(u, http.StatusOK, "motivo", "baja", "puede", "false")
	b.exigir(b.api(b.lectura, http.MethodGet, "/conversaciones/destinatarios?q=Zoila", nil), http.StatusForbidden)
	// Vacío: [] (nunca null).
	v := b.api(b.agente, http.MethodGet, "/conversaciones/destinatarios?q=zzzz-nadie-"+pruebas.Unico(), nil)
	b.exigir(v, http.StatusOK)
	if d, ok := v.cuerpo["data"].([]any); !ok || len(d) != 0 {
		t.Fatalf("vacío: %v", v.cuerpo)
	}
}

func TestConvDisponibilidadLatidoYVencimiento(t *testing.T) {
	b := nuevaBandeja(t, nil)
	g := b.api(b.agente, http.MethodGet, "/conversaciones/disponibilidad", nil)
	b.exigir(g, http.StatusOK, "estado", "no_disponible", "version", "0", "minutosInactividad", "15")
	b.exigir(b.api(b.agente, http.MethodPost, "/conversaciones/disponibilidad", map[string]any{"estado": "disponible", "version": 3}), http.StatusConflict)
	d := b.disponible(b.agente, "disponible")
	if d["origen"] != "manual" {
		t.Fatalf("manual: %v", d)
	}
	if n := b.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'conversaciones.disponibilidad' AND resultado = 'ok' AND actor_id = $1`, b.agente.id); n != 1 {
		t.Fatalf("el cambio manual se audita: %d", n)
	}
	evento := func(origen string) int {
		return b.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'agente.disponibilidad_cambiada' AND payload->>'usuario_id' = $1 AND payload->>'origen' = $2`,
			fmt.Sprint(b.agente.id), origen)
	}
	if evento("manual") != 1 {
		t.Fatal("agente.disponibilidad_cambiada manual")
	}
	// Los latidos no auditan ni emiten.
	antes := b.contar(`SELECT count(*) FROM auditoria.acciones WHERE actor_id = $1`, b.agente.id)
	for range 3 {
		*b.reloj = b.reloj.Add(10 * time.Minute)
		b.exigir(b.api(b.agente, http.MethodPost, "/conversaciones/disponibilidad/latido", nil), http.StatusOK, "estado", "disponible")
		b.m.VencerDisponibilidad(b.ctx, b.bd)
	}
	if n := b.contar(`SELECT count(*) FROM auditoria.acciones WHERE actor_id = $1`, b.agente.id); n != antes {
		t.Fatalf("los latidos dejaron %d filas de auditoría", n-antes)
	}
	if n := b.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'agente.disponibilidad_cambiada' AND payload->>'usuario_id' = $1`, fmt.Sprint(b.agente.id)); n != 1 {
		t.Fatalf("eventos del agente: %d", n)
	}
	// Una acción de la bandeja también cuenta como actividad.
	num := numeroCliente()
	b.texto(num, "wamid.v1", "Hola")
	*b.reloj = b.reloj.Add(14 * time.Minute)
	c := b.conv(num)
	b.exigir(b.api(b.agente, http.MethodPost, "/conversaciones/"+cid(c)+"/tomar", map[string]any{"version": ver(c)}), http.StatusOK)
	*b.reloj = b.reloj.Add(14 * time.Minute)
	b.m.VencerDisponibilidad(b.ctx, b.bd)
	b.exigir(b.api(b.agente, http.MethodGet, "/conversaciones/disponibilidad", nil), http.StatusOK, "estado", "disponible")
	// 16 min sin actividad: inactividad.
	*b.reloj = b.reloj.Add(16 * time.Minute)
	b.m.VencerDisponibilidad(b.ctx, b.bd)
	b.exigir(b.api(b.agente, http.MethodGet, "/conversaciones/disponibilidad", nil), http.StatusOK, "estado", "no_disponible", "origen", "inactividad")
	if evento("inactividad") != 1 {
		t.Fatal("agente.disponibilidad_cambiada inactividad")
	}
	// Cada transición automática deja una fila (actor sistema); los latidos, ninguna.
	if n := b.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'mensajeria.vencer_disponibilidad' AND actor_tipo = 'sistema'
		AND entidad_id = $1`, fmt.Sprint(dispID(b, b.agente))); n != 1 {
		t.Fatalf("fila de auditoría del vencimiento: %d", n)
	}
	// Ajuste de la empresa: 5 minutos (solo canales.configurar).
	aj := b.api(b.supervisor, http.MethodGet, "/conversaciones/ajustes", nil)
	b.exigir(aj, http.StatusOK)
	b.exigir(b.api(b.agente, http.MethodPost, "/conversaciones/ajustes", map[string]any{"version": aj.cuerpo["version"], "minutosInactividad": 5}), http.StatusForbidden)
	b.exigir(b.api(b.supervisor, http.MethodPost, "/conversaciones/ajustes", map[string]any{"version": aj.cuerpo["version"], "minutosInactividad": 5}), http.StatusOK,
		"minutosInactividad", "5")
	t.Cleanup(func() {
		_, _ = b.bd.Exec(b.ctx, `UPDATE mensajeria.ajustes_bandeja SET minutos_inactividad = 15 WHERE empresa_id = 1`)
	})
	b.disponible(b.agente, "disponible")
	*b.reloj = b.reloj.Add(6 * time.Minute)
	b.m.VencerDisponibilidad(b.ctx, b.bd)
	b.exigir(b.api(b.agente, http.MethodGet, "/conversaciones/disponibilidad", nil), http.StatusOK, "origen", "inactividad")
	// Horario de su cola: 12:00–13:00 Guatemala. A las 12:30 se pone disponible; a las 13:01 pasa a no_disponible (horario).
	if _, err := b.bd.Exec(b.ctx, `UPDATE mensajeria.ajustes_bandeja SET minutos_inactividad = 120 WHERE empresa_id = 1`); err != nil {
		t.Fatal(err)
	}
	h := b.horario(todosLosDias("12:00", "13:00"), nil)
	cola := b.colaActual(b.cola)
	b.exigir(b.api(b.supervisor, http.MethodPost, fmt.Sprintf("/colas/%d/actualizar", b.cola), map[string]any{"version": cola["version"], "nombre": cola["nombre"],
		"slaRespuestaMin": 60, "horarioId": h["id"]}), http.StatusOK)
	*b.reloj = time.Date(2026, 11, 5, 18, 30, 0, 0, time.UTC)
	d = b.disponible(b.agente, "disponible")
	if d["horarioHasta"] != "2026-11-05T19:00:00Z" {
		t.Fatalf("horarioHasta: %v", d)
	}
	*b.reloj = time.Date(2026, 11, 5, 19, 1, 0, 0, time.UTC)
	b.m.VencerDisponibilidad(b.ctx, b.bd)
	b.exigir(b.api(b.agente, http.MethodGet, "/conversaciones/disponibilidad", nil), http.StatusOK, "estado", "no_disponible", "origen", "horario")
	// Cierre de sesión: la sesión con que se puso disponible se cerró.
	b.disponible(b.agente, "disponible")
	if _, err := b.bd.Exec(b.ctx, `UPDATE nucleo.sesiones SET revocada_en = now() WHERE usuario_id = $1`, b.agente.id); err != nil {
		t.Fatal(err)
	}
	b.m.VencerDisponibilidad(b.ctx, b.bd)
	if evento("cierre_sesion") != 1 {
		t.Fatal("agente.disponibilidad_cambiada cierre_sesion")
	}
}

// botActivo configura el bot del número de la prueba.
func (b *bandeja) botActivo(bot map[string]any) {
	b.t.Helper()
	l := b.api(b.supervisor, http.MethodGet, "/conversaciones/canales", nil)
	b.exigir(l, http.StatusOK)
	var version any
	for _, x := range l.cuerpo["data"].([]any) {
		if c := x.(map[string]any); int64(c["id"].(float64)) == b.canal.ID {
			version = c["version"]
		}
	}
	b.exigir(b.api(b.supervisor, http.MethodPost, fmt.Sprintf("/canales/%d/configurar", b.canal.ID),
		map[string]any{"version": version, "usos": []string{"soporte"}, "avisosEmpresa": false, "colaId": b.cola, "bot": bot}), http.StatusOK)
}

func (b *bandeja) otraCola() int64 {
	b.t.Helper()
	r := b.api(b.supervisor, http.MethodPost, "/colas", map[string]any{"nombre": "Ventas " + pruebas.Unico()})
	b.exigir(r, http.StatusCreated)
	return int64(r.cuerpo["id"].(float64))
}

func (b *bandeja) disparar() {
	b.t.Helper()
	b.m.DispararBots(b.ctx, b.bd)
}

func TestBotConfiguracion(t *testing.T) {
	b := nuevaBandeja(t, nil)
	b.exigir(b.configurarNumero(nil, nil), http.StatusOK)
	l := b.api(b.supervisor, http.MethodGet, "/conversaciones/canales", nil)
	for _, x := range l.cuerpo["data"].([]any) {
		if c := x.(map[string]any); int64(c["id"].(float64)) == b.canal.ID {
			bot := c["bot"].(map[string]any)
			if bot["activo"] != false || bot["minutosSinConfirmacion"].(float64) != 30 || bot["minutosSinDisponibles"].(float64) != 3 || bot["menu"] == nil {
				t.Fatalf("bot por defecto: %v", bot)
			}
			if c["iniciar"] != true {
				t.Fatalf("el supervisor puede iniciar desde el número: %v", c)
			}
		}
	}
	version := func() any {
		l := b.api(b.supervisor, http.MethodGet, "/conversaciones/canales", nil)
		for _, x := range l.cuerpo["data"].([]any) {
			if c := x.(map[string]any); int64(c["id"].(float64)) == b.canal.ID {
				return c["version"]
			}
		}
		return nil
	}
	conf := func(bot map[string]any) respuesta {
		return b.api(b.supervisor, http.MethodPost, fmt.Sprintf("/canales/%d/configurar", b.canal.ID),
			map[string]any{"version": version(), "usos": []string{"soporte"}, "avisosEmpresa": false, "colaId": b.cola, "bot": bot})
	}
	base := map[string]any{"activo": true, "atiende": "agente_autonomo", "minutosSinConfirmacion": 30, "minutosSinDisponibles": 3, "saludo": "Hola", "menu": []any{}}
	b.exigir(conf(base), http.StatusUnprocessableEntity, "type", "urn:iris:problema:no_disponible_aun")
	base["atiende"] = "bot"
	base["menu"] = []any{map[string]any{"texto": "Ventas", "colaId": 999999999, "etiquetaIds": []any{}}}
	b.exigir(conf(base), http.StatusUnprocessableEntity)
	base["menu"] = []any{}
	b.exigir(conf(base), http.StatusOK)
}

func TestBotSinConfirmacionYMenu(t *testing.T) {
	b := nuevaBandeja(t, nil)
	ventas := b.otraCola()
	et := b.api(b.supervisor, http.MethodPost, "/etiquetas", map[string]any{"nombre": "Venta " + pruebas.Unico(), "color": "info"})
	b.exigir(et, http.StatusCreated)
	b.botActivo(map[string]any{"activo": true, "atiende": "bot", "minutosSinConfirmacion": 30, "minutosSinDisponibles": 3,
		"saludo": "Gracias por escribir.", "menu": []any{
			map[string]any{"texto": "Soporte técnico", "colaId": b.cola, "etiquetaIds": []any{}},
			map[string]any{"texto": "Ventas", "colaId": ventas, "etiquetaIds": []any{et.cuerpo["id"]}}}})
	b.disponible(b.agente, "disponible")
	num := numeroCliente()
	b.texto(num, "wamid.b1", "Quiero cambiar mi plan")
	c := b.conv(num)
	// Se avisa a los disponibles de la cola y el bot contesta a las 10:30 si nadie la toma.
	e := c["espera"].(map[string]any)
	if e["totalDisponibles"].(float64) != 1 || e["botContestaEn"] != "2026-11-05T16:30:00Z" || e["botMotivo"] != "sin_confirmacion" {
		t.Fatalf("espera: %v", e)
	}
	ms := b.mensajes(b.supervisor, cid(c))
	if a := eventosSistema(ms, "aviso_sin_atender"); len(a) != 1 || a[0]["disponibles"].(float64) != 1 {
		t.Fatalf("aviso_sin_atender: %v", a)
	}
	if len(automaticos(ms, "bot")) != 0 {
		t.Fatal("el bot contestó antes de tiempo")
	}
	if o := b.outbox("conversacion.aviso_sin_atender", cid(c)); len(o) != 1 || fmt.Sprint(o[0]["usuarios"]) != fmt.Sprintf("[%d]", b.agente.id) {
		t.Fatalf("conversacion.aviso_sin_atender: %v", o)
	}
	// Llega el aviso del flujo solo al avisado.
	if n := b.contar(`SELECT count(*) FROM mensajeria.conv_eventos WHERE conversacion_id = $1 AND tipo = 'aviso_sin_atender' AND usuario_id = $2`, cid(c), b.agente.id); n != 1 {
		t.Fatalf("evento del flujo: %d", n)
	}
	// Otro mensaje del cliente en la misma espera no repite el aviso.
	b.texto(num, "wamid.b2", "¿Hola?")
	if a := eventosSistema(b.mensajes(b.supervisor, cid(c)), "aviso_sin_atender"); len(a) != 1 {
		t.Fatalf("aviso repetido: %d", len(a))
	}
	// A los 29 min no pasa nada; a los 31 contesta el bot con saludo y menú.
	*b.reloj = b.reloj.Add(29 * time.Minute)
	b.disparar()
	if len(automaticos(b.mensajes(b.supervisor, cid(c)), "bot")) != 0 {
		t.Fatal("el bot contestó a los 29 min")
	}
	*b.reloj = b.reloj.Add(2 * time.Minute)
	b.disparar()
	b.disparar() // idempotente
	ms = b.mensajes(b.supervisor, cid(c))
	bot := automaticos(ms, "bot")
	if len(bot) != 1 || !strings.Contains(bot[0]["texto"].(string), "Gracias por escribir.") || !strings.Contains(bot[0]["texto"].(string), "2. Ventas") {
		t.Fatalf("respuesta del bot: %v", bot)
	}
	if a := eventosSistema(ms, "bot_atendio"); len(a) != 1 || a[0]["detalle"] != "sin_confirmacion" || a[0]["minutos"].(float64) != 30 {
		t.Fatalf("bot_atendio: %v", a)
	}
	if o := b.outbox("conversacion.bot_atendio", cid(c)); len(o) != 1 || o[0]["motivo"] != "sin_confirmacion" || o[0]["menu"] != true {
		t.Fatalf("conversacion.bot_atendio: %v", o)
	}
	if b.jobsDe("mensajeria.enviar_mensaje") == nil {
		t.Fatal("el mensaje del bot no se encoló")
	}
	// El cliente elige 2: pasa a Ventas con su etiqueta.
	b.texto(num, "wamid.b3", " 2. ")
	c = b.conv(num)
	if c["cola"].(map[string]any)["id"].(float64) != float64(ventas) || len(c["etiquetas"].([]any)) != 1 {
		t.Fatalf("menú elegido: %v %v", c["cola"], c["etiquetas"])
	}
	if o := b.outbox("conversacion.menu_elegido", cid(c)); len(o) != 1 || o[0]["opcion"].(float64) != 2 || o[0]["a_cola_id"].(float64) != float64(ventas) {
		t.Fatalf("conversacion.menu_elegido: %v", o)
	}
	if m := eventosSistema(b.mensajes(b.supervisor, cid(c)), "menu_elegido"); len(m) != 1 || m[0]["detalle"] != "Ventas" {
		t.Fatalf("evento menu_elegido: %v", m)
	}
	// Ya atendió: otro "1" ya no cambia de cola.
	b.texto(num, "wamid.b4", "1")
	if b.conv(num)["cola"].(map[string]any)["id"].(float64) != float64(ventas) {
		t.Fatal("el menú siguió activo después de elegir")
	}
}

func TestBotCanceladoAlTomar(t *testing.T) {
	b := nuevaBandeja(t, nil)
	b.botActivo(map[string]any{"activo": true, "atiende": "bot", "minutosSinConfirmacion": 30, "minutosSinDisponibles": 3, "saludo": "Hola", "menu": []any{}})
	b.disponible(b.agente, "disponible")
	num := numeroCliente()
	b.texto(num, "wamid.t1", "Hola")
	c := b.conv(num)
	if c["espera"].(map[string]any)["botContestaEn"] == nil {
		t.Fatalf("sin temporizador: %v", c["espera"])
	}
	b.exigir(b.api(b.agente, http.MethodPost, "/conversaciones/"+cid(c)+"/tomar", map[string]any{"version": ver(c)}), http.StatusOK)
	c = b.conv(num)
	if _, ok := c["espera"]; ok {
		t.Fatalf("asignada: espera presente: %v", c["espera"])
	}
	*b.reloj = b.reloj.Add(40 * time.Minute)
	b.disparar()
	if bot := automaticos(b.mensajes(b.supervisor, cid(c)), "bot"); len(bot) != 0 {
		t.Fatalf("el bot contestó una conversación tomada: %v", bot)
	}
	if n := b.contar(`SELECT count(*) FROM mensajeria.conversaciones WHERE id = $1 AND (bot_vence_en IS NOT NULL OR bot_espera_en IS NOT NULL)`, cid(c)); n != 0 {
		t.Fatal("el temporizador sigue")
	}
}

func TestBotSinDisponibles(t *testing.T) {
	b := nuevaBandeja(t, nil)
	b.botActivo(map[string]any{"activo": true, "atiende": "bot", "minutosSinConfirmacion": 30, "minutosSinDisponibles": 3,
		"saludo": "Todos ocupados, ya te atendemos.", "menu": []any{map[string]any{"texto": "Soporte", "colaId": b.cola, "etiquetaIds": []any{}}}})
	// La cola manda: 5 minutos sin disponibles.
	cola := b.colaActual(b.cola)
	b.exigir(b.api(b.supervisor, http.MethodPost, fmt.Sprintf("/colas/%d/actualizar", b.cola), map[string]any{"version": cola["version"], "nombre": cola["nombre"],
		"slaRespuestaMin": 60, "botSinDisponiblesMin": 5}), http.StatusOK)
	num := numeroCliente()
	b.texto(num, "wamid.n1", "Hola")
	c := b.conv(num)
	ms := b.mensajes(b.supervisor, cid(c))
	if bot := automaticos(ms, "bot"); len(bot) != 1 || bot[0]["texto"] != "Todos ocupados, ya te atendemos." {
		t.Fatalf("saludo inmediato: %v", bot)
	}
	e := c["espera"].(map[string]any)
	if e["totalDisponibles"].(float64) != 0 || e["botMotivo"] != "sin_disponibles" || e["botContestaEn"] != "2026-11-05T16:05:00Z" {
		t.Fatalf("espera: %v", e)
	}
	// A los 3 min (lo del número) no: vale el de la cola.
	*b.reloj = b.reloj.Add(4 * time.Minute)
	b.disparar()
	if len(automaticos(b.mensajes(b.supervisor, cid(c)), "bot")) != 1 {
		t.Fatal("disparó con los minutos del número")
	}
	*b.reloj = b.reloj.Add(2 * time.Minute)
	b.disparar()
	ms = b.mensajes(b.supervisor, cid(c))
	if bot := automaticos(ms, "bot"); len(bot) != 2 || !strings.Contains(bot[1]["texto"].(string), "1. Soporte") {
		t.Fatalf("menú al tomar la conversación: %v", bot)
	}
	if a := eventosSistema(ms, "bot_atendio"); len(a) != 1 || a[0]["detalle"] != "sin_disponibles" {
		t.Fatalf("bot_atendio: %v", a)
	}
	// Respuesta inválida: repite el menú una vez; la segunda, ya no.
	b.texto(num, "wamid.n2", "no sé")
	b.texto(num, "wamid.n3", "tampoco")
	if bot := automaticos(b.mensajes(b.supervisor, cid(c)), "bot"); len(bot) != 3 || !strings.HasPrefix(bot[2]["texto"].(string), "No entendí") {
		t.Fatalf("menú repetido una sola vez: %d", len(bot))
	}
	// Nunca a quien escribe BAJA.
	baja := numeroCliente()
	b.texto(baja, "wamid.n4", "BAJA")
	cb := b.conv(baja)
	*b.reloj = b.reloj.Add(10 * time.Minute)
	b.disparar()
	for _, m := range b.mensajes(b.supervisor, cid(cb)) {
		if m["direccion"] == "saliente" {
			t.Fatalf("el bot contestó a una baja: %v", m)
		}
	}
}

func TestBotFueraDeHorarioConMenu(t *testing.T) {
	b := nuevaBandeja(t, nil)
	h := b.horario(todosLosDias("12:00", "13:00"), nil)
	b.exigir(b.configurarNumero(h["id"], map[string]any{"responder": true, "mensaje": "Atendemos de 12 a 13."}), http.StatusOK)
	b.botActivo(map[string]any{"activo": true, "atiende": "bot", "minutosSinConfirmacion": 30, "minutosSinDisponibles": 3, "saludo": "Hola",
		"menu": []any{map[string]any{"texto": "Soporte", "colaId": b.cola, "etiquetaIds": []any{}}}})
	// botActivo no manda horario: se vuelve a fijar.
	b.exigir(b.configurarNumero(h["id"], map[string]any{"responder": true, "mensaje": "Atendemos de 12 a 13."}), http.StatusOK)
	num := numeroCliente()
	b.texto(num, "wamid.f1", "Hola")
	c := b.conv(num)
	ms := b.mensajes(b.supervisor, cid(c))
	if f := automaticos(ms, "fuera_de_horario"); len(f) != 1 || f[0]["texto"] != "Atendemos de 12 a 13." {
		t.Fatalf("respuesta de fuera de horario: %v", f)
	}
	if bot := automaticos(ms, "bot"); len(bot) != 1 || !strings.Contains(bot[0]["texto"].(string), "1. Soporte") {
		t.Fatalf("menú fuera de horario: %v", bot)
	}
	if o := b.outbox("conversacion.bot_atendio", cid(c)); len(o) != 1 || o[0]["motivo"] != "fuera_de_horario" {
		t.Fatalf("bot_atendio fuera de horario: %v", o)
	}
	if len(eventosSistema(ms, "aviso_sin_atender")) != 0 {
		t.Fatal("aviso fuera de horario")
	}
}
