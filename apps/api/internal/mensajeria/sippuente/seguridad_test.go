package sippuente_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"

	"github.com/HCDestroyer/iris/apps/api/internal/mensajeria/sippuente"
)

// Hallazgos de la revisión de seguridad de E4c (#246): inyección de cabeceras, desvíos, DTMF,
// lista blanca, etiquetas y SDP.

func opciones(cabeceras, cuerpo string) string {
	return "OPTIONS sip:" + dominio + " SIP/2.0\r\nVia: SIP/2.0/WSS abc.invalid;branch=z9hG4bKopt\r\nMax-Forwards: 69\r\n" +
		"From: <sip:201@" + dominio + ">;tag=o1\r\nTo: <sip:" + dominio + ">\r\nCall-ID: opt-1\r\nCSeq: 1 OPTIONS\r\n" + cabeceras +
		fmt.Sprintf("Content-Length: %d\r\n\r\n", len(cuerpo)) + cuerpo
}

func TestValidarCrudoCasosDelAnalizador(t *testing.T) {
	bien := opciones("User-Agent: JsSIP 3.10\r\n", "")
	if err := sippuente.ValidarCrudo([]byte(bien)); err != nil {
		t.Fatalf("rechazó un mensaje bueno: %v", err)
	}
	malos := map[string]string{
		"LF suelto en un valor":       opciones("User-Agent: x\nRefer-To: <sip:0019005551234@pbx>\r\n", ""),
		"LF suelto en el display":     strings.Replace(bien, `From: <sip:201@`, "From: \"a\nP-Asserted-Identity: <sip:+15551234@pbx>\" <sip:201@", 1),
		"CR suelto en un valor":       opciones("User-Agent: x\rRefer-To: <sip:0019005551234@pbx>\r\n", ""),
		"NUL en una cabecera":         opciones("User-Agent: x\x00y\r\n", ""),
		"control en una cabecera":     opciones("User-Agent: x\x1by\r\n", ""),
		"cabecera plegada":            opciones("User-Agent: x\r\n Refer-To: <sip:1@pbx>\r\n", ""),
		"línea sin dos puntos":        opciones("Refer-To <sip:1@pbx>\r\n", ""),
		"dos Content-Length":          opciones("Content-Length: 0\r\n", ""),
		"cuerpo sin Content-Length":   strings.Replace(opciones("", ""), "Content-Length: 0\r\n\r\n", "\r\n", 1) + "v=0\r\n",
		"segundo mensaje en el frame": opciones("", "") + "REFER sip:201@pbx SIP/2.0\r\nRefer-To: <sip:0019005551234@pbx>\r\n\r\n",
		"LF suelto en el cuerpo":      opciones("Content-Type: application/sdp\r\n", "v=0\na=candidate:1 1 udp 1 1.2.3.4 9 typ host\r\n"),
		"CR suelto en el cuerpo":      opciones("Content-Type: application/sdp\r\n", "v=0\ra=x\r\n"),
		"sin fin de cabeceras":        "OPTIONS sip:pbx SIP/2.0\r\nVia: x\r\n",
	}
	for nombre, m := range malos {
		if err := sippuente.ValidarCrudo([]byte(m)); err == nil {
			t.Errorf("%s: aceptado", nombre)
		}
	}
}

func TestInyeccionDeCabecerasNoLlega(t *testing.T) {
	b := nuevoBanco(t, nil, secretoSIP)
	for _, m := range []string{
		opciones("User-Agent: x\nRefer-To: <sip:0019005551234@pbx>\r\n", ""),
		strings.Replace(opciones("", ""), "From: <sip:201@", "From: \"a\nP-Asserted-Identity: <sip:+15551234@pbx>\" <sip:201@", 1),
		opciones("User-Agent: x\rContact: <sip:201@203.0.113.9:5060>\r\n", ""),
		opciones("", "") + "REFER sip:201@pbx SIP/2.0\r\nRefer-To: <sip:0019005551234@pbx>\r\n\r\n",
	} {
		b.nav.entrada <- []byte(m)
	}
	b.nada()
	if n := len(b.falso.Recibidos()); n != 0 {
		t.Fatalf("llegó a la centralita: %v", b.falso.Recibidos())
	}
	// Las cabeceras fuera de la lista blanca se quitan del mensaje que sale.
	b.nav.entrada <- []byte(opciones("P-Asserted-Identity: <sip:+15551234@pbx>\r\nRemote-Party-ID: <sip:+15551234@pbx>;party=calling\r\n"+
		"Diversion: <sip:+15551234@pbx>\r\nHistory-Info: <sip:+15551234@pbx>;index=1\r\nPrivacy: id\r\nReplaces: a;to-tag=b;from-tag=c\r\n"+
		"Join: a;to-tag=b;from-tag=c\r\nReferred-By: <sip:1@pbx>\r\nRoute: <sip:evil.example;lr>\r\nX-Algo: 1\r\nUser-Agent: JsSIP\r\n", ""))
	b.respuesta(200)
	opts := b.recibidosCon(empieza("OPTIONS"))
	if len(opts) != 1 {
		t.Fatalf("OPTIONS: %v", opts)
	}
	for _, c := range []string{"P-Asserted", "Remote-Party", "Diversion", "History-Info", "Privacy", "Replaces", "Join", "Referred-By", "Route", "X-Algo"} {
		if strings.Contains(opts[0], c) {
			t.Fatalf("%s llegó a la centralita:\n%s", c, opts[0])
		}
	}
	if !strings.Contains(opts[0], "User-Agent: JsSIP") {
		t.Fatalf("quitó una cabecera permitida:\n%s", opts[0])
	}
}

func TestDesvioDelNavegadorEs480(t *testing.T) {
	b := nuevoBanco(t, nil, secretoSIP)
	for i, codigo := range []int{302, 380, 485} {
		b.falso.Llamar("50255550000", dominio)
		m := b.recibir()
		inv, ok := m.m.(*sip.Request)
		if !ok || !inv.IsInvite() {
			t.Fatalf("esperaba el INVITE: %s", m.crudo)
		}
		r := sip.NewResponseFromRequest(inv, codigo, "Moved", nil)
		r.AppendHeader(sip.NewHeader("Contact", "<sip:201@x.invalid;transport=ws>"))
		b.nav.entrada <- []byte(r.String())
		for fin := time.Now().Add(2 * time.Second); len(b.recibidosCon(empieza("SIP/2.0 480"))) < i+1 && time.Now().Before(fin); {
			time.Sleep(10 * time.Millisecond)
		}
		if rs := b.recibidosCon(empieza("SIP/2.0 480")); len(rs) != i+1 || strings.Contains(rs[i], "Contact") {
			t.Fatalf("%d: %v", codigo, b.falso.Recibidos())
		}
		b.recibir() // el ACK
	}
	if n := len(b.recibidosCon(empieza("SIP/2.0 3"))); n != 0 {
		t.Fatalf("llegó un desvío: %v", b.falso.Recibidos())
	}
	// Un Contact con otro usuario (un número a la troncal) tampoco: 488.
	b.falso.Llamar("50255550000", dominio)
	inv := b.recibir().m.(*sip.Request)
	r := sip.NewResponseFromRequest(inv, 200, "OK", nil)
	r.AppendHeader(sip.NewHeader("Contact", "<sip:0019005551234@x.invalid;transport=ws>"))
	b.nav.entrada <- []byte(r.String())
	if _, ok := b.falso.Esperar(2*time.Second, empieza("SIP/2.0 488")); !ok {
		t.Fatalf("Contact ajeno: %v", b.falso.Recibidos())
	}
}

func info(rama, totag, tipo, cuerpo string, seq int) string {
	return fmt.Sprintf("INFO sip:asterisk@127.0.0.1;transport=ws SIP/2.0\r\nVia: SIP/2.0/WSS abc.invalid;branch=z9hG4bK%s\r\nMax-Forwards: 69\r\n"+
		"To: <sip:205@%s>;tag=%s\r\nFrom: <sip:201@%s>;tag=nav-k1\r\nCall-ID: call-k1\r\nCSeq: %d INFO\r\nContent-Type: %s\r\nContent-Length: %d\r\n\r\n%s",
		rama, dominio, totag, dominio, seq, tipo, len(cuerpo), cuerpo)
}

func TestDTMFPorINFOYDialogoAjeno(t *testing.T) {
	b := nuevoBanco(t, nil, secretoSIP)
	b.nav.entrada <- []byte(strings.ReplaceAll(strings.Replace(strings.Replace(invite("205", "k1", 3), "nav2", "nav-k1", 1), "inv-call-1", "call-k1", 1), "\n", "\r\n")[2:])
	ok := b.respuesta(200)
	totag, _ := ok.To().Params.Get("tag")
	relay := "application/dtmf-relay"
	b.nav.entrada <- []byte(info("d1", totag, relay, "Signal=5\r\nDuration=160\r\n", 4))
	b.respuesta(200)
	// ## (transferir en Asterisk) y *2 (transferir con consulta) no pasan.
	b.nav.entrada <- []byte(info("d2", totag, relay, "Signal=#\r\nDuration=160\r\n", 5))
	b.respuesta(200)
	b.nav.entrada <- []byte(info("d3", totag, relay, "Signal=#\r\nDuration=160\r\n", 6))
	b.respuesta(403)
	b.nav.entrada <- []byte(info("d4", totag, relay, "Signal=*\r\nDuration=160\r\n", 7))
	b.respuesta(200)
	b.nav.entrada <- []byte(info("d5", totag, relay, "Signal=2\r\nDuration=160\r\n", 8))
	b.respuesta(403)
	// Varios dígitos, letras u otro tipo: 415.
	for i, c := range []string{"Signal=12\r\n", "Signal=A\r\n", "Signal=1\r\nX=1\r\n"} {
		b.nav.entrada <- []byte(info(fmt.Sprint("d6", i), totag, relay, c, 9+i))
		b.respuesta(415)
	}
	b.nav.entrada <- []byte(info("d9", totag, "text/plain", "##", 20))
	b.respuesta(415)
	infos := b.recibidosCon(empieza("INFO"))
	if len(infos) != 3 {
		t.Fatalf("INFO en la centralita: %d %v", len(infos), infos)
	}
	// Dentro del diálogo, otro From o la etiqueta de otro: 403 o 481.
	bye := func(rama, from, ftag string) string {
		return fmt.Sprintf("BYE sip:asterisk@127.0.0.1;transport=ws SIP/2.0\r\nVia: SIP/2.0/WSS abc.invalid;branch=z9hG4bK%s\r\nMax-Forwards: 69\r\n"+
			"To: <sip:205@%s>;tag=%s\r\nFrom: <sip:%s@%s>;tag=%s\r\nCall-ID: call-k1\r\nCSeq: 30 BYE\r\nContent-Length: 0\r\n\r\n", rama, dominio, totag, from, dominio, ftag)
	}
	b.nav.entrada <- []byte(bye("b1", "202", "nav-k1"))
	b.respuesta(403)
	b.nav.entrada <- []byte(bye("b2", "201", "otra"))
	b.respuesta(481)
	// Un OPTIONS no abre un «diálogo» donde colar un BYE.
	b.nav.entrada <- []byte(opciones("", ""))
	b.respuesta(200)
	b.nav.entrada <- []byte(strings.Replace(strings.Replace(bye("b3", "201", "o1"), "call-k1", "opt-1", 1), "tag="+totag, "tag=zz", 1))
	b.respuesta(481)
	if n := len(b.recibidosCon(empieza("BYE"))); n != 0 {
		t.Fatalf("un BYE ajeno llegó: %v", b.recibidosCon(empieza("BYE")))
	}
}

func TestSDPAtributosFueraDeLaLista(t *testing.T) {
	b := nuevoBanco(t, func(o *sippuente.Opciones) { o.Relevo = "10.20.0.9" }, secretoSIP)
	extra := candidatoRelevo + "a=rtcp:9 IN IP4 192.168.1.50\na=remote-candidates:1 1.2.3.4 9\nk=clear:secreto\na=rtcp:49171 IN IP4 10.20.0.9\n"
	b.mandar(inviteSDP("r1", fmt.Sprintf(sdpBase, "10.20.0.9", extra)))
	b.respuesta(200)
	invs := b.recibidosCon(func(m string) bool { return strings.HasPrefix(m, "INVITE") && strings.Contains(m, "Authorization") })
	if len(invs) != 1 {
		t.Fatalf("INVITE: %v", invs)
	}
	for _, malo := range []string{"192.168.1.50", "remote-candidates", "k=clear"} {
		if strings.Contains(invs[0], malo) {
			t.Fatalf("%s llegó a la centralita:\n%s", malo, invs[0])
		}
	}
	if !strings.Contains(invs[0], "a=rtcp:49171 IN IP4 10.20.0.9") {
		t.Fatalf("quitó el a=rtcp del relevo:\n%s", invs[0])
	}
}
