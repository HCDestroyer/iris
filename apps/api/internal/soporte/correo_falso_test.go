package soporte_test

import (
	"bufio"
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"math/big"
	"net"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/soporte"
	"github.com/HCDestroyer/iris/apps/api/internal/soporte/correo"
)

// Servidores falsos para las pruebas del correo: IMAP en memoria (go-imap imapmemserver) y un
// SMTP mínimo, los dos con TLS de una CA de prueba (127.0.0.1).

// tlsPrueba genera una CA y un certificado de servidor para 127.0.0.1 y localhost.
func tlsPrueba(t *testing.T) (*tls.Config, *x509.CertPool) {
	t.Helper()
	clave, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	plantilla := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: "iris-prueba"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true,
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, DNSNames: []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, plantilla, plantilla, &clave.PublicKey, clave)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	raices := x509.NewCertPool()
	raices.AddCert(cert)
	return &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: clave}}, MinVersion: tls.VersionTLS12}, raices
}

// imapFalso es un servidor IMAP en memoria con un usuario e INBOX.
type imapFalso struct {
	puerto  int
	usuario *imapmemserver.User
	trafico *bytes.Buffer // lo que viajó en claro antes de TLS (modo sin TLS)
	mu      *sync.Mutex
}

// modo: "tls" (IMAPS), "starttls" o "claro" (sin STARTTLS: para probar que Iris no sigue).
func nuevoIMAPFalso(t *testing.T, cfg *tls.Config, modo, usuario, clave string) *imapFalso {
	t.Helper()
	mem := imapmemserver.New()
	u := imapmemserver.NewUser(usuario, clave)
	if err := u.Create("INBOX", nil); err != nil {
		t.Fatal(err)
	}
	if err := u.Create("Procesados", nil); err != nil {
		t.Fatal(err)
	}
	mem.AddUser(u)
	f := &imapFalso{usuario: u, trafico: &bytes.Buffer{}, mu: &sync.Mutex{}}
	op := &imapserver.Options{
		NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return mem.NewSession(), nil, nil
		},
		Caps:   imap.CapSet{imap.CapIMAP4rev1: {}, imap.CapIMAP4rev2: {}, imap.CapMove: {}},
		Logger: silencioIMAP{},
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	switch modo {
	case "tls":
		ln = tls.NewListener(ln, cfg)
	case "starttls":
		op.TLSConfig = cfg
	case "claro":
		op.InsecureAuth = true
		op.DebugWriter = escritorSeguro{f}
	}
	srv := imapserver.New(op)
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	f.puerto = ln.Addr().(*net.TCPAddr).Port
	return f
}

type silencioIMAP struct{}

func (silencioIMAP) Printf(string, ...any) {}

type escritorSeguro struct{ f *imapFalso }

func (e escritorSeguro) Write(p []byte) (int, error) {
	e.f.mu.Lock()
	defer e.f.mu.Unlock()
	return e.f.trafico.Write(p)
}

func (f *imapFalso) vioEnClaro(s string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.Contains(f.trafico.String(), s)
}

// agregar deja un correo en INBOX (con banderas opcionales).
func (f *imapFalso) agregar(t *testing.T, crudo string, banderas ...imap.Flag) {
	t.Helper()
	b := []byte(strings.ReplaceAll(crudo, "\n", "\r\n"))
	if _, err := f.usuario.Append("INBOX", literal{bytes.NewReader(b), int64(len(b))}, &imap.AppendOptions{Flags: banderas}); err != nil {
		t.Fatal(err)
	}
}

type literal struct {
	*bytes.Reader
	n int64
}

func (l literal) Size() int64 { return l.n }

// estado devuelve (mensajes, no leídos) de la carpeta.
func (f *imapFalso) estado(t *testing.T, carpeta string) (uint32, uint32) {
	t.Helper()
	d, err := f.usuario.Status(carpeta, &imap.StatusOptions{NumMessages: true, NumUnseen: true})
	if err != nil {
		t.Fatal(err)
	}
	return *d.NumMessages, *d.NumUnseen
}

// smtpFalso es un servidor SMTP mínimo con STARTTLS (o TLS implícito) y AUTH PLAIN.
type smtpFalso struct {
	puerto   int
	mu       sync.Mutex
	mensajes []string
	auth     []string // usuario|contraseña recibidos
}

func nuevoSMTPFalso(t *testing.T, cfg *tls.Config, modo string) *smtpFalso {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if modo == "tls" {
		ln = tls.NewListener(ln, cfg)
	}
	s := &smtpFalso{puerto: ln.Addr().(*net.TCPAddr).Port}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go s.atender(c, cfg, modo == "tls")
		}
	}()
	return s
}

func (s *smtpFalso) atender(c net.Conn, cfg *tls.Config, enTLS bool) {
	defer func() { _ = c.Close() }()
	_ = c.SetDeadline(time.Now().Add(20 * time.Second))
	r, w := bufio.NewReader(c), bufio.NewWriter(c)
	decir := func(l string) { _, _ = w.WriteString(l + "\r\n"); _ = w.Flush() }
	decir("220 smtp-falso")
	for {
		l, err := r.ReadString('\n')
		if err != nil {
			return
		}
		l = strings.TrimRight(l, "\r\n")
		cmd := strings.ToUpper(strings.SplitN(l, " ", 2)[0])
		switch cmd {
		case "EHLO", "HELO":
			if !enTLS {
				decir("250-smtp-falso")
				decir("250 STARTTLS")
			} else {
				decir("250-smtp-falso")
				decir("250 AUTH PLAIN")
			}
		case "STARTTLS":
			decir("220 listo")
			t := tls.Server(c, cfg)
			if err := t.Handshake(); err != nil {
				return
			}
			c, enTLS = t, true
			r, w = bufio.NewReader(c), bufio.NewWriter(c)
		case "AUTH":
			partes := strings.Fields(l)
			if len(partes) == 3 {
				crudo, _ := base64.StdEncoding.DecodeString(partes[2])
				campos := strings.Split(string(crudo), "\x00")
				if len(campos) == 3 {
					s.mu.Lock()
					s.auth = append(s.auth, campos[1]+"|"+campos[2])
					s.mu.Unlock()
				}
			}
			decir("235 ok")
		case "MAIL", "RCPT", "RSET", "NOOP":
			decir("250 ok")
		case "DATA":
			decir("354 adelante")
			var b strings.Builder
			for {
				l, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
				b.WriteString(strings.TrimPrefix(l, "."))
			}
			s.mu.Lock()
			s.mensajes = append(s.mensajes, b.String())
			s.mu.Unlock()
			decir("250 aceptado")
		case "QUIT":
			decir("221 adiós")
			return
		default:
			decir("502 no")
		}
	}
}

func (s *smtpFalso) recibidos() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.mensajes...)
}

// moduloCorreo arma el módulo con cifrado, red local permitida y la CA de prueba.
func moduloCorreo(t *testing.T, raices *x509.CertPool) *soporte.Modulo {
	t.Helper()
	return moduloCorreoCon(t, raices, []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")})
}

// moduloCorreoCon: como moduloCorreo con las redes internas permitidas dadas (nil = producción).
func moduloCorreoCon(t *testing.T, raices *x509.CertPool, redes []netip.Prefix) *soporte.Modulo {
	t.Helper()
	return moduloCorreoAjustado(t, raices, redes, nil)
}

// moduloCorreoAjustado: como moduloCorreoCon, con un ajuste a la configuración.
func moduloCorreoAjustado(t *testing.T, raices *x509.CertPool, redes []netip.Prefix, ajustar func(*soporte.Config)) *soporte.Modulo {
	t.Helper()
	clave := make([]byte, 32)
	_, _ = rand.Read(clave)
	cif, err := nucleo.NuevoCifrador(base64.StdEncoding.EncodeToString(clave))
	if err != nil {
		t.Fatal(err)
	}
	cfg := soporte.Config{Cifrador: cif, Correo: soporte.ConfigCorreo{Red: correo.Opciones{
		RedesPermitidas: redes, Raices: raices, Timeout: 10 * time.Second,
	}}}
	if ajustar != nil {
		ajustar(&cfg)
	}
	return soporte.Nuevo(cfg)
}

func puerto(p int) int32 { return int32(p) }
