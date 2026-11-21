package sippuente

// Pruebas de la segunda revisión de seguridad de #246 (N1, N2, N3, N5, N7, N8): el puente con los dos
// lados en memoria, para ver exactamente qué sale hacia cada uno.

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
)

type ladoPrueba struct {
	entrada chan []byte
	salida  chan string
	cerrado chan struct{}
	una     sync.Once
	mu      sync.Mutex
	codigo  int
	motivo  string
}

func nuevoLadoPrueba() *ladoPrueba {
	return &ladoPrueba{entrada: make(chan []byte, 512), salida: make(chan string, 1024), cerrado: make(chan struct{})}
}

func (l *ladoPrueba) Leer(ctx context.Context) ([]byte, error) {
	select {
	case b := <-l.entrada:
		return b, nil
	case <-l.cerrado:
		return nil, errors.New("cerrado")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (l *ladoPrueba) Escribir(_ context.Context, b []byte) error {
	l.salida <- string(b)
	return nil
}

func (l *ladoPrueba) Cerrar(codigo int, motivo string) {
	l.una.Do(func() {
		l.mu.Lock()
		l.codigo, l.motivo = codigo, motivo
		l.mu.Unlock()
		close(l.cerrado)
	})
}

func (l *ladoPrueba) cierre() (int, string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.codigo, l.motivo
}

type politicaPrueba struct {
	mu           sync.Mutex
	llamada      uuid.UUID
	credenciales int
}

func (*politicaPrueba) Invitar(_ context.Context, u string) (Destino, error) {
	if u == "205" || u == "0019005551234" {
		return Destino{Usuario: "205"}, nil
	}
	return Destino{}, errors.New("no")
}
func (*politicaPrueba) Transferir(context.Context, string, *uuid.UUID) (string, error) {
	return "", errors.New("no")
}
func (p *politicaPrueba) Entrante(context.Context, string) *uuid.UUID { id := p.llamada; return &id }
func (*politicaPrueba) Hecho(context.Context, HechoLlamada)           {}
func (p *politicaPrueba) Credenciales(context.Context) {
	p.mu.Lock()
	p.credenciales++
	p.mu.Unlock()
}

type pruebaPuente struct {
	t        *testing.T
	p        *Puente
	nav, cen *ladoPrueba
	pol      *politicaPrueba
	fin      chan error
}

func arrancarPrueba(t *testing.T) *pruebaPuente {
	t.Helper()
	b := &pruebaPuente{t: t, nav: nuevoLadoPrueba(), cen: nuevoLadoPrueba(), pol: &politicaPrueba{llamada: uuid.New()}, fin: make(chan error, 1)}
	b.p = Nuevo(Opciones{Credencial: Credencial{Usuario: "201", Secreto: "clave-larga-201", Dominio: "pbx"}, Registrar: true, Politica: b.pol,
		Relevo: "10.0.0.9", MaxPorMinuto: 10000}, b.nav, b.cen)
	ctx, cancelar := context.WithCancel(context.Background())
	go func() { b.fin <- b.p.Correr(ctx) }()
	t.Cleanup(func() {
		cancelar()
		<-b.fin
	})
	return b
}

func lineas(l ...string) []byte { return []byte(strings.Join(l, "\r\n")) }

func (b *pruebaPuente) de(l *ladoPrueba) string {
	b.t.Helper()
	select {
	case s := <-l.salida:
		return s
	case <-time.After(2 * time.Second):
		b.t.Fatal("no llegó nada")
		return ""
	}
}

func (b *pruebaPuente) nadaDe(l *ladoPrueba) {
	b.t.Helper()
	select {
	case s := <-l.salida:
		b.t.Fatalf("no debía salir nada y salió:\n%s", s)
	case <-time.After(200 * time.Millisecond):
	}
}

func primera(s string) string { l, _, _ := strings.Cut(s, "\r\n"); return l }

func peticion(t *testing.T, s string) *sip.Request {
	t.Helper()
	m, err := sip.ParseMessage([]byte(s))
	r, ok := m.(*sip.Request)
	if err != nil || !ok {
		t.Fatalf("no es una petición: %v\n%s", err, s)
	}
	return r
}

func contestarA(t *testing.T, s string, codigo int, extra ...string) []byte {
	t.Helper()
	r := sip.NewResponseFromRequest(peticion(t, s), codigo, "X", nil)
	for _, h := range extra {
		n, v, _ := strings.Cut(h, ": ")
		r.AppendHeader(sip.NewHeader(n, v))
	}
	return []byte(r.String())
}

func tieneCabecera(s, nombre string) bool {
	cab, _, _ := strings.Cut(s, "\r\n\r\n")
	for _, l := range strings.Split(cab, "\r\n")[1:] {
		n, _, _ := strings.Cut(l, ":")
		if strings.EqualFold(strings.TrimSpace(n), nombre) {
			return true
		}
	}
	return false
}

func entranteConTodo(callID, rama string) []byte {
	return []byte("INVITE sip:201@x.invalid;transport=ws SIP/2.0\r\nVia: SIP/2.0/WSS 192.168.50.10:8089;branch=" + rama + "\r\n" +
		"Record-Route: <sip:192.168.50.10;lr>\r\nFrom: \"Juan\" <sip:+50255551234@pbx>;tag=pbxtag\r\nTo: <sip:201@pbx>\r\nCall-ID: " + callID + "\r\n" +
		"CSeq: 1 INVITE\r\nContact: <sip:asterisk@192.168.50.10:8089;transport=ws>\r\nP-Asserted-Identity: <sip:+50255551234@10.1.1.1>\r\n" +
		"Remote-Party-ID: <sip:+50255551234@10.1.1.1>;party=calling\r\nServer: FPBX-15.0.17(16.30.0)\r\nUser-Agent: FPBX-15.0.17\r\n" +
		"Call-Info: <sip:x>;answer-after=0\r\nAlert-Info: <http://malo.example/ring>;info=alert-autoanswer\r\nDiversion: <sip:200@pbx>\r\n" +
		"X-IRIS-LLAMADA: falso\r\nx-iris-otro: falso\r\nX-Asterisk-Algo: 1\r\nContent-Length: 0\r\n\r\n")
}

var cabecerasProhibidas = []string{"P-Asserted-Identity", "Remote-Party-ID", "Server", "User-Agent", "Call-Info", "Alert-Info", "Diversion", "x-iris-otro",
	"X-Asterisk-Algo", "WWW-Authenticate"}

// N1: de la Issabel al navegador solo pasa la lista blanca (en peticiones y en respuestas), y la única
// X-Iris-Llamada es la que pone el puente.
func TestIssabelANavegadorSoloListaBlanca(t *testing.T) {
	b := arrancarPrueba(t)
	b.cen.entrada <- entranteConTodo("entr1", "z9hG4bKpbx1")
	inv := b.de(b.nav)
	for _, n := range cabecerasProhibidas {
		if tieneCabecera(inv, n) {
			t.Fatalf("pasó %s al navegador:\n%s", n, inv)
		}
	}
	if strings.Count(strings.ToLower(inv), "x-iris-llamada:") != 1 || !strings.Contains(inv, "X-Iris-Llamada: "+b.pol.llamada.String()) {
		t.Fatalf("X-Iris-Llamada: solo la del puente:\n%s", inv)
	}
	for _, n := range []string{"Via", "Record-Route", "Contact", "From", "To", "Call-ID", "CSeq"} {
		if !tieneCabecera(inv, n) {
			t.Fatalf("faltó %s:\n%s", n, inv)
		}
	}

	// Respuesta de la Issabel a un REGISTER del navegador.
	b.nav.entrada <- lineas("REGISTER sip:pbx SIP/2.0", "Via: SIP/2.0/WSS a.invalid;branch=z9hG4bKreg1", "From: <sip:201@pbx>;tag=a", "To: <sip:201@pbx>",
		"Call-ID: reg1", "CSeq: 1 REGISTER", "Contact: <sip:201@a.invalid;transport=ws>", "Expires: 600", "Content-Length: 0", "", "")
	reg := b.de(b.cen)
	b.cen.entrada <- contestarA(t, reg, 200, "Server: FPBX-15.0.17", "P-Asserted-Identity: <sip:201@10.1.1.1>", "X-IRIS-Llamada: falso",
		"Call-Info: <sip:x>;answer-after=0", "Contact: <sip:201@a.invalid;transport=ws>;expires=600")
	ok := b.de(b.nav)
	if primera(ok) != "SIP/2.0 200 X" {
		t.Fatalf("200: %s", ok)
	}
	for _, n := range append(cabecerasProhibidas, "X-Iris-Llamada") {
		if tieneCabecera(ok, n) {
			t.Fatalf("pasó %s al navegador en la respuesta:\n%s", n, ok)
		}
	}
}

// N2: el Contact del navegador solo lleva transport y ob; nada de maddr, lr, contraseña ni ?cabeceras.
func TestContactSoloTransportYOb(t *testing.T) {
	casos := map[string]bool{
		"<sip:201@a.invalid;transport=ws>":                               true,
		"<sip:201@a.invalid;transport=wss;ob>":                           true,
		"<sip:201@a.invalid;transport=ws;maddr=203.0.113.5>":             false,
		"<sip:201@a.invalid;transport=ws;lr>":                            false,
		"<sip:201@a.invalid;transport=ws;user=phone>":                    false,
		"<sip:201@a.invalid;transport=ws?Route=%3Csip:x@203.0.113.5%3E>": false,
		"<sip:201:clave@a.invalid;transport=ws>":                         false,
		"<sip:201@a.invalid;transport=ws;transport=udp>":                 false,
		"<sip:201@a.invalid;transport=udp>":                              false,
		"<sip:201@a.invalid>":                                            false,
		"<sip:201@a.invalid;ob>":                                         false,
	}
	for contacto, quiere := range casos {
		r := peticion(t, "OPTIONS sip:pbx SIP/2.0\r\nVia: SIP/2.0/WSS a.invalid;branch=z9hG4bK1\r\nFrom: <sip:201@pbx>;tag=a\r\nTo: <sip:pbx>\r\n"+
			"Call-ID: c\r\nCSeq: 1 OPTIONS\r\nContact: "+contacto+"\r\nContent-Length: 0\r\n\r\n")
		if got := contactoPropio(r.Contact()); got != quiere {
			t.Errorf("%s: %v, quería %v", contacto, got, quiere)
		}
	}

	// Y en el camino: una 181 con maddr hacia la Issabel sale como 488 sin Contact.
	b := arrancarPrueba(t)
	b.cen.entrada <- entranteConTodo("entr2", "z9hG4bKpbx2")
	b.de(b.nav)
	b.nav.entrada <- lineas("SIP/2.0 181 Call Is Being Forwarded", "Via: SIP/2.0/WSS 192.168.50.10:8089;branch=z9hG4bKpbx2",
		"From: <sip:+50255551234@pbx>;tag=pbxtag", "To: <sip:201@pbx>;tag=navtag", "Call-ID: entr2", "CSeq: 1 INVITE",
		"Contact: <sip:201@a.invalid;transport=ws;maddr=203.0.113.5>", "Content-Length: 0", "", "")
	if s := b.de(b.cen); primera(s) != "SIP/2.0 488 Not Acceptable Here" || strings.Contains(s, "203.0.113.5") {
		t.Fatalf("181 con maddr:\n%s", s)
	}
}

// N3: OPTIONS y REGISTER con Call-ID distintos no ocupan los lugares de las llamadas.
func TestOptionsYRegisterNoOcupanDialogos(t *testing.T) {
	b := arrancarPrueba(t)
	for i := 0; i < 100; i++ {
		b.nav.entrada <- lineas("OPTIONS sip:pbx SIP/2.0", fmt.Sprintf("Via: SIP/2.0/WSS a.invalid;branch=z9hG4bKo%d", i), "From: <sip:201@pbx>;tag=a",
			"To: <sip:pbx>", fmt.Sprintf("Call-ID: opt%d", i), "CSeq: 1 OPTIONS", "Content-Length: 0", "", "")
		b.de(b.cen)
		b.nav.entrada <- lineas("REGISTER sip:pbx SIP/2.0", fmt.Sprintf("Via: SIP/2.0/WSS a.invalid;branch=z9hG4bKr%d", i), "From: <sip:201@pbx>;tag=a",
			"To: <sip:201@pbx>", fmt.Sprintf("Call-ID: reg%d", i), "CSeq: 1 REGISTER", "Contact: <sip:201@a.invalid;transport=ws>", "Content-Length: 0", "", "")
		b.de(b.cen)
	}
	b.p.mu.Lock()
	dialogos, otros := len(b.p.dialogos), len(b.p.otros)
	b.p.mu.Unlock()
	if dialogos != 0 || otros > maxOtrosGuardados {
		t.Fatalf("dialogos=%d otros=%d", dialogos, otros)
	}
	b.nav.entrada <- lineas("INVITE sip:205@pbx SIP/2.0", "Via: SIP/2.0/WSS a.invalid;branch=z9hG4bKinv", "From: <sip:201@pbx>;tag=a", "To: <sip:205@pbx>",
		"Call-ID: inv1", "CSeq: 1 INVITE", "Contact: <sip:201@a.invalid;transport=ws>", "Content-Length: 0", "", "")
	if s := b.de(b.cen); primera(s) != "INVITE sip:205@pbx SIP/2.0" {
		t.Fatalf("el INVITE no salió: %s", s)
	}
	b.cen.entrada <- entranteConTodo("entr9", "z9hG4bKpbx9")
	if s := b.de(b.nav); primera(s) != "INVITE sip:201@x.invalid;transport=ws SIP/2.0" {
		t.Fatalf("la entrante no llegó: %s", s)
	}

	// La final de un OPTIONS borra su estado.
	b.nav.entrada <- lineas("OPTIONS sip:pbx SIP/2.0", "Via: SIP/2.0/WSS a.invalid;branch=z9hG4bKofin", "From: <sip:201@pbx>;tag=a",
		"To: <sip:pbx>", "Call-ID: optfin", "CSeq: 1 OPTIONS", "Content-Length: 0", "", "")
	b.cen.entrada <- contestarA(t, b.de(b.cen), 200)
	b.de(b.nav)
	b.p.mu.Lock()
	_, sigue := b.p.otros["optfin"]
	b.p.mu.Unlock()
	if sigue {
		t.Fatal("el OPTIONS respondido sigue guardado")
	}
}

// N5: si la Issabel ofrece SHA-256 (con auth-int) y MD5, Iris contesta con MD5; si no ofrece nada que
// Iris sepa contestar, corta con otro motivo y no marca la credencial de la extensión.
func TestDesafiosVariosYNoSoportado(t *testing.T) {
	registro := lineas("REGISTER sip:pbx SIP/2.0", "Via: SIP/2.0/WSS a.invalid;branch=z9hG4bKreg1", "From: <sip:201@pbx>;tag=a", "To: <sip:201@pbx>",
		"Call-ID: reg1", "CSeq: 1 REGISTER", "Contact: <sip:201@a.invalid;transport=ws>", "Content-Length: 0", "", "")

	b := arrancarPrueba(t)
	b.nav.entrada <- registro
	reg := b.de(b.cen)
	b.cen.entrada <- contestarA(t, reg, 401,
		`WWW-Authenticate: Digest realm="asterisk",nonce="n1",algorithm=SHA-256,qop="auth-int"`,
		`WWW-Authenticate: Digest realm="asterisk",nonce="n2",algorithm=MD5,qop="auth"`)
	otra := b.de(b.cen)
	if !strings.Contains(otra, "Authorization: Digest") || !strings.Contains(otra, `nonce="n2"`) {
		t.Fatalf("no contestó el desafío MD5:\n%s", otra)
	}
	b.nadaDe(b.nav)

	b2 := arrancarPrueba(t)
	b2.nav.entrada <- registro
	reg = b2.de(b2.cen)
	b2.cen.entrada <- contestarA(t, reg, 401, `WWW-Authenticate: Digest realm="asterisk",nonce="n1",algorithm=SHA-512`,
		`WWW-Authenticate: Digest realm="asterisk",nonce="n2",qop="auth-int"`)
	if s := b2.de(b2.nav); primera(s) != "SIP/2.0 403 Forbidden" || tieneCabecera(s, "WWW-Authenticate") {
		t.Fatalf("sin desafío útil: %s", s)
	}
	select {
	case <-b2.nav.cerrado:
	case <-time.After(2 * time.Second):
		t.Fatal("no cortó")
	}
	if c, m := b2.nav.cierre(); c != CierreSinAcceso || m != "desafio_no_soportado" {
		t.Fatalf("cierre %d %q", c, m)
	}
	time.Sleep(100 * time.Millisecond)
	b2.pol.mu.Lock()
	defer b2.pol.mu.Unlock()
	if b2.pol.credenciales != 0 {
		t.Fatal("marcó la credencial de la extensión")
	}
}

// N7: en un INVITE nuevo, To es la URI que se marca y From va sin nombre visible.
func TestInviteToYFromLimpios(t *testing.T) {
	b := arrancarPrueba(t)
	b.nav.entrada <- lineas("INVITE sip:205@pbx SIP/2.0", "Via: SIP/2.0/WSS a.invalid;branch=z9hG4bKinv2", "Max-Forwards: 70",
		`From: "Banco Industrial" <sip:201@pbx>;tag=a`, `To: "Soporte" <sip:0019005551234@pbx>`, "Call-ID: inv2", "CSeq: 1 INVITE",
		"Contact: <sip:201@a.invalid;transport=ws>", "Content-Length: 0", "", "")
	s := b.de(b.cen)
	r := peticion(t, s)
	if r.To().Address.User != "205" || r.To().DisplayName != "" || r.Recipient.User != "205" || strings.Contains(s, "0019005551234") {
		t.Fatalf("To:\n%s", s)
	}
	if r.From().DisplayName != "" || strings.Contains(s, "Banco") {
		t.Fatalf("From:\n%s", s)
	}
}

// N8: a la Issabel solo vuelven respuestas a sus propias peticiones, y una etiqueta «*» no vale como
// la del navegador antes de que conteste.
func TestRespuestasSoloDeTransaccionesDeLaIssabel(t *testing.T) {
	b := arrancarPrueba(t)
	b.nav.entrada <- lineas("SIP/2.0 200 OK", "Via: SIP/2.0/WSS 192.168.50.10:8089;branch=z9hG4bKotra", "From: <sip:999@pbx>;tag=x", "To: <sip:888@pbx>;tag=y",
		"Call-ID: inexistente", "CSeq: 7 INVITE", "Content-Length: 0", "", "")
	b.nadaDe(b.cen)

	b.cen.entrada <- entranteConTodo("entr3", "z9hG4bKpbx3")
	b.de(b.nav)
	// Otra branch o otro método en la misma llamada: tampoco.
	b.nav.entrada <- lineas("SIP/2.0 200 OK", "Via: SIP/2.0/WSS 192.168.50.10:8089;branch=z9hG4bKpbx3", "From: <sip:+50255551234@pbx>;tag=pbxtag",
		"To: <sip:201@pbx>;tag=navtag", "Call-ID: entr3", "CSeq: 1 BYE", "Content-Length: 0", "", "")
	b.nadaDe(b.cen)
	// BYE con From tag=* antes de contestar: 481 local, nada a la Issabel.
	b.nav.entrada <- lineas("BYE sip:asterisk@192.168.50.10:8089;transport=ws SIP/2.0", "Via: SIP/2.0/WSS a.invalid;branch=z9hG4bKbye",
		"From: <sip:201@pbx>;tag=*", "To: <sip:+50255551234@pbx>;tag=pbxtag", "Call-ID: entr3", "CSeq: 2 BYE", "Content-Length: 0", "", "")
	if s := b.de(b.nav); primera(s) != "SIP/2.0 481 Call/Transaction Does Not Exist" {
		t.Fatalf("BYE con tag=*: %s", s)
	}
	b.nadaDe(b.cen)
	// La respuesta buena sí pasa.
	b.nav.entrada <- lineas("SIP/2.0 180 Ringing", "Via: SIP/2.0/WSS 192.168.50.10:8089;branch=z9hG4bKpbx3", "From: <sip:+50255551234@pbx>;tag=pbxtag",
		"To: <sip:201@pbx>;tag=navtag", "Call-ID: entr3", "CSeq: 1 INVITE", "Content-Length: 0", "", "")
	if s := b.de(b.cen); primera(s) != "SIP/2.0 180 Ringing" {
		t.Fatalf("180: %s", s)
	}
}
