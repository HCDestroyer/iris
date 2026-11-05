package mensajeria_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/HCDestroyer/iris/apps/api/internal/mensajeria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/adjuntos"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/servidor"
)

// Revisión de seguridad del chat (PR #110): fotos de mensajes eliminados o nunca enviados,
// revalidación del SSE, topes de flujos, despacho «unido» y el cursor del feed.

// servirSSE sirve e.h por HTTP con LISTEN iris_chat; al terminar corta los flujos y cierra.
func (e *entorno) servirSSE(t *testing.T) (string, context.Context) {
	t.Helper()
	srv := httptest.NewServer(e.h)
	t.Cleanup(srv.Close)
	ctx, cancelar := context.WithCancel(context.Background())
	t.Cleanup(cancelar) // corre antes que srv.Close
	go e.m.EscucharChat(ctx, pruebas.Pool(t), nil)
	return srv.URL, ctx
}

// estadoSSE abre un flujo y devuelve su status (el cuerpo queda abierto hasta cancelar ctx si es 200).
func estadoSSE(t *testing.T, ctx context.Context, url string, p persona) int {
	t.Helper()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url+rutaChat+"/eventos", nil)
	req.Header.Set("Authorization", "Bearer "+p.token)
	req.Header.Set("Accept", "text/event-stream")
	res, err := http.DefaultClient.Do(req) //nolint:bodyclose // los 200 los cierra cancelar ctx
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK {
		_ = res.Body.Close()
	} else {
		go func() { _, _ = io.Copy(io.Discard, res.Body); _ = res.Body.Close() }()
	}
	return res.StatusCode
}

// cerrado espera a que el flujo se cierre (sin entregar mensaje_nuevo).
func cerrado(t *testing.T, c <-chan eventoSSE) {
	t.Helper()
	limite := time.After(5 * time.Second)
	for {
		select {
		case ev, ok := <-c:
			if !ok {
				return
			}
			if ev.tipo == "mensaje_nuevo" {
				t.Fatalf("entregó un mensaje después de perder el acceso: %v", ev.datos)
			}
		case <-limite:
			t.Fatal("el flujo no se cerró")
		}
	}
}

func TestChatEliminarBorraLaFoto(t *testing.T) {
	alm, err := adjuntos.NuevoDisco(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e := nuevoEntorno(t, func(c *mensajeria.Config) { c.Almacen = alm })
	adj := adjuntos.Nuevo(adjuntos.Config{Almacen: alm})
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	e.h, _ = servidor.Nuevo(servidor.Deps{BD: e.bd, Log: log, Version: "prueba",
		Idempotencia: nucleo.OpcionesIdempotencia{Omitir: mensajeria.IdempotenciaPropia}, Modulos: []func(huma.API){e.m.Registrar, adj.RegistrarRutas}})
	mensajeria.RegistrarAdjuntos()
	a := e.persona([]int64{1}, rolBase("tecnico"))
	b := e.persona([]int64{1}, rolBase("despacho"))
	c := idConv(e.chat(a, http.MethodPost, "/directas", "", map[string]any{"usuario_id": b.id}))
	contenido := []byte("\xff\xd8\xff\xe0 foto de la caja NAP")
	foto := func() uuid.UUID {
		id := uuid.Must(uuid.NewV7())
		clave := "chat/" + id.String()
		if err := alm.Guardar(e.ctx, clave, bytes.NewReader(contenido), int64(len(contenido)), "image/jpeg", ""); err != nil {
			t.Fatal(err)
		}
		if _, err := e.bd.Exec(e.ctx, `INSERT INTO nucleo.adjuntos (id, entidad, entidad_id, tipo, mime, tamano, recibido, sha256, estado, completado_en,
			empresa_id, subido_por, almacenamiento, clave) VALUES ($1, 'chat_conversacion', $2, 'foto', 'image/jpeg', $3, $3, repeat('a', 64), 'completo', now(),
			1, $4, 'local', $5)`, id, c, len(contenido), a.id, clave); err != nil {
			t.Fatal(err)
		}
		return id
	}
	existe := func(id uuid.UUID) bool {
		r, err := alm.Abrir(e.ctx, "chat/"+id.String())
		if err == nil {
			_ = r.Close()
		}
		return err == nil
	}
	listar := func(p persona) []any {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/adjuntos?entidad=chat_conversacion&entidadId="+c, nil)
		req.Header.Set("Authorization", "Bearer "+p.token)
		w := httptest.NewRecorder()
		e.h.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("listar adjuntos: %d %s", w.Code, w.Body.String())
		}
		var cuerpo map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &cuerpo)
		return cuerpo["data"].([]any)
	}
	descargar := func(p persona, id uuid.UUID) int {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/adjuntos/"+id.String(), nil)
		req.Header.Set("Authorization", "Bearer "+p.token)
		w := httptest.NewRecorder()
		e.h.ServeHTTP(w, req)
		return w.Code
	}

	enviada, sinEnviar := foto(), foto()
	msj := nuevoID()
	e.exigir(e.chat(a, http.MethodPost, "/conversaciones/"+c+"/mensajes", "", map[string]any{"id": msj, "adjunto_id": enviada}), http.StatusCreated)
	// Con el mensaje vivo se ve y se descarga; la nunca enviada, no (ni para su autor).
	if l := listar(b); len(l) != 1 || l[0].(map[string]any)["id"] != enviada.String() {
		t.Fatalf("lista con el mensaje vivo: %v", l)
	}
	if s := descargar(b, enviada); s != http.StatusOK {
		t.Fatalf("descargar la foto enviada: %d", s)
	}
	if s := descargar(a, sinEnviar); s != http.StatusNotFound {
		t.Fatalf("descargar la foto nunca enviada: %d", s)
	}

	// Eliminar el mensaje: la foto queda rechazada, fuera de la lista, 404 y fuera del almacén.
	e.exigir(e.chat(a, http.MethodPost, "/mensajes/"+msj+"/eliminar", "", nil), http.StatusOK, "eliminado", "true")
	if l := listar(b); len(l) != 0 {
		t.Fatalf("lista después de eliminar: %v", l)
	}
	if s := descargar(b, enviada); s != http.StatusNotFound {
		t.Fatalf("descargar después de eliminar: %d", s)
	}
	if existe(enviada) {
		t.Fatal("la foto sigue en el almacén")
	}
	if n := e.contar(`SELECT count(*) FROM nucleo.adjuntos WHERE id = $1 AND estado = 'rechazado' AND motivo_rechazo = 'retirado'`, enviada); n != 1 {
		t.Fatal("la foto no quedó rechazada")
	}
	// La lápida no guarda nada del contenido.
	if n := e.contar(`SELECT count(*) FROM mensajeria.chat_mensajes WHERE id = $1 AND hash_contenido IS NULL AND adjunto_id IS NULL`, msj); n != 1 {
		t.Fatal("la lápida guarda datos")
	}
	// Un reenvío del mismo id (la cola sin conexión) responde la lápida, sin 409.
	e.exigir(e.chat(a, http.MethodPost, "/conversaciones/"+c+"/mensajes", "", map[string]any{"id": msj, "adjunto_id": enviada}), http.StatusOK, "eliminado", "true")

	// La nunca enviada se retira a las 48 h sin actividad.
	if err := e.m.PurgarFotosSinEnviar(e.ctx, e.bd); err != nil {
		t.Fatal(err)
	}
	if !existe(sinEnviar) {
		t.Fatal("se retiró antes de tiempo")
	}
	e.reloj = ptrTiempo(time.Now().Add(49 * time.Hour))
	if err := e.m.PurgarFotosSinEnviar(e.ctx, e.bd); err != nil {
		t.Fatal(err)
	}
	if existe(sinEnviar) || e.contar(`SELECT count(*) FROM nucleo.adjuntos WHERE id = $1 AND estado = 'rechazado'`, sinEnviar) != 1 {
		t.Fatal("la foto nunca enviada no se retiró")
	}
}

func TestChatSSERevalidaLaSesion(t *testing.T) {
	e := nuevoEntorno(t, func(c *mensajeria.Config) { c.ChatLatido = 100 * time.Millisecond })
	url, ctx := e.servirSSE(t)
	q := e.equipo(1)
	a := e.persona([]int64{1}, rolBase("tecnico"))
	e.ficha(a.id, q.cuadrilla1)
	c := idConv(e.chat(a, http.MethodPost, fmt.Sprintf("/cuadrillas/%d/abrir", q.cuadrilla1), "", nil))

	// Sesión del panel (cookie): cerrarla corta el flujo.
	panel := e.persona([]int64{1}, rolBase("tecnico"))
	e.ficha(panel.id, q.cuadrilla1)
	s, err := identChat.IniciarSesion.Ejecutar(nucleo.ConBD(context.Background(), e.bd), identidad.EntradaIniciarSesion{Correo: panel.correo, Contrasena: "contrasena-larga-1"})
	if err != nil {
		t.Fatal(err)
	}
	panel.cookie = s.Token
	porCookie := abrirSSE(t, ctx, url, panel, "")
	e.exigir(e.enviarChat(a, c, nuevoID(), "primero"), http.StatusCreated)
	esperar(t, porCookie, "mensaje_nuevo")
	if _, err := e.bd.Exec(e.ctx, `UPDATE nucleo.sesiones SET revocada_en = now() WHERE usuario_id = $1`, panel.id); err != nil {
		t.Fatal(err)
	}
	cerrado(t, porCookie)

	// Token revocado: igual. Usuario desactivado: igual.
	tok := e.persona([]int64{1}, rolBase("tecnico"))
	e.ficha(tok.id, q.cuadrilla1)
	porToken := abrirSSE(t, ctx, url, tok, "")
	esperar(t, porToken, "latido")
	if _, err := e.bd.Exec(e.ctx, `UPDATE nucleo.tokens_api SET revocado_en = now() WHERE usuario_id = $1`, tok.id); err != nil {
		t.Fatal(err)
	}
	cerrado(t, porToken)
	inactivo := e.persona([]int64{1}, rolBase("tecnico"))
	e.ficha(inactivo.id, q.cuadrilla1)
	flujo := abrirSSE(t, ctx, url, inactivo, "")
	esperar(t, flujo, "latido")
	if _, err := e.bd.Exec(e.ctx, `UPDATE nucleo.usuarios SET activo = false WHERE id = $1`, inactivo.id); err != nil {
		t.Fatal(err)
	}
	e.exigir(e.enviarChat(a, c, nuevoID(), "no lo ve"), http.StatusCreated)
	cerrado(t, flujo)

	// Quitar chat.supervisar (le queda chat.ver por lectura): el flujo sigue, pero ya no entrega lo
	// de ese alcance.
	sup := e.persona([]int64{1}, rolBase("lectura"), identidad.AsignacionRol{Rol: "supervisor", ZonaID: &q.zona1})
	vigila := abrirSSE(t, ctx, url, sup, "")
	e.exigir(e.enviarChat(a, c, nuevoID(), "visto por supervisión"), http.StatusCreated)
	esperar(t, vigila, "mensaje_nuevo")
	if _, err := e.bd.Exec(e.ctx, `DELETE FROM nucleo.usuarios_roles WHERE usuario_id = $1 AND rol_id = (SELECT id FROM nucleo.roles WHERE codigo = 'supervisor')`, sup.id); err != nil {
		t.Fatal(err)
	}
	e.exigir(e.enviarChat(a, c, nuevoID(), "ya no le toca"), http.StatusCreated)
	limite := time.After(1500 * time.Millisecond)
	for latidos := 0; ; {
		select {
		case ev, ok := <-vigila:
			if !ok {
				t.Fatal("el flujo se cerró (todavía tiene chat.ver)")
			}
			if ev.tipo == "mensaje_nuevo" {
				t.Fatalf("entregó un mensaje fuera de su alcance: %v", ev.datos)
			}
			latidos++
		case <-limite:
			if latidos == 0 {
				t.Fatal("sin latidos")
			}
			return
		}
	}
}

func TestChatSSETopes(t *testing.T) {
	e := nuevoEntorno(t, func(c *mensajeria.Config) { c.ChatSSEMax = 7 })
	url, ctx := e.servirSSE(t)
	a := e.persona([]int64{1}, rolBase("tecnico"))
	b := e.persona([]int64{1}, rolBase("tecnico"))
	c := e.persona([]int64{1}, rolBase("tecnico"))
	fctx, cerrar := context.WithCancel(ctx)
	for i := range 5 {
		if s := estadoSSE(t, fctx, url, a); s != http.StatusOK {
			t.Fatalf("flujo %d: %d", i+1, s)
		}
	}
	// El sexto del mismo usuario: 429 (no se cierra el más viejo).
	if s := estadoSSE(t, ctx, url, a); s != http.StatusTooManyRequests {
		t.Fatalf("sexto flujo: %d", s)
	}
	// Tope del proceso (7): b abre 2 y c ya no.
	for range 2 {
		if s := estadoSSE(t, fctx, url, b); s != http.StatusOK {
			t.Fatalf("flujo de b: %d", s)
		}
	}
	if s := estadoSSE(t, ctx, url, c); s != http.StatusServiceUnavailable {
		t.Fatalf("sobre el tope del proceso: %d", s)
	}
	// Al cerrarse, los lugares se liberan.
	cerrar()
	deadline := time.Now().Add(3 * time.Second)
	for estadoSSE(t, ctx, url, c) != http.StatusOK {
		if time.Now().After(deadline) {
			t.Fatal("los lugares no se liberaron")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestChatDespachoUnidoPierdeLaOrden(t *testing.T) {
	e := nuevoEntorno(t, nil)
	q := e.equipo(1)
	tec := e.persona([]int64{1}, rolBase("tecnico"))
	desp := e.persona([]int64{1}, identidad.AsignacionRol{Rol: "despacho", ZonaID: &q.zona2})
	o := e.orden(1, q.zona2, q.cuadrilla2, tec.id)
	r := e.chat(desp, http.MethodPost, fmt.Sprintf("/ordenes/%d/abrir", o), "", nil)
	e.exigir(r, http.StatusCreated)
	co := idConv(r)
	e.exigir(e.chat(desp, http.MethodGet, "/conversaciones/"+co, "", nil), http.StatusOK)
	// La orden pasa a otra zona y cuadrilla: su ordenes.ver ya no la cubre y deja de ver el hilo.
	if _, err := e.bd.Exec(e.ctx, `UPDATE campo.ordenes SET zona_id = $2, cuadrilla_id = $3 WHERE id = $1`, o, q.zona1, q.cuadrilla1); err != nil {
		t.Fatal(err)
	}
	e.exigir(e.chat(desp, http.MethodGet, "/conversaciones/"+co, "", nil), http.StatusNotFound)
	e.exigir(e.enviarChat(desp, co, nuevoID(), "hola"), http.StatusNotFound)
	if _, ok := e.conversacionesDe(desp)[co]; ok {
		t.Fatal("sigue en su lista")
	}
	// Ni le llega el push.
	e.exigir(e.enviarChat(tec, co, nuevoID(), "¿alguien?"), http.StatusCreated)
	p := e.ultimoEvento("chat.mensaje_enviado", "conversacion_id", co)
	for _, d := range p["destinatarios"].([]any) {
		if int64(d.(float64)) == desp.id {
			t.Fatal("el despacho que ya no ve la orden sigue en destinatarios")
		}
	}
	// Si vuelve a su zona, vuelve a verla.
	if _, err := e.bd.Exec(e.ctx, `UPDATE campo.ordenes SET zona_id = $2 WHERE id = $1`, o, q.zona2); err != nil {
		t.Fatal(err)
	}
	e.exigir(e.chat(desp, http.MethodGet, "/conversaciones/"+co, "", nil), http.StatusOK)
}

// Un evento que confirma entre la lectura de la cabeza y la de los eventos no se salta.
func TestChatFeedNoPierdeEventos(t *testing.T) {
	e := nuevoEntorno(t, nil)
	a := e.persona([]int64{1}, rolBase("tecnico"))
	b := e.persona([]int64{1}, rolBase("despacho"))
	c := idConv(e.chat(a, http.MethodPost, "/directas", "", map[string]any{"usuario_id": b.id}))
	cab := e.chat(b, http.MethodGet, "/cambios", "", nil)
	e.exigir(cab, http.StatusOK)
	id := nuevoID()
	una := true
	e.m.FijarTrasLeerCabeza(func() {
		if una {
			una = false
			e.exigir(e.enviarChat(a, c, id, "llegó justo entre las dos lecturas"), http.StatusCreated)
		}
	})
	defer e.m.FijarTrasLeerCabeza(nil)
	cursor := cab.cuerpo["nextCursor"].(string)
	visto := false
	for range 2 {
		r := e.chat(b, http.MethodGet, "/cambios?cursor="+cursor, "", nil)
		e.exigir(r, http.StatusOK)
		for _, ev := range r.cuerpo["data"].([]any) {
			if m, ok := ev.(map[string]any)["mensaje"].(map[string]any); ok && m["id"] == id {
				visto = true
			}
		}
		cursor = r.cuerpo["nextCursor"].(string)
	}
	if !visto {
		t.Fatal("el feed se saltó el evento")
	}
}
