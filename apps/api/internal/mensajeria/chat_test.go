package mensajeria_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/HCDestroyer/iris/apps/api/internal/mensajeria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// Chat interno del equipo (contratos/chat.md): pruebas contra Postgres real y por HTTP, con tokens
// iris_pat_ de personas con sus roles base.

const rutaChat = "/api/v1/mensajeria/chat"

var identChat = identidad.Nuevo(identidad.Config{})

type persona struct {
	id    int64
	token string
	// empresa: X-Iris-Empresa (0 = sin cabecera).
	empresa int64
	correo  string
	// cookie: sesión del panel (en lugar del token) para abrirSSE.
	cookie string
	// csrf: el token CSRF de esa sesión (peticiones que cambian estado con cookie).
	csrf string
}

// persona crea un usuario con roles y empresas, y su token (con sus permisos).
func (e *entorno) persona(empresas []int64, roles ...identidad.AsignacionRol) persona {
	e.t.Helper()
	correo := pruebas.Correo("chat")
	u, err := identChat.CrearUsuario.Ejecutar(e.ctx, identidad.EntradaCrearUsuario{Nombre: "Persona " + pruebas.Unico(), Correo: correo,
		Contrasena: "contrasena-larga-1", Roles: roles, Empresas: empresas})
	if err != nil {
		e.t.Fatal(err)
	}
	tk, err := identChat.CrearToken.Ejecutar(pruebas.ContextoUsuario(e.bd, u.ID, nucleo.PermTokensCrear), identidad.EntradaCrearToken{Nombre: "chat"})
	if err != nil {
		e.t.Fatal(err)
	}
	return persona{id: u.ID, token: tk.Valor, correo: correo}
}

func rolBase(r string) identidad.AsignacionRol { return identidad.AsignacionRol{Rol: r} }

func (e *entorno) chat(p persona, metodo, ruta, clave string, cuerpo any) respuesta {
	e.t.Helper()
	var b io.Reader
	if cuerpo != nil {
		crudo, _ := json.Marshal(cuerpo)
		b = bytes.NewReader(crudo)
	}
	req := httptest.NewRequest(metodo, rutaChat+ruta, b)
	if b != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	if p.empresa != 0 {
		req.Header.Set(identidad.CabeceraEmpresa, fmt.Sprint(p.empresa))
	}
	if clave != "" {
		req.Header.Set("Idempotency-Key", clave)
	}
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, req)
	r := respuesta{status: w.Code, h: w.Header()}
	_ = json.Unmarshal(w.Body.Bytes(), &r.cuerpo)
	return r
}

// Datos de agenda y campo (I2), escritos directo para la prueba.
type equipo struct {
	zona1, zona2           int64
	cuadrilla1, cuadrilla2 int64
}

func (e *entorno) equipo(empresa int64) equipo {
	e.t.Helper()
	var q equipo
	base := siguienteCliente.Add(10)
	q.zona1, q.zona2 = base, base+1
	if _, err := e.bd.Exec(e.ctx, `INSERT INTO agenda.zonas (id, empresa_id, origen, nombre) VALUES ($1, $3, 'local', $4), ($2, $3, 'local', $5)`,
		q.zona1, q.zona2, empresa, fmt.Sprint("Z1 ", q.zona1), fmt.Sprint("Z2 ", q.zona2)); err != nil {
		e.t.Fatal(err)
	}
	for i, z := range []int64{q.zona1, q.zona2} {
		var id int64
		if err := e.bd.QueryRow(e.ctx, `INSERT INTO agenda.cuadrillas (empresa_id, nombre) VALUES ($1, $2) RETURNING id`, empresa,
			fmt.Sprintf("Cuadrilla %d %s", i+1, pruebas.Unico())).Scan(&id); err != nil {
			e.t.Fatal(err)
		}
		if _, err := e.bd.Exec(e.ctx, `INSERT INTO agenda.cuadrilla_zonas (cuadrilla_id, zona_id) VALUES ($1, $2)`, id, z); err != nil {
			e.t.Fatal(err)
		}
		if i == 0 {
			q.cuadrilla1 = id
		} else {
			q.cuadrilla2 = id
		}
	}
	return q
}

func (e *entorno) ficha(usuario, cuadrilla int64) {
	e.t.Helper()
	if _, err := e.bd.Exec(e.ctx, `INSERT INTO agenda.tecnicos (usuario_id, cuadrilla_id) VALUES ($1, $2)`, usuario, cuadrilla); err != nil {
		e.t.Fatal(err)
	}
}

func (e *entorno) orden(empresa, zona, cuadrilla, tecnico int64) int64 {
	e.t.Helper()
	var id int64
	if err := e.bd.QueryRow(e.ctx, `INSERT INTO campo.ordenes (empresa_id, tipo_id, zona_id, cuadrilla_id, direccion, tecnico_id, asignada_en)
		VALUES ($1, (SELECT id FROM campo.tipos_orden ORDER BY id LIMIT 1), $2, $3, 'Zona 10', $4, now()) RETURNING id`,
		empresa, zona, cuadrilla, tecnico).Scan(&id); err != nil {
		e.t.Fatal(err)
	}
	return id
}

func idConv(r respuesta) string { return fmt.Sprint(int64(r.cuerpo["id"].(float64))) }

func nuevoID() string { return uuid.Must(uuid.NewV7()).String() }

func (e *entorno) enviarChat(p persona, conv, id, texto string) respuesta {
	return e.chat(p, http.MethodPost, "/conversaciones/"+conv+"/mensajes", "", map[string]any{"id": id, "texto": texto})
}

func (e *entorno) conversacionesDe(p persona) map[string]map[string]any {
	e.t.Helper()
	r := e.chat(p, http.MethodGet, "/conversaciones?limit=200", "", nil)
	e.exigir(r, http.StatusOK)
	salida := map[string]map[string]any{}
	for _, c := range r.cuerpo["data"].([]any) {
		m := c.(map[string]any)
		salida[fmt.Sprint(int64(m["id"].(float64)))] = m
	}
	return salida
}

func (e *entorno) ultimoEvento(evento, campo, valor string) map[string]any {
	e.t.Helper()
	var crudo []byte
	err := e.bd.QueryRow(e.ctx, `SELECT payload FROM integracion.outbox WHERE evento = $1 AND payload->>$2 = $3 ORDER BY ocurrido_en DESC LIMIT 1`,
		evento, campo, valor).Scan(&crudo)
	if err != nil {
		e.t.Fatalf("%s con %s=%s: %v", evento, campo, valor, err)
	}
	var p map[string]any
	_ = json.Unmarshal(crudo, &p)
	return p
}

func TestChatMiembrosYAlcance(t *testing.T) {
	e := nuevoEntorno(t, nil)
	q := e.equipo(1)
	uno := []int64{1}
	tecA := e.persona(uno, rolBase("tecnico"))
	tecB := e.persona(uno, rolBase("tecnico"))
	e.ficha(tecA.id, q.cuadrilla1)
	e.ficha(tecB.id, q.cuadrilla2)
	supZona1 := e.persona(uno, identidad.AsignacionRol{Rol: "supervisor", ZonaID: &q.zona1})
	supCuad2 := e.persona(uno, identidad.AsignacionRol{Rol: "supervisor", CuadrillaID: &q.cuadrilla2})
	supGlobal := e.persona(uno, rolBase("supervisor"))
	despacho := e.persona(uno, rolBase("despacho"))
	lectura := e.persona(uno, rolBase("lectura"))

	// Cuadrilla 1: la abre su técnico (201 y luego 200); otro técnico no (404); la supervisión por zona sí.
	r := e.chat(tecA, http.MethodPost, fmt.Sprintf("/cuadrillas/%d/abrir", q.cuadrilla1), "", nil)
	e.exigir(r, http.StatusCreated, "tipo", "cuadrilla")
	c1 := idConv(r)
	e.exigir(e.chat(tecA, http.MethodPost, fmt.Sprintf("/cuadrillas/%d/abrir", q.cuadrilla1), "", nil), http.StatusOK, "id", c1)
	e.exigir(e.chat(tecB, http.MethodPost, fmt.Sprintf("/cuadrillas/%d/abrir", q.cuadrilla1), "", nil), http.StatusNotFound)
	e.exigir(e.chat(supZona1, http.MethodPost, fmt.Sprintf("/cuadrillas/%d/abrir", q.cuadrilla1), "", nil), http.StatusOK, "id", c1)
	if n := e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'chat.conversacion_creada' AND payload->>'conversacion_id' = $1`, c1); n != 1 {
		t.Fatalf("chat.conversacion_creada: %d", n)
	}

	e.exigir(e.enviarChat(tecA, c1, nuevoID(), "Llegué a la caja NAP 14"), http.StatusCreated, "texto", "Llegué a la caja NAP 14")
	e.exigir(e.chat(tecB, http.MethodGet, "/conversaciones/"+c1, "", nil), http.StatusNotFound)
	e.exigir(e.enviarChat(tecB, c1, nuevoID(), "hola"), http.StatusNotFound)
	e.exigir(e.chat(supZona1, http.MethodGet, "/conversaciones/"+c1, "", nil), http.StatusOK, "no_leidos", "1")
	e.exigir(e.chat(supCuad2, http.MethodGet, "/conversaciones/"+c1, "", nil), http.StatusNotFound)
	e.exigir(e.chat(supGlobal, http.MethodGet, "/conversaciones/"+c1, "", nil), http.StatusOK)
	// lectura solo ve (chat.ver) lo suyo; no envía.
	e.exigir(e.chat(lectura, http.MethodGet, "/conversaciones/"+c1, "", nil), http.StatusNotFound)
	e.exigir(e.chat(lectura, http.MethodPost, fmt.Sprintf("/cuadrillas/%d/abrir", q.cuadrilla1), "", nil), http.StatusForbidden)
	if _, ok := e.conversacionesDe(supZona1)[c1]; !ok {
		t.Fatal("la supervisión por zona no lista el chat de la cuadrilla")
	}
	if _, ok := e.conversacionesDe(tecB)[c1]; ok {
		t.Fatal("un técnico de otra cuadrilla lista el chat")
	}
	// Destinatarios del push: técnicos de la cuadrilla y supervisión que la cubre, nunca el autor.
	p := e.ultimoEvento("chat.mensaje_enviado", "conversacion_id", c1)
	dest := fmt.Sprint(p["destinatarios"])
	for _, s := range []persona{supZona1, supGlobal} {
		if !strings.Contains(dest, fmt.Sprint(s.id)) {
			t.Fatalf("falta %d en destinatarios %s", s.id, dest)
		}
	}
	for _, s := range []persona{tecA, tecB, supCuad2} {
		if strings.Contains(" "+strings.Trim(dest, "[]")+" ", " "+fmt.Sprint(s.id)+" ") {
			t.Fatalf("sobra %d en destinatarios %s", s.id, dest)
		}
	}
	if _, ok := p["texto"]; ok {
		t.Fatal("el evento lleva el texto")
	}

	// Hilo de orden (zona 2, cuadrilla 2, técnico A): su técnico, la supervisión de la cuadrilla 2 y
	// despacho (se une); la supervisión de la zona 1, no.
	o := e.orden(1, q.zona2, q.cuadrilla2, tecA.id)
	r = e.chat(tecA, http.MethodPost, fmt.Sprintf("/ordenes/%d/abrir", o), "", nil)
	e.exigir(r, http.StatusCreated, "tipo", "orden")
	co := idConv(r)
	e.exigir(e.chat(supCuad2, http.MethodGet, "/conversaciones/"+co, "", nil), http.StatusOK)
	e.exigir(e.chat(supZona1, http.MethodGet, "/conversaciones/"+co, "", nil), http.StatusNotFound)
	e.exigir(e.chat(supZona1, http.MethodPost, fmt.Sprintf("/ordenes/%d/abrir", o), "", nil), http.StatusNotFound)
	e.exigir(e.chat(tecB, http.MethodPost, fmt.Sprintf("/ordenes/%d/abrir", o), "", nil), http.StatusNotFound)
	e.exigir(e.chat(despacho, http.MethodGet, "/conversaciones/"+co, "", nil), http.StatusNotFound)
	e.exigir(e.chat(despacho, http.MethodPost, fmt.Sprintf("/ordenes/%d/abrir", o), "", nil), http.StatusCreated, "id", co)
	e.exigir(e.chat(despacho, http.MethodPost, fmt.Sprintf("/ordenes/%d/abrir", o), "", nil), http.StatusOK, "id", co)
	e.exigir(e.enviarChat(despacho, co, nuevoID(), "¿Llevas ONU de cambio?"), http.StatusCreated)
	// Reasignar la orden: el técnico nuevo entra; el anterior sale (miembros derivados, no copiados).
	if _, err := e.bd.Exec(e.ctx, `UPDATE campo.ordenes SET tecnico_id = $2 WHERE id = $1`, o, tecB.id); err != nil {
		t.Fatal(err)
	}
	e.exigir(e.chat(tecB, http.MethodGet, "/conversaciones/"+co, "", nil), http.StatusOK, "no_leidos", "1")
	e.exigir(e.chat(tecA, http.MethodGet, "/conversaciones/"+co, "", nil), http.StatusNotFound)

	// Directa: solo sus dos miembros; ninguna supervisión la ve.
	r = e.chat(tecA, http.MethodPost, "/directas", "", map[string]any{"usuario_id": tecB.id})
	e.exigir(r, http.StatusCreated, "tipo", "directa")
	cd := idConv(r)
	e.exigir(e.chat(tecB, http.MethodPost, "/directas", "", map[string]any{"usuario_id": tecA.id}), http.StatusOK, "id", cd)
	e.exigir(e.chat(supGlobal, http.MethodGet, "/conversaciones/"+cd, "", nil), http.StatusNotFound)
	e.exigir(e.chat(tecA, http.MethodPost, "/directas", "", map[string]any{"usuario_id": tecA.id}), http.StatusUnprocessableEntity)
	ventas := e.persona(uno, rolBase("ventas"))
	e.exigir(e.chat(tecA, http.MethodPost, "/directas", "", map[string]any{"usuario_id": ventas.id}), http.StatusUnprocessableEntity,
		"type", "urn:iris:problema:usuario_no_disponible")
}

func TestChatAislamientoEmpresa(t *testing.T) {
	e := nuevoEntorno(t, nil)
	q1, q2 := e.equipo(1), e.equipo(2)
	tec1 := e.persona([]int64{1}, rolBase("tecnico"))
	tec2 := e.persona([]int64{2}, rolBase("tecnico"))
	e.ficha(tec1.id, q1.cuadrilla1)
	e.ficha(tec2.id, q2.cuadrilla1)
	sup2 := e.persona([]int64{2}, rolBase("supervisor"))
	ambas := e.persona([]int64{1, 2}, rolBase("supervisor"))

	r := e.chat(tec1, http.MethodPost, fmt.Sprintf("/cuadrillas/%d/abrir", q1.cuadrilla1), "", nil)
	e.exigir(r, http.StatusCreated, "empresa_id", "1")
	c1 := idConv(r)
	e.exigir(e.enviarChat(tec1, c1, nuevoID(), "solo empresa 1"), http.StatusCreated)
	// La supervisión global de la empresa 2 no ve ni abre lo de la empresa 1.
	e.exigir(e.chat(sup2, http.MethodGet, "/conversaciones/"+c1, "", nil), http.StatusNotFound)
	e.exigir(e.chat(sup2, http.MethodPost, fmt.Sprintf("/cuadrillas/%d/abrir", q1.cuadrilla1), "", nil), http.StatusNotFound)
	if _, ok := e.conversacionesDe(sup2)[c1]; ok {
		t.Fatal("la empresa 2 lista una conversación de la empresa 1")
	}
	// Con dos empresas manda la activa.
	ambas.empresa = 2
	e.exigir(e.chat(ambas, http.MethodGet, "/conversaciones/"+c1, "", nil), http.StatusNotFound)
	e.exigir(e.enviarChat(ambas, c1, nuevoID(), "desde la 2"), http.StatusNotFound)
	ambas.empresa = 1
	e.exigir(e.chat(ambas, http.MethodGet, "/conversaciones/"+c1, "", nil), http.StatusOK)
	ambas.empresa = 0
	e.exigir(e.chat(ambas, http.MethodGet, "/conversaciones", "", nil), http.StatusUnprocessableEntity)
	// Directa con quien no opera en la empresa: 422.
	e.exigir(e.chat(tec1, http.MethodPost, "/directas", "", map[string]any{"usuario_id": tec2.id}), http.StatusUnprocessableEntity)
	// El feed de la empresa 2 no trae los eventos de la 1.
	r = e.chat(sup2, http.MethodGet, "/cambios?cursor=0", "", nil)
	e.exigir(r, http.StatusOK)
	for _, ev := range r.cuerpo["data"].([]any) {
		if fmt.Sprint(int64(ev.(map[string]any)["conversacion_id"].(float64))) == c1 {
			t.Fatal("el feed de la empresa 2 trae la empresa 1")
		}
	}
}

func TestChatReenvioIdempotente(t *testing.T) {
	e := nuevoEntorno(t, nil)
	a := e.persona([]int64{1}, rolBase("tecnico"))
	b := e.persona([]int64{1}, rolBase("despacho"))
	r := e.chat(a, http.MethodPost, "/directas", "", map[string]any{"usuario_id": b.id})
	e.exigir(r, http.StatusCreated)
	c := idConv(r)
	id := nuevoID()
	creado := e.reloj.Add(-3 * time.Minute)
	cuerpo := map[string]any{"id": id, "texto": "  Listo, −21 dBm. Ya navega.  ", "creado_en": creado, "ubicacion": map[string]any{"lat": 14.6, "lon": -90.5}}
	r = e.chat(a, http.MethodPost, "/conversaciones/"+c+"/mensajes", "", cuerpo)
	e.exigir(r, http.StatusCreated, "texto", "Listo, −21 dBm. Ya navega.", "seq", "1")
	// La cola sin conexión reenvía el mismo id: 200 con el mismo mensaje, sin duplicar ni auditar otra vez.
	e.reloj = ptrTiempo(e.reloj.Add(time.Minute))
	r = e.chat(a, http.MethodPost, "/conversaciones/"+c+"/mensajes", "", cuerpo)
	e.exigir(r, http.StatusOK, "id", id, "seq", "1")
	if r.h.Get("Idempotent-Replayed") != "true" {
		t.Fatal("falta Idempotent-Replayed")
	}
	if n := e.contar(`SELECT count(*) FROM mensajeria.chat_mensajes WHERE id = $1`, id); n != 1 {
		t.Fatalf("mensajes: %d", n)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'chat.mensaje_enviado' AND payload->>'mensaje_id' = $1`, id); n != 1 {
		t.Fatalf("eventos: %d", n)
	}
	if n := e.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'chat.enviar' AND entidad_id = $1 AND resultado = 'ok'`, id); n != 1 {
		t.Fatalf("auditoría: %d", n)
	}
	// La auditoría guarda el id, no el texto.
	if n := e.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'chat.enviar' AND entrada::text LIKE '%dBm%'`); n != 0 {
		t.Fatal("la auditoría guarda el texto")
	}
	if n := e.contar(`SELECT count(*) FROM auditoria.cambios WHERE despues::text LIKE '%dBm%'`); n != 0 {
		t.Fatal("los cambios guardan el texto")
	}
	// El mismo id con otro contenido, o de otro autor: 409.
	e.exigir(e.enviarChat(a, c, id, "otro texto"), http.StatusConflict, "type", "urn:iris:problema:mensaje_id_reutilizado")
	e.exigir(e.chat(b, http.MethodPost, "/conversaciones/"+c+"/mensajes", "", cuerpo), http.StatusConflict)
	// Idempotency-Key (regla 6): misma clave y cuerpo, misma respuesta.
	clave := "chat-" + pruebas.Unico()
	id2 := nuevoID()
	primero := e.chat(a, http.MethodPost, "/conversaciones/"+c+"/mensajes", clave, map[string]any{"id": id2, "texto": "con clave"})
	e.exigir(primero, http.StatusCreated)
	otra := e.chat(a, http.MethodPost, "/conversaciones/"+c+"/mensajes", clave, map[string]any{"id": id2, "texto": "con clave"})
	e.exigir(otra, http.StatusOK, "id", id2) // la clave la aplica el caso: el mensaje actual, sin guardar la respuesta
	e.exigir(e.chat(a, http.MethodPost, "/conversaciones/"+c+"/mensajes", clave, map[string]any{"id": nuevoID(), "texto": "otra"}), http.StatusUnprocessableEntity)
	// Validación: uuid v4, vacío, más de 4000 caracteres, futuro.
	e.exigir(e.enviarChat(a, c, uuid.NewString(), "v4"), http.StatusUnprocessableEntity)
	e.exigir(e.enviarChat(a, c, nuevoID(), "   "), http.StatusUnprocessableEntity, "type", "urn:iris:problema:mensaje_vacio")
	e.exigir(e.enviarChat(a, c, nuevoID(), strings.Repeat("ñ", 4001)), http.StatusUnprocessableEntity)
	e.exigir(e.enviarChat(a, c, nuevoID(), strings.Repeat("ñ", 4000)), http.StatusCreated)
	e.exigir(e.chat(a, http.MethodPost, "/conversaciones/"+c+"/mensajes", "", map[string]any{"id": nuevoID(), "texto": "x",
		"creado_en": e.reloj.Add(time.Hour)}), http.StatusUnprocessableEntity)
	// El texto se guarda tal cual (texto plano: el HTML no se interpreta ni se limpia).
	e.exigir(e.enviarChat(a, c, nuevoID(), "<b>hola</b>"), http.StatusCreated, "texto", "<b>hola</b>")
}

func ptrTiempo(t time.Time) *time.Time { return &t }

func TestChatEliminarEnQuinceMinutos(t *testing.T) {
	e := nuevoEntorno(t, nil)
	a := e.persona([]int64{1}, rolBase("tecnico"))
	b := e.persona([]int64{1}, rolBase("soporte"))
	r := e.chat(a, http.MethodPost, "/directas", "", map[string]any{"usuario_id": b.id})
	c := idConv(r)
	id := nuevoID()
	e.exigir(e.enviarChat(a, c, id, "me equivoqué de cliente"), http.StatusCreated)
	viejo := nuevoID()
	e.exigir(e.enviarChat(a, c, viejo, "este queda"), http.StatusCreated)

	e.exigir(e.chat(b, http.MethodPost, "/mensajes/"+id+"/eliminar", "", nil), http.StatusForbidden, "type", "urn:iris:problema:no_es_autor")
	e.reloj = ptrTiempo(e.reloj.Add(14*time.Minute + 59*time.Second))
	r = e.chat(a, http.MethodPost, "/mensajes/"+id+"/eliminar", "", nil)
	e.exigir(r, http.StatusOK, "eliminado", "true", "texto", "<nil>")
	e.exigir(e.chat(a, http.MethodPost, "/mensajes/"+id+"/eliminar", "", nil), http.StatusOK, "eliminado", "true")
	if n := e.contar(`SELECT count(*) FROM mensajeria.chat_mensajes WHERE id = $1 AND texto IS NULL AND eliminado_en IS NOT NULL`, id); n != 1 {
		t.Fatal("no quedó la lápida")
	}
	if n := e.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'chat.eliminar' AND entidad_id = $1 AND resultado = 'ok'`, id); n != 1 {
		t.Fatalf("auditoría del eliminado: %d", n)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'chat.mensaje_eliminado' AND payload->>'mensaje_id' = $1`, id); n != 1 {
		t.Fatalf("chat.mensaje_eliminado: %d", n)
	}
	// La vista previa muestra la lápida si era el último; aquí el último es «este queda».
	e.reloj = ptrTiempo(e.reloj.Add(2 * time.Second))
	e.exigir(e.chat(a, http.MethodPost, "/mensajes/"+viejo+"/eliminar", "", nil), http.StatusUnprocessableEntity,
		"type", "urn:iris:problema:plazo_vencido")
	// En la lista de mensajes, la lápida.
	r = e.chat(b, http.MethodGet, "/conversaciones/"+c+"/mensajes", "", nil)
	e.exigir(r, http.StatusOK)
	data := r.cuerpo["data"].([]any)
	if len(data) != 2 || data[0].(map[string]any)["eliminado"] != true || data[1].(map[string]any)["texto"] != "este queda" {
		t.Fatalf("mensajes: %v", data)
	}
}

func TestChatLeidosYNoLeidos(t *testing.T) {
	e := nuevoEntorno(t, nil)
	a := e.persona([]int64{1}, rolBase("tecnico"))
	b := e.persona([]int64{1}, rolBase("despacho"))
	c := idConv(e.chat(a, http.MethodPost, "/directas", "", map[string]any{"usuario_id": b.id}))
	ids := []string{nuevoID(), nuevoID(), nuevoID(), nuevoID()}
	for i, id := range ids {
		e.exigir(e.enviarChat(a, c, id, fmt.Sprintf("mensaje %d", i+1)), http.StatusCreated)
	}
	e.exigir(e.chat(a, http.MethodPost, "/mensajes/"+ids[3]+"/eliminar", "", nil), http.StatusOK)
	conv := e.conversacionesDe(b)[c]
	if conv["no_leidos"] != float64(3) {
		t.Fatalf("no leídos de b: %v", conv["no_leidos"])
	}
	if u := conv["ultimo_mensaje"].(map[string]any); u["contenido"] != "eliminado" {
		t.Fatalf("vista previa: %v", u)
	}
	if e.conversacionesDe(a)[c]["no_leidos"] != float64(0) {
		t.Fatal("los propios cuentan como no leídos")
	}
	e.exigir(e.chat(b, http.MethodPost, "/conversaciones/"+c+"/leer", "", map[string]any{"hasta_mensaje_id": ids[1]}), http.StatusOK,
		"no_leidos", "1", "leido_hasta_mensaje_id", ids[1])
	// La lectura no retrocede.
	e.exigir(e.chat(b, http.MethodPost, "/conversaciones/"+c+"/leer", "", map[string]any{"hasta_mensaje_id": ids[0]}), http.StatusOK,
		"no_leidos", "1", "leido_hasta_mensaje_id", ids[1])
	if n := e.contar(`SELECT count(*) FROM mensajeria.chat_eventos WHERE conversacion_id = $1 AND tipo = 'leido'`, c); n != 1 {
		t.Fatalf("eventos leído: %d", n)
	}
	// Un mensaje de otra conversación: 422.
	otra := e.persona([]int64{1}, rolBase("soporte"))
	c2 := idConv(e.chat(a, http.MethodPost, "/directas", "", map[string]any{"usuario_id": otra.id}))
	ajeno := nuevoID()
	e.exigir(e.enviarChat(a, c2, ajeno, "otro"), http.StatusCreated)
	e.exigir(e.chat(b, http.MethodPost, "/conversaciones/"+c+"/leer", "", map[string]any{"hasta_mensaje_id": ajeno}), http.StatusUnprocessableEntity)
	e.exigir(e.chat(b, http.MethodPost, "/conversaciones/"+c2+"/leer", "", map[string]any{"hasta_mensaje_id": ajeno}), http.StatusNotFound)
	// El «visto»: el detalle trae hasta dónde leyó b.
	r := e.chat(a, http.MethodGet, "/conversaciones/"+c, "", nil)
	e.exigir(r, http.StatusOK)
	ls := r.cuerpo["lecturas"].([]any)
	if len(ls) != 1 || ls[0].(map[string]any)["hasta_mensaje_id"] != ids[1] {
		t.Fatalf("lecturas: %v", ls)
	}
	e.exigir(e.chat(b, http.MethodPost, "/conversaciones/"+c+"/leer", "", map[string]any{"hasta_mensaje_id": ids[3]}), http.StatusOK, "no_leidos", "0")

	// Paginación en las dos direcciones, siempre por seq ascendente.
	r = e.chat(b, http.MethodGet, "/conversaciones/"+c+"/mensajes?limit=2", "", nil)
	e.exigir(r, http.StatusOK)
	if d := r.cuerpo["data"].([]any); len(d) != 2 || d[0].(map[string]any)["seq"] != float64(3) || d[1].(map[string]any)["seq"] != float64(4) {
		t.Fatalf("anteriores: %v", d)
	}
	r = e.chat(b, http.MethodGet, "/conversaciones/"+c+"/mensajes?limit=2&cursor="+r.cuerpo["nextCursor"].(string), "", nil)
	if d := r.cuerpo["data"].([]any); len(d) != 2 || d[0].(map[string]any)["seq"] != float64(1) || r.cuerpo["nextCursor"] != nil {
		t.Fatalf("anteriores 2: %v", r.cuerpo)
	}
	r = e.chat(b, http.MethodGet, "/conversaciones/"+c+"/mensajes?direccion=siguientes&desde_seq=2&limit=1", "", nil)
	if d := r.cuerpo["data"].([]any); len(d) != 1 || d[0].(map[string]any)["seq"] != float64(3) {
		t.Fatalf("siguientes: %v", r.cuerpo)
	}
}

func TestChatLimitePorMinuto(t *testing.T) {
	e := nuevoEntorno(t, nil)
	a := e.persona([]int64{1}, rolBase("tecnico"))
	b := e.persona([]int64{1}, rolBase("despacho"))
	c := idConv(e.chat(a, http.MethodPost, "/directas", "", map[string]any{"usuario_id": b.id}))
	primero := nuevoID()
	for i := range 30 {
		id := nuevoID()
		if i == 0 {
			id = primero
		}
		e.exigir(e.enviarChat(a, c, id, fmt.Sprint(i)), http.StatusCreated)
		e.reloj = ptrTiempo(e.reloj.Add(time.Second))
	}
	r := e.enviarChat(a, c, nuevoID(), "el 31")
	e.exigir(r, http.StatusTooManyRequests, "type", "urn:iris:problema:demasiados_mensajes")
	if r.h.Get("Retry-After") == "" {
		t.Fatal("falta Retry-After")
	}
	// Un reenvío no cuenta; el otro usuario no comparte el límite.
	e.exigir(e.enviarChat(a, c, primero, "0"), http.StatusOK)
	e.exigir(e.enviarChat(b, c, nuevoID(), "yo sí puedo"), http.StatusCreated)
	// Pasado el minuto del primero, vuelve a poder.
	e.reloj = ptrTiempo(e.reloj.Add(31 * time.Second))
	e.exigir(e.enviarChat(a, c, nuevoID(), "ahora sí"), http.StatusCreated)
}

// sse lee un flujo SSE: cada evento por el canal (los comentarios de latido como tipo "latido").
type eventoSSE struct {
	id, tipo string
	datos    map[string]any
}

// abrirSSE abre el flujo; el cuerpo lo cierra la lectura al terminar (cancelar ctx).
func abrirSSE(t *testing.T, ctx context.Context, url string, p persona, ultimo string) <-chan eventoSSE {
	t.Helper()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url+rutaChat+"/eventos", nil)
	if p.cookie != "" {
		req.AddCookie(&http.Cookie{Name: identidad.CookieSesion, Value: p.cookie})
	} else {
		req.Header.Set("Authorization", "Bearer "+p.token)
	}
	req.Header.Set("Accept", "text/event-stream")
	if ultimo != "" {
		req.Header.Set("Last-Event-ID", ultimo)
	}
	res, err := http.DefaultClient.Do(req) //nolint:bodyclose // lo cierra la goroutine que lee el flujo
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK {
		cuerpo, _ := io.ReadAll(res.Body)
		_ = res.Body.Close()
		t.Fatalf("SSE %d: %s", res.StatusCode, cuerpo)
	}
	c := make(chan eventoSSE, 100)
	go func() {
		defer close(c)
		defer func() { _ = res.Body.Close() }()
		lector := bufio.NewScanner(res.Body)
		lector.Buffer(make([]byte, 64*1024), 1024*1024)
		var ev eventoSSE
		for lector.Scan() {
			linea := lector.Text()
			switch {
			case strings.HasPrefix(linea, ": latido"):
				c <- eventoSSE{tipo: "latido"}
			case strings.HasPrefix(linea, "id: "):
				ev.id = strings.TrimPrefix(linea, "id: ")
			case strings.HasPrefix(linea, "event: "):
				ev.tipo = strings.TrimPrefix(linea, "event: ")
			case strings.HasPrefix(linea, "data: "):
				_ = json.Unmarshal([]byte(strings.TrimPrefix(linea, "data: ")), &ev.datos)
			case linea == "" && ev.tipo != "":
				c <- ev
				ev = eventoSSE{}
			}
		}
	}()
	return c
}

// esperar el siguiente evento que no sea latido.
func esperar(t *testing.T, c <-chan eventoSSE, tipo string) eventoSSE {
	t.Helper()
	limite := time.After(5 * time.Second)
	for {
		select {
		case ev, ok := <-c:
			if !ok {
				t.Fatalf("el flujo se cerró esperando %s", tipo)
			}
			if ev.tipo == "latido" && tipo != "latido" {
				continue
			}
			if ev.tipo != tipo {
				t.Fatalf("llegó %s (esperaba %s): %v", ev.tipo, tipo, ev.datos)
			}
			return ev
		case <-limite:
			t.Fatalf("no llegó %s", tipo)
		}
	}
}

func TestChatSSEEntregaYReanuda(t *testing.T) {
	e := nuevoEntorno(t, func(c *mensajeria.Config) {
		c.ChatLatido = 200 * time.Millisecond
		c.ChatDuracionMax = time.Minute
	})
	// Como el proceso de mensajería: WriteTimeout y ReadTimeout cortos; el flujo debe sobrevivirlos.
	srv := httptest.NewUnstartedServer(e.h)
	srv.Config.WriteTimeout, srv.Config.ReadTimeout = time.Second, time.Second
	srv.Start()
	defer srv.Close()
	ctx, cancelar := context.WithCancel(context.Background())
	defer cancelar() // antes de srv.Close: corta los flujos abiertos
	go e.m.EscucharChat(ctx, pruebas.Pool(t), nil)

	q := e.equipo(1)
	a := e.persona([]int64{1}, rolBase("tecnico"))
	b := e.persona([]int64{1}, rolBase("tecnico"))
	ajeno := e.persona([]int64{1}, rolBase("tecnico"))
	e.ficha(a.id, q.cuadrilla1)
	e.ficha(b.id, q.cuadrilla1)
	e.ficha(ajeno.id, q.cuadrilla2)
	c := idConv(e.chat(a, http.MethodPost, fmt.Sprintf("/cuadrillas/%d/abrir", q.cuadrilla1), "", nil))
	// Cabeza del feed antes de abrir el flujo (lo que hace la app al arrancar).
	cab := e.chat(ajeno, http.MethodGet, "/cambios", "", nil)
	e.exigir(cab, http.StatusOK)
	cabezaAjeno := cab.cuerpo["nextCursor"].(string)

	fctx, cerrar := context.WithCancel(ctx)
	eventos := abrirSSE(t, fctx, srv.URL, b, "")
	esperar(t, eventos, "latido")
	time.Sleep(1200 * time.Millisecond) // más que el WriteTimeout y el ReadTimeout del servidor
	id1 := nuevoID()
	e.exigir(e.enviarChat(a, c, id1, "¿alguien tiene conector SC/APC?"), http.StatusCreated)
	ev := esperar(t, eventos, "mensaje_nuevo")
	msj := ev.datos["mensaje"].(map[string]any)
	if msj["id"] != id1 || msj["texto"] != "¿alguien tiene conector SC/APC?" || ev.datos["no_leidos"] != float64(1) || ev.id != ev.datos["id"] {
		t.Fatalf("evento: %v", ev)
	}
	ultimo := ev.id
	e.exigir(e.chat(b, http.MethodPost, "/conversaciones/"+c+"/leer", "", map[string]any{"hasta_mensaje_id": id1}), http.StatusOK)
	lect := esperar(t, eventos, "leido")
	if lect.datos["no_leidos"] != float64(0) || lect.datos["lectura"].(map[string]any)["usuario_id"] != float64(b.id) {
		t.Fatalf("leído: %v", lect.datos)
	}
	cerrar()
	// Sin conexión: llegan dos mensajes y se elimina uno.
	id2, id3 := nuevoID(), nuevoID()
	e.exigir(e.enviarChat(a, c, id2, "segundo"), http.StatusCreated)
	e.exigir(e.enviarChat(a, c, id3, "tercero"), http.StatusCreated)
	e.exigir(e.chat(a, http.MethodPost, "/mensajes/"+id3+"/eliminar", "", nil), http.StatusOK)
	// Reanuda con Last-Event-ID: el evento de leído, los dos mensajes (el tercero ya como lápida) y la eliminación.
	eventos = abrirSSE(t, ctx, srv.URL, b, ultimo)
	esperar(t, eventos, "leido")
	if ev := esperar(t, eventos, "mensaje_nuevo"); ev.datos["mensaje"].(map[string]any)["id"] != id2 {
		t.Fatalf("reanudar: %v", ev.datos)
	}
	if ev := esperar(t, eventos, "mensaje_nuevo"); ev.datos["mensaje"].(map[string]any)["eliminado"] != true {
		t.Fatalf("reanudar (lápida): %v", ev.datos)
	}
	elim := esperar(t, eventos, "mensaje_eliminado")
	// El sondeo da lo mismo con el mismo cursor.
	r := e.chat(b, http.MethodGet, "/cambios?cursor="+ultimo, "", nil)
	e.exigir(r, http.StatusOK, "nextCursor", elim.id)
	if len(r.cuerpo["data"].([]any)) != 4 {
		t.Fatalf("cambios: %v", r.cuerpo["data"])
	}
	// El técnico de otra cuadrilla no recibió nada.
	r = e.chat(ajeno, http.MethodGet, "/cambios?cursor="+cabezaAjeno, "", nil)
	e.exigir(r, http.StatusOK)
	if len(r.cuerpo["data"].([]any)) != 0 {
		t.Fatalf("el ajeno recibió: %v", r.cuerpo["data"])
	}
	// Una conversación nueva llega como conversacion_actualizada a quien la ve.
	r = e.chat(a, http.MethodPost, "/directas", "", map[string]any{"usuario_id": b.id})
	e.exigir(r, http.StatusCreated)
	if ev := esperar(t, eventos, "conversacion_actualizada"); ev.datos["conversacion"].(map[string]any)["tipo"] != "directa" {
		t.Fatalf("conversación: %v", ev.datos)
	}

	// Un cursor anterior a la purga (7 días): 410 en SSE y en el sondeo.
	e.reloj = ptrTiempo(e.reloj.Add(8 * 24 * time.Hour))
	if err := e.m.PurgarFeedChat(e.ctx, e.bd); err != nil {
		t.Fatal(err)
	}
	e.exigir(e.chat(b, http.MethodGet, "/cambios?cursor="+ultimo, "", nil), http.StatusGone, "type", "urn:iris:problema:cursor_vencido")
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+rutaChat+"/eventos", nil)
	req.Header.Set("Authorization", "Bearer "+b.token)
	req.Header.Set("Last-Event-ID", ultimo)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusGone {
		t.Fatalf("SSE con cursor vencido: %d", res.StatusCode)
	}
	// Sin permiso de chat: 403.
	ventas := e.persona([]int64{1}, rolBase("ventas"))
	req, _ = http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+rutaChat+"/eventos", nil)
	req.Header.Set("Authorization", "Bearer "+ventas.token)
	if res, err = http.DefaultClient.Do(req); err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("SSE sin chat.ver: %d", res.StatusCode)
	}
}

func TestChatFotoDelMensaje(t *testing.T) {
	e := nuevoEntorno(t, nil)
	mensajeria.RegistrarAdjuntos()
	a := e.persona([]int64{1}, rolBase("tecnico"))
	b := e.persona([]int64{1}, rolBase("despacho"))
	c := idConv(e.chat(a, http.MethodPost, "/directas", "", map[string]any{"usuario_id": b.id}))
	adj := uuid.Must(uuid.NewV7())
	insertar := func(id uuid.UUID, entidadID string, tipo string, autor int64) {
		if _, err := e.bd.Exec(e.ctx, `INSERT INTO nucleo.adjuntos (id, entidad, entidad_id, tipo, mime, tamano, sha256, empresa_id, subido_por, almacenamiento, clave)
			VALUES ($1, 'chat_conversacion', $2, $3, 'image/jpeg', 10, repeat('a', 64), 1, $4, 'local', $5)`, id, entidadID, tipo, autor, "chat/"+id.String()); err != nil {
			t.Fatal(err)
		}
	}
	insertar(adj, c, "foto", a.id)
	r := e.chat(a, http.MethodPost, "/conversaciones/"+c+"/mensajes", "", map[string]any{"id": nuevoID(), "adjunto_id": adj})
	e.exigir(r, http.StatusCreated)
	if f := r.cuerpo["adjunto"].(map[string]any); f["estado"] != "pendiente" || f["id"] != adj.String() {
		t.Fatalf("adjunto: %v", f)
	}
	// La misma foto en otro mensaje: 409; de otro autor u otra conversación: 422.
	e.exigir(e.chat(a, http.MethodPost, "/conversaciones/"+c+"/mensajes", "", map[string]any{"id": nuevoID(), "adjunto_id": adj}), http.StatusConflict)
	ajena := uuid.Must(uuid.NewV7())
	insertar(ajena, c, "foto", b.id)
	e.exigir(e.chat(a, http.MethodPost, "/conversaciones/"+c+"/mensajes", "", map[string]any{"id": nuevoID(), "adjunto_id": ajena}), http.StatusUnprocessableEntity)
	otra := uuid.Must(uuid.NewV7())
	insertar(otra, "999999", "foto", a.id)
	e.exigir(e.chat(a, http.MethodPost, "/conversaciones/"+c+"/mensajes", "", map[string]any{"id": nuevoID(), "adjunto_id": otra}), http.StatusUnprocessableEntity)
	firma := uuid.Must(uuid.NewV7())
	insertar(firma, c, "firma", a.id)
	e.exigir(e.chat(a, http.MethodPost, "/conversaciones/"+c+"/mensajes", "", map[string]any{"id": nuevoID(), "adjunto_id": firma}), http.StatusUnprocessableEntity,
		"type", "urn:iris:problema:adjunto_no_es_foto")
}
