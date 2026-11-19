package mensajeria_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/mensajeria"
)

// rafagaEnHilo: el objeto rafaga de la tarjeta de la llamada (leída como el panel, por la API).
func (x *bandejaVoz) rafagaEnHilo(conv, llamadaID string) map[string]any {
	x.t.Helper()
	r := x.exigir(x.api(x.supervisor, http.MethodGet, "/conversaciones/"+conv+"/mensajes?limit=100", nil), http.StatusOK)
	data, _ := r.cuerpo["data"].([]any)
	for _, m := range data {
		l, _ := m.(map[string]any)["llamada"].(map[string]any)
		if l != nil && l["id"] == llamadaID {
			rf, _ := l["rafaga"].(map[string]any)
			return rf
		}
	}
	x.t.Fatalf("la llamada %s no está en el hilo", llamadaID)
	return nil
}

// P1 de la revisión de #217: la ráfaga se decide antes de registrar nada. La primera que rechaza deja una
// sola tarjeta (sin reabrir la conversación ni emitir llamada.entrante ni conversacion.mensaje_recibido);
// los reintentos de la misma ventana solo suman a esa tarjeta y se rechazan en Meta.
func TestRafagaRastroAcotado(t *testing.T) {
	x := nuevaBandejaVoz(t)
	x.recibe(x.agente)
	num := numeroCliente()
	x.texto(num, "wamid.raf"+num, "Hola")
	conv := cid(x.conv(num))
	for i := range 3 {
		call := fmt.Sprintf("wacid.acot%d%s", i, num)
		x.connect(num, call)
		x.webhook(x.eventoLlamada(num, call, "terminate", "", 0))
		x.pasa(time.Second, x.agente)
	}
	// Se resuelve la conversación: la ráfaga no la reabre.
	c := x.conv(num)
	x.exigir(x.api(x.supervisor, http.MethodPost, "/conversaciones/"+conv+"/resolver", map[string]any{"version": c["version"]}), http.StatusOK)
	mensajes := x.contar(`SELECT count(*) FROM mensajeria.conversacion_mensajes WHERE conversacion_id = $1::bigint`, conv)
	primera := "wacid.acot3" + num
	x.connect(num, primera)
	id, estado, motivo := x.llamada(primera)
	if estado != "rechazada" || motivo != "limite" || x.g.cuenta("reject:"+primera) != 1 {
		t.Fatalf("primera de la ráfaga: %s %s", estado, motivo)
	}
	if x.eventos("llamada.entrante", id) != 0 || x.eventos("llamada.rechazada", id) != 1 ||
		x.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'conversacion.mensaje_recibido' AND payload->>'conversacion_id' = $1 AND payload->>'tipo' = 'llamada'`, conv) != 3 {
		t.Fatal("la llamada que rechaza la ráfaga no debía emitir llamada.entrante ni mensaje_recibido")
	}
	if x.conv(num)["estado"] != "resuelta" {
		t.Fatal("la ráfaga no debía reabrir la conversación")
	}
	// Reintentos: sin fila, sin mensaje, contados en la tarjeta y rechazados en Meta. La tarjeta se
	// refresca en el feed como mucho cada 30 s (B1): dos reintentos seguidos dejan un solo refresco.
	feed := func() int {
		return x.contar(`SELECT count(*) FROM mensajeria.conv_eventos WHERE llamada_id = $1::uuid AND tipo = 'llamada_actualizada'`, id)
	}
	antes := feed()
	for i := 4; i < 7; i++ {
		call := fmt.Sprintf("wacid.acot%d%s", i, num)
		if i == 4 {
			x.pasa(31*time.Second, x.agente)
		} else {
			x.pasa(5*time.Second, x.agente)
		}
		x.connect(num, call)
		if n := x.contar(`SELECT count(*) FROM mensajeria.llamadas WHERE id_externo = $1`, call); n != 0 || x.g.cuenta("reject:"+call) != 1 {
			t.Fatalf("reintento %d: filas %d, reject %d", i, n, x.g.cuenta("reject:"+call))
		}
	}
	if n := feed() - antes; n != 1 {
		t.Fatalf("refrescos de la tarjeta en el feed: %d, esperaba 1 (como mucho cada 30 s)", n)
	}
	// B2: el mismo call_id otra vez (Meta reintenta el webhook; aquí con otro timestamp para que no lo
	// descarte antes la deduplicación de entrantes) no cuenta dos veces ni se rechaza otra vez.
	x.pasa(time.Second, x.agente)
	x.connect(num, "wacid.acot6"+num)
	if x.g.cuenta("reject:wacid.acot6"+num) != 1 {
		t.Fatal("un connect repetido no debía rechazarse otra vez")
	}
	if n := x.contar(`SELECT count(*) FROM mensajeria.conversacion_mensajes WHERE conversacion_id = $1::bigint`, conv); n != mensajes+1 {
		t.Fatalf("mensajes en el hilo: %d, esperaba %d (una sola tarjeta)", n, mensajes+1)
	}
	rf := x.rafagaEnHilo(conv, id)
	if rf == nil || rf["intentos"] != float64(3) || rf["levantada"] != false || rf["hasta"] == nil {
		t.Fatalf("tarjeta de la ráfaga: %v", rf)
	}
}

// Con la ráfaga encima, un connect viejo (p. ej. reprocesado por el job) se rechaza sin fila: no deja
// una perdida con mensaje, devolución y aviso al cliente.
func TestRafagaConnectViejo(t *testing.T) {
	x := nuevaBandejaVoz(t)
	x.recibe(x.agente)
	num := numeroCliente()
	x.texto(num, "wamid.rafv"+num, "Hola")
	conv := cid(x.conv(num))
	for i := range 3 {
		call := fmt.Sprintf("wacid.viejo%d%s", i, num)
		x.connect(num, call)
		x.webhook(x.eventoLlamada(num, call, "terminate", "", 0))
		x.pasa(time.Second, x.agente)
	}
	mensajes := x.contar(`SELECT count(*) FROM mensajeria.conversacion_mensajes WHERE conversacion_id = $1::bigint`, conv)
	viejo := "wacid.viejo3" + num
	x.webhook(x.eventoLlamada(num, viejo, "connect", fmt.Sprintf(`,"session":{"sdp_type":"offer","sdp":%q}`, sdpOfertaMeta), 2*time.Minute))
	if n := x.contar(`SELECT count(*) FROM mensajeria.llamadas WHERE id_externo = $1`, viejo); n != 0 || x.g.cuenta("reject:"+viejo) != 1 {
		t.Fatalf("connect viejo con ráfaga: filas %d, reject %d", n, x.g.cuenta("reject:"+viejo))
	}
	if n := x.contar(`SELECT count(*) FROM mensajeria.conversacion_mensajes WHERE conversacion_id = $1::bigint`, conv); n != mensajes {
		t.Fatalf("el connect viejo dejó mensajes: %d → %d", mensajes, n)
	}
	if n := x.contar(`SELECT count(*) FROM mensajeria.llamada_devoluciones d JOIN mensajeria.llamadas l ON l.id = d.llamada_id
		WHERE l.numero = $1 AND l.id_externo = $2`, "+"+num, viejo); n != 0 {
		t.Fatalf("el connect viejo dejó una devolución: %d", n)
	}
}

// Quitar el bloqueo a mano: «Dejar que vuelva a llamar» (llamadas.desbloquear_numero, supervisor y
// admin). Las entrantes anteriores dejan de contar y la próxima suena; queda auditado y con su evento.
func TestRafagaLevantadaAMano(t *testing.T) {
	x := nuevaBandejaVoz(t)
	x.recibe(x.agente)
	num := numeroCliente()
	x.texto(num, "wamid.rafm"+num, "Hola")
	conv := cid(x.conv(num))
	for i := range 4 {
		call := fmt.Sprintf("wacid.mano%d%s", i, num)
		x.connect(num, call)
		if i < 3 {
			x.webhook(x.eventoLlamada(num, call, "terminate", "", 0))
		}
		x.pasa(time.Second, x.agente)
	}
	id, estado, _ := x.llamada("wacid.mano3" + num)
	if estado != "rechazada" {
		t.Fatalf("la cuarta: %s", estado)
	}
	ruta := "/conversaciones/" + conv + "/llamadas/desbloquear"
	x.exigir(x.api(x.agente, http.MethodPost, ruta, nil), http.StatusForbidden)
	x.exigir(x.api(x.otro, http.MethodPost, ruta, nil), http.StatusForbidden)
	r := x.exigir(x.api(x.supervisor, http.MethodPost, ruta, nil), http.StatusOK)
	if r.cuerpo["estabaActiva"] != true {
		t.Fatalf("desbloquear: %s", crudo(r))
	}
	if n := x.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'voz.rafaga_levantada' AND payload->>'origen' = 'manual'
		AND payload->>'conversacion_id' = $1 AND NOT (payload::text LIKE '%' || $2 || '%')`, conv, num); n != 1 {
		t.Fatalf("evento voz.rafaga_levantada (sin el número): %d", n)
	}
	if n := x.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'llamadas.desbloquear_numero' AND resultado = 'ok' AND actor_id = $1`, x.supervisor.id); n != 1 {
		t.Fatalf("auditoría: %d", n)
	}
	if rf := x.rafagaEnHilo(conv, id); rf["levantada"] != true || rf["puedeLevantar"] != true {
		t.Fatalf("la tarjeta debía decir que ya se levantó: %v", rf)
	}
	// La siguiente suena (sin esperar los 10 minutos).
	x.pasa(time.Second, x.agente)
	if _, estado, motivo := x.llamada(x.conectar(num, "wacid.mano4"+num)); estado != "sonando" {
		t.Fatalf("tras levantar la ráfaga debía sonar: %s %s", estado, motivo)
	}
	// Repetirlo sin ráfaga a la vista: la marca se mueve igual (gana la carrera contra una entrante en
	// proceso) y queda auditado, pero no sale el evento.
	var marca time.Time
	if err := x.bd.QueryRow(x.ctx, `SELECT levantada_en FROM mensajeria.voz_rafagas_levantadas WHERE numero = $1`, "+"+num).Scan(&marca); err != nil {
		t.Fatal(err)
	}
	x.pasa(time.Second, x.agente)
	if r := x.exigir(x.api(x.supervisor, http.MethodPost, ruta, nil), http.StatusOK); r.cuerpo["estabaActiva"] != false {
		t.Fatalf("sin ráfaga: %s", crudo(r))
	}
	var marca2 time.Time
	if err := x.bd.QueryRow(x.ctx, `SELECT levantada_en FROM mensajeria.voz_rafagas_levantadas WHERE numero = $1`, "+"+num).Scan(&marca2); err != nil || !marca2.After(marca) {
		t.Fatalf("a mano la marca debía moverse siempre: %v %v %v", err, marca, marca2)
	}
	if n := x.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'voz.rafaga_levantada' AND payload->>'conversacion_id' = $1`, conv); n != 1 {
		t.Fatalf("eventos tras repetir sin ráfaga: %d", n)
	}
	if n := x.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'llamadas.desbloquear_numero' AND resultado = 'ok' AND actor_id = $1`, x.supervisor.id); n != 2 {
		t.Fatalf("la repetición debía quedar auditada: %d", n)
	}
	// I1: pasados 10 minutos la marca ya no cambia nada y guarda el número: el mantenimiento la borra.
	if n := x.contar(`SELECT count(*) FROM mensajeria.voz_rafagas_levantadas WHERE numero = $1`, "+"+num); n != 1 {
		t.Fatalf("marca de la ráfaga levantada: %d", n)
	}
	x.webhook(x.eventoLlamada(num, "wacid.mano4"+num, "terminate", "", 0))
	x.pasa(11*time.Minute, x.agente)
	x.mantener()
	if n := x.contar(`SELECT count(*) FROM mensajeria.voz_rafagas_levantadas WHERE numero = $1`, "+"+num); n != 0 {
		t.Fatalf("la marca vieja debía borrarse: %d", n)
	}
}

// Llamar levanta el bloqueo: una saliente a ese número que WhatsApp acepta levanta la ráfaga.
func TestRafagaLevantadaAlLlamar(t *testing.T) {
	x := nuevaBandejaVoz(t)
	x.limitesVoz(1000, 100000)
	x.recibe(x.agente)
	num, conv := x.convConPermiso()
	for i := range 4 {
		call := fmt.Sprintf("wacid.llam%d%s", i, num)
		x.connect(num, call)
		if i < 3 {
			x.webhook(x.eventoLlamada(num, call, "terminate", "", 0))
		}
		x.pasa(time.Second, x.agente)
	}
	if _, estado, _ := x.llamada("wacid.llam3" + num); estado != "rechazada" {
		t.Fatalf("la cuarta: %s", estado)
	}
	// Si Meta rechaza el connect, la saliente no salió: la ráfaga sigue (ni marca ni evento).
	x.g.falla("connect", &mensajeria.ErrMetaLlamada{Status: 400})
	x.exigir(x.llamar(x.supervisor, conv, nil), http.StatusBadGateway)
	x.g.falla("connect", nil)
	if n := x.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'voz.rafaga_levantada' AND payload->>'conversacion_id' = $1`, conv) +
		x.contar(`SELECT count(*) FROM mensajeria.voz_rafagas_levantadas WHERE numero = $1`, "+"+num); n != 0 {
		t.Fatalf("una saliente que Meta rechazó levantó la ráfaga: %d", n)
	}
	x.pasa(time.Second, x.agente)
	_, call := x.saliente(x.supervisor, conv, nil)
	x.webhookSaliente(num, call, "terminate", `,"status":"FAILED"`)
	if n := x.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'voz.rafaga_levantada' AND payload->>'origen' = 'llamada' AND payload->>'conversacion_id' = $1
		AND NOT (payload::text LIKE '%' || $2 || '%')`, conv, num); n != 1 {
		t.Fatalf("evento al llamar (sin el número): %d", n)
	}
	x.pasa(time.Second, x.agente)
	if _, estado, motivo := x.llamada(x.conectar(num, "wacid.llam4"+num)); estado != "sonando" {
		t.Fatalf("tras llamarlo debía sonar: %s %s", estado, motivo)
	}
	x.webhook(x.eventoLlamada(num, "wacid.llam4"+num, "terminate", "", 0))
	// Una saliente sin ráfaga encima no deja rastro.
	_, call2 := x.saliente(x.supervisor, conv, nil)
	x.webhookSaliente(num, call2, "terminate", `,"status":"FAILED"`)
	if n := x.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'voz.rafaga_levantada' AND payload->>'conversacion_id' = $1`, conv); n != 1 {
		t.Fatalf("una saliente sin ráfaga no debía emitir: %d", n)
	}
}

// conectar: connect y devuelve el call_id (para encadenar con llamada()).
func (x *bandejaVoz) conectar(num, call string) string {
	x.t.Helper()
	x.connect(num, call)
	return call
}
