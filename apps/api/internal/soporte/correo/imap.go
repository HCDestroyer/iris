package correo

import (
	"context"
	"errors"
	"net"
	"slices"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

// SesionIMAP es una conexión autenticada (siempre sobre TLS) con una carpeta seleccionada.
type SesionIMAP struct {
	c       *imapclient.Client
	conn    net.Conn
	timeout time.Duration
	ctx     context.Context
	parar   func() bool // deja de vigilar ctx
}

// Carpeta es el estado de la carpeta seleccionada.
type Carpeta struct {
	UIDValidity uint32
	UIDNext     uint32
}

// AbrirIMAP conecta (TLS implícito o STARTTLS obligatorio), inicia sesión y selecciona la
// carpeta. Los errores vienen resumidos (Resumir): nunca llevan la contraseña.
func AbrirIMAP(ctx context.Context, op Opciones, s Servidor, carpeta string) (*SesionIMAP, Carpeta, error) {
	conn, err := op.conectar(ctx, s)
	if err != nil {
		return nil, Carpeta{}, Resumir(err)
	}
	opciones := &imapclient.Options{TLSConfig: op.tlsConfig(s.Host)}
	var c *imapclient.Client
	if s.TLS == TLSStartTLS {
		// STARTTLS obligatorio: si el servidor no lo acepta, se corta sin mandar LOGIN (las
		// credenciales nunca viajan en claro).
		if c, err = imapclient.NewStartTLS(conn, opciones); err != nil {
			_ = conn.Close()
			var rechazo *imap.Error
			if errors.As(err, &rechazo) {
				return nil, Carpeta{}, Resumir(ErrSinTLS)
			}
			return nil, Carpeta{}, Resumir(err)
		}
	} else {
		c = imapclient.New(conn, opciones)
	}
	ses := &SesionIMAP{c: c, conn: conn, timeout: op.timeout(), ctx: ctx}
	// Si ctx se cancela, la conexión se cierra (el cliente IMAP no recibe ctx).
	ses.parar = context.AfterFunc(ctx, func() { _ = conn.Close() })
	if err := c.WaitGreeting(); err != nil {
		ses.Cerrar()
		return nil, Carpeta{}, resumirCtx(ctx, err)
	}
	if err := c.Login(s.Usuario, s.Contrasena).Wait(); err != nil {
		ses.Cerrar()
		if ctx.Err() != nil {
			return nil, Carpeta{}, resumirCtx(ctx, err)
		}
		return nil, Carpeta{}, nuevoError("autenticacion", "El servidor rechazó el usuario o la contraseña", err)
	}
	ses.plazo()
	sel, err := c.Select(carpeta, nil).Wait()
	if err != nil {
		ses.Cerrar()
		if ctx.Err() != nil {
			return nil, Carpeta{}, resumirCtx(ctx, err)
		}
		return nil, Carpeta{}, nuevoError("carpeta", "No se pudo abrir la carpeta "+carpeta, err)
	}
	return ses, Carpeta{UIDValidity: sel.UIDValidity, UIDNext: uint32(sel.UIDNext)}, nil
}

// resumirCtx: si el plazo (o la cancelación) de ctx cortó la conexión, es un error de conexión
// por tiempo agotado; si no, Resumir.
func resumirCtx(ctx context.Context, err error) *Error {
	if ctx.Err() != nil {
		return nuevoError("conexion", "No se pudo conectar con el servidor (rechazo o tiempo agotado)", err)
	}
	return Resumir(err)
}

// plazo renueva el tiempo máximo de la conexión para el siguiente comando, sin pasar el plazo
// de ctx (si lo tiene: p. ej. la prueba de un buzón tiene un plazo total).
func (s *SesionIMAP) plazo() {
	l := time.Now().Add(s.timeout)
	if d, ok := s.ctx.Deadline(); ok && d.Before(l) {
		l = d
	}
	_ = s.conn.SetDeadline(l)
}

// Cerrar termina la sesión (LOGOUT de cortesía).
func (s *SesionIMAP) Cerrar() {
	if s == nil || s.c == nil {
		return
	}
	if s.parar != nil {
		s.parar()
	}
	s.plazo()
	_ = s.c.Logout().Wait()
	_ = s.c.Close()
}

// UIDs devuelve, en orden, los UID mayores que desde; con soloNoLeidos, los que no tienen \Seen
// (primera lectura de un buzón: no se importa el historial).
func (s *SesionIMAP) UIDs(desde uint32, soloNoLeidos bool) ([]uint32, error) {
	s.plazo()
	criterio := &imap.SearchCriteria{}
	if soloNoLeidos {
		criterio.NotFlag = []imap.Flag{imap.FlagSeen}
	} else {
		criterio.UID = []imap.UIDSet{{imap.UIDRange{Start: imap.UID(desde + 1), Stop: 0}}}
	}
	datos, err := s.c.UIDSearch(criterio, nil).Wait()
	if err != nil {
		return nil, Resumir(err)
	}
	var salida []uint32
	for _, uid := range datos.AllUIDs() {
		// «n:*» siempre incluye el último aunque sea menor que n.
		if uint32(uid) > desde {
			salida = append(salida, uint32(uid))
		}
	}
	slices.Sort(salida)
	return salida, nil
}

// Traer descarga el mensaje sin marcarlo leído (BODY.PEEK). Si pesa más que max, solo trae
// las cabeceras y truncado = true.
func (s *SesionIMAP) Traer(uid uint32, maximo int64) (crudo []byte, truncado bool, err error) {
	s.plazo()
	set := imap.UIDSetNum(imap.UID(uid))
	tam, err := s.c.Fetch(set, &imap.FetchOptions{UID: true, RFC822Size: true}).Collect()
	if err != nil {
		return nil, false, Resumir(err)
	}
	if len(tam) == 0 {
		return nil, false, nil // borrado entre la búsqueda y la descarga
	}
	seccion := &imap.FetchItemBodySection{Peek: true}
	if tam[0].RFC822Size > maximo {
		seccion.Specifier, truncado = imap.PartSpecifierHeader, true
	}
	s.plazo()
	msgs, err := s.c.Fetch(set, &imap.FetchOptions{UID: true, BodySection: []*imap.FetchItemBodySection{seccion}}).Collect()
	if err != nil {
		return nil, false, Resumir(err)
	}
	if len(msgs) == 0 {
		return nil, false, nil
	}
	return msgs[0].FindBodySection(seccion), truncado, nil
}

// Marcar deja el mensaje como leído y, si hay carpeta de procesados, lo mueve ahí.
func (s *SesionIMAP) Marcar(uid uint32, carpetaProcesados string) error {
	s.plazo()
	set := imap.UIDSetNum(imap.UID(uid))
	if err := s.c.Store(set, &imap.StoreFlags{Op: imap.StoreFlagsAdd, Silent: true, Flags: []imap.Flag{imap.FlagSeen}}, nil).Close(); err != nil {
		return Resumir(err)
	}
	if carpetaProcesados != "" {
		s.plazo()
		if _, err := s.c.Move(set, carpetaProcesados).Wait(); err != nil {
			return nuevoError("carpeta", "No se pudo mover el correo a "+carpetaProcesados, err)
		}
	}
	return nil
}
