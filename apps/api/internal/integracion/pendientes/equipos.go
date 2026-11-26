package pendientes

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	pendientesbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/pendientes/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Equipos del cliente y activación desde la orden (contrato argosisp.md 0.9.0 §12, B12, B14 y B16).
// Las llamadas van por la cola como el resto de §3: aprovisionar (A32), cambiar el Wi-Fi (A35) y
// reenviar la bienvenida (A37). La clave Wi-Fi nunca va en el payload: viaja en el depósito de
// tránsito (wifi.clave o clave) y se borra al 2xx de Argos o a su rechazo.

// Operaciones de la 0.9.0.
const (
	// OpAprovisionar: POST /servicios/:id/aprovisionar (§12.2), iris:aprovisionar:<orden_id>:<intento>.
	OpAprovisionar = "aprovisionar"
	// OpCambiarWifi: POST /servicios/:id/wifi (§12.4), iris:wifi:<origen>:<id>:<uuid>.
	OpCambiarWifi = "cambiar_wifi"
	// OpReenviarBienvenida: POST /servicios/:id/bienvenida/reenviar (§12.5), iris:bienvenida:<orden_id>:<n>.
	OpReenviarBienvenida = "reenviar_bienvenida"
)

// CabeceraConversacionIris: la conversación desde la que actúa el asistente (§7.1).
const CabeceraConversacionIris = "X-Iris-Conversacion"

// CodigoServicioNoPendiente: Argos ya no tiene el servicio pendiente_instalacion (§3.1).
const CodigoServicioNoPendiente = "servicio_no_pendiente"

// CodigoClaveDescartada: la clave Wi-Fi de este pendiente ya se descartó (rechazo de Argos o 7 días):
// hay que pedir el cambio otra vez (otro intento, otra clave).
const CodigoClaveDescartada = "clave_wifi_descartada"

// ClaveAprovisionar: iris:aprovisionar:<orden_id>:<intento>.
func ClaveAprovisionar(orden int64, intento int32) string {
	return "iris:aprovisionar:" + strconv.FormatInt(orden, 10) + ":" + strconv.FormatInt(int64(intento), 10)
}

// ClaveWifi: iris:wifi:<orden|ticket|conversacion>:<id>:<uuid>.
func ClaveWifi(origen string, id int64, uuid string) string {
	return "iris:wifi:" + origen + ":" + strconv.FormatInt(id, 10) + ":" + url.PathEscape(uuid)
}

// ClaveBienvenida: iris:bienvenida:<orden_id>:<n>.
func ClaveBienvenida(orden int64, n int32) string {
	return "iris:bienvenida:" + strconv.FormatInt(orden, 10) + ":" + strconv.FormatInt(int64(n), 10)
}

// conClaveWifi: operaciones cuyo depósito guarda la clave Wi-Fi (se descarta también al rechazo).
func conClaveWifi(operacion string) bool {
	return operacion == OpAprovisionar || operacion == OpCambiarWifi
}

// AccionConTransito encola una acción cuyo cuerpo tiene una parte sensible (la clave Wi-Fi): la
// guarda cifrada en el depósito de tránsito de la entidad y encola el pendiente apuntándolo. Sin
// transito, es Encolar. Sin cifrador y con transito, 503 (no se guarda la clave sin cifrar).
func AccionConTransito(ctx context.Context, t *auditoria.Tx, cif *nucleo.Cifrador, n Accion, transito *Transito, aviso Encolador) (Registro, bool, error) {
	if transito != nil {
		id, err := strconv.ParseInt(n.EntidadID, 10, 64)
		if err != nil {
			return Registro{}, false, errors.New("pendientes.AccionConTransito: la entidad necesita un id numérico")
		}
		tid, err := GuardarTransitoDe(ctx, t, cif, n.EmpresaID, n.Entidad, id, *transito, n.Desde)
		if err != nil {
			return Registro{}, false, err
		}
		n.TransitoID = &tid
	}
	return Encolar(ctx, t, n, aviso)
}

// descartarClave: un pendiente con la clave Wi-Fi que Argos rechazó (4xx de negocio) no vuelve a
// salir con ella: el depósito se borra ya (no espera a los 7 días).
func descartarClave(ctx context.Context, t *auditoria.Tx, f pendientesbd.IntegracionPendiente, ahora time.Time) error {
	if !conClaveWifi(f.Operacion) || f.Estado != EstadoFallidoNegocio {
		return nil
	}
	return borrarTransito(ctx, t, f, "rechazado", ahora)
}

// activacionLista: la orden del pendiente tiene un intento de activación listo (§12.2): Argos ya activó
// el servicio, así que su 409 servicio_no_pendiente a activar-instalacion es conciliado (§3).
func activacionLista(ctx context.Context, q pendientesbd.DBTX, f pendientesbd.IntegracionPendiente) bool {
	if f.Entidad != "orden" {
		return false
	}
	orden, err := strconv.ParseInt(f.EntidadID, 10, 64)
	if err != nil {
		return false
	}
	var lista bool
	err = q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM campo.activaciones WHERE orden_id = $1 AND estado = 'listo')`, orden).Scan(&lista)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return false
	}
	return lista
}

// BloquearSiDependenciaFallida: un pendiente que nace detrás de una dependencia ya fallida, agotada
// o bloqueada queda bloqueado (y avisa) en vez de esperar en silencio.
func BloquearSiDependenciaFallida(ctx context.Context, t *auditoria.Tx, depende []int64) error {
	return bloquearSiDependenciaFallida(ctx, t, depende)
}

// CodigoAprovisionamientoEnCurso: 409 de …/aprovisionar con otro aprovisionamiento en curso para el
// servicio (§12.2), con extensions.aprovisionamiento_id y, desde 0.14.3, extensions.iniciado_por.
const CodigoAprovisionamientoEnCurso = "aprovisionamiento_en_curso"

// CodigoAprovisionamientoPropio: el 409 aprovisionamiento_en_curso nombra un intento de Iris de la
// misma orden (0.14.3 §12.2): no es ajeno. El intento nuevo se descarta (fallido_negocio con este
// código) y la activación sigue el intento que ya tiene ese aprovisionamiento_id.
const CodigoAprovisionamientoPropio = "aprovisionamiento_propio"

// EsperaIntentoSinRespuesta: cuánto espera el intento nuevo mientras se reintenta primero el intento
// de Iris de la misma orden que salió sin respuesta.
const EsperaIntentoSinRespuesta = time.Minute

// aprovisionamientoEnCurso decide el 409 aprovisionamiento_en_curso (0.14.3 §12.2, §10.13 puntos 4 a 6):
//   - si extensions.aprovisionamiento_id es de otro intento de Iris de la misma orden, no es ajeno:
//     el nuevo queda fallido con aprovisionamiento_propio y la activación sigue el anterior;
//   - si la orden tiene otro intento de Iris enviado sin respuesta y todavía en la cola, primero se
//     reintenta ese (con su clave) y este espera: es temporal;
//   - si no, es ajeno (iniciado_por argos, iris de otra orden o sin dato): fallido con la causa, sin
//     seguirlo (no se relaciona su aprovisionamiento_id con la orden ni se toma su resultado).
func (m *Modulo) aprovisionamientoEnCurso(ctx context.Context, f pendientesbd.IntegracionPendiente, er *conector.ErrorRemoto, r *resultadoIntento) {
	q := nucleo.BDDe(ctx)
	orden, err := strconv.ParseInt(f.EntidadID, 10, 64)
	if q == nil || f.Entidad != "orden" || err != nil {
		return
	}
	iniciado := er.Extensiones["iniciado_por"]
	if iniciado != "argos" && iniciado != "iris" {
		iniciado = "desconocido"
	}
	aprov, errID := strconv.ParseInt(er.Extensiones["aprovisionamiento_id"], 10, 64)
	if errID == nil && aprov > 0 && iniciado != "argos" {
		var propio bool
		err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM campo.activaciones
			WHERE orden_id = $1 AND aprovisionamiento_id = $2 AND idempotency_key <> $3)
			OR EXISTS (SELECT 1 FROM integracion.pendientes
			WHERE operacion = $4 AND entidad = 'orden' AND entidad_id = $5 AND id <> $6
			  AND estado = 'hecho' AND respuesta->>'aprovisionamiento_id' = $2::text)`,
			orden, aprov, f.IdempotencyKey, OpAprovisionar, f.EntidadID, f.ID).Scan(&propio)
		if err != nil {
			slog.WarnContext(ctx, "pendientes: no se pudo ver si el aprovisionamiento en curso es de Iris", "pendiente", f.ID, "err", err)
			r.Negocio, r.Espera = false, EsperaIntentoSinRespuesta
			return
		}
		if propio {
			r.Negocio, r.Codigo = true, CodigoAprovisionamientoPropio
			// La activación lo lee de la respuesta guardada para seguir el intento anterior.
			r.respuesta = fmt.Appendf(nil, `{"aprovisionamiento_id":%d}`, aprov)
			r.Error = fmt.Sprintf("%s: el aprovisionamiento %d es de otro intento de Iris de esta orden; se sigue ese y se descarta este", CodigoAprovisionamientoPropio, aprov)
			return
		}
	}
	var sinRespuesta bool
	err = q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM integracion.pendientes
		WHERE operacion = $1 AND entidad = 'orden' AND entidad_id = $2 AND id <> $3
		  AND envio_sin_respuesta AND estado IN ('pendiente', 'en_curso'))`,
		OpAprovisionar, f.EntidadID, f.ID).Scan(&sinRespuesta)
	if err != nil || sinRespuesta {
		// Primero el intento propio sin respuesta (la clave guardada en Argos dice si es el mismo).
		r.Negocio, r.Espera = false, EsperaIntentoSinRespuesta
		r.Error += "; se reintenta primero el intento de Iris de esta orden que salió sin respuesta"
		return
	}
	r.Error += "; ajeno (iniciado_por: " + iniciado + "), Iris no lo sigue"
	if errID == nil && aprov > 0 {
		r.Error += fmt.Sprintf(", aprovisionamiento_id %d", aprov)
	}
}
