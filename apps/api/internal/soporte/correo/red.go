// Package correo es el protocolo de la entrada y salida de tickets por correo: cliente IMAP
// (lectura por UID), cliente SMTP (respuestas), lectura MIME a texto seguro y la red con
// protección SSRF. No sabe de tickets ni de la base: eso vive en internal/soporte.
//
// Reglas de seguridad:
//   - Solo TLS: IMAPS/SMTPS implícito o STARTTLS obligatorio (si el servidor no lo ofrece, se
//     corta antes de mandar credenciales). Nunca en claro. Certificado verificado siempre.
//   - SSRF: el host lo escribe un usuario del panel. Toda conexión pasa por un net.Dialer cuyo
//     Control mira la IP ya resuelta (argos.DestinoPermitido: loopback, privadas, enlace local,
//     CGNAT, reservadas…) salvo las redes de IRIS_CORREO_REDES_PERMITIDAS.
//   - La contraseña nunca sale en errores ni logs: los errores se resumen (Resumir).
package correo

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
)

// Modos de TLS. No hay modo «sin TLS».
const (
	TLSImplicito = "tls"
	TLSStartTLS  = "starttls"
)

// ErrDestinoBloqueado: el servidor resuelve a una red interna no permitida.
var ErrDestinoBloqueado = errors.New("correo: destino no permitido (red interna)")

// ErrSinTLS: el servidor no ofrece STARTTLS (no se sigue en claro).
var ErrSinTLS = errors.New("correo: el servidor no ofrece STARTTLS")

// Opciones de red comunes a IMAP y SMTP.
type Opciones struct {
	// RedesPermitidas (IRIS_CORREO_REDES_PERMITIDAS): redes internas a las que sí se puede
	// conectar (desarrollo y laboratorio). Vacía = solo IPs públicas.
	RedesPermitidas []netip.Prefix
	// Raices: autoridades de certificación además de las del sistema (nil = las del sistema).
	Raices *x509.CertPool
	// Timeout de cada conexión completa (30 s por defecto).
	Timeout time.Duration
}

func (o Opciones) timeout() time.Duration {
	if o.Timeout <= 0 {
		return 30 * time.Second
	}
	return o.Timeout
}

// Servidor es el destino de una conexión.
type Servidor struct {
	Host       string
	Puerto     int
	TLS        string // tls | starttls
	Usuario    string
	Contrasena string `json:"-"`
}

func (s Servidor) direccion() string { return net.JoinHostPort(s.Host, strconv.Itoa(s.Puerto)) }

// HostPermitido valida un host escrito: si es una IP literal, debe estar permitida (los nombres
// se revisan al conectar, tras resolver el DNS).
func HostPermitido(host string, permitidas []netip.Prefix) bool {
	if ip, err := netip.ParseAddr(strings.Trim(host, "[]")); err == nil {
		return argos.DestinoPermitido(ip, permitidas)
	}
	return true
}

func (o Opciones) dialer() *net.Dialer {
	return &net.Dialer{
		Timeout: 10 * time.Second,
		Control: func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return ErrDestinoBloqueado
			}
			ip, err := netip.ParseAddr(host)
			if err != nil || !argos.DestinoPermitido(ip, o.RedesPermitidas) {
				return fmt.Errorf("%w (%s)", ErrDestinoBloqueado, host)
			}
			return nil
		},
	}
}

func (o Opciones) tlsConfig(host string) *tls.Config {
	return &tls.Config{ServerName: host, RootCAs: o.Raices, MinVersion: tls.VersionTLS12}
}

// conectar abre la conexión TCP (con la protección SSRF) y la deja con un plazo total. Con TLS
// implícito, hace el apretón de manos.
func (o Opciones) conectar(ctx context.Context, s Servidor) (net.Conn, error) {
	ctx, cancelar := context.WithTimeout(ctx, o.timeout())
	defer cancelar()
	conn, err := o.dialer().DialContext(ctx, "tcp", s.direccion())
	if err != nil {
		return nil, err
	}
	if err := conn.SetDeadline(time.Now().Add(o.timeout())); err != nil {
		_ = conn.Close()
		return nil, err
	}
	switch s.TLS {
	case TLSImplicito:
		t := tls.Client(conn, o.tlsConfig(s.Host))
		if err := t.HandshakeContext(ctx); err != nil {
			_ = conn.Close()
			return nil, err
		}
		return t, nil
	case TLSStartTLS:
		return conn, nil
	}
	_ = conn.Close()
	return nil, fmt.Errorf("correo: modo TLS desconocido %q", s.TLS)
}

// Error resumido para guardar o mostrar: nunca lleva credenciales ni la respuesta cruda.
type Error struct {
	Codigo  string // destino_no_permitido, dns, conexion, tls, sin_tls, autenticacion, carpeta, protocolo
	Detalle string
	causa   error
}

func (e *Error) Error() string { return e.Codigo + ": " + e.Detalle }
func (e *Error) Unwrap() error { return e.causa }

func nuevoError(codigo, detalle string, causa error) *Error {
	return &Error{Codigo: codigo, Detalle: detalle, causa: causa}
}

// Resumir traduce un error de red o de protocolo a un Error sin datos sensibles.
func Resumir(err error) *Error {
	if err == nil {
		return nil
	}
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	var dns *net.DNSError
	var cert *tls.CertificateVerificationError
	var host x509.HostnameError
	var ua x509.UnknownAuthorityError
	var rec tls.RecordHeaderError
	switch {
	case errors.Is(err, ErrDestinoBloqueado):
		return nuevoError("destino_no_permitido", "El servidor está en una red interna no permitida", err)
	case errors.Is(err, ErrSinTLS):
		return nuevoError("sin_tls", "El servidor no ofrece STARTTLS; Iris no envía credenciales en claro", err)
	case errors.As(err, &dns):
		return nuevoError("dns", "No se pudo resolver el nombre del servidor", err)
	case errors.As(err, &cert), errors.As(err, &host), errors.As(err, &ua), errors.As(err, &rec):
		return nuevoError("tls", "No se pudo establecer TLS con el servidor (certificado o protocolo)", err)
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, syscall.ECONNREFUSED), isTimeout(err):
		return nuevoError("conexion", "No se pudo conectar con el servidor (rechazo o tiempo agotado)", err)
	}
	return nuevoError("protocolo", "El servidor respondió con un error", err)
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}
