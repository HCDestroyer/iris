package mensajeria_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/mensajeria"
)

// Voz E1 (contratos/voz.md): llamadas entrantes de WhatsApp contestadas en el panel, contra Postgres
// real. La Graph API de llamadas es un doble fiel a los supuestos de §10.1 (connect con oferta SDP;
// pre_accept, accept, reject y terminate por call_id; terminate con duration).

type graphFalso struct {
	mu       sync.Mutex
	acciones []string // "accion:call_id"
	sdps     []string
	fallar   map[string]error // por acción
	ajustes  mensajeria.AjustesMetaLlamadas
	fijados  []mensajeria.AjustesMetaLlamadas
	tokens   []string
	// alAccionar corre mientras «Meta» atiende la acción (fuera del candado): simula lo que pasa en el
	// panel entre la intención y el resultado.
	alAccionar func(accion string)
}

func (g *graphFalso) Accion(_ context.Context, canal mensajeria.CanalGraph, callID, accion, sdp string, _ bool) error {
	g.mu.Lock()
	g.acciones = append(g.acciones, accion+":"+callID)
	g.tokens = append(g.tokens, canal.Token)
	if sdp != "" {
		g.sdps = append(g.sdps, sdp)
	}
	err, gancho := g.fallar[accion], g.alAccionar
	g.mu.Unlock()
	if gancho != nil {
		gancho(accion)
	}
	return err
}

func (g *graphFalso) Ajustes(context.Context, mensajeria.CanalGraph) (mensajeria.AjustesMetaLlamadas, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.ajustes, g.fallar["ajustes"]
}

func (g *graphFalso) FijarAjustes(_ context.Context, _ mensajeria.CanalGraph, a mensajeria.AjustesMetaLlamadas) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.fallar["fijar"]; err != nil {
		return err
	}
	g.fijados = append(g.fijados, a)
	return nil
}

func (g *graphFalso) falla(accion string, err error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err == nil {
		delete(g.fallar, accion)
		return
	}
	g.fallar[accion] = err
}

func (g *graphFalso) ultimoFijado() mensajeria.AjustesMetaLlamadas {
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.fijados) == 0 {
		return mensajeria.AjustesMetaLlamadas{}
	}
	return g.fijados[len(g.fijados)-1]
}

func (g *graphFalso) hechas() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.acciones...)
}

func (g *graphFalso) cuenta(accion string) int {
	n := 0
	for _, a := range g.hechas() {
		if a == accion {
			n++
		}
	}
	return n
}

// sdpOfertaMeta: una oferta con la forma de la de Meta (ICE-lite, Opus, DTLS, una dirección pública).
// Chromium la acepta: con ella se generó testdata/respuesta_chromium.sdp.
var sdpOfertaMeta = leerSDP("oferta_meta.sdp")

// sdpChromium: la respuesta real de Chromium 153 (Playwright, micrófono concedido con
// --use-fake-ui-for-media-stream) a esa oferta, con la recolección ICE terminada. Sus candidatos host
// llevan la dirección de la red local de la máquina donde se generó.
var sdpChromium = leerSDP("respuesta_chromium.sdp")

func leerSDP(nombre string) string {
	b, err := os.ReadFile(filepath.Join("testdata", nombre))
	if err != nil {
		panic(err)
	}
	return string(b)
}

func sdpNavegador(extra string) string {
	return "v=0\r\no=- 1 2 IN IP4 127.0.0.1\r\ns=-\r\nt=0 0\r\nm=audio 9 UDP/TLS/RTP/SAVPF 111 126\r\nc=IN IP4 0.0.0.0\r\n" +
		"a=rtpmap:111 opus/48000/2\r\na=rtpmap:126 telephone-event/8000\r\na=setup:active\r\na=fingerprint:sha-256 0F:1E:2D\r\n" +
		"a=candidate:1 1 udp 2113937151 190.56.10.7 54400 typ srflx\r\n" + extra
}

type bandejaVoz struct {
	*bandeja
	g *graphFalso
}

// nuevaBandejaVoz: la bandeja con el doble de la Graph API y las llamadas encendidas en el número.
func nuevaBandejaVoz(t *testing.T) *bandejaVoz {
	t.Helper()
	g := &graphFalso{fallar: map[string]error{}, ajustes: mensajeria.AjustesMetaLlamadas{Habilitadas: true, IconoVisible: true, LimiteMensajeria: 10000}}
	x := &bandejaVoz{bandeja: nuevaBandeja(t, func(c *mensajeria.Config) { c.Llamadas = g }), g: g}
	// Cada persona en su navegador (el mismo id que usa recibe()).
	x.agente.navegador, x.supervisor.navegador = fmt.Sprintf("navegador-%d", x.agente.id), fmt.Sprintf("navegador-%d", x.supervisor.id)
	c := x.exigir(x.api(x.supervisor, http.MethodPost, x.rutaCanal("/llamadas/comprobar"), nil), http.StatusOK)
	x.exigir(x.configurar(int(c.cuerpo["version"].(float64)), nil), http.StatusOK)
	// El supervisor también atiende la cola (x.otro no: nunca es candidato).
	if _, err := x.bd.Exec(x.ctx, `INSERT INTO mensajeria.cola_miembros (cola_id, usuario_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, x.cola, x.supervisor.id); err != nil {
		t.Fatal(err)
	}
	return x
}

func crudo(r respuesta) string {
	b, _ := json.Marshal(r.cuerpo)
	return string(b)
}

// exigir comprueba el status y devuelve la respuesta.
func (x *bandejaVoz) exigir(r respuesta, status int) respuesta {
	x.t.Helper()
	x.bandeja.exigir(r, status)
	return r
}

func (x *bandejaVoz) rutaCanal(resto string) string {
	return fmt.Sprintf("/canales/%d%s", x.canal.ID, resto)
}

func (x *bandejaVoz) configurar(version int, cambios map[string]any) respuesta {
	c := map[string]any{"version": version, "llamadasActivas": true, "segundosSonando": 30, "fueraDeHorario": "rechazar_con_mensaje",
		"iconoVisible": true, "devolucion": false, "mensajePerdida": map[string]any{"activo": true, "texto": "No pudimos contestarte. Escríbenos por aquí."}}
	for k, v := range cambios {
		c[k] = v
	}
	return x.api(x.supervisor, http.MethodPost, x.rutaCanal("/llamadas/configurar"), c)
}

// recibe: la persona queda disponible, con «Recibir llamadas» encendido y su panel «abierto».
func (x *bandejaVoz) recibe(ps ...persona) {
	x.t.Helper()
	for _, p := range ps {
		x.disponible(p, "disponible")
		x.exigir(x.api(p, http.MethodPost, "/llamadas/recibir/encender", map[string]any{"navegador": fmt.Sprintf("navegador-%d", p.id)}), http.StatusOK)
	}
}

func (x *bandejaVoz) eventoLlamada(de, callID, evento, extra string, hace time.Duration) string {
	return fmt.Sprintf(`{"object":"whatsapp_business_account","entry":[{"changes":[{"field":"calls","value":{"metadata":{"phone_number_id":%q},`+
		`"contacts":[{"wa_id":%q,"profile":{"name":"Carlos R."}}],"calls":[{"id":%q,"from":%q,"to":"50222000010","event":%q,"timestamp":"%d",`+
		`"direction":"USER_INITIATED"%s}]}}]}]}`, x.pnid, de, callID, de, evento, x.reloj.Add(-hace).Unix(), extra)
}

func (x *bandejaVoz) connect(de, callID string) {
	x.t.Helper()
	x.webhook(x.eventoLlamada(de, callID, "connect", fmt.Sprintf(`,"session":{"sdp_type":"offer","sdp":%q}`, sdpOfertaMeta), 0))
}

// llamada: la llamada de ese call_id (id y estado), leída de la base.
func (x *bandejaVoz) llamada(callID string) (id, estado, motivo string) {
	x.t.Helper()
	if err := x.bd.QueryRow(x.ctx, `SELECT id::text, estado, coalesce(motivo_fin, '') FROM mensajeria.llamadas WHERE canal_id = $1 AND id_externo = $2`,
		x.canal.ID, callID).Scan(&id, &estado, &motivo); err != nil {
		x.t.Fatalf("llamada %s: %v", callID, err)
	}
	return id, estado, motivo
}

// mantener: un ciclo del mantenimiento y otro del job de escrituras a Meta.
func (x *bandejaVoz) mantener() {
	x.t.Helper()
	x.soloMantener()
	x.despachar()
}

func (x *bandejaVoz) soloMantener() {
	x.t.Helper()
	if err := x.m.MantenerLlamadas(x.ctx, x.bd); err != nil {
		x.t.Fatal(err)
	}
}

func (x *bandejaVoz) despachar() {
	x.t.Helper()
	if err := x.m.DespacharMetaVoz(x.ctx, x.bd); err != nil {
		x.t.Fatal(err)
	}
}

func (x *bandejaVoz) gancho(f func(accion string)) {
	x.g.mu.Lock()
	x.g.alAccionar = f
	x.g.mu.Unlock()
}

func (x *bandejaVoz) sql(sql string, args ...any) {
	x.t.Helper()
	if _, err := x.bd.Exec(x.ctx, sql, args...); err != nil {
		x.t.Fatal(err)
	}
}

func (x *bandejaVoz) pasa(d time.Duration, vivos ...persona) {
	*x.reloj = x.reloj.Add(d)
	for _, p := range vivos {
		x.exigir(x.api(p, http.MethodPost, "/llamadas/recibir/encender", map[string]any{"navegador": fmt.Sprintf("navegador-%d", p.id)}), http.StatusOK)
	}
}

func (x *bandejaVoz) eventos(nombre, llamada string) int {
	return x.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = $1 AND payload->>'llamada_id' = $2`, nombre, llamada)
}

// contestar lleva la llamada hasta en_curso como p.
func (x *bandejaVoz) contestar(p persona, id string) {
	x.t.Helper()
	x.exigir(x.api(p, http.MethodPost, "/llamadas/"+id+"/reclamar", nil), http.StatusOK)
	x.exigir(x.api(p, http.MethodPost, "/llamadas/"+id+"/contestar", map[string]any{"sdpRespuesta": sdpNavegador("")}), http.StatusOK)
	if r := x.exigir(x.api(p, http.MethodPost, "/llamadas/"+id+"/confirmar-medio", nil), http.StatusOK); r.cuerpo["estado"] != "en_curso" {
		x.t.Fatalf("confirmar-medio: %v", r.cuerpo)
	}
}

func TestLlamadaEntranteContestadaEnElPanel(t *testing.T) {
	x := nuevaBandejaVoz(t)
	x.recibe(x.agente, x.supervisor)
	num, call := numeroCliente(), "wacid.contestada"+numeroCliente()

	// Llega: el connect se procesa en la misma petición (antes de que corra el job).
	cuerpo := x.eventoLlamada(num, call, "connect", fmt.Sprintf(`,"session":{"sdp_type":"offer","sdp":%q}`, sdpOfertaMeta), 0)
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("%s/canales/%d/webhook", rutaMsj, x.canal.ID), strings.NewReader(cuerpo))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hub-Signature-256", mensajeria.FirmarMeta(secretoWA, []byte(cuerpo)))
	w := httptest.NewRecorder()
	x.h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("webhook: %d %s", w.Code, w.Body.String())
	}
	id, estado, _ := x.llamada(call)
	if estado != "sonando" || x.eventos("llamada.entrante", id) != 1 {
		t.Fatalf("tras el connect en línea: %s", estado)
	}
	// El job (o un reintento de Meta) no crea otra.
	x.connect(num, call)
	if n := x.contar(`SELECT count(*) FROM mensajeria.llamadas WHERE id_externo = $1`, call); n != 1 {
		t.Fatalf("llamadas con ese call_id: %d", n)
	}
	// Le suena al agente de la cola: la ve sin SDP, y en el hilo hay un mensaje de tipo llamada.
	r := x.exigir(x.api(x.agente, http.MethodGet, "/llamadas?estado=sonando", nil), http.StatusOK)
	if d := r.cuerpo["data"].([]any); len(d) != 1 || d[0].(map[string]any)["puede"].(map[string]any)["contestar"] != true || strings.Contains(crudo(r), "fingerprint") ||
		strings.Contains(crudo(r), call) {
		t.Fatalf("lista del agente: %s", crudo(r))
	}
	conv := cid(x.conv(num))
	ms := x.mensajes(x.agente, conv)
	ultimo := ms[len(ms)-1]
	if ultimo["tipo"] != "llamada" || ultimo["llamada"].(map[string]any)["estado"] != "sonando" || ultimo["texto"] != nil {
		t.Fatalf("mensaje de la llamada: %v", ultimo)
	}
	if n := x.contar(`SELECT count(*) FROM mensajeria.conv_eventos WHERE llamada_id = $1::uuid AND tipo = 'llamada_entrante' AND usuario_id = $2`, id, x.agente.id); n != 1 {
		t.Fatalf("timbre para el agente: %d", n)
	}

	// Reclamar: quien no es candidato de esa llamada, 404 aunque vea la conversación; el agente gana.
	x.exigir(x.api(x.otro, http.MethodPost, "/llamadas/"+id+"/reclamar", nil), http.StatusNotFound)
	x.exigir(x.api(x.lectura, http.MethodPost, "/llamadas/"+id+"/reclamar", nil), http.StatusForbidden)
	rec := x.exigir(x.apiClave(x.agente, http.MethodPost, "/llamadas/"+id+"/reclamar", "clave-reclamar-1", nil), http.StatusOK)
	if rec.cuerpo["sdpOferta"] != sdpOfertaMeta || rec.cuerpo["llamada"].(map[string]any)["estado"] != "conectando" {
		t.Fatalf("reclamada: %s", crudo(rec))
	}
	// El otro candidato pierde; el mismo agente en otra pestaña recibe la misma oferta (releída).
	if r := x.exigir(x.api(x.supervisor, http.MethodPost, "/llamadas/"+id+"/reclamar", nil), http.StatusConflict); !strings.Contains(crudo(r), "llamada_ya_contestada") {
		t.Fatalf("el que pierde: %s", crudo(r))
	}
	if r := x.exigir(x.apiClave(x.agente, http.MethodPost, "/llamadas/"+id+"/reclamar", "clave-reclamar-1", nil), http.StatusOK); r.cuerpo["sdpOferta"] != sdpOfertaMeta {
		t.Fatalf("repetir reclamar: %s", crudo(r))
	}
	// Solo quien la reclamó sigue: un supervisor recibe 404.
	x.exigir(x.api(x.supervisor, http.MethodPost, "/llamadas/"+id+"/contestar", map[string]any{"sdpRespuesta": sdpNavegador("")}), http.StatusNotFound)
	x.exigir(x.api(x.supervisor, http.MethodPost, "/llamadas/"+id+"/confirmar-medio", nil), http.StatusNotFound)

	// Contestar: el SDP se valida antes de mandarlo a Meta.
	for nombre, malo := range map[string]string{
		"con video":  sdpNavegador("m=video 9 UDP/TLS/RTP/SAVPF 96\r\na=rtpmap:96 VP8/90000\r\n"),
		"con datos":  sdpNavegador("m=application 9 UDP/DTLS/SCTP webrtc-datachannel\r\n"),
		"otro códec": strings.Replace(sdpNavegador(""), "opus/48000/2", "PCMU/8000", 1),
		"sin huella": strings.Replace(sdpNavegador(""), "a=fingerprint:sha-256 0F:1E:2D\r\n", "", 1),
		"enorme":     sdpNavegador(strings.Repeat("a=x:"+strings.Repeat("y", 90)+"\r\n", 90)),
	} {
		if r := x.api(x.agente, http.MethodPost, "/llamadas/"+id+"/contestar", map[string]any{"sdpRespuesta": malo}); r.status != http.StatusUnprocessableEntity {
			t.Fatalf("SDP %s: %d %s", nombre, r.status, crudo(r))
		}
	}
	if len(x.g.hechas()) != 0 {
		t.Fatalf("un SDP inválido no llega a Meta: %v", x.g.hechas())
	}
	x.exigir(x.api(x.agente, http.MethodPost, "/llamadas/"+id+"/confirmar-medio", nil), http.StatusConflict) // aún sin pre_accept
	// Contesta con la respuesta real de Chromium: trae direcciones de la red local, que no se rechazan:
	// a Meta llega el SDP saneado (sin ellas), con la huella y las credenciales ICE intactas.
	c := x.exigir(x.api(x.agente, http.MethodPost, "/llamadas/"+id+"/contestar", map[string]any{"sdpRespuesta": sdpChromium}), http.StatusOK)
	if c.cuerpo["estado"] != "conectando" || x.g.cuenta("pre_accept:"+call) != 1 || len(x.g.sdps) != 1 || strings.Contains(crudo(c), "fingerprint") {
		t.Fatalf("contestar: %s %v", crudo(c), x.g.hechas())
	}
	if aMeta := x.g.sdps[0]; strings.Contains(aMeta, "a=candidate") || !strings.Contains(aMeta, "c=IN IP4 0.0.0.0\r\n") || !strings.Contains(aMeta, "a=fingerprint:sha-256 7A:7C:01") ||
		!strings.Contains(aMeta, "a=ice-ufrag:YLOI\r\n") || mensajeria.ValidarSDP(aMeta) != nil {
		t.Fatalf("SDP que llegó a Meta: %q", aMeta)
	}
	c = x.exigir(x.api(x.agente, http.MethodPost, "/llamadas/"+id+"/confirmar-medio", nil), http.StatusOK)
	if c.cuerpo["estado"] != "en_curso" || c.cuerpo["atendidaPor"] != "persona" || x.g.cuenta("accept:"+call) != 1 || len(c.cuerpo["tramos"].([]any)) != 1 ||
		c.cuerpo["puede"].(map[string]any)["colgar"] != true || x.eventos("llamada.contestada", id) != 1 {
		t.Fatalf("en curso: %s", crudo(c))
	}
	if a := x.exigir(x.api(x.agente, http.MethodGet, "/llamadas/activa", nil), http.StatusOK); a.cuerpo["id"] != id {
		t.Fatalf("llamada activa: %s", crudo(a))
	}
	x.exigir(x.api(x.supervisor, http.MethodGet, "/llamadas/activa", nil), http.StatusNoContent)

	// Nota (va también al hilo) y colgar.
	x.exigir(x.api(x.otro, http.MethodPost, "/llamadas/"+id+"/anotar", map[string]any{"nota": "no es mía"}), http.StatusNotFound)
	n := x.exigir(x.api(x.agente, http.MethodPost, "/llamadas/"+id+"/anotar", map[string]any{"nota": "Se le explicó el corte por falla de zona", "resultado": "resuelto"}), http.StatusOK)
	if n.cuerpo["nota"] != "Se le explicó el corte por falla de zona" || n.cuerpo["resultado"] != "resuelto" {
		t.Fatalf("anotada: %s", crudo(n))
	}
	x.exigir(x.api(x.otro, http.MethodPost, "/llamadas/"+id+"/colgar", nil), http.StatusNotFound)
	f := x.exigir(x.api(x.agente, http.MethodPost, "/llamadas/"+id+"/colgar", nil), http.StatusOK)
	if f.cuerpo["estado"] != "terminada" || f.cuerpo["motivoFin"] != "agente_colgo" || x.g.cuenta("terminate:"+call) != 1 || x.eventos("llamada.terminada", id) != 1 {
		t.Fatalf("colgada: %s %v", crudo(f), x.g.hechas())
	}
	x.exigir(x.api(x.agente, http.MethodPost, "/llamadas/"+id+"/colgar", nil), http.StatusConflict)
	// El terminate de Meta llega después: solo completa la duración, no reabre ni repite eventos.
	x.webhook(x.eventoLlamada(num, call, "terminate", `,"status":"COMPLETED","duration":42`, 0))
	v := x.exigir(x.api(x.agente, http.MethodGet, "/llamadas/"+id, nil), http.StatusOK)
	if v.cuerpo["estado"] != "terminada" || v.cuerpo["duracionSeg"] != float64(42) || x.eventos("llamada.terminada", id) != 1 {
		t.Fatalf("tras el terminate de Meta: %s", crudo(v))
	}
	ms = x.mensajes(x.agente, conv)
	var llamadaMsg, nota map[string]any
	for _, m := range ms {
		if m["tipo"] == "llamada" {
			llamadaMsg = m["llamada"].(map[string]any)
		}
		if m["direccion"] == "nota" {
			nota = m
		}
	}
	if llamadaMsg["estado"] != "terminada" || llamadaMsg["tieneNota"] != true || nota == nil || nota["texto"] != "Se le explicó el corte por falla de zona" {
		t.Fatalf("hilo: %v %v", llamadaMsg, nota)
	}
	// El SDP se borra al terminar y nunca quedó en auditoría, eventos, feed ni idempotencia; el token tampoco.
	for _, sql := range []string{
		`SELECT count(*) FROM mensajeria.llamadas WHERE sdp_oferta IS NOT NULL AND id_externo = '` + call + `'`,
		`SELECT count(*) FROM auditoria.acciones WHERE entrada::text LIKE '%fingerprint%' OR entrada::text LIKE '%token-de-meta%'`,
		`SELECT count(*) FROM integracion.outbox WHERE payload::text LIKE '%fingerprint%' OR payload::text LIKE '%` + call + `%'`,
		`SELECT count(*) FROM nucleo.idempotencia WHERE position(convert_to('fingerprint', 'UTF8') in respuesta) > 0`,
	} {
		if n := x.contar(sql); n != 0 {
			t.Fatalf("quedó SDP, call_id o token donde no debe (%d): %s", n, sql)
		}
	}
	// La huella auditada es la del SDP saneado (el que llegó a Meta), no la del que mandó el navegador.
	huella := func(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
	if n := x.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'llamadas.contestar' AND entrada->>'sdp_sha256' = $2 AND actor_id = $1`, x.agente.id, huella(x.g.sdps[0])); n != 1 {
		t.Fatal("la auditoría de contestar debía llevar la huella del SDP saneado")
	}
	if n := x.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'llamadas.contestar' AND entrada->>'sdp_sha256' = $1`, huella(sdpChromium)); n != 0 {
		t.Fatal("quedó auditada la huella del SDP sin sanear")
	}
}

func TestLlamadaPerdidaDevolucionYAviso(t *testing.T) {
	x := nuevaBandejaVoz(t)
	x.recibe(x.agente)
	num, call := numeroCliente(), "wacid.perdida"+numeroCliente()
	// El cliente escribió antes: la ventana de 24 h está abierta (una llamada sola no la abre).
	x.texto(num, "wamid.antes"+num, "Hola, ¿me pueden llamar?")
	x.connect(num, call)
	id, _, _ := x.llamada(call)
	x.pasa(20 * time.Second)
	x.mantener()
	if _, estado, _ := x.llamada(call); estado != "sonando" {
		t.Fatalf("a los 20 s sigue sonando: %s", estado)
	}
	x.pasa(11 * time.Second)
	x.mantener()
	_, estado, motivo := x.llamada(call)
	if estado != "perdida" || motivo != "nadie_contesto" || x.g.cuenta("reject:"+call) != 1 || x.eventos("llamada.perdida", id) != 1 {
		t.Fatalf("perdida: %s %s %v", estado, motivo, x.g.hechas())
	}
	conv := cid(x.conv(num))
	automaticos := func() int {
		return x.contar(`SELECT count(*) FROM mensajeria.conversacion_mensajes WHERE conversacion_id = $1 AND direccion = 'saliente' AND texto = 'No pudimos contestarte. Escríbenos por aquí.'`, conv)
	}
	if automaticos() != 1 || x.eventos("llamada.devolucion_pedida", id) != 1 {
		t.Fatalf("aviso y devolución: %d avisos, %d devoluciones, mensajes %v", automaticos(), x.eventos("llamada.devolucion_pedida", id), x.mensajes(x.agente, conv))
	}
	// Ya no se puede reclamar.
	x.exigir(x.api(x.agente, http.MethodPost, "/llamadas/"+id+"/reclamar", nil), http.StatusConflict)
	// Otra perdida en la misma hora: ni otro mensaje ni otra devolución pendiente.
	call2 := "wacid.perdida2" + numeroCliente()
	x.pasa(time.Minute, x.agente)
	x.connect(num, call2)
	x.webhook(x.eventoLlamada(num, call2, "terminate", `,"status":"FAILED"`, 0)) // el cliente colgó antes de que contestaran
	if _, estado, _ := x.llamada(call2); estado != "perdida" || automaticos() != 1 {
		t.Fatalf("segunda perdida: %s, avisos %d", estado, automaticos())
	}
	d := x.exigir(x.api(x.agente, http.MethodGet, "/llamadas/devoluciones", nil), http.StatusOK).cuerpo["data"].([]any)
	var mia map[string]any
	for _, f := range d {
		if fmt.Sprint(int64(f.(map[string]any)["conversacionId"].(float64))) == conv {
			if mia != nil {
				t.Fatal("dos devoluciones pendientes de la misma conversación")
			}
			mia = f.(map[string]any)
		}
	}
	// Trae el título y el número de su conversación (el panel no los busca en otra lista).
	if mia == nil || mia["llamadaId"] != id || mia["pedidaPor"] != "sistema" || mia["numero"] != "+"+strings.TrimPrefix(num, "+") || mia["titulo"] != "Carlos R." {
		t.Fatalf("por devolver: %v", d)
	}
	ruta := fmt.Sprintf("/llamadas/devoluciones/%d/descartar", int64(mia["id"].(float64)))
	x.exigir(x.api(x.lectura, http.MethodPost, ruta, map[string]any{"motivo": "x"}), http.StatusForbidden)
	if r := x.exigir(x.api(x.agente, http.MethodPost, ruta, map[string]any{"motivo": "Ya se resolvió por chat"}), http.StatusOK); r.cuerpo["estado"] != "descartada" || r.cuerpo["titulo"] != "Carlos R." {
		t.Fatalf("descartada: %s", crudo(r))
	}
	x.exigir(x.api(x.agente, http.MethodPost, ruta, map[string]any{"motivo": "otra vez"}), http.StatusConflict)
}

func TestLlamadaReservaSoltarYMetaRechaza(t *testing.T) {
	x := nuevaBandejaVoz(t)
	x.recibe(x.agente, x.supervisor)
	num, call := numeroCliente(), "wacid.reserva"+numeroCliente()
	x.connect(num, call)
	id, _, _ := x.llamada(call)
	// La reserva vence sin contestar: vuelve a sonar.
	x.exigir(x.api(x.agente, http.MethodPost, "/llamadas/"+id+"/reclamar", nil), http.StatusOK)
	x.pasa(13*time.Second, x.agente, x.supervisor)
	x.exigir(x.api(x.agente, http.MethodPost, "/llamadas/"+id+"/contestar", map[string]any{"sdpRespuesta": sdpNavegador("")}), http.StatusConflict)
	x.mantener()
	if _, estado, _ := x.llamada(call); estado != "sonando" {
		t.Fatalf("tras vencer la reserva: %s", estado)
	}
	// Meta rechaza el pre_accept: 502 sin el mensaje de Meta, y la llamada vuelve a sonar.
	x.exigir(x.api(x.agente, http.MethodPost, "/llamadas/"+id+"/reclamar", nil), http.StatusOK)
	x.g.fallar["pre_accept"] = &mensajeria.ErrMetaLlamada{Status: 400}
	if r := x.exigir(x.api(x.agente, http.MethodPost, "/llamadas/"+id+"/contestar", map[string]any{"sdpRespuesta": sdpNavegador("")}), http.StatusBadGateway); !strings.Contains(crudo(r), "meta_rechazo") {
		t.Fatalf("meta rechaza: %s", crudo(r))
	}
	delete(x.g.fallar, "pre_accept")
	if _, estado, _ := x.llamada(call); estado != "sonando" {
		t.Fatalf("tras el rechazo de Meta: %s", estado)
	}
	// Quien dejó ir dos reservas ya no recibe esta llamada; el otro candidato sí.
	x.exigir(x.api(x.agente, http.MethodPost, "/llamadas/"+id+"/reclamar", nil), http.StatusNotFound)
	x.exigir(x.api(x.supervisor, http.MethodPost, "/llamadas/"+id+"/reclamar", nil), http.StatusOK)
	if r := x.exigir(x.api(x.supervisor, http.MethodPost, "/llamadas/"+id+"/soltar", map[string]any{"motivo": "sin_microfono"}), http.StatusOK); r.cuerpo["estado"] != "sonando" {
		t.Fatalf("soltada: %s", crudo(r))
	}
	// Contestó pero el audio no conectó en 5 s: no se acepta y se suelta.
	x.exigir(x.api(x.supervisor, http.MethodPost, "/llamadas/"+id+"/reclamar", nil), http.StatusOK)
	x.exigir(x.api(x.supervisor, http.MethodPost, "/llamadas/"+id+"/contestar", map[string]any{"sdpRespuesta": sdpNavegador("")}), http.StatusOK)
	x.pasa(6 * time.Second)
	x.exigir(x.api(x.supervisor, http.MethodPost, "/llamadas/"+id+"/confirmar-medio", nil), http.StatusConflict)
	if x.g.cuenta("accept:"+call) != 0 {
		t.Fatal("sin confirmar el medio a tiempo no se manda accept")
	}
	// Rechazar: por defecto solo deja de sonarle a uno; para todos es de quien supervisa.
	num2, call2 := numeroCliente(), "wacid.rechazo"+numeroCliente()
	x.pasa(time.Second, x.agente, x.supervisor)
	x.connect(num2, call2)
	id2, _, _ := x.llamada(call2)
	x.exigir(x.api(x.agente, http.MethodPost, "/llamadas/"+id2+"/rechazar", map[string]any{"paraTodos": true}), http.StatusForbidden)
	x.exigir(x.api(x.agente, http.MethodPost, "/llamadas/"+id2+"/rechazar", map[string]any{}), http.StatusOK)
	x.exigir(x.api(x.agente, http.MethodPost, "/llamadas/"+id2+"/reclamar", nil), http.StatusNotFound)
	if _, estado, _ := x.llamada(call2); estado != "sonando" {
		t.Fatalf("rechazo de uno: %s", estado)
	}
	if r := x.exigir(x.api(x.supervisor, http.MethodPost, "/llamadas/"+id2+"/rechazar", map[string]any{"paraTodos": true}), http.StatusOK); r.cuerpo["estado"] != "rechazada" ||
		x.g.cuenta("reject:"+call2) != 1 || x.eventos("llamada.rechazada", id2) != 1 {
		t.Fatalf("rechazada para todos: %s", crudo(r))
	}
}

// Webhook falso, repetido o viejo; número sin llamadas; ráfaga; duración máxima y panel caído.
func TestLlamadaLimitesYWebhook(t *testing.T) {
	x := nuevaBandejaVoz(t)
	x.recibe(x.agente)
	// Un connect de hace más de 60 s no hace sonar nada.
	num, viejo := numeroCliente(), "wacid.viejo"+numeroCliente()
	x.webhook(x.eventoLlamada(num, viejo, "connect", fmt.Sprintf(`,"session":{"sdp_type":"offer","sdp":%q}`, sdpOfertaMeta), 2*time.Minute))
	idViejo, estado, _ := x.llamada(viejo)
	if estado != "perdida" || x.contar(`SELECT count(*) FROM mensajeria.llamada_candidatos WHERE llamada_id = $1::uuid`, idViejo) != 0 {
		t.Fatalf("connect viejo: %s", estado)
	}
	// Ráfaga: la cuarta llamada del número en 10 minutos se rechaza.
	rafaga := numeroCliente()
	for i := range 4 {
		call := fmt.Sprintf("wacid.rafaga%d%s", i, rafaga)
		x.pasa(time.Second, x.agente)
		x.connect(rafaga, call)
		_, estado, motivo := x.llamada(call)
		if i < 3 && estado != "sonando" || i == 3 && (estado != "rechazada" || motivo != "limite" || x.g.cuenta("reject:"+call) != 1) {
			t.Fatalf("llamada %d de la ráfaga: %s %s", i+1, estado, motivo)
		}
		if i < 3 {
			x.webhook(x.eventoLlamada(rafaga, call, "terminate", "", 0))
		}
	}
	// Duración máxima (10 min en esta empresa) y panel caído.
	x.exigir(x.api(x.supervisor, http.MethodPost, "/voz/ajustes/actualizar", map[string]any{"version": 0, "duracionMaxPersonaMin": 10,
		"segundosAsignado": 0}), http.StatusForbidden)
	admin := x.persona([]int64{1}, rolBase("admin"))
	admin.empresa = 1 // opera en todas: indica cuál con X-Iris-Empresa
	aj := x.exigir(x.api(admin, http.MethodPost, "/voz/ajustes/actualizar", map[string]any{"version": 0, "duracionMaxPersonaMin": 10,
		"segundosAsignado": 0}), http.StatusOK)
	if aj.cuerpo["version"] != float64(1) || x.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'voz.ajustes_cambiados' AND payload ? 'campos'`) == 0 {
		t.Fatalf("ajustes de voz: %s", crudo(aj))
	}
	x.exigir(x.api(admin, http.MethodPost, "/voz/ajustes/actualizar", map[string]any{"version": 0, "duracionMaxPersonaMin": 10,
		"segundosAsignado": 0}), http.StatusConflict)
	x.exigir(x.api(admin, http.MethodPost, "/voz/ajustes/actualizar", map[string]any{"version": 1, "duracionMaxPersonaMin": 500,
		"segundosAsignado": 0}), http.StatusUnprocessableEntity)
	larga, callLarga := numeroCliente(), "wacid.larga"+numeroCliente()
	x.pasa(time.Second, x.agente)
	x.connect(larga, callLarga)
	idLarga, _, _ := x.llamada(callLarga)
	x.contestar(x.agente, idLarga)
	x.pasa(9*time.Minute, x.agente)
	x.mantener()
	if _, estado, _ := x.llamada(callLarga); estado != "en_curso" {
		t.Fatalf("a los 9 minutos sigue: %s", estado)
	}
	x.pasa(61*time.Second, x.agente)
	x.mantener()
	if _, estado, motivo := x.llamada(callLarga); estado != "terminada" || motivo != "duracion_maxima" || x.g.cuenta("terminate:"+callLarga) != 1 {
		t.Fatalf("duración máxima: %s %s", estado, motivo)
	}
	caida, callCaida := numeroCliente(), "wacid.caida"+numeroCliente()
	x.connect(caida, callCaida)
	idCaida, _, _ := x.llamada(callCaida)
	x.contestar(x.agente, idCaida)
	x.pasa(50 * time.Second) // sin latido del panel del agente
	x.mantener()
	if _, estado, _ := x.llamada(callCaida); estado != "en_curso" {
		t.Fatalf("dentro de la gracia sigue: %s", estado)
	}
	x.pasa(10 * time.Second)
	x.mantener()
	if _, estado, motivo := x.llamada(callCaida); estado != "terminada" || motivo != "error_medio" {
		t.Fatalf("panel caído: %s %s", estado, motivo)
	}
	// Con las llamadas apagadas, un connect no crea nada y se rechaza en Meta.
	v := x.exigir(x.api(x.supervisor, http.MethodGet, x.rutaCanal("/llamadas"), nil), http.StatusOK)
	x.exigir(x.configurar(int(v.cuerpo["version"].(float64)), map[string]any{"llamadasActivas": false}), http.StatusOK)
	apagado := "wacid.apagado" + numeroCliente()
	x.connect(numeroCliente(), apagado)
	if x.contar(`SELECT count(*) FROM mensajeria.llamadas WHERE id_externo = $1`, apagado) != 0 || x.g.cuenta("reject:"+apagado) != 1 {
		t.Fatalf("número con las llamadas apagadas: %v", x.g.hechas())
	}
	// Tampoco queda su SDP en el webhook guardado.
	if n := x.contar(`SELECT count(*) FROM mensajeria.entrantes WHERE canal_id = $1 AND payload::text LIKE '%' || $2 || '%' AND payload::text LIKE '%fingerprint%'`, x.canal.ID, apagado); n != 0 {
		t.Fatal("quedó el SDP de una llamada rechazada sin registro")
	}
	if n := x.contar(`SELECT count(*) FROM mensajeria.entrantes WHERE canal_id = $1 AND payload::text LIKE '%' || $2 || '%' AND payload::text LIKE '%"sdp": "***"%'`, x.canal.ID, apagado); n != 1 {
		t.Fatalf("webhook de la llamada rechazada sin registro, con el SDP tapado: %d", n)
	}
}

func TestLlamadasAjustesDelNumero(t *testing.T) {
	g := &graphFalso{fallar: map[string]error{}, ajustes: mensajeria.AjustesMetaLlamadas{Habilitadas: false, LimiteMensajeria: 250}}
	x := &bandejaVoz{bandeja: nuevaBandeja(t, func(c *mensajeria.Config) { c.Llamadas = g }), g: g}
	// De fábrica: apagadas, version 0.
	v := x.exigir(x.api(x.supervisor, http.MethodGet, x.rutaCanal("/llamadas"), nil), http.StatusOK)
	if v.cuerpo["llamadasActivas"] != false || v.cuerpo["version"] != float64(0) || v.cuerpo["disponible"] != true || v.cuerpo["segundosSonando"] != float64(30) {
		t.Fatalf("de fábrica: %s", crudo(v))
	}
	x.exigir(x.api(x.agente, http.MethodGet, x.rutaCanal("/llamadas"), nil), http.StatusForbidden)
	// Encender sin cumplir los requisitos de Meta: 422 con cada requisito.
	c := x.exigir(x.api(x.supervisor, http.MethodPost, x.rutaCanal("/llamadas/comprobar"), nil), http.StatusOK)
	if c.cuerpo["requisitos"].(map[string]any)["cumple"] != false {
		t.Fatalf("comprobación: %s", crudo(c))
	}
	r := x.exigir(x.configurar(int(c.cuerpo["version"].(float64)), nil), http.StatusUnprocessableEntity)
	if !strings.Contains(crudo(r), "requisitos_sin_cumplir") || !strings.Contains(crudo(r), "limite_mensajeria") || !strings.Contains(crudo(r), "calling_habilitado") {
		t.Fatalf("sin requisitos: %s", crudo(r))
	}
	// Meta ya cumple: se enciende, se escribe en Meta y sale el evento.
	g.mu.Lock()
	g.ajustes = mensajeria.AjustesMetaLlamadas{Habilitadas: true, IconoVisible: true, LimiteMensajeria: 2000}
	g.mu.Unlock()
	c = x.exigir(x.api(x.supervisor, http.MethodPost, x.rutaCanal("/llamadas/comprobar"), nil), http.StatusOK)
	// B6: la suscripción al campo calls no se puede leer: queda «sin comprobar» (aviso, no impide
	// encender) hasta que llega la primera llamada al número.
	webhookCalls := func(r respuesta) map[string]any {
		for _, f := range r.cuerpo["requisitos"].(map[string]any)["requisitos"].([]any) {
			if f.(map[string]any)["codigo"] == "webhook_calls" {
				return f.(map[string]any)
			}
		}
		t.Fatalf("sin el requisito webhook_calls: %s", crudo(r))
		return nil
	}
	if w := webhookCalls(c); w["cumple"] != false || w["sinComprobar"] != true || w["aviso"] != true || c.cuerpo["requisitos"].(map[string]any)["cumple"] != true {
		t.Fatalf("webhook_calls sin comprobar: %s", crudo(c))
	}
	version := int(c.cuerpo["version"].(float64))
	x.exigir(x.configurar(version, map[string]any{"segundosSonando": 10}), http.StatusUnprocessableEntity)
	x.exigir(x.configurar(version, map[string]any{"fueraDeHorario": "asistente"}), http.StatusUnprocessableEntity)
	x.exigir(x.configurar(version, map[string]any{"colaId": 999999999}), http.StatusUnprocessableEntity)
	ok := x.exigir(x.configurar(version, map[string]any{"segundosSonando": 20}), http.StatusOK)
	if ok.cuerpo["llamadasActivas"] != true || ok.cuerpo["segundosSonando"] != float64(20) || len(g.fijados) != 1 || !g.fijados[0].Habilitadas {
		t.Fatalf("encendidas: %s %v", crudo(ok), g.fijados)
	}
	if ev := x.ultimoEvento("canal_mensajeria.llamadas_cambiadas", "canal_id", fmt.Sprint(x.canal.ID)); ev["llamadas_activas"] != true || ev["canal_id"] != float64(x.canal.ID) {
		t.Fatalf("evento: %v", ev)
	}
	x.exigir(x.configurar(version, nil), http.StatusConflict)
	x.connect(numeroCliente(), "wacid.primera"+numeroCliente())
	c = x.exigir(x.api(x.supervisor, http.MethodPost, x.rutaCanal("/llamadas/comprobar"), nil), http.StatusOK)
	if w := webhookCalls(c); w["cumple"] != true || w["sinComprobar"] == true {
		t.Fatalf("webhook_calls tras la primera llamada: %s", crudo(c))
	}
	// Un número por QR: nunca.
	if _, err := x.bd.Exec(x.ctx, `UPDATE mensajeria.voz_ajustes_canal SET llamadas_activas = false WHERE canal_id = $1`, x.canal.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := x.bd.Exec(x.ctx, `UPDATE mensajeria.canales SET tipo_conexion = 'qr', conectado_en = now() WHERE id = $1`, x.canal.ID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = x.bd.Exec(x.ctx, `UPDATE mensajeria.canales SET tipo_conexion = 'api' WHERE id = $1`, x.canal.ID)
	})
	v = x.exigir(x.api(x.supervisor, http.MethodGet, x.rutaCanal("/llamadas"), nil), http.StatusOK)
	if v.cuerpo["disponible"] != false {
		t.Fatalf("QR: %s", crudo(v))
	}
	if r := x.exigir(x.configurar(int(v.cuerpo["version"].(float64)), nil), http.StatusUnprocessableEntity); !strings.Contains(crudo(r), "llamadas_no_disponibles_qr") {
		t.Fatalf("encender en QR: %s", crudo(r))
	}
}

// SanearSDP: la respuesta del navegador no se rechaza por traer direcciones locales; se limpian.
func TestSanearSDP(t *testing.T) {
	// La respuesta real de Chromium, tal cual: estricta no pasa (direcciones de la red local); saneada sí.
	if !strings.Contains(sdpChromium, "typ host") || mensajeria.ValidarSDP(sdpChromium) == nil {
		t.Fatal("el fixture de Chromium debía traer candidatos host con direcciones locales")
	}
	san, err := mensajeria.SanearSDP(sdpChromium)
	if err != nil {
		t.Fatalf("la respuesta real de Chromium debe aceptarse: %v", err)
	}
	if err := mensajeria.ValidarSDP(san); err != nil {
		t.Fatalf("el SDP saneado debe pasar la validación estricta: %v\n%s", err, san)
	}
	if strings.Contains(san, "192.0.2.") || strings.Contains(san, "a=candidate") || !strings.Contains(san, "c=IN IP4 0.0.0.0\r\n") {
		t.Fatalf("quedaron direcciones locales:\n%s", san)
	}
	// Todo lo demás llega intacto y en el mismo orden.
	sin := func(sdp string) (r []string) {
		for _, l := range strings.Split(strings.TrimSpace(strings.ReplaceAll(sdp, "\r\n", "\n")), "\n") {
			if !strings.HasPrefix(l, "a=candidate:") && !strings.HasPrefix(l, "c=") {
				r = append(r, l)
			}
		}
		return r
	}
	if a, b := sin(sdpChromium), sin(san); strings.Join(a, "|") != strings.Join(b, "|") {
		t.Fatalf("el saneado cambió otras líneas:\n%v\n%v", a, b)
	}
	// La misma respuesta en una red de casa u oficina: host privado, mDNS, IPv6 local, srflx con raddr,
	// CGNAT y NAT64. Solo queda el candidato público, sin raddr ni rport.
	casa := strings.Replace(strings.ReplaceAll(sdpChromium, "192.0.2.2", "192.168.1.23"), "a=ice-ufrag:",
		"a=candidate:1 1 udp 2122194687 4f6e1a2b-0c3d-4e5f-8a9b-1c2d3e4f5a6b.local 51000 typ host generation 0\r\n"+
			"a=candidate:2 1 udp 2122129151 fe80::1c2d:3e4f:5a6b:7c8d 51001 typ host generation 0\r\n"+
			"a=candidate:3 1 udp 1685987071 190.56.10.7 51002 typ srflx raddr 192.168.1.23 rport 33630 generation 0 network-id 1\r\n"+
			"a=candidate:4 1 udp 1685987070 100.72.3.4 51003 typ srflx raddr 10.0.0.2 rport 9 generation 0\r\n"+
			"a=candidate:5 1 udp 1685987069 64:ff9b::c0a8:117 51004 typ host generation 0\r\n"+
			"a=candidate:6 1 udp 1685987068 127.0.0.1 51005 typ host generation 0\r\na=ice-ufrag:", 1)
	san, err = mensajeria.SanearSDP(casa)
	if err != nil {
		t.Fatalf("respuesta desde una red de casa: %v", err)
	}
	var quedan []string
	for _, l := range strings.Split(san, "\r\n") {
		if strings.HasPrefix(l, "a=candidate:") {
			quedan = append(quedan, l)
		}
	}
	if len(quedan) != 1 || quedan[0] != "a=candidate:3 1 udp 1685987071 190.56.10.7 51002 typ srflx generation 0 network-id 1" {
		t.Fatalf("candidatos tras sanear: %q", quedan)
	}
	for _, malo := range []string{"192.168.", "10.0.0.", ".local", "fe80", "100.72.", "64:ff9b", "127.0.0.1 51005", "raddr", "rport"} {
		if strings.Contains(san, malo) {
			t.Fatalf("quedó %q:\n%s", malo, san)
		}
	}
	if err := mensajeria.ValidarSDP(san); err != nil {
		t.Fatalf("saneado: %v", err)
	}
	// raddr y rport se quitan estén donde estén, y un 6to4 de una dirección pública se conserva.
	san, err = mensajeria.SanearSDP(sdpNavegador("a=candidate:2 1 udp 1 190.56.10.9 9 typ srflx suelta raddr 10.0.0.5 rport 9 generation 0\r\n" +
		"a=candidate:3 1 udp 1 2002:be38:0a07::1 9 typ host\r\na=candidate:4 1 udp 1 2002:c0a8:0101::1 9 typ host\r\na=candidate:5 1 udp 1 ::10.0.0.1 9 typ host\r\n"))
	if err != nil || !strings.Contains(san, "a=candidate:2 1 udp 1 190.56.10.9 9 typ srflx suelta generation 0\r\n") || !strings.Contains(san, "a=candidate:3 ") ||
		strings.Contains(san, "a=candidate:4 ") || strings.Contains(san, "a=candidate:5 ") || strings.Contains(san, "raddr") || strings.Contains(san, "rport") {
		t.Fatalf("raddr desplazado y 6to4: %v\n%s", err, san)
	}
	// Candidatos de más: se quedan los primeros 32.
	if san, err = mensajeria.SanearSDP(sdpNavegador(strings.Repeat("a=candidate:2 1 udp 1 190.56.10.9 9 typ host\r\n", 40))); err != nil ||
		strings.Count(san, "a=candidate:") != mensajeria.MaxCandidatosSDP {
		t.Fatalf("tope de candidatos: %v, %d", err, strings.Count(san, "a=candidate:"))
	}
	// Lo estructuralmente inválido o fuera de la lista cerrada se sigue rechazando.
	for nombre, malo := range map[string]string{
		"vacío":                "",
		"con video":            sdpChromium + "m=video 9 UDP/TLS/RTP/SAVPF 96\r\na=rtpmap:96 VP8/90000\r\n",
		"con datos":            sdpChromium + "m=application 9 UDP/DTLS/SCTP webrtc-datachannel\r\n",
		"atributo desconocido": sdpChromium + "a=x-google-flag:conference\r\n",
		"línea desconocida":    sdpChromium + "u=http://interno.ejemplo/\r\n",
		"otro códec":           strings.Replace(sdpChromium, "opus/48000/2", "PCMU/8000", 1),
		"sin huella":           strings.Replace(sdpChromium, "a=fingerprint:", "a=mid:", 1),
		"RTP sin cifrar":       strings.Replace(sdpChromium, "UDP/TLS/RTP/SAVPF", "RTP/AVP", 1),
		"candidato corto":      sdpChromium + "a=candidate:9 1 udp 1 190.56.10.7\r\n",
		"c= mal formada":       strings.Replace(sdpChromium, "c=IN IP4 192.0.2.2", "c=IN IP4", 1),
		"a=rtcp mal formado":   strings.Replace(sdpChromium, "a=rtcp:9 IN IP4 0.0.0.0", "a=rtcp:9 IN", 1),
		"enorme":               sdpChromium + strings.Repeat("a=mid:"+strings.Repeat("y", 90)+"\r\n", 90),
	} {
		if _, err := mensajeria.SanearSDP(malo); err == nil {
			t.Errorf("%s: debía rechazarse", nombre)
		}
	}
}

func TestValidarSDP(t *testing.T) {
	if err := mensajeria.ValidarSDP(sdpNavegador("")); err != nil {
		t.Fatalf("SDP de navegador válido: %v", err)
	}
	for nombre, malo := range map[string]string{
		"vacío":                            "",
		"línea de control":                 sdpNavegador("a=x:\x00\r\n"),
		"candidato IPv6 ::1":               sdpNavegador("a=candidate:2 1 udp 1 ::1 9 typ host\r\n"),
		"candidato fe80":                   sdpNavegador("a=candidate:2 1 udp 1 fe80::1 9 typ host\r\n"),
		"nombre arbitrario":                sdpNavegador("a=candidate:2 1 udp 1 interno.ejemplo 9 typ host\r\n"),
		"candidato mDNS":                   sdpNavegador("a=candidate:3 1 udp 1 abcd1234-0000.local 9 typ host\r\n"),
		"rango de documentación":           sdpNavegador("a=candidate:2 1 udp 1 203.0.113.7 9 typ host\r\n"),
		"respuesta de Chromium sin sanear": sdpChromium,
		"sin audio":                        "v=0\r\ns=-\r\na=setup:active\r\na=fingerprint:sha-256 AA\r\n",
		"sin setup":                        strings.Replace(sdpNavegador(""), "a=setup:active\r\n", "", 1),
		"RTP sin cifrar":                   strings.Replace(sdpNavegador(""), "UDP/TLS/RTP/SAVPF", "RTP/AVP", 1),
		// Lista cerrada (B1): líneas y atributos desconocidos, y toda dirección del SDP.
		"atributo desconocido": sdpNavegador("a=x-google-flag:conference\r\n"),
		"línea desconocida":    sdpNavegador("u=http://interno.ejemplo/\r\n"),
		"c= privada":           strings.Replace(sdpNavegador(""), "c=IN IP4 0.0.0.0", "c=IN IP4 192.168.1.10", 1),
		"c= metadata":          strings.Replace(sdpNavegador(""), "c=IN IP4 0.0.0.0", "c=IN IP4 169.254.169.254", 1),
		"c= con nombre":        strings.Replace(sdpNavegador(""), "c=IN IP4 0.0.0.0", "c=IN IP4 interno.ejemplo", 1),
		"a=rtcp privada":       sdpNavegador("a=rtcp:9 IN IP4 10.1.2.3\r\n"),
		"raddr privada":        sdpNavegador("a=candidate:2 1 udp 1 190.56.10.9 9 typ srflx raddr 10.0.0.5 rport 9\r\n"),
		"candidato CGNAT":      sdpNavegador("a=candidate:2 1 udp 1 100.64.1.1 9 typ host\r\n"),
		"candidato NAT64":      sdpNavegador("a=candidate:2 1 udp 1 64:ff9b::a00:1 9 typ host\r\n"),
		"IPv4 en IPv6":         sdpNavegador("a=candidate:2 1 udp 1 ::ffff:10.0.0.1 9 typ host\r\n"),
		"candidatos de más":    sdpNavegador(strings.Repeat("a=candidate:2 1 udp 1 190.56.10.9 9 typ host\r\n", mensajeria.MaxCandidatosSDP)),
		"dos audios":           sdpNavegador("m=audio 9 UDP/TLS/RTP/SAVPF 111\r\n"),
		// IPv6 que embeben una IPv4 privada, y site-local.
		"IPv4 compatible":     sdpNavegador("a=candidate:2 1 udp 1 ::10.0.0.1 9 typ host\r\n"),
		"6to4 de una privada": sdpNavegador("a=candidate:2 1 udp 1 2002:0a00:0001::1 9 typ host\r\n"),
		"IPv4 traducida":      sdpNavegador("a=candidate:2 1 udp 1 ::ffff:0:10.0.0.1 9 typ host\r\n"),
		"site-local":          sdpNavegador("a=candidate:2 1 udp 1 fec0::1 9 typ host\r\n"),
		// raddr fuera de su sitio (tras una extensión suelta): se encuentra igual.
		"raddr desplazado": sdpNavegador("a=candidate:2 1 udp 1 190.56.10.9 9 typ srflx suelta raddr 10.0.0.5 rport 9\r\n"),
	} {
		if err := mensajeria.ValidarSDP(malo); err == nil {
			t.Errorf("%s: debía rechazarse", nombre)
		}
	}
	for nombre, bueno := range map[string]string{
		"oferta de Meta":       sdpOfertaMeta,
		"rtcp y raddr al cero": sdpNavegador("a=rtcp:9 IN IP4 0.0.0.0\r\na=candidate:2 1 udp 1 190.56.10.9 9 typ srflx raddr 0.0.0.0 rport 0\r\na=rtcp-mux\r\na=mid:0\r\na=sendrecv\r\n"),
	} {
		if err := mensajeria.ValidarSDP(bueno); err != nil {
			t.Errorf("%s: %v", nombre, err)
		}
	}
}

// Revisión de seguridad de la E1 (M1, M2, B1 a B5 y B7).

// M1: con un pre_accept o un accept en vuelo no se suelta ni se reclama; y su resultado solo vale si
// la llamada sigue conectando en manos de quien tenía la reserva.
func TestLlamadaAccionEnVuelo(t *testing.T) {
	x := nuevaBandejaVoz(t)
	x.recibe(x.agente, x.supervisor)
	num, call := numeroCliente(), "wacid.vuelo"+numeroCliente()
	x.connect(num, call)
	id, _, _ := x.llamada(call)
	x.exigir(x.api(x.agente, http.MethodPost, "/llamadas/"+id+"/reclamar", nil), http.StatusOK)
	// Mientras Meta atiende el pre_accept y el accept: soltar da 409 y nadie más la reclama.
	vistos := map[string]bool{}
	x.gancho(func(accion string) {
		vistos[accion] = true
		if r := x.api(x.agente, http.MethodPost, "/llamadas/"+id+"/soltar", map[string]any{"motivo": "sin_microfono"}); r.status != http.StatusConflict ||
			!strings.Contains(crudo(r), "accion_en_curso") {
			t.Errorf("soltar con %s en vuelo: %d %s", accion, r.status, crudo(r))
		}
		if r := x.api(x.supervisor, http.MethodPost, "/llamadas/"+id+"/reclamar", nil); r.status != http.StatusConflict {
			t.Errorf("reclamar con %s en vuelo: %d %s", accion, r.status, crudo(r))
		}
	})
	x.exigir(x.api(x.agente, http.MethodPost, "/llamadas/"+id+"/contestar", map[string]any{"sdpRespuesta": sdpNavegador("")}), http.StatusOK)
	r := x.exigir(x.api(x.agente, http.MethodPost, "/llamadas/"+id+"/confirmar-medio", nil), http.StatusOK)
	x.gancho(nil)
	if !vistos["pre_accept"] || !vistos["accept"] || r.cuerpo["estado"] != "en_curso" {
		t.Fatalf("acciones vistas %v, estado %v", vistos, r.cuerpo["estado"])
	}
	if n := x.contar(`SELECT count(*) FROM mensajeria.llamadas WHERE id = $1::uuid AND usuario_id = $2 AND estado = 'en_curso'`, id, x.agente.id); n != 1 {
		t.Fatal("la llamada debía quedar en curso para quien la contestó")
	}
	x.exigir(x.api(x.agente, http.MethodPost, "/llamadas/"+id+"/colgar", nil), http.StatusOK)

	// Una llamada que suena con una acción de la reserva anterior a medias: reclamar da 409 con su regla.
	num2, call2 := numeroCliente(), "wacid.vuelo2"+numeroCliente()
	x.pasa(time.Second, x.agente, x.supervisor)
	x.connect(num2, call2)
	id2, _, _ := x.llamada(call2)
	x.sql(`UPDATE mensajeria.llamadas SET accion_meta = 'accept', accion_meta_estado = 'pendiente', accion_meta_en = $2 WHERE id = $1::uuid`, id2, *x.reloj)
	if r := x.exigir(x.api(x.supervisor, http.MethodPost, "/llamadas/"+id2+"/reclamar", nil), http.StatusConflict); !strings.Contains(crudo(r), "accion_en_curso") {
		t.Fatalf("reclamar con una acción a medias: %s", crudo(r))
	}
	x.sql(`UPDATE mensajeria.llamadas SET accion_meta = NULL, accion_meta_estado = NULL, accion_meta_en = NULL WHERE id = $1::uuid`, id2)

	// La carrera original, forzada en la base: mientras Meta atiende el accept la llamada queda sonando
	// sin reserva (a) o reservada por otra persona (b). El resultado no se aplica: la llamada no queda
	// en curso para nadie, se termina en Meta y nada revienta.
	for caso, forzar := range map[string]func(id string){
		"sin reserva": func(id string) {
			x.sql(`UPDATE mensajeria.llamadas SET estado = 'sonando', reclamada_por = NULL, reclamada_hasta = NULL WHERE id = $1::uuid`, id)
		},
		"reservada por otra persona": func(id string) {
			x.sql(`UPDATE mensajeria.llamadas SET reclamada_por = $2 WHERE id = $1::uuid`, id, x.supervisor.id)
		},
	} {
		x.pasa(time.Second, x.agente, x.supervisor)
		n, c := numeroCliente(), "wacid.carrera"+numeroCliente()
		x.connect(n, c)
		idc, _, _ := x.llamada(c)
		x.exigir(x.api(x.agente, http.MethodPost, "/llamadas/"+idc+"/reclamar", nil), http.StatusOK)
		x.exigir(x.api(x.agente, http.MethodPost, "/llamadas/"+idc+"/contestar", map[string]any{"sdpRespuesta": sdpNavegador("")}), http.StatusOK)
		x.gancho(func(accion string) {
			if accion == "accept" {
				forzar(idc)
			}
		})
		rc := x.api(x.agente, http.MethodPost, "/llamadas/"+idc+"/confirmar-medio", nil)
		x.gancho(nil)
		_, estado, motivo := x.llamada(c)
		if estado != "fallida" || motivo != "error_meta" || x.g.cuenta("terminate:"+c) != 1 || rc.status >= 500 && rc.status != http.StatusBadGateway {
			t.Fatalf("%s: %s %s, confirmar-medio %d, Meta %v", caso, estado, motivo, rc.status, x.g.hechas())
		}
		if n := x.contar(`SELECT count(*) FROM mensajeria.llamadas WHERE id = $1::uuid AND (usuario_id IS NOT NULL OR contestada_en IS NOT NULL)`, idc); n != 0 {
			t.Fatalf("%s: la llamada quedó a nombre de alguien", caso)
		}
		if x.contar(`SELECT count(*) FROM mensajeria.llamada_tramos WHERE llamada_id = $1::uuid`, idc) != 0 || x.eventos("llamada.contestada", idc) != 0 {
			t.Fatalf("%s: quedó un tramo o un evento de contestada", caso)
		}
	}
}

// M2: en un número prestado, la llamada de un cliente de la empresa prestataria no usa nada de la
// dueña: ni su cola, ni sus candidatos, ni su texto. No suena y queda perdida en la prestataria.
func TestLlamadaEnNumeroPrestado(t *testing.T) {
	x := nuevaBandejaVoz(t)
	x.recibe(x.agente, x.supervisor)
	sup2 := x.persona([]int64{2}, rolBase("supervisor"))
	rc := x.exigir(x.api(sup2, http.MethodPost, "/colas", map[string]any{"nombre": "Norte " + numeroCliente()}), http.StatusCreated)
	cola2 := int64(rc.cuerpo["id"].(float64))
	x.exigir(x.api(x.supervisor, http.MethodPost, x.rutaCanal("/prestamos"), map[string]any{"empresaId": 2, "usos": []string{"soporte"}, "colaId": cola2}), http.StatusOK)
	num, call := numeroCliente(), "wacid.prestado"+numeroCliente()
	x.texto(num, "wamid.prestado"+num, "Hola")
	conv := cid(x.conv(num))
	x.exigir(x.api(x.supervisor, http.MethodPost, "/conversaciones/"+conv+"/pasar-empresa", map[string]any{"version": ver(x.conv(num)), "empresaId": 2}), http.StatusOK)
	x.connect(num, call)
	id, estado, motivo := x.llamada(call)
	if estado != "perdida" || motivo != "nadie_contesto" || x.g.cuenta("reject:"+call) != 1 {
		t.Fatalf("llamada de la prestataria: %s %s %v", estado, motivo, x.g.hechas())
	}
	if n := x.contar(`SELECT count(*) FROM mensajeria.llamadas WHERE id = $1::uuid AND empresa_id = 2 AND cola_id IS NOT DISTINCT FROM $2 AND conversacion_id = $3`, id, cola2, conv); n != 1 {
		t.Fatal("la llamada debía quedar en la empresa prestataria, con su cola")
	}
	// Nadie de la dueña fue candidato ni recibió el timbre, ni la ve; la prestataria sí.
	if n := x.contar(`SELECT count(*) FROM mensajeria.llamada_candidatos WHERE llamada_id = $1::uuid`, id) +
		x.contar(`SELECT count(*) FROM mensajeria.conv_eventos WHERE llamada_id = $1::uuid AND usuario_id IN ($2, $3)`, id, x.agente.id, x.supervisor.id); n != 0 {
		t.Fatalf("candidatos o timbres de la dueña: %d", n)
	}
	x.exigir(x.api(x.supervisor, http.MethodGet, "/llamadas/"+id, nil), http.StatusNotFound)
	x.exigir(x.api(x.agente, http.MethodPost, "/llamadas/"+id+"/reclamar", nil), http.StatusNotFound)
	x.exigir(x.api(sup2, http.MethodGet, "/llamadas/"+id, nil), http.StatusOK)
	// Ni la cola ni el texto de la dueña cruzan: el evento lleva la cola de la prestataria y el mensaje es el neutro.
	if n := x.contar(`SELECT count(*) FROM integracion.outbox WHERE payload->>'llamada_id' = $1 AND (payload->>'empresa_id' <> '2' OR payload->>'cola_id' = $2)`, id, fmt.Sprint(x.cola)); n != 0 {
		t.Fatalf("eventos con la empresa o la cola de la dueña: %d", n)
	}
	if n := x.contar(`SELECT count(*) FROM mensajeria.conversacion_mensajes WHERE conversacion_id = $1 AND texto = 'No pudimos contestarte. Escríbenos por aquí.'`, conv); n != 0 {
		t.Fatal("salió el texto de la dueña en la conversación de la prestataria")
	}
	if n := x.contar(`SELECT count(*) FROM mensajeria.conversacion_mensajes WHERE conversacion_id = $1 AND direccion = 'saliente' AND texto = $2`, conv, mensajeria.MensajePerdidaNeutro); n != 1 {
		t.Fatalf("mensaje neutro: %d", n)
	}
	if n := x.contar(`SELECT count(*) FROM mensajeria.llamada_devoluciones WHERE llamada_id = $1::uuid AND empresa_id = 2`, id); n != 1 {
		t.Fatalf("devolución en la prestataria: %d", n)
	}
	// La ráfaga vale también aquí: de la cuarta llamada en 10 minutos no sale ni mensaje ni devolución ni aviso.
	var ultima string
	for i := range 3 {
		ultima = fmt.Sprintf("wacid.prestado%d%s", i, num)
		x.pasa(time.Second)
		x.connect(num, ultima)
	}
	idU, estado, motivo := x.llamada(ultima)
	if estado != "rechazada" || motivo != "limite" || x.eventos("llamada.perdida", idU) != 0 {
		t.Fatalf("cuarta llamada de la prestataria: %s %s", estado, motivo)
	}
	if n := x.contar(`SELECT count(*) FROM mensajeria.conversacion_mensajes WHERE conversacion_id = $1 AND direccion = 'saliente' AND texto = $2`, conv, mensajeria.MensajePerdidaNeutro); n != 1 {
		t.Fatalf("mensajes neutros tras la ráfaga: %d", n)
	}
	// Un cliente desconocido en el mismo número sigue sonando a la dueña.
	call3 := "wacid.propia" + numeroCliente()
	x.connect(numeroCliente(), call3)
	if _, estado, _ := x.llamada(call3); estado != "sonando" {
		t.Fatalf("llamada de un desconocido en el número prestado: %s", estado)
	}
}

// B1 (oferta de Meta), B2, B3, B4, B5 y B7.
func TestLlamadaRevisionE1(t *testing.T) {
	x := nuevaBandejaVoz(t)
	x.recibe(x.agente, x.supervisor)
	// B1: una oferta de Meta que no pasa la validación no llega al navegador: la llamada no suena.
	mala := "wacid.ofertamala" + numeroCliente()
	x.webhook(x.eventoLlamada(numeroCliente(), mala, "connect", fmt.Sprintf(`,"session":{"sdp_type":"offer","sdp":%q}`,
		strings.Replace(sdpOfertaMeta, "2113937151 157.240.19.130", "2113937151 10.0.0.9", 1)), 0))
	idMala, estado, _ := x.llamada(mala)
	if estado != "perdida" || x.contar(`SELECT count(*) FROM mensajeria.llamadas WHERE id = $1::uuid AND sdp_oferta IS NOT NULL`, idMala) != 0 ||
		x.contar(`SELECT count(*) FROM mensajeria.llamada_candidatos WHERE llamada_id = $1::uuid`, idMala) != 0 {
		t.Fatalf("oferta de Meta inválida: %s", estado)
	}

	// B5: dos llamadas suenan; quien contesta una no reclama la otra, y pasado el timbre ya no se reclama.
	numA, callA, numB, callB := numeroCliente(), "wacid.b5a"+numeroCliente(), numeroCliente(), "wacid.b5b"+numeroCliente()
	x.connect(numA, callA)
	x.connect(numB, callB)
	idA, _, _ := x.llamada(callA)
	idB, _, _ := x.llamada(callB)
	x.exigir(x.api(x.agente, http.MethodPost, "/llamadas/"+idA+"/reclamar", nil), http.StatusOK)
	if r := x.exigir(x.api(x.agente, http.MethodPost, "/llamadas/"+idB+"/reclamar", nil), http.StatusConflict); !strings.Contains(crudo(r), "llamada_en_curso") {
		t.Fatalf("reclamar con otra llamada viva (reservada): %s", crudo(r))
	}
	x.exigir(x.api(x.agente, http.MethodPost, "/llamadas/"+idA+"/contestar", map[string]any{"sdpRespuesta": sdpNavegador("")}), http.StatusOK)
	x.exigir(x.api(x.agente, http.MethodPost, "/llamadas/"+idA+"/confirmar-medio", nil), http.StatusOK)
	if r := x.exigir(x.api(x.agente, http.MethodPost, "/llamadas/"+idB+"/reclamar", nil), http.StatusConflict); !strings.Contains(crudo(r), "llamada_en_curso") {
		t.Fatalf("reclamar con otra llamada viva (en curso): %s", crudo(r))
	}
	if _, estado, _ := x.llamada(callB); estado != "sonando" {
		t.Fatalf("la otra llamada sigue sonando: %s", estado)
	}
	// Pasó segundosSonando y el mantenimiento aún no corrió: el supervisor ya no la reclama.
	x.pasa(31*time.Second, x.agente, x.supervisor)
	if r := x.exigir(x.api(x.supervisor, http.MethodPost, "/llamadas/"+idB+"/reclamar", nil), http.StatusConflict); !strings.Contains(crudo(r), "llamada_ya_contestada") {
		t.Fatalf("reclamar pasado el timbre: %s", crudo(r))
	}

	// B4: el mantenimiento no llama a Meta; el reject lo manda el job de escrituras, que reintenta si
	// Meta falla y se rinde a los 2 minutos.
	x.g.falla("reject", &mensajeria.ErrMetaLlamada{Status: 500})
	x.soloMantener()
	if _, estado, _ := x.llamada(callB); estado != "perdida" || x.g.cuenta("reject:"+callB) != 0 {
		t.Fatalf("tras el mantenimiento: %s, Meta %v", estado, x.g.hechas())
	}
	pendiente := func() int {
		return x.contar(`SELECT count(*) FROM mensajeria.llamadas WHERE id = $1::uuid AND accion_meta = 'reject' AND accion_meta_estado = 'pendiente'`, idB)
	}
	x.despachar()
	x.despachar() // el segundo ciclo no la repite: quedó apartada
	if x.g.cuenta("reject:"+callB) != 1 || pendiente() != 1 {
		t.Fatalf("primer intento fallido: %v, pendiente %d", x.g.hechas(), pendiente())
	}
	// Quedó apartada lote × plazo (20 s): a los 9 s ningún ciclo la retoma.
	x.pasa(9*time.Second, x.agente)
	x.despachar()
	if x.g.cuenta("reject:"+callB) != 1 {
		t.Fatalf("retomada antes de tiempo: %v", x.g.hechas())
	}
	x.g.falla("reject", nil)
	x.pasa(12*time.Second, x.agente)
	x.despachar()
	if x.g.cuenta("reject:"+callB) != 2 || pendiente() != 0 {
		t.Fatalf("reintento: %v, pendiente %d", x.g.hechas(), pendiente())
	}
	x.exigir(x.api(x.agente, http.MethodPost, "/llamadas/"+idA+"/colgar", nil), http.StatusOK)
	callC := "wacid.b4c" + numeroCliente()
	x.connect(numeroCliente(), callC)
	x.g.falla("reject", &mensajeria.ErrMetaLlamada{Status: 500})
	x.pasa(31*time.Second, x.agente)
	x.mantener()
	x.pasa(3*time.Minute, x.agente)
	antes := x.g.cuenta("reject:" + callC)
	x.despachar()
	x.g.falla("reject", nil)
	if n := x.contar(`SELECT count(*) FROM mensajeria.llamadas WHERE id_externo = $1 AND accion_meta_estado = 'fallida'`, callC); n != 1 || x.g.cuenta("reject:"+callC) != antes {
		t.Fatalf("pasados 2 minutos se rinde sin llamar a Meta: %d, %v", n, x.g.hechas())
	}

	// B2: al terminar la llamada, el SDP ya no está en los webhooks guardados.
	if n := x.contar(`SELECT count(*) FROM mensajeria.entrantes WHERE canal_id = $1 AND (payload::text LIKE '%' || $2 || '%' OR payload::text LIKE '%' || $3 || '%')
		AND payload::text LIKE '%fingerprint%'`, x.canal.ID, callA, callB); n != 0 {
		t.Fatalf("quedó el SDP en %d webhooks de llamadas terminadas", n)
	}
	if n := x.contar(`SELECT count(*) FROM mensajeria.entrantes WHERE canal_id = $1 AND payload::text LIKE '%' || $2 || '%' AND payload::text LIKE '%"sdp": "***"%'`, x.canal.ID, callA); n == 0 {
		t.Fatal("el webhook de la llamada debía conservarse con el SDP tapado")
	}

	// B3: tope de 10 navegadores por usuario; los que llevan 30 días sin verse se quitan.
	x.sql(`UPDATE mensajeria.llamada_receptores SET visto_en = $2 WHERE usuario_id = $1`, x.supervisor.id, x.reloj.Add(-31*24*time.Hour))
	x.exigir(x.api(x.supervisor, http.MethodPost, "/llamadas/recibir/encender", map[string]any{"navegador": "navegador-nuevo"}), http.StatusOK)
	if n := x.contar(`SELECT count(*) FROM mensajeria.llamada_receptores WHERE usuario_id = $1 AND recibe`, x.supervisor.id); n != 1 {
		t.Fatalf("navegadores viejos del supervisor: %d", n)
	}
	// El navegador apagado sabe por qué (id por cabecera; también vale la query; sin ninguno, 422).
	if r := x.exigir(x.api(x.supervisor, http.MethodGet, "/llamadas/recibir", nil), http.StatusOK); r.cuerpo["recibe"] != false || r.cuerpo["motivo"] != "inactivo" {
		t.Fatalf("apagado por inactivo: %s", crudo(r))
	}
	sinNav := x.supervisor
	sinNav.navegador = ""
	if r := x.exigir(x.api(sinNav, http.MethodGet, "/llamadas/recibir?navegador=navegador-nuevo", nil), http.StatusOK); r.cuerpo["recibe"] != true || r.cuerpo["motivo"] != nil {
		t.Fatalf("por query: %s", crudo(r))
	}
	x.exigir(x.api(sinNav, http.MethodGet, "/llamadas/recibir", nil), http.StatusUnprocessableEntity)
	for i := range 14 {
		x.pasa(time.Second)
		x.exigir(x.api(x.agente, http.MethodPost, "/llamadas/recibir/encender", map[string]any{"navegador": fmt.Sprintf("navegador-extra-%d", i)}), http.StatusOK)
	}
	if n := x.contar(`SELECT count(*) FROM mensajeria.llamada_receptores WHERE usuario_id = $1 AND recibe`, x.agente.id); n != mensajeria.MaxNavegadoresRecibir {
		t.Fatalf("navegadores del agente: %d", n)
	}
	if n := x.contar(`SELECT count(*) FROM mensajeria.llamada_receptores WHERE usuario_id = $1 AND recibe AND navegador IN ('navegador-extra-13', 'navegador-extra-4')`, x.agente.id); n != 2 {
		t.Fatal("debían quedar los 10 navegadores más recientes")
	}
	viejo := x.agente
	viejo.navegador = "navegador-extra-0"
	if r := x.exigir(x.api(viejo, http.MethodGet, "/llamadas/recibir", nil), http.StatusOK); r.cuerpo["recibe"] != false || r.cuerpo["motivo"] != "tope" {
		t.Fatalf("apagado por tope: %s", crudo(r))
	}
	// Al volver a encenderlo se borra el motivo.
	if r := x.exigir(x.api(viejo, http.MethodPost, "/llamadas/recibir/encender", map[string]any{"navegador": "navegador-extra-0"}), http.StatusOK); r.cuerpo["motivo"] != nil {
		t.Fatalf("reencendido: %s", crudo(r))
	}

	// B7: despacho contesta llamadas pero no descarta devoluciones (§8.2).
	despacho := x.persona([]int64{1}, rolBase("despacho"))
	dev := x.exigir(x.api(x.supervisor, http.MethodGet, "/llamadas/devoluciones", nil), http.StatusOK).cuerpo["data"].([]any)
	if len(dev) == 0 {
		t.Fatal("sin devoluciones pendientes")
	}
	ruta := fmt.Sprintf("/llamadas/devoluciones/%d/descartar", int64(dev[0].(map[string]any)["id"].(float64)))
	x.exigir(x.api(despacho, http.MethodPost, ruta, map[string]any{"motivo": "no me toca"}), http.StatusForbidden)
	x.exigir(x.api(despacho, http.MethodGet, "/llamadas", nil), http.StatusOK)
	x.exigir(x.api(x.supervisor, http.MethodPost, ruta, map[string]any{"motivo": "Ya se resolvió por chat"}), http.StatusOK)
}

// versionLlamadas: la versión vigente de los ajustes de llamadas del número.
func (x *bandejaVoz) versionLlamadas() int {
	x.t.Helper()
	return int(x.exigir(x.api(x.supervisor, http.MethodGet, x.rutaCanal("/llamadas"), nil), http.StatusOK).cuerpo["version"].(float64))
}

// en: fija el reloj en una hora local de Guatemala.
func (x *bandejaVoz) en(fechaHora string) {
	x.t.Helper()
	gt, err := time.LoadLocation("America/Guatemala")
	if err != nil {
		x.t.Fatal(err)
	}
	h, err := time.ParseInLocation("2006-01-02 15:04", fechaHora, gt)
	if err != nil {
		x.t.Fatal(err)
	}
	*x.reloj = h.UTC()
}

// Fuera de horario (§3.6): dentro, fuera, feriado, una franja que cruza la medianoche y la zona
// America/Guatemala; el mensaje sale una vez por ventana cerrada, compartida con el chat; y el buzón.
func TestLlamadaFueraDeHorario(t *testing.T) {
	x := nuevaBandejaVoz(t)
	// Lunes a viernes de 08:00 a 17:00; el viernes además de 22:00 a medianoche y el sábado de 00:00 a
	// 02:00 (una franja que cruza la medianoche); el viernes 6 de noviembre de 2026 es feriado.
	h := x.horario([]map[string]any{
		{"dias": []int{1, 2, 3, 4}, "inicio": "08:00", "fin": "17:00"},
		{"dias": []int{5}, "inicio": "08:00", "fin": "17:00"}, {"dias": []int{5}, "inicio": "22:00", "fin": "24:00"},
		{"dias": []int{6}, "inicio": "00:00", "fin": "02:00"},
	}, []map[string]any{{"fecha": "2026-11-06", "nombre": "Feriado", "franjas": []any{}}})
	x.bandeja.exigir(x.configurarNumero(h["id"], map[string]any{"responder": true, "mensaje": "Estamos cerrados. Te atendemos al abrir."}), http.StatusOK)
	x.exigir(x.configurar(x.versionLlamadas(), nil), http.StatusOK)
	llamar := func(cuando string) (num, call, estado string) {
		t.Helper()
		x.en(cuando)
		num, call = numeroCliente(), "wacid.horario"+numeroCliente()
		x.connect(num, call)
		_, estado, _ = x.llamada(call)
		return num, call, estado
	}
	cerrados := func(num string) int {
		return x.contar(`SELECT count(*) FROM mensajeria.conversacion_mensajes m JOIN mensajeria.conversaciones c ON c.id = m.conversacion_id
			WHERE c.numero = $1 AND m.direccion = 'saliente' AND m.texto = 'Estamos cerrados. Te atendemos al abrir.'`, "+"+num)
	}
	for _, c := range []struct{ cuando, estado, nota string }{
		{"2026-11-05 10:00", "sonando", "jueves en horario"},
		{"2026-11-05 17:30", "fuera_de_horario", "jueves 17:30 de Guatemala (23:30 UTC): la zona es la de Guatemala, no UTC"},
		{"2026-11-05 07:59", "fuera_de_horario", "un minuto antes de abrir"},
		{"2026-11-06 10:00", "fuera_de_horario", "feriado, aunque sea viernes a media mañana"},
		{"2026-11-13 23:30", "sonando", "viernes 23:30: dentro de la franja que cruza la medianoche"},
		{"2026-11-14 01:30", "sonando", "sábado 01:30: la misma franja, ya del otro lado"},
		{"2026-11-14 02:00", "fuera_de_horario", "sábado 02:00: cerró"},
		{"2026-11-15 12:00", "fuera_de_horario", "domingo"},
	} {
		if _, call, estado := llamar(c.cuando); estado != c.estado {
			t.Errorf("%s (%s): %s", c.nota, c.cuando, estado)
		} else if estado == "fuera_de_horario" && x.g.cuenta("reject:"+call) != 1 {
			t.Errorf("%s: debía rechazarse en WhatsApp", c.nota)
		}
	}
	// El cliente escribió hace poco (ventana de 24 h abierta) y llama con el horario cerrado: se rechaza
	// y sale el mensaje de fuera de horario, una sola vez por ventana cerrada, compartida con el chat.
	x.en("2026-11-16 16:00")
	num := numeroCliente()
	x.texto(num, "wamid.dia"+num, "Hola")
	x.en("2026-11-16 19:00")
	call := "wacid.cerrado" + num
	x.connect(num, call)
	id, estado, motivo := x.llamada(call)
	if estado != "fuera_de_horario" || motivo != "fuera_de_horario" || cerrados(num) != 1 || x.eventos("llamada.rechazada", id) != 1 {
		t.Fatalf("fuera de horario con mensaje: %s %s, mensajes %d", estado, motivo, cerrados(num))
	}
	x.pasa(time.Minute)
	x.connect(num, "wacid.cerrado2"+num)
	x.texto(num, "wamid.noche"+num, "¿Hay alguien?")
	if cerrados(num) != 1 {
		t.Fatalf("el mensaje de fuera de horario sale una vez por ventana cerrada: %d", cerrados(num))
	}
	if x.contar(`SELECT count(*) FROM mensajeria.llamada_devoluciones d JOIN mensajeria.llamadas l ON l.id = d.llamada_id WHERE l.id = $1::uuid`, id) != 0 {
		t.Fatal("una llamada fuera de horario no es una perdida: no crea devolución")
	}
	// Buzón de voz: Iris no toca la llamada y el recado que llega como audio queda marcado.
	x.exigir(x.configurar(x.versionLlamadas(), map[string]any{"fueraDeHorario": "buzon"}), http.StatusOK)
	x.en("2026-11-17 20:00")
	numB, callB := numeroCliente(), "wacid.buzon"+numeroCliente()
	x.connect(numB, callB)
	if _, estado, _ := x.llamada(callB); estado != "fuera_de_horario" || x.g.cuenta("reject:"+callB) != 0 {
		t.Fatalf("buzón: %s, acciones %v", estado, x.g.hechas())
	}
	x.pasa(2 * time.Minute)
	x.webhook(x.mensajeCliente(numB, "wamid.recado"+numB, `"type":"audio","audio":{"id":"medio-1","mime_type":"audio/ogg"}`))
	x.en("2026-11-18 10:00")
	x.webhook(x.mensajeCliente(numB, "wamid.nota"+numB, `"type":"audio","audio":{"id":"medio-2","mime_type":"audio/ogg"}`))
	var recado, nota bool
	for _, m := range x.mensajes(x.supervisor, cid(x.conv(numB))) {
		if m["tipo"] == "audio" {
			if m["buzonDeVoz"] == true {
				recado = true
			} else {
				nota = true
			}
		}
	}
	if !recado || !nota {
		t.Fatalf("buzón de voz: recado marcado %v, audio normal sin marcar %v", recado, nota)
	}
}

// Los ajustes del número se escriben en WhatsApp como un pendiente con reintentos y su estado se ve
// en GET /canales/{id}/llamadas; el horario de llamadas es el espejo del de Iris cuando cabe.
func TestLlamadasAjustesPendientesEnWhatsApp(t *testing.T) {
	x := nuevaBandejaVoz(t)
	ver := func() map[string]any {
		return x.exigir(x.api(x.supervisor, http.MethodGet, x.rutaCanal("/llamadas"), nil), http.StatusOK).cuerpo
	}
	if w := ver()["whatsapp"].(map[string]any); w["estado"] != "aplicado" || !x.g.ultimoFijado().Habilitadas || x.g.ultimoFijado().Horas != nil {
		t.Fatalf("recién encendidas: %v %+v", w, x.g.ultimoFijado())
	}
	// Con un horario que cabe en los límites de Meta, viaja como horario de llamadas.
	h := x.horario([]map[string]any{{"dias": []int{1, 2, 3, 4, 5}, "inicio": "08:00", "fin": "12:00"}, {"dias": []int{1, 2, 3, 4, 5}, "inicio": "14:00", "fin": "18:00"}},
		[]map[string]any{{"fecha": "2026-12-25", "nombre": "Navidad", "franjas": []any{}}})
	r := x.exigir(x.configurar(x.versionLlamadas(), map[string]any{"horarioId": h["id"]}), http.StatusOK)
	horas := x.g.ultimoFijado().Horas
	if horas == nil || horas.Zona != "America/Guatemala" || len(horas.Franjas) != 10 || horas.Franjas[0].Abre != "0800" || horas.Franjas[1].Cierra != "1800" ||
		len(horas.Feriados) != 1 || horas.Feriados[0].Fecha != "2026-12-25" {
		t.Fatalf("horario de llamadas en WhatsApp: %+v", horas)
	}
	if v := ver(); v["whatsapp"].(map[string]any)["estado"] != "aplicado" || v["horasWhatsapp"].(map[string]any)["zona"] != "America/Guatemala" {
		t.Fatalf("espejo: %v (%v)", v, r.cuerpo)
	}
	// Un horario con tres franjas en un día no cabe: WhatsApp queda abierto y el horario lo aplica Iris.
	h3 := x.horario([]map[string]any{{"dias": []int{1}, "inicio": "08:00", "fin": "10:00"}, {"dias": []int{1}, "inicio": "11:00", "fin": "13:00"},
		{"dias": []int{1}, "inicio": "15:00", "fin": "18:00"}}, nil)
	x.exigir(x.configurar(x.versionLlamadas(), map[string]any{"horarioId": h3["id"]}), http.StatusOK)
	if x.g.ultimoFijado().Horas != nil || ver()["horasWhatsapp"] != nil {
		t.Fatalf("un horario que no cabe no se manda: %+v", x.g.ultimoFijado().Horas)
	}
	// WhatsApp no responde: queda pendiente, visible, y se reintenta solo con espera creciente.
	x.g.falla("fijar", &mensajeria.ErrMetaLlamada{SinRespuesta: true})
	antes := len(x.g.fijados)
	x.exigir(x.configurar(x.versionLlamadas(), map[string]any{"iconoVisible": false}), http.StatusOK)
	w := ver()["whatsapp"].(map[string]any)
	if w["estado"] != "pendiente" || w["intentos"] != float64(1) || w["proximoIntentoEn"] == nil || w["detalle"] == nil || strings.Contains(fmt.Sprint(w["detalle"]), "meta:") {
		t.Fatalf("pendiente: %v", w)
	}
	x.pasa(30 * time.Second)
	x.mantener()
	if ver()["whatsapp"].(map[string]any)["intentos"] != float64(1) {
		t.Fatal("antes de tiempo no se reintenta")
	}
	x.g.falla("fijar", nil)
	x.pasa(31 * time.Second)
	x.mantener()
	if w := ver()["whatsapp"].(map[string]any); w["estado"] != "aplicado" || len(x.g.fijados) != antes+1 || x.g.ultimoFijado().IconoVisible {
		t.Fatalf("aplicado al reintentar: %v", w)
	}
	// Se agotan los reintentos: error, hasta que alguien pulse «Reintentar».
	x.g.falla("fijar", &mensajeria.ErrMetaLlamada{Status: 400})
	x.exigir(x.configurar(x.versionLlamadas(), map[string]any{"iconoVisible": true}), http.StatusOK)
	for range len(mensajeria.EsperasMeta) {
		x.pasa(13 * time.Hour)
		x.mantener()
	}
	if w := ver()["whatsapp"].(map[string]any); w["estado"] != "error" || w["proximoIntentoEn"] != nil {
		t.Fatalf("agotado: %v", w)
	}
	x.pasa(13 * time.Hour)
	x.mantener() // en error ya no se reintenta solo
	x.g.falla("fijar", nil)
	x.exigir(x.api(x.agente, http.MethodPost, x.rutaCanal("/llamadas/aplicar"), nil), http.StatusForbidden)
	if r := x.exigir(x.api(x.supervisor, http.MethodPost, x.rutaCanal("/llamadas/aplicar"), nil), http.StatusOK); r.cuerpo["whatsapp"].(map[string]any)["estado"] != "aplicado" ||
		!x.g.ultimoFijado().IconoVisible {
		t.Fatalf("reintentar: %v", r.cuerpo)
	}
}

// Seguridad (contratos/voz.md §6.2, filas que tocan E1): webhook sin firma o de otro número, cruce de
// empresas, eventos sin número ni SDP, y la grabación que todavía no se puede encender.
func TestLlamadaSeguridadE1(t *testing.T) {
	x := nuevaBandejaVoz(t)
	x.recibe(x.agente)
	num, call := numeroCliente(), "wacid.seg"+numeroCliente()
	// Fila 10: sin firma, con la firma de otro secreto, o de otro phone_number_id: no crea nada.
	cuerpo := x.eventoLlamada(num, call, "connect", fmt.Sprintf(`,"session":{"sdp_type":"offer","sdp":%q}`, sdpOfertaMeta), 0)
	for nombre, firma := range map[string]string{"sin firma": "", "firma de otro secreto": mensajeria.FirmarMeta("otro-secreto-de-app", []byte(cuerpo))} {
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("%s/canales/%d/webhook", rutaMsj, x.canal.ID), strings.NewReader(cuerpo))
		req.Header.Set("Content-Type", "application/json")
		if firma != "" {
			req.Header.Set("X-Hub-Signature-256", firma)
		}
		w := httptest.NewRecorder()
		x.h.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("%s: %d", nombre, w.Code)
		}
	}
	x.webhook(strings.Replace(cuerpo, fmt.Sprintf(`"phone_number_id":%q`, x.pnid), `"phone_number_id":"999000111"`, 1))
	if n := x.contar(`SELECT count(*) FROM mensajeria.llamadas WHERE id_externo = $1`, call); n != 0 {
		t.Fatalf("webhook falso o de otro número creó una llamada: %d", n)
	}
	// Fila 1: una persona de otra empresa no ve ni toca la llamada, ni siquiera con el id.
	x.connect(num, call)
	id, _, _ := x.llamada(call)
	ajena := x.persona([]int64{2}, rolBase("supervisor"))
	for _, ruta := range []string{"/reclamar", "/colgar", "/confirmar-medio"} {
		x.exigir(x.api(ajena, http.MethodPost, "/llamadas/"+id+ruta, nil), http.StatusNotFound)
	}
	x.exigir(x.api(ajena, http.MethodPost, "/llamadas/"+id+"/rechazar", map[string]any{"paraTodos": true}), http.StatusNotFound)
	x.exigir(x.api(ajena, http.MethodPost, "/llamadas/"+id+"/contestar", map[string]any{"sdpRespuesta": sdpNavegador("")}), http.StatusNotFound)
	x.exigir(x.api(ajena, http.MethodGet, "/llamadas/"+id, nil), http.StatusNotFound)
	x.exigir(x.api(ajena, http.MethodGet, x.rutaCanal("/llamadas"), nil), http.StatusNotFound)
	// (otra prueba puede haber dejado llamadas de la empresa 2: lo que no debe salir es nada de la 1)
	for _, f := range x.exigir(x.api(ajena, http.MethodGet, "/llamadas", nil), http.StatusOK).cuerpo["data"].([]any) {
		if f.(map[string]any)["id"] == id || f.(map[string]any)["empresaId"] != float64(2) {
			t.Fatalf("lista de otra empresa: %v", f)
		}
	}
	if _, estado, _ := x.llamada(call); estado != "sonando" {
		t.Fatalf("la llamada no debía cambiar: %s", estado)
	}
	// Un rol sin llamadas.contestar (técnico) y uno de solo lectura.
	x.exigir(x.api(x.tecnico, http.MethodGet, "/llamadas/"+id, nil), http.StatusForbidden)
	// Lectura tiene llamadas.ver, pero una llamada solo se ve si se ve su conversación.
	x.exigir(x.api(x.lectura, http.MethodGet, "/llamadas/"+id, nil), http.StatusNotFound)
	x.exigir(x.api(x.supervisor, http.MethodGet, "/llamadas/"+id, nil), http.StatusOK)
	x.exigir(x.api(x.lectura, http.MethodPost, "/llamadas/recibir/encender", map[string]any{"navegador": "navegador-lectura"}), http.StatusForbidden)
	// Eventos y auditoría: solo ids, estados y códigos; nunca el número del cliente ni el SDP.
	x.contestar(x.agente, id)
	x.exigir(x.api(x.agente, http.MethodPost, "/llamadas/"+id+"/colgar", nil), http.StatusOK)
	if n := x.contar(`SELECT count(*) FROM integracion.outbox WHERE evento LIKE 'llamada.%' AND (payload::text LIKE '%' || $1 || '%' OR payload::text LIKE '%fingerprint%')`, num); n != 0 {
		t.Fatalf("eventos de llamada con el número o el SDP: %d", n)
	}
	if n := x.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion LIKE 'llamadas.%' AND (entrada::text LIKE '%' || $1 || '%' OR entrada::text LIKE '%fingerprint%')`, num); n != 0 {
		t.Fatalf("auditoría de llamadas con el número o el SDP: %d", n)
	}
	// Grabación (E1c): no se ofrece ni se acepta, y nunca se pide a Meta.
	admin := x.persona([]int64{1}, rolBase("admin"))
	admin.empresa = 1
	if v := x.exigir(x.api(admin, http.MethodGet, "/voz/ajustes", nil), http.StatusOK); v.cuerpo["grabar"] != "nunca" {
		t.Fatalf("ajustes: %v", v.cuerpo)
	}
	for _, g := range []string{"todas", "asistente"} {
		r := x.exigir(x.api(admin, http.MethodPost, "/voz/ajustes/actualizar", map[string]any{"version": 0, "grabar": g, "duracionMaxPersonaMin": 60, "segundosAsignado": 10}),
			http.StatusUnprocessableEntity)
		if !strings.Contains(crudo(r), "grabacion_no_disponible") {
			t.Fatalf("grabar %s: %s", g, crudo(r))
		}
	}
	if n := x.contar(`SELECT count(*) FROM mensajeria.llamadas WHERE grabada`) + x.contar(`SELECT count(*) FROM mensajeria.llamada_grabaciones`); n != 0 {
		t.Fatalf("ninguna llamada se graba todavía: %d", n)
	}
}

// La reserva es de un usuario EN un navegador: la misma persona en dos navegadores no se estorba.
func TestLlamadaReservaPorNavegador(t *testing.T) {
	x := nuevaBandejaVoz(t)
	x.recibe(x.agente, x.supervisor)
	otroNav := x.agente
	otroNav.navegador = fmt.Sprintf("navegador-b-%d", x.agente.id)
	x.exigir(x.api(otroNav, http.MethodPost, "/llamadas/recibir/encender", map[string]any{"navegador": otroNav.navegador}), http.StatusOK)
	sinNav := x.agente
	sinNav.navegador = ""
	num, call := numeroCliente(), "wacid.nav"+numeroCliente()
	x.connect(num, call)
	id, _, _ := x.llamada(call)
	// Sin el id del navegador no se reserva.
	if r := x.exigir(x.api(sinNav, http.MethodPost, "/llamadas/"+id+"/reclamar", nil), http.StatusUnprocessableEntity); !strings.Contains(crudo(r), "navegador_requerido") {
		t.Fatalf("reclamar sin navegador: %s", crudo(r))
	}
	x.exigir(x.api(x.agente, http.MethodPost, "/llamadas/"+id+"/reclamar", nil), http.StatusOK)
	// El otro navegador de la misma persona: 409 misma_persona al reclamar (ni siquiera dentro de la
	// reserva recibe la oferta), y sus soltar, contestar y confirmar-medio no tocan la reserva.
	if r := x.exigir(x.api(otroNav, http.MethodPost, "/llamadas/"+id+"/reclamar", nil), http.StatusConflict); !strings.Contains(crudo(r), "misma_persona") || strings.Contains(crudo(r), "fingerprint") {
		t.Fatalf("reclamar desde el otro navegador: %s", crudo(r))
	}
	for _, p := range []persona{otroNav, sinNav} {
		for ruta, cuerpo := range map[string]any{"/soltar": map[string]any{"motivo": "sin_microfono"}, "/contestar": map[string]any{"sdpRespuesta": sdpNavegador("")}, "/confirmar-medio": nil} {
			if r := x.exigir(x.api(p, http.MethodPost, "/llamadas/"+id+ruta, cuerpo), http.StatusConflict); !strings.Contains(crudo(r), "otro_navegador") {
				t.Fatalf("%s desde otro navegador: %s", ruta, crudo(r))
			}
		}
	}
	if n := x.contar(`SELECT count(*) FROM mensajeria.llamadas WHERE id = $1::uuid AND estado = 'conectando' AND reclamada_por = $2 AND reclamada_navegador = $3`,
		id, x.agente.id, x.agente.navegador); n != 1 || len(x.g.hechas()) != 0 {
		t.Fatalf("la reserva del primer navegador debía seguir intacta: %v", x.g.hechas())
	}
	// El primero sigue y contesta; repetir reclamar desde él devuelve la oferta.
	if r := x.exigir(x.api(x.agente, http.MethodPost, "/llamadas/"+id+"/reclamar", nil), http.StatusOK); r.cuerpo["sdpOferta"] != sdpOfertaMeta {
		t.Fatalf("repetir reclamar en el mismo navegador: %s", crudo(r))
	}
	x.exigir(x.api(x.agente, http.MethodPost, "/llamadas/"+id+"/contestar", map[string]any{"sdpRespuesta": sdpNavegador("")}), http.StatusOK)
	x.exigir(x.api(otroNav, http.MethodPost, "/llamadas/"+id+"/confirmar-medio", nil), http.StatusConflict)
	x.exigir(x.api(x.agente, http.MethodPost, "/llamadas/"+id+"/confirmar-medio", nil), http.StatusOK)
	if n := x.contar(`SELECT count(*) FROM mensajeria.llamadas WHERE id = $1::uuid AND estado = 'en_curso' AND reclamada_navegador IS NULL`, id); n != 1 {
		t.Fatal("en curso, ya sin reserva")
	}
	// Colgar: quien la tiene, desde cualquier navegador.
	if r := x.exigir(x.api(otroNav, http.MethodPost, "/llamadas/"+id+"/colgar", nil), http.StatusOK); r.cuerpo["estado"] != "terminada" {
		t.Fatalf("colgar desde el otro navegador: %s", crudo(r))
	}
	// Soltar desde el navegador de la reserva sí la libera, y limpia el navegador.
	num2, call2 := numeroCliente(), "wacid.nav2"+numeroCliente()
	x.pasa(time.Second, x.agente, x.supervisor)
	x.connect(num2, call2)
	id2, _, _ := x.llamada(call2)
	x.exigir(x.api(otroNav, http.MethodPost, "/llamadas/"+id2+"/reclamar", nil), http.StatusOK)
	x.exigir(x.api(x.agente, http.MethodPost, "/llamadas/"+id2+"/soltar", map[string]any{}), http.StatusConflict)
	x.exigir(x.api(otroNav, http.MethodPost, "/llamadas/"+id2+"/soltar", map[string]any{}), http.StatusOK)
	if n := x.contar(`SELECT count(*) FROM mensajeria.llamadas WHERE id = $1::uuid AND estado = 'sonando' AND reclamada_por IS NULL AND reclamada_navegador IS NULL`, id2); n != 1 {
		t.Fatal("soltada desde su navegador")
	}
	// Supervisor (§3.3-8 y §8.2, ya en E1): cuelga la llamada de otra persona con llamadas.colgar; otro agente, no.
	x.contestar(x.supervisor, id2)
	x.exigir(x.api(x.agente, http.MethodPost, "/llamadas/"+id2+"/colgar", nil), http.StatusNotFound)
	num3, call3 := numeroCliente(), "wacid.nav3"+numeroCliente()
	x.pasa(time.Second, x.agente, x.supervisor)
	x.exigir(x.api(x.supervisor, http.MethodPost, "/llamadas/"+id2+"/colgar", nil), http.StatusOK)
	x.connect(num3, call3)
	id3, _, _ := x.llamada(call3)
	x.contestar(x.agente, id3)
	if r := x.exigir(x.api(x.supervisor, http.MethodPost, "/llamadas/"+id3+"/colgar", nil), http.StatusOK); r.cuerpo["estado"] != "terminada" || x.g.cuenta("terminate:"+call3) != 1 {
		t.Fatalf("el supervisor cuelga la llamada de otra persona: %s", crudo(r))
	}
}

// Señales del cliente en la llamada: servicio, ticket abierto y «tiene saldo vencido» (solo un
// booleano, solo en GET /llamadas/{id} y solo para quien puede ver el estado de cuenta).
func TestLlamadaSenalesDelCliente(t *testing.T) {
	x := nuevaBandejaVoz(t)
	x.recibe(x.agente)
	num, call := numeroCliente(), "wacid.senales"+numeroCliente()
	x.texto(num, "wamid.senales"+num, "Hola")
	conv := cid(x.conv(num))
	var cliente int64
	if err := x.bd.QueryRow(x.ctx, `SELECT coalesce(max(id), 0) + 1000 FROM integracion.argos_clientes`).Scan(&cliente); err != nil {
		t.Fatal(err)
	}
	x.sql(`INSERT INTO integracion.argos_clientes (id, empresa_id, nombre, sincronizado_en, empresas) VALUES ($1, 1, 'Ana Señales', now(), '{1}')`, cliente)
	x.sql(`INSERT INTO integracion.argos_servicios (id, empresa_id, cliente_id, codigo, plan_nombre, estado, sincronizado_en) VALUES ($1, 1, $2, 'SRV-9', 'Fibra 100', 'suspendido', now())`, cliente*10, cliente)
	x.exigir(x.api(x.agente, http.MethodPost, "/conversaciones/"+conv+"/asociar-cliente", map[string]any{"version": ver(x.conv(num)), "clienteId": cliente, "servicioId": cliente * 10}), http.StatusOK)
	x.sql(`INSERT INTO soporte.tickets (prioridad, origen, asunto, sla_inicio, empresa_id, cliente_id) VALUES ('media', 'panel', 'Sin internet desde ayer', now(), 1, $1)`, cliente)
	x.argos.mu.Lock()
	x.argos.cuenta = &argos.EstadoCuenta{Saldo: ptrTexto("350.00"), Moneda: "GTQ", FacturasVencidas: ptrEntero(2), MontoVencido: ptrTexto("350.00")}
	x.argos.mu.Unlock()
	x.connect(num, call)
	id, estado, _ := x.llamada(call)
	if estado != "sonando" {
		t.Fatalf("la llamada debía sonar: %s", estado)
	}
	// En la lista (lo que lee el aviso): servicio y ticket, sin saldo y sin llamar a Argos.
	antes := x.argos.cuentas
	l := x.exigir(x.api(x.agente, http.MethodGet, "/llamadas?estado=sonando", nil), http.StatusOK).cuerpo["data"].([]any)
	var s map[string]any
	for _, f := range l {
		if f.(map[string]any)["id"] == id {
			s, _ = f.(map[string]any)["senales"].(map[string]any)
		}
	}
	if s == nil || s["servicio"].(map[string]any)["plan"] != "Fibra 100" || s["servicio"].(map[string]any)["estado"] != "suspendido" ||
		s["ticketAbierto"].(map[string]any)["asunto"] != "Sin internet desde ayer" || s["saldoVencido"] != nil || x.argos.cuentas != antes {
		t.Fatalf("señales en la lista: %v", s)
	}
	// Al ver la llamada, quien puede ver el estado de cuenta recibe el booleano; nunca montos.
	v := x.exigir(x.api(x.agente, http.MethodGet, "/llamadas/"+id, nil), http.StatusOK)
	if v.cuerpo["senales"].(map[string]any)["saldoVencido"] != true || strings.Contains(crudo(v), "350") || strings.Contains(crudo(v), "GTQ") {
		t.Fatalf("saldo vencido: %s", crudo(v))
	}
	// Despacho contesta llamadas pero no ve saldos: el campo no viaja y no se le pregunta a Argos.
	despacho := x.persona([]int64{1}, rolBase("despacho"))
	x.sql(`INSERT INTO mensajeria.cola_miembros (cola_id, usuario_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, x.cola, despacho.id)
	antes = x.argos.cuentas
	v = x.exigir(x.api(despacho, http.MethodGet, "/llamadas/"+id, nil), http.StatusOK)
	if sd := v.cuerpo["senales"].(map[string]any); sd["saldoVencido"] != nil || sd["servicio"] == nil || x.argos.cuentas != antes {
		t.Fatalf("despacho no ve el saldo: %s", crudo(v))
	}
	// Al día: false. Argos caído: no se sabe (el campo no viaja).
	x.argos.mu.Lock()
	x.argos.cuenta = &argos.EstadoCuenta{Saldo: ptrTexto("0.00"), Moneda: "GTQ", FacturasVencidas: ptrEntero(0)}
	x.argos.mu.Unlock()
	if v := x.exigir(x.api(x.agente, http.MethodGet, "/llamadas/"+id, nil), http.StatusOK); v.cuerpo["senales"].(map[string]any)["saldoVencido"] != false {
		t.Fatalf("al día: %s", crudo(v))
	}
	x.argos.mu.Lock()
	x.argos.cuenta = nil
	x.argos.mu.Unlock()
	if v := x.exigir(x.api(x.agente, http.MethodGet, "/llamadas/"+id, nil), http.StatusOK); v.cuerpo["senales"].(map[string]any)["saldoVencido"] != nil {
		t.Fatalf("Argos no responde: %s", crudo(v))
	}
	// Terminada: ya no lleva señales.
	x.webhook(x.eventoLlamada(num, call, "terminate", "", 0))
	if v := x.exigir(x.api(x.agente, http.MethodGet, "/llamadas/"+id, nil), http.StatusOK); v.cuerpo["senales"] != nil {
		t.Fatalf("llamada terminada con señales: %s", crudo(v))
	}
}

func ptrTexto(s string) *string { return &s }
func ptrEntero(n int) *int      { return &n }

// El job de escrituras a Meta sigue tomando lotes dentro del mismo ciclo: con más pendientes que un
// lote, ninguna espera al ciclo siguiente.
func TestLlamadaMetaVariosLotes(t *testing.T) {
	x := nuevaBandejaVoz(t)
	x.recibe(x.agente)
	var calls []string
	for i := range 3*mensajeria.LoteMetaVoz + 2 {
		c := fmt.Sprintf("wacid.lote%d%s", i, numeroCliente())
		calls = append(calls, c)
		x.connect(numeroCliente(), c)
	}
	x.pasa(31*time.Second, x.agente)
	x.soloMantener()
	x.despachar()
	for _, c := range calls {
		if _, estado, _ := x.llamada(c); estado != "perdida" || x.g.cuenta("reject:"+c) != 1 {
			t.Fatalf("%s: %s, reject %d", c, estado, x.g.cuenta("reject:"+c))
		}
	}
}
