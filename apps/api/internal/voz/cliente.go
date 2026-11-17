package voz

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"sync"
	"syscall"
	"time"
)

// ErrHuella: el servidor presentó un certificado que no es el fijado.
var ErrHuella = errors.New("voz: el certificado del servidor no es el fijado")

// ErrFirmaRespuesta: la respuesta no viene firmada por quien tiene el token.
var ErrFirmaRespuesta = errors.New("voz: la respuesta del servidor no está firmada")

// ErrorPuerta es una respuesta de error de la puerta.
type ErrorPuerta struct {
	Estado       int
	Codigo       string
	Detalle      string
	HoraServidor int64
}

func (e *ErrorPuerta) Error() string { return fmt.Sprintf("voz: %d %s", e.Estado, e.Codigo) }

// OpcionesCliente es la conexión de Iris con un servidor ya enlazado.
type OpcionesCliente struct {
	// URL base: https://host:puerto (sin ruta).
	URL string
	// Huellas del certificado del servidor que se aceptan (la vigente y, en una renovación, la siguiente).
	Huellas []string
	// Cert: certificado de cliente de Iris (mTLS).
	Cert tls.Certificate
	// Token de servicio (secreto de la firma).
	Token []byte
	// VersionIris: va en X-Iris-Version, dentro de la cadena firmada.
	VersionIris string
	// Control: filtro de direcciones al conectar (las reglas de URL de Iris).
	Control func(red, direccion string, c syscall.RawConn) error
	Ahora   func() time.Time
}

// Cliente llama a la puerta: fija la huella del servidor, presenta su certificado, firma cada
// petición y exige la firma de la respuesta.
type Cliente struct {
	o     OpcionesCliente
	http  *http.Client
	mu    sync.Mutex
	vista string
}

func validarBase(crudo string) error {
	u, err := url.Parse(crudo)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("voz: la dirección del servidor debe ser https://servidor:puerto")
	}
	return nil
}

func marcador(control func(string, string, syscall.RawConn) error) *net.Dialer {
	return &net.Dialer{Timeout: 5 * time.Second, Control: control}
}

// NuevoCliente arma el cliente de un servidor enlazado.
func NuevoCliente(o OpcionesCliente) (*Cliente, error) {
	if err := validarBase(o.URL); err != nil {
		return nil, err
	}
	if len(o.Huellas) == 0 || len(o.Token) != LargoToken || !VersionValida(o.VersionIris) {
		return nil, errors.New("voz: faltan la huella, el token o la versión")
	}
	if o.Ahora == nil {
		o.Ahora = time.Now
	}
	c := &Cliente{o: o}
	cfg := &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{o.Cert},
		// No hay CA: la confianza es la huella fijada en el emparejamiento, comprobada abajo.
		InsecureSkipVerify: true, //nolint:gosec // se fija la huella en VerifyConnection
		VerifyConnection: func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 {
				return ErrHuella
			}
			h := Huella(cs.PeerCertificates[0].Raw)
			if !slices.Contains(o.Huellas, h) {
				return ErrHuella
			}
			c.mu.Lock()
			c.vista = h
			c.mu.Unlock()
			return nil
		},
	}
	c.http = &http.Client{
		Transport: &http.Transport{Proxy: nil, TLSClientConfig: cfg, DialContext: marcador(o.Control).DialContext,
			TLSHandshakeTimeout: 5 * time.Second, MaxIdleConns: 4, IdleConnTimeout: 60 * time.Second, ForceAttemptHTTP2: false},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	return c, nil
}

// Cerrar suelta las conexiones.
func (c *Cliente) Cerrar() { c.http.CloseIdleConnections() }

// HuellaVista: la huella del certificado que el servidor presentó en el último saludo.
func (c *Cliente) HuellaVista() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.vista
}

// Hacer manda una petición firmada y devuelve el cuerpo de una respuesta 2xx con la firma
// comprobada. tope es el máximo de bytes que se leen de la respuesta. Un error de la puerta sale
// como *ErrorPuerta.
func (c *Cliente) Hacer(ctx context.Context, metodo, ruta string, cuerpo []byte, tope int64) ([]byte, error) {
	nonce, err := NuevoNonce()
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, metodo, c.o.URL+ruta, bytes.NewReader(cuerpo))
	if err != nil {
		return nil, err
	}
	marca := strconv.FormatInt(c.o.Ahora().Unix(), 10)
	req.Header.Set(CabMarca, marca)
	req.Header.Set(CabNonce, nonce)
	req.Header.Set(CabVersion, c.o.VersionIris)
	req.Header.Set(CabFirma, FirmaPeticion(c.o.Token, metodo, ruta, marca, nonce, c.o.VersionIris, cuerpo))
	if len(cuerpo) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(io.LimitReader(resp.Body, tope+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > tope {
		return nil, &ErrorPuerta{Estado: resp.StatusCode, Codigo: "respuesta_demasiado_grande"}
	}
	firmada := Iguales(resp.Header.Get(CabFirmaRespuesta), FirmaRespuesta(c.o.Token, nonce, resp.StatusCode, b))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		e := &ErrorPuerta{Estado: resp.StatusCode, Codigo: "error"}
		var p problema
		if json.Unmarshal(b, &p) == nil && p.Codigo != "" && len(p.Codigo) <= 64 {
			e.Codigo, e.HoraServidor = p.Codigo, p.HoraServidor
			if firmada && len(p.Detalle) <= 200 {
				e.Detalle = p.Detalle
			}
		}
		return nil, e
	}
	if !firmada {
		return nil, ErrFirmaRespuesta
	}
	return b, nil
}

func (c *Cliente) leer(ctx context.Context, ruta string, tope int64, destino any) error {
	b, err := c.Hacer(ctx, http.MethodGet, ruta, nil, tope)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, destino)
}

// Salud lee GET /salud.
func (c *Cliente) Salud(ctx context.Context) (Salud, error) {
	var s Salud
	return s, c.leer(ctx, RutaSalud, 64<<10, &s)
}

// Version lee GET /version.
func (c *Cliente) Version(ctx context.Context) (Version, error) {
	var v Version
	return v, c.leer(ctx, RutaVersion, 64<<10, &v)
}

// TopeMetricas: máximo de bytes que Iris lee de /metricas.
const TopeMetricas = 256 << 10

// Metricas lee GET /metricas. Quien la usa debe validar los valores: el servidor no es de confianza.
func (c *Cliente) Metricas(ctx context.Context) (Metricas, error) {
	var m Metricas
	return m, c.leer(ctx, RutaMetricas, TopeMetricas, &m)
}

// Credencial rota el token o el certificado de cliente, o confirma la huella siguiente.
func (c *Cliente) Credencial(ctx context.Context, p PeticionCredencial) error {
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	_, err = c.Hacer(ctx, http.MethodPost, RutaCredencial, b, 4<<10)
	return err
}

// OpcionesEnlace: lo que hace falta para el intercambio de enlace (voz.md §4.8, paso 4).
type OpcionesEnlace struct {
	URL     string
	Codigo  string
	Cert    tls.Certificate
	Control func(red, direccion string, c syscall.RawConn) error
}

// ResultadoEnlace: lo que Iris guarda tras enlazar. El token se derivó en los dos lados; no viajó.
type ResultadoEnlace struct {
	HuellaServidor string
	Token          []byte
	Manifiesto     Manifiesto
}

// ErrEnlace: el servidor no aceptó el enlace o no demostró conocer el código (sin detalle a propósito).
var ErrEnlace = errors.New("voz: el servidor no aceptó el enlace")

// Enlazar hace el intercambio: presenta el certificado de cliente, manda una prueba del código
// atada a los dos certificados del saludo y exige la prueba del servidor. Ninguno manda el código.
func Enlazar(ctx context.Context, o OpcionesEnlace) (ResultadoEnlace, error) {
	var r ResultadoEnlace
	if err := validarBase(o.URL); err != nil {
		return r, err
	}
	if !CodigoValido(o.Codigo) {
		return r, ErrEnlace
	}
	u, _ := url.Parse(o.URL)
	huellaCliente := huellaDeTLS(&o.Cert)
	crudo, err := marcador(o.Control).DialContext(ctx, "tcp", u.Host)
	if err != nil {
		return r, err
	}
	con := tls.Client(crudo, &tls.Config{
		MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{o.Cert},
		// Todavía no hay huella que fijar: se captura la que presenta el servidor, la prueba del
		// código va atada a ella y el admin la compara con la del instalador antes de activar.
		InsecureSkipVerify: true, //nolint:gosec // ver arriba
	})
	defer func() { _ = con.Close() }()
	plazo, hay := ctx.Deadline()
	if !hay {
		plazo = time.Now().Add(10 * time.Second)
	}
	_ = con.SetDeadline(plazo)
	if err := con.HandshakeContext(ctx); err != nil {
		return r, err
	}
	pares := con.ConnectionState().PeerCertificates
	if len(pares) == 0 {
		return r, ErrEnlace
	}
	r.HuellaServidor = Huella(pares[0].Raw)
	nonce, err := NuevoNonce()
	if err != nil {
		return r, err
	}
	cuerpo, err := json.Marshal(peticionEnlace{Nonce: nonce, Prueba: PruebaIris(o.Codigo, nonce, r.HuellaServidor, huellaCliente)})
	if err != nil {
		return r, err
	}
	// La petición va por la conexión cuyo certificado se acaba de ver, no por otra.
	usada := false
	cli := &http.Client{Transport: &http.Transport{DisableKeepAlives: true, Proxy: nil,
		DialTLSContext: func(context.Context, string, string) (net.Conn, error) {
			if usada {
				return nil, errors.New("voz: conexión de enlace ya usada")
			}
			usada = true
			return con, nil
		}},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.URL+RutaEnlazar, bytes.NewReader(cuerpo))
	if err != nil {
		return r, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := cli.Do(req)
	if err != nil {
		return r, err
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	if err != nil || resp.StatusCode != http.StatusOK {
		return r, ErrEnlace
	}
	var out respuestaEnlace
	if json.Unmarshal(b, &out) != nil || !nonceValido(out.Nonce) ||
		!Iguales(out.Prueba, PruebaServidor(o.Codigo, nonce, out.Nonce, r.HuellaServidor, huellaCliente)) {
		return r, ErrEnlace
	}
	r.Token = TokenDeEnlace(o.Codigo, nonce, out.Nonce, r.HuellaServidor, huellaCliente)
	r.Manifiesto = out.Manifiesto
	return r, nil
}
