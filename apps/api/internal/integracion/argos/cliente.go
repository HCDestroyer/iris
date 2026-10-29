package argos

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
)

// Cliente HTTP de la API de Argos (/api/v1, contratos/argosisp.md §2.1), sobre el cliente
// común de los conectores (conector.Cliente): las lecturas idempotentes se reintentan con
// espera exponencial ante fallas de red, 429 y 5xx; los errores problem+json de Argos llegan
// como *ErrorArgos. El token nunca aparece en errores ni logs.
type Cliente struct {
	*conector.Cliente
}

// OpcionesCliente configuran el cliente.
type OpcionesCliente = conector.OpcionesCliente

// NuevoCliente: base es el origen de Argos (https://argos.isp.gt, sin /api/v1); token, el atk_….
// Sin OpcionesCliente.HTTP usa http.DefaultClient (sin la protección contra SSRF: solo pruebas).
func NuevoCliente(base, token string, op OpcionesCliente) *Cliente {
	return &Cliente{Cliente: conector.NuevoCliente(Remoto, base, token, op)}
}

// ErrorArgos es una respuesta de error de Argos (problem+json) o una falla de red (Status 0).
type ErrorArgos = conector.ErrorRemoto

// EsCaido indica si err es una falla temporal de Argos.
func EsCaido(err error) bool { return conector.EsCaido(err) }

// EsNoEncontrado indica si Argos respondió 404.
func EsNoEncontrado(err error) bool { return conector.EsNoEncontrado(err) }

// Problema traduce un error de Argos al problema que ve el cliente de Iris (conector.Problema):
// 422 destino_no_permitido, 404, 502 argos_sin_permiso, 503 argos_no_disponible o 502
// argos_rechazo (resumen, nunca el detail crudo). Cualquier otro error pasa tal cual.
func Problema(err error) error { return conector.Problema(err) }

// Pagina es una página de una lista de Argos.
type Pagina[T any] = conector.Pagina[T]

// Lecturas de §2.1 ------------------------------------------------------------------------

// BuscarClientes: GET /clientes?q=&limit=&cursor=[&empresas=]. La empresa (≠ 0) acota la
// búsqueda en Argos; Iris además descarta lo que sabe que es de otra empresa.
func (c *Cliente) BuscarClientes(ctx context.Context, q string, limite int, cursor string, empresaID int64) (Pagina[ClienteArgos], error) {
	v := url.Values{"q": {q}, "limit": {strconv.Itoa(limite)}}
	if cursor != "" {
		v.Set("cursor", cursor)
	}
	if empresaID != 0 {
		// Argos 1.3.0 filtra con empresas= (clientes con algún servicio en la empresa o sin
		// servicios); empresa_id queda por compatibilidad con versiones anteriores del supuesto.
		v.Set("empresas", strconv.FormatInt(empresaID, 10))
		v.Set("empresa_id", strconv.FormatInt(empresaID, 10))
	}
	return conector.ListaDe[ClienteArgos](ctx, c.Cliente, "/clientes", v)
}

// ObtenerCliente: GET /clientes/:id.
func (c *Cliente) ObtenerCliente(ctx context.Context, id int64) (ClienteArgos, error) {
	var cl ClienteArgos
	err := c.Pedir(ctx, http.MethodGet, "/clientes/"+strconv.FormatInt(id, 10), nil, nil, true, &conector.Recurso{Destino: &cl})
	return cl, err
}

// ObtenerServicio: GET /servicios/:id (supuesto: existe como /clientes/:id).
func (c *Cliente) ObtenerServicio(ctx context.Context, id int64) (ServicioArgos, error) {
	var s ServicioArgos
	err := c.Pedir(ctx, http.MethodGet, "/servicios/"+strconv.FormatInt(id, 10), nil, nil, true, &conector.Recurso{Destino: &s})
	return s, err
}

// ServiciosDeCliente: GET /servicios?cliente_id= (todas las páginas).
func (c *Cliente) ServiciosDeCliente(ctx context.Context, clienteID int64) ([]ServicioArgos, error) {
	return conector.Todas[ServicioArgos](ctx, c.Cliente, "/servicios", url.Values{"cliente_id": {strconv.FormatInt(clienteID, 10)}})
}

// EstadoCuenta: GET /clientes/:id/estado-cuenta (requiere clientes.estado_cuenta en Argos).
func (c *Cliente) EstadoCuenta(ctx context.Context, clienteID int64) (EstadoCuentaArgos, error) {
	var e EstadoCuentaArgos
	err := c.Pedir(ctx, http.MethodGet, "/clientes/"+strconv.FormatInt(clienteID, 10)+"/estado-cuenta", nil, nil, true, &conector.Recurso{Destino: &e})
	return e, err
}

// ResolverIP pregunta a Argos de quién es una IP. Usa el endpoint del contrato (pedido A8,
// POST /integraciones/iris/resolver { ip }); si Argos aún no lo tiene (404, 405 o 501), usa el
// respaldo existente GET /ip/{ip}. Un 404 del respaldo es «IP sin cliente». empresaID (≠ 0) va
// en el cuerpo y en la consulta (supuesto): la misma IP privada puede existir en dos empresas.
func (c *Cliente) ResolverIP(ctx context.Context, ip string, empresaID int64) (ResolucionArgos, error) {
	var r ResolucionArgos
	cuerpo := map[string]any{"ip": ip}
	var consulta url.Values
	if empresaID != 0 {
		cuerpo["empresa_id"] = empresaID
		consulta = url.Values{"empresa_id": {strconv.FormatInt(empresaID, 10)}}
	}
	err := c.Pedir(ctx, http.MethodPost, "/integraciones/iris/resolver", nil, cuerpo, true, &conector.Recurso{Destino: &r})
	var ea *ErrorArgos
	if err == nil || !errors.As(err, &ea) || (ea.Status != http.StatusNotFound && ea.Status != http.StatusMethodNotAllowed && ea.Status != http.StatusNotImplemented) {
		return r, err
	}
	r = ResolucionArgos{}
	err = c.Pedir(ctx, http.MethodGet, "/ip/"+url.PathEscape(ip), consulta, nil, true, &conector.Recurso{Destino: &r})
	return r, err
}

// Planes: GET /planes (todas las páginas).
func (c *Cliente) Planes(ctx context.Context) ([]PlanArgos, error) {
	return conector.Todas[PlanArgos](ctx, c.Cliente, "/planes", nil)
}

// Routers: GET /routers (todas las páginas).
func (c *Cliente) Routers(ctx context.Context) ([]RouterArgos, error) {
	return conector.Todas[RouterArgos](ctx, c.Cliente, "/routers", nil)
}

// Zonas: GET /zonas (todas las páginas).
func (c *Cliente) Zonas(ctx context.Context) ([]ZonaArgos, error) {
	return conector.Todas[ZonaArgos](ctx, c.Cliente, "/zonas", nil)
}
