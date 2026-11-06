package notificaciones_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/HCDestroyer/iris/apps/api/internal/notificaciones"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// Dispositivos y preferencias -------------------------------------------------------------------

func TestRegistrarDispositivoIdempotentePorToken(t *testing.T) {
	e := nuevoEntorno(t, true)
	ana := e.persona([]int64{1}, "tecnico")
	beto := e.persona([]int64{1}, "tecnico")
	token := tokenFCM() + "abc123"
	cuerpo := map[string]any{"token": token, "plataforma": "android", "appVersion": "1.4.0"}

	r := e.esperar(e.api(ana, http.MethodPost, "/api/v1/dispositivos", cuerpo), http.StatusCreated)
	id := r.cuerpo["id"]
	if strings.Contains(r.crudo, token) || r.cuerpo["tokenFin"] != "abc123" {
		t.Fatalf("la respuesta no debe traer el token, solo su final: %s", r.crudo)
	}
	cuerpo["appVersion"] = "1.5.0"
	r = e.esperar(e.api(ana, http.MethodPost, "/api/v1/dispositivos", cuerpo), http.StatusOK)
	if r.cuerpo["id"] != id || r.cuerpo["appVersion"] != "1.5.0" {
		t.Fatalf("renovar debe ser el mismo dispositivo: %s", r.crudo)
	}
	if p := e.ultimoEvento("dispositivo.registrado"); p["nuevo"] != false || p["token_fin"] != "abc123" {
		t.Fatalf("dispositivo.registrado: %v", p)
	}
	// La auditoría nunca guarda el token.
	if n := e.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'dispositivos.registrar' AND entrada::text LIKE '%' || $1 || '%'`, token); n != 0 {
		t.Fatalf("el token quedó en la auditoría (%d filas)", n)
	}
	if n := e.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'dispositivos.registrar' AND entrada->'auditoria'->>'fin_fcm' = 'abc123'`); n != 2 {
		t.Fatalf("la auditoría debe tener el final del token: %d", n)
	}

	// Beto no ve ni quita el de Ana.
	l := e.esperar(e.api(beto, http.MethodGet, "/api/v1/dispositivos", nil), http.StatusOK)
	if len(l.cuerpo["data"].([]any)) != 0 {
		t.Fatalf("Beto ve dispositivos ajenos: %s", l.crudo)
	}
	e.esperar(e.api(beto, http.MethodPost, fmt.Sprintf("/api/v1/dispositivos/%v/quitar", id), nil), http.StatusNotFound)

	// El teléfono cambia de manos: el token pasa a Beto y se le quita a Ana.
	e.esperar(e.api(beto, http.MethodPost, "/api/v1/dispositivos", cuerpo), http.StatusCreated)
	if p := e.ultimoEvento("dispositivo.quitado"); p["motivo"] != notificaciones.QuitadoOtroUsuario || int64(p["usuario_id"].(float64)) != ana.id {
		t.Fatalf("dispositivo.quitado: %v", p)
	}
	if l := e.esperar(e.api(ana, http.MethodGet, "/api/v1/dispositivos", nil), http.StatusOK); len(l.cuerpo["data"].([]any)) != 0 {
		t.Fatalf("Ana sigue con el dispositivo: %s", l.crudo)
	}
	l = e.esperar(e.api(beto, http.MethodGet, "/api/v1/dispositivos", nil), http.StatusOK)
	propio := l.cuerpo["data"].([]any)[0].(map[string]any)["id"]
	e.esperar(e.api(beto, http.MethodPost, fmt.Sprintf("/api/v1/dispositivos/%v/quitar", propio), nil), http.StatusOK)
	e.esperar(e.api(beto, http.MethodPost, fmt.Sprintf("/api/v1/dispositivos/%v/quitar", propio), nil), http.StatusNotFound)

	// Validación e integraciones.
	for _, malo := range []string{"corto:APA91b", strings.Repeat("a", 120) + " con espacio", strings.Repeat("a", 120) + "<b>", strings.Repeat("a", 4097)} {
		e.esperar(e.api(ana, http.MethodPost, "/api/v1/dispositivos", map[string]any{"token": malo, "plataforma": "android", "appVersion": "1"}), http.StatusUnprocessableEntity)
	}
	e.esperar(e.api(ana, http.MethodPost, "/api/v1/dispositivos", map[string]any{"token": tokenFCM(), "plataforma": "ios", "appVersion": "1"}), http.StatusUnprocessableEntity)
	e.esperar(e.api(persona{}, http.MethodGet, "/api/v1/dispositivos", nil), http.StatusUnauthorized)
}

func TestCerrarSesionQuitaSusDispositivos(t *testing.T) {
	e := nuevoEntorno(t, true)
	ana := e.persona([]int64{1}, "tecnico")
	otra := e.entrar(ana) // la misma persona en otro teléfono
	e.dispositivo(ana)
	e.dispositivo(otra)
	if n := e.contar(`SELECT count(*) FROM notificaciones.dispositivos WHERE usuario_id = $1`, ana.id); n != 2 {
		t.Fatalf("dispositivos: %d", n)
	}
	e.esperar(e.api(ana, http.MethodDelete, "/api/v1/sesion", nil), http.StatusNoContent)
	if n := e.contar(`SELECT count(*) FROM notificaciones.dispositivos WHERE usuario_id = $1`, ana.id); n != 1 {
		t.Fatalf("al cerrar sesión se quita solo el de esa sesión: %d", n)
	}
	if p := e.ultimoEvento("dispositivo.quitado"); p["motivo"] != notificaciones.QuitadoSesionCerrada {
		t.Fatalf("dispositivo.quitado: %v", p)
	}
	// La sesión que vence o cierra un administrador: la purga.
	if _, err := e.bd.Exec(e.ctx, `UPDATE nucleo.sesiones SET revocada_en = now() WHERE usuario_id = $1`, ana.id); err != nil {
		t.Fatal(err)
	}
	r, err := e.notif.PurgarCaducos.Ejecutar(e.ctx, struct{}{})
	if err != nil || r.Dispositivos < 1 {
		t.Fatalf("purga: %+v %v", r, err)
	}
	if n := e.contar(`SELECT count(*) FROM notificaciones.dispositivos WHERE usuario_id = $1`, ana.id); n != 0 {
		t.Fatalf("la purga deja dispositivos sin sesión: %d", n)
	}
}

func TestPreferenciasConVersion(t *testing.T) {
	e := nuevoEntorno(t, false)
	ana := e.persona([]int64{1}, "tecnico")
	r := e.esperar(e.api(ana, http.MethodGet, "/api/v1/yo/notificaciones", nil), http.StatusOK)
	nm := r.cuerpo["noMolestar"].(map[string]any)
	if r.cuerpo["version"] != 0.0 || r.cuerpo["contenido"] != "completo" || nm["desde"] != "21:00" || nm["hasta"] != "06:00" ||
		nm["activo"] != false || r.cuerpo["zonaHoraria"] != "America/Guatemala" || r.cuerpo["bandejaDisponible"] != false {
		t.Fatalf("valores por defecto: %s", r.crudo)
	}
	if p := r.cuerpo["push"].(map[string]any); p["estado"] != "sin_configurar" || p["texto"] != "Notificaciones push: sin configurar" {
		t.Fatalf("sin FCM: %v", p)
	}
	cuerpo := map[string]any{"version": 0, "tipos": map[string]any{"chat": false, "mencion": true, "orden": true, "bandeja": true},
		"contenido": "remitente", "noMolestar": map[string]any{"activo": true, "desde": "22:30", "hasta": "05:00", "urgenteIgual": false},
		"zonaHoraria": "America/Guatemala"}
	r = e.esperar(e.api(ana, http.MethodPost, "/api/v1/yo/notificaciones", cuerpo), http.StatusOK)
	if r.cuerpo["version"] != 1.0 || r.cuerpo["contenido"] != "remitente" {
		t.Fatalf("guardar: %s", r.crudo)
	}
	// Otra pantalla con la versión vieja: 409.
	e.esperar(e.api(ana, http.MethodPost, "/api/v1/yo/notificaciones", cuerpo), http.StatusConflict)
	p := e.ultimoEvento("notificaciones.preferencias_cambiadas")
	if int64(p["usuario_id"].(float64)) != ana.id || p["version"] != 1.0 || !strings.Contains(fmt.Sprint(p["campos"]), "contenido") {
		t.Fatalf("evento: %v", p)
	}
	// Se ve igual desde otro teléfono (es de la cuenta).
	if r := e.esperar(e.api(e.entrar(ana), http.MethodGet, "/api/v1/yo/notificaciones", nil), http.StatusOK); r.cuerpo["version"] != 1.0 {
		t.Fatalf("otra sesión: %s", r.crudo)
	}
	cuerpo["version"] = 1
	cuerpo["noMolestar"] = map[string]any{"activo": true, "desde": "05:00", "hasta": "05:00", "urgenteIgual": true}
	e.esperar(e.api(ana, http.MethodPost, "/api/v1/yo/notificaciones", cuerpo), http.StatusUnprocessableEntity)
	cuerpo["noMolestar"] = map[string]any{"activo": true, "desde": "21:00", "hasta": "06:00", "urgenteIgual": true}
	cuerpo["zonaHoraria"] = "Marte/Olimpo"
	e.esperar(e.api(ana, http.MethodPost, "/api/v1/yo/notificaciones", cuerpo), http.StatusUnprocessableEntity)
	if n := e.contar(`SELECT count(*) FROM auditoria.cambios c JOIN auditoria.acciones a ON a.id = c.accion_id
		WHERE a.accion = 'notificaciones.guardar_preferencias' AND c.entidad_id = $1 AND c.campo = 'contenido'`, fmt.Sprint(ana.id)); n != 1 {
		t.Fatalf("auditoría de preferencias: %d", n)
	}
}

// Chat -----------------------------------------------------------------------------------------

func TestChatAvisaAlOtroYNuncaAlAutor(t *testing.T) {
	e := nuevoEntorno(t, true)
	ana := e.persona([]int64{1}, "tecnico")
	beto := e.persona([]int64{1}, "tecnico")
	tokAna, tokBeto := e.dispositivo(ana), e.dispositivo(beto)
	conv := e.directa(ana, beto)
	msj := e.mensaje(ana, conv, map[string]any{"texto": "ya llegué a la casa"})
	if n := e.procesar(); n != 1 {
		t.Fatalf("envíos encolados: %d", n)
	}
	if len(e.fcm.para(tokAna)) != 0 {
		t.Fatal("el autor no recibe su propio mensaje")
	}
	ms := e.fcm.para(tokBeto)
	if len(ms) != 1 {
		t.Fatalf("Beto: %d push", len(ms))
	}
	m := ms[0]
	d := m.Data
	if d["v"] != "1" || d["tipo"] != "chat" || d["canal"] != "chat" || d["publicoTitulo"] != "Nuevo mensaje" || d["titulo"] != ana.nombre ||
		d["cuerpo"] != "ya llegué a la casa" || d["enlace"] != fmt.Sprintf("iris://chat/%d", conv) || d["acciones"] != "responder,leido" ||
		d["empresaId"] != "1" || d["usuarioId"] != fmt.Sprint(beto.id) || d["grupo"] != fmt.Sprintf("chat-%d", conv) {
		t.Fatalf("data: %v", d)
	}
	if m.Android.Prioridad != "HIGH" || m.Android.TTL != "3600s" {
		t.Fatalf("android: %+v", m.Android)
	}
	dt := datos(t, m)
	rem := dt["remitente"].(map[string]any)
	if dt["mensajeId"] != msj || dt["texto"] != "ya llegué a la casa" || dt["contenido"] != "texto" || dt["conversacionTipo"] != "directa" ||
		rem["nombre"] != ana.nombre || rem["iniciales"] != "P"+string([]rune(strings.Fields(ana.nombre)[1])[0:1]) {
		t.Fatalf("datos: %v", dt)
	}

	// El mismo evento otra vez (el relay puede entregarlo dos veces): ningún envío nuevo.
	var ev notificaciones.EventoPush
	var payload []byte
	if err := e.bd.QueryRow(e.ctx, `SELECT id, payload FROM integracion.outbox WHERE evento = 'chat.mensaje_enviado' AND payload->>'mensaje_id' = $1`, msj).
		Scan(&ev.ID, &payload); err != nil {
		t.Fatal(err)
	}
	ev.Evento, ev.Payload, ev.EmpresaID = "chat.mensaje_enviado", payload, emp(1)
	e.repetir(ev)
	if n := e.enviarEncolados(); n != 0 {
		t.Fatalf("un evento repetido encoló %d envíos", n)
	}
	// Y el job repetido no manda otra vez.
	var envio int64
	if err := e.bd.QueryRow(e.ctx, `SELECT id FROM notificaciones.envios WHERE evento_id = $1`, ev.ID).Scan(&envio); err != nil {
		t.Fatal(err)
	}
	if err := e.notif.Enviar(e.ctx, envio, false); err != nil {
		t.Fatal(err)
	}
	if len(e.fcm.para(tokBeto)) != 1 {
		t.Fatal("un evento repetido mandó dos veces")
	}
	if estado, _ := e.envio(ev.ID); estado != "enviado" {
		t.Fatalf("estado: %s", estado)
	}
}

func TestChatSoloRemitenteRecorteYUbicacion(t *testing.T) {
	e := nuevoEntorno(t, true)
	ana := e.persona([]int64{1}, "tecnico")
	beto := e.persona([]int64{1}, "tecnico")
	tok := e.dispositivo(beto)
	conv := e.directa(ana, beto)

	largo := strings.Repeat("ñandú ", 60) // 360 caracteres
	e.mensaje(ana, conv, map[string]any{"texto": largo})
	e.mensaje(ana, conv, map[string]any{"ubicacion": map[string]any{"lat": 14.6, "lon": -90.5}})
	e.procesar()
	ms := e.fcm.para(tok)
	if len(ms) != 2 {
		t.Fatalf("push: %d", len(ms))
	}
	recortado := datos(t, ms[0])["texto"].(string)
	if r := []rune(recortado); len(r) != notificaciones.MaxTexto || !strings.HasSuffix(recortado, "…") {
		t.Fatalf("recorte a %d: %d %q", notificaciones.MaxTexto, len(r), recortado)
	}
	if d := datos(t, ms[1]); d["texto"] != "📍 Ubicación" || d["contenido"] != "ubicacion" || ms[1].Data["cuerpo"] != "📍 Ubicación" {
		t.Fatalf("ubicación: %v", d)
	}

	// contenido = remitente: solo «Nuevo mensaje de X», sin el texto en ninguna clave.
	e.preferencias(beto, func(c map[string]any) { c["contenido"] = "remitente" })
	e.mensaje(ana, conv, map[string]any{"texto": "la clave del router es secreta"})
	e.procesar()
	ms = e.fcm.para(tok)
	m := ms[len(ms)-1]
	if m.Data["cuerpo"] != "Nuevo mensaje de "+ana.nombre || datos(t, m)["texto"] != nil {
		t.Fatalf("remitente: %v", m.Data)
	}
	b, _ := json.Marshal(m)
	if strings.Contains(string(b), "secreta") {
		t.Fatalf("con remitente el texto no viaja: %s", b)
	}

	// Tipo apagado: no sale.
	e.preferencias(beto, func(c map[string]any) { c["tipos"].(map[string]any)["chat"] = false })
	e.mensaje(ana, conv, map[string]any{"texto": "otro"})
	e.procesar()
	if len(e.fcm.para(tok)) != 3 {
		t.Fatal("con el chat apagado no debe salir push")
	}
	if n := e.contar(`SELECT count(*) FROM notificaciones.envios WHERE usuario_id = $1 AND motivo = 'preferencia'`, beto.id); n != 1 {
		t.Fatalf("omitido por preferencia: %d", n)
	}
}

func TestChatCuadrillaQuienSaleDejaDeRecibir(t *testing.T) {
	e := nuevoEntorno(t, true)
	var c1, c2 int64
	for i, c := range []*int64{&c1, &c2} {
		if err := e.bd.QueryRow(e.ctx, `INSERT INTO agenda.cuadrillas (empresa_id, nombre) VALUES (1, $1) RETURNING id`,
			fmt.Sprintf("Cuadrilla Norte %d %s", i, pruebas.Unico())).Scan(c); err != nil {
			t.Fatal(err)
		}
	}
	ana := e.persona([]int64{1}, "tecnico")
	beto := e.persona([]int64{1}, "tecnico")
	caro := e.persona([]int64{1}, "tecnico")
	for _, p := range []persona{ana, beto, caro} {
		if _, err := e.bd.Exec(e.ctx, `INSERT INTO agenda.tecnicos (usuario_id, cuadrilla_id) VALUES ($1, $2)`, p.id, c1); err != nil {
			t.Fatal(err)
		}
	}
	tokBeto, tokCaro := e.dispositivo(beto), e.dispositivo(caro)
	r := e.api(ana, http.MethodPost, fmt.Sprintf("/api/v1/mensajeria/chat/cuadrillas/%d/abrir", c1), nil)
	if r.status != http.StatusCreated && r.status != http.StatusOK {
		t.Fatalf("abrir cuadrilla: %d %s", r.status, r.crudo)
	}
	conv := int64(r.cuerpo["id"].(float64))
	e.mensaje(ana, conv, map[string]any{"texto": "salimos 8:00"})
	// Caro pasa a otra cuadrilla antes de que salga el push.
	if _, err := e.bd.Exec(e.ctx, `UPDATE agenda.tecnicos SET cuadrilla_id = $2 WHERE usuario_id = $1`, caro.id, c2); err != nil {
		t.Fatal(err)
	}
	if n := e.procesar(); n != 2 {
		t.Fatalf("envíos: %d", n)
	}
	ms := e.fcm.para(tokBeto)
	if len(ms) != 1 || !strings.HasPrefix(ms[0].Data["titulo"], "Cuadrilla Norte 0") || ms[0].Data["cuerpo"] != ana.nombre+": salimos 8:00" {
		t.Fatalf("Beto: %+v", ms)
	}
	if len(e.fcm.para(tokCaro)) != 0 {
		t.Fatal("quien salió de la cuadrilla no recibe sus push")
	}
	if n := e.contar(`SELECT count(*) FROM notificaciones.envios WHERE usuario_id = $1 AND motivo = 'sin_permiso'`, caro.id); n != 1 {
		t.Fatalf("omitido sin permiso: %d", n)
	}

	// Sin chat.ver (rol de lectura sin chat): tampoco.
	if _, err := e.bd.Exec(e.ctx, `DELETE FROM nucleo.usuarios_roles WHERE usuario_id = $1`, beto.id); err != nil {
		t.Fatal(err)
	}
	e.mensaje(ana, conv, map[string]any{"texto": "¿alguien trae escalera?"})
	e.procesar()
	if len(e.fcm.para(tokBeto)) != 1 {
		t.Fatal("con el permiso revocado no debe salir push")
	}
}

func TestEmpresasAisladas(t *testing.T) {
	e := nuevoEntorno(t, true)
	ana := e.persona([]int64{1}, "tecnico")
	beto := e.persona([]int64{1, 2}, "tecnico")
	beto2 := beto
	beto2.empresa = 2
	tok2 := e.dispositivo(beto2) // solo en la empresa 2
	conv := e.directa(ana, beto)
	e.mensaje(ana, conv, map[string]any{"texto": "hola"})
	e.procesar()
	if len(e.fcm.para(tok2)) != 0 {
		t.Fatal("un mensaje de la empresa 1 llegó al teléfono registrado en la empresa 2")
	}

	// Beto en la empresa 1 y luego sale de ella: el dispositivo se quita al enviar.
	tok1 := e.dispositivo(beto)
	e.mensaje(ana, conv, map[string]any{"texto": "¿sigues?"})
	if _, err := e.bd.Exec(e.ctx, `DELETE FROM nucleo.usuarios_empresas WHERE usuario_id = $1 AND empresa_id = 1`, beto.id); err != nil {
		t.Fatal(err)
	}
	e.procesar()
	if len(e.fcm.para(tok1)) != 0 {
		t.Fatal("quien salió de la empresa no recibe push")
	}
	if p := e.ultimoEvento("dispositivo.quitado"); p["motivo"] != notificaciones.QuitadoSinAcceso {
		t.Fatalf("dispositivo.quitado: %v", p)
	}
}

// Bandeja --------------------------------------------------------------------------------------

func (e *entorno) conversacionCliente(empresa int64, asignado *int64) int64 {
	e.t.Helper()
	var canal, conv int64
	if err := e.bd.QueryRow(e.ctx, `INSERT INTO mensajeria.canales (empresa_id, tipo, canal, nombre, numero, phone_number_id, credenciales_cifradas, activo)
		VALUES ($1, 'whatsapp_cloud', 'whatsapp', 'Soporte', '+50222000099', $2, '\x00', false) RETURNING id`, empresa,
		fmt.Sprint(time.Now().UnixNano()%1e12)).Scan(&canal); err != nil {
		e.t.Fatal(err)
	}
	if err := e.bd.QueryRow(e.ctx, `INSERT INTO mensajeria.conversaciones (empresa_id, canal_id, numero, perfil_nombre, asignado_a, ultima_actividad)
		VALUES ($1, $2, '+50255550001', 'Juan Pérez', $3, now()) RETURNING id`, empresa, canal, asignado).Scan(&conv); err != nil {
		e.t.Fatal(err)
	}
	return conv
}

func (e *entorno) nota(conv, empresa, autor int64, texto string) uuid.UUID {
	e.t.Helper()
	id := uuid.Must(uuid.NewV7())
	if _, err := e.bd.Exec(e.ctx, `INSERT INTO mensajeria.conversacion_mensajes (id, conversacion_id, empresa_id, seq, direccion, tipo, texto, autor_id, creado_en)
		VALUES ($1, $2, $3, (SELECT coalesce(max(seq), 0) + 1 FROM mensajeria.conversacion_mensajes WHERE conversacion_id = $2), 'nota', 'texto', $4, $5, now())`,
		id, conv, empresa, texto, autor); err != nil {
		e.t.Fatal(err)
	}
	return id
}

func TestMencionEnLaBandeja(t *testing.T) {
	e := nuevoEntorno(t, true)
	ana := e.persona([]int64{1}, "soporte")
	laura := e.persona([]int64{1}, "soporte")
	tokAna, tokLaura := e.dispositivo(ana), e.dispositivo(laura)
	conv := e.conversacionCliente(1, nil)
	n := e.nota(conv, 1, ana.id, "@Laura ¿le das seguimiento?")
	e.evento("conversacion.mencion", emp(1), "", map[string]any{"conversacion_id": conv, "mensaje_id": n, "empresa_id": 1, "usuario_id": laura.id, "autor_id": ana.id})
	// Mencionarse a sí mismo no avisa.
	e.evento("conversacion.mencion", emp(1), "", map[string]any{"conversacion_id": conv, "mensaje_id": n, "empresa_id": 1, "usuario_id": ana.id, "autor_id": ana.id})
	e.enviarEncolados()
	if len(e.fcm.para(tokAna)) != 0 {
		t.Fatal("el autor no recibe su propia mención")
	}
	ms := e.fcm.para(tokLaura)
	if len(ms) != 1 {
		t.Fatalf("Laura: %d", len(ms))
	}
	d := ms[0].Data
	if d["tipo"] != "mencion" || d["canal"] != "mencion" || d["publicoTitulo"] != "Te mencionaron" || d["titulo"] != "Juan Pérez" ||
		d["cuerpo"] != ana.nombre+" te mencionó: @Laura ¿le das seguimiento?" || d["enlace"] != fmt.Sprintf("iris://conversaciones/%d", conv) {
		t.Fatalf("data: %v", d)
	}
	if dt := datos(t, ms[0]); dt["origen"] != "bandeja" || dt["mensajeId"] != n.String() {
		t.Fatalf("datos: %v", dt)
	}

	// Un técnico sin conversaciones.ver no recibe la mención aunque llegue el evento.
	tec := e.persona([]int64{1}, "tecnico")
	tokTec := e.dispositivo(tec)
	ev := e.evento("conversacion.mencion", emp(1), "", map[string]any{"conversacion_id": conv, "mensaje_id": n, "empresa_id": 1, "usuario_id": tec.id, "autor_id": ana.id})
	e.enviarEncolados()
	if len(e.fcm.para(tokTec)) != 0 {
		t.Fatal("sin conversaciones.ver no hay push")
	}
	if estado, motivo := e.envio(ev.ID); estado != "omitido" || motivo != "sin_permiso" {
		t.Fatalf("envío: %s %s", estado, motivo)
	}

	// Con «solo quién escribe» no sale ni el texto ni el nombre del cliente.
	e.preferencias(laura, func(c map[string]any) { c["contenido"] = "remitente" })
	n2 := e.nota(conv, 1, ana.id, "@Laura el cliente debe dos meses")
	e.evento("conversacion.mencion", emp(1), "", map[string]any{"conversacion_id": conv, "mensaje_id": n2, "empresa_id": 1, "usuario_id": laura.id, "autor_id": ana.id})
	e.enviarEncolados()
	ms = e.fcm.para(tokLaura)
	m := ms[len(ms)-1]
	if m.Data["titulo"] != notificaciones.TituloConversacionCliente || m.Data["cuerpo"] != ana.nombre+" te mencionó" {
		t.Fatalf("remitente: %v", m.Data)
	}
	if b, _ := json.Marshal(m); strings.Contains(string(b), "Juan Pérez") || strings.Contains(string(b), "debe dos meses") {
		t.Fatalf("con remitente no viajan el cliente ni el texto: %s", b)
	}
}

func TestBandejaAlguienLaAtiende(t *testing.T) {
	e := nuevoEntorno(t, true)
	ana := e.persona([]int64{1}, "soporte")
	beto := e.persona([]int64{1}, "soporte")
	tokAna, tokBeto := e.dispositivo(ana), e.dispositivo(beto)
	conv := e.conversacionCliente(1, nil)
	e.evento("conversacion.aviso_sin_atender", emp(1), "", map[string]any{"conversacion_id": conv, "empresa_id": 1, "cola_id": nil,
		"usuarios": []int64{ana.id}, "bot_contesta_en": "2026-11-06T16:30:00Z"})
	e.enviarEncolados()
	if len(e.fcm.para(tokBeto)) != 0 {
		t.Fatal("solo a los disponibles del evento")
	}
	ms := e.fcm.para(tokAna)
	if len(ms) != 1 {
		t.Fatalf("Ana: %d", len(ms))
	}
	if d := ms[0].Data; d["tipo"] != "bandeja" || d["titulo"] != "¿Alguien la atiende?" || d["cuerpo"] != "Cliente esperando en la bandeja · Juan Pérez" ||
		d["publicoTitulo"] != "Cliente esperando" || d["acciones"] != "ver" {
		t.Fatalf("data: %v", d)
	}
	// Si ya la tomó alguien antes de salir el push, no se pregunta.
	if _, err := e.bd.Exec(e.ctx, `UPDATE mensajeria.conversaciones SET asignado_a = $2 WHERE id = $1`, conv, beto.id); err != nil {
		t.Fatal(err)
	}
	ev := e.evento("conversacion.aviso_sin_atender", emp(1), "", map[string]any{"conversacion_id": conv, "empresa_id": 1, "usuarios": []int64{ana.id}})
	e.enviarEncolados()
	if estado, motivo := e.envio(ev.ID); estado != "omitido" || motivo != "ya_no_aplica" || len(e.fcm.para(tokAna)) != 1 {
		t.Fatalf("ya asignada: %s %s", estado, motivo)
	}
}

// Órdenes --------------------------------------------------------------------------------------

func (e *entorno) orden(empresa int64, tecnico int64, inicio, fin time.Time) (int64, string) {
	e.t.Helper()
	zona := time.Now().UnixNano() % 1_000_000_000
	if _, err := e.bd.Exec(e.ctx, `INSERT INTO agenda.zonas (id, empresa_id, origen, nombre) VALUES ($1, $2, 'local', 'Villa Nueva')`, zona, empresa); err != nil {
		e.t.Fatal(err)
	}
	var id int64
	var numero string
	if err := e.bd.QueryRow(e.ctx, `INSERT INTO campo.ordenes (empresa_id, tipo_id, zona_id, direccion, contacto_nombre, tecnico_id, asignada_en, estado, programada_inicio, programada_fin)
		VALUES ($1, (SELECT id FROM campo.tipos_orden WHERE codigo = 'instalacion'), $2, '5a. calle 3-21, zona 4', 'Familia Morales', $3, now(), 'programada', $4, $5)
		RETURNING id, numero`, empresa, zona, tecnico, inicio, fin).Scan(&id, &numero); err != nil {
		e.t.Fatal(err)
	}
	return id, numero
}

func TestOrdenAsignadaYReprogramadaUnSoloPush(t *testing.T) {
	e := nuevoEntorno(t, true)
	tec := e.persona([]int64{1}, "tecnico")
	tok := e.dispositivo(tec)
	// Hoy 14:00–16:00 en Guatemala.
	hoy, _ := e.orden(1, tec.id, time.Date(2026, 11, 6, 20, 0, 0, 0, time.UTC), time.Date(2026, 11, 6, 22, 0, 0, 0, time.UTC))
	// Reprogramar con cambio de técnico emite los dos en la misma acción: un solo push.
	e.evento("orden.programada", emp(1), "req-1", map[string]any{"orden_id": hoy, "tecnico_id": tec.id, "reprogramada": true, "origen": "reprogramar"})
	e.evento("orden.asignada", emp(1), "req-1", map[string]any{"orden_id": hoy, "tecnico_id": tec.id, "tecnico_anterior_id": 999, "origen": "reprogramar"})
	e.enviarEncolados()
	ms := e.fcm.para(tok)
	if len(ms) != 1 {
		t.Fatalf("push: %d", len(ms))
	}
	m := ms[0]
	numero := datos(t, m)["numero"].(string)
	if d := m.Data; d["tipo"] != "orden" || d["titulo"] != numero+" asignada para hoy" || d["cuerpo"] != "14:00–16:00 · Villa Nueva · Instalación · Familia Morales" ||
		d["publicoTitulo"] != "Orden actualizada" || d["acciones"] != "ver_orden,como_llegar" || d["enlace"] != fmt.Sprintf("iris://orden/%d", hoy) {
		t.Fatalf("data: %v", d)
	}
	if dt := datos(t, m); dt["cambio"] != "asignada" || dt["hoy"] != true || dt["tipo"] != "instalacion" || dt["cliente"] != "Familia Morales" {
		t.Fatalf("datos: %v", dt)
	}
	if m.Android.CollapseKey != fmt.Sprintf("orden-%d", hoy) || m.Android.TTL != "43200s" {
		t.Fatalf("android: %+v", m.Android)
	}

	// Reprogramada para mañana (otra acción).
	manana, _ := e.orden(1, tec.id, time.Date(2026, 11, 7, 15, 0, 0, 0, time.UTC), time.Date(2026, 11, 7, 17, 0, 0, 0, time.UTC))
	e.evento("orden.programada", emp(1), "req-2", map[string]any{"orden_id": manana, "tecnico_id": tec.id, "reprogramada": true, "origen": "reprogramar"})
	// Lo que no avisa: la primera programación, la de origen asignar y una asignación al mismo técnico.
	e.evento("orden.programada", emp(1), "req-3", map[string]any{"orden_id": manana, "tecnico_id": tec.id, "reprogramada": false, "origen": "programar"})
	e.evento("orden.programada", emp(1), "req-4", map[string]any{"orden_id": manana, "tecnico_id": tec.id, "reprogramada": true, "origen": "asignar"})
	e.evento("orden.asignada", emp(1), "req-5", map[string]any{"orden_id": manana, "tecnico_id": tec.id, "tecnico_anterior_id": tec.id})
	if n := e.enviarEncolados(); n != 1 {
		t.Fatalf("envíos: %d", n)
	}
	ms = e.fcm.para(tok)
	if len(ms) != 2 || !strings.HasSuffix(ms[1].Data["titulo"], " reprogramada para mañana") || datos(t, ms[1])["hoy"] != false {
		t.Fatalf("mañana: %v", ms[len(ms)-1].Data)
	}

	// Si la orden ya pasó a otro técnico al salir el push, no se avisa.
	otro := e.persona([]int64{1}, "tecnico")
	ev := e.evento("orden.asignada", emp(1), "req-6", map[string]any{"orden_id": manana, "tecnico_id": tec.id, "tecnico_anterior_id": nil})
	if _, err := e.bd.Exec(e.ctx, `UPDATE campo.ordenes SET tecnico_id = $2 WHERE id = $1`, manana, otro.id); err != nil {
		t.Fatal(err)
	}
	e.enviarEncolados()
	if estado, motivo := e.envio(ev.ID); estado != "omitido" || motivo != "ya_no_aplica" {
		t.Fatalf("reasignada: %s %s", estado, motivo)
	}
}

// No molestar ----------------------------------------------------------------------------------

func TestNoMolestarConYSinUrgencia(t *testing.T) {
	e := nuevoEntorno(t, true)
	ana := e.persona([]int64{1}, "tecnico")
	tec := e.persona([]int64{1}, "tecnico", "soporte")
	tok := e.dispositivo(tec)
	e.preferencias(tec, func(c map[string]any) {
		c["noMolestar"] = map[string]any{"activo": true, "desde": "21:00", "hasta": "06:00", "urgenteIgual": true}
	})
	// 22:00 en Guatemala.
	e.reloj = time.Date(2026, 11, 7, 4, 0, 0, 0, time.UTC)
	conv := e.directa(ana, tec)
	e.mensaje(ana, conv, map[string]any{"texto": "¿mañana a qué hora?"})
	e.procesar()
	if len(e.fcm.para(tok)) != 0 {
		t.Fatal("en No molestar el chat no sale")
	}
	if n := e.contar(`SELECT count(*) FROM notificaciones.envios WHERE usuario_id = $1 AND motivo = 'no_molestar'`, tec.id); n != 1 {
		t.Fatalf("omitido por no molestar: %d", n)
	}
	// Una orden para hoy (23:00 local) es urgente: sale.
	hoy, _ := e.orden(1, tec.id, time.Date(2026, 11, 7, 5, 0, 0, 0, time.UTC), time.Date(2026, 11, 7, 5, 30, 0, 0, time.UTC))
	e.evento("orden.asignada", emp(1), "", map[string]any{"orden_id": hoy, "tecnico_id": tec.id})
	// Una para mañana no.
	manana, _ := e.orden(1, tec.id, time.Date(2026, 11, 7, 16, 0, 0, 0, time.UTC), time.Date(2026, 11, 7, 17, 0, 0, 0, time.UTC))
	e.evento("orden.asignada", emp(1), "", map[string]any{"orden_id": manana, "tecnico_id": tec.id})
	// Una mención es urgente.
	cc := e.conversacionCliente(1, nil)
	n := e.nota(cc, 1, ana.id, "@tec mira esto")
	e.evento("conversacion.mencion", emp(1), "", map[string]any{"conversacion_id": cc, "mensaje_id": n, "usuario_id": tec.id, "autor_id": ana.id})
	e.enviarEncolados()
	ms := e.fcm.para(tok)
	if len(ms) != 2 || ms[0].Data["tipo"] != "orden" || ms[1].Data["tipo"] != "mencion" {
		t.Fatalf("urgentes en No molestar: %d", len(ms))
	}
	// Sin «avisar lo urgente igual», nada.
	e.preferencias(tec, func(c map[string]any) {
		c["noMolestar"] = map[string]any{"activo": true, "desde": "21:00", "hasta": "06:00", "urgenteIgual": false}
	})
	e.evento("orden.asignada", emp(1), "", map[string]any{"orden_id": hoy, "tecnico_id": tec.id})
	e.enviarEncolados()
	if len(e.fcm.para(tok)) != 2 {
		t.Fatal("sin urgenteIgual no sale ni lo urgente")
	}
	// A las 7:00 ya sale todo (no se guardó nada para después).
	e.reloj = time.Date(2026, 11, 7, 13, 0, 0, 0, time.UTC)
	e.mensaje(ana, conv, map[string]any{"texto": "buenos días"})
	e.procesar()
	if ms := e.fcm.para(tok); len(ms) != 3 || ms[2].Data["cuerpo"] != "buenos días" {
		t.Fatalf("fuera del horario: %d", len(ms))
	}
}

// Respuestas de FCM ----------------------------------------------------------------------------

func TestTokenInvalidoSeQuitaYReintentos(t *testing.T) {
	e := nuevoEntorno(t, true)
	ana := e.persona([]int64{1}, "tecnico")
	tec := e.persona([]int64{1}, "tecnico")
	tok := e.dispositivo(tec)
	conv := e.directa(ana, tec)

	// 503: se reintenta (el envío sigue pendiente) y en el último intento queda agotado.
	e.fcm.responder(tok, http.StatusServiceUnavailable, "UNAVAILABLE")
	e.mensaje(ana, conv, map[string]any{"texto": "uno"})
	if _, err := e.relay.Ejecutar(e.ctx); err != nil {
		t.Fatal(err)
	}
	e.mu.Lock()
	id := e.envios[0]
	e.envios = nil
	e.mu.Unlock()
	if err := e.notif.Enviar(e.ctx, id, false); !errors.Is(err, notificaciones.ErrReintentar) {
		t.Fatalf("503: %v", err)
	}
	if n := e.contar(`SELECT count(*) FROM notificaciones.envios WHERE id = $1 AND estado = 'pendiente' AND intentos = 1`, id); n != 1 {
		t.Fatal("tras un 503 el envío sigue pendiente")
	}
	if err := e.notif.Enviar(e.ctx, id, true); err != nil {
		t.Fatalf("último intento: %v", err)
	}
	if n := e.contar(`SELECT count(*) FROM notificaciones.envios WHERE id = $1 AND estado = 'fallido' AND motivo LIKE 'agotado%'`, id); n != 1 {
		t.Fatal("agotado")
	}
	if d := notificaciones.EsperaReintento(1); d != 15*time.Second || notificaciones.EsperaReintento(3) != time.Minute || notificaciones.EsperaReintento(20) != 30*time.Minute {
		t.Fatalf("espera exponencial: %v", d)
	}

	// 403 (otro 4xx): no se reintenta y el dispositivo queda.
	e.fcm.responder(tok, http.StatusForbidden, "PERMISSION_DENIED")
	e.mensaje(ana, conv, map[string]any{"texto": "dos"})
	e.procesar()
	if n := e.contar(`SELECT count(*) FROM notificaciones.envios WHERE usuario_id = $1 AND estado = 'fallido' AND motivo LIKE 'rechazado%'`, tec.id); n != 1 {
		t.Fatal("un 4xx queda fallido sin reintento")
	}

	// UNREGISTERED: se quita el dispositivo.
	e.fcm.responder(tok, http.StatusNotFound, "UNREGISTERED")
	e.mensaje(ana, conv, map[string]any{"texto": "tres"})
	e.procesar()
	if n := e.contar(`SELECT count(*) FROM notificaciones.dispositivos WHERE usuario_id = $1`, tec.id); n != 0 {
		t.Fatal("un token inválido se quita")
	}
	if p := e.ultimoEvento("dispositivo.quitado"); p["motivo"] != notificaciones.QuitadoTokenInvalido {
		t.Fatalf("dispositivo.quitado: %v", p)
	}
	// Un 404 sin UNREGISTERED, o INVALID_ARGUMENT / SENDER_ID_MISMATCH sin señalar message.token (un
	// error del servidor, no del teléfono): fallido y el dispositivo queda.
	tok2 := e.dispositivo(tec)
	for i, r := range []struct {
		status int
		codigo string
	}{{http.StatusNotFound, "NOT_FOUND"}, {http.StatusBadRequest, "INVALID_ARGUMENT"}, {http.StatusForbidden, "SENDER_ID_MISMATCH"}} {
		e.fcm.responder(tok2, r.status, r.codigo)
		e.mensaje(ana, conv, map[string]any{"texto": fmt.Sprint("sin quitar ", i)})
		e.procesar()
		if n := e.contar(`SELECT count(*) FROM notificaciones.dispositivos WHERE usuario_id = $1`, tec.id); n != 1 {
			t.Fatalf("%s sin message.token no quita el dispositivo", r.codigo)
		}
	}
	if n := e.contar(`SELECT count(*) FROM notificaciones.envios WHERE usuario_id = $1 AND estado = 'fallido' AND motivo LIKE 'rechazado%'`, tec.id); n != 4 {
		t.Fatalf("fallidos sin quitar: %d", n)
	}
	// INVALID_ARGUMENT que señala message.token: sí se quita.
	e.fcm.responderCampo(tok2, http.StatusBadRequest, "INVALID_ARGUMENT", "message.token")
	e.mensaje(ana, conv, map[string]any{"texto": "cuatro"})
	e.procesar()
	if n := e.contar(`SELECT count(*) FROM notificaciones.dispositivos WHERE usuario_id = $1`, tec.id); n != 0 {
		t.Fatal("INVALID_ARGUMENT en message.token quita el token")
	}
}

func TestClienteFCMGuardaElTokenDeAcceso(t *testing.T) {
	f := nuevoFCM(t)
	c := f.cliente(t)
	ctx := context.Background()
	for range 3 {
		if _, err := c.Enviar(ctx, notificaciones.MensajeFCM{Token: "t1", Data: map[string]string{"v": "1"}}); err != nil {
			t.Fatal(err)
		}
	}
	if f.tokens != 1 {
		t.Fatalf("el token de acceso se guarda hasta vencer: pedidos %d", f.tokens)
	}
	// Un 401 renueva el token una vez y reintenta.
	f.mu.Lock()
	f.rechazar = 1
	f.mu.Unlock()
	if _, err := c.Enviar(ctx, notificaciones.MensajeFCM{Token: "t1", Data: map[string]string{}}); err != nil {
		t.Fatal(err)
	}
	if f.tokens != 2 || len(f.para("t1")) != 4 {
		t.Fatalf("401: tokens %d, mensajes %d", f.tokens, len(f.para("t1")))
	}
}

// Sin configuración ----------------------------------------------------------------------------

func TestSinConfiguracionElPushQuedaApagado(t *testing.T) {
	e := nuevoEntorno(t, false)
	ana := e.persona([]int64{1}, "tecnico")
	beto := e.persona([]int64{1}, "tecnico")
	e.dispositivo(beto) // se registra igual
	conv := e.directa(ana, beto)
	e.mensaje(ana, conv, map[string]any{"texto": "hola"})
	if n := e.procesar(); n != 0 {
		t.Fatalf("sin FCM no se crean envíos: %d", n)
	}
	if n := e.contar(`SELECT count(*) FROM notificaciones.envios WHERE usuario_id = $1`, beto.id); n != 0 {
		t.Fatalf("envíos: %d", n)
	}
	e.preferencias(beto, func(c map[string]any) { c["contenido"] = "remitente" })
	r := e.esperar(e.api(persona{}, http.MethodGet, "/api/v1/salud", nil), http.StatusOK)
	if r.cuerpo["notificacionesPush"] != "sin_configurar" {
		t.Fatalf("salud: %s", r.crudo)
	}

	// Estados de la configuración por entorno.
	t.Setenv("IRIS_FCM_PROYECTO", "")
	t.Setenv("IRIS_FCM_CREDENCIALES", "")
	if f, est := notificaciones.ConfigDesdeEntorno(); f != nil || est.Estado != notificaciones.PushSinConfigurar {
		t.Fatalf("sin variables: %+v", est)
	}
	t.Setenv("IRIS_FCM_PROYECTO", proyecto)
	if f, est := notificaciones.ConfigDesdeEntorno(); f != nil || est.Estado != notificaciones.PushError {
		t.Fatalf("sin credenciales: %+v", est)
	}
	dir := t.TempDir()
	malo := filepath.Join(dir, "malo.json")
	_ = os.WriteFile(malo, []byte(`{"type":"authorized_user"}`), 0o600)
	t.Setenv("IRIS_FCM_CREDENCIALES", malo)
	if f, est := notificaciones.ConfigDesdeEntorno(); f != nil || est.Estado != notificaciones.PushError {
		t.Fatalf("JSON que no es de cuenta de servicio: %+v", est)
	}
	t.Setenv("IRIS_FCM_CREDENCIALES", filepath.Join(dir, "no-existe.json"))
	if _, est := notificaciones.ConfigDesdeEntorno(); est.Estado != notificaciones.PushError || strings.Contains(est.Detalle, dir) {
		t.Fatalf("archivo que no existe: %+v", est)
	}
	_, pemClave := clave(t)
	bueno := filepath.Join(dir, "cuenta.json")
	b, _ := json.Marshal(map[string]string{"type": "service_account", "project_id": proyecto, "private_key_id": "k1", "private_key": pemClave,
		"client_email": "push@" + proyecto + ".iam.gserviceaccount.com", "token_uri": "https://oauth2.googleapis.com/token"})
	_ = os.WriteFile(bueno, b, 0o600)
	t.Setenv("IRIS_FCM_CREDENCIALES", bueno)
	if f, est := notificaciones.ConfigDesdeEntorno(); f == nil || est.Estado != notificaciones.PushConfigurado || est.Detalle != "" {
		t.Fatalf("configurado: %+v", est)
	}
	// La cuenta de otro proyecto: error y sin cliente (FCM respondería SENDER_ID_MISMATCH a todos).
	t.Setenv("IRIS_FCM_PROYECTO", "otro-proyecto")
	if f, est := notificaciones.ConfigDesdeEntorno(); f != nil || est.Estado != notificaciones.PushError {
		t.Fatalf("otro proyecto: %+v", est)
	}
	// Un token_uri ajeno se rechaza: el JWT firmado solo va a Google.
	ajeno := filepath.Join(dir, "ajeno.json")
	b, _ = json.Marshal(map[string]string{"type": "service_account", "project_id": proyecto, "private_key": pemClave,
		"client_email": "push@" + proyecto + ".iam.gserviceaccount.com", "token_uri": "http://169.254.169.254/token"})
	_ = os.WriteFile(ajeno, b, 0o600)
	t.Setenv("IRIS_FCM_PROYECTO", proyecto)
	t.Setenv("IRIS_FCM_CREDENCIALES", ajeno)
	if f, est := notificaciones.ConfigDesdeEntorno(); f != nil || est.Estado != notificaciones.PushError || !strings.Contains(est.Detalle, "token_uri") {
		t.Fatalf("token_uri ajeno: %+v", est)
	}
	if _, k, err := notificaciones.ParsearCuentaServicio(b); err == nil || k != nil {
		t.Fatal("ParsearCuentaServicio acepta un token_uri ajeno")
	}
	google := strings.Replace(string(b), `"http://169.254.169.254/token"`, `"`+notificaciones.URLToken+`"`, 1)
	if c, k, err := notificaciones.ParsearCuentaServicio([]byte(google)); err != nil || k == nil || c.ClavePrivada != "" {
		t.Fatalf("con el token_uri de Google se acepta y la clave en texto no se guarda: %v", err)
	}
}

func TestTopeDeDispositivosPorUsuario(t *testing.T) {
	e := nuevoEntorno(t, true)
	ana := e.persona([]int64{1}, "tecnico")
	primero := e.dispositivo(ana)
	for range notificaciones.MaxDispositivos {
		e.dispositivo(ana)
	}
	if n := e.contar(`SELECT count(*) FROM notificaciones.dispositivos WHERE usuario_id = $1`, ana.id); n != notificaciones.MaxDispositivos {
		t.Fatalf("dispositivos: %d", n)
	}
	h := sha256.Sum256([]byte(primero))
	if n := e.contar(`SELECT count(*) FROM notificaciones.dispositivos WHERE token_hash = $1`, h[:]); n != 0 {
		t.Fatal("se quita el más viejo")
	}
	if p := e.ultimoEvento("dispositivo.quitado"); p["motivo"] != notificaciones.QuitadoUsuario || int64(p["usuario_id"].(float64)) != ana.id {
		t.Fatalf("dispositivo.quitado: %v", p)
	}
}

func TestRedireccionNoSeSigue(t *testing.T) {
	destino := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("el cliente siguió la redirección")
	}))
	defer destino.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destino.URL+"/otro", http.StatusTemporaryRedirect)
	}))
	defer srv.Close()
	k, _ := clave(t)
	c := notificaciones.NuevoClienteFCM(notificaciones.OpcionesFCM{Proyecto: proyecto, URL: srv.URL, URLToken: srv.URL + "/token", Clave: k,
		Cuenta: notificaciones.CuentaServicio{Tipo: "service_account", Correo: "x@y.iam.gserviceaccount.com"}})
	if _, err := c.Enviar(context.Background(), notificaciones.MensajeFCM{Token: "t"}); err == nil {
		t.Fatal("una redirección del servidor de tokens debe fallar")
	}
}
