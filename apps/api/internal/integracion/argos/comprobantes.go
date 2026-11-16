package argos

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Aprobar y rechazar comprobantes desde la conversación (contrato 0.7.0 §7.7, pedido A23). Una
// persona de Iris transmite la decisión; decide el usuario de Argos vinculado a X-Iris-Actor
// (usuario:<id>). El asistente de IA nunca puede. Las dos llamadas son sincrónicas y nunca van por
// integracion.pendientes: quien decide necesita la respuesta al momento.

// Comprobante es el comprobante completo de Argos (GET /comprobantes/:id y la respuesta 200 de
// aprobar y rechazar). Solo los campos que usa Iris.
type Comprobante struct {
	ID         int64          `json:"id"`
	EmpresaID  *int64         `json:"empresa_id"`
	ClienteID  *int64         `json:"cliente_id"`
	ServicioID *int64         `json:"servicio_id"`
	Monto      conector.Texto `json:"monto"`
	Canal      string         `json:"canal"`
	// Estado: pendiente, aprobado o rechazado.
	Estado string `json:"estado"`
	// PagoID: el pago creado al aprobar (null al rechazar).
	PagoID        *int64  `json:"pago_id"`
	MotivoRechazo *string `json:"motivo_rechazo"`
	CausaRechazo  *string `json:"causa_rechazo"`
	// VerificadoPorNombre: el usuario de Argos que decidió (el vinculado, o caja en el panel).
	VerificadoPorNombre *string `json:"verificado_por_nombre"`
	// IrisActor e IrisConversacion: las cabeceras de §7.1 con que se decidió desde Iris (null si
	// decidió caja en el panel de Argos).
	IrisActor        *string `json:"iris_actor"`
	IrisConversacion *string `json:"iris_conversacion"`
}

// Estados de un comprobante en Argos.
const (
	ComprobantePendiente = "pendiente"
	ComprobanteAprobado  = "aprobado"
	ComprobanteRechazado = "rechazado"
)

// CanalComprobanteIris es el canal de los comprobantes que registró Iris (los únicos que se
// resuelven con el token de Iris).
const CanalComprobanteIris = "iris"

// ClaveAprobar es la Idempotency-Key de aprobar (§7.7): una por comprobante. La huella de Argos
// incluye el actor: la misma clave con otro actor responde 422 idempotency_key_reutilizada, nunca
// un replay.
func ClaveAprobar(comprobanteID int64) string {
	return "iris:aprobar:" + strconv.FormatInt(comprobanteID, 10)
}

// ClaveRechazar es la Idempotency-Key de rechazar (§7.7).
func ClaveRechazar(comprobanteID int64) string {
	return "iris:rechazar:" + strconv.FormatInt(comprobanteID, 10)
}

// cuerpoAprobar y cuerpoRechazar: structs (no mapas) para que un reintento mande exactamente los
// mismos bytes (§3.1: la huella es el cuerpo JSON crudo).
type cuerpoAprobar struct {
	Facturas []int64 `json:"facturas,omitempty"`
	Nota     string  `json:"nota,omitempty"`
}

type cuerpoRechazar struct {
	Motivo string `json:"motivo"`
	Causa  string `json:"causa,omitempty"`
}

// EsperasEnCurso son los reintentos cortos de un 409 idempotencia_en_curso (§3.1): la primera
// petición con esa clave sigue en curso en Argos. Variable para que las pruebas no esperen.
var EsperasEnCurso = []time.Duration{300 * time.Millisecond, 700 * time.Millisecond, 1500 * time.Millisecond}

// EsEnCurso indica si Argos respondió 409 idempotencia_en_curso.
func EsEnCurso(err error) bool {
	var er *ErrorArgos
	return errors.As(err, &er) && er.Status == http.StatusConflict && strings.HasPrefix(er.Codigo, "idempotencia_en_")
}

// conAtencion agrega las cabeceras de §7.1 (y la empresa activa).
func (c *Cliente) conAtencion(a AtencionIris) *conector.Cliente {
	cli := c.ConCabecera(CabeceraActor, a.Actor).ConCabecera(CabeceraConversacion, a.Conversacion)
	if a.EmpresaID != 0 {
		cli = cli.ConCabecera(CabeceraEmpresaArgos, strconv.FormatInt(a.EmpresaID, 10))
	}
	return cli
}

func (c *Cliente) decidir(ctx context.Context, a AtencionIris, ruta string, cuerpo any, clave string) (Comprobante, error) {
	cli := c.conAtencion(a)
	for i := 0; ; i++ {
		var r Comprobante
		err := cli.Escribir(ctx, http.MethodPost, ruta, cuerpo, clave, &conector.Recurso{Destino: &r})
		if err == nil {
			return r, nil
		}
		if !EsEnCurso(err) || i >= len(EsperasEnCurso) {
			return Comprobante{}, err
		}
		select {
		case <-ctx.Done():
			return Comprobante{}, err
		case <-time.After(EsperasEnCurso[i]):
		}
	}
}

// AprobarComprobante: POST /comprobantes/:id/aprobar { facturas?, nota? } con X-Iris-Actor,
// X-Iris-Conversacion e Idempotency-Key iris:aprobar:<id>. Sin monto ni fecha: se aprueba por el
// monto del comprobante. Reintenta con espera corta un 409 idempotencia_en_curso.
func (c *Cliente) AprobarComprobante(ctx context.Context, a AtencionIris, id int64, facturas []int64, nota string) (Comprobante, error) {
	return c.decidir(ctx, a, "/comprobantes/"+strconv.FormatInt(id, 10)+"/aprobar", cuerpoAprobar{Facturas: facturas, Nota: nota}, ClaveAprobar(id))
}

// RechazarComprobante: POST /comprobantes/:id/rechazar { motivo, causa? } con las cabeceras de §7.1
// e Idempotency-Key iris:rechazar:<id>.
func (c *Cliente) RechazarComprobante(ctx context.Context, a AtencionIris, id int64, motivo, causa string) (Comprobante, error) {
	return c.decidir(ctx, a, "/comprobantes/"+strconv.FormatInt(id, 10)+"/rechazar", cuerpoRechazar{Motivo: motivo, Causa: causa}, ClaveRechazar(id))
}

// ObtenerComprobante: GET /comprobantes/:id (§7.2), para sincronizar el estado local cuando Argos
// dice que ya se revisó.
func (c *Cliente) ObtenerComprobante(ctx context.Context, a AtencionIris, id int64) (Comprobante, error) {
	var r Comprobante
	err := c.conAtencion(a).Pedir(ctx, http.MethodGet, "/comprobantes/"+strconv.FormatInt(id, 10), nil, nil, true, &conector.Recurso{Destino: &r})
	return r, err
}

// SinAprobacion: el conector Argos no informa contrato ≥ 0.7.0.
func SinAprobacion() *nucleo.Problema {
	return nucleo.NuevoProblema(http.StatusUnprocessableEntity, "argos_sin_aprobacion", "Argos no lo permite todavía",
		"El Argos conectado no habla el contrato "+ContratoAprobarComprobantes+": actualiza Argos para aprobar y rechazar pagos desde Iris y para vincular tu usuario de Argos")
}

// ApruebaComprobantes indica si el conector Argos activo informa contrato ≥ 0.7.0 (false sin
// conector activo). q puede ser una transacción o el pool.
func (m *Modulo) ApruebaComprobantes(ctx context.Context, q conector.DBTX) (bool, error) {
	var contrato *string
	err := q.QueryRow(ctx, `SELECT contrato_remoto FROM integracion.conectores WHERE sistema = $1 AND estado = 'activo'
		ORDER BY id DESC LIMIT 1`, Sistema).Scan(&contrato)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return SoportaAprobarComprobantes(contrato), err
}

// clienteDecision: el cliente del conector activo, que debe informar contrato ≥ 0.7.0.
func (m *Modulo) clienteDecision(ctx context.Context) (*Cliente, error) {
	bd := nucleo.BDDe(ctx)
	if bd == nil {
		return nil, nucleo.ErrSinBD
	}
	c, err := m.conectorActivo(ctx, bd)
	if err != nil {
		return nil, err
	}
	if !SoportaAprobarComprobantes(c.ContratoRemoto) {
		return nil, SinAprobacion()
	}
	return m.clienteDe(c), nil
}

// AprobarComprobante aprueba el comprobante en Argos con el conector activo (§7.7).
func (m *Modulo) AprobarComprobante(ctx context.Context, a AtencionIris, id int64, facturas []int64, nota string) (Comprobante, error) {
	cli, err := m.clienteDecision(ctx)
	if err != nil {
		return Comprobante{}, err
	}
	return cli.AprobarComprobante(ctx, a, id, facturas, nota)
}

// RechazarComprobante rechaza el comprobante en Argos con el conector activo (§7.7).
func (m *Modulo) RechazarComprobante(ctx context.Context, a AtencionIris, id int64, motivo, causa string) (Comprobante, error) {
	cli, err := m.clienteDecision(ctx)
	if err != nil {
		return Comprobante{}, err
	}
	return cli.RechazarComprobante(ctx, a, id, motivo, causa)
}

// ObtenerComprobante lee el comprobante de Argos con el conector activo (§7.2).
func (m *Modulo) ObtenerComprobante(ctx context.Context, a AtencionIris, id int64) (Comprobante, error) {
	bd := nucleo.BDDe(ctx)
	if bd == nil {
		return Comprobante{}, nucleo.ErrSinBD
	}
	cli, err := m.ClienteActivo(ctx, bd)
	if err != nil {
		return Comprobante{}, err
	}
	return cli.ObtenerComprobante(ctx, a, id)
}
