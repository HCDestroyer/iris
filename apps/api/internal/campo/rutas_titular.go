package campo

import (
	"context"
	"net/http"
	"reflect"

	"github.com/danielgtaylor/huma/v2"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// registrarTitular publica el paso «Confirmar titular y plan» (contrato argosisp.md 0.10.0 §3.4):
// su estado, la validación de la identidad desde el panel y los esquemas de las cuatro
// operaciones del lote (packages/sync los toma del contrato).
func (m *Modulo) registrarTitular(api huma.API) {
	ver := operacion("verTitularOrden", http.MethodGet, "/api/v1/ordenes/{id}/titular", "Estado del paso «Confirmar titular y plan»",
		nucleo.PermOrdenesVer, http.StatusOK, http.StatusNotFound)
	ver.Description = "Permiso: `ordenes.ver` u `ordenes.ver_propias` (la orden que el actor ve). Quién firma, el tipo de documento y el nombre " +
		"(nunca el número, el teléfono, el correo ni las fotos), si la identidad está registrada o validada, el último plan pedido y cada paso hacia " +
		"Argos con su estado: `enviando`, `hecho`, `rechazado` (con `codigo`), `agotado`, `bloqueado` o `resuelto`. `enRevision`: Argos respondió " +
		"`titular_no_coincide` (lo resuelve un supervisor); `pendienteEnArgos`: un paso no llegó y la activación no sale. Sin datos del cliente de Argos " +
		"ni `posible_duplicado`."
	huma.Register(api, ver, func(ctx context.Context, in *EntradaVerOrden) (*cuerpo[EstadoTitular], error) {
		return responder(m.Titular.Ver.Ejecutar(ctx, *in))
	})

	registrarAccion(api, accion("validarIdentidadOrden", "validar-identidad", "Validar la identidad del titular desde el panel",
		"Un supervisor, con motivo (10 a 500), en una instalación no cancelada. Solo sirve para activar: nunca cambia el titular ni toma una identidad. "+
			"Sin identidad tomada, la orden se puede cerrar y activar sin ella. Con la identidad del mismo titular rechazada o agotada en Argos y su foto aún "+
			"en el depósito de tránsito (7 días), la manda otra vez como `revision_panel` (la foto tomada en sitio, que nadie ve en Iris); si la foto ya se "+
			"borró, el paso queda resuelto y la activación sale sin ella. 409 `identidad_registrada`, `identidad_en_envio` o `identidad_ya_validada`; "+
			"422 `titular_en_revision` (un cambio de titular no se valida), `orden_no_es_instalacion`, `argos_sin_conector`. Emite `orden.identidad_validada`.",
		nucleo.PermOrdenesValidarIdentidad), m.Titular.ValidarIdentidad.Ejecutar, func(e *EntradaValidarIdentidad, id int64) { e.ID = id })

	registrarAccion(api, accion("levantarRevisionTitularOrden", "levantar-revision-titular", "Levantar la revisión del titular",
		"Un supervisor, con motivo (10 a 500), cuando Argos respondió `titular_no_coincide`: mientras la orden está en revisión no se toma otra "+
			"identidad ni otro titular (422 `titular_en_revision`). No manda nada a Argos. 409 `sin_revision`. Emite `orden.revision_titular_levantada`.",
		nucleo.PermOrdenesValidarIdentidad), m.Titular.LevantarRevision.Ejecutar, func(e *EntradaLevantarRevision, id int64) { e.ID = id })

	for _, v := range datosTitularLote {
		api.OpenAPI().Components.Schemas.Schema(reflect.TypeOf(v), true, "")
	}
}
