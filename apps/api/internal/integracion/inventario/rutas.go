package inventario

import (
	"context"
	"net/http"
	"reflect"

	"github.com/danielgtaylor/huma/v2"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

const etiqueta = "campo"

// Registrar publica las lecturas de la app de campo y el esquema de los datos de traslado.recibir
// (la operación viaja en POST /sync/lote; el esquema se publica para @iris/sync).
func (m *Modulo) Registrar(api huma.API) {
	bodega := conector.Operacion(etiqueta, "miBodega", http.MethodGet, "/api/v1/campo/inventario/mi-bodega",
		"Mi bodega en Argos", nucleo.PermCampoVerBodega, http.StatusOK, http.StatusBadGateway, http.StatusServiceUnavailable)
	bodega.Description += " Contrato 0.5.0 §3.3 (Argos ≥ 1.6.0; si no, 422 `argos_sin_inventario_tecnico`): `GET /inventario/mi-bodega` con el vínculo de tu ficha de agenda (422 `tecnico_sin_vincular`) y la empresa activa (`X-Iris-Empresa`). Existencias con modalidad, unidades, órdenes de traslado por recibir (ids) y los últimos 50 movimientos. Caché de 1 min por técnico y empresa. 422 `tecnico_sin_bodega`, `tecnico_ambiguo`, `empresa_requerida` (con el detalle de Argos). Sin conector, 503 `conector_apagado`."
	huma.Register(api, bodega, func(ctx context.Context, _ *struct{}) (*conector.Cuerpo[MiBodegaRespuesta], error) {
		return conector.Responder(m.MiBodega.Ejecutar(ctx))
	})
	traslados := conector.Operacion(etiqueta, "misTraslados", http.MethodGet, "/api/v1/campo/inventario/traslados",
		"Mis órdenes de traslado", nucleo.PermCampoVerBodega, http.StatusOK)
	traslados.Description += " Las órdenes de traslado hacia tu bodega que Argos avisó (`inventario.traslado_*`), de la empresa activa, más recientes primero, con el estado de tu recepción si la mandaste (`recepcion`). Para «Por recibir»: `?estado=enviado`. 422 `tecnico_sin_vincular`."
	huma.Register(api, traslados, func(ctx context.Context, in *EntradaTraslados) (*conector.Cuerpo[PaginaTraslados], error) {
		return conector.Responder(m.Traslados.Ejecutar(ctx, *in))
	})
	// Datos de la operación traslado.recibir y su estado (cambios): esquemas para el cliente TS.
	api.OpenAPI().Components.Schemas.Schema(reflect.TypeOf(EntradaRecibirTraslado{}), true, "")
	api.OpenAPI().Components.Schemas.Schema(reflect.TypeOf(EstadoRecepcion{}), true, "")
}
