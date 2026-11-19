package mensajeria

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Cuentas bancarias de la empresa (contrato con Argos 0.8.0 §7.8, B5) y bancos de origen. Las cuentas
// son de Argos: Iris las lee de su instantánea de lectura (integracion.argos_cuentas_bancarias, que el
// conector refresca por webhook, por la relectura periódica y al usarla si tiene más de 24 h) y, antes
// de registrar una boleta, relee la cuenta elegida en Argos. El filtro es del servidor: solo cuentas
// activas, de la empresa de la conversación y en quetzales; sin empresa, ninguna.

// CuentaBancaria es una cuenta de la empresa que recibe depósitos.
type CuentaBancaria struct {
	ID     int64  `json:"id"`
	Nombre string `json:"nombre" doc:"El alias o, sin alias, el banco y los últimos 4 dígitos"`
	Banco  string `json:"banco" doc:"Nombre del banco (banco_nombre de Argos)"`
	Alias  string `json:"alias" doc:"El nombre que la empresa le puso en Argos; puede venir vacío"`
	Numero string `json:"numero"`
	Moneda string `json:"moneda"`
	// Titular y Tipo: contrato 0.8.0; null con un Argos anterior o en una cuenta que nadie editó.
	Titular  *string `json:"titular" nullable:"true" doc:"A nombre de quién está la cuenta; null si Argos no lo tiene"`
	Tipo     *string `json:"tipo" nullable:"true" enum:"monetaria,ahorro" doc:"null si Argos no lo informó"`
	SeOfrece bool    `json:"seOfrece" doc:"Tiene titular: «Enviar cuentas» y el asistente se la pueden dar al cliente. Sin titular solo sirve para registrar una boleta ya depositada (§10.2, punto 14)"`
}

// BancoOrigen es un banco desde el que se deposita o transfiere.
type BancoOrigen struct {
	ID     int64  `json:"id"`
	Nombre string `json:"nombre"`
}

// ListaCuentasBancarias es { data, nextCursor } y cuándo se leyó de Argos.
type ListaCuentasBancarias struct {
	Data           []CuentaBancaria `json:"data"`
	NextCursor     *string          `json:"nextCursor" nullable:"true"`
	SincronizadoEn *time.Time       `json:"sincronizadoEn" nullable:"true" doc:"Cuándo se leyó de Argos la lista de cuentas de la empresa (null si la conversación no tiene empresa)"`
}

// ListaBancos es { data, nextCursor }.
type ListaBancos struct {
	Data       []BancoOrigen `json:"data"`
	NextCursor *string       `json:"nextCursor" nullable:"true"`
}

// EntradaCuentasBancarias es GET /conversaciones/cuentas-bancarias.
type EntradaCuentasBancarias struct {
	ConversacionID int64 `query:"conversacionId" required:"false" minimum:"1" doc:"La conversación de la boleta: lista las cuentas de la empresa de esa conversación (la misma contra la que se valida al registrar). Sin él, las de la empresa activa"`
}

// nuevoPermisoBoleta: el alcance de quien pide. Con una conversación (id ≠ 0) exige que la vea y
// devuelve su empresa en vez de la activa.
func nuevoPermisoBoleta() *auditoria.Caso[int64, alcanceConv] {
	return nuevoAlcanceCuentas("conversaciones.cuentas_bancarias", nucleo.PermConversacionesRegistrarBoleta)
}

func nuevoAlcanceCuentas(accion string, permiso nucleo.Permiso) *auditoria.Caso[int64, alcanceConv] {
	return auditoria.Nuevo(auditoria.Caso[int64, alcanceConv]{
		Accion: accion, Permiso: permiso, AuditarExito: sinAuditar[alcanceConv],
		Logica: func(ctx context.Context, t *auditoria.Tx, id int64) (alcanceConv, error) {
			a, err := alcanceConvDe(ctx)
			if err != nil || id == 0 {
				return a, err
			}
			c, err := convVisible(ctx, mensajeriabd.New(t), a, id, false)
			if err != nil {
				return alcanceConv{}, err
			}
			a.empresa = empresaDeConv(c)
			return a, nil
		},
	})
}

// cuentaParaBoleta: la cuenta recibe depósitos de esa empresa (activa, suya y en quetzales). Falla
// cerrado: una cuenta sin empresa_id, de otra empresa o sin moneda no se ofrece ni se acepta.
func cuentaParaBoleta(c argos.CuentaBancariaArgos, empresa int64) bool {
	return c.RecibeDe(empresa) && c.Moneda == "GTQ"
}

// nombreCuentaModelo: cómo ve el modelo una cuenta: banco, tipo y los últimos 4 dígitos. Nunca el
// alias (texto libre de la empresa) ni el titular.
func nombreCuentaModelo(c argos.CuentaBancariaArgos) string {
	n := datoCuenta(c.Banco, 60)
	if c.Tipo != nil {
		n += " " + *c.Tipo
	}
	if d := soloDigitos(c.Numero); d != "" {
		if len(d) > 4 {
			d = d[len(d)-4:]
		}
		n += " …" + d
	}
	return strings.TrimSpace(n)
}

// cuentaParaOfrecer: además tiene titular (§10.2, punto 14: decirle a un cliente dónde depositar sin
// decirle a nombre de quién facilita un fraude).
func cuentaParaOfrecer(c argos.CuentaBancariaArgos, empresa int64) bool {
	return cuentaParaBoleta(c, empresa) && c.Titular != nil && strings.TrimSpace(*c.Titular) != ""
}

// cuentaNoValida: la cuenta de la boleta no es una de las que la empresa de la conversación ofrece.
func cuentaNoValida() *nucleo.Problema {
	return campoConv("cuentaBancariaId", "Esa cuenta no es una cuenta activa de la empresa de la conversación", "cuenta_de_la_empresa")
}

// cuentaDesactivada: la cuenta es de la empresa pero Argos la tiene desactivada (activa: false).
func cuentaDesactivada() *nucleo.Problema {
	return campoConv("cuentaBancariaId", "Esa cuenta está desactivada en Argos: elige otra. Si el cliente ya depositó ahí, caja registra el pago a mano en Argos", "inactiva")
}

// cuentaLeida es lo que respondió Argos al releer la cuenta de la boleta antes de abrir la transacción
// (sin llamadas de red con la conversación bloqueada).
type cuentaLeida struct {
	cuenta argos.CuentaBancariaArgos
	err    error
}

// Marca de la boleta (columna cuenta_validacion y auditoría): si Argos validó la cuenta al registrarla.
const (
	CuentaValidada   = "validada"
	CuentaSinValidar = "sin_validar_argos_no_disponible"
	// CuentaSinConector: el conector con Argos está apagado; la boleta queda por enviar como siempre.
	CuentaSinConector = "sin_validar_conector_apagado"
)

// conectorApagado: el error es el 503 conector_apagado (no hay conector con Argos activo).
func conectorApagado(err error) bool {
	var p *nucleo.Problema
	return errors.As(err, &p) && p.Codigo() == "conector_apagado"
}

// RegistrarBoleta es el camino de una persona: relee en Argos la cuenta elegida antes de la
// transacción y registra la boleta con ese dato. Si Argos no responde, la boleta se registra igual y
// sigue su camino de reintentos (regla 4): el despacho la revalida antes de mandarla.
func (m *Modulo) RegistrarBoleta(ctx context.Context, in EntradaBoleta) (Boleta, error) {
	if m.cfg.Argos != nil && in.CuentaBancariaID > 0 {
		// Solo si ve la conversación y tiene el permiso; si no, el caso de uso responde (y audita) el
		// error de siempre sin que Iris llame a Argos.
		if _, err := m.permisoBoleta.Ejecutar(ctx, in.ID); err == nil {
			k, err := m.cfg.Argos.ReleerCuentaBancaria(ctx, in.CuentaBancariaID)
			in.cuenta = &cuentaLeida{cuenta: k, err: err}
		}
	}
	return m.Conv.Boleta.Ejecutar(ctx, in)
}

// argosNoResponde: Argos no contestó de verdad esa lectura (red o plazo, 429 o 5xx suyos). Una
// respuesta 200 que Iris no pudo decodificar no es una caída: es un desajuste de contrato.
func argosNoResponde(err error) bool {
	return conector.EsCaido(err) && !conector.EsIlegible(err)
}

// argosNiegaCuenta: Argos dice que la cuenta no existe (404) o es de una empresa fuera del alcance (403).
func argosNiegaCuenta(err error) bool {
	return conector.EsNoEncontrado(err) || argos.EsFueraDeAlcance(err)
}

// problemaCuentas es el error para la persona cuando no se pudieron leer las cuentas de Argos.
func problemaCuentas(err error) error {
	if conector.EsIlegible(err) {
		return nucleo.NuevoProblema(http.StatusBadGateway, "argos_respuesta_ilegible", "Respuesta de Argos ilegible",
			"Argos respondió las cuentas bancarias en una forma que Iris no pudo leer; avisa a soporte")
	}
	if errors.Is(err, argos.ErrCuentasViejas) {
		return nucleo.NuevoProblema(http.StatusServiceUnavailable, "argos_no_disponible", "Argos no responde",
			"Argos no respondió y las cuentas guardadas tienen más de 24 horas: no se ofrecen hasta volver a leerlas")
	}
	return argos.Problema(err)
}

// decidirCuenta dice, con lo que respondió Argos al releer la cuenta, si la boleta se registra y con
// qué marca. Sin empresa (0) nunca. Argos caído o el conector apagado: se registra sin validar (el
// despacho la revalida). Una cuenta que Argos no tiene, de otra empresa o desactivada: no se registra.
func decidirCuenta(empresa int64, l cuentaLeida, cuenta int64) (string, error) {
	switch {
	case empresa == 0:
		return "", cuentaNoValida()
	case l.err == nil:
		k := l.cuenta
		if k.ID != cuenta {
			return "", cuentaNoValida()
		}
		if !k.Activa && k.EmpresaID != nil && *k.EmpresaID == empresa {
			return "", cuentaDesactivada()
		}
		if !cuentaParaBoleta(k, empresa) {
			return "", cuentaNoValida()
		}
		return CuentaValidada, nil
	case conectorApagado(l.err):
		return CuentaSinConector, nil
	case argosNiegaCuenta(l.err):
		return "", cuentaNoValida()
	case argosNoResponde(l.err):
		return CuentaSinValidar, nil
	}
	return "", problemaCuentas(l.err)
}

// validarCuentaBoleta comprueba que la cuenta sea de la empresa de la conversación, esté activa y
// reciba depósitos. Con la cuenta ya releída (una persona, el asistente) no llama a Argos. Sin
// relectura previa la hace aquí; el asistente de IA falla cerrado: sin poder leerla no registra.
func (m *Modulo) validarCuentaBoleta(ctx context.Context, a alcanceConv, c *mensajeriabd.MensajeriaConversacion, in EntradaBoleta) (string, error) {
	empresa := empresaDeConv(*c)
	if empresa == 0 {
		return "", cuentaNoValida()
	}
	l := in.cuenta
	if l == nil {
		k, err := m.cfg.Argos.ReleerCuentaBancaria(ctx, in.CuentaBancariaID)
		l = &cuentaLeida{cuenta: k, err: err}
	}
	if a.asistente && l.err != nil {
		if argosNiegaCuenta(l.err) {
			return "", cuentaNoValida()
		}
		return "", problemaCuentas(l.err)
	}
	marca, err := decidirCuenta(empresa, *l, in.CuentaBancariaID)
	if err == nil && marca != CuentaValidada {
		slog.WarnContext(ctx, "boleta: la cuenta queda sin validar", "conversacion_id", c.ID, "cuenta_bancaria_id", in.CuentaBancariaID, "marca", marca)
	}
	return marca, err
}

// MotivoCuentaBoleta: el detalle de una boleta que falla en el despacho porque su cuenta, registrada
// sin validar, no es una cuenta activa en quetzales de la empresa.
const MotivoCuentaBoleta = "La cuenta bancaria no es una cuenta activa en quetzales de la empresa"

// MotivoCuentaInactiva: el detalle de una boleta que Argos rechazó porque la cuenta se desactivó
// después del depósito (422 rule inactiva, §7.8). Iris no reintenta: caja la registra a mano.
const MotivoCuentaInactiva = "La cuenta bancaria se desactivó en Argos: caja registra este pago a mano en Argos"

// aCuentaAPI arma la cuenta del panel.
func aCuentaAPI(c argos.CuentaBancariaArgos, empresa int64) CuentaBancaria {
	return CuentaBancaria{ID: c.ID, Nombre: c.Nombre, Banco: c.Banco, Alias: c.Alias, Numero: c.Numero, Moneda: c.Moneda, Titular: c.Titular, Tipo: c.Tipo,
		SeOfrece: cuentaParaOfrecer(c, empresa)}
}

// cuentasDe lee la instantánea de la empresa de at (relee Argos si tiene más de 24 h).
func (m *Modulo) cuentasDe(ctx context.Context, at argos.AtencionIris) (argos.CuentasEmpresa, error) {
	ce, err := m.cfg.Argos.CuentasDeEmpresa(ctx, at)
	if err != nil {
		return argos.CuentasEmpresa{}, problemaCuentas(err)
	}
	return ce, nil
}

// CuentasBancarias lista las cuentas activas en GTQ (Argos) de la empresa de la conversación o, sin
// conversación, de la empresa activa.
func (m *Modulo) CuentasBancarias(ctx context.Context, in EntradaCuentasBancarias) (ListaCuentasBancarias, error) {
	a, err := m.permisoBoleta.Ejecutar(ctx, in.ConversacionID)
	if err != nil {
		return ListaCuentasBancarias{}, err
	}
	if m.cfg.Argos == nil {
		return ListaCuentasBancarias{}, noDisponible("Leer las cuentas de Argos")
	}
	out := ListaCuentasBancarias{Data: []CuentaBancaria{}}
	if a.empresa == 0 {
		return out, nil
	}
	at := argos.AtencionIris{Actor: "usuario:" + itoa(a.usuario), Conversacion: "iris:bandeja", EmpresaID: a.empresa}
	if in.ConversacionID != 0 {
		at = atencionDe(a, in.ConversacionID)
	}
	ce, err := m.cuentasDe(ctx, at)
	if err != nil {
		return ListaCuentasBancarias{}, err
	}
	out.SincronizadoEn = ce.LeidoEn
	for _, c := range ce.Cuentas {
		if cuentaParaBoleta(c, a.empresa) {
			out.Data = append(out.Data, aCuentaAPI(c, a.empresa))
		}
	}
	return out, nil
}

// Bancos lista los bancos de origen (Argos).
func (m *Modulo) Bancos(ctx context.Context) (ListaBancos, error) {
	a, err := m.permisoBoleta.Ejecutar(ctx, 0)
	if err != nil {
		return ListaBancos{}, err
	}
	if m.cfg.Argos == nil {
		return ListaBancos{}, noDisponible("Leer los bancos de Argos")
	}
	bs, err := m.cfg.Argos.Bancos(ctx, argos.AtencionIris{Actor: "usuario:" + itoa(a.usuario), Conversacion: "iris:bandeja", EmpresaID: a.empresa})
	if err != nil {
		return ListaBancos{}, argos.Problema(err)
	}
	out := ListaBancos{Data: []BancoOrigen{}}
	for _, b := range bs {
		out.Data = append(out.Data, BancoOrigen{ID: b.ID, Nombre: b.Nombre})
	}
	return out, nil
}
