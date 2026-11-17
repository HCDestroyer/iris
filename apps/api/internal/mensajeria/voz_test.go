package mensajeria_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

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
}

func (g *graphFalso) Accion(_ context.Context, canal mensajeria.CanalGraph, callID, accion, sdp string, _ bool) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.acciones = append(g.acciones, accion+":"+callID)
	g.tokens = append(g.tokens, canal.Token)
	if sdp != "" {
		g.sdps = append(g.sdps, sdp)
	}
	return g.fallar[accion]
}

func (g *graphFalso) Ajustes(context.Context, mensajeria.CanalGraph) (mensajeria.AjustesMetaLlamadas, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.ajustes, g.fallar["ajustes"]
}

func (g *graphFalso) FijarAjustes(_ context.Context, _ mensajeria.CanalGraph, a mensajeria.AjustesMetaLlamadas) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.fijados = append(g.fijados, a)
	return nil
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

const sdpOfertaMeta = "v=0\r\no=- 7 2 IN IP4 127.0.0.1\r\ns=-\r\nt=0 0\r\nm=audio 3480 UDP/TLS/RTP/SAVPF 111 126\r\nc=IN IP4 157.240.19.130\r\n" +
	"a=rtpmap:111 opus/48000/2\r\na=rtpmap:126 telephone-event/8000\r\na=setup:actpass\r\na=fingerprint:sha-256 AA:BB:CC\r\n" +
	"a=candidate:1 1 udp 2122260223 157.240.19.130 3480 typ host\r\n"

func sdpNavegador(extra string) string {
	return "v=0\r\no=- 1 2 IN IP4 127.0.0.1\r\ns=-\r\nt=0 0\r\nm=audio 9 UDP/TLS/RTP/SAVPF 111 126\r\nc=IN IP4 0.0.0.0\r\n" +
		"a=rtpmap:111 opus/48000/2\r\na=rtpmap:126 telephone-event/8000\r\na=setup:active\r\na=fingerprint:sha-256 0F:1E:2D\r\n" +
		"a=candidate:1 1 udp 2113937151 203.0.113.7 54400 typ srflx\r\n" + extra
}

type bandejaVoz struct {
	*bandeja
	g *graphFalso
}

// nuevaBandejaVoz: la bandeja con el doble de la Graph API y las llamadas encendidas en el número.
func nuevaBandejaVoz(t *testing.T) *bandejaVoz {
	t.Helper()
	g := &graphFalso{fallar: map[string]error{}, ajustes: mensajeria.AjustesMetaLlamadas{Habilitadas: true, IconoVisible: true, LimiteMensajeria: 10000, WebhookCalls: true}}
	x := &bandejaVoz{bandeja: nuevaBandeja(t, func(c *mensajeria.Config) { c.Llamadas = g }), g: g}
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

func (x *bandejaVoz) mantener() {
	x.t.Helper()
	if err := x.m.MantenerLlamadas(x.ctx, x.bd); err != nil {
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
		"con video":          sdpNavegador("m=video 9 UDP/TLS/RTP/SAVPF 96\r\na=rtpmap:96 VP8/90000\r\n"),
		"con datos":          sdpNavegador("m=application 9 UDP/DTLS/SCTP webrtc-datachannel\r\n"),
		"candidato loopback": sdpNavegador("a=candidate:2 1 udp 1 127.0.0.1 9 typ host\r\n"),
		"candidato metadata": sdpNavegador("a=candidate:2 1 udp 1 169.254.169.254 9 typ host\r\n"),
		"candidato privado":  sdpNavegador("a=candidate:2 1 udp 1 10.0.0.8 9 typ host\r\n"),
		"otro códec":         strings.Replace(sdpNavegador(""), "opus/48000/2", "PCMU/8000", 1),
		"sin huella":         strings.Replace(sdpNavegador(""), "a=fingerprint:sha-256 0F:1E:2D\r\n", "", 1),
		"enorme":             sdpNavegador(strings.Repeat("a=x:"+strings.Repeat("y", 90)+"\r\n", 90)),
	} {
		if r := x.api(x.agente, http.MethodPost, "/llamadas/"+id+"/contestar", map[string]any{"sdpRespuesta": malo}); r.status != http.StatusUnprocessableEntity {
			t.Fatalf("SDP %s: %d %s", nombre, r.status, crudo(r))
		}
	}
	if len(x.g.hechas()) != 0 {
		t.Fatalf("un SDP inválido no llega a Meta: %v", x.g.hechas())
	}
	x.exigir(x.api(x.agente, http.MethodPost, "/llamadas/"+id+"/confirmar-medio", nil), http.StatusConflict) // aún sin pre_accept
	c := x.exigir(x.api(x.agente, http.MethodPost, "/llamadas/"+id+"/contestar", map[string]any{"sdpRespuesta": sdpNavegador("")}), http.StatusOK)
	if c.cuerpo["estado"] != "conectando" || x.g.cuenta("pre_accept:"+call) != 1 || len(x.g.sdps) != 1 || x.g.sdps[0] != sdpNavegador("") || strings.Contains(crudo(c), "fingerprint") {
		t.Fatalf("contestar: %s %v", crudo(c), x.g.hechas())
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
	if n := x.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'llamadas.contestar' AND entrada->>'sdp_sha256' IS NOT NULL AND actor_id = $1`, x.agente.id); n == 0 {
		t.Fatal("la auditoría de contestar debía llevar la huella del SDP")
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
	if mia == nil || mia["llamadaId"] != id || mia["pedidaPor"] != "sistema" {
		t.Fatalf("por devolver: %v", d)
	}
	ruta := fmt.Sprintf("/llamadas/devoluciones/%d/descartar", int64(mia["id"].(float64)))
	x.exigir(x.api(x.lectura, http.MethodPost, ruta, map[string]any{"motivo": "x"}), http.StatusForbidden)
	if r := x.exigir(x.api(x.agente, http.MethodPost, ruta, map[string]any{"motivo": "Ya se resolvió por chat"}), http.StatusOK); r.cuerpo["estado"] != "descartada" {
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
	x.exigir(x.api(x.supervisor, http.MethodPost, "/voz/ajustes/actualizar", map[string]any{"version": 0, "grabar": "nunca", "duracionMaxPersonaMin": 10,
		"segundosAsignado": 0, "retencionGrabacionesDias": 30}), http.StatusForbidden)
	admin := x.persona([]int64{1}, rolBase("admin"))
	admin.empresa = 1 // opera en todas: indica cuál con X-Iris-Empresa
	aj := x.exigir(x.api(admin, http.MethodPost, "/voz/ajustes/actualizar", map[string]any{"version": 0, "grabar": "nunca", "duracionMaxPersonaMin": 10,
		"segundosAsignado": 0, "retencionGrabacionesDias": 30}), http.StatusOK)
	if aj.cuerpo["version"] != float64(1) || x.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'voz.ajustes_cambiados' AND payload ? 'campos'`) == 0 {
		t.Fatalf("ajustes de voz: %s", crudo(aj))
	}
	x.exigir(x.api(admin, http.MethodPost, "/voz/ajustes/actualizar", map[string]any{"version": 0, "grabar": "nunca", "duracionMaxPersonaMin": 10,
		"segundosAsignado": 0, "retencionGrabacionesDias": 30}), http.StatusConflict)
	x.exigir(x.api(admin, http.MethodPost, "/voz/ajustes/actualizar", map[string]any{"version": 1, "grabar": "nunca", "duracionMaxPersonaMin": 500,
		"segundosAsignado": 0, "retencionGrabacionesDias": 30}), http.StatusUnprocessableEntity)
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
	g.ajustes = mensajeria.AjustesMetaLlamadas{Habilitadas: true, IconoVisible: true, LimiteMensajeria: 2000, WebhookCalls: true}
	g.mu.Unlock()
	c = x.exigir(x.api(x.supervisor, http.MethodPost, x.rutaCanal("/llamadas/comprobar"), nil), http.StatusOK)
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

func TestValidarSDP(t *testing.T) {
	if err := mensajeria.ValidarSDP(sdpNavegador("")); err != nil {
		t.Fatalf("SDP de navegador válido: %v", err)
	}
	if err := mensajeria.ValidarSDP(sdpNavegador("a=candidate:3 1 udp 1 abcd1234-0000.local 9 typ host\r\n")); err != nil {
		t.Fatalf("candidato mDNS del navegador: %v", err)
	}
	for nombre, malo := range map[string]string{
		"vacío":              "",
		"línea de control":   sdpNavegador("a=x:\x00\r\n"),
		"candidato IPv6 ::1": sdpNavegador("a=candidate:2 1 udp 1 ::1 9 typ host\r\n"),
		"candidato fe80":     sdpNavegador("a=candidate:2 1 udp 1 fe80::1 9 typ host\r\n"),
		"nombre arbitrario":  sdpNavegador("a=candidate:2 1 udp 1 interno.ejemplo 9 typ host\r\n"),
		"sin audio":          "v=0\r\ns=-\r\na=setup:active\r\na=fingerprint:sha-256 AA\r\n",
		"sin setup":          strings.Replace(sdpNavegador(""), "a=setup:active\r\n", "", 1),
		"RTP sin cifrar":     strings.Replace(sdpNavegador(""), "UDP/TLS/RTP/SAVPF", "RTP/AVP", 1),
	} {
		if err := mensajeria.ValidarSDP(malo); err == nil {
			t.Errorf("%s: debía rechazarse", nombre)
		}
	}
}
