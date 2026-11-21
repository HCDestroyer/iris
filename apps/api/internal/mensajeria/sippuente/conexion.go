package sippuente

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/coder/websocket"
	"github.com/emiago/sipgo/sip"
)

// Límites de los mensajes.
const (
	// MaxNavegador: bytes de un mensaje del navegador.
	MaxNavegador = 16 << 10
	// MaxCentralita: bytes de un mensaje de la Issabel (un INVITE con SDP de WebRTC ronda 4 KB).
	MaxCentralita = 64 << 10
	// Subprotocolo de SIP sobre WebSocket (RFC 7118).
	Subprotocolo = "sip"
)

var (
	// ErrConexion: no se pudo conectar (un solo error para todo: la prueba no sirve para escanear).
	ErrConexion = errors.New("sippuente: sin conexión")
	// ErrNoSIP: abrió el WebSocket pero no habla SIP.
	ErrNoSIP = errors.New("sippuente: no es SIP")
)

// ErrHuella: el certificado no es el fijado.
type ErrHuella struct{ Vista string }

func (e *ErrHuella) Error() string { return "sippuente: la huella del certificado cambió" }

// LadoWS es un Lado sobre un WebSocket de github.com/coder/websocket.
type LadoWS struct {
	c  *websocket.Conn
	mu sync.Mutex
}

// NuevoLadoWS envuelve una conexión (con su tope de lectura).
func NuevoLadoWS(c *websocket.Conn, maximo int64) *LadoWS {
	c.SetReadLimit(maximo)
	return &LadoWS{c: c}
}

var errBinario = errors.New("sippuente: mensaje binario")

// Leer un mensaje de texto (un binario es un error).
func (l *LadoWS) Leer(ctx context.Context) ([]byte, error) {
	t, b, err := l.c.Read(ctx)
	if err != nil {
		return nil, err
	}
	if t != websocket.MessageText {
		// SIP sobre WebSocket va en texto (RFC 7118): un binario corta (y así no escapa al tope por minuto).
		return nil, errBinario
	}
	return b, nil
}

// Escribir un mensaje de texto (las escrituras se ordenan).
func (l *LadoWS) Escribir(ctx context.Context, b []byte) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	wctx, cancelar := context.WithTimeout(ctx, 10*time.Second)
	defer cancelar()
	return l.c.Write(wctx, websocket.MessageText, b)
}

// Cerrar con código y motivo: espera el cierre del otro lado como mucho 2 s (así el código llega antes
// de que el handler termine) y después corta.
func (l *LadoWS) Cerrar(codigo int, motivo string) {
	hecho := make(chan struct{})
	go func() {
		defer close(hecho)
		_ = l.c.Close(websocket.StatusCode(codigo), motivo)
	}()
	select {
	case <-hecho:
	case <-time.After(2 * time.Second):
		_ = l.c.CloseNow()
	}
}

// OpcionesWSS de la conexión de Iris al WSS de la Issabel.
type OpcionesWSS struct {
	Host   string
	Puerto int
	Ruta   string
	// Huella SHA-256 (hex) fijada. Vacía solo al probar: se acepta y se informa.
	Huella string
	Plazo  time.Duration
	// Control del marcador (reglas de red: sin loopback, link-local ni metadata).
	Control func(network, address string, c syscall.RawConn) error
}

// Conectar abre wss://host:puerto/ruta con el subprotocolo sip y el certificado fijado.
func Conectar(ctx context.Context, o OpcionesWSS) (*websocket.Conn, string, error) {
	if o.Plazo <= 0 {
		o.Plazo = 10 * time.Second
	}
	if !strings.HasPrefix(o.Ruta, "/") || strings.ContainsAny(o.Ruta, "?#\r\n ") {
		return nil, "", ErrConexion
	}
	var vista string
	var errHuella error
	tr := &http.Transport{
		Proxy:               nil,
		DialContext:         (&net.Dialer{Timeout: o.Plazo, Control: o.Control, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout: o.Plazo,
		ForceAttemptHTTP2:   false,
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
			// La verificación es por huella: Issabel trae certificados autofirmados.
			InsecureSkipVerify: true, //nolint:gosec // se verifica la huella fijada en VerifyConnection
			VerifyConnection: func(cs tls.ConnectionState) error {
				if len(cs.PeerCertificates) == 0 {
					return ErrConexion
				}
				s := sha256.Sum256(cs.PeerCertificates[0].Raw)
				vista = hex.EncodeToString(s[:])
				if o.Huella != "" && !strings.EqualFold(vista, o.Huella) {
					errHuella = &ErrHuella{Vista: vista}
					return errHuella
				}
				return nil
			},
			NextProtos: []string{"http/1.1"},
		},
	}
	defer tr.CloseIdleConnections()
	cli := &http.Client{Transport: tr, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	dctx, cancelar := context.WithTimeout(ctx, o.Plazo)
	defer cancelar()
	url := "wss://" + net.JoinHostPort(o.Host, strconv.Itoa(o.Puerto)) + o.Ruta
	c, resp, err := websocket.Dial(dctx, url, &websocket.DialOptions{HTTPClient: cli, Subprotocols: []string{Subprotocolo}})
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		if errHuella != nil {
			return nil, vista, errHuella
		}
		if resp != nil && vista != "" {
			return nil, vista, ErrNoSIP // respondió HTTP sobre TLS, pero no abrió el WebSocket
		}
		return nil, vista, ErrConexion
	}
	if c.Subprotocol() != Subprotocolo {
		_ = c.Close(websocket.StatusProtocolError, "sip")
		return nil, vista, ErrNoSIP
	}
	return c, vista, nil
}

// Resultados de las pruebas.
const (
	PruebaOK             = "ok"
	PruebaHuellaDistinta = "huella_distinta"
	PruebaNoConecta      = "no_conecta"
	PruebaNoEsSIP        = "no_es_sip"
	PruebaCredenciales   = "credenciales"
)

// Prueba es el resultado de Probar o ProbarRegistro.
type Prueba struct {
	Estado string
	Huella string
}

func resultadoConexion(err error, vista string) Prueba {
	var eh *ErrHuella
	switch {
	case errors.As(err, &eh):
		return Prueba{Estado: PruebaHuellaDistinta, Huella: eh.Vista}
	case errors.Is(err, ErrNoSIP):
		return Prueba{Estado: PruebaNoEsSIP, Huella: vista}
	default:
		return Prueba{Estado: PruebaNoConecta, Huella: vista}
	}
}

// Probar abre el WSS y manda un OPTIONS sin credenciales: cualquier respuesta SIP es «ok».
func Probar(ctx context.Context, o OpcionesWSS, dominio string) Prueba {
	c, vista, err := Conectar(ctx, o)
	if err != nil {
		return resultadoConexion(err, vista)
	}
	defer func() { _ = c.CloseNow() }()
	l := NuevoLadoWS(c, MaxCentralita)
	pctx, cancelar := context.WithTimeout(ctx, plazo(o))
	defer cancelar()
	rama := sip.GenerateBranch()
	opt := fmt.Sprintf("OPTIONS sip:%s SIP/2.0\r\nVia: SIP/2.0/WSS iris.invalid;branch=%s\r\nMax-Forwards: 70\r\nFrom: <sip:iris@iris.invalid>;tag=%s\r\n"+
		"To: <sip:%s>\r\nCall-ID: %s\r\nCSeq: 1 OPTIONS\r\nContent-Length: 0\r\n\r\n", dominio, rama, sip.GenerateTagN(10), dominio, sip.GenerateTagN(20))
	if err := l.Escribir(pctx, []byte(opt)); err != nil {
		return Prueba{Estado: PruebaNoConecta, Huella: vista}
	}
	for {
		b, err := l.Leer(pctx)
		if err != nil {
			return Prueba{Estado: PruebaNoEsSIP, Huella: vista}
		}
		if r, ok := respuestaA(b, rama); ok && r.StatusCode >= 200 {
			return Prueba{Estado: PruebaOK, Huella: vista}
		}
	}
}

func plazo(o OpcionesWSS) time.Duration {
	if o.Plazo <= 0 {
		return 10 * time.Second
	}
	return o.Plazo
}

func respuestaA(b []byte, rama string) (*sip.Response, bool) {
	m, err := sip.ParseMessage(b)
	r, ok := m.(*sip.Response)
	if err != nil || !ok || r.Via() == nil || r.Via().Params.GetOr("branch", "") != rama {
		return nil, false
	}
	return r, true
}

// ProbarRegistro manda un REGISTER de consulta (sin Contact: no agrega ni quita registros) y
// responde el desafío con la credencial.
func ProbarRegistro(ctx context.Context, o OpcionesWSS, cred Credencial) Prueba {
	c, vista, err := Conectar(ctx, o)
	if err != nil {
		return resultadoConexion(err, vista)
	}
	defer func() { _ = c.CloseNow() }()
	l := NuevoLadoWS(c, MaxCentralita)
	pctx, cancelar := context.WithTimeout(ctx, 2*plazo(o))
	defer cancelar()
	callID, etiqueta := sip.GenerateTagN(20), sip.GenerateTagN(10)
	uri := "sip:" + cred.Dominio
	armar := func(seq int, rama, autorizacion string) string {
		var b strings.Builder
		fmt.Fprintf(&b, "REGISTER %s SIP/2.0\r\nVia: SIP/2.0/WSS iris.invalid;branch=%s\r\nMax-Forwards: 70\r\n", uri, rama)
		fmt.Fprintf(&b, "From: <sip:%s@%s>;tag=%s\r\nTo: <sip:%s@%s>\r\nCall-ID: %s\r\nCSeq: %d REGISTER\r\n", cred.Usuario, cred.Dominio, etiqueta,
			cred.Usuario, cred.Dominio, callID, seq)
		if autorizacion != "" {
			b.WriteString(autorizacion + "\r\n")
		}
		b.WriteString("Content-Length: 0\r\n\r\n")
		return b.String()
	}
	rama := sip.GenerateBranch()
	if err := l.Escribir(pctx, []byte(armar(1, rama, ""))); err != nil {
		return Prueba{Estado: PruebaNoConecta, Huella: vista}
	}
	intentos := 0
	for {
		b, err := l.Leer(pctx)
		if err != nil {
			return Prueba{Estado: PruebaNoConecta, Huella: vista}
		}
		r, ok := respuestaA(b, rama)
		if !ok || r.StatusCode < 200 {
			continue
		}
		switch {
		case r.IsSuccess():
			return Prueba{Estado: PruebaOK, Huella: vista}
		case (r.StatusCode == sip.StatusUnauthorized || r.StatusCode == sip.StatusProxyAuthRequired) && intentos == 0:
			cab, nombre := "WWW-Authenticate", "Authorization"
			if r.StatusCode == sip.StatusProxyAuthRequired {
				cab, nombre = "Proxy-Authenticate", "Proxy-Authorization"
			}
			h := r.GetHeader(cab)
			if h == nil {
				return Prueba{Estado: PruebaNoConecta, Huella: vista}
			}
			dz, err := LeerDesafio(h.Value())
			if err != nil {
				return Prueba{Estado: PruebaNoConecta, Huella: vista}
			}
			v, err := Responder(dz, cred.Usuario, cred.Secreto, "REGISTER", uri, 1)
			if err != nil {
				return Prueba{Estado: PruebaNoConecta, Huella: vista}
			}
			intentos++
			rama = sip.GenerateBranch()
			if err := l.Escribir(pctx, []byte(armar(2, rama, nombre+": "+v))); err != nil {
				return Prueba{Estado: PruebaNoConecta, Huella: vista}
			}
		case r.StatusCode == sip.StatusUnauthorized || r.StatusCode == sip.StatusForbidden || r.StatusCode == sip.StatusProxyAuthRequired:
			return Prueba{Estado: PruebaCredenciales, Huella: vista}
		default:
			return Prueba{Estado: PruebaNoConecta, Huella: vista}
		}
	}
}
