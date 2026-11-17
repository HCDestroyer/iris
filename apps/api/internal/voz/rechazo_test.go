package voz_test

import (
	"context"
	"crypto"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/voz"
	"github.com/HCDestroyer/iris/apps/api/internal/voz/vozprueba"
)

// Qué cuenta como «el servidor dijo que no» (lo que Iris usa para dar por no entregada una
// credencial): solo lo que nadie en el camino puede falsificar.
func TestRechazoExplicito(t *testing.T) {
	b := vozprueba.NuevoBanco(t, vozprueba.Opciones{})
	cert := vozprueba.Certificado(t)
	c, r := b.Enlazar(cert, versionIris)
	if _, err := c.Salud(t.Context()); err != nil {
		t.Fatal(err)
	}

	// El rechazo real del certificado de cliente: la puerta lo corta con una alerta TLS que llega
	// después del saludo (cifrada, con el servidor ya autenticado por su huella). Cuenta.
	otro := b.Cliente(vozprueba.Certificado(t), r.Token, versionIris, r.HuellaServidor)
	_, err := otro.Salud(t.Context())
	var rt *voz.ErrorRechazoTLS
	if !errors.As(err, &rt) || !voz.RechazoExplicito(err) {
		t.Fatalf("el rechazo del certificado de cliente debía ser un rechazo explícito: %T %v", err, err)
	}
	// Un token que el servidor no conoce: 401 por la firma. Cuenta.
	token, _ := voz.NuevoToken()
	_, err = b.Cliente(cert, token, versionIris, r.HuellaServidor).Salud(t.Context())
	if !voz.RechazoExplicito(err) {
		t.Fatalf("un 401 por la firma debía ser un rechazo explícito: %v", err)
	}

	// Alguien en el camino contesta al ClientHello con una alerta TLS en claro («bad certificate»),
	// sin tener ningún certificado. Se ve igual que un rechazo, pero llega durante el saludo: no cuenta.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	go func() {
		for {
			con, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = con.Close() }()
				cabecera := make([]byte, 5)
				if _, err := io.ReadFull(con, cabecera); err != nil {
					return
				}
				_, _ = io.CopyN(io.Discard, con, int64(cabecera[3])<<8|int64(cabecera[4]))
				// Registro de alerta (21), TLS 1.2 en el registro, 2 bytes: fatal (2), bad_certificate (42).
				_, _ = con.Write([]byte{21, 3, 3, 0, 2, 2, 42})
			}()
		}
	}()
	falso, err := voz.NuevoCliente(voz.OpcionesCliente{URL: "https://" + l.Addr().String(), Huellas: []string{r.HuellaServidor}, Cert: cert,
		Token: r.Token, VersionIris: versionIris})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(falso.Cerrar)
	_, err = falso.Salud(t.Context())
	var op *net.OpError
	if err == nil || !errors.As(err, &op) || op.Op != "remote error" {
		t.Fatalf("el servidor falso debía cortar con una alerta TLS: %T %v", err, err)
	}
	if errors.As(err, &rt) || voz.RechazoExplicito(err) {
		t.Fatalf("una alerta durante el saludo (falsificable) no es un rechazo explícito: %v", err)
	}

	// Y lo que no es una respuesta del servidor tampoco: tiempo agotado, puerto cerrado, otros errores.
	_ = l.Close()
	_, errCerrado := falso.Salud(t.Context())
	for nombre, e := range map[string]error{"tiempo agotado": context.DeadlineExceeded, "puerto cerrado": errCerrado,
		"reloj desfasado": &voz.ErrorPuerta{Estado: 401, Codigo: "reloj_desfasado"}, "error del servidor": &voz.ErrorPuerta{Estado: 500, Codigo: "error"},
		"huella del servidor distinta": voz.ErrHuella, "ninguno": nil} {
		if voz.RechazoExplicito(e) {
			t.Fatalf("%s no es un rechazo explícito: %v", nombre, e)
		}
	}
}

// sinClave «firma» con una clave que no tiene: lo que le pasa a quien presenta un certificado ajeno.
type sinClave struct{ publica crypto.PublicKey }

func (s sinClave) Public() crypto.PublicKey { return s.publica }
func (sinClave) Sign(io.Reader, []byte, crypto.SignerOpts) ([]byte, error) {
	return nil, errors.New("no tengo la clave de este certificado")
}

// impostor es un servidor TLS que presenta el certificado (público) de la puerta sin tener su clave:
// manda el Certificate y, en lugar de la firma (CertificateVerify), una alerta fatal.
func impostor(t *testing.T, b *vozprueba.Banco) *tls.Config {
	t.Helper()
	// El certificado de la puerta lo ve cualquiera que se conecte.
	con, err := tls.Dial("tcp", strings.TrimPrefix(b.URL, "https://"), &tls.Config{InsecureSkipVerify: true,
		Certificates: []tls.Certificate{vozprueba.Certificado(t)}})
	if err != nil {
		t.Fatal(err)
	}
	hoja := con.ConnectionState().PeerCertificates[0]
	_ = con.Close()
	if voz.Huella(hoja.Raw) != b.Huella {
		t.Fatal("no se copió el certificado de la puerta")
	}
	return &tls.Config{MinVersion: tls.VersionTLS13, ClientAuth: tls.RequireAnyClientCert,
		Certificates: []tls.Certificate{{Certificate: [][]byte{hoja.Raw}, PrivateKey: sinClave{publica: hoja.PublicKey}, Leaf: hoja}}}
}

// N7: quien está en el camino presenta el certificado fijado (es público) sin tener su clave y corta
// con una alerta antes de firmar. El saludo no terminó: no es un rechazo explícito.
func TestRechazoExplicitoConCertificadoAjeno(t *testing.T) {
	b := vozprueba.NuevoBanco(t, vozprueba.Opciones{})
	cert := vozprueba.Certificado(t)
	_, r := b.Enlazar(cert, versionIris)
	l, err := tls.Listen("tcp", "127.0.0.1:0", impostor(t, b))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	go func() {
		for {
			con, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				_ = con.(*tls.Conn).HandshakeContext(context.Background())
				_ = con.Close()
			}()
		}
	}()
	c, err := voz.NuevoCliente(voz.OpcionesCliente{URL: "https://" + l.Addr().String(), Huellas: []string{r.HuellaServidor}, Cert: cert,
		Token: r.Token, VersionIris: versionIris})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Cerrar)
	_, err = c.Salud(t.Context())
	var op *net.OpError
	if err == nil || !errors.As(err, &op) || op.Op != "remote error" {
		t.Fatalf("el impostor debía cortar con una alerta TLS tras presentar el certificado: %T %v", err, err)
	}
	// Iris sí vio la huella fijada en ese saludo (el certificado es el de verdad)…
	if c.HuellaVista() != b.Huella {
		t.Fatalf("el impostor debía presentar el certificado fijado: %s", c.HuellaVista())
	}
	// …pero el servidor nunca probó tener la clave: no es un rechazo.
	var rt *voz.ErrorRechazoTLS
	if errors.As(err, &rt) || voz.RechazoExplicito(err) {
		t.Fatalf("una alerta antes de la firma del servidor no es un rechazo explícito: %v", err)
	}
}

// El estado del saludo va por conexión: con el mismo cliente, una petición que recibe el rechazo
// real de la puerta y otra, a la vez, que recibe la alerta del impostor no se contaminan.
func TestRechazoExplicitoPorConexion(t *testing.T) {
	b := vozprueba.NuevoBanco(t, vozprueba.Opciones{})
	_, r := b.Enlazar(vozprueba.Certificado(t), versionIris)
	falso := impostor(t, b)
	// Un punto de entrada que manda las conexiones impares a la puerta de verdad y las pares al impostor.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	var turno atomic.Int64
	go func() {
		for {
			con, err := l.Accept()
			if err != nil {
				return
			}
			if turno.Add(1)%2 == 1 {
				go func() {
					defer func() { _ = con.Close() }()
					real, err := net.Dial("tcp", strings.TrimPrefix(b.URL, "https://"))
					if err != nil {
						return
					}
					defer func() { _ = real.Close() }()
					go func() { _, _ = io.Copy(real, con) }()
					_, _ = io.Copy(con, real)
				}()
				continue
			}
			go func() {
				s := tls.Server(con, falso)
				_ = s.HandshakeContext(context.Background())
				_ = con.Close()
			}()
		}
	}()
	for ronda := range 5 {
		// Un certificado de cliente que la puerta no conoce: la de verdad lo rechaza tras el saludo.
		c, err := voz.NuevoCliente(voz.OpcionesCliente{URL: "https://" + l.Addr().String(), Huellas: []string{r.HuellaServidor},
			Cert: vozprueba.Certificado(t), Token: r.Token, VersionIris: versionIris})
		if err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		res := make([]error, 2)
		for i := range res {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, res[i] = c.Salud(t.Context())
			}()
		}
		wg.Wait()
		c.Cerrar()
		explicitos := 0
		for _, e := range res {
			if e == nil {
				t.Fatal("ninguna de las dos debía pasar")
			}
			if voz.RechazoExplicito(e) {
				explicitos++
			}
		}
		if explicitos != 1 {
			t.Fatalf("ronda %d: una (la de la puerta de verdad) y solo una debía ser rechazo explícito: %v | %v", ronda, res[0], res[1])
		}
	}
}
