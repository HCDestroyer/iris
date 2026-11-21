package sippuente_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/google/uuid"

	"github.com/HCDestroyer/iris/apps/api/internal/mensajeria/sippuente"
	"github.com/HCDestroyer/iris/apps/api/internal/mensajeria/sippuente/sipfalso"
)

const (
	usuarioSIP = "201"
	secretoSIP = "clave-SIP-larga-201"
	dominio    = "pbx.oficina"
)

func TestDigestRFC2617(t *testing.T) {
	// Ejemplo de RFC 2617 §3.5.
	d, err := sippuente.LeerDesafio(`Digest realm="testrealm@host.com", qop="auth,auth-int", nonce="dcd98b7102dd2f0e8b11d0f600bfb0c093", opaque="5ccc069c403ebaf9f0171e9517f40e41"`)
	if err != nil {
		t.Fatal(err)
	}
	v := sippuente.ResponderCon(d, "Mufasa", "Circle Of Life", "GET", "/dir/index.html", 1, "0a4f113b")
	if !strings.Contains(v, `response="6629fae49393a05397450978507c4ef1"`) || !strings.Contains(v, "nc=00000001") || !strings.Contains(v, `opaque="5ccc069c403ebaf9f0171e9517f40e41"`) {
		t.Fatalf("digest: %s", v)
	}
	if !sippuente.VerificarDigest(v, "Circle Of Life", "GET", "dcd98b7102dd2f0e8b11d0f600bfb0c093") {
		t.Fatal("no verifica su propia respuesta")
	}
	if sippuente.VerificarDigest(v, "otra", "GET", "dcd98b7102dd2f0e8b11d0f600bfb0c093") {
		t.Fatal("verifica con otra contraseña")
	}
	for _, malo := range []string{`Basic realm="x"`, `Digest realm="x"`, `Digest realm="x",nonce="n",algorithm=SHA-512`, `Digest realm="x",nonce="n",qop="auth-int"`, `Digest realm="a\"b",nonce="n"`} {
		if _, err := sippuente.LeerDesafio(malo); err == nil {
			t.Fatalf("aceptó %q", malo)
		}
	}
}

// ladoMem es el navegador de las pruebas: lo que la prueba escribe entra al puente y lo que el
// puente escribe sale por salida.
type ladoMem struct {
	entrada chan []byte
	salida  chan string
	cerrado chan struct{}
	una     sync.Once
	mu      sync.Mutex
	codigo  int
	motivo  string
}

func nuevoLadoMem() *ladoMem {
	return &ladoMem{entrada: make(chan []byte, 64), salida: make(chan string, 256), cerrado: make(chan struct{})}
}

func (l *ladoMem) Leer(ctx context.Context) ([]byte, error) {
	select {
	case b := <-l.entrada:
		return b, nil
	case <-l.cerrado:
		return nil, errors.New("cerrado")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (l *ladoMem) Escribir(_ context.Context, b []byte) error {
	select {
	case <-l.cerrado:
		return errors.New("cerrado")
	default:
	}
	l.salida <- string(b)
	return nil
}

func (l *ladoMem) Cerrar(codigo int, motivo string) {
	l.una.Do(func() {
		l.mu.Lock()
		l.codigo, l.motivo = codigo, motivo
		l.mu.Unlock()
		close(l.cerrado)
	})
}

// politicaFalsa: solo deja marcar iris-<id> (externa) o 205 (interna) y transferir a 205 o 600.
type politicaFalsa struct {
	mu      sync.Mutex
	llamada uuid.UUID
	hechos  []sippuente.HechoLlamada
	usados  map[string]bool
}

func (p *politicaFalsa) Invitar(_ context.Context, u string) (sippuente.Destino, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if u == "iris-"+p.llamada.String() && !p.usados[u] {
		p.usados[u] = true
		id := p.llamada
		return sippuente.Destino{Usuario: "55551234", LlamadaID: &id}, nil
	}
	if u == "205" {
		return sippuente.Destino{Usuario: "205"}, nil
	}
	return sippuente.Destino{}, errors.New("no")
}

func (p *politicaFalsa) Transferir(_ context.Context, u string, _ *uuid.UUID) (string, error) {
	if u == "205" || u == "600" {
		return u, nil
	}
	return "", errors.New("no")
}

func (p *politicaFalsa) Entrante(context.Context, string) *uuid.UUID {
	id := p.llamada
	return &id
}

func (p *politicaFalsa) Hecho(_ context.Context, h sippuente.HechoLlamada) {
	p.mu.Lock()
	p.hechos = append(p.hechos, h)
	p.mu.Unlock()
}

func (p *politicaFalsa) tipos() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []string
	for _, h := range p.hechos {
		s := h.Tipo
		if h.Codigo != 0 {
			s += fmt.Sprint(":", h.Codigo)
		}
		if h.Cancelada {
			s += ":cancelada"
		}
		out = append(out, s)
	}
	return out
}

type banco struct {
	t      *testing.T
	falso  *sipfalso.Servidor
	nav    *ladoMem
	pol    *politicaFalsa
	puente *sippuente.Puente
	hecho  chan error
}

func nuevoBanco(t *testing.T, ajustar func(*sippuente.Opciones), secreto string) *banco {
	t.Helper()
	f := sipfalso.Nuevo(usuarioSIP, secretoSIP)
	t.Cleanup(f.Cerrar)
	c, _, err := sippuente.Conectar(context.Background(), sippuente.OpcionesWSS{Host: f.Host(), Puerto: f.Puerto(), Ruta: "/ws", Huella: f.Huella(), Plazo: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	// El falso anota la conexión después del upgrade: Llamar necesita verla.
	for fin := time.Now().Add(2 * time.Second); f.Conectados() == 0 && time.Now().Before(fin); {
		time.Sleep(5 * time.Millisecond)
	}
	b := &banco{t: t, falso: f, nav: nuevoLadoMem(), pol: &politicaFalsa{llamada: uuid.New(), usados: map[string]bool{}}, hecho: make(chan error, 1)}
	o := sippuente.Opciones{Credencial: sippuente.Credencial{Usuario: usuarioSIP, Secreto: secreto, Dominio: dominio}, Registrar: true, Politica: b.pol}
	if ajustar != nil {
		ajustar(&o)
	}
	b.puente = sippuente.Nuevo(o, b.nav, sippuente.NuevoLadoWS(c, sippuente.MaxCentralita))
	go func() { b.hecho <- b.puente.Correr(context.Background()) }()
	t.Cleanup(func() {
		b.puente.Cortar(sippuente.CierreNormal, "")
		select {
		case <-b.hecho:
		case <-time.After(5 * time.Second):
			t.Error("el puente no terminó")
		}
	})
	return b
}

func (b *banco) mandar(s string) {
	b.nav.entrada <- []byte(strings.ReplaceAll(strings.TrimLeft(s, "\n"), "\n", "\r\n"))
}

// recibir espera el siguiente mensaje del puente al navegador.
func (b *banco) recibir() *sipMsg {
	b.t.Helper()
	select {
	case s := <-b.nav.salida:
		m, err := sip.ParseMessage([]byte(s))
		if err != nil {
			b.t.Fatalf("el navegador recibió algo que no es SIP: %v\n%s", err, s)
		}
		return &sipMsg{m: m, crudo: s}
	case <-time.After(3 * time.Second):
		b.t.Fatal("el navegador no recibió nada")
	}
	return nil
}

// respuestaFinal salta las provisionales.
func (b *banco) respuesta(codigo int) *sip.Response {
	b.t.Helper()
	for {
		m := b.recibir()
		r, ok := m.m.(*sip.Response)
		if !ok {
			b.t.Fatalf("esperaba una respuesta %d, llegó una petición:\n%s", codigo, m.crudo)
		}
		if r.StatusCode == sip.StatusUnauthorized || r.StatusCode == sip.StatusProxyAuthRequired || strings.Contains(strings.ToLower(m.crudo), "authenticate") {
			b.t.Fatalf("un desafío llegó al navegador:\n%s", m.crudo)
		}
		if r.StatusCode == codigo {
			return r
		}
		if r.StatusCode >= 200 {
			b.t.Fatalf("esperaba %d, llegó:\n%s", codigo, m.crudo)
		}
	}
}

type sipMsg struct {
	m     sip.Message
	crudo string
}

func (b *banco) nada() {
	b.t.Helper()
	select {
	case s := <-b.nav.salida:
		b.t.Fatalf("no esperaba nada y llegó:\n%s", s)
	case <-time.After(150 * time.Millisecond):
	}
}

func register(seq int, rama, extra string) string {
	return fmt.Sprintf(`
REGISTER sip:%[1]s SIP/2.0
Via: SIP/2.0/WSS abc.invalid;branch=z9hG4bK%[2]s
Max-Forwards: 69
To: <sip:%[3]s@%[1]s>
From: "Ana" <sip:%[3]s@%[1]s>;tag=nav1
Call-ID: reg-call-1
CSeq: %[4]d REGISTER
Contact: <sip:x7@abc.invalid;transport=ws>;expires=600
Expires: 86400
%[5]sContent-Length: 0

`, dominio, rama, usuarioSIP, seq, extra)
}

func invite(uri, rama string, seq int) string {
	return fmt.Sprintf(`
INVITE sip:%[1]s@%[2]s SIP/2.0
Via: SIP/2.0/WSS abc.invalid;branch=z9hG4bK%[3]s
Max-Forwards: 69
To: <sip:%[1]s@%[2]s>
From: "Ana" <sip:%[4]s@%[2]s>;tag=nav2
Call-ID: inv-call-1
CSeq: %[5]d INVITE
Contact: <sip:x7@abc.invalid;transport=ws;ob>
Content-Type: application/sdp
Content-Length: 4

v=0
`, uri, dominio, rama, usuarioSIP, seq)
}

func (b *banco) recibidosCon(f func(string) bool) []string {
	var out []string
	for _, m := range b.falso.Recibidos() {
		if f(m) {
			out = append(out, m)
		}
	}
	return out
}

func empieza(p string) func(string) bool { return func(m string) bool { return strings.HasPrefix(m, p) } }

func TestRegistroSinDesafioParaElNavegador(t *testing.T) {
	b := nuevoBanco(t, nil, secretoSIP)
	// El navegador intenta poner su propio Authorization (en minúsculas): se quita.
	b.mandar(register(1, "r1", "authorization: Digest username=\"201\",realm=\"x\",nonce=\"y\",uri=\"sip:x\",response=\"0\"\n"))
	r := b.respuesta(200)
	if r.CSeq().SeqNo != 1 || r.Via().Params.GetOr("branch", "") != "z9hG4bKr1" {
		t.Fatalf("la respuesta no volvió con la CSeq y la branch del navegador: %s", r)
	}
	regs := b.recibidosCon(empieza("REGISTER"))
	if len(regs) != 2 {
		t.Fatalf("REGISTER en la centralita: %d", len(regs))
	}
	if strings.Contains(strings.ToLower(regs[0]), "authorization") || strings.Contains(regs[0], `response="0"`) {
		t.Fatalf("el Authorization del navegador llegó a la centralita:\n%s", regs[0])
	}
	if !strings.Contains(regs[1], "Authorization: Digest") || !strings.Contains(regs[1], "CSeq: 2 REGISTER") || !strings.Contains(regs[1], "Expires: 600") {
		t.Fatalf("el reintento con credenciales:\n%s", regs[1])
	}
	// Re-REGISTER: el navegador sigue con su CSeq; la centralita ve la suya, siempre creciente.
	b.mandar(register(2, "r2", ""))
	r = b.respuesta(200)
	if r.CSeq().SeqNo != 2 {
		t.Fatalf("CSeq al navegador: %d", r.CSeq().SeqNo)
	}
	regs = b.recibidosCon(empieza("REGISTER"))
	if len(regs) != 4 || !strings.Contains(regs[2], "CSeq: 3 REGISTER") || !strings.Contains(regs[3], "CSeq: 4 REGISTER") {
		t.Fatalf("CSeq en la centralita: %v", regs)
	}
	for _, m := range b.falso.Recibidos() {
		if strings.Contains(m, secretoSIP) {
			t.Fatal("la contraseña viajó en claro")
		}
	}
}

func TestCredencialRechazadaEs403(t *testing.T) {
	b := nuevoBanco(t, nil, "otra-clave-mala")
	b.mandar(register(1, "m1", ""))
	b.respuesta(403)
}

func TestRechazosLocales(t *testing.T) {
	b := nuevoBanco(t, nil, secretoSIP)
	// From ajeno.
	b.mandar(strings.Replace(register(1, "a1", ""), `From: "Ana" <sip:201@`, `From: "Ana" <sip:202@`, 1))
	b.respuesta(403)
	// INVITE a un número libre o internacional: no llega a la centralita.
	for i, uri := range []string{"+15551234567", "50255551234", "900123", "iris-" + uuid.NewString()} {
		b.mandar(invite(uri, fmt.Sprint("x", i), 1))
		b.respuesta(403)
		b.mandar(fmt.Sprintf("\nACK sip:%s@%s SIP/2.0\nVia: SIP/2.0/WSS abc.invalid;branch=z9hG4bKx%d\nMax-Forwards: 69\nTo: <sip:%s@%s>;tag=t\nFrom: <sip:201@%s>;tag=nav2\nCall-ID: inv-call-1\nCSeq: 1 ACK\nContent-Length: 0\n\n",
			uri, dominio, i, uri, dominio, dominio))
	}
	// MESSAGE y SUBSCRIBE: 405.
	for _, m := range []string{"MESSAGE", "SUBSCRIBE", "PUBLISH", "NOTIFY"} {
		b.mandar(fmt.Sprintf("\n%s sip:205@%s SIP/2.0\nVia: SIP/2.0/WSS abc.invalid;branch=z9hG4bKm%s\nMax-Forwards: 69\nTo: <sip:205@%s>\nFrom: <sip:201@%s>;tag=a\nCall-ID: msg-%s\nCSeq: 1 %s\nContent-Length: 0\n\n",
			m, dominio, m, dominio, dominio, m, m))
		b.respuesta(405)
	}
	// BYE fuera de un diálogo: 481.
	b.mandar(fmt.Sprintf("\nBYE sip:x@%s SIP/2.0\nVia: SIP/2.0/WSS abc.invalid;branch=z9hG4bKbye\nMax-Forwards: 69\nTo: <sip:205@%s>;tag=zz\nFrom: <sip:201@%s>;tag=a\nCall-ID: nada\nCSeq: 5 BYE\nContent-Length: 0\n\n", dominio, dominio, dominio))
	b.respuesta(481)
	time.Sleep(100 * time.Millisecond)
	if n := len(b.recibidosCon(func(m string) bool { return !strings.HasPrefix(m, "SIP/2.0") })); n != 0 {
		t.Fatalf("llegaron %d peticiones a la centralita: %v", n, b.falso.Recibidos())
	}
}

func TestSalienteConDesafioACKYBye(t *testing.T) {
	b := nuevoBanco(t, nil, secretoSIP)
	b.mandar(invite("iris-"+b.pol.llamada.String(), "i1", 10))
	r := b.respuesta(200)
	if r.CSeq().SeqNo != 10 || r.Via().Params.GetOr("branch", "") != "z9hG4bKi1" {
		t.Fatalf("200 al navegador: %s", r)
	}
	invs := b.recibidosCon(empieza("INVITE"))
	if len(invs) != 2 || !strings.HasPrefix(invs[1], "INVITE sip:55551234@"+dominio+" SIP/2.0") || !strings.Contains(invs[1], "Authorization: Digest") ||
		!strings.Contains(invs[1], "CSeq: 11 INVITE") {
		t.Fatalf("INVITE en la centralita: %v", invs)
	}
	// Iris acusó el 401 (el navegador nunca lo vio).
	if acks := b.recibidosCon(func(m string) bool { return strings.HasPrefix(m, "ACK") && strings.Contains(m, "CSeq: 10 ACK") }); len(acks) != 1 {
		t.Fatalf("ACK del 401: %v", b.falso.Recibidos())
	}
	totag, _ := r.To().Params.Get("tag")
	// ACK de la 2xx del navegador: CSeq corregida.
	b.mandar(fmt.Sprintf("\nACK sip:asterisk@127.0.0.1;transport=ws SIP/2.0\nVia: SIP/2.0/WSS abc.invalid;branch=z9hG4bKack1\nMax-Forwards: 69\nTo: <sip:x@%s>;tag=%s\nFrom: <sip:201@%s>;tag=nav2\nCall-ID: inv-call-1\nCSeq: 10 ACK\nContent-Length: 0\n\n",
		dominio, totag, dominio))
	if _, ok := b.falso.Esperar(2*time.Second, func(m string) bool { return strings.HasPrefix(m, "ACK") && strings.Contains(m, "CSeq: 11 ACK") }); !ok {
		t.Fatalf("ACK de la 2xx: %v", b.falso.Recibidos())
	}
	// Transferir: a un número libre no; a la lista sí, sin Replaces.
	refer := func(rama, destino string) string {
		return fmt.Sprintf("\nREFER sip:asterisk@127.0.0.1;transport=ws SIP/2.0\nVia: SIP/2.0/WSS abc.invalid;branch=z9hG4bK%s\nMax-Forwards: 69\nTo: <sip:x@%s>;tag=%s\nFrom: <sip:201@%s>;tag=nav2\nCall-ID: inv-call-1\nCSeq: 11 REFER\nRefer-To: <%s>\nReferred-By: <sip:201@%s>\nContent-Length: 0\n\n",
			rama, dominio, totag, dominio, destino, dominio)
	}
	b.mandar(refer("rf1", "sip:+15551234567@"+dominio))
	b.respuesta(403)
	b.mandar(refer("rf2", "sip:205@evil.example?Replaces=abc%3Bto-tag%3D1"))
	b.respuesta(202)
	refers := b.recibidosCon(empieza("REFER"))
	if len(refers) != 1 || !strings.Contains(refers[0], "Refer-To: <sip:205@"+dominio+">") || strings.Contains(refers[0], "Replaces") ||
		strings.Contains(refers[0], "evil") || !strings.Contains(refers[0], "CSeq: 12 REFER") {
		t.Fatalf("REFER en la centralita: %v", refers)
	}
	// BYE del navegador.
	b.mandar(fmt.Sprintf("\nBYE sip:asterisk@127.0.0.1;transport=ws SIP/2.0\nVia: SIP/2.0/WSS abc.invalid;branch=z9hG4bKbye1\nMax-Forwards: 69\nTo: <sip:x@%s>;tag=%s\nFrom: <sip:201@%s>;tag=nav2\nCall-ID: inv-call-1\nCSeq: 12 BYE\nContent-Length: 0\n\n",
		dominio, totag, dominio))
	if r := b.respuesta(200); r.CSeq().SeqNo != 12 {
		t.Fatalf("200 del BYE: %s", r)
	}
	if byes := b.recibidosCon(empieza("BYE")); len(byes) != 1 || !strings.Contains(byes[0], "CSeq: 13 BYE") {
		t.Fatalf("BYE en la centralita: %v", byes)
	}
	esperarHechos(t, b.pol, "contestada", "colgada")
	// El mismo destino no se puede volver a marcar.
	b.mandar(strings.Replace(invite("iris-"+b.pol.llamada.String(), "i2", 1), "inv-call-1", "inv-call-2", 1))
	b.respuesta(403)
}

func esperarHechos(t *testing.T, p *politicaFalsa, esperados ...string) {
	t.Helper()
	fin := time.Now().Add(3 * time.Second)
	for time.Now().Before(fin) {
		if strings.Join(p.tipos(), ",") == strings.Join(esperados, ",") {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("hechos: %v, esperaba %v", p.tipos(), esperados)
}

func TestCancelarDespuesDelDesafio(t *testing.T) {
	b := nuevoBanco(t, nil, secretoSIP)
	b.falso.ContestarManual = true
	b.mandar(invite("iris-"+b.pol.llamada.String(), "c1", 7))
	b.respuesta(180)
	b.mandar(fmt.Sprintf("\nCANCEL sip:iris-%s@%s SIP/2.0\nVia: SIP/2.0/WSS abc.invalid;branch=z9hG4bKc1\nMax-Forwards: 69\nTo: <sip:iris-%s@%s>\nFrom: \"Ana\" <sip:201@%s>;tag=nav2\nCall-ID: inv-call-1\nCSeq: 7 CANCEL\nContent-Length: 0\n\n",
		b.pol.llamada, dominio, b.pol.llamada, dominio, dominio))
	// 200 al CANCEL y 487 al INVITE, con la CSeq y la branch del navegador.
	vistos := map[string]bool{}
	for len(vistos) < 2 {
		m := b.recibir()
		r, ok := m.m.(*sip.Response)
		if !ok {
			t.Fatalf("petición inesperada: %s", m.crudo)
		}
		if r.CSeq().SeqNo != 7 || r.Via().Params.GetOr("branch", "") != "z9hG4bKc1" {
			t.Fatalf("respuesta sin corregir: %s", m.crudo)
		}
		vistos[fmt.Sprintf("%d %s", r.StatusCode, r.CSeq().MethodName)] = true
	}
	if !vistos["200 CANCEL"] || !vistos["487 INVITE"] {
		t.Fatalf("respuestas: %v", vistos)
	}
	cancels := b.recibidosCon(empieza("CANCEL"))
	if len(cancels) != 1 || !strings.Contains(cancels[0], "CSeq: 8 CANCEL") || strings.Contains(cancels[0], "branch=z9hG4bKc1\r") {
		t.Fatalf("CANCEL en la centralita: %v", cancels)
	}
	esperarHechos(t, b.pol, "fallo:487:cancelada")
}

func TestCortarCuelgaLaLlamada(t *testing.T) {
	b := nuevoBanco(t, nil, secretoSIP)
	b.mandar(invite("205", "k1", 3))
	b.respuesta(200)
	if len(b.falso.Establecidas()) != 1 {
		t.Fatal("la interna no quedó establecida")
	}
	b.puente.Cortar(sippuente.CierreSinAcceso, "sin_acceso")
	select {
	case <-b.hecho:
	case <-time.After(3 * time.Second):
		t.Fatal("el puente no terminó")
	}
	if _, ok := b.falso.Esperar(2*time.Second, func(m string) bool { return strings.HasPrefix(m, "BYE") && strings.Contains(m, "CSeq: 5 BYE") }); !ok {
		t.Fatalf("no llegó el BYE: %v", b.falso.Recibidos())
	}
	b.nav.mu.Lock()
	defer b.nav.mu.Unlock()
	if b.nav.codigo != sippuente.CierreSinAcceso || b.nav.motivo != "sin_acceso" {
		t.Fatalf("cierre: %d %s", b.nav.codigo, b.nav.motivo)
	}
	b.hecho <- nil
}

func TestEntranteYColgarAlCortar(t *testing.T) {
	b := nuevoBanco(t, nil, secretoSIP)
	callID := b.falso.Llamar("50255550000", dominio)
	m := b.recibir()
	inv, ok := m.m.(*sip.Request)
	if !ok || !inv.IsInvite() {
		t.Fatalf("esperaba el INVITE: %s", m.crudo)
	}
	if h := inv.GetHeaders("X-Iris-Llamada"); len(h) != 1 || h[0].Value() != b.pol.llamada.String() {
		t.Fatalf("X-Iris-Llamada: %v", h)
	}
	r := sip.NewResponseFromRequest(inv, 200, "OK", nil)
	r.AppendHeader(sip.NewHeader("Contact", "<sip:x7@abc.invalid;transport=ws>"))
	b.nav.entrada <- []byte(r.String())
	if _, ok := b.falso.Esperar(2*time.Second, empieza("SIP/2.0 200 OK")); !ok {
		t.Fatal("la 2xx no llegó a la centralita")
	}
	b.recibir() // el ACK de la centralita
	b.puente.Cortar(sippuente.CierreReemplazada, "reemplazada")
	<-b.hecho
	if _, ok := b.falso.Esperar(2*time.Second, func(m string) bool { return strings.HasPrefix(m, "BYE") && strings.Contains(m, callID) }); !ok {
		t.Fatalf("no llegó el BYE: %v", b.falso.Recibidos())
	}
	b.hecho <- nil
}

func TestAppNoRegistraNiRecibe(t *testing.T) {
	b := nuevoBanco(t, func(o *sippuente.Opciones) { o.Registrar = false }, secretoSIP)
	b.mandar(register(1, "ap1", ""))
	b.respuesta(403)
	b.falso.Llamar("50255550000", dominio)
	if _, ok := b.falso.Esperar(2*time.Second, empieza("SIP/2.0 480")); !ok {
		t.Fatal("la entrante a la app no se rechazó")
	}
	b.nada()
	// Pero sí marca.
	b.mandar(invite("205", "ap2", 1))
	b.respuesta(200)
}

func TestLimitePorMinuto(t *testing.T) {
	b := nuevoBanco(t, func(o *sippuente.Opciones) { o.MaxPorMinuto = 3 }, secretoSIP)
	for i := 0; i < 5; i++ {
		b.nav.entrada <- []byte("\r\n\r\n")
	}
	select {
	case <-b.hecho:
	case <-time.After(3 * time.Second):
		t.Fatal("no cortó")
	}
	b.nav.mu.Lock()
	codigo := b.nav.codigo
	b.nav.mu.Unlock()
	if codigo != sippuente.CierreLimite {
		t.Fatalf("cierre %d", codigo)
	}
	b.hecho <- nil
}

func TestProbar(t *testing.T) {
	f := sipfalso.Nuevo(usuarioSIP, secretoSIP)
	defer f.Cerrar()
	o := sippuente.OpcionesWSS{Host: f.Host(), Puerto: f.Puerto(), Ruta: "/ws", Plazo: 2 * time.Second}
	if r := sippuente.Probar(context.Background(), o, dominio); r.Estado != sippuente.PruebaOK || r.Huella != f.Huella() {
		t.Fatalf("probar: %+v", r)
	}
	o.Huella = strings.Repeat("a", 64)
	if r := sippuente.Probar(context.Background(), o, dominio); r.Estado != sippuente.PruebaHuellaDistinta || r.Huella != f.Huella() {
		t.Fatalf("huella: %+v", r)
	}
	o.Huella = f.Huella()
	if r := sippuente.ProbarRegistro(context.Background(), o, sippuente.Credencial{Usuario: usuarioSIP, Secreto: secretoSIP, Dominio: dominio}); r.Estado != sippuente.PruebaOK {
		t.Fatalf("registro: %+v", r)
	}
	if r := sippuente.ProbarRegistro(context.Background(), o, sippuente.Credencial{Usuario: usuarioSIP, Secreto: "mala-mala-mala", Dominio: dominio}); r.Estado != sippuente.PruebaCredenciales {
		t.Fatalf("registro malo: %+v", r)
	}
	for _, m := range f.Recibidos() {
		if strings.HasPrefix(m, "REGISTER") && strings.Contains(strings.ToLower(m), "\ncontact:") {
			t.Fatalf("el REGISTER de prueba agrega un registro:\n%s", m)
		}
	}
	f.SinSIP = true
	o.Plazo = 300 * time.Millisecond
	if r := sippuente.Probar(context.Background(), o, dominio); r.Estado != sippuente.PruebaNoEsSIP {
		t.Fatalf("sin SIP: %+v", r)
	}
	o.Puerto = 1
	if r := sippuente.Probar(context.Background(), o, dominio); r.Estado != sippuente.PruebaNoConecta {
		t.Fatalf("sin conexión: %+v", r)
	}
}
