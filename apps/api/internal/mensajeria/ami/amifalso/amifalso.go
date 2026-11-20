// Package amifalso es un AMI de Asterisk falso, sobre TLS, para las pruebas de la centralita
// (E4a). Se porta como el real en lo que Iris usa: saludo, Login (con Events), Ping, ListCommands
// (solo las acciones que el usuario puede escribir), Logoff y eventos. Las pruebas le piden
// emitir eventos, cortar la conexión, cambiar de certificado o mandar basura.
package amifalso

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"math/big"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/mensajeria/ami"
)

// Servidor es el AMI falso.
type Servidor struct {
	ln      net.Listener
	mu      sync.Mutex
	cert    tls.Certificate
	usuario string
	secreto string
	// Acciones que ListCommands devuelve (lo que el usuario puede escribir, además de Ping), con
	// sus clases (Priv) como las anota Asterisk.
	acciones [][2]string
	// pausado: corta cada conexión nueva antes del TLS (la centralita no responde).
	pausado  bool
	conns    map[*sesion]bool
	logins   int
	fallidos int
	// recibido: cada acción que llegó (nombre), para comprobar que Iris no manda nada de escritura.
	recibido []string
	// eventos de las sesiones: lo último pedido en Login.
	clases []string
	// sinRespuestaPing: no contesta Ping (para probar el corte por latido).
	sinRespuestaPing bool
	// sinRespuestaLC: no contesta ListCommands.
	sinRespuestaLC bool
}

type sesion struct {
	c  net.Conn
	mu sync.Mutex
}

// Nuevo levanta el AMI en 127.0.0.1 con un certificado autofirmado nuevo.
func Nuevo(usuario, secreto string) (*Servidor, error) {
	s := &Servidor{usuario: usuario, secreto: secreto, conns: map[*sesion]bool{}}
	cert, err := certificado()
	if err != nil {
		return nil, err
	}
	s.cert = cert
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	s.ln = ln
	go s.aceptar()
	return s, nil
}

// Puerto donde escucha.
func (s *Servidor) Puerto() int { return s.ln.Addr().(*net.TCPAddr).Port }

// Huella del certificado actual.
func (s *Servidor) Huella() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return ami.HuellaDe(s.cert.Certificate[0])
}

// CambiarCertificado genera otro certificado (otra huella) para las conexiones nuevas.
func (s *Servidor) CambiarCertificado() error {
	c, err := certificado()
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.cert = c
	s.mu.Unlock()
	return nil
}

// clasesDe: las clases (Priv) con que Asterisk anota algunas acciones de escritura.
var clasesDe = map[string]string{
	"originate": "originate,all", "command": "command,all", "hangup": "system,call,all", "redirect": "call,all",
	"updateconfig": "config,all", "queueadd": "agent,all", "playdtmf": "call,all", "userevent": "user,all",
}

// PermitirEscritura hace que ListCommands devuelva estas acciones (p. ej. Originate, Command), con
// sus clases de escritura.
func (s *Servidor) PermitirEscritura(acciones ...string) {
	s.mu.Lock()
	s.acciones = nil
	for _, a := range acciones {
		c, ok := clasesDe[strings.ToLower(a)]
		if !ok {
			c = "system,call,all"
		}
		s.acciones = append(s.acciones, [2]string{a, c})
	}
	s.mu.Unlock()
}

// PermitirAccion suma a ListCommands una acción con las clases dadas (p. ej. "agent,all").
func (s *Servidor) PermitirAccion(accion, clases string) {
	s.mu.Lock()
	s.acciones = append(s.acciones, [2]string{accion, clases})
	s.mu.Unlock()
}

// Pausar: con true, cada conexión nueva se corta antes del TLS (como una centralita que no responde).
func (s *Servidor) Pausar(v bool) {
	s.mu.Lock()
	s.pausado = v
	s.mu.Unlock()
}

// SinPing deja de contestar Ping.
func (s *Servidor) SinPing(v bool) {
	s.mu.Lock()
	s.sinRespuestaPing = v
	s.mu.Unlock()
}

// SinListCommands deja de contestar ListCommands.
func (s *Servidor) SinListCommands(v bool) {
	s.mu.Lock()
	s.sinRespuestaLC = v
	s.mu.Unlock()
}

// Logins correctos y fallidos.
func (s *Servidor) Logins() (ok, fallidos int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.logins, s.fallidos
}

// Recibido: las acciones que llegaron, en orden.
func (s *Servidor) Recibido() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.recibido...)
}

// Clases de eventos pedidas en el último Login.
func (s *Servidor) Clases() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.clases...)
}

// Conectados: sesiones abiertas y autenticadas.
func (s *Servidor) Conectados() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.conns)
}

// Emitir manda un evento a todas las sesiones autenticadas. campos: "Clave", "valor", …
func (s *Servidor) Emitir(campos ...string) {
	var b strings.Builder
	for i := 0; i+1 < len(campos); i += 2 {
		fmt.Fprintf(&b, "%s: %s\r\n", campos[i], campos[i+1])
	}
	b.WriteString("\r\n")
	s.Crudo(b.String())
}

// Crudo escribe bytes tal cual a todas las sesiones (basura, paquetes enormes…).
func (s *Servidor) Crudo(t string) {
	s.mu.Lock()
	ss := make([]*sesion, 0, len(s.conns))
	for x := range s.conns {
		ss = append(ss, x)
	}
	s.mu.Unlock()
	for _, x := range ss {
		x.mu.Lock()
		_ = x.c.SetWriteDeadline(time.Now().Add(2 * time.Second))
		_, _ = x.c.Write([]byte(t))
		x.mu.Unlock()
	}
}

// Cortar cierra todas las sesiones abiertas.
func (s *Servidor) Cortar() {
	s.mu.Lock()
	for x := range s.conns {
		_ = x.c.Close()
	}
	s.conns = map[*sesion]bool{}
	s.mu.Unlock()
}

// Cerrar apaga el servidor.
func (s *Servidor) Cerrar() {
	_ = s.ln.Close()
	s.Cortar()
}

func (s *Servidor) aceptar() {
	for {
		c, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.atender(c)
	}
}

func (s *Servidor) atender(raw net.Conn) {
	s.mu.Lock()
	cert, pausado := s.cert, s.pausado
	s.mu.Unlock()
	if pausado {
		_ = raw.Close()
		return
	}
	c := tls.Server(raw, &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})
	if err := c.Handshake(); err != nil {
		_ = raw.Close()
		return
	}
	x := &sesion{c: c}
	escribir := func(t string) {
		x.mu.Lock()
		_, _ = c.Write([]byte(t))
		x.mu.Unlock()
	}
	escribir("Asterisk Call Manager/7.0.3\r\n")
	r := bufio.NewReader(c)
	autenticado := false
	defer func() {
		s.mu.Lock()
		delete(s.conns, x)
		s.mu.Unlock()
		_ = c.Close()
	}()
	for {
		p, err := leer(r)
		if err != nil {
			return
		}
		accion := p["action"]
		id := p["actionid"]
		s.mu.Lock()
		s.recibido = append(s.recibido, accion)
		s.mu.Unlock()
		switch strings.ToLower(accion) {
		case "login":
			s.mu.Lock()
			ok := p["username"] == s.usuario && p["secret"] == s.secreto
			if ok {
				s.logins++
				s.clases = strings.Split(p["events"], ",")
			} else {
				s.fallidos++
			}
			s.mu.Unlock()
			if !ok {
				escribir("Response: Error\r\nActionID: " + id + "\r\nMessage: Authentication failed\r\n\r\n")
				return
			}
			autenticado = true
			s.mu.Lock()
			s.conns[x] = true
			s.mu.Unlock()
			escribir("Response: Success\r\nActionID: " + id + "\r\nMessage: Authentication accepted\r\n\r\n")
			escribir("Event: FullyBooted\r\nPrivilege: system,all\r\nStatus: Fully Booted\r\n\r\n")
		case "ping":
			if !autenticado {
				return
			}
			s.mu.Lock()
			calla := s.sinRespuestaPing
			s.mu.Unlock()
			if !calla {
				escribir("Response: Success\r\nActionID: " + id + "\r\nPing: Pong\r\nTimestamp: 1700000000.000000\r\n\r\n")
			}
		case "listcommands":
			if !autenticado {
				return
			}
			s.mu.Lock()
			if s.sinRespuestaLC {
				s.mu.Unlock()
				continue
			}
			// Las que no exigen clase salen siempre, con «<none>»; las demás, si el write= las deja.
			acc := append([][2]string{{"Ping", "<none>"}, {"Logoff", "<none>"}, {"ListCommands", "<none>"}, {"Events", "<none>"}}, s.acciones...)
			s.mu.Unlock()
			var b strings.Builder
			b.WriteString("Response: Success\r\nActionID: " + id + "\r\n")
			for _, a := range acc {
				fmt.Fprintf(&b, "%s: %s.  (Priv: %s)\r\n", a[0], a[0], a[1])
			}
			b.WriteString("\r\n")
			escribir(b.String())
		case "logoff":
			escribir("Response: Goodbye\r\nActionID: " + id + "\r\n\r\n")
			return
		default:
			// Como Asterisk sin permiso: Permission denied.
			escribir("Response: Error\r\nActionID: " + id + "\r\nMessage: Permission denied\r\n\r\n")
		}
	}
}

func leer(r *bufio.Reader) (map[string]string, error) {
	p := map[string]string{}
	for {
		l, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		l = strings.TrimRight(l, "\r\n")
		if l == "" {
			if len(p) == 0 {
				continue
			}
			return p, nil
		}
		k, v, _ := strings.Cut(l, ":")
		p[strings.ToLower(strings.TrimSpace(k))] = strings.TrimSpace(v)
	}
}

func certificado() (tls.Certificate, error) {
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	sn, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		return tls.Certificate{}, err
	}
	t := &x509.Certificate{SerialNumber: sn, Subject: pkix.Name{CommonName: "issabel.local"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, t, t, &k.PublicKey, k)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: k}, nil
}
