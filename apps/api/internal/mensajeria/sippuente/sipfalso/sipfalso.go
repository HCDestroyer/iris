// Package sipfalso es un Asterisk falso para las pruebas del teléfono de Iris (E4c): un WSS con TLS
// propio y subprotocolo sip que pide credenciales Digest de verdad a REGISTER, INVITE y REFER, contesta
// las llamadas (al momento o cuando la prueba lo pide), acepta CANCEL, BYE y REFER y puede llamar al
// navegador. Guarda todo lo que recibe para que la prueba lo revise. No hay Issabel en CI.
package sipfalso

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/emiago/sipgo/sip"

	"github.com/HCDestroyer/iris/apps/api/internal/mensajeria/sippuente"
)

// Servidor es el Asterisk falso.
type Servidor struct {
	srv     *httptest.Server
	usuario string
	secreto string
	nonce   string

	mu sync.Mutex
	// ContestarManual: los INVITE quedan sonando hasta Contestar (si no, se contestan al momento).
	ContestarManual bool
	// Ocupados: destinos que responden 486.
	Ocupados  map[string]bool
	recibidos []string
	conns     []*websocket.Conn
	sonando   map[string]*sip.Request // Call-ID → INVITE autenticado sin final
	contestas map[string]contestada   // Call-ID → diálogo establecido
	llamadas  map[string]*sip.Request // Call-ID → INVITE de la centralita al navegador
	cseq      uint32
	// SinSIP: abre el WebSocket con el subprotocolo pero no contesta nada.
	SinSIP bool
	// registroProhibido: contesta 403 a todo REGISTER, sin desafío (una Issabel que ya no acepta la
	// extensión). Se cambia con ProhibirRegistro.
	registroProhibido bool
}

// ProhibirRegistro hace que la Issabel falsa conteste 403 a todo REGISTER, sin desafío.
func (s *Servidor) ProhibirRegistro(si bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.registroProhibido = si
}

type contestada struct {
	invite *sip.Request
	totag  string
	conn   *websocket.Conn
	// entrante: la llamó la centralita (From/To al revés).
	entrante bool
	resp     *sip.Response
}

// Nuevo arranca el servidor con la credencial de la extensión.
func Nuevo(usuario, secreto string) *Servidor {
	s := &Servidor{usuario: usuario, secreto: secreto, nonce: sip.GenerateTagN(24), Ocupados: map[string]bool{}, sonando: map[string]*sip.Request{},
		contestas: map[string]contestada{}, llamadas: map[string]*sip.Request{}, cseq: 100}
	s.srv = httptest.NewTLSServer(http.HandlerFunc(s.atender))
	return s
}

// Cerrar apaga el servidor.
func (s *Servidor) Cerrar() {
	s.mu.Lock()
	for _, c := range s.conns {
		_ = c.CloseNow()
	}
	s.mu.Unlock()
	s.srv.Close()
}

// Host y Puerto del WSS.
func (s *Servidor) Host() string { return "127.0.0.1" }

// Puerto del WSS.
func (s *Servidor) Puerto() int {
	_, p, _ := net.SplitHostPort(s.srv.Listener.Addr().String())
	n, _ := strconv.Atoi(p)
	return n
}

// Huella SHA-256 del certificado.
func (s *Servidor) Huella() string {
	h := sha256.Sum256(s.srv.Certificate().Raw)
	return hex.EncodeToString(h[:])
}

// Recibidos: copia de todo lo que llegó.
func (s *Servidor) Recibidos() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.recibidos...)
}

// Conectados: WebSockets abiertos.
func (s *Servidor) Conectados() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.conns)
}

func (s *Servidor) atender(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{"sip"}})
	if err != nil {
		return
	}
	c.SetReadLimit(1 << 20)
	s.mu.Lock()
	s.conns = append(s.conns, c)
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		for i, x := range s.conns {
			if x == c {
				s.conns = append(s.conns[:i], s.conns[i+1:]...)
				break
			}
		}
		s.mu.Unlock()
		_ = c.CloseNow()
	}()
	ctx := context.Background()
	for {
		_, b, err := c.Read(ctx)
		if err != nil {
			return
		}
		s.mu.Lock()
		s.recibidos = append(s.recibidos, string(b))
		sinSIP := s.SinSIP
		s.mu.Unlock()
		if sinSIP {
			continue
		}
		m, err := sip.ParseMessage(b)
		if err != nil {
			continue
		}
		switch x := m.(type) {
		case *sip.Request:
			s.peticion(ctx, c, x)
		case *sip.Response:
			s.respuesta(ctx, c, x)
		}
	}
}

func (s *Servidor) escribir(ctx context.Context, c *websocket.Conn, m sip.Message) {
	_ = c.Write(ctx, websocket.MessageText, []byte(m.String()))
}

func (s *Servidor) responder(ctx context.Context, c *websocket.Conn, req *sip.Request, codigo int, razon string) *sip.Response {
	r := sip.NewResponseFromRequest(req, codigo, razon, nil)
	s.escribir(ctx, c, r)
	return r
}

func (s *Servidor) autenticado(req *sip.Request) bool {
	h := req.GetHeader("Authorization")
	return h != nil && sippuente.VerificarDigest(h.Value(), s.secreto, string(req.Method), s.nonce) &&
		strings.Contains(h.Value(), `username="`+s.usuario+`"`)
}

func (s *Servidor) desafio(ctx context.Context, c *websocket.Conn, req *sip.Request) {
	r := sip.NewResponseFromRequest(req, sip.StatusUnauthorized, "Unauthorized", nil)
	r.AppendHeader(sip.NewHeader("WWW-Authenticate", fmt.Sprintf(`Digest realm="asterisk",nonce="%s",opaque="op4c0",algorithm=MD5,qop="auth"`, s.nonce)))
	s.escribir(ctx, c, r)
}

func (s *Servidor) peticion(ctx context.Context, c *websocket.Conn, req *sip.Request) {
	switch req.Method {
	case sip.OPTIONS:
		s.responder(ctx, c, req, 200, "OK")
	case sip.REGISTER:
		s.mu.Lock()
		prohibido := s.registroProhibido
		s.mu.Unlock()
		if prohibido {
			s.responder(ctx, c, req, 403, "Forbidden")
			return
		}
		if !s.autenticado(req) {
			s.desafio(ctx, c, req)
			return
		}
		r := sip.NewResponseFromRequest(req, 200, "OK", nil)
		if ct := req.Contact(); ct != nil {
			r.AppendHeader(sip.NewHeader("Contact", ct.Value()))
		}
		s.escribir(ctx, c, r)
	case sip.INVITE:
		if tag, _ := req.To().Params.Get("tag"); tag != "" {
			s.responder(ctx, c, req, 200, "OK") // re-INVITE
			return
		}
		if !s.autenticado(req) {
			s.desafio(ctx, c, req)
			return
		}
		s.responder(ctx, c, req, 100, "Trying")
		if s.Ocupados[req.Recipient.User] {
			s.responder(ctx, c, req, 486, "Busy Here")
			return
		}
		s.mu.Lock()
		s.sonando[string(*req.CallID())] = req
		manual := s.ContestarManual
		s.mu.Unlock()
		s.responder(ctx, c, req, 180, "Ringing")
		if !manual {
			s.contestar(ctx, c, string(*req.CallID()))
		}
	case sip.CANCEL:
		s.responder(ctx, c, req, 200, "OK")
		s.mu.Lock()
		inv := s.sonando[string(*req.CallID())]
		delete(s.sonando, string(*req.CallID()))
		s.mu.Unlock()
		if inv != nil {
			s.responder(ctx, c, inv, 487, "Request Terminated")
		}
	case sip.BYE:
		s.mu.Lock()
		delete(s.contestas, string(*req.CallID()))
		s.mu.Unlock()
		s.responder(ctx, c, req, 200, "OK")
	case sip.REFER:
		s.responder(ctx, c, req, 202, "Accepted")
	case sip.ACK:
	case sip.INFO, sip.UPDATE, sip.PRACK:
		s.responder(ctx, c, req, 200, "OK")
	default:
		s.responder(ctx, c, req, 405, "Method Not Allowed")
	}
}

func (s *Servidor) contestar(ctx context.Context, c *websocket.Conn, callID string) bool {
	s.mu.Lock()
	inv := s.sonando[callID]
	delete(s.sonando, callID)
	s.mu.Unlock()
	if inv == nil {
		return false
	}
	r := sip.NewResponseFromRequest(inv, 200, "OK", []byte("v=0\r\no=- 1 1 IN IP4 127.0.0.1\r\ns=-\r\nc=IN IP4 127.0.0.1\r\nt=0 0\r\nm=audio 10000 UDP/TLS/RTP/SAVPF 111\r\n"))
	r.AppendHeader(sip.NewHeader("Contact", "<sip:asterisk@127.0.0.1;transport=ws>"))
	r.AppendHeader(sip.NewHeader("Content-Type", "application/sdp"))
	totag, _ := r.To().Params.Get("tag")
	s.mu.Lock()
	s.contestas[callID] = contestada{invite: inv, totag: totag, conn: c, resp: r}
	s.mu.Unlock()
	s.escribir(ctx, c, r)
	return true
}

// Contestar la llamada que suena con ese Call-ID (con ContestarManual).
func (s *Servidor) Contestar(callID string) bool {
	s.mu.Lock()
	var c *websocket.Conn
	if len(s.conns) > 0 {
		c = s.conns[len(s.conns)-1]
	}
	s.mu.Unlock()
	if c == nil {
		return false
	}
	return s.contestar(context.Background(), c, callID)
}

// Sonando: los Call-ID de los INVITE autenticados sin final.
func (s *Servidor) Sonando() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.sonando))
	for k := range s.sonando {
		out = append(out, k)
	}
	return out
}

// Establecidas: los Call-ID de las llamadas contestadas sin BYE.
func (s *Servidor) Establecidas() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.contestas))
	for k := range s.contestas {
		out = append(out, k)
	}
	return out
}

// Colgar manda BYE desde la centralita en una llamada saliente contestada.
func (s *Servidor) Colgar(callID string) bool {
	s.mu.Lock()
	x, ok := s.contestas[callID]
	delete(s.contestas, callID)
	s.cseq++
	seq := s.cseq
	s.mu.Unlock()
	if !ok {
		return false
	}
	from := x.invite.From()
	bye := sip.NewRequest(sip.BYE, sip.Uri{Scheme: "sip", User: from.Address.User, Host: "iris.invalid"})
	v := &sip.ViaHeader{ProtocolName: "SIP", ProtocolVersion: "2.0", Transport: "WSS", Host: "127.0.0.1", Params: sip.HeaderParams{}}
	v.Params.Add("branch", sip.GenerateBranch())
	bye.AppendHeader(v)
	if x.entrante {
		bye.AppendHeader(sip.NewHeader("From", x.invite.From().Value()))
		bye.AppendHeader(sip.NewHeader("To", x.resp.To().Value()))
	} else {
		bye.AppendHeader(sip.NewHeader("From", x.resp.To().Value()))
		bye.AppendHeader(sip.NewHeader("To", x.invite.From().Value()))
	}
	cid := *x.invite.CallID()
	bye.AppendHeader(&cid)
	bye.AppendHeader(&sip.CSeqHeader{SeqNo: seq, MethodName: sip.BYE})
	cl := sip.ContentLengthHeader(0)
	bye.AppendHeader(&cl)
	s.escribir(context.Background(), x.conn, bye)
	return true
}

// Llamar manda un INVITE de la centralita al último navegador conectado (una entrante desde numero).
// Devuelve el Call-ID.
func (s *Servidor) Llamar(numero, dominio string) string {
	s.mu.Lock()
	var c *websocket.Conn
	if len(s.conns) > 0 {
		c = s.conns[len(s.conns)-1]
	}
	s.cseq++
	seq := s.cseq
	s.mu.Unlock()
	if c == nil {
		return ""
	}
	callID := sip.GenerateTagN(20)
	inv := sip.NewRequest(sip.INVITE, sip.Uri{Scheme: "sip", User: s.usuario, Host: "iris.invalid"})
	v := &sip.ViaHeader{ProtocolName: "SIP", ProtocolVersion: "2.0", Transport: "WSS", Host: "127.0.0.1", Params: sip.HeaderParams{}}
	v.Params.Add("branch", sip.GenerateBranch())
	inv.AppendHeader(v)
	inv.AppendHeader(sip.NewHeader("From", fmt.Sprintf(`"Cliente" <sip:%s@%s>;tag=%s`, numero, dominio, sip.GenerateTagN(8))))
	inv.AppendHeader(sip.NewHeader("To", fmt.Sprintf("<sip:%s@%s>", s.usuario, dominio)))
	cid := sip.CallIDHeader(callID)
	inv.AppendHeader(&cid)
	inv.AppendHeader(&sip.CSeqHeader{SeqNo: seq, MethodName: sip.INVITE})
	inv.AppendHeader(sip.NewHeader("Contact", "<sip:asterisk@127.0.0.1;transport=ws>"))
	// Un intento de colarle una cabecera de Iris al navegador: el puente la quita.
	inv.AppendHeader(sip.NewHeader("X-Iris-Llamada", "00000000-0000-0000-0000-000000000000"))
	cl := sip.ContentLengthHeader(0)
	inv.AppendHeader(&cl)
	s.mu.Lock()
	s.llamadas[callID] = inv
	s.mu.Unlock()
	s.escribir(context.Background(), c, inv)
	return callID
}

// respuesta del navegador a una entrante: con 2xx, ACK y queda establecida.
func (s *Servidor) respuesta(ctx context.Context, c *websocket.Conn, res *sip.Response) {
	if res.CSeq().MethodName != sip.INVITE || res.StatusCode < 200 {
		return
	}
	callID := string(*res.CallID())
	s.mu.Lock()
	inv := s.llamadas[callID]
	delete(s.llamadas, callID)
	s.mu.Unlock()
	if inv == nil {
		return
	}
	destino := inv.Recipient
	if ct := res.Contact(); ct != nil {
		destino = ct.Address
	}
	ack := sip.NewRequest(sip.ACK, destino)
	v := &sip.ViaHeader{ProtocolName: "SIP", ProtocolVersion: "2.0", Transport: "WSS", Host: "127.0.0.1", Params: sip.HeaderParams{}}
	v.Params.Add("branch", sip.GenerateBranch())
	ack.AppendHeader(v)
	ack.AppendHeader(sip.NewHeader("From", inv.From().Value()))
	ack.AppendHeader(sip.NewHeader("To", res.To().Value()))
	cid := *inv.CallID()
	ack.AppendHeader(&cid)
	ack.AppendHeader(&sip.CSeqHeader{SeqNo: inv.CSeq().SeqNo, MethodName: sip.ACK})
	cl := sip.ContentLengthHeader(0)
	ack.AppendHeader(&cl)
	s.escribir(ctx, c, ack)
	if res.IsSuccess() {
		s.mu.Lock()
		s.contestas[callID] = contestada{invite: inv, conn: c, entrante: true, resp: res}
		s.mu.Unlock()
	}
}

// Esperar hasta que llegue un mensaje que cumpla f (o se acabe el plazo).
func (s *Servidor) Esperar(plazo time.Duration, f func(string) bool) (string, bool) {
	fin := time.Now().Add(plazo)
	for time.Now().Before(fin) {
		for _, m := range s.Recibidos() {
			if f(m) {
				return m, true
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	return "", false
}
