package pendientes

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

type cuerpo[T any] struct {
	Body T
}

type entradaReintentarHTTP struct {
	nucleo.ConIdempotencia
	ID   int64 `path:"id" minimum:"1"`
	Body struct {
		Version int32 `json:"version" minimum:"1"`
	}
}

// Registrar publica la lista y el reintento manual de la cola.
func (m *Modulo) Registrar(api huma.API) {
	lista := conector.Operacion(conector.EtiquetaIntegraciones, "listarPendientesIntegracion", http.MethodGet,
		"/api/v1/integraciones/pendientes", "Cola de acciones pendientes hacia Argos", nucleo.PermIntegracionesVerPendientes, http.StatusOK)
	lista.Description += " De la empresa activa, más recientes primero. Filtros `entidad` + `entidadId` (p. ej. una orden: " +
		"«cerrada con pendiente en Argos» si alguno no está `hecho`) y `estado`. `fallido_negocio` trae en `ultimoError` el " +
		"detail de Argos; `agotado` agotó los reintentos (1 min, 5 min, 30 min, 2 h, 12 h); `bloqueado` espera a una dependencia " +
		"fallida. `conflicto=true`: los que la orden volvió a cerrar con otros datos sin pisarlos (`payloadConflicto`; corregir a mano en Argos)."
	huma.Register(api, lista, func(ctx context.Context, in *EntradaListar) (*cuerpo[nucleo.Pagina[Pendiente]], error) {
		p, err := m.Listar.Ejecutar(ctx, *in)
		if err != nil {
			return nil, err
		}
		return &cuerpo[nucleo.Pagina[Pendiente]]{Body: p}, nil
	})

	re := conector.Operacion(conector.EtiquetaIntegraciones, "reintentarPendienteIntegracion", http.MethodPost,
		"/api/v1/integraciones/pendientes/{id}/reintentar", "Reintentar a mano una acción hacia Argos",
		nucleo.PermIntegracionesReintentar, http.StatusOK, http.StatusNotFound, http.StatusConflict)
	re.Description += " Desde `fallido_negocio`, `agotado` o `pendiente` (adelanta el próximo intento): vuelve a `pendiente` con " +
		"los intentos en 0 y se procesa enseguida, con la misma Idempotency-Key. 409 por `version`; 422 " +
		"`pendiente_no_reintentable` (hecho, en curso o bloqueado: reintenta su dependencia) o `pendiente_incompleto` (la orden no traía los datos). Auditado; emite " +
		"`integracion.pendiente_reintentado`."
	huma.Register(api, re, func(ctx context.Context, in *entradaReintentarHTTP) (*cuerpo[Pendiente], error) {
		p, err := m.Reintentar.Ejecutar(ctx, EntradaReintentar{ID: in.ID, Version: in.Body.Version})
		if err != nil {
			return nil, err
		}
		return &cuerpo[Pendiente]{Body: p}, nil
	})
}
