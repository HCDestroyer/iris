package mensajeria

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Cuentas bancarias de la empresa y bancos de origen (lecturas de Argos, contrato §7.2) para el
// formulario de la boleta. No se guardan en Iris.

// CuentaBancaria es una cuenta de la empresa que recibe depósitos.
type CuentaBancaria struct {
	ID     int64  `json:"id"`
	Nombre string `json:"nombre"`
	Banco  string `json:"banco"`
	Numero string `json:"numero"`
	Moneda string `json:"moneda"`
}

// BancoOrigen es un banco desde el que se deposita o transfiere.
type BancoOrigen struct {
	ID     int64  `json:"id"`
	Nombre string `json:"nombre"`
}

// ListaCuentasBancarias es { data, nextCursor }.
type ListaCuentasBancarias struct {
	Data       []CuentaBancaria `json:"data"`
	NextCursor *string          `json:"nextCursor" nullable:"true"`
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
	return auditoria.Nuevo(auditoria.Caso[int64, alcanceConv]{
		Accion: "conversaciones.cuentas_bancarias", Permiso: nucleo.PermConversacionesRegistrarBoleta, AuditarExito: sinAuditar[alcanceConv],
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
// cerrado: una cuenta sin empresa_id o de otra empresa no se ofrece ni se acepta.
func cuentaParaBoleta(c argos.CuentaBancariaArgos, empresa int64) bool {
	return c.RecibeDe(empresa) && (c.Moneda == "" || c.Moneda == "GTQ")
}

// cuentaNoValida: la cuenta de la boleta no es una de las que la empresa de la conversación ofrece.
func cuentaNoValida() *nucleo.Problema {
	return campoConv("cuentaBancariaId", "Esa cuenta no es una cuenta activa de la empresa de la conversación", "cuenta_de_la_empresa")
}

// cuentasLeidas es lo que respondió Argos al leer las cuentas antes de abrir la transacción de la
// boleta (sin llamadas de red con la conversación bloqueada).
type cuentasLeidas struct {
	cuentas []argos.CuentaBancariaArgos
	err     error
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

// RegistrarBoleta es el camino de una persona: lee las cuentas de Argos antes de la transacción y
// registra la boleta con ese dato. Si Argos no responde, la boleta se registra igual y sigue su
// camino de reintentos (regla 4): quien valida al final es Argos en POST /comprobantes.
func (m *Modulo) RegistrarBoleta(ctx context.Context, in EntradaBoleta) (Boleta, error) {
	if m.cfg.Argos != nil {
		// Solo para leer las cuentas de la empresa de la conversación; si no la ve o no tiene el
		// permiso, el caso de uso responde (y audita) el error de siempre.
		if a, err := m.permisoBoleta.Ejecutar(ctx, in.ID); err == nil {
			cs, err := m.cfg.Argos.CuentasBancarias(ctx, atencionDe(a, in.ID))
			in.cuentas = &cuentasLeidas{cuentas: cs, err: err}
		}
	}
	return m.Conv.Boleta.Ejecutar(ctx, in)
}

// argosNoResponde: Argos no contestó de verdad esa lectura (red o plazo, 429 o 5xx suyos). Una
// respuesta 200 que Iris no pudo decodificar no es una caída: es un desajuste de contrato.
func argosNoResponde(err error) bool {
	return conector.EsCaido(err) && !conector.EsIlegible(err)
}

// problemaCuentas es el error para la persona cuando no se pudieron leer las cuentas de Argos.
func problemaCuentas(err error) error {
	if conector.EsIlegible(err) {
		return nucleo.NuevoProblema(http.StatusBadGateway, "argos_respuesta_ilegible", "Respuesta de Argos ilegible",
			"Argos respondió las cuentas bancarias en una forma que Iris no pudo leer; avisa a soporte")
	}
	return argos.Problema(err)
}

// cuentaEnLista: la cuenta está entre las que la empresa ofrece para depósitos.
func cuentaEnLista(cs []argos.CuentaBancariaArgos, empresa, cuenta int64) bool {
	for _, k := range cs {
		if k.ID == cuenta && cuentaParaBoleta(k, empresa) {
			return true
		}
	}
	return false
}

// decidirCuenta dice, con lo que respondió Argos al leer las cuentas, si la boleta se registra y con
// qué marca. Sin empresa (0) nunca: no se registra una boleta con cuenta bancaria. Argos caído o el
// conector apagado: se registra sin validar (el despacho la revalida). Una respuesta ilegible o un
// rechazo de Argos: no se registra.
func decidirCuenta(empresa int64, l cuentasLeidas, cuenta int64) (string, error) {
	switch {
	case empresa == 0:
		return "", cuentaNoValida()
	case l.err == nil:
		if !cuentaEnLista(l.cuentas, empresa, cuenta) {
			return "", cuentaNoValida()
		}
		return CuentaValidada, nil
	case conectorApagado(l.err):
		return CuentaSinConector, nil
	case argosNoResponde(l.err):
		return CuentaSinValidar, nil
	}
	return "", problemaCuentas(l.err)
}

// validarCuentaBoleta comprueba que la cuenta sea de la empresa de la conversación, esté activa y
// reciba depósitos. Con las cuentas ya leídas (una persona) no llama a Argos. Sin lectura previa (el
// asistente de IA) lee aquí y falla cerrado: sin poder leer las cuentas no registra.
func (m *Modulo) validarCuentaBoleta(ctx context.Context, a alcanceConv, c *mensajeriabd.MensajeriaConversacion, in EntradaBoleta) (string, error) {
	empresa := empresaDeConv(*c)
	if empresa == 0 {
		return "", cuentaNoValida()
	}
	l := in.cuentas
	if l == nil || a.asistente {
		alc := a
		alc.empresa = empresa
		cs, err := m.cfg.Argos.CuentasBancarias(ctx, atencionDe(alc, c.ID))
		if err != nil {
			return "", problemaCuentas(err)
		}
		l = &cuentasLeidas{cuentas: cs}
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
	at := argos.AtencionIris{Actor: "usuario:" + itoa(a.usuario), Conversacion: "iris:bandeja", EmpresaID: a.empresa}
	if in.ConversacionID != 0 {
		at = atencionDe(a, in.ConversacionID)
	}
	cs, err := m.cfg.Argos.CuentasBancarias(ctx, at)
	if err != nil {
		return ListaCuentasBancarias{}, problemaCuentas(err)
	}
	out := ListaCuentasBancarias{Data: []CuentaBancaria{}}
	for _, c := range cs {
		if !cuentaParaBoleta(c, a.empresa) {
			continue
		}
		out.Data = append(out.Data, CuentaBancaria{ID: c.ID, Nombre: c.Nombre, Banco: c.Banco, Numero: c.Numero, Moneda: c.Moneda})
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
