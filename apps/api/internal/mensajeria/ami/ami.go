// Package ami es un cliente mínimo del Asterisk Manager Interface (AMI) para la centralita Issabel
// de solo lectura (contratos/voz.md §3.12, E4a). El protocolo es texto: paquetes de líneas
// «Clave: valor» terminados por una línea vacía. Solo hace lo que Iris necesita en E4a: entrar
// (Login), Ping, ListCommands (para comprobar que el usuario no tenga permisos de escritura) y leer
// eventos. No sabe mandar Originate ni ninguna otra acción que cambie algo en la centralita.
//
// Seguridad:
//   - Solo TLS (AMI cifrado, 5039 de fábrica). El certificado de Issabel suele ser autofirmado: se
//     fija su huella SHA-256 (la confirma un administrador) y cualquier otra corta la conexión.
//   - Todo lo que llega es texto no confiable: líneas, campos y paquetes con tope de tamaño; un
//     paquete que se pasa corta la conexión.
//   - Nada de lo que se escribe en el socket viene sin validar: ningún valor puede llevar CR, LF ni
//     NUL (un salto de línea sería inyección de cabeceras AMI).
//   - El secreto nunca se registra en un log ni aparece en un error.
package ami

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// Límites de lo que se acepta del AMI.
const (
	// MaxLinea: bytes de una línea (sin el fin de línea).
	MaxLinea = 2048
	// MaxCampos: líneas de un paquete (ListCommands trae unas 200).
	MaxCampos = 600
	// MaxPaquete: bytes de un paquete.
	MaxPaquete = 64 << 10
	// ColaEventos: eventos leídos que esperan a quien los procesa; los que no caben se descartan.
	ColaEventos = 1000
)

var (
	// ErrConexion: no se pudo conectar, el TLS falló o la conexión se cortó (sin detalle: el mismo
	// error para todo, así «Probar» no sirve para escanear la red).
	ErrConexion = errors.New("ami: sin conexión")
	// ErrCredenciales: el AMI rechazó el usuario o la contraseña.
	ErrCredenciales = errors.New("ami: credenciales rechazadas")
	// ErrProtocolo: lo que llegó no es AMI o se pasa de los límites.
	ErrProtocolo = errors.New("ami: respuesta no válida")
	// ErrValor: un valor a escribir lleva CR, LF o NUL.
	ErrValor = errors.New("ami: valor no permitido")
)

// ErrHuella: el certificado no es el fijado. Vista es la huella que presentó el servidor.
type ErrHuella struct{ Vista string }

func (e *ErrHuella) Error() string { return "ami: la huella del certificado cambió" }

// Paquete es un mensaje AMI: campos en orden. Las claves se comparan sin mayúsculas.
type Paquete struct {
	Campos [][2]string
}

// Valor devuelve el primer valor de la clave (o "").
func (p Paquete) Valor(clave string) string {
	for _, c := range p.Campos {
		if strings.EqualFold(c[0], clave) {
			return c[1]
		}
	}
	return ""
}

// Tiene indica si el paquete trae la clave.
func (p Paquete) Tiene(clave string) bool {
	for _, c := range p.Campos {
		if strings.EqualFold(c[0], clave) {
			return true
		}
	}
	return false
}

// Opciones de la conexión.
type Opciones struct {
	Host   string
	Puerto int
	// Usuario y Secreto del AMI. El secreto nunca sale en un log ni en un error.
	Usuario string
	Secreto string
	// Huella SHA-256 (hex) fijada. Vacía solo al probar por primera vez: se acepta y se informa.
	Huella string
	// Eventos: las clases pedidas al entrar ("call,cdr" para escuchar, "off" para probar).
	Eventos string
	// Plazo para conectar, el TLS y el Login.
	Plazo time.Duration
	// Control del marcador (reglas de red: sin loopback, link-local ni metadata).
	Control func(network, address string, c syscall.RawConn) error
	// TLS extra (pruebas: ninguno; la verificación siempre es por huella).
	ServerName string
}

// Conexion es una sesión AMI abierta y autenticada.
type Conexion struct {
	conn     net.Conn
	huella   string
	version  string
	escribir sync.Mutex
	eventos  chan Paquete
	pend     sync.Map // ActionID → chan Paquete
	sigID    atomic.Int64
	err      atomic.Pointer[error]
	cerrada  chan struct{}
	cerrar   sync.Once
	// Descartados: eventos que no cupieron en la cola.
	Descartados atomic.Int64
}

// Huella SHA-256 (hex) del certificado que presentó el servidor.
func (c *Conexion) Huella() string { return c.huella }

// Version del AMI que anunció la centralita (p. ej. "Asterisk Call Manager/5.0.1").
func (c *Conexion) Version() string { return c.version }

// Eventos que llegan del AMI (se cierra al cortarse la conexión).
func (c *Conexion) Eventos() <-chan Paquete { return c.eventos }

// Err: por qué se cortó (nil mientras sigue viva).
func (c *Conexion) Err() error {
	if e := c.err.Load(); e != nil {
		return *e
	}
	return nil
}

// Cerrada se cierra cuando la conexión termina.
func (c *Conexion) Cerrada() <-chan struct{} { return c.cerrada }

// Cerrar termina la sesión (intenta Logoff sin esperar).
func (c *Conexion) Cerrar() {
	c.cerrar.Do(func() {
		_ = c.conn.SetWriteDeadline(time.Now().Add(time.Second))
		_, _ = c.conn.Write([]byte("Action: Logoff\r\n\r\n"))
		_ = c.conn.Close()
	})
}

// HuellaDe calcula la huella SHA-256 (hex en minúsculas) de un certificado DER.
func HuellaDe(der []byte) string {
	s := sha256.Sum256(der)
	return hex.EncodeToString(s[:])
}

// ValorSeguro indica si v se puede escribir en el socket (sin CR, LF ni NUL).
func ValorSeguro(v string) bool {
	return !strings.ContainsAny(v, "\r\n\x00")
}

// Conectar abre TLS, verifica la huella, lee el saludo y entra con Login.
func Conectar(ctx context.Context, o Opciones) (*Conexion, error) {
	if !ValorSeguro(o.Usuario) || !ValorSeguro(o.Secreto) || !ValorSeguro(o.Eventos) || strings.Contains(o.Usuario, ":") {
		return nil, ErrValor
	}
	if o.Plazo <= 0 {
		o.Plazo = 10 * time.Second
	}
	if o.Eventos == "" {
		o.Eventos = "off"
	}
	ctx, cancelar := context.WithTimeout(ctx, o.Plazo)
	defer cancelar()
	d := &net.Dialer{Timeout: o.Plazo, Control: o.Control, KeepAlive: 30 * time.Second}
	raw, err := d.DialContext(ctx, "tcp", net.JoinHostPort(o.Host, strconv.Itoa(o.Puerto)))
	if err != nil {
		return nil, ErrConexion
	}
	var vista string
	cfg := &tls.Config{
		MinVersion: tls.VersionTLS12,
		ServerName: o.ServerName,
		// La verificación es por huella (VerifyConnection): Issabel trae certificados autofirmados.
		InsecureSkipVerify: true, //nolint:gosec // se verifica la huella fijada en VerifyConnection
		VerifyConnection: func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 {
				return ErrConexion
			}
			vista = HuellaDe(cs.PeerCertificates[0].Raw)
			if o.Huella != "" && !strings.EqualFold(vista, o.Huella) {
				return &ErrHuella{Vista: vista}
			}
			return nil
		},
	}
	tc := tls.Client(raw, cfg)
	if err := tc.HandshakeContext(ctx); err != nil {
		_ = raw.Close()
		var eh *ErrHuella
		if errors.As(err, &eh) {
			return nil, eh
		}
		if o.Huella != "" && vista != "" && !strings.EqualFold(vista, o.Huella) {
			return nil, &ErrHuella{Vista: vista}
		}
		return nil, ErrConexion
	}
	if dl, ok := ctx.Deadline(); ok {
		_ = tc.SetDeadline(dl)
	}
	r := bufio.NewReaderSize(tc, 4096)
	saludo, err := leerLinea(r)
	if err != nil || !strings.HasPrefix(saludo, "Asterisk Call Manager/") {
		_ = tc.Close()
		return nil, ErrProtocolo
	}
	c := &Conexion{conn: tc, huella: vista, version: saludo, eventos: make(chan Paquete, ColaEventos), cerrada: make(chan struct{})}
	// Login sincrónico (aún sin lector): la respuesta es el siguiente paquete con su ActionID.
	id := "iris-login"
	if err := c.escribirAccion("Login", id, [][2]string{{"Username", o.Usuario}, {"Secret", o.Secreto}, {"Events", o.Eventos}}); err != nil {
		_ = tc.Close()
		return nil, ErrConexion
	}
	for {
		p, err := leerPaquete(r)
		if err != nil {
			_ = tc.Close()
			if errors.Is(err, ErrProtocolo) {
				return nil, ErrProtocolo
			}
			return nil, ErrConexion
		}
		if p.Valor("ActionID") != id {
			continue // un evento que se adelantó: se ignora
		}
		if !strings.EqualFold(p.Valor("Response"), "Success") {
			_ = tc.Close()
			return nil, ErrCredenciales
		}
		break
	}
	_ = tc.SetDeadline(time.Time{})
	go c.leer(r)
	return c, nil
}

func (c *Conexion) escribirAccion(accion, id string, campos [][2]string) error {
	var b bytes.Buffer
	escribirCampo := func(k, v string) error {
		if !ValorSeguro(k) || !ValorSeguro(v) || strings.Contains(k, ":") {
			return ErrValor
		}
		b.WriteString(k)
		b.WriteString(": ")
		b.WriteString(v)
		b.WriteString("\r\n")
		return nil
	}
	if err := escribirCampo("Action", accion); err != nil {
		return err
	}
	if err := escribirCampo("ActionID", id); err != nil {
		return err
	}
	for _, kv := range campos {
		if err := escribirCampo(kv[0], kv[1]); err != nil {
			return err
		}
	}
	b.WriteString("\r\n")
	c.escribir.Lock()
	defer c.escribir.Unlock()
	_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	_, err := c.conn.Write(b.Bytes())
	return err
}

// Accion manda una acción de lectura y espera su respuesta. Solo las de esta lista: el cliente no
// sabe mandar acciones que cambien algo en la centralita.
func (c *Conexion) Accion(ctx context.Context, accion string) (Paquete, error) {
	switch accion {
	case "Ping", "ListCommands", "CoreSettings":
	default:
		return Paquete{}, ErrValor
	}
	id := "iris-" + strconv.FormatInt(c.sigID.Add(1), 10)
	ch := make(chan Paquete, 1)
	c.pend.Store(id, ch)
	defer c.pend.Delete(id)
	if err := c.escribirAccion(accion, id, nil); err != nil {
		if errors.Is(err, ErrValor) {
			return Paquete{}, err
		}
		return Paquete{}, ErrConexion
	}
	select {
	case p := <-ch:
		return p, nil
	case <-c.cerrada:
		return Paquete{}, ErrConexion
	case <-ctx.Done():
		return Paquete{}, ErrConexion
	}
}

func (c *Conexion) leer(r *bufio.Reader) {
	defer func() {
		close(c.eventos)
		c.cerrar.Do(func() { _ = c.conn.Close() })
		close(c.cerrada)
	}()
	for {
		p, err := leerPaquete(r)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
				err = ErrConexion
			} else if !errors.Is(err, ErrProtocolo) {
				err = ErrConexion
			}
			c.err.Store(&err)
			return
		}
		if id := p.Valor("ActionID"); id != "" && p.Tiene("Response") {
			if v, ok := c.pend.Load(id); ok {
				select {
				case v.(chan Paquete) <- p:
				default:
				}
			}
			continue
		}
		if !p.Tiene("Event") {
			continue
		}
		select {
		case c.eventos <- p:
		default:
			c.Descartados.Add(1)
		}
	}
}

// leerLinea lee una línea sin su fin (CRLF o LF) con tope de MaxLinea.
func leerLinea(r *bufio.Reader) (string, error) {
	var b []byte
	for {
		frag, err := r.ReadSlice('\n')
		b = append(b, frag...)
		if len(b) > MaxLinea+2 {
			return "", ErrProtocolo
		}
		if err == nil {
			break
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		return "", err
	}
	s := strings.TrimRight(string(b), "\r\n")
	if strings.ContainsRune(s, 0) {
		return "", ErrProtocolo
	}
	return s, nil
}

// leerPaquete lee hasta la línea vacía, con los topes de campos y tamaño.
func leerPaquete(r *bufio.Reader) (Paquete, error) {
	var p Paquete
	total := 0
	for {
		l, err := leerLinea(r)
		if err != nil {
			return p, err
		}
		if l == "" {
			if len(p.Campos) == 0 {
				continue // líneas vacías sueltas
			}
			return p, nil
		}
		total += len(l)
		if total > MaxPaquete || len(p.Campos) >= MaxCampos {
			return p, ErrProtocolo
		}
		k, v, ok := strings.Cut(l, ":")
		if !ok {
			// ListCommands en versiones viejas o salida de comandos: se ignora la línea.
			continue
		}
		p.Campos = append(p.Campos, [2]string{strings.TrimSpace(k), strings.TrimSpace(v)})
	}
}

// ClasesDeEscritura son las clases de permiso del AMI (`write=` de manager.conf) que dejan cambiar
// algo en la centralita: marcar, colgar, transferir, ejecutar comandos de consola, tocar la
// configuración, colas y agentes, mandar DTMF, eventos de usuario, AGI o mensajes. ListCommands
// anota en cada acción las clases que la habilitan («(Priv: originate,all)»); una acción con
// cualquiera de estas clases es permiso de más para E4a. El usuario de Iris debe tener `write=` vacío:
// así ListCommands solo trae acciones sin clase («(Priv: <none>)»: Ping, Logoff, ListCommands…).
var ClasesDeEscritura = []string{"system", "call", "originate", "config", "command", "agent", "user", "dtmf", "agi", "message"}

// AccionesDeEscritura: segunda red para AMI que no anotan las clases en ListCommands (versiones
// viejas). Si ListCommands devuelve cualquiera de ellas, el usuario tiene permisos de más.
var AccionesDeEscritura = []string{
	"Originate", "Redirect", "Hangup", "Command", "Atxfer", "BlindTransfer", "Bridge", "Park", "Setvar",
	"UpdateConfig", "CreateConfig", "ModuleLoad", "Reload", "QueueAdd", "QueueRemove", "QueuePause",
	"PlayDTMF", "Monitor", "StopMonitor", "MixMonitor", "StopMixMonitor", "SendText", "UserEvent", "DBPut", "DBDel", "DBDelTree",
	"AbsoluteTimeout", "MuteAudio", "SIPnotify", "PJSIPNotify", "ConfbridgeKick", "MeetmeKick",
}

// clasesDeAccion lee las clases de la descripción de ListCommands («Texto.  (Priv: a,b,all)»). ok
// es false si la línea no las trae.
func clasesDeAccion(desc string) (clases []string, ok bool) {
	i := strings.LastIndex(strings.ToLower(desc), "(priv:")
	if i < 0 {
		return nil, false
	}
	resto := desc[i+len("(priv:"):]
	if j := strings.Index(resto, ")"); j >= 0 {
		resto = resto[:j]
	}
	for _, c := range strings.Split(resto, ",") {
		if c = strings.ToLower(strings.TrimSpace(c)); c != "" {
			clases = append(clases, c)
		}
	}
	return clases, true
}

// deEscritura: alguna clase de escritura, o solo «all» (no se sabe cuál: se toma por escritura).
func deEscritura(clases []string) bool {
	propias := 0
	for _, c := range clases {
		if c == "all" || c == "<none>" || c == "none" {
			continue
		}
		propias++
		if slices.Contains(ClasesDeEscritura, c) {
			return true
		}
	}
	return propias == 0 && slices.Contains(clases, "all")
}

// PermisosDeMas revisa la respuesta de ListCommands (Asterisk solo lista las acciones que el `write=`
// del usuario permite, más las que no exigen clase): devuelve las acciones que el usuario puede
// ejecutar y que cambian algo, por su clase (Priv) o, si la línea no la trae, por la lista de
// AccionesDeEscritura.
func PermisosDeMas(p Paquete) []string {
	var out []string
	vistas := map[string]bool{}
	for _, c := range p.Campos {
		accion := c[0]
		if strings.EqualFold(accion, "Response") || strings.EqualFold(accion, "ActionID") || strings.EqualFold(accion, "Message") || strings.EqualFold(accion, "EventList") {
			continue
		}
		clases, ok := clasesDeAccion(c[1])
		escritura := ok && deEscritura(clases)
		if !escritura {
			for _, a := range AccionesDeEscritura {
				if strings.EqualFold(accion, a) {
					escritura = true
					break
				}
			}
		}
		if escritura && !vistas[strings.ToLower(accion)] {
			vistas[strings.ToLower(accion)] = true
			out = append(out, accion)
		}
	}
	return out
}

// Resumen para los logs (nunca el secreto).
func (o Opciones) String() string {
	return fmt.Sprintf("ami(%s:%d usuario=%s)", o.Host, o.Puerto, o.Usuario)
}
