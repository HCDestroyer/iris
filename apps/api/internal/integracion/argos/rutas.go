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

	// Técnicos de Argos con bodega (contrato 0.5.0 §3.3): selector del vínculo de agenda.
	tecnicos := conector.Operacion(conector.EtiquetaIntegraciones, "listarTecnicosArgos", http.MethodGet, "/api/v1/integraciones/argosisp/tecnicos",
		"Técnicos de Argos con bodega (selector del vínculo)", nucleo.PermAgendaVincularArgos, http.StatusOK, http.StatusBadGateway, http.StatusServiceUnavailable)
	tecnicos.Description += " Proxy paginado de `GET /inventario/tecnicos` de Argos (≥ 1.6.0; si no, 422 `argos_sin_inventario_tecnico`). Cada uno trae sus bodegas, si tiene bodega activa en la empresa activa (`enEmpresa`: solo así se puede vincular) y la ficha de Iris que ya lo tiene (`vinculadoA`). Lectura auditada (correos)." + descConectorApagado
	huma.Register(api, tecnicos, func(ctx context.Context, in *EntradaTecnicosArgos) (*conector.Cuerpo[PaginaTecnicosArgos], error) {
		return conector.Responder(m.ListarTecnicosArgos.Ejecutar(ctx, *in))
	})

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
	ficha.Description += " Lectura auditada. Refresca la instantánea si tiene más de 24 h, si es de una versión anterior de los datos o con `?refrescar=true` (si se leyó hace más de 30 s); si Argos no responde, devuelve la última con `stale: true`. El estado de cuenta exige además `clientes.estado_cuenta` y se lee en vivo." + descConectorApagado
	huma.Register(api, ficha, func(ctx context.Context, in *EntradaFicha) (*conector.Cuerpo[Ficha], error) {
		return conector.Responder(m.ObtenerFicha.Ejecutar(ctx, *in))
	})
}
