package mensajeria_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // verificar la credencial efímera de TURN (HMAC-SHA1 por estándar)
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/emiago/sipgo/sip"

	"github.com/HCDestroyer/iris/apps/api/internal/campo"
	"github.com/HCDestroyer/iris/apps/api/internal/mensajeria"
	"github.com/HCDestroyer/iris/apps/api/internal/mensajeria/sippuente/sipfalso"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
)

// El teléfono de Iris, E4c (contratos/voz.md §3.12): la administración, mi teléfono, llamar y el puente
// de punta a punta contra Postgres real y un Asterisk falso (no hay Issabel en CI).

const (
	secretoSIP  = "clave-SIP-larga-201"
	dominioSIP  = "pbx.oficina"
	secretoTURN = "secreto-de-coturn-0123456789"
	baseIris    = "https://iris.prueba"
)

type bancoTelefono struct {
	*bancoCentralita
	sip     *sipfalso.Servidor
	srv     *httptest.Server
	agente2 persona
	extID   float64
}

func nuevoBancoTelefono(t *testing.T) *bancoTelefono {
	t.Helper()
	falso := sipfalso.Nuevo("201", secretoSIP)
	t.Cleanup(falso.Cerrar)
	x := nuevoBancoCentralita(t, func(c *mensajeria.Config) {
		c.CentralitaPuertos = append(c.CentralitaPuertos, falso.Puerto())
		c.TURN = mensajeria.TURNEntorno{URLs: []string{"turn:iris.prueba:3478?transport=udp", "turns:iris.prueba:5349?transport=tcp"}, Secreto: secretoTURN}
		c.TelefonoLatido = 150 * time.Millisecond
		c.BaseURL = baseIris
		c.Campo = campo.Nuevo(campo.Config{Reloj: c.Ahora, Soporte: c.Soporte, Adjuntos: campo.SinAdjuntosObligatorios{}})
	})
	b := &bancoTelefono{bancoCentralita: x, sip: falso}
	b.srv = httptest.NewServer(x.h)
	t.Cleanup(b.srv.Close)
	x.conectar()
	b.configurar()
	b.extID = x.asignar(x.agente, "201")
	b.agente2 = x.persona([]int64{1}, rolBase("soporte"))
	x.asignar(b.agente2, "205")
	// Sin contraseña no hay navegador.
	x.exigirProblema(x.api(x.admin, http.MethodPost, b.rutaExt("/telefono"), map[string]any{"tipo": "navegador"}), http.StatusUnprocessableEntity, "clave_requerida")
	r := x.api(x.admin, http.MethodPost, b.rutaExt("/telefono"), map[string]any{"tipo": "navegador", "sipSecreto": secretoSIP})
	x.exigir(r, http.StatusOK)
	if r.cuerpo["sipGuardadoEn"] == nil || r.cuerpo["tipo"] != "navegador" || r.cuerpo["sipUsuario"] != "201" || strings.Contains(fmt.Sprint(r.cuerpo), secretoSIP) {
		t.Fatalf("extensión: %v", r.cuerpo)
	}
	return b
}

func (b *bancoTelefono) rutaExt(resto string) string {
	return b.ruta(fmt.Sprintf("/extensiones/%d%s", int64(b.extID), resto))
}

// configurar hace el camino del administrador: probar sin guardar, guardar con la huella, probar los
// guardados y encender.
func (b *bancoTelefono) configurar() {
	b.t.Helper()
	r := b.api(b.admin, http.MethodPost, b.ruta("/telefono/probar"), map[string]any{"wssPuerto": b.sip.Puerto(), "wssRuta": "/ws"})
	b.exigir(r, http.StatusOK)
	if r.cuerpo["estado"] != "ok" || r.cuerpo["huella"] != b.sip.Huella() || r.cuerpo["centralita"] != nil {
		b.t.Fatalf("probar: %v", r.cuerpo)
	}
	r = b.api(b.admin, http.MethodPost, b.ruta("/telefono"), map[string]any{"version": b.version, "wssPuerto": b.sip.Puerto(), "wssRuta": "/ws",
		"huellaWss": b.sip.Huella(), "sipDominio": dominioSIP, "prefijoSalida": "9", "llamadasPorUsuarioDia": 60, "llamadasSimultaneas": 10})
	b.exigir(r, http.StatusOK)
	b.version = r.cuerpo["version"].(float64)
	b.exigirProblema(b.api(b.admin, http.MethodPost, b.ruta("/telefono/activar"), map[string]any{"version": b.version}), http.StatusUnprocessableEntity, "sin_probar")
	r = b.api(b.admin, http.MethodPost, b.ruta("/telefono/probar"), map[string]any{})
	b.exigir(r, http.StatusOK)
	c, _ := r.cuerpo["centralita"].(map[string]any)
	if r.cuerpo["estado"] != "ok" || c == nil || c["telefono"].(map[string]any)["probadoEn"] == nil {
		b.t.Fatalf("probar guardados: %v", r.cuerpo)
	}
	b.version = c["version"].(float64)
	r = b.api(b.admin, http.MethodPost, b.ruta("/telefono/activar"), map[string]any{"version": b.version})
	b.exigir(r, http.StatusOK)
	b.version = r.cuerpo["version"].(float64)
	if r.cuerpo["telefono"].(map[string]any)["activo"] != true {
		b.t.Fatalf("activar: %v", r.cuerpo)
	}
}

func (b *bancoTelefono) abrir(p persona, dispositivo string) string {
	b.t.Helper()
	r := b.api(p, http.MethodPost, "/telefono/abrir", map[string]any{"dispositivo": dispositivo})
	b.exigir(r, http.StatusCreated)
	return r.cuerpo["ws"].(string)
}

type telefonoWS struct {
	t *testing.T
	c *websocket.Conn
}

func (b *bancoTelefono) dial(p persona, ruta string, origen string) (*telefonoWS, *http.Response, error) {
	h := http.Header{}
	h.Add("Cookie", identidad.CookieSesion+"="+p.cookie)
	if p.empresa != 0 {
		h.Set(identidad.CabeceraEmpresa, fmt.Sprint(p.empresa))
	}
	if origen != "" {
		h.Set("Origin", origen)
	}
	ctx, cancelar := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelar()
	c, resp, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(b.srv.URL, "http")+ruta, &websocket.DialOptions{Subprotocols: []string{"sip"}, HTTPHeader: h})
	if err != nil {
		return nil, resp, err
	}
	w := &telefonoWS{t: b.t, c: c}
	b.t.Cleanup(func() { _ = c.CloseNow() })
	return w, resp, nil
}

func (w *telefonoWS) mandar(s string) {
	w.t.Helper()
	ctx, cancelar := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelar()
	if err := w.c.Write(ctx, websocket.MessageText, []byte(strings.ReplaceAll(strings.TrimLeft(s, "\n"), "\n", "\r\n"))); err != nil {
		w.t.Fatal(err)
	}
}

// respuesta espera la respuesta final con ese código (las provisionales se saltan; un desafío falla).
func (w *telefonoWS) respuesta(codigo int) *sip.Response {
	w.t.Helper()
	for {
		ctx, cancelar := context.WithTimeout(context.Background(), 5*time.Second)
		_, b, err := w.c.Read(ctx)
		cancelar()
		if err != nil {
			w.t.Fatalf("esperaba %d: %v", codigo, err)
		}
		if strings.Contains(strings.ToLower(string(b)), "authenticate") {
			w.t.Fatalf("un desafío llegó al navegador:\n%s", b)
		}
		m, err := sip.ParseMessage(b)
		if err != nil {
			w.t.Fatal(err)
		}
		r, ok := m.(*sip.Response)
		if !ok {
			continue
		}
		if r.StatusCode == codigo {
			return r
		}
		if r.StatusCode >= 200 {
			w.t.Fatalf("esperaba %d, llegó:\n%s", codigo, b)
		}
	}
}

// cierre espera que Iris cierre el WebSocket y devuelve el código.
func (w *telefonoWS) cierre() websocket.StatusCode {
	w.t.Helper()
	for {
		ctx, cancelar := context.WithTimeout(context.Background(), 5*time.Second)
		_, _, err := w.c.Read(ctx)
		cancelar()
		if err != nil {
			var ce websocket.CloseError
			if errors.As(err, &ce) {
				return ce.Code
			}
			w.t.Fatalf("el WebSocket no se cerró con un código: %v", err)
		}
	}
}

func sipInvite(uri, rama string, seq int) string {
	return fmt.Sprintf(`
INVITE %[1]s SIP/2.0
Via: SIP/2.0/WSS abc.invalid;branch=z9hG4bK%[2]s
Max-Forwards: 69
To: <%[1]s>
From: "Ana" <sip:201@%[3]s>;tag=nav-%[2]s
Call-ID: call-%[2]s
CSeq: %[4]d INVITE
Contact: <sip:x7@abc.invalid;transport=ws;ob>
Content-Type: application/sdp
Content-Length: 4

v=0
`, uri, rama, dominioSIP, seq)
}

func sipBye(rama, totag string, seq int) string {
	return fmt.Sprintf("\nBYE sip:asterisk@127.0.0.1;transport=ws SIP/2.0\nVia: SIP/2.0/WSS abc.invalid;branch=z9hG4bKb%[1]s\nMax-Forwards: 69\nTo: <sip:x@%[2]s>;tag=%[3]s\n"+
		"From: <sip:201@%[2]s>;tag=nav-%[1]s\nCall-ID: call-%[1]s\nCSeq: %[4]d BYE\nContent-Length: 0\n\n", rama, dominioSIP, totag, seq)
}

func (b *bancoTelefono) estadoLlamada(id string) string {
	var e string
	if err := b.bd.QueryRow(b.ctx, `SELECT estado FROM mensajeria.llamadas WHERE id = $1`, id).Scan(&e); err != nil {
		b.t.Fatal(err)
	}
	return e
}

func TestTelefonoAdministracion(t *testing.T) {
	b := nuevoBancoTelefono(t)
	// La contraseña SIP queda cifrada y nunca aparece en claro ni en la auditoría.
	var cif []byte
	if err := b.bd.QueryRow(b.ctx, `SELECT sip_secreto_cifrado FROM mensajeria.centralita_extensiones WHERE id = $1`, int64(b.extID)).Scan(&cif); err != nil {
		t.Fatal(err)
	}
	if len(cif) == 0 || strings.Contains(string(cif), secretoSIP) {
		t.Fatal("la contraseña SIP no está cifrada")
	}
	var n int
	if err := b.bd.QueryRow(b.ctx, `SELECT count(*) FROM auditoria.acciones WHERE entrada::text LIKE '%' || $1 || '%' OR resultado::text LIKE '%' || $1 || '%'`,
		secretoSIP).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("la contraseña SIP quedó en la auditoría")
	}
	var nOut int
	if err := b.bd.QueryRow(b.ctx, `SELECT count(*) FROM integracion.outbox WHERE payload::text LIKE '%' || $1 || '%'`, secretoSIP).Scan(&nOut); err != nil || nOut != 0 {
		t.Fatalf("la contraseña SIP quedó en el outbox (%d, %v)", nOut, err)
	}
	// Probar registro: ok con la buena; con otra, «credenciales».
	r := b.api(b.admin, http.MethodPost, b.rutaExt("/probar-registro"), map[string]any{})
	b.exigir(r, http.StatusOK)
	if r.cuerpo["estado"] != "ok" || r.cuerpo["extension"].(map[string]any)["sipProbadoEn"] == nil {
		t.Fatalf("probar registro: %v", r.cuerpo)
	}
	b.exigir(b.api(b.admin, http.MethodPost, b.rutaExt("/telefono"), map[string]any{"tipo": "navegador", "sipSecreto": "otra-clave-mala"}), http.StatusOK)
	r = b.api(b.admin, http.MethodPost, b.rutaExt("/probar-registro"), map[string]any{})
	b.exigir(r, http.StatusOK)
	if r.cuerpo["estado"] != "credenciales" {
		t.Fatalf("registro con la clave mala: %v", r.cuerpo)
	}
	// Cambiar el usuario SIP exige volver a escribir la contraseña.
	b.exigirProblema(b.api(b.admin, http.MethodPost, b.rutaExt("/telefono"), map[string]any{"tipo": "navegador", "sipUsuario": "otro201"}),
		http.StatusUnprocessableEntity, "clave_requerida")
	// Grupos.
	r = b.api(b.admin, http.MethodPost, b.ruta("/grupos"), map[string]any{"nombre": "Soporte", "numero": "600"})
	b.exigir(r, http.StatusOK)
	b.exigirProblema(b.api(b.admin, http.MethodPost, b.ruta("/grupos"), map[string]any{"nombre": "Otro", "numero": "600"}), http.StatusConflict, "grupo_existente")
	r = b.api(b.admin, http.MethodPost, b.ruta(fmt.Sprintf("/grupos/%d/actualizar", int64(r.cuerpo["id"].(float64)))),
		map[string]any{"version": r.cuerpo["version"], "nombre": "Soporte técnico", "numero": "600", "activo": true})
	b.exigir(r, http.StatusOK)
	// Un supervisor (asigna extensiones) no configura el teléfono.
	b.exigir(b.api(b.supervisor, http.MethodPost, b.ruta("/telefono/desactivar"), map[string]any{"version": b.version}), http.StatusForbidden)
	// Cambiar la huella apaga el teléfono y borra la prueba.
	r = b.api(b.admin, http.MethodPost, b.ruta("/telefono"), map[string]any{"version": b.version, "wssPuerto": b.sip.Puerto(), "wssRuta": "/ws",
		"huellaWss": strings.Repeat("a", 64), "sipDominio": dominioSIP, "prefijoSalida": "9", "llamadasPorUsuarioDia": 60, "llamadasSimultaneas": 10})
	b.exigir(r, http.StatusOK)
	tel := r.cuerpo["telefono"].(map[string]any)
	if tel["activo"] != false || tel["probadoEn"] != nil {
		t.Fatalf("cambiar la huella: %v", tel)
	}
	r = b.api(b.admin, http.MethodPost, b.ruta("/telefono/probar"), map[string]any{})
	b.exigir(r, http.StatusOK)
	if r.cuerpo["estado"] != "huella_distinta" {
		t.Fatalf("huella distinta: %v", r.cuerpo)
	}
	// Puerto no permitido.
	b.exigirProblema(b.api(b.admin, http.MethodPost, b.ruta("/telefono/probar"), map[string]any{"wssPuerto": 5060}), http.StatusUnprocessableEntity, "puerto_no_permitido")
}

func TestTelefonoMioYAbrir(t *testing.T) {
	b := nuevoBancoTelefono(t)
	r := b.api(b.agente, http.MethodGet, "/telefono", nil)
	b.exigir(r, http.StatusOK)
	if r.cuerpo["disponible"] != true || r.cuerpo["extension"] != "201" || r.cuerpo["puede"].(map[string]any)["llamar"] != true ||
		r.cuerpo["puede"].(map[string]any)["transferir"] != true {
		t.Fatalf("mi teléfono: %v", r.cuerpo)
	}
	r = b.api(b.agente2, http.MethodGet, "/telefono", nil)
	b.exigir(r, http.StatusOK)
	if r.cuerpo["disponible"] != false || r.cuerpo["motivo"] != "extension_escritorio" {
		t.Fatalf("escritorio: %v", r.cuerpo)
	}
	r = b.api(b.otro, http.MethodGet, "/telefono", nil)
	b.exigir(r, http.StatusOK)
	if r.cuerpo["motivo"] != "sin_extension" {
		t.Fatalf("sin extensión: %v", r.cuerpo)
	}
	b.exigirProblema(b.api(b.otro, http.MethodPost, "/telefono/abrir", map[string]any{"dispositivo": "panel"}), http.StatusUnprocessableEntity, "sin_extension")
	// Un token de API no tiene teléfono.
	sinCookie := b.agente
	sinCookie.cookie = ""
	b.exigir(b.api(sinCookie, http.MethodGet, "/telefono", nil), http.StatusNotFound)
	r = b.api(b.agente, http.MethodPost, "/telefono/abrir", map[string]any{"dispositivo": "panel"})
	b.exigir(r, http.StatusCreated)
	if strings.Contains(fmt.Sprint(r.cuerpo), secretoSIP) || strings.Contains(fmt.Sprint(r.cuerpo), secretoTURN) {
		t.Fatal("abrir devuelve un secreto")
	}
	sipm := r.cuerpo["sip"].(map[string]any)
	if sipm["uri"] != "sip:201@"+dominioSIP || r.cuerpo["politicaIce"] != "relay" || !strings.HasPrefix(r.cuerpo["ws"].(string), mensajeria.RutaTelefonoSIP+"?ticket=") {
		t.Fatalf("abrir: %v", r.cuerpo)
	}
	ice := r.cuerpo["ice"].([]any)[0].(map[string]any)
	usuario := ice["username"].(string)
	h := hmac.New(sha1.New, []byte(secretoTURN))
	h.Write([]byte(usuario))
	if !strings.HasSuffix(usuario, fmt.Sprintf(":iris-%d", b.agente.id)) || ice["credential"] != base64.StdEncoding.EncodeToString(h.Sum(nil)) {
		t.Fatalf("TURN: %v", ice)
	}
	// El ticket es de un solo uso, de esta sesión y con Origin del panel.
	ws := r.cuerpo["ws"].(string)
	if _, resp, err := b.dial(b.agente, ws, "https://otro.sitio"); err == nil || resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("Origin ajeno: %v", err)
	}
	otraSesion := b.sesion(b.agente)
	if _, resp, err := b.dial(otraSesion, b.abrir(b.agente, "panel"), baseIris); err == nil || resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("ticket de otra sesión: %v", err)
	}
	ws = b.abrir(b.agente, "panel")
	if _, _, err := b.dial(b.agente, ws, baseIris); err != nil {
		t.Fatal(err)
	}
	if _, resp, err := b.dial(b.agente, ws, baseIris); err == nil || resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("ticket reusado: %v", err)
	}
}

func TestTelefonoPuenteDePuntaAPunta(t *testing.T) {
	b := nuevoBancoTelefono(t)
	w, _, err := b.dial(b.agente, b.abrir(b.agente, "panel"), baseIris)
	if err != nil {
		t.Fatal(err)
	}
	// Registrarse: el desafío lo responde Iris.
	w.mandar(fmt.Sprintf("\nREGISTER sip:%[1]s SIP/2.0\nVia: SIP/2.0/WSS abc.invalid;branch=z9hG4bKreg1\nMax-Forwards: 69\nTo: <sip:201@%[1]s>\n"+
		"From: <sip:201@%[1]s>;tag=r1\nCall-ID: reg-1\nCSeq: 1 REGISTER\nContact: <sip:x7@abc.invalid;transport=ws>\nExpires: 600\nContent-Length: 0\n\n", dominioSIP))
	w.respuesta(200)
	// Un número libre no llega a la Issabel.
	w.mandar(sipInvite("sip:+15551234567@"+dominioSIP, "libre", 1))
	w.respuesta(403)
	// Llamar por el teclado: el servidor da el destino; el puente marca 9 + 8 dígitos.
	r := b.api(b.agente, http.MethodPost, "/telefono/llamar", map[string]any{"numero": "5555-1234"})
	b.exigir(r, http.StatusCreated)
	destino := r.cuerpo["destino"].(string)
	l := r.cuerpo["llamada"].(map[string]any)
	id := l["id"].(string)
	if !strings.HasPrefix(destino, "sip:iris-") || l["estado"] != "marcando" || l["origen"] != "centralita" {
		t.Fatalf("llamar: %v", r.cuerpo)
	}
	w.mandar(sipInvite(destino, "ext1", 5))
	ok := w.respuesta(200)
	totag, _ := ok.To().Params.Get("tag")
	if _, encontrado := b.sip.Esperar(2*time.Second, func(m string) bool {
		return strings.HasPrefix(m, "INVITE sip:955551234@"+dominioSIP+" ") && strings.Contains(m, "Authorization: Digest")
	}); !encontrado {
		t.Fatalf("el INVITE no llegó reescrito: %v", b.sip.Recibidos())
	}
	b.hasta(func() bool { return b.estadoLlamada(id) == "en_curso" }, "la llamada no quedó en curso")
	// El mismo destino no se puede volver a usar.
	w.mandar(sipInvite(destino, "ext2", 1))
	w.respuesta(403)
	w.mandar(sipBye("ext1", totag, 6))
	w.respuesta(200)
	b.hasta(func() bool { return b.estadoLlamada(id) == "terminada" }, "la llamada no terminó")
	if p := b.outbox("llamada.terminada", id); p["origen"] != "centralita" {
		t.Fatalf("llamada.terminada: %v", p)
	}
	// Una interna a un compañero, y quitarle la extensión en medio: BYE a la Issabel y cierre 4003.
	r = b.api(b.agente, http.MethodPost, "/telefono/llamar", map[string]any{"usuarioId": b.agente2.id})
	b.exigir(r, http.StatusCreated)
	if r.cuerpo["destino"] != "sip:205@"+dominioSIP || r.cuerpo["llamada"] != nil {
		t.Fatalf("interna: %v", r.cuerpo)
	}
	w.mandar(sipInvite("sip:205@"+dominioSIP, "int1", 1))
	w.respuesta(200)
	b.exigir(b.api(b.admin, http.MethodPost, b.rutaExt("/quitar"), nil), http.StatusOK)
	if c := w.cierre(); c != 4003 {
		t.Fatalf("cierre al quitar la extensión: %d", c)
	}
	if _, encontrado := b.sip.Esperar(3*time.Second, func(m string) bool { return strings.HasPrefix(m, "BYE") && strings.Contains(m, "call-int1") }); !encontrado {
		t.Fatalf("no llegó el BYE: %v", b.sip.Recibidos())
	}
}

func TestTelefonoReemplazoYUsuarioDesactivado(t *testing.T) {
	b := nuevoBancoTelefono(t)
	w1, _, err := b.dial(b.agente, b.abrir(b.agente, "panel"), baseIris)
	if err != nil {
		t.Fatal(err)
	}
	// Otra pestaña toma el teléfono: la primera se cierra con 4001.
	w2, _, err := b.dial(b.agente, b.abrir(b.agente, "panel"), baseIris)
	if err != nil {
		t.Fatal(err)
	}
	if c := w1.cierre(); c != 4001 {
		t.Fatalf("reemplazada: %d", c)
	}
	// Desactivar a la persona corta su teléfono.
	b.sql(`UPDATE nucleo.usuarios SET activo = false WHERE id = $1`, b.agente.id)
	if c := w2.cierre(); c != 4003 {
		t.Fatalf("usuario desactivado: %d", c)
	}
}

func TestTelefonoAppSoloMarca(t *testing.T) {
	b := nuevoBancoTelefono(t)
	w, _, err := b.dial(b.agente, b.abrir(b.agente, "app"), "")
	if err != nil {
		t.Fatal(err)
	}
	w.mandar(fmt.Sprintf("\nREGISTER sip:%[1]s SIP/2.0\nVia: SIP/2.0/WSS abc.invalid;branch=z9hG4bKreg1\nMax-Forwards: 69\nTo: <sip:201@%[1]s>\n"+
		"From: <sip:201@%[1]s>;tag=r1\nCall-ID: reg-1\nCSeq: 1 REGISTER\nContact: <sip:x7@abc.invalid;transport=ws>\nContent-Length: 0\n\n", dominioSIP))
	w.respuesta(403)
}

func TestTelefonoLlamarReglas(t *testing.T) {
	b := nuevoBancoTelefono(t)
	for _, n := range []string{"+15551234567", "1234", "90012345", "+50288881234", "502123", "5555-12a4"} {
		b.exigirProblema(b.api(b.agente, http.MethodPost, "/telefono/llamar", map[string]any{"numero": n}), http.StatusUnprocessableEntity, "destino_no_permitido")
	}
	// Su propia extensión no.
	b.exigirProblema(b.api(b.agente, http.MethodPost, "/telefono/llamar", map[string]any{"numero": "201"}), http.StatusUnprocessableEntity, "destino_no_permitido")
	// Dos a la vez no.
	b.exigir(b.api(b.agente, http.MethodPost, "/telefono/llamar", map[string]any{"numero": "205", "usuarioId": b.agente2.id}), http.StatusUnprocessableEntity)
	// Un cliente: el número lo pone el servidor (móvil de la instantánea) y queda enlazado.
	cliente := b.clienteConMovil("44445555", "Cliente Uno")
	r := b.api(b.agente, http.MethodPost, "/telefono/llamar", map[string]any{"clienteId": cliente})
	b.exigir(r, http.StatusCreated)
	l := r.cuerpo["llamada"].(map[string]any)
	if l["numero"] != "+50244445555" || l["contacto"].(map[string]any)["clienteId"] != float64(cliente) {
		t.Fatalf("cliente: %v", l)
	}
	b.exigirProblema(b.api(b.agente, http.MethodPost, "/telefono/llamar", map[string]any{"clienteId": cliente, "telefono": "principal"}),
		http.StatusUnprocessableEntity, "sin_contacto")
	b.exigir(b.api(b.agente, http.MethodPost, "/telefono/llamar", map[string]any{"clienteId": 99999999}), http.StatusNotFound)
	// La auditoría nunca guarda el número del teclado.
	b.exigir(b.api(b.agente, http.MethodPost, "/telefono/llamar", map[string]any{"numero": "33334444"}), http.StatusCreated)
	var n int
	if err := b.bd.QueryRow(b.ctx, `SELECT count(*) FROM auditoria.acciones WHERE accion = 'telefono.llamar' AND entrada::text LIKE '%33334444%'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("el número quedó en la auditoría (%d, %v)", n, err)
	}
	// Límite del día y simultáneas.
	b.sql(`UPDATE mensajeria.centralitas SET llamadas_simultaneas = 2 WHERE id = $1`, b.id)
	b.exigirProblema(b.api(b.agente, http.MethodPost, "/telefono/llamar", map[string]any{"numero": "33335555"}), http.StatusConflict, "llamadas_simultaneas")
	b.sql(`UPDATE mensajeria.centralitas SET llamadas_simultaneas = 100, llamadas_por_usuario_dia = 2 WHERE id = $1`, b.id)
	b.exigirProblema(b.api(b.agente, http.MethodPost, "/telefono/llamar", map[string]any{"numero": "33335555"}), http.StatusUnprocessableEntity, "limite_llamadas")
	// Las marcaciones sin INVITE en 60 s quedan fallidas (y siguen contando para el día).
	*b.reloj = b.reloj.Add(2 * time.Minute)
	b.sql(`UPDATE mensajeria.centralitas SET llamadas_por_usuario_dia = 300 WHERE id = $1`, b.id)
	b.exigir(b.api(b.agente, http.MethodPost, "/telefono/llamar", map[string]any{"numero": "33335555"}), http.StatusCreated)
	var fallidas int
	if err := b.bd.QueryRow(b.ctx, `SELECT count(*) FROM mensajeria.llamadas WHERE puente AND usuario_id = $1 AND estado = 'fallida' AND motivo_fin = 'error_medio'`,
		b.agente.id).Scan(&fallidas); err != nil || fallidas != 2 {
		t.Fatalf("marcaciones vencidas: %d %v", fallidas, err)
	}
	// El técnico no transfiere; sin centralita.llamar no se llama.
	b.exigir(b.api(b.tecnico, http.MethodGet, "/telefono/transferir/destinos", nil), http.StatusForbidden)
	b.exigir(b.api(b.lectura, http.MethodPost, "/telefono/llamar", map[string]any{"numero": "33335555"}), http.StatusForbidden)
	r = b.api(b.agente, http.MethodGet, "/telefono/transferir/destinos", nil)
	b.exigir(r, http.StatusOK)
	if ps := r.cuerpo["personas"].([]any); len(ps) != 1 || ps[0].(map[string]any)["extension"] != "205" {
		t.Fatalf("destinos: %v", r.cuerpo)
	}
}

func TestTelefonoOrdenYLlamadaDirecta(t *testing.T) {
	b := nuevoBancoTelefono(t)
	q := b.equipo(1)
	b.ficha(b.tecnico.id, q.cuadrilla1)
	cliente := b.clienteConMovil("44446666", "Cliente Orden")
	orden := b.orden(1, q.zona1, q.cuadrilla1, b.tecnico.id)
	b.sql(`UPDATE campo.ordenes SET cliente_id = $2, contacto_telefono = '5555 7777' WHERE id = $1`, orden, cliente)
	// Sin extensión, solo la directa: deja la llamada en la orden y da el número para tel:.
	b.exigirProblema(b.api(b.tecnico, http.MethodPost, "/telefono/llamar", map[string]any{"ordenId": orden}), http.StatusUnprocessableEntity, "sin_extension")
	r := b.api(b.tecnico, http.MethodPost, "/telefono/llamada-directa", map[string]any{"ordenId": orden})
	b.exigir(r, http.StatusCreated)
	l := r.cuerpo["llamada"].(map[string]any)
	if r.cuerpo["telefono"] != "+50255557777" || l["origen"] != "celular" || l["estado"] != "terminada" || l["motivoFin"] != "directa" ||
		l["ordenId"] != float64(orden) || l["duracionSeg"] != nil {
		t.Fatalf("directa: %v", r.cuerpo)
	}
	if p := b.outbox("llamada.directa_registrada", l["id"].(string)); p["orden_id"] != float64(orden) || strings.Contains(fmt.Sprint(p), "5555") {
		t.Fatalf("evento: %v", p)
	}
	// Una orden ajena no.
	otra := b.persona([]int64{1}, rolBase("tecnico"))
	b.exigir(b.api(otra, http.MethodPost, "/telefono/llamada-directa", map[string]any{"ordenId": orden}), http.StatusNotFound)
	// Con extensión, «Llamar por la empresa» marca el contacto de la orden y queda en la orden.
	b.asignar(b.tecnico, "230")
	r = b.api(b.admin, http.MethodPost, b.ruta("/extensiones"), nil)
	_ = r
	var ext int64
	if err := b.bd.QueryRow(b.ctx, `SELECT id FROM mensajeria.centralita_extensiones WHERE usuario_id = $1 AND activa`, b.tecnico.id).Scan(&ext); err != nil {
		t.Fatal(err)
	}
	b.exigir(b.api(b.admin, http.MethodPost, b.ruta(fmt.Sprintf("/extensiones/%d/telefono", ext)), map[string]any{"tipo": "navegador", "sipSecreto": "clave-del-tecnico"}),
		http.StatusOK)
	r = b.api(b.tecnico, http.MethodPost, "/telefono/llamar", map[string]any{"ordenId": orden})
	b.exigir(r, http.StatusCreated)
	if l := r.cuerpo["llamada"].(map[string]any); l["numero"] != "+50255557777" || l["ordenId"] != float64(orden) {
		t.Fatalf("llamar por la orden: %v", r.cuerpo)
	}
	r = b.api(b.tecnico, http.MethodGet, fmt.Sprintf("/centralitas/llamadas?ordenId=%d", orden), nil)
	b.exigir(r, http.StatusOK)
	if n := len(r.cuerpo["data"].([]any)); n != 2 {
		t.Fatalf("llamadas de la orden: %d %v", n, r.cuerpo)
	}
}
