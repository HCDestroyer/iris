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
		"`pendiente_no_reintentable` (hecho, en curso, resuelto a mano o bloqueado: reintenta su dependencia o resuélvela a mano) o `pendiente_incompleto` (la orden no traía los datos). 409 `cargo_terminal`: un cargo rechazado con `422 capacidad_no_disponible` nunca se reintenta con su clave (0.14.3 §3). Auditado; emite " +
		"`integracion.pendiente_reintentado`."
	huma.Register(api, re, func(ctx context.Context, in *entradaReintentarHTTP) (*cuerpo[Pendiente], error) {
		p, err := m.Reintentar.Ejecutar(ctx, EntradaReintentar{ID: in.ID, Version: in.Body.Version})
		if err != nil {
			return nil, err
		}
		return &cuerpo[Pendiente]{Body: p}, nil
	})

	rm := conector.Operacion(conector.EtiquetaIntegraciones, "resolverPendienteIntegracion", http.MethodPost,
		"/api/v1/integraciones/pendientes/{id}/resolver-manual", "Dar por resuelta a mano una acción que Argos rechazó",
		nucleo.PermIntegracionesResolverPendiente, http.StatusOK, http.StatusNotFound, http.StatusConflict)
	rm.Description += " Para lo que Argos rechazó por negocio (`fallido_negocio`, también `datosIncompletos`) y ya se corrigió " +
		"directo en Argos: queda `resuelto_manual` (final, nunca vuelve a salir) con `motivo` y quién lo resolvió, y lo que " +
		"dependía de él (p. ej. el cargo que esperaba al consumo) vuelve a `pendiente` y sale. 409 por `version`; 422 " +
		"`pendiente_no_resoluble` (otro estado: lo agotado se reintenta y lo bloqueado espera a su dependencia) o " +
		"`decision_cargo_requerida` (un consumo con un cargo de un producto del consumo esperándolo: manda `cargo` = " +
		"`liberar` o `resolver_tambien`, que lo deja también `resuelto_manual` sin mandarlo). Los dependientes que " +
		"esperaban su respuesta quedan `fallido_negocio` incompletos (`codigoError` = `dependencia_resuelta_a_mano`), para " +
		"resolverlos también a mano o corregirlos con los datos reales. Auditado; emite `integracion.pendiente_resuelto_manual` " +
		"(uno más por cargo resuelto también)."
	huma.Register(api, rm, func(ctx context.Context, in *entradaResolverManualHTTP) (*cuerpo[Pendiente], error) {
		p, err := m.ResolverManual.Ejecutar(ctx, EntradaResolverManual{ID: in.ID, Version: in.Body.Version, Motivo: in.Body.Motivo, Cargo: in.Body.Cargo})
		if err != nil {
			return nil, err
		}
		return &cuerpo[Pendiente]{Body: p}, nil
	})

	cc := conector.Operacion(conector.EtiquetaIntegraciones, "confirmarCobroIntegracion", http.MethodPost,
		"/api/v1/integraciones/pendientes/{id}/confirmar-cobro", "Confirmar en Argos un cobro «por confirmar»",
		nucleo.PermIntegracionesConfirmarCobro, http.StatusOK, http.StatusNotFound, http.StatusConflict)
	cc.Description += " Un cargo que recibió `422 capacidad_no_disponible` después de un envío sin respuesta (timeout o 5xx) " +
		"queda «por confirmar» (`porConfirmar`): Argos pudo haber creado la factura con el primer envío, y nada sale hasta que " +
		"un supervisor revise Argos (contrato 0.14.3 §3; nunca se concilia solo por `origen_id`). Con `existe` = true y el " +
		"`numero` de la factura, Iris la lee en Argos y la valida (misma empresa, mismo servicio, `origen_id` de la orden y " +
		"líneas iguales al cuerpo del cargo): el cargo queda `hecho` con ese `factura_id` y no sale el reemplazo (422 " +
		"`factura_no_encontrada` o `factura_no_coincide`; 503 si Argos no responde). Con `existe` = false, el cargo queda " +
		"`fallido_negocio` (terminal) y sus demás líneas salen sin la de instalación como un cobro nuevo, con otro `cargo_id`. " +
		"409 por `version` o `cobro_no_por_confirmar`. Auditado; emite `integracion.pendiente_hecho` o `integracion.pendiente_fallido`."
	huma.Register(api, cc, func(ctx context.Context, in *entradaConfirmarCobroHTTP) (*cuerpo[Pendiente], error) {
		p, err := m.ConfirmarCobro(ctx, EntradaConfirmarCobro{ID: in.ID, Version: in.Body.Version, Existe: in.Body.Existe,
			Numero: in.Body.Numero, Motivo: in.Body.Motivo})
		if err != nil {
			return nil, err
		}
		return &cuerpo[Pendiente]{Body: p}, nil
	})
}

type entradaConfirmarCobroHTTP struct {
	nucleo.ConIdempotencia
	ID   int64 `path:"id" minimum:"1"`
	Body struct {
		Version int32   `json:"version" minimum:"1"`
		Existe  bool    `json:"existe" doc:"true: la factura existe en Argos (con numero). false: no existe (sale el cobro de reemplazo)"`
		Numero  *string `json:"numero,omitempty" required:"false" minLength:"1" maxLength:"60" doc:"Con existe = true: número de la factura en Argos"`
		Motivo  string  `json:"motivo" minLength:"5" maxLength:"1000" doc:"Qué se revisó en Argos"`
	}
}

type entradaResolverManualHTTP struct {
	nucleo.ConIdempotencia
	ID   int64 `path:"id" minimum:"1"`
	Body struct {
		Version int32  `json:"version" minimum:"1"`
		Motivo  string `json:"motivo" minLength:"5" maxLength:"1000" doc:"Qué se hizo en Argos (si se rehízo el consumo, con origen_tipo/origen_id de la orden)"`
		Cargo   string `json:"cargo,omitempty" required:"false" enum:"liberar,resolver_tambien" doc:"Obligatorio si un cargo de un producto del consumo lo espera: liberar (sale hacia Argos) o resolver_tambien (Argos ya lo cobró: queda resuelto_manual y no sale)"`
	}
}
