package argos

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Handlers delgados: leen la petición, llaman al caso de uso y arman la respuesta.

const (
	etiquetaClientes    = "clientes"
	descConectorApagado = " Sin conector Argos activo responde 503 `conector_apagado` (el panel trabaja con datos locales)."
)

// Registrar publica las rutas de conectores (comunes y de Argos) y de clientes en la API.
func (m *Modulo) Registrar(api huma.API) {
	// Conectores: rutas comunes (listar, activar, desactivar, URL pública, enlaces) y las de
	// conexión y webhook de Argos. Las de Horus las publica horus.Modulo.Registrar.
	m.nucleo.Registrar(api)
	m.conexion.Registrar(api)

	// Clientes (lectura de Argos) --------------------------------------------------------
	buscar := conector.Operacion(etiquetaClientes, "buscarClientes", http.MethodGet, "/api/v1/clientes",
		"Buscar clientes en Argos", nucleo.PermClientesVer, http.StatusOK, http.StatusBadGateway, http.StatusServiceUnavailable)
	buscar.Description += " Lectura auditada. Si Argos no responde, busca en las instantáneas locales y responde `stale: true`." + descConectorApagado
	huma.Register(api, buscar, func(ctx context.Context, in *EntradaBuscarClientes) (*conector.Cuerpo[ResultadoBusqueda], error) {
		return conector.Responder(m.BuscarClientes.Ejecutar(ctx, *in))
	})

	ip := conector.Operacion(etiquetaClientes, "resolverIP", http.MethodGet, "/api/v1/clientes/resolver-ip",
		"Cliente y servicio dueños de una IP", nucleo.PermClientesVer, http.StatusOK, http.StatusNotFound, http.StatusBadGateway, http.StatusServiceUnavailable)
	ip.Description += " Lectura auditada. Pregunta a Argos (pedido A8, con respaldo a `GET /ip/{ip}`); si no responde, usa la instantánea (`stale: true`)." + descConectorApagado
	huma.Register(api, ip, func(ctx context.Context, in *EntradaResolverIP) (*conector.Cuerpo[IPResuelta], error) {
		return conector.Responder(m.ResolverIP.Ejecutar(ctx, *in))
	})

	ficha := conector.Operacion(etiquetaClientes, "obtenerFichaCliente", http.MethodGet, "/api/v1/clientes/{id}",
		"Ficha del cliente: datos, servicios y estado de cuenta resumido", nucleo.PermClientesVer, http.StatusOK,
		http.StatusNotFound, http.StatusBadGateway, http.StatusServiceUnavailable)
	ficha.Description += " Lectura auditada. Refresca la instantánea si tiene más de 24 h; si Argos no responde, devuelve la última con `stale: true`. El estado de cuenta exige además `clientes.estado_cuenta` y se lee en vivo." + descConectorApagado
	huma.Register(api, ficha, func(ctx context.Context, in *EntradaFicha) (*conector.Cuerpo[Ficha], error) {
		return conector.Responder(m.ObtenerFicha.Ejecutar(ctx, *in))
	})
}
