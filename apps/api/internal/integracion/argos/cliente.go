package argos

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Cliente HTTP de la API de Argos (/api/v1, contratos/argosisp.md §2.1). Las lecturas
// idempotentes se reintentan con espera exponencial ante fallas de red, 429 y 5xx; las demás
// llamadas (el canje) van una sola vez. Los errores problem+json de Argos llegan como
// *ErrorArgos. El token nunca aparece en errores ni logs.
type Cliente struct {
	base      string
	token     string
	http      *http.Client
	timeout   time.Duration
	intentos  int
	espera    time.Duration
	userAgent string
}

// OpcionesCliente configuran el cliente.
type OpcionesCliente struct {
	HTTP      *http.Client
	Timeout   time.Duration
	Intentos  int
	Espera    time.Duration
	UserAgent string
}

// NuevoCliente: base es el origen de Argos (https://argos.isp.gt, sin /api/v1); token, el atk_….
func NuevoCliente(base, token string, op OpcionesCliente) *Cliente {
	if op.HTTP == nil {
		op.HTTP = http.DefaultClient
	}
	if op.Timeout <= 0 {
		op.Timeout = 5 * time.Second
	}
	if op.Intentos <= 0 {
		op.Intentos = 3
	}
	if op.Espera <= 0 {
		op.Espera = 200 * time.Millisecond
	}
	if op.UserAgent == "" {
		op.UserAgent = "Iris"
	}
	return &Cliente{
		base: strings.TrimRight(base, "/"), token: token, http: op.HTTP, timeout: op.Timeout,
		intentos: op.Intentos, espera: op.Espera, userAgent: op.UserAgent,
	}
}

// ErrorArgos es una respuesta de error de Argos (problem+json) o una falla de red (Status 0).
type ErrorArgos struct {
	Status  int
	Tipo    string
	Titulo  string
	Detalle string

	reintentarEn time.Duration
	causa        error
}

func (e *ErrorArgos) Error() string {
	if e.Status == 0 {
		return "argos: sin respuesta: " + e.causa.Error()
	}
	msg := fmt.Sprintf("argos: %d", e.Status)
	if e.Tipo != "" {
		msg += " " + e.Tipo
	}
	if e.Detalle != "" {
		msg += ": " + e.Detalle
	}
	return msg
}

func (e *ErrorArgos) Unwrap() error { return e.causa }

// Caido indica que Argos no respondió o respondió con una falla temporal: se puede servir la
// última instantánea marcada stale.
func (e *ErrorArgos) Caido() bool {
	return e.Status == 0 || e.Status == http.StatusTooManyRequests || e.Status >= 500
}

// EsCaido indica si err es una falla temporal de Argos.
func EsCaido(err error) bool {
	var ea *ErrorArgos
	return errors.As(err, &ea) && ea.Caido()
}

// EsNoEncontrado indica si Argos respondió 404.
func EsNoEncontrado(err error) bool {
	var ea *ErrorArgos
	return errors.As(err, &ea) && ea.Status == http.StatusNotFound
}

// Problema traduce un error de Argos al problema que ve el cliente de Iris:
//
//   - 404 → 404 no_encontrado;
//   - 401/403 → 502 argos_sin_permiso (el token del conector es inválido o le falta un permiso);
//   - red, timeout, 429 o 5xx → 503 argos_no_disponible;
//   - otro 4xx → 502 argos_rechazo con el detail de Argos.
//
// Cualquier otro error pasa tal cual.
func Problema(err error) error {
	var ea *ErrorArgos
	if !errors.As(err, &ea) {
		return err
	}
	switch {
	case ea.Status == http.StatusNotFound:
		return nucleo.NoEncontrado("No existe en Argos")
	case ea.Status == http.StatusUnauthorized || ea.Status == http.StatusForbidden:
		return nucleo.NuevoProblema(http.StatusBadGateway, "argos_sin_permiso", "Argos rechazó la credencial",
			"El token del conector no es válido o le falta un permiso en Argos ("+strconv.Itoa(ea.Status)+")")
	case ea.Caido():
		return nucleo.NuevoProblema(http.StatusServiceUnavailable, "argos_no_disponible", "Argos no disponible",
			"ArgosISP no respondió; vuelve a intentar en unos minutos")
	}
	detalle := ea.Detalle
	if detalle == "" {
		detalle = ea.Titulo
	}
	return nucleo.NuevoProblema(http.StatusBadGateway, "argos_rechazo", "Argos rechazó la petición", detalle)
}

func reintentable(err error) (bool, time.Duration) {
	var ea *ErrorArgos
	if !errors.As(err, &ea) {
		return false, 0
	}
	if ea.Status == 0 || ea.Status == http.StatusTooManyRequests || ea.Status >= 500 {
		return true, ea.reintentarEn
	}
	return false, 0
}

const maxRespuesta = 4 << 20

// pedir hace la llamada; con idempotente=true reintenta según las opciones.
func (c *Cliente) pedir(ctx context.Context, metodo, ruta string, consulta url.Values, cuerpo any, idempotente bool, destino any) error {
	intentos := 1
	if idempotente {
		intentos = c.intentos
	}
	espera := c.espera
	for i := 1; ; i++ {
		err := c.una(ctx, metodo, ruta, consulta, cuerpo, destino)
		if err == nil {
			return nil
		}
		otra, sugerida := reintentable(err)
		if !otra || i >= intentos || ctx.Err() != nil {
			return err
		}
		pausa := espera
		if sugerida > 0 {
			pausa = min(sugerida, 5*time.Second)
		}
		select {
		case <-ctx.Done():
			return err
		case <-time.After(pausa):
		}
		espera *= 2
	}
}

func (c *Cliente) una(ctx context.Context, metodo, ruta string, consulta url.Values, cuerpo any, destino any) error {
	ctx, cancelar := context.WithTimeout(ctx, c.timeout)
	defer cancelar()
	u := c.base + "/api/v1" + ruta
	if len(consulta) > 0 {
		u += "?" + consulta.Encode()
	}
	var lector io.Reader
	if cuerpo != nil {
		b, err := json.Marshal(cuerpo)
		if err != nil {
			return err
		}
		lector = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, metodo, u, lector)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json, application/problem+json")
	req.Header.Set("User-Agent", c.userAgent)
	if cuerpo != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if id := contexto.RequestIDDe(ctx); id != "" {
		req.Header.Set("X-Request-Id", id)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return &ErrorArgos{causa: limpiarErrorURL(err)}
	}
	defer func() { _ = resp.Body.Close() }()
	crudo, err := io.ReadAll(io.LimitReader(resp.Body, maxRespuesta))
	if err != nil {
		return &ErrorArgos{causa: err}
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return errorDeRespuesta(resp, crudo)
	}
	if destino == nil || len(bytes.TrimSpace(crudo)) == 0 {
		return nil
	}
	if err := decodificar(crudo, destino); err != nil {
		return &ErrorArgos{Status: http.StatusBadGateway, Titulo: "Respuesta de Argos ilegible", causa: err}
	}
	return nil
}

// limpiarErrorURL quita la URL completa del error (puede llevar la consulta del usuario).
func limpiarErrorURL(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return fmt.Errorf("%s: %w", ue.Op, ue.Err)
	}
	return err
}

func errorDeRespuesta(resp *http.Response, crudo []byte) *ErrorArgos {
	e := &ErrorArgos{Status: resp.StatusCode, causa: errors.New(resp.Status)}
	var p struct {
		Type   string `json:"type"`
		Title  string `json:"title"`
		Detail string `json:"detail"`
	}
	if json.Unmarshal(crudo, &p) == nil {
		e.Tipo, e.Titulo, e.Detalle = p.Type, p.Title, p.Detail
	}
	if len(e.Detalle) > 500 {
		e.Detalle = e.Detalle[:500]
	}
	if v := resp.Header.Get("Retry-After"); v != "" {
		if s, err := strconv.Atoi(v); err == nil && s >= 0 {
			e.reintentarEn = time.Duration(s) * time.Second
		}
	}
	return e
}

// Lecturas de §2.1 ------------------------------------------------------------------------

// Pagina es una página de una lista de Argos.
type Pagina[T any] struct {
	Data       []T
	NextCursor string
}

// BuscarClientes: GET /clientes?q=&limit=&cursor=.
func (c *Cliente) BuscarClientes(ctx context.Context, q string, limite int, cursor string) (Pagina[ClienteArgos], error) {
	v := url.Values{"q": {q}, "limit": {strconv.Itoa(limite)}}
	if cursor != "" {
		v.Set("cursor", cursor)
	}
	return lista[ClienteArgos](ctx, c, "/clientes", v)
}

// ObtenerCliente: GET /clientes/:id.
func (c *Cliente) ObtenerCliente(ctx context.Context, id int64) (ClienteArgos, error) {
	var cl ClienteArgos
	err := c.pedir(ctx, http.MethodGet, "/clientes/"+strconv.FormatInt(id, 10), nil, nil, true, &envoltura{destino: &cl})
	return cl, err
}

// ServiciosDeCliente: GET /servicios?cliente_id= (todas las páginas).
func (c *Cliente) ServiciosDeCliente(ctx context.Context, clienteID int64) ([]ServicioArgos, error) {
	return todas[ServicioArgos](ctx, c, "/servicios", url.Values{"cliente_id": {strconv.FormatInt(clienteID, 10)}})
}

// EstadoCuenta: GET /clientes/:id/estado-cuenta (requiere clientes.estado_cuenta en Argos).
func (c *Cliente) EstadoCuenta(ctx context.Context, clienteID int64) (EstadoCuentaArgos, error) {
	var e EstadoCuentaArgos
	err := c.pedir(ctx, http.MethodGet, "/clientes/"+strconv.FormatInt(clienteID, 10)+"/estado-cuenta", nil, nil, true, &envoltura{destino: &e})
	return e, err
}

// ResolverIP pregunta a Argos de quién es una IP. Usa el endpoint del contrato (pedido A8,
// POST /integraciones/iris/resolver { ip }); si Argos aún no lo tiene (404, 405 o 501), usa el
// respaldo existente GET /ip/{ip}. Un 404 del respaldo es «IP sin cliente».
func (c *Cliente) ResolverIP(ctx context.Context, ip string) (ResolucionArgos, error) {
	var r ResolucionArgos
	err := c.pedir(ctx, http.MethodPost, "/integraciones/iris/resolver", nil, map[string]string{"ip": ip}, true, &envoltura{destino: &r})
	var ea *ErrorArgos
	if err == nil || !errors.As(err, &ea) || (ea.Status != http.StatusNotFound && ea.Status != http.StatusMethodNotAllowed && ea.Status != http.StatusNotImplemented) {
		return r, err
	}
	r = ResolucionArgos{}
	err = c.pedir(ctx, http.MethodGet, "/ip/"+url.PathEscape(ip), nil, nil, true, &envoltura{destino: &r})
	return r, err
}

// Planes: GET /planes (todas las páginas).
func (c *Cliente) Planes(ctx context.Context) ([]PlanArgos, error) {
	return todas[PlanArgos](ctx, c, "/planes", nil)
}

// Routers: GET /routers (todas las páginas).
func (c *Cliente) Routers(ctx context.Context) ([]RouterArgos, error) {
	return todas[RouterArgos](ctx, c, "/routers", nil)
}

// Zonas: GET /zonas (todas las páginas).
func (c *Cliente) Zonas(ctx context.Context) ([]ZonaArgos, error) {
	return todas[ZonaArgos](ctx, c, "/zonas", nil)
}

// Canjear llama al canje de Argos (§1.3): POST /integraciones/iris/conexion/canjear. Sin
// reintentos (el código es de un solo uso) y sin token.
func (c *Cliente) Canjear(ctx context.Context, p PeticionCanje) (RespuestaCanje, error) {
	var r RespuestaCanje
	err := c.pedir(ctx, http.MethodPost, "/integraciones/iris/conexion/canjear", nil, p, false, &envoltura{destino: &r})
	return r, err
}

func lista[T any](ctx context.Context, c *Cliente, ruta string, v url.Values) (Pagina[T], error) {
	var l listaArgos[T]
	if err := c.pedir(ctx, http.MethodGet, ruta, v, nil, true, &l); err != nil {
		return Pagina[T]{}, err
	}
	return Pagina[T]{Data: l.data, NextCursor: l.next}, nil
}

// maxPaginas acota las listas completas (catálogos, servicios de un cliente).
const maxPaginas = 50

func todas[T any](ctx context.Context, c *Cliente, ruta string, v url.Values) ([]T, error) {
	if v == nil {
		v = url.Values{}
	}
	v.Set("limit", "200")
	var salida []T
	for range maxPaginas {
		p, err := lista[T](ctx, c, ruta, v)
		if err != nil {
			return nil, err
		}
		salida = append(salida, p.Data...)
		if p.NextCursor == "" {
			return salida, nil
		}
		v.Set("cursor", p.NextCursor)
	}
	return nil, &ErrorArgos{Status: http.StatusBadGateway, Titulo: "Lista de Argos demasiado larga", causa: errors.New("más de 50 páginas en " + ruta)}
}
