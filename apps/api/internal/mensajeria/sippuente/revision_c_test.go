package sippuente

import (
	"fmt"
	"strings"
	"testing"

	"github.com/emiago/sipgo/sip"
)

// Revisión de #252, punto 5: con dos `Expires` se quitan todos y queda uno solo, acotado (la Issabel
// podría leer el segundo y dejar el registro un día entero).
func TestAcotarExpiraConVarias(t *testing.T) {
	casos := map[string]string{
		"Expires: 300\r\nExpires: 86400\r\n":  "300",
		"Expires: 86400\r\nExpires: 300\r\n":  "300",
		"Expires: 86400\r\nExpires: 9999\r\n": "600",
		"Expires: x\r\nExpires: -5\r\n":       "600",
		"Expires: 120\r\n":                    "120",
	}
	for cab, quiere := range casos {
		m, err := sip.ParseMessage([]byte("REGISTER sip:pbx SIP/2.0\r\nVia: SIP/2.0/WSS a.invalid;branch=z9hG4bKx\r\nFrom: <sip:201@pbx>;tag=a\r\n" +
			"To: <sip:201@pbx>\r\nCall-ID: c\r\nCSeq: 1 REGISTER\r\n" + cab + "Content-Length: 0\r\n\r\n"))
		if err != nil {
			t.Fatal(err)
		}
		req := m.(*sip.Request)
		acotarExpira(req)
		hs := req.GetHeaders("Expires")
		if len(hs) != 1 || hs[0].Value() != quiere {
			var vs []string
			for _, h := range hs {
				vs = append(vs, h.Value())
			}
			t.Errorf("%q: Expires %v, quería solo %s", cab, vs, quiere)
		}
	}
}

// Tercera revisión de #246: llamadas de la Issabel de punta a punta (sin regresiones por la lista de
// transacciones) y los casos 2 a 4.

func entrante(callID, rama string) []byte {
	return []byte("INVITE sip:201@x.invalid;transport=ws SIP/2.0\r\nVia: SIP/2.0/WSS 192.168.50.10:8089;branch=" + rama + "\r\n" +
		"Record-Route: <sip:192.168.50.10;lr>\r\nFrom: \"Juan\" <sip:+50255551234@pbx>;tag=pbxtag\r\nTo: <sip:201@pbx>\r\nCall-ID: " + callID + "\r\n" +
		"CSeq: 1 INVITE\r\nContact: <sip:asterisk@192.168.50.10:8089;transport=ws>\r\nSupported: replaces, timer\r\nk: 100rel\r\nContent-Length: 0\r\n\r\n")
}

func resp(codigo int, razon, rama, callID, metodo, totag string) []byte {
	return lineas(fmt.Sprintf("SIP/2.0 %d %s", codigo, razon), "Via: SIP/2.0/WSS 192.168.50.10:8089;branch="+rama,
		"From: <sip:+50255551234@pbx>;tag=pbxtag", "To: <sip:201@pbx>;tag="+totag, "Call-ID: "+callID, "CSeq: 1 "+metodo,
		"Contact: <sip:201@a.invalid;transport=ws>", "Content-Length: 0", "", "")
}

// Llamada entrante completa: 180, 200, ACK de la Issabel, BYE del navegador y su 200.
func TestEntranteCompletaDeLaIssabel(t *testing.T) {
	b := arrancarPrueba(t)
	b.cen.entrada <- entrante("e1", "z9hG4bKe1")
	inv := b.de(b.nav)
	t.Logf("INVITE al navegador:\n%s", inv)
	b.nav.entrada <- resp(180, "Ringing", "z9hG4bKe1", "e1", "INVITE", "nav1")
	if s := b.de(b.cen); primera(s) != "SIP/2.0 180 Ringing" {
		t.Fatal(s)
	}
	b.nav.entrada <- resp(200, "OK", "z9hG4bKe1", "e1", "INVITE", "nav1")
	if s := b.de(b.cen); primera(s) != "SIP/2.0 200 OK" {
		t.Fatal(s)
	}
	b.cen.entrada <- lineas("ACK sip:201@a.invalid;transport=ws SIP/2.0", "Via: SIP/2.0/WSS 192.168.50.10:8089;branch=z9hG4bKack1",
		"From: <sip:+50255551234@pbx>;tag=pbxtag", "To: <sip:201@pbx>;tag=nav1", "Call-ID: e1", "CSeq: 1 ACK", "Content-Length: 0", "", "")
	if s := b.de(b.nav); !strings.HasPrefix(s, "ACK ") {
		t.Fatal(s)
	}
	b.nav.entrada <- lineas("BYE sip:asterisk@192.168.50.10:8089;transport=ws SIP/2.0", "Via: SIP/2.0/WSS a.invalid;branch=z9hG4bKbye1",
		"From: <sip:201@pbx>;tag=nav1", "To: <sip:+50255551234@pbx>;tag=pbxtag", "Call-ID: e1", "CSeq: 2 BYE", "Route: <sip:192.168.50.10;lr>", "Content-Length: 0", "", "")
	bye := b.de(b.cen)
	if !strings.HasPrefix(bye, "BYE ") {
		t.Fatal(bye)
	}
	b.cen.entrada <- contestarA(t, bye, 200)
	if s := b.de(b.nav); primera(s) != "SIP/2.0 200 X" {
		t.Fatal(s)
	}
}

// CANCEL de la Issabel mientras suena: 200 al CANCEL, 487 al INVITE y el ACK.
func TestCancelDeLaIssabel(t *testing.T) {
	b := arrancarPrueba(t)
	b.cen.entrada <- entrante("e2", "z9hG4bKe2")
	b.de(b.nav)
	b.nav.entrada <- resp(180, "Ringing", "z9hG4bKe2", "e2", "INVITE", "nav2")
	b.de(b.cen)
	b.cen.entrada <- lineas("CANCEL sip:201@x.invalid;transport=ws SIP/2.0", "Via: SIP/2.0/WSS 192.168.50.10:8089;branch=z9hG4bKe2",
		"From: <sip:+50255551234@pbx>;tag=pbxtag", "To: <sip:201@pbx>", "Call-ID: e2", "CSeq: 1 CANCEL", "Content-Length: 0", "", "")
	if s := b.de(b.nav); !strings.HasPrefix(s, "CANCEL ") {
		t.Fatal(s)
	}
	b.nav.entrada <- resp(200, "OK", "z9hG4bKe2", "e2", "CANCEL", "nav2")
	if s := b.de(b.cen); primera(s) != "SIP/2.0 200 OK" {
		t.Fatal("200 CANCEL:", s)
	}
	b.nav.entrada <- resp(487, "Request Terminated", "z9hG4bKe2", "e2", "INVITE", "nav2")
	if s := b.de(b.cen); primera(s) != "SIP/2.0 487 Request Terminated" {
		t.Fatal("487:", s)
	}
	b.cen.entrada <- lineas("ACK sip:201@x.invalid;transport=ws SIP/2.0", "Via: SIP/2.0/WSS 192.168.50.10:8089;branch=z9hG4bKe2",
		"From: <sip:+50255551234@pbx>;tag=pbxtag", "To: <sip:201@pbx>;tag=nav2", "Call-ID: e2", "CSeq: 1 ACK", "Content-Length: 0", "", "")
	if s := b.de(b.nav); !strings.HasPrefix(s, "ACK ") {
		t.Fatal(s)
	}
}

// qualify de Asterisk: OPTIONS fuera de diálogo hacia el navegador y su 200 vuelve.
func TestQualifyDeLaIssabel(t *testing.T) {
	b := arrancarPrueba(t)
	b.cen.entrada <- lineas("OPTIONS sip:201@a.invalid;transport=ws SIP/2.0", "Via: SIP/2.0/WSS 192.168.50.10:8089;branch=z9hG4bKq1",
		"From: <sip:201@pbx>;tag=q", "To: <sip:201@a.invalid>", "Call-ID: q1", "CSeq: 1 OPTIONS", "Content-Length: 0", "", "")
	o := b.de(b.nav)
	b.nav.entrada <- contestarA(t, o, 200)
	if s := b.de(b.cen); primera(s) != "SIP/2.0 200 X" {
		t.Fatal(s)
	}
}

// 2: una respuesta con la branch de una transacción de la Issabel pero el Call-ID de otra llamada no pasa
// ni confirma la otra llamada.
func TestRespuestaConOtroCallIDNoPasa(t *testing.T) {
	b := arrancarPrueba(t)
	b.cen.entrada <- entrante("A", "z9hG4bKA")
	b.de(b.nav)
	b.cen.entrada <- entrante("B", "z9hG4bKB")
	b.de(b.nav)
	b.nav.entrada <- resp(200, "OK", "z9hG4bKA", "B", "INVITE", "elegida")
	b.nadaDe(b.cen)
	b.p.mu.Lock()
	d := b.p.dialogos["B"]
	conf, tag := d.confirmado, d.tagLocal
	b.p.mu.Unlock()
	if conf || tag != "" {
		t.Fatalf("la llamada B cambió: confirmado=%v tagLocal=%q", conf, tag)
	}
	// La buena sí.
	b.nav.entrada <- resp(200, "OK", "z9hG4bKB", "B", "INVITE", "navB")
	if s := b.de(b.cen); primera(s) != "SIP/2.0 200 OK" {
		t.Fatal(s)
	}
}

// 3: un INVITE inicial de la Issabel con el Call-ID de una llamada viva recibe 482 y no la toca.
func TestInviteConCallIDDeLlamadaViva(t *testing.T) {
	b := arrancarPrueba(t)
	b.cen.entrada <- entrante("v1", "z9hG4bKv1")
	b.de(b.nav)
	b.nav.entrada <- resp(200, "OK", "z9hG4bKv1", "v1", "INVITE", "navv1")
	b.de(b.cen)
	b.cen.entrada <- entrante("v1", "z9hG4bKv1otra")
	if s := b.de(b.cen); primera(s) != "SIP/2.0 482 Loop Detected" {
		t.Fatal(s)
	}
	b.nadaDe(b.nav)
	b.p.mu.Lock()
	d := b.p.dialogos["v1"]
	conf, tag := d.confirmado, d.tagLocal
	b.p.mu.Unlock()
	if !conf || tag != "navv1" {
		t.Fatalf("la llamada viva cambió: confirmado=%v tagLocal=%q", conf, tag)
	}
	// La saliente también: el Call-ID de un INVITE del navegador.
	b.nav.entrada <- lineas("INVITE sip:205@pbx SIP/2.0", "Via: SIP/2.0/WSS a.invalid;branch=z9hG4bKsal", "From: <sip:201@pbx>;tag=a", "To: <sip:205@pbx>",
		"Call-ID: sal1", "CSeq: 1 INVITE", "Contact: <sip:201@a.invalid;transport=ws>", "Content-Length: 0", "", "")
	b.de(b.cen)
	b.cen.entrada <- entrante("sal1", "z9hG4bKsal1")
	if s := b.de(b.cen); primera(s) != "SIP/2.0 482 Loop Detected" {
		t.Fatal(s)
	}
	b.nadaDe(b.nav)
}

// 4: si no cabe la transacción de un INVITE de la Issabel, no queda un diálogo ocupando lugar.
func TestSinLugarParaLaTransaccionNoDejaDialogo(t *testing.T) {
	b := arrancarPrueba(t)
	for i := 0; i < maxDeCentralita; i++ {
		b.cen.entrada <- lineas("OPTIONS sip:201@a.invalid;transport=ws SIP/2.0", fmt.Sprintf("Via: SIP/2.0/WSS 192.168.50.10:8089;branch=z9hG4bKq%d", i),
			"From: <sip:201@pbx>;tag=q", "To: <sip:201@a.invalid>", fmt.Sprintf("Call-ID: q%d", i), "CSeq: 1 OPTIONS", "Content-Length: 0", "", "")
		b.de(b.nav)
	}
	b.cen.entrada <- entrante("lleno", "z9hG4bKlleno")
	if s := b.de(b.cen); primera(s) != "SIP/2.0 503 Service Unavailable" {
		t.Fatal(s)
	}
	b.nadaDe(b.nav)
	b.p.mu.Lock()
	n := len(b.p.dialogos)
	b.p.mu.Unlock()
	if n != 0 {
		t.Fatalf("quedaron %d diálogos", n)
	}
}

// Fase 2: las formas compactas de la Issabel (k:, o:, u:, x:) valen lo mismo que su nombre largo en
// la lista blanca hacia el navegador; las que no están en la lista (s:, e:, a:) se quitan igual.
func TestFormasCompactasHaciaElNavegador(t *testing.T) {
	b := arrancarPrueba(t)
	b.cen.entrada <- []byte("INVITE sip:201@x.invalid;transport=ws SIP/2.0\r\nv: SIP/2.0/WSS 192.168.50.10:8089;branch=z9hG4bKk1\r\n" +
		"f: <sip:+50255551234@pbx>;tag=pbxtag\r\nt: <sip:201@pbx>\r\ni: k1\r\nCSeq: 1 INVITE\r\nm: <sip:asterisk@192.168.50.10:8089;transport=ws>\r\n" +
		"k: 100rel, timer\r\no: talk\r\nu: talk, hold\r\nx: 1800;refresher=uac\r\ns: urgente\r\ne: gzip\r\na: *;audio\r\nl: 0\r\n\r\n")
	s := b.de(b.nav)
	for _, n := range []string{"k", "o", "u", "x"} {
		if !tieneCabecera(s, n) {
			t.Fatalf("se perdió %s:\n%s", n, s)
		}
	}
	for _, n := range []string{"s", "e", "a"} {
		if tieneCabecera(s, n) {
			t.Fatalf("pasó %s:\n%s", n, s)
		}
	}
	r := peticion(t, s)
	if r.CallID() == nil || string(*r.CallID()) != "k1" || r.From() == nil || r.Contact() == nil {
		t.Fatalf("cabeceras básicas:\n%s", s)
	}
}

// Y del navegador: «r:» (Refer-To) fuera de un REFER no llega a la Issabel.
func TestReferToCompactoFueraDeRefer(t *testing.T) {
	b := arrancarPrueba(t)
	b.nav.entrada <- lineas("OPTIONS sip:pbx SIP/2.0", "Via: SIP/2.0/WSS a.invalid;branch=z9hG4bKor", "From: <sip:201@pbx>;tag=a",
		"To: <sip:pbx>", "Call-ID: or1", "CSeq: 1 OPTIONS", "r: <sip:0019005551234@pbx>", "k: replaces", "Content-Length: 0", "", "")
	s := b.de(b.cen)
	if tieneCabecera(s, "r") || tieneCabecera(s, "Refer-To") || strings.Contains(s, "0019005551234") {
		t.Fatalf("pasó Refer-To:\n%s", s)
	}
	if !tieneCabecera(s, "k") {
		t.Fatalf("se perdió Supported compacto:\n%s", s)
	}
}
