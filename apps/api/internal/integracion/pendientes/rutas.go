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
		"`pendiente_no_reintentable` (hecho, en curso, resuelto a mano o bloqueado: reintenta su dependencia o resuélvela a mano) o `pendiente_incompleto` (la orden no traía los datos). Auditado; emite " +
		"`integracion.pendiente_reintentado`."
	huma.Register(api, re, func(ctx context.Context, in *entradaReintentarHTTP) (*cuerpo[Pendiente], error) {
		p, err := m.Reintentar.Ejecutar(ctx, EntradaReintentar{ID: in.ID, Version: in.Body.Version})
		if err != nil {
			return nil, err
		}
		return &cuerpo[Pendiente]{Body: p}, nil
	})

	ver := conector.Operacion(conector.EtiquetaIntegraciones, "verificarIntentoRecuperacion", http.MethodGet,
		"/api/v1/integraciones/argosisp/intentos-recuperacion/verificacion", "Verificar un cambio o un intento de recuperación (Argos)",
		nucleo.PermAdjuntosVerificarIntegracion, http.StatusOK, http.StatusNotFound, http.StatusTooManyRequests)
	ver.Description += " Contrato con ArgosISP 0.14.0 §3.10: Argos consulta, con su `iris_pat_` del panel y la `Idempotency-Key` " +
		"con la que recibió el cambio o el intento, si Iris se lo mandó. `verificado: true` solo si Iris recibió de ese conector un 2xx " +
		"para esa clave y la foto (siempre en un intento; en un cambio, si la lleva) está completa, es de esa orden y de su autor, se " +
		"tomó después de su llegada y no la usa otra operación. Si no, `motivo`: `envio_en_curso`, `no_enviado_por_iris`, " +
		"`no_coincide` (Argos respondió 422 `idempotency_key_reutilizada`), `foto_pendiente` o `foto_invalida`. 404 igual para una " +
		"clave desconocida o fuera de las empresas del alcance del conector. Nunca devuelve la imagen, el autor ni las horas. " +
		"Auditada; 60 consultas por minuto por conector y 20 por clave al día (429 `demasiadas_peticiones` con `Retry-After`)."
	huma.Register(api, ver, func(ctx context.Context, in *EntradaVerificarIntento) (*cuerpo[VerificacionIntento], error) {
		v, err := m.VerificarIntento(ctx, *in)
		if err != nil {
			return nil, err
		}
		return &cuerpo[VerificacionIntento]{Body: v}, nil
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
		p, err := m.ResolverManual.Ejecutar(ctx, EntradaResolverManual{ID: in.ID, Version: in.Body.Version, Motivo: in.Body.Motivo, Cargo: in.Body.Cargo, ClienteID: in.Body.ClienteID})
		if err != nil {
			return nil, err
		}
		return &cuerpo[Pendiente]{Body: p}, nil
	})
}

type entradaResolverManualHTTP struct {
	nucleo.ConIdempotencia
	ID   int64 `path:"id" minimum:"1"`
	Body struct {
		Version int32  `json:"version" minimum:"1"`
		Motivo  string `json:"motivo" minLength:"5" maxLength:"1000" doc:"Qué se hizo en Argos (si se rehízo el consumo, con origen_tipo/origen_id de la orden)"`
		Cargo   string `json:"cargo,omitempty" required:"false" enum:"liberar,resolver_tambien" doc:"Obligatorio si un cargo de un producto del consumo lo espera: liberar (sale hacia Argos) o resolver_tambien (Argos ya lo cobró: queda resuelto_manual y no sale)"`
		// ClienteID: solo al resolver un cambio de titular.
		ClienteID *int64 `json:"clienteId,omitempty" required:"false" minimum:"1" doc:"Solo con cambiar_titular: el cliente de Argos que quedó como titular (el cobro y el contrato regenerado van a su nombre). Resolver a mano un cambiar_plan o cambiar_titular significa que el cambio se aplicó en Argos"`
	}
}
