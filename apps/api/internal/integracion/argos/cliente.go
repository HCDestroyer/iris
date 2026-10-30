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

// CabeceraEmpresaArgos es la cabecera de la empresa activa en Argos.
const CabeceraEmpresaArgos = "X-Empresa-Id"

// BuscarClientes: GET /clientes?q=&limit=&cursor= con X-Empresa-Id (≠ 0), que acota la búsqueda en
// Argos; Iris además descarta lo que sabe que es de otra empresa.
func (c *Cliente) BuscarClientes(ctx context.Context, q string, limite int, cursor string, empresaID int64) (Pagina[ClienteArgos], error) {
	v := url.Values{"q": {q}, "limit": {strconv.Itoa(limite)}}
	if cursor != "" {
		v.Set("cursor", cursor)
	}
	cli := c.Cliente
	if empresaID != 0 {
		// La empresa activa en Argos (multiempresa.md): X-Empresa-Id. Argos lista los clientes con
		// algún servicio en ella o sin servicios. (?empresas= solo acepta «todas».)
		cli = cli.ConCabecera(CabeceraEmpresaArgos, strconv.FormatInt(empresaID, 10))
	}
	return conector.ListaDe[ClienteArgos](ctx, cli, "/clientes", v)
}

// ObtenerCliente: GET /clientes/:id.
func (c *Cliente) ObtenerCliente(ctx context.Context, id int64) (ClienteArgos, error) {
	var cl ClienteArgos
	err := c.Pedir(ctx, http.MethodGet, "/clientes/"+strconv.FormatInt(id, 10), nil, nil, true, &conector.Recurso{Destino: &cl})
	return cl, err
}

// ObtenerServicio: GET /servicios/:id (404 si no existe o Argos lo eliminó: servicio.eliminado, 0.3.1).
func (c *Cliente) ObtenerServicio(ctx context.Context, id int64) (ServicioArgos, error) {
	var s ServicioArgos
	err := c.Pedir(ctx, http.MethodGet, "/servicios/"+strconv.FormatInt(id, 10), nil, nil, true, &conector.Recurso{Destino: &s})
	return s, err
}

// ServiciosDeCliente: GET /servicios?cliente_id= (todas las páginas).
func (c *Cliente) ServiciosDeCliente(ctx context.Context, clienteID int64) ([]ServicioArgos, error) {
	return conector.Todas[ServicioArgos](ctx, c.Cliente, "/servicios", url.Values{"cliente_id": {strconv.FormatInt(clienteID, 10)}})
}

// ServiciosDeClienteCompletos: GET /servicios?cliente_id=&empresas=todas, el consolidado de todas
// las empresas. Sin «todas», Argos 1.3.0 filtra por las empresas del token (empresasDeLaLista):
// una lista vacía no probaría que el cliente no tiene servicios. Si Argos no da el consolidado
// (403: el token no tiene alcance global), se leen los de su alcance y completa = false.
func (c *Cliente) ServiciosDeClienteCompletos(ctx context.Context, clienteID int64) (servicios []ServicioArgos, completa bool, err error) {
	v := url.Values{"cliente_id": {strconv.FormatInt(clienteID, 10)}, "empresas": {"todas"}}
	servicios, err = conector.Todas[ServicioArgos](ctx, c.Cliente, "/servicios", v)
	if err == nil {
		return servicios, true, nil
	}
	var e *conector.ErrorRemoto
	if errors.As(err, &e) && e.Status == http.StatusForbidden {
		servicios, err = c.ServiciosDeCliente(ctx, clienteID)
		return servicios, false, err
	}
	return nil, false, err
}

// EstadoCuenta: GET /clientes/:id/estado-cuenta (requiere clientes.estado_cuenta en Argos). Con
// empresa (≠ 0), solo la de esa empresa (?empresa_id= y X-Empresa-Id; multiempresa.md de Argos);
// con 0, el consolidado.
func (c *Cliente) EstadoCuenta(ctx context.Context, clienteID, empresa int64) (EstadoCuentaArgos, error) {
	var e EstadoCuentaArgos
	cli := c.Cliente
	var v url.Values
	if empresa != 0 {
		v = url.Values{"empresa_id": {strconv.FormatInt(empresa, 10)}}
		cli = cli.ConCabecera(CabeceraEmpresaArgos, strconv.FormatInt(empresa, 10))
	}
	err := cli.Pedir(ctx, http.MethodGet, "/clientes/"+strconv.FormatInt(clienteID, 10)+"/estado-cuenta", v, nil, true, &conector.Recurso{Destino: &e})
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

// ObtenerRouter: GET /routers/:id (contrato 0.3.1: 404 si Argos lo eliminó).
func (c *Cliente) ObtenerRouter(ctx context.Context, id int64) (RouterArgos, error) {
	var r RouterArgos
	err := c.Pedir(ctx, http.MethodGet, "/routers/"+strconv.FormatInt(id, 10), nil, nil, true, &conector.Recurso{Destino: &r})
	return r, err
}

// obtenerServicioUnaVez: GET /servicios/:id sin reintentos (confirmarVivos).
func (c *Cliente) obtenerServicioUnaVez(ctx context.Context, id int64) (ServicioArgos, error) {
	var s ServicioArgos
	err := c.Pedir(ctx, http.MethodGet, "/servicios/"+strconv.FormatInt(id, 10), nil, nil, false, &conector.Recurso{Destino: &s})
	return s, err
}

// obtenerRouterUnaVez: GET /routers/:id sin reintentos (confirmarVivos).
func (c *Cliente) obtenerRouterUnaVez(ctx context.Context, id int64) (RouterArgos, error) {
	var r RouterArgos
	err := c.Pedir(ctx, http.MethodGet, "/routers/"+strconv.FormatInt(id, 10), nil, nil, false, &conector.Recurso{Destino: &r})
	return r, err
}

// Zonas: GET /zonas (todas las páginas).
func (c *Cliente) Zonas(ctx context.Context) ([]ZonaArgos, error) {
	return conector.Todas[ZonaArgos](ctx, c.Cliente, "/zonas", nil)
}
