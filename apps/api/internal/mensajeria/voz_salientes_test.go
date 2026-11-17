package mensajeria_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/HCDestroyer/iris/apps/api/internal/mensajeria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
)

// Voz E1b (contratos/voz.md §2.5 y §3.7): llamadas salientes y permiso de llamada, contra Postgres
// real. El doble de Graph sigue los supuestos de §10.1-5: connect con la oferta SDP devuelve el
// call_id; la respuesta SDP llega por webhook (connect, BUSINESS_INITIATED); los estados RINGING,
// ACCEPTED y REJECTED llegan en statuses del campo calls; call_permissions dice si se puede llamar.

func (g *graphFalso) Llamar(_ context.Context, canal mensajeria.CanalGraph, numero, sdp string) (string, error) {
	g.mu.Lock()
	g.salientes++
	id := g.proximaSaliente
	if id == "" {
		id = fmt.Sprintf("wacid.saliente%d.%s", g.salientes, strings.TrimPrefix(numero, "+"))
	}
	g.proximaSaliente = ""
	g.ultimaSaliente = id
	g.acciones = append(g.acciones, "connect:"+numero)
	g.tokens = append(g.tokens, canal.Token)
	g.ofertas = append(g.ofertas, sdp)
	err, gancho := g.fallar["connect"], g.alAccionar
	g.mu.Unlock()
	if gancho != nil {
		gancho("connect")
	}
	if err != nil {
		return "", err
	}
	return id, nil
}

func (g *graphFalso) Permiso(context.Context, mensajeria.CanalGraph, string) (mensajeria.PermisoMeta, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.permisosLeidos++
	return g.permiso, g.fallar["permiso"]
}

func (g *graphFalso) Plantilla(_ context.Context, _ mensajeria.CanalGraph, nombre, idioma string) (mensajeria.PlantillaMetaInfo, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.plantillas[nombre+"|"+idioma], g.fallar["plantilla"]
}

func (g *graphFalso) ultima() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.ultimaSaliente
}

// ofertaNavegador: la oferta SDP del navegador de quien llama (con su dirección local, que se quita).
func ofertaNavegador() string {
	return strings.Replace(sdpNavegador("a=candidate:9 1 udp 1 192.168.1.50 9 typ host\r\n"), "a=setup:active", "a=setup:actpass", 1)
}

// respuestaMeta: la respuesta SDP de Meta a una saliente.
func respuestaMeta() string {
	return strings.Replace(sdpOfertaMeta, "a=setup:actpass", "a=setup:passive", 1)
}

// webhookSaliente: un evento de calls[] de una saliente (connect con la respuesta SDP, o terminate).
func (x *bandejaVoz) webhookSaliente(para, callID, evento, extra string) {
	x.t.Helper()
	x.webhook(fmt.Sprintf(`{"object":"whatsapp_business_account","entry":[{"changes":[{"field":"calls","value":{"metadata":{"phone_number_id":%q},`+
		`"calls":[{"id":%q,"from":"50222000010","to":%q,"event":%q,"timestamp":"%d","direction":"BUSINESS_INITIATED"%s}]}}]}]}`,
		x.pnid, callID, para, evento, x.reloj.Unix(), extra))
}

func (x *bandejaVoz) respuestaSaliente(para, callID string) {
	x.t.Helper()
	x.webhookSaliente(para, callID, "connect", fmt.Sprintf(`,"session":{"sdp_type":"answer","sdp":%q}`, respuestaMeta()))
}

// estadoSaliente: RINGING, ACCEPTED o REJECTED de una saliente (statuses del campo calls).
func (x *bandejaVoz) estadoSaliente(para, callID, estado string) {
	x.t.Helper()
	x.webhook(fmt.Sprintf(`{"object":"whatsapp_business_account","entry":[{"changes":[{"field":"calls","value":{"metadata":{"phone_number_id":%q},`+
		`"statuses":[{"id":%q,"timestamp":"%d","type":"call","status":%q,"recipient_id":%q}]}}]}]}`, x.pnid, callID, x.reloj.Unix(), estado, para))
}

// darPermiso: el cliente responde a la solicitud de permiso, y «Meta» pasa a decir lo mismo.
func (x *bandejaVoz) darPermiso(num, respuesta string, permanente bool) {
	x.t.Helper()
	vence := x.reloj.Add(7 * 24 * time.Hour)
	x.webhook(x.mensajeCliente(num, "wamid.permiso"+uuid.NewString(), fmt.Sprintf(`"type":"interactive","interactive":{"type":"call_permission_reply",`+
		`"call_permission_reply":{"response":%q,"is_permanent":%v,"expiration_timestamp":%d,"response_source":"user_action"}}`, respuesta, permanente, vence.Unix())))
	p := mensajeria.PermisoMeta{Estado: "sin_pedir"}
	if respuesta == "accept" {
		p = mensajeria.PermisoMeta{Estado: "temporal", VenceEn: &vence, PuedeLlamar: true}
		if permanente {
			p = mensajeria.PermisoMeta{Estado: "permanente", PuedeLlamar: true}
		}
	}
	x.g.mu.Lock()
	x.g.permiso = p
	x.g.mu.Unlock()
}

func (x *bandejaVoz) llamar(p persona, conv string, extra map[string]any) respuesta {
	c := map[string]any{"sdpOferta": ofertaNavegador()}
	for k, v := range extra {
		c[k] = v
	}
	return x.api(p, http.MethodPost, "/conversaciones/"+conv+"/llamar", c)
}

// saliente: una llamada saliente recién hecha (marcando), con su id y su call_id.
func (x *bandejaVoz) saliente(p persona, conv string, extra map[string]any) (id, callID string) {
	x.t.Helper()
	r := x.exigir(x.llamar(p, conv, extra), http.StatusCreated)
	if r.cuerpo["estado"] != "marcando" || r.cuerpo["direccion"] != "saliente" {
		x.t.Fatalf("saliente: %s", crudo(r))
	}
	return r.cuerpo["id"].(string), x.g.ultima()
}

// limitesVoz fija los límites de salientes de la empresa 1 por la API (como admin).
func (x *bandejaVoz) limitesVoz(porDia, minutosMes int) {
	x.t.Helper()
	admin := x.persona([]int64{1}, rolBase("admin"))
	admin.empresa = 1
	v := x.exigir(x.api(admin, http.MethodGet, "/voz/ajustes", nil), http.StatusOK)
	x.exigir(x.api(admin, http.MethodPost, "/voz/ajustes/actualizar", map[string]any{"version": v.cuerpo["version"], "duracionMaxPersonaMin": v.cuerpo["duracionMaxPersonaMin"],
		"segundosAsignado": v.cuerpo["segundosAsignado"], "maxSalientesPorDia": porDia, "topeMinutosSalientesMes": minutosMes}), http.StatusOK)
}

// convConPermiso: una conversación con la ventana abierta cuyo contacto ya dio permiso de llamada.
func (x *bandejaVoz) convConPermiso() (num, conv string) {
	x.t.Helper()
	num = numeroCliente()
	x.texto(num, "wamid.sal"+num, "Hola, ¿me pueden llamar?")
	conv = cid(x.conv(num))
	x.exigir(x.api(x.agente, http.MethodPost, "/conversaciones/"+conv+"/solicitar-permiso-llamada", nil), http.StatusOK)
	x.darPermiso(num, "accept", false)
	return num, conv
}

func TestLlamadaSaliente(t *testing.T) {
	x := nuevaBandejaVoz(t)
	x.limitesVoz(1000, 100000)
	num := numeroCliente()
	x.texto(num, "wamid.s1"+num, "Hola")
	conv := cid(x.conv(num))
	ruta := "/conversaciones/" + conv

	// Sin pedir: se puede solicitar; nadie puede llamar.
	p := x.exigir(x.api(x.agente, http.MethodGet, ruta+"/permiso-llamada", nil), http.StatusOK)
	if p.cuerpo["estado"] != "sin_pedir" || p.cuerpo["puedeSolicitar"] != true || p.cuerpo["puedeLlamar"] != false || p.cuerpo["motivo"] != "sin_permiso_llamada" {
		t.Fatalf("permiso de fábrica: %s", crudo(p))
	}
	// llamadas.llamar es solo de supervisor y admin; solicitar, también de soporte. Lectura, ninguno.
	x.exigir(x.llamar(x.agente, conv, nil), http.StatusForbidden)
	x.exigir(x.api(x.lectura, http.MethodPost, ruta+"/solicitar-permiso-llamada", nil), http.StatusForbidden)
	x.exigir(x.api(x.otro, http.MethodPost, ruta+"/solicitar-permiso-llamada", nil), http.StatusNotFound) // no ve la conversación
	// Sin permiso del contacto (lo dice Meta) no sale la llamada.
	if r := x.exigir(x.llamar(x.supervisor, conv, nil), http.StatusUnprocessableEntity); !strings.Contains(crudo(r), "sin_permiso_llamada") || x.g.cuenta("connect:+"+num) != 0 {
		t.Fatalf("llamar sin permiso: %s %v", crudo(r), x.g.hechas())
	}

	// Solicitar: dentro de la ventana va como mensaje interactivo; queda en el hilo y respeta 1 por 24 h.
	s := x.exigir(x.api(x.agente, http.MethodPost, ruta+"/solicitar-permiso-llamada", nil), http.StatusOK)
	if s.cuerpo["estado"] != "pedido" || s.cuerpo["puedeSolicitar"] != false || s.cuerpo["proximaSolicitudEn"] == nil {
		t.Fatalf("solicitado: %s", crudo(s))
	}
	var msg string
	if err := x.bd.QueryRow(x.ctx, `SELECT id::text FROM mensajeria.conversacion_mensajes WHERE conversacion_id = $1 AND automatico = 'permiso_llamada'`, conv).Scan(&msg); err != nil {
		t.Fatal(err)
	}
	if _, err := x.m.ProcesarMensaje(x.ctx, uuid.MustParse(msg), false); err != nil {
		t.Fatal(err)
	}
	x.meta.mu.Lock()
	envio := x.meta.envios[len(x.meta.envios)-1]
	x.meta.mu.Unlock()
	if in, _ := envio["interactive"].(map[string]any); envio["type"] != "interactive" || in["type"] != "call_permission_request" || envio["to"] != num {
		t.Fatalf("solicitud enviada a Meta: %v", envio)
	}
	if r := x.exigir(x.api(x.agente, http.MethodPost, ruta+"/solicitar-permiso-llamada", nil), http.StatusUnprocessableEntity); !strings.Contains(crudo(r), "limite_solicitudes") {
		t.Fatalf("segunda solicitud en 24 h: %s", crudo(r))
	}
	if x.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'llamada.permiso_solicitado' AND payload->>'conversacion_id' = $1`, conv) != 1 {
		t.Fatal("falta llamada.permiso_solicitado")
	}

	// El cliente acepta (temporal, 7 días).
	x.darPermiso(num, "accept", false)
	p = x.exigir(x.api(x.supervisor, http.MethodGet, ruta+"/permiso-llamada", nil), http.StatusOK)
	if p.cuerpo["estado"] != "temporal" || p.cuerpo["venceEn"] == nil || p.cuerpo["puedeLlamar"] != true {
		t.Fatalf("permiso temporal: %s", crudo(p))
	}
	if p := x.exigir(x.api(x.agente, http.MethodGet, ruta+"/permiso-llamada", nil), http.StatusOK); p.cuerpo["puedeLlamar"] != false || p.cuerpo["motivo"] != "no_autorizado" {
		t.Fatalf("quien no tiene llamadas.llamar: %s", crudo(p))
	}
	if r := x.exigir(x.api(x.agente, http.MethodPost, ruta+"/solicitar-permiso-llamada", nil), http.StatusUnprocessableEntity); !strings.Contains(crudo(r), "permiso_ya_vigente") {
		t.Fatalf("solicitar con permiso vigente: %s", crudo(r))
	}
	if n := x.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'llamada.permiso_cambiado' AND payload->>'a' = 'temporal' AND payload::text NOT LIKE '%' || $1 || '%'
		AND payload->>'canal_id' = $2`, num, fmt.Sprint(x.canal.ID)); n == 0 {
		t.Fatal("falta llamada.permiso_cambiado (sin el número)")
	}

	// Llamar: exige el navegador y una oferta válida; a Meta va la oferta saneada.
	sinNav := x.supervisor
	sinNav.navegador = ""
	if r := x.exigir(x.llamar(sinNav, conv, nil), http.StatusUnprocessableEntity); !strings.Contains(crudo(r), "navegador_requerido") {
		t.Fatalf("llamar sin navegador: %s", crudo(r))
	}
	if r := x.exigir(x.llamar(x.supervisor, conv, map[string]any{"sdpOferta": ofertaNavegador() + "m=video 9 UDP/TLS/RTP/SAVPF 96\r\na=rtpmap:96 VP8/90000\r\n"}),
		http.StatusUnprocessableEntity); !strings.Contains(crudo(r), "sdp_invalido") || x.g.cuenta("connect:+"+num) != 0 {
		t.Fatalf("oferta inválida: %s", crudo(r))
	}
	id, call := x.saliente(x.supervisor, conv, nil)
	if len(x.g.ofertas) != 1 || strings.Contains(x.g.ofertas[0], "192.168.") || !strings.Contains(x.g.ofertas[0], "a=fingerprint") || x.g.cuenta("connect:+"+num) != 1 {
		t.Fatalf("connect a Meta: %v %q", x.g.hechas(), x.g.ofertas)
	}
	if n := x.contar(`SELECT count(*) FROM mensajeria.llamadas WHERE id = $1::uuid AND direccion = 'saliente' AND estado = 'marcando' AND usuario_id = $2 AND id_externo = $3
		AND sdp_oferta IS NULL AND navegador = $4`, id, x.supervisor.id, call, x.supervisor.navegador); n != 1 {
		t.Fatal("la saliente debía quedar marcando, con su call_id y sin la oferta")
	}
	if x.eventos("llamada.saliente_iniciada", id) != 1 {
		t.Fatal("falta llamada.saliente_iniciada")
	}
	// Una a la vez por persona y por contacto; y es su llamada activa.
	if r := x.exigir(x.llamar(x.supervisor, conv, nil), http.StatusConflict); !strings.Contains(crudo(r), "llamada_en_curso") {
		t.Fatalf("segunda llamada: %s", crudo(r))
	}
	if a := x.exigir(x.api(x.supervisor, http.MethodGet, "/llamadas/activa", nil), http.StatusOK); a.cuerpo["id"] != id || a.cuerpo["puede"].(map[string]any)["colgar"] != true {
		t.Fatalf("llamada activa: %s", crudo(a))
	}
	// La respuesta SDP: todavía no; después, solo a quien llamó y desde su navegador.
	if r := x.exigir(x.api(x.supervisor, http.MethodGet, "/llamadas/"+id+"/respuesta-sdp", nil), http.StatusConflict); !strings.Contains(crudo(r), "sin_respuesta") {
		t.Fatalf("respuesta antes de tiempo: %s", crudo(r))
	}
	x.respuestaSaliente(num, call)
	if r := x.exigir(x.api(x.supervisor, http.MethodGet, "/llamadas/"+id+"/respuesta-sdp", nil), http.StatusOK); r.cuerpo["sdpRespuesta"] != respuestaMeta() {
		t.Fatalf("respuesta SDP: %s", crudo(r))
	}
	otroNav := x.supervisor
	otroNav.navegador = "navegador-otro-de-supervisor"
	if r := x.exigir(x.api(otroNav, http.MethodGet, "/llamadas/"+id+"/respuesta-sdp", nil), http.StatusConflict); !strings.Contains(crudo(r), "otro_navegador") {
		t.Fatalf("respuesta desde otro navegador: %s", crudo(r))
	}
	sup2 := x.persona([]int64{1}, rolBase("supervisor"))
	sup2.navegador = x.supervisor.navegador
	x.exigir(x.api(sup2, http.MethodGet, "/llamadas/"+id+"/respuesta-sdp", nil), http.StatusNotFound)
	x.exigir(x.api(x.agente, http.MethodGet, "/llamadas/"+id+"/respuesta-sdp", nil), http.StatusForbidden)
	if n := x.contar(`SELECT count(*) FROM mensajeria.conv_eventos WHERE llamada_id = $1::uuid AND tipo = 'llamada_respuesta' AND usuario_id = $2`, id, x.supervisor.id); n != 1 {
		t.Fatalf("aviso de la respuesta, solo a quien llamó: %d", n)
	}
	if n := x.contar(`SELECT count(*) FROM mensajeria.conv_eventos WHERE llamada_id = $1::uuid AND tipo = 'llamada_respuesta' AND usuario_id IS DISTINCT FROM $2`, id, x.supervisor.id); n != 0 {
		t.Fatal("la respuesta se avisó a alguien más")
	}
	// Suena y contesta.
	x.estadoSaliente(num, call, "RINGING")
	if _, estado, _ := x.llamada(call); estado != "marcando" {
		t.Fatalf("sonando en el teléfono: %s", estado)
	}
	x.estadoSaliente(num, call, "ACCEPTED")
	x.estadoSaliente(num, call, "ACCEPTED") // repetido: no duplica
	v := x.exigir(x.api(x.supervisor, http.MethodGet, "/llamadas/"+id, nil), http.StatusOK)
	if v.cuerpo["estado"] != "en_curso" || v.cuerpo["atendidaPor"] != "persona" || len(v.cuerpo["tramos"].([]any)) != 1 || x.eventos("llamada.contestada", id) != 1 {
		t.Fatalf("contestada: %s", crudo(v))
	}
	// En el hilo es un mensaje saliente de tipo llamada.
	ms := x.mensajes(x.agente, conv)
	if u := ms[len(ms)-1]; u["tipo"] != "llamada" || u["direccion"] != "saliente" || u["llamada"].(map[string]any)["estado"] != "en_curso" {
		t.Fatalf("mensaje de la saliente: %v", u)
	}
	x.pasa(20 * time.Second)
	f := x.exigir(x.api(x.supervisor, http.MethodPost, "/llamadas/"+id+"/colgar", nil), http.StatusOK)
	if f.cuerpo["estado"] != "terminada" || f.cuerpo["duracionSeg"] != float64(20) || x.g.cuenta("terminate:"+call) != 1 {
		t.Fatalf("colgada: %s %v", crudo(f), x.g.hechas())
	}
	x.exigir(x.api(x.supervisor, http.MethodGet, "/llamadas/"+id+"/respuesta-sdp", nil), http.StatusConflict)
	// Nada del SDP queda guardado ni auditado; el número y el call_id no van a eventos.
	for _, sql := range []string{
		`SELECT count(*) FROM mensajeria.llamadas WHERE id = '` + id + `'::uuid AND (sdp_oferta IS NOT NULL OR sdp_respuesta IS NOT NULL)`,
		`SELECT count(*) FROM mensajeria.entrantes WHERE canal_id = ` + fmt.Sprint(x.canal.ID) + ` AND payload::text LIKE '%` + call + `%' AND payload::text LIKE '%fingerprint%'`,
		`SELECT count(*) FROM auditoria.acciones WHERE accion LIKE 'llamadas.%' AND entrada::text LIKE '%fingerprint%'`,
		`SELECT count(*) FROM integracion.outbox WHERE evento LIKE 'llamada.%' AND (payload::text LIKE '%` + call + `%' OR payload::text LIKE '%fingerprint%' OR payload::text LIKE '%` + num + `%')`,
		`SELECT count(*) FROM nucleo.idempotencia WHERE position(convert_to('fingerprint', 'UTF8') in respuesta) > 0`,
	} {
		if n := x.contar(sql); n != 0 {
			t.Fatalf("quedó SDP, call_id o número donde no debe (%d): %s", n, sql)
		}
	}
	if n := x.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'llamadas.llamar' AND entrada->>'sdp_sha256' <> '' AND actor_id = $1`, x.supervisor.id); n == 0 {
		t.Fatal("la auditoría de llamar debía llevar la huella de la oferta")
	}

	// Un webhook que llega antes de que se guarde el call_id se vincula por número.
	x.pasa(61 * time.Second)
	adelantado := "wacid.adelantado" + num
	x.g.mu.Lock()
	x.g.proximaSaliente = adelantado
	x.g.mu.Unlock()
	x.gancho(func(accion string) {
		if accion == "connect" {
			x.respuestaSaliente(num, adelantado)
		}
	})
	id2, call2 := x.saliente(x.supervisor, conv, nil)
	x.gancho(nil)
	if n := x.contar(`SELECT count(*) FROM mensajeria.llamadas WHERE id = $1::uuid AND id_externo = $2 AND sdp_respuesta IS NOT NULL AND estado = 'marcando'`, id2, adelantado); n != 1 || call2 != adelantado {
		t.Fatal("el webhook adelantado debía vincularse a la saliente")
	}
	// Colgar mientras marca: se cancela en Meta y no cuenta como sin contestar.
	if r := x.exigir(x.api(x.supervisor, http.MethodPost, "/llamadas/"+id2+"/colgar", nil), http.StatusOK); r.cuerpo["estado"] != "terminada" || x.g.cuenta("terminate:"+adelantado) != 1 {
		t.Fatalf("cancelada: %s", crudo(r))
	}
	if n := x.contar(`SELECT sin_contestar FROM mensajeria.llamada_permisos WHERE canal_id = $1 AND numero = $2`, x.canal.ID, "+"+num); n != 0 {
		t.Fatalf("una cancelada no cuenta como sin contestar: %d", n)
	}
	// Meta rechaza el connect: 502 sin el mensaje de Meta y la llamada queda fallida.
	x.pasa(61 * time.Second)
	x.g.falla("connect", &mensajeria.ErrMetaLlamada{Status: 400})
	r := x.exigir(x.llamar(x.supervisor, conv, nil), http.StatusBadGateway)
	x.g.falla("connect", nil)
	if !strings.Contains(crudo(r), "meta_rechazo") || x.contar(`SELECT count(*) FROM mensajeria.llamadas WHERE conversacion_id = $1 AND estado = 'fallida' AND motivo_fin = 'error_meta'`, conv) != 1 {
		t.Fatalf("Meta rechaza el connect: %s", crudo(r))
	}
	x.exigir(x.api(x.supervisor, http.MethodGet, "/llamadas/activa", nil), http.StatusNoContent)
}

// Devolver la llamada, sin contestar (revocación a la cuarta) y la respuesta de Meta inválida.
func TestLlamadaSalienteDevolucionYSinContestar(t *testing.T) {
	x := nuevaBandejaVoz(t)
	x.limitesVoz(1000, 100000)
	x.recibe(x.agente)
	num, conv := x.convConPermiso()
	// Una entrante perdida deja la devolución pendiente.
	perdida := "wacid.pordevolver" + num
	x.connect(num, perdida)
	x.pasa(31 * time.Second)
	x.mantener()
	dev := x.exigir(x.api(x.supervisor, http.MethodGet, "/llamadas/devoluciones", nil), http.StatusOK).cuerpo["data"].([]any)
	var devID float64
	for _, d := range dev {
		if fmt.Sprint(int64(d.(map[string]any)["conversacionId"].(float64))) == conv {
			devID = d.(map[string]any)["id"].(float64)
		}
	}
	if devID == 0 {
		t.Fatalf("sin devolución pendiente: %v", dev)
	}
	x.exigir(x.llamar(x.supervisor, conv, map[string]any{"devolucionId": 999999999}), http.StatusConflict)
	automaticos := func() int {
		return x.contar(`SELECT count(*) FROM mensajeria.conversacion_mensajes WHERE conversacion_id = $1 AND direccion = 'saliente' AND automatico = 'bot'`, conv)
	}
	antes := automaticos()
	// «Devolver la llamada»: el cliente la rechaza. Queda perdida, sin otra devolución ni mensaje
	// automático, la devolución sigue pendiente y cuenta una sin contestar.
	id, call := x.saliente(x.supervisor, conv, map[string]any{"devolucionId": devID})
	x.estadoSaliente(num, call, "REJECTED")
	if _, estado, motivo := x.llamada(call); estado != "perdida" || motivo != "nadie_contesto" {
		t.Fatalf("rechazada por el cliente: %s %s", estado, motivo)
	}
	sinContestar := func() int {
		return x.contar(`SELECT sin_contestar FROM mensajeria.llamada_permisos WHERE canal_id = $1 AND numero = $2`, x.canal.ID, "+"+num)
	}
	if sinContestar() != 1 || automaticos() != antes || x.contar(`SELECT count(*) FROM mensajeria.llamada_devoluciones WHERE conversacion_id = $1`, conv) != 1 ||
		x.contar(`SELECT count(*) FROM mensajeria.llamada_devoluciones WHERE id = $1 AND estado = 'pendiente'`, int64(devID)) != 1 {
		t.Fatalf("tras una saliente sin contestar: sin_contestar %d, automáticos %d → %d", sinContestar(), antes, automaticos())
	}
	if n := x.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'llamada.perdida' AND payload->>'llamada_id' = $1 AND payload->>'direccion' = 'saliente'
		AND payload->>'devolucion_id' IS NULL AND jsonb_array_length(payload->'usuarios') = 0`, id); n != 1 {
		t.Fatal("llamada.perdida de una saliente: sin devolución ni avisos")
	}
	// Otra vez, y contesta: la devolución queda hecha y la cuenta vuelve a cero.
	id2, call2 := x.saliente(x.supervisor, conv, map[string]any{"devolucionId": devID})
	x.respuestaSaliente(num, call2)
	x.estadoSaliente(num, call2, "ACCEPTED")
	if n := x.contar(`SELECT count(*) FROM mensajeria.llamada_devoluciones WHERE id = $1 AND estado = 'hecha' AND atendida_por = $2 AND llamada_devuelta_id = $3::uuid`,
		int64(devID), x.supervisor.id, id2); n != 1 || sinContestar() != 0 {
		t.Fatalf("devolución hecha: %d, sin contestar %d", n, sinContestar())
	}
	if n := x.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'llamada.devolucion_cerrada' AND payload->>'estado' = 'hecha' AND payload->>'llamada_devuelta_id' = $1`, id2); n != 1 {
		t.Fatal("falta llamada.devolucion_cerrada")
	}
	// El cliente cuelga: terminate de Meta con su duración.
	x.pasa(10 * time.Second)
	x.webhookSaliente(num, call2, "terminate", `,"status":"COMPLETED","duration":9`)
	if v := x.exigir(x.api(x.supervisor, http.MethodGet, "/llamadas/"+id2, nil), http.StatusOK); v.cuerpo["estado"] != "terminada" || v.cuerpo["motivoFin"] != "cliente_colgo" ||
		v.cuerpo["duracionSeg"] != float64(9) {
		t.Fatalf("el cliente colgó: %s", crudo(v))
	}

	// Tres seguidas sin contestar (vence el tiempo de marcado: perdida + terminate en Meta).
	for i := range 3 {
		x.pasa(61 * time.Second)
		_, c := x.saliente(x.supervisor, conv, nil)
		x.pasa(61 * time.Second)
		x.mantener()
		if _, estado, _ := x.llamada(c); estado != "perdida" || x.g.cuenta("terminate:"+c) != 1 || sinContestar() != i+1 {
			t.Fatalf("sin contestar %d: %s, terminate %d, cuenta %d", i+1, estado, x.g.cuenta("terminate:"+c), sinContestar())
		}
	}
	// La cuarta revoca el permiso: hay que confirmarlo.
	x.pasa(61 * time.Second)
	if r := x.exigir(x.llamar(x.supervisor, conv, nil), http.StatusUnprocessableEntity); !strings.Contains(crudo(r), "confirmar_riesgo") {
		t.Fatalf("la cuarta sin confirmar: %s", crudo(r))
	}
	if p := x.exigir(x.api(x.supervisor, http.MethodGet, "/conversaciones/"+conv+"/permiso-llamada", nil), http.StatusOK); p.cuerpo["sinContestar"] != float64(3) {
		t.Fatalf("permiso con tres sin contestar: %s", crudo(p))
	}
	_, c4 := x.saliente(x.supervisor, conv, map[string]any{"confirmarRiesgo": true})
	x.webhookSaliente(num, c4, "terminate", `,"status":"FAILED"`)
	p := x.exigir(x.api(x.supervisor, http.MethodGet, "/conversaciones/"+conv+"/permiso-llamada", nil), http.StatusOK)
	if p.cuerpo["estado"] != "revocado" || p.cuerpo["puedeLlamar"] != false ||
		x.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'llamada.permiso_cambiado' AND payload->>'a' = 'revocado' AND payload->>'conversacion_id' = $1`, conv) != 1 {
		t.Fatalf("permiso revocado: %s", crudo(p))
	}
	// Meta ya dice que no hay permiso: no sale la llamada.
	x.g.mu.Lock()
	x.g.permiso = mensajeria.PermisoMeta{Estado: "sin_pedir"}
	x.g.mu.Unlock()
	x.pasa(61 * time.Second)
	if r := x.exigir(x.llamar(x.supervisor, conv, map[string]any{"confirmarRiesgo": true}), http.StatusUnprocessableEntity); !strings.Contains(crudo(r), "sin_permiso_llamada") {
		t.Fatalf("llamar con el permiso revocado: %s", crudo(r))
	}

	// Una respuesta SDP de Meta que no pasa la validación no llega al navegador: fallida y terminate.
	num2, conv2 := x.convConPermiso()
	id5, c5 := x.saliente(x.supervisor, conv2, nil)
	x.webhookSaliente(num2, c5, "connect", fmt.Sprintf(`,"session":{"sdp_type":"answer","sdp":%q}`,
		strings.Replace(respuestaMeta(), "2113937151 157.240.19.130", "2113937151 10.0.0.9", 1)))
	if _, estado, motivo := x.llamada(c5); estado != "fallida" || motivo != "error_meta" || x.g.cuenta("terminate:"+c5) != 1 {
		t.Fatalf("respuesta de Meta inválida: %s %s", estado, motivo)
	}
	x.exigir(x.api(x.supervisor, http.MethodGet, "/llamadas/"+id5+"/respuesta-sdp", nil), http.StatusConflict)
}

// Límites, tope de minutos con sus eventos, número prestado y aislamiento por empresa.
func TestLlamadaSalienteLimitesYEmpresas(t *testing.T) {
	x := nuevaBandejaVoz(t)
	x.limitesVoz(1000, 100000)
	t.Cleanup(func() { x.limitesVoz(1000, 100000) })
	num, conv := x.convConPermiso()
	completa := func(seg int) string {
		t.Helper()
		id, call := x.saliente(x.supervisor, conv, nil)
		x.respuestaSaliente(num, call)
		x.estadoSaliente(num, call, "ACCEPTED")
		x.pasa(time.Duration(seg) * time.Second)
		x.exigir(x.api(x.supervisor, http.MethodPost, "/llamadas/"+id+"/colgar", nil), http.StatusOK)
		x.pasa(61 * time.Second)
		return id
	}
	completa(5)
	// Límite por día de la empresa.
	hoy := x.contar(`SELECT count(*) FROM mensajeria.llamadas WHERE empresa_id = 1 AND direccion = 'saliente' AND estado <> 'fallida' AND sono_en >= $1`, x.reloj.Add(-10*time.Hour))
	x.limitesVoz(hoy, 100000)
	if r := x.exigir(x.llamar(x.supervisor, conv, nil), http.StatusUnprocessableEntity); !strings.Contains(crudo(r), "limite_llamadas") {
		t.Fatalf("límite por día: %s", crudo(r))
	}
	// Límite de Meta por número (100 conectadas en 24 h).
	x.limitesVoz(1000, 100000)
	x.sql(`INSERT INTO mensajeria.llamadas (id, empresa_id, origen, canal_id, conversacion_id, id_externo, direccion, numero, estado, sono_en, suena_hasta, contestada_en, terminada_en, duracion_seg)
		SELECT gen_random_uuid(), 1, 'whatsapp', $1, $2, 'wacid.relleno' || n, 'saliente', $3, 'terminada', $4, $4, $4, $4, 0 FROM generate_series(1, 100) n`,
		x.canal.ID, conv, "+"+num, x.reloj.Add(-2*time.Hour))
	if r := x.exigir(x.llamar(x.supervisor, conv, nil), http.StatusUnprocessableEntity); !strings.Contains(crudo(r), "limite_llamadas") {
		t.Fatalf("límite de Meta por número: %s", crudo(r))
	}
	x.sql(`DELETE FROM mensajeria.llamadas WHERE canal_id = $1 AND id_externo LIKE 'wacid.relleno%'`, x.canal.ID)

	// Tope de minutos del mes: eventos al 80 % y al 100 %, y después no se llama.
	var usados int
	if err := x.bd.QueryRow(x.ctx, `SELECT coalesce(sum(duracion_seg), 0)::int FROM mensajeria.llamadas WHERE empresa_id = 1 AND direccion = 'saliente' AND contestada_en IS NOT NULL`).Scan(&usados); err != nil {
		t.Fatal(err)
	}
	tope := usados/48 + 5 // minutos: el 80 % queda 240 s por encima de lo usado
	x.limitesVoz(1000, tope)
	topes := func(p int) int {
		return x.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'voz.tope_alcanzado' AND payload->>'tipo' = 'salientes' AND (payload->>'porcentaje')::int = $1
			AND (payload->>'tope')::int = $2`, p, tope)
	}
	completa(48*tope - usados + 1)
	if topes(80) != 1 || topes(100) != 0 {
		t.Fatalf("al 80 %%: %d y %d", topes(80), topes(100))
	}
	completa(60*tope - 48*tope)
	if topes(80) != 1 || topes(100) != 1 {
		t.Fatalf("al 100 %%: %d y %d", topes(80), topes(100))
	}
	if r := x.exigir(x.llamar(x.supervisor, conv, nil), http.StatusUnprocessableEntity); !strings.Contains(crudo(r), "tope_alcanzado") {
		t.Fatalf("pasado el tope: %s", crudo(r))
	}
	x.limitesVoz(1000, 100000)

	// Con las llamadas apagadas en el número no se llama ni se pide permiso.
	v := x.exigir(x.api(x.supervisor, http.MethodGet, x.rutaCanal("/llamadas"), nil), http.StatusOK)
	x.exigir(x.configurar(int(v.cuerpo["version"].(float64)), map[string]any{"llamadasActivas": false}), http.StatusOK)
	if r := x.exigir(x.llamar(x.supervisor, conv, nil), http.StatusUnprocessableEntity); !strings.Contains(crudo(r), "llamadas_inactivas") {
		t.Fatalf("llamar con las llamadas apagadas: %s", crudo(r))
	}
	v = x.exigir(x.api(x.supervisor, http.MethodGet, x.rutaCanal("/llamadas"), nil), http.StatusOK)
	// La plantilla de permiso del número se guarda con los ajustes.
	c := x.exigir(x.configurar(int(v.cuerpo["version"].(float64)), map[string]any{"plantillaPermiso": "permiso_de_llamada"}), http.StatusOK)
	if c.cuerpo["plantillaPermiso"] != "permiso_de_llamada" {
		t.Fatalf("plantilla de permiso: %s", crudo(c))
	}

	// Fuera de la ventana de 24 h a un desconocido no se le manda nada (regla de consentimiento del chat).
	numF := numeroCliente()
	x.texto(numF, "wamid.fuera"+numF, "Hola")
	convF := cid(x.conv(numF))
	x.sql(`UPDATE mensajeria.conversaciones SET ultimo_entrante_en = $2 WHERE id = $1`, convF, x.reloj.Add(-30*time.Hour))
	if r := x.api(x.agente, http.MethodPost, "/conversaciones/"+convF+"/solicitar-permiso-llamada", nil); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("solicitar fuera de ventana a un desconocido: %d %s", r.status, crudo(r))
	}

	// Número prestado: la empresa prestataria no llama ni pide permiso con el número de otra, y la
	// dueña no toca la conversación de la prestataria.
	sup2 := x.persona([]int64{2}, rolBase("supervisor"))
	sup2.navegador = "navegador-de-la-empresa-2"
	rc := x.exigir(x.api(sup2, http.MethodPost, "/colas", map[string]any{"nombre": "Norte " + numeroCliente()}), http.StatusCreated)
	x.exigir(x.api(x.supervisor, http.MethodPost, x.rutaCanal("/prestamos"), map[string]any{"empresaId": 2, "usos": []string{"soporte"}, "colaId": rc.cuerpo["id"]}), http.StatusOK)
	numP := numeroCliente()
	x.texto(numP, "wamid.prest"+numP, "Hola")
	convP := cid(x.conv(numP))
	x.exigir(x.api(x.supervisor, http.MethodPost, "/conversaciones/"+convP+"/pasar-empresa", map[string]any{"version": ver(x.conv(numP)), "empresaId": 2}), http.StatusOK)
	if p := x.exigir(x.api(sup2, http.MethodGet, "/conversaciones/"+convP+"/permiso-llamada", nil), http.StatusOK); p.cuerpo["puedeLlamar"] != false ||
		p.cuerpo["puedeSolicitar"] != false || p.cuerpo["motivo"] != "llamadas_inactivas" {
		t.Fatalf("permiso en un número prestado: %s", crudo(p))
	}
	antes := len(x.g.hechas())
	for _, r := range []respuesta{x.llamar(sup2, convP, nil), x.api(sup2, http.MethodPost, "/conversaciones/"+convP+"/solicitar-permiso-llamada", nil)} {
		if r.status != http.StatusUnprocessableEntity || !strings.Contains(crudo(r), "llamadas_inactivas") {
			t.Fatalf("la prestataria con el número de otra: %d %s", r.status, crudo(r))
		}
	}
	x.exigir(x.llamar(x.supervisor, convP, nil), http.StatusNotFound)
	x.exigir(x.api(x.supervisor, http.MethodGet, "/conversaciones/"+convP+"/permiso-llamada", nil), http.StatusNotFound)
	// Otra empresa no ve ni toca una saliente de la empresa 1.
	x.exigir(x.llamar(sup2, conv, nil), http.StatusNotFound)
	if len(x.g.hechas()) != antes {
		t.Fatalf("nada de esto debía llegar a Meta: %v", x.g.hechas()[antes:])
	}
}

// La solicitud de permiso fuera de la ventana de 24 h: con la plantilla del número (y solo con ella),
// y lo que pasa si Meta la rechaza.
func TestSolicitarPermisoConPlantilla(t *testing.T) {
	x := nuevaBandejaVoz(t)
	// Un cliente de Argos confirmado y con permiso de contacto, que escribió hace más de 24 h.
	cliente := func() (num, conv string) {
		t.Helper()
		num = numeroCliente()
		x.texto(num, "wamid.plantilla"+num, "Hola")
		conv = cid(x.conv(num))
		var id int64
		if err := x.bd.QueryRow(x.ctx, `SELECT coalesce(max(id), 0) + 1000 FROM integracion.argos_clientes`).Scan(&id); err != nil {
			t.Fatal(err)
		}
		x.sql(`INSERT INTO integracion.argos_clientes (id, empresa_id, nombre, sincronizado_en, empresas) VALUES ($1, 1, 'Rosa Plantilla', now(), '{1}')`, id)
		x.sql(`UPDATE mensajeria.conversaciones SET cliente_id = $2, cliente_verificado = true, asociacion = 'manual', ultimo_entrante_en = $3 WHERE id = $1`,
			conv, id, x.reloj.Add(-30*time.Hour))
		x.sql(`INSERT INTO mensajeria.consentimientos (empresa_id, cliente_id, canal, estado, origen, ocurrido_en) VALUES (1, $1, 'whatsapp', 'acepta', 'agente', $2)`,
			id, x.reloj.Add(-48*time.Hour))
		return num, conv
	}
	enviados := func() int {
		x.meta.mu.Lock()
		defer x.meta.mu.Unlock()
		return len(x.meta.envios)
	}
	mensajeDe := func(conv string) string {
		t.Helper()
		var id string
		if err := x.bd.QueryRow(x.ctx, `SELECT id::text FROM mensajeria.conversacion_mensajes WHERE conversacion_id = $1 AND automatico = 'permiso_llamada' ORDER BY seq DESC LIMIT 1`,
			conv).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	num, conv := cliente()
	ruta := "/conversaciones/" + conv + "/solicitar-permiso-llamada"
	// Sin plantilla indicada en el número: 422, sin mensaje ni solicitud contada.
	antes := enviados()
	if r := x.exigir(x.api(x.agente, http.MethodPost, ruta, nil), http.StatusUnprocessableEntity); !strings.Contains(crudo(r), "sin_plantilla_permiso") {
		t.Fatalf("sin plantilla: %s", crudo(r))
	}
	if n := x.contar(`SELECT count(*) FROM mensajeria.conversacion_mensajes WHERE conversacion_id = $1 AND automatico = 'permiso_llamada'`, conv); n != 0 || enviados() != antes {
		t.Fatalf("sin plantilla no debía salir nada: %d mensajes", n)
	}
	if p := x.exigir(x.api(x.agente, http.MethodGet, "/conversaciones/"+conv+"/permiso-llamada", nil), http.StatusOK); p.cuerpo["estado"] != "sin_pedir" || p.cuerpo["puedeSolicitar"] != true {
		t.Fatalf("la solicitud fallida no cuenta para el límite: %s", crudo(p))
	}
	// Con la plantilla del número: a Meta va la plantilla (no el interactivo), en su idioma.
	x.exigir(x.configurar(x.versionLlamadas(), map[string]any{"plantillaPermiso": "permiso_de_llamada", "plantillaPermisoIdioma": "es_MX"}), http.StatusOK)
	if s := x.exigir(x.api(x.agente, http.MethodPost, ruta, nil), http.StatusOK); s.cuerpo["estado"] != "pedido" {
		t.Fatalf("solicitado con plantilla: %s", crudo(s))
	}
	msg := mensajeDe(conv)
	if _, err := x.m.ProcesarMensaje(x.ctx, uuid.MustParse(msg), false); err != nil {
		t.Fatal(err)
	}
	x.meta.mu.Lock()
	envio := x.meta.envios[len(x.meta.envios)-1]
	x.meta.mu.Unlock()
	tpl, _ := envio["template"].(map[string]any)
	if envio["type"] != "template" || envio["to"] != num || tpl["name"] != "permiso_de_llamada" || tpl["language"].(map[string]any)["code"] != "es_MX" || envio["interactive"] != nil {
		t.Fatalf("solicitud con plantilla enviada a Meta: %v", envio)
	}
	if n := x.contar(`SELECT count(*) FROM mensajeria.conversacion_mensajes WHERE id = $1::uuid AND estado_entrega = 'enviado'`, msg); n != 1 {
		t.Fatal("el mensaje de la solicitud debía quedar enviado")
	}

	// Meta rechaza la plantilla (no existe o no está aprobada): el mensaje queda fallido a la vista,
	// sin el texto de Meta, y el permiso sigue «pedido» (la solicitud cuenta: no se reintenta sola).
	num2, conv2 := cliente()
	x.exigir(x.api(x.agente, http.MethodPost, "/conversaciones/"+conv2+"/solicitar-permiso-llamada", nil), http.StatusOK)
	msg2 := mensajeDe(conv2)
	x.meta.error.Store(132001) // la plantilla no existe en ese idioma
	_, err := x.m.ProcesarMensaje(x.ctx, uuid.MustParse(msg2), true)
	x.meta.error.Store(0)
	if err != nil {
		t.Fatal(err)
	}
	if n := x.contar(`SELECT count(*) FROM mensajeria.conversacion_mensajes WHERE id = $1::uuid AND estado_entrega = 'fallido' AND motivo_falla IS NOT NULL`, msg2); n != 1 {
		t.Fatal("la solicitud rechazada por Meta debía quedar fallida")
	}
	if n := x.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'conversacion.mensaje_fallido' AND payload->>'mensaje_id' = $1 AND payload::text NOT LIKE '%' || $2 || '%'`, msg2, num2); n != 1 {
		t.Fatalf("conversacion.mensaje_fallido de la solicitud: %d", n)
	}
	p := x.exigir(x.api(x.agente, http.MethodGet, "/conversaciones/"+conv2+"/permiso-llamada", nil), http.StatusOK)
	if p.cuerpo["estado"] != "pedido" || p.cuerpo["puedeSolicitar"] != false || p.cuerpo["puedeLlamar"] != false {
		t.Fatalf("tras el rechazo de Meta: %s", crudo(p))
	}
	// Si alguien quita la plantilla antes de que salga el mensaje, no se manda como interactivo fuera de ventana.
	num3, conv3 := cliente()
	x.exigir(x.api(x.agente, http.MethodPost, "/conversaciones/"+conv3+"/solicitar-permiso-llamada", nil), http.StatusOK)
	x.exigir(x.configurar(x.versionLlamadas(), map[string]any{"plantillaPermiso": ""}), http.StatusOK)
	antes = enviados()
	if _, err := x.m.ProcesarMensaje(x.ctx, uuid.MustParse(mensajeDe(conv3)), false); err != nil {
		t.Fatal(err)
	}
	if enviados() != antes || x.contar(`SELECT count(*) FROM mensajeria.conversacion_mensajes WHERE conversacion_id = $1 AND automatico = 'permiso_llamada' AND estado_entrega = 'fallido'`, conv3) != 1 {
		t.Fatalf("sin plantilla al enviar (%s): no debía salir nada y el mensaje queda fallido", num3)
	}
}

// Meta no manda ACCEPTED: la respuesta SDP llegó, hubo audio y después llega el terminate con su
// duración. La llamada queda terminada (no perdida), con esa duración, y cuenta para el tope.
func TestLlamadaSalienteSinAccepted(t *testing.T) {
	x := nuevaBandejaVoz(t)
	x.limitesVoz(1000, 100000)
	t.Cleanup(func() { x.limitesVoz(1000, 100000) })
	x.recibe(x.agente)
	num, conv := x.convConPermiso()
	// Con una devolución pendiente, para ver que también queda hecha.
	x.connect(num, "wacid.sinaccepted"+num)
	x.pasa(31 * time.Second)
	x.mantener()
	var dev int64
	if err := x.bd.QueryRow(x.ctx, `SELECT id FROM mensajeria.llamada_devoluciones WHERE conversacion_id = $1 AND estado = 'pendiente'`, conv).Scan(&dev); err != nil {
		t.Fatal(err)
	}
	var usados int
	if err := x.bd.QueryRow(x.ctx, `SELECT coalesce(sum(duracion_seg), 0)::int FROM mensajeria.llamadas WHERE empresa_id = 1 AND direccion = 'saliente' AND contestada_en IS NOT NULL`).Scan(&usados); err != nil {
		t.Fatal(err)
	}
	tope := usados/48 + 5
	x.limitesVoz(1000, tope)
	dur := 48*tope - usados + 1 // lo justo para cruzar el 80 %
	id, call := x.saliente(x.supervisor, conv, map[string]any{"devolucionId": dev})
	x.respuestaSaliente(num, call)
	x.pasa(time.Duration(dur+3) * time.Second) // (más de 60 s marcando, pero el mantenimiento no llegó a correr)
	x.webhookSaliente(num, call, "terminate", fmt.Sprintf(`,"status":"COMPLETED","duration":%d`, dur))
	v := x.exigir(x.api(x.supervisor, http.MethodGet, "/llamadas/"+id, nil), http.StatusOK)
	if v.cuerpo["estado"] != "terminada" || v.cuerpo["motivoFin"] != "cliente_colgo" || v.cuerpo["duracionSeg"] != float64(dur) || v.cuerpo["atendidaPor"] != "persona" ||
		len(v.cuerpo["tramos"].([]any)) != 1 {
		t.Fatalf("terminate con duración sin ACCEPTED: %s", crudo(v))
	}
	if x.eventos("llamada.contestada", id) != 1 || x.eventos("llamada.terminada", id) != 1 || x.eventos("llamada.perdida", id) != 0 {
		t.Fatal("debía contar como contestada y terminada, no como perdida")
	}
	if n := x.contar(`SELECT sin_contestar FROM mensajeria.llamada_permisos WHERE canal_id = $1 AND numero = $2`, x.canal.ID, "+"+num); n != 0 {
		t.Fatalf("no cuenta como sin contestar: %d", n)
	}
	if n := x.contar(`SELECT count(*) FROM mensajeria.llamada_devoluciones WHERE id = $1 AND estado = 'hecha' AND llamada_devuelta_id = $2::uuid`, dev, id); n != 1 {
		t.Fatal("la devolución debía quedar hecha")
	}
	// Cuenta para el tope de minutos: cruzó el 80 %.
	if n := x.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'voz.tope_alcanzado' AND payload->>'tipo' = 'salientes' AND (payload->>'porcentaje')::int = 80
		AND (payload->>'tope')::int = $1`, tope); n != 1 {
		t.Fatalf("voz.tope_alcanzado al 80 %%: %d", n)
	}
	if n := x.contar(`SELECT coalesce(sum(duracion_seg), 0)::int FROM mensajeria.llamadas WHERE empresa_id = 1 AND direccion = 'saliente' AND contestada_en IS NOT NULL`); n != usados+dur {
		t.Fatalf("minutos del mes: %d, esperaba %d", n, usados+dur)
	}
	// Un terminate sin duración (o con 0) de una que marcaba sigue siendo una llamada perdida.
	x.pasa(61 * time.Second)
	_, call2 := x.saliente(x.supervisor, conv, nil)
	x.respuestaSaliente(num, call2)
	x.webhookSaliente(num, call2, "terminate", `,"status":"COMPLETED","duration":0`)
	if _, estado, _ := x.llamada(call2); estado != "perdida" {
		t.Fatalf("terminate con duración 0: %s", estado)
	}
	// El ACCEPTED que llega tarde, después del terminate, no reabre nada.
	x.estadoSaliente(num, call, "ACCEPTED")
	if _, estado, _ := x.llamada(call); estado != "terminada" || x.eventos("llamada.contestada", id) != 1 {
		t.Fatalf("ACCEPTED tardío: %s", estado)
	}
}

// supervisores: n personas con llamadas.llamar, cada una en su navegador.
func (x *bandejaVoz) supervisores(n int) []persona {
	ps := make([]persona, n)
	for i := range ps {
		ps[i] = x.persona([]int64{1}, rolBase("supervisor"))
		ps[i].navegador = fmt.Sprintf("navegador-sup-%d", ps[i].id)
	}
	return ps
}

// A1: los límites de las salientes no se burlan con peticiones en paralelo.
func TestLlamadaSalienteEnParalelo(t *testing.T) {
	x := nuevaBandejaVoz(t)
	x.limitesVoz(1000, 100000)
	t.Cleanup(func() { x.limitesVoz(1000, 100000) })
	const n = 8
	convs := make([]string, n)
	for i := range convs {
		_, convs[i] = x.convConPermiso()
	}
	enParalelo := func(quien func(i int) persona) (hechas, conflicto, limite int) {
		var wg sync.WaitGroup
		rs := make([]respuesta, n)
		for i := range n {
			wg.Add(1)
			go func() {
				defer wg.Done()
				rs[i] = x.llamar(quien(i), convs[i], nil)
			}()
		}
		wg.Wait()
		for _, r := range rs {
			switch {
			case r.status == http.StatusCreated:
				hechas++
			case r.status == http.StatusConflict && strings.Contains(crudo(r), "llamada_en_curso"):
				conflicto++
			case r.status == http.StatusUnprocessableEntity && strings.Contains(crudo(r), "limite_llamadas"):
				limite++
			default:
				t.Errorf("respuesta inesperada en paralelo: %d %s", r.status, crudo(r))
			}
		}
		return hechas, conflicto, limite
	}
	// La misma persona llama a 8 conversaciones a la vez: sale una sola.
	if hechas, conflicto, _ := enParalelo(func(int) persona { return x.supervisor }); hechas != 1 || conflicto != n-1 {
		t.Fatalf("una por persona en paralelo: %d hechas, %d en conflicto", hechas, conflicto)
	}
	if v := x.contar(`SELECT count(*) FROM mensajeria.llamadas WHERE usuario_id = $1 AND direccion = 'saliente' AND estado IN ('marcando', 'en_curso')`, x.supervisor.id); v != 1 {
		t.Fatalf("salientes vivas de la misma persona: %d", v)
	}
	x.sql(`UPDATE mensajeria.llamadas SET estado = 'terminada', terminada_en = sono_en, motivo_fin = 'agente_colgo', contestada_en = sono_en, duracion_seg = 0
		WHERE usuario_id = $1 AND estado = 'marcando'`, x.supervisor.id)
	// Ocho personas distintas a la vez con sitio para dos más hoy: salen dos.
	hoy := x.contar(`SELECT count(*) FROM mensajeria.llamadas WHERE empresa_id = 1 AND direccion = 'saliente' AND estado <> 'fallida' AND sono_en >= $1`, x.reloj.Add(-10*time.Hour))
	x.limitesVoz(hoy+2, 100000)
	x.pasa(61 * time.Second)
	sups := x.supervisores(n)
	if hechas, _, limite := enParalelo(func(i int) persona { return sups[i] }); hechas != 2 || limite != n-2 {
		t.Fatalf("máximo diario en paralelo: %d hechas, %d por límite", hechas, limite)
	}
	// El índice único es la segunda barrera: dos salientes vivas de la misma persona no caben.
	if _, err := x.bd.Exec(x.ctx, `INSERT INTO mensajeria.llamadas (id, empresa_id, origen, canal_id, id_externo, direccion, numero, estado, usuario_id, sono_en, suena_hasta)
		SELECT gen_random_uuid(), 1, 'whatsapp', $1, 'wacid.doble' || n, 'saliente', '+50255550000', 'marcando', $2, now(), now() FROM generate_series(1, 2) n`,
		x.canal.ID, x.agente.id); err == nil || !strings.Contains(err.Error(), "llamadas_saliente_viva_uq") {
		t.Fatalf("el índice único de saliente viva debía impedirlo: %v", err)
	}
}

// A2: el tope cuenta lo que llevan las llamadas en curso y el mantenimiento corta al cruzarlo.
func TestLlamadaSalienteTopeEnCurso(t *testing.T) {
	x := nuevaBandejaVoz(t)
	x.limitesVoz(1000, 100000)
	t.Cleanup(func() { x.limitesVoz(1000, 100000) })
	num, conv := x.convConPermiso()
	_, conv2 := x.convConPermiso()
	usados := x.contar(`SELECT coalesce(sum(duracion_seg), 0)::int FROM mensajeria.llamadas WHERE empresa_id = 1 AND direccion = 'saliente' AND contestada_en IS NOT NULL`)
	tope := usados/60 + 2
	queda := tope*60 - usados // entre 61 y 120 segundos
	x.limitesVoz(1000, tope)
	id, call := x.saliente(x.supervisor, conv, nil)
	x.respuestaSaliente(num, call)
	x.estadoSaliente(num, call, "ACCEPTED")
	x.pasa(time.Duration(queda-10)*time.Second, x.supervisor)
	x.mantener()
	if _, estado, _ := x.llamada(call); estado != "en_curso" {
		t.Fatalf("antes del tope sigue en curso: %s", estado)
	}
	// Cruzó el tope con la llamada todavía en curso: nadie más llama, aunque el mantenimiento no haya corrido.
	x.pasa(11*time.Second, x.supervisor)
	otro := x.supervisores(1)[0]
	if r := x.exigir(x.llamar(otro, conv2, nil), http.StatusUnprocessableEntity); !strings.Contains(crudo(r), "tope_alcanzado") {
		t.Fatalf("llamar con el tope cruzado por una llamada en curso: %s", crudo(r))
	}
	// Y el mantenimiento la corta: dura, como mucho, lo que quedaba (más un ciclo).
	x.mantener()
	v := x.exigir(x.api(x.supervisor, http.MethodGet, "/llamadas/"+id, nil), http.StatusOK)
	if v.cuerpo["estado"] != "terminada" || v.cuerpo["motivoFin"] != "tope_alcanzado" || v.cuerpo["duracionSeg"] != float64(queda+1) || x.g.cuenta("terminate:"+call) != 1 {
		t.Fatalf("cortada por el tope: %s %v", crudo(v), x.g.hechas())
	}
	if n := x.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'voz.tope_alcanzado' AND payload->>'tipo' = 'salientes' AND (payload->>'porcentaje')::int = 100
		AND (payload->>'tope')::int = $1`, tope); n != 1 {
		t.Fatalf("voz.tope_alcanzado al 100 %%: %d", n)
	}
}

// flujoDe abre el flujo de eventos de la bandeja como p (con su navegador, si tiene) y espera a que
// el servidor lo haya atendido; lo cierra antes de volver.
func (x *bandejaVoz) flujoDe(p persona, porQuery bool) {
	x.t.Helper()
	srv := httptest.NewServer(x.h)
	defer srv.Close()
	ctx, cancelar := context.WithTimeout(x.ctx, 5*time.Second)
	defer cancelar()
	url := srv.URL + rutaMsj + "/conversaciones/eventos"
	if porQuery && p.navegador != "" {
		url += "?navegador=" + p.navegador
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	// El flujo es de una sesión del panel (cookie), no de un token.
	req.AddCookie(&http.Cookie{Name: identidad.CookieSesion, Value: x.sesion(p).cookie})
	req.Header.Set("Accept", "text/event-stream")
	if !porQuery && p.navegador != "" {
		req.Header.Set(mensajeria.CabeceraNavegador, p.navegador)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		x.t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		cuerpo, _ := io.ReadAll(res.Body)
		x.t.Fatalf("flujo de eventos: %d %s (%s)", res.StatusCode, cuerpo, url)
	}
	b := make([]byte, 64)
	if _, err := res.Body.Read(b); err != nil {
		x.t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond) // el latido del flujo toca la fila justo después de abrir
}

// M1: el latido que mantiene viva una saliente es el del navegador desde el que se llamó.
func TestLlamadaSalienteLatidoDelNavegador(t *testing.T) {
	x := nuevaBandejaVoz(t)
	x.limitesVoz(1000, 100000)
	num, conv := x.convConPermiso()
	enCurso := func() (id, call string) {
		t.Helper()
		id, call = x.saliente(x.supervisor, conv, nil)
		x.respuestaSaliente(num, call)
		x.estadoSaliente(num, call, "ACCEPTED")
		return id, call
	}
	visto := func(nav string) time.Time {
		t.Helper()
		var v time.Time
		if err := x.bd.QueryRow(x.ctx, `SELECT visto_en FROM mensajeria.llamada_receptores WHERE usuario_id = $1 AND navegador = $2`, x.supervisor.id, nav).Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	_, call := enCurso()
	inicio := visto(x.supervisor.navegador)
	x.pasa(50 * time.Second)
	// Otra pestaña u otro equipo de la misma persona (otro navegador, o un panel viejo sin navegador)
	// no mantiene viva la llamada.
	otroNav := x.supervisor
	otroNav.navegador = "navegador-de-otro-equipo"
	x.exigir(x.api(otroNav, http.MethodPost, "/llamadas/recibir/encender", map[string]any{"navegador": otroNav.navegador}), http.StatusOK)
	x.flujoDe(otroNav, true)
	sinNav := x.supervisor
	sinNav.navegador = ""
	x.flujoDe(sinNav, true)
	if v := visto(x.supervisor.navegador); !v.Equal(inicio) {
		t.Fatalf("el flujo de otro navegador tocó el navegador de la llamada: %s → %s", inicio, v)
	}
	if v := visto(otroNav.navegador); !v.Equal(*x.reloj) {
		t.Fatalf("el flujo debía tocar su propio navegador: %s", v)
	}
	x.pasa(10 * time.Second)
	x.mantener()
	if _, estado, motivo := x.llamada(call); estado != "terminada" || motivo != "error_medio" {
		t.Fatalf("sin el latido de su navegador se corta: %s %s", estado, motivo)
	}
	// Con el flujo del navegador que llamó (por parámetro o por cabecera) sigue viva.
	for _, porQuery := range []bool{true, false} {
		x.pasa(61 * time.Second)
		_, call2 := enCurso()
		x.pasa(50 * time.Second)
		x.flujoDe(x.supervisor, porQuery)
		x.pasa(10 * time.Second)
		x.mantener()
		if _, estado, _ := x.llamada(call2); estado != "en_curso" {
			t.Fatalf("con el latido de su navegador (por query: %v) sigue en curso: %s", porQuery, estado)
		}
		x.exigir(x.api(x.supervisor, http.MethodPost, "/llamadas/"+x.idDe(call2)+"/colgar", nil), http.StatusOK)
	}
}

func (x *bandejaVoz) idDe(callID string) string {
	id, _, _ := x.llamada(callID)
	return id
}

// M2 y M3: ninguna llamada queda colgada en Meta, y un webhook con otro call_id no se pega a la llamada nueva.
func TestLlamadaSalienteHuerfanasYCallID(t *testing.T) {
	x := nuevaBandejaVoz(t)
	x.limitesVoz(1000, 100000)
	num, conv := x.convConPermiso()
	huerfanas := func(call, estado string) int {
		return x.contar(`SELECT count(*) FROM mensajeria.llamadas_huerfanas WHERE canal_id = $1 AND call_id = $2 AND estado = $3`, x.canal.ID, call, estado)
	}
	sdpGuardado := func(call string) int {
		return x.contar(`SELECT count(*) FROM mensajeria.entrantes WHERE canal_id = $1 AND payload::text LIKE '%' || $2 || '%' AND payload::text LIKE '%fingerprint%'`, x.canal.ID, call)
	}

	// Un connect saliente de un call_id desconocido SIN rastro de ser de Iris (ningún connect de Iris a
	// ese número en los últimos 2 minutos): puede ser de otro sistema que usa el mismo número. No se
	// termina: queda anotada (sin el número) y su SDP no se guarda.
	ajena := "wacid.ajena" + num
	x.respuestaSaliente(num, ajena)
	x.estadoSaliente(num, ajena, "ACCEPTED")
	x.despachar()
	if huerfanas(ajena, "ajena") != 1 || x.g.cuenta("terminate:"+ajena) != 0 || sdpGuardado(ajena) != 0 {
		t.Fatalf("call_id ajeno: %d anotadas, terminate %d, %d webhooks con SDP", huerfanas(ajena, "ajena"), x.g.cuenta("terminate:"+ajena), sdpGuardado(ajena))
	}
	if n := x.contar(`SELECT count(*) FROM auditoria.acciones WHERE entrada::text LIKE '%' || $1 || '%' OR resultado::text LIKE '%' || $1 || '%'`, num); n != 0 {
		t.Fatalf("el número no debía quedar en la auditoría: %d", n)
	}
	// Con rastro (Iris llamó a ese número hace menos de 2 minutos y la llamada ya está cerrada): un
	// call_id desconocido sí se termina en Meta, con reintentos, y su SDP no queda guardado.
	idR, _ := x.saliente(x.supervisor, conv, nil)
	x.exigir(x.api(x.supervisor, http.MethodPost, "/llamadas/"+idR+"/colgar", nil), http.StatusOK)
	x.pasa(30 * time.Second)
	rastro := "wacid.rastro" + num
	x.g.falla("terminate", &mensajeria.ErrMetaLlamada{Status: 500})
	x.respuestaSaliente(num, rastro)
	if huerfanas(rastro, "pendiente") != 1 || sdpGuardado(rastro) != 0 {
		t.Fatalf("call_id con rastro: %d huérfanas, %d webhooks con SDP", huerfanas(rastro, "pendiente"), sdpGuardado(rastro))
	}
	x.despachar()
	x.despachar() // apartada: el segundo ciclo no la repite
	if x.g.cuenta("terminate:"+rastro) != 1 || huerfanas(rastro, "pendiente") != 1 {
		t.Fatalf("primer intento fallido: %v", x.g.hechas())
	}
	x.g.falla("terminate", nil)
	x.pasa(21 * time.Second)
	x.despachar()
	if x.g.cuenta("terminate:"+rastro) != 2 || huerfanas(rastro, "hecha") != 1 {
		t.Fatalf("reintento: %v", x.g.hechas())
	}
	// Pasados los 2 minutos desde el connect de Iris, otro desconocido ya no se toca.
	x.pasa(2 * time.Minute)
	tarde := "wacid.tarde" + num
	x.respuestaSaliente(num, tarde)
	x.despachar()
	if huerfanas(tarde, "ajena") != 1 || x.g.cuenta("terminate:"+tarde) != 0 {
		t.Fatalf("desconocido fuera de la ventana: %v", x.g.hechas())
	}
	x.estadoSaliente(num, "wacid.rechazada"+num, "REJECTED") // ya no vive en Meta: nada que terminar
	if huerfanas("wacid.rechazada"+num, "pendiente") != 0 {
		t.Fatal("un REJECTED sin llamada no deja nada por terminar")
	}

	// Se cuelga mientras el connect está en vuelo: la llamada existe en Meta y hay que terminarla. La
	// oferta SDP nunca se guardó.
	x.gancho(func(accion string) {
		if accion != "connect" {
			return
		}
		var id string
		if err := x.bd.QueryRow(x.ctx, `SELECT id::text FROM mensajeria.llamadas WHERE conversacion_id = $1 AND estado = 'marcando' AND sdp_oferta IS NULL`, conv).Scan(&id); err != nil {
			t.Errorf("la llamada que marca (sin oferta guardada): %v", err)
			return
		}
		if r := x.api(x.supervisor, http.MethodPost, "/llamadas/"+id+"/colgar?propia=true", nil); r.status != http.StatusOK {
			t.Errorf("colgar con el connect en vuelo: %d %s", r.status, crudo(r))
		}
	})
	r := x.exigir(x.llamar(x.supervisor, conv, nil), http.StatusCreated)
	x.gancho(nil)
	enVuelo := x.g.ultima()
	if r.cuerpo["estado"] != "terminada" || x.g.cuenta("terminate:"+enVuelo) != 1 || huerfanas(enVuelo, "hecha") != 1 {
		t.Fatalf("colgada con el connect en vuelo: %s %v", crudo(r), x.g.hechas())
	}
	// Su webhook llega después: ni la reabre ni deja su SDP guardado.
	x.respuestaSaliente(num, enVuelo)
	if sdpGuardado(enVuelo) != 0 || x.contar(`SELECT count(*) FROM mensajeria.llamadas WHERE conversacion_id = $1 AND estado IN ('marcando', 'en_curso')`, conv) != 0 {
		t.Fatal("el webhook de una llamada ya colgada no debía dejar nada")
	}

	// M3: un webhook con OTRO call_id llega mientras el connect está en vuelo. No se pega a la llamada
	// nueva: manda el call_id de la respuesta del connect, y el otro se termina.
	x.pasa(61 * time.Second)
	intruso := "wacid.intruso" + num
	x.gancho(func(accion string) {
		if accion == "connect" {
			x.respuestaSaliente(num, intruso)
			x.estadoSaliente(num, intruso, "ACCEPTED")
		}
	})
	id, call := x.saliente(x.supervisor, conv, nil)
	x.gancho(nil)
	if n := x.contar(`SELECT count(*) FROM mensajeria.llamadas WHERE id = $1::uuid AND id_externo = $2 AND estado = 'marcando' AND sdp_respuesta IS NULL AND externo_webhook IS NULL`, id, call); n != 1 {
		t.Fatal("la llamada debía quedarse con el call_id del connect, sin la respuesta del otro")
	}
	if x.g.cuenta("terminate:"+intruso) != 1 || huerfanas(intruso, "hecha") != 1 || sdpGuardado(intruso) != 0 || x.g.cuenta("terminate:"+call) != 0 {
		t.Fatalf("el call_id discrepante debía terminarse: %v", x.g.hechas())
	}
	x.exigir(x.api(x.supervisor, http.MethodGet, "/llamadas/"+id+"/respuesta-sdp", nil), http.StatusConflict)
	if n := x.contar(`SELECT count(*) FROM mensajeria.conv_eventos WHERE llamada_id = $1::uuid AND tipo = 'llamada_respuesta'`, id); n != 0 {
		t.Fatal("no debía avisarse la respuesta de otra llamada")
	}
	// La suya, con su call_id, sí.
	x.respuestaSaliente(num, call)
	x.exigir(x.api(x.supervisor, http.MethodGet, "/llamadas/"+id+"/respuesta-sdp", nil), http.StatusOK)
	x.exigir(x.api(x.supervisor, http.MethodPost, "/llamadas/"+id+"/colgar", nil), http.StatusOK)
}

// Timbrar y colgar, la plantilla de permiso contra la cuenta de WhatsApp y el límite de solicitudes
// por contacto en todos los números de la empresa.
func TestLlamadaSalienteAbusoYPlantilla(t *testing.T) {
	x := nuevaBandejaVoz(t)
	x.limitesVoz(1000, 100000)
	_, conv := x.convConPermiso()
	// Tres salientes colgadas mientras marcaban al mismo contacto: hoy ya no se le llama.
	for range 3 {
		id, _ := x.saliente(x.supervisor, conv, nil)
		x.exigir(x.api(x.supervisor, http.MethodPost, "/llamadas/"+id+"/colgar", nil), http.StatusOK)
		x.pasa(61 * time.Second)
	}
	if r := x.exigir(x.llamar(x.supervisor, conv, nil), http.StatusUnprocessableEntity); !strings.Contains(crudo(r), "limite_llamadas") || !strings.Contains(crudo(r), "colgadas_marcando") {
		t.Fatalf("timbrar y colgar: %s", crudo(r))
	}
	// Es por contacto: a otro sí se le llama. Y es configurable.
	_, conv2 := x.convConPermiso()
	id2, _ := x.saliente(x.supervisor, conv2, nil)
	x.exigir(x.api(x.supervisor, http.MethodPost, "/llamadas/"+id2+"/colgar", nil), http.StatusOK)
	admin := x.persona([]int64{1}, rolBase("admin"))
	admin.empresa = 1
	v := x.exigir(x.api(admin, http.MethodGet, "/voz/ajustes", nil), http.StatusOK)
	if v.cuerpo["maxColgadasMarcandoDia"] != float64(3) {
		t.Fatalf("de fábrica: %s", crudo(v))
	}
	x.exigir(x.api(admin, http.MethodPost, "/voz/ajustes/actualizar", map[string]any{"version": v.cuerpo["version"], "duracionMaxPersonaMin": v.cuerpo["duracionMaxPersonaMin"],
		"segundosAsignado": v.cuerpo["segundosAsignado"], "maxColgadasMarcandoDia": 5}), http.StatusOK)
	x.pasa(61 * time.Second)
	id3, _ := x.saliente(x.supervisor, conv, nil)
	x.exigir(x.api(x.supervisor, http.MethodPost, "/llamadas/"+id3+"/colgar", nil), http.StatusOK)
	v = x.exigir(x.api(admin, http.MethodGet, "/voz/ajustes", nil), http.StatusOK)
	x.exigir(x.api(admin, http.MethodPost, "/voz/ajustes/actualizar", map[string]any{"version": v.cuerpo["version"], "duracionMaxPersonaMin": v.cuerpo["duracionMaxPersonaMin"],
		"segundosAsignado": v.cuerpo["segundosAsignado"], "maxColgadasMarcandoDia": 3}), http.StatusOK)

	// La plantilla de permiso se comprueba en la cuenta de WhatsApp al guardarla.
	for nombre, plantilla := range map[string]string{"no existe": "inventada", "sin aprobar": "permiso_en_revision", "de otra categoría": "codigo_de_acceso"} {
		if r := x.exigir(x.configurar(x.versionLlamadas(), map[string]any{"plantillaPermiso": plantilla}), http.StatusUnprocessableEntity); !strings.Contains(crudo(r), "plantilla_permiso_invalida") {
			t.Fatalf("plantilla %s: %s", nombre, crudo(r))
		}
	}
	if r := x.exigir(x.configurar(x.versionLlamadas(), map[string]any{"plantillaPermiso": "permiso_de_llamada", "plantillaPermisoIdioma": "fr"}), http.StatusUnprocessableEntity); !strings.Contains(crudo(r), "plantilla_permiso_invalida") {
		t.Fatalf("plantilla en otro idioma: %s", crudo(r))
	}
	x.g.falla("plantilla", &mensajeria.ErrMetaLlamada{SinRespuesta: true})
	x.exigir(x.configurar(x.versionLlamadas(), map[string]any{"plantillaPermiso": "permiso_de_llamada"}), http.StatusBadGateway)
	x.g.falla("plantilla", nil)
	if c := x.exigir(x.api(x.supervisor, http.MethodGet, x.rutaCanal("/llamadas"), nil), http.StatusOK); c.cuerpo["plantillaPermiso"] != nil {
		t.Fatalf("ninguna de esas debía guardarse: %s", crudo(c))
	}
	x.exigir(x.configurar(x.versionLlamadas(), map[string]any{"plantillaPermiso": "permiso_de_llamada"}), http.StatusOK)

	// El límite de solicitudes es por contacto: cuenta las hechas desde otro número de la empresa.
	empresa := int64(1)
	otro, err := x.m.CrearCanal.Ejecutar(x.ctx, mensajeria.EntradaCrearCanal{EmpresaID: &empresa, Nombre: "Ventas", Numero: "+50222000099",
		PhoneNumberID: fmt.Sprint(time.Now().UnixNano()%1e12 + 7), Token: "token-de-meta-0123456789abcdef", AppSecret: secretoWA, VerifyToken: "verify-token-0123456789",
		Usos: []string{"soporte"}, ColaID: &x.cola})
	if err != nil {
		t.Fatal(err)
	}
	num := numeroCliente()
	x.texto(num, "wamid.contacto"+num, "Hola")
	conv3 := cid(x.conv(num))
	x.sql(`INSERT INTO mensajeria.llamada_permisos (empresa_id, canal_id, numero, estado, solicitudes) VALUES (1, $1, $2, 'pedido', jsonb_build_array($3::timestamptz))`,
		otro.ID, "+"+num, x.reloj.Add(-2*time.Hour))
	p := x.exigir(x.api(x.agente, http.MethodGet, "/conversaciones/"+conv3+"/permiso-llamada", nil), http.StatusOK)
	if p.cuerpo["puedeSolicitar"] != false || p.cuerpo["proximaSolicitudEn"] == nil {
		t.Fatalf("pedido hace 2 h desde otro número: %s", crudo(p))
	}
	if r := x.exigir(x.api(x.agente, http.MethodPost, "/conversaciones/"+conv3+"/solicitar-permiso-llamada", nil), http.StatusUnprocessableEntity); !strings.Contains(crudo(r), "limite_solicitudes") {
		t.Fatalf("solicitar desde este número: %s", crudo(r))
	}
	// Pasadas 24 h sí; y con dos en la semana (una por número), ya no hasta que pasen 7 días.
	x.pasa(23 * time.Hour)
	x.texto(num, "wamid.contacto2"+num, "Sigo aquí")
	x.exigir(x.api(x.agente, http.MethodPost, "/conversaciones/"+conv3+"/solicitar-permiso-llamada", nil), http.StatusOK)
	x.pasa(25 * time.Hour)
	x.texto(num, "wamid.contacto3"+num, "¿Y?")
	if r := x.exigir(x.api(x.agente, http.MethodPost, "/conversaciones/"+conv3+"/solicitar-permiso-llamada", nil), http.StatusUnprocessableEntity); !strings.Contains(crudo(r), "limite_solicitudes") {
		t.Fatalf("tercera en la semana contando los dos números: %s", crudo(r))
	}
}
