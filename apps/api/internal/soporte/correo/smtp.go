package correo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"strings"
	"time"
)

// Saliente es una respuesta a enviar.
type Saliente struct {
	De          mail.Address
	Para        mail.Address
	Asunto      string
	MessageID   string // sin <>
	InReplyTo   string // sin <>; vacío si no hay
	Referencias []string
	Texto       string
	Fecha       time.Time
}

// Componer arma el mensaje RFC 5322 (texto plano UTF-8, quoted-printable). Las cabeceras se
// codifican (RFC 2047) y no admiten saltos de línea: no hay inyección de cabeceras. No lleva
// Auto-Submitted: la respuesta la escribió un agente (RFC 3834 la reserva a lo automático, y
// con ella el cliente de correo del destinatario la trataría como autorespuesta).
func Componer(s Saliente) []byte {
	var b bytes.Buffer
	cab := func(k, v string) { b.WriteString(k + ": " + v + "\r\n") }
	cab("From", direccion(s.De))
	cab("To", direccion(s.Para))
	cab("Subject", mime.QEncoding.Encode("utf-8", limpiarLinea(s.Asunto)))
	cab("Date", s.Fecha.Format(time.RFC1123Z))
	cab("Message-ID", "<"+limpiarID(s.MessageID)+">")
	if s.InReplyTo != "" {
		cab("In-Reply-To", "<"+limpiarID(s.InReplyTo)+">")
	}
	if len(s.Referencias) > 0 {
		ids := make([]string, 0, len(s.Referencias))
		for _, r := range s.Referencias {
			if r = limpiarID(r); r != "" {
				ids = append(ids, "<"+r+">")
			}
		}
		cab("References", strings.Join(ids, " "))
	}
	cab("MIME-Version", "1.0")
	cab("Content-Type", "text/plain; charset=utf-8")
	cab("Content-Transfer-Encoding", "quoted-printable")
	b.WriteString("\r\n")
	qp := quotedprintable.NewWriter(&b)
	_, _ = qp.Write([]byte(strings.ReplaceAll(strings.ReplaceAll(s.Texto, "\r\n", "\n"), "\n", "\r\n")))
	_ = qp.Close()
	return b.Bytes()
}

func direccion(a mail.Address) string {
	a.Name = limpiarLinea(a.Name)
	a.Address = limpiarLinea(a.Address)
	return a.String()
}

func limpiarID(id string) string {
	return strings.Trim(strings.Map(func(r rune) rune {
		if r <= ' ' || r == '<' || r == '>' || r > '~' {
			return -1
		}
		return r
	}, id), " ")
}

// Enviar entrega el mensaje por SMTP con TLS implícito o STARTTLS obligatorio (si el servidor
// no lo ofrece, se corta antes de AUTH y de enviar nada). Con usuario, AUTH PLAIN (ya sobre TLS).
func Enviar(ctx context.Context, op Opciones, srv Servidor, s Saliente) error {
	c, err := abrirSMTP(ctx, op, srv)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()
	if err := c.Mail(s.De.Address); err != nil {
		return nuevoError("protocolo", "El servidor rechazó el remitente", err)
	}
	if err := c.Rcpt(s.Para.Address); err != nil {
		return nuevoError("protocolo", "El servidor rechazó el destinatario", err)
	}
	w, err := c.Data()
	if err != nil {
		return nuevoError("protocolo", "El servidor rechazó el mensaje", err)
	}
	if _, err := w.Write(Componer(s)); err != nil {
		return Resumir(err)
	}
	if err := w.Close(); err != nil {
		return nuevoError("protocolo", "El servidor rechazó el mensaje", err)
	}
	return c.Quit()
}

// ProbarSMTP conecta, negocia TLS y se autentica (sin enviar nada).
func ProbarSMTP(ctx context.Context, op Opciones, srv Servidor) error {
	c, err := abrirSMTP(ctx, op, srv)
	if err != nil {
		return err
	}
	_ = c.Quit()
	return nil
}

func abrirSMTP(ctx context.Context, op Opciones, srv Servidor) (*smtp.Client, error) {
	conn, err := op.conectar(ctx, srv)
	if err != nil {
		return nil, Resumir(err)
	}
	c, err := smtp.NewClient(conn, srv.Host)
	if err != nil {
		_ = conn.Close()
		return nil, Resumir(err)
	}
	if err := c.Hello("iris"); err != nil {
		_ = c.Close()
		return nil, Resumir(err)
	}
	if srv.TLS == TLSStartTLS {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			_ = c.Close()
			return nil, Resumir(ErrSinTLS)
		}
		if err := c.StartTLS(op.tlsConfig(srv.Host)); err != nil {
			_ = c.Close()
			return nil, Resumir(err)
		}
	}
	if srv.Usuario != "" {
		if err := c.Auth(smtp.PlainAuth("", srv.Usuario, srv.Contrasena, srv.Host)); err != nil {
			_ = c.Close()
			var tp *textproto.Error
			if errors.As(err, &tp) {
				return nil, nuevoError("autenticacion", "El servidor rechazó el usuario o la contraseña", err)
			}
			return nil, Resumir(fmt.Errorf("smtp auth: %w", err))
		}
	}
	return c, nil
}
