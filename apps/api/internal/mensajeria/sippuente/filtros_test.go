package sippuente_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"

	"github.com/HCDestroyer/iris/apps/api/internal/mensajeria/sippuente"
)

func inviteSDP(rama, sdp string) string {
	n := len(strings.ReplaceAll(sdp, "\n", "\r\n"))
	return fmt.Sprintf("\nINVITE sip:205@%[1]s SIP/2.0\nVia: SIP/2.0/WSS abc.invalid;branch=z9hG4bK%[2]s\nMax-Forwards: 69\nTo: <sip:205@%[1]s>\n"+
		"From: <sip:201@%[1]s>;tag=nav-%[2]s\nCall-ID: sdp-%[2]s\nCSeq: 1 INVITE\nContact: <sip:201@abc.invalid;transport=ws>\nContent-Type: application/sdp\n"+
		"Content-Length: %[3]d\n\n", dominio, rama, n) + sdp
}

const sdpBase = `v=0
o=- 1 1 IN IP4 203.0.113.9
s=-
c=IN IP4 %s
t=0 0
m=audio 50000 UDP/TLS/RTP/SAVPF 111
a=candidate:1 1 udp 2122260223 192.168.1.20 50000 typ host
a=candidate:2 1 udp 1686052607 198.51.100.7 50000 typ srflx raddr 192.168.1.20 rport 50000
%s`

const candidatoRelevo = "a=candidate:3 1 udp 41885439 10.20.0.9 49170 typ relay raddr 0.0.0.0 rport 0\n"

func TestContactYSDPDelNavegador(t *testing.T) {
	b := nuevoBanco(t, func(o *sippuente.Opciones) { o.Relevo = "10.20.0.9" }, secretoSIP)
	// Un Contact hacia internet: la Issabel le mandaría SIP ahí. No.
	b.mandar(strings.Replace(register(1, "ct1", ""), "<sip:201@abc.invalid;transport=ws>", "<sip:201@203.0.113.9:5060;transport=udp>", 1))
	b.respuesta(403)
	// Solo candidatos host y srflx (audio directo, sin el relevo): 488.
	b.mandar(inviteSDP("s1", fmt.Sprintf(sdpBase, "10.20.0.9", "")))
	b.respuesta(488)
	// c= ajeno al relevo: 488.
	b.mandar(inviteSDP("s2", fmt.Sprintf(sdpBase, "203.0.113.9", candidatoRelevo)))
	b.respuesta(488)
	// Con el relevo: pasa, y sin los candidatos directos.
	b.mandar(inviteSDP("s3", fmt.Sprintf(sdpBase, "10.20.0.9", candidatoRelevo)))
	r := b.respuesta(200)
	invs := b.recibidosCon(func(m string) bool { return strings.HasPrefix(m, "INVITE") && strings.Contains(m, "Authorization") })
	if len(invs) != 1 || strings.Contains(invs[0], "typ host") || strings.Contains(invs[0], "typ srflx") || !strings.Contains(invs[0], "typ relay") {
		t.Fatalf("SDP en la centralita: %v", invs)
	}
	// Otro diálogo colado con el mismo Call-ID y otra etiqueta: 481.
	totag, _ := r.To().Params.Get("tag")
	bye := func(rama, tag string) string {
		return fmt.Sprintf("\nBYE sip:asterisk@127.0.0.1;transport=ws SIP/2.0\nVia: SIP/2.0/WSS abc.invalid;branch=z9hG4bK%s\nMax-Forwards: 69\nTo: <sip:x@%s>;tag=%s\n"+
			"From: <sip:201@%s>;tag=nav-s3\nCall-ID: sdp-s3\nCSeq: 2 BYE\nContent-Length: 0\n\n", rama, dominio, tag, dominio)
	}
	b.mandar(bye("bx", "otra"))
	b.respuesta(481)
	b.mandar(bye("by", totag))
	b.respuesta(200)
	// La respuesta del navegador a una entrante con audio directo: la Issabel recibe 488.
	b.falso.Llamar("50255550000", dominio)
	m := b.recibir()
	inv, ok := m.m.(*sip.Request)
	if !ok {
		t.Fatalf("esperaba el INVITE: %s", m.crudo)
	}
	sdp := strings.ReplaceAll(fmt.Sprintf(sdpBase, "10.20.0.9", ""), "\n", "\r\n")
	res := sip.NewResponseFromRequest(inv, 200, "OK", []byte(sdp))
	res.AppendHeader(sip.NewHeader("Contact", "<sip:201@abc.invalid;transport=ws>"))
	res.AppendHeader(sip.NewHeader("Content-Type", "application/sdp"))
	b.nav.entrada <- []byte(res.String())
	if _, ok := b.falso.Esperar(2*time.Second, empieza("SIP/2.0 488")); !ok {
		t.Fatalf("la 2xx con audio directo llegó: %v", b.falso.Recibidos())
	}
}
