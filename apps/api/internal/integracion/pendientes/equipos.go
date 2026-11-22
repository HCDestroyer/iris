package pendientes

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
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
