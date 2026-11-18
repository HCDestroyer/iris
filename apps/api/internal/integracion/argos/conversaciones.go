package argos

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Pagos y atención desde la bandeja de conversaciones (contrato 0.6.0 §7): buscar cliente por
// teléfono, registrar la boleta del cliente (comprobante) y pedir una promesa de pago. Iris nunca
// registra un pago ni una promesa: lo pide a Argos y Argos decide. Cada llamada lleva X-Iris-Actor
// (usuario:<id>) y X-Iris-Conversacion (§7.1).

// Cabeceras de §7.1.
const (
	CabeceraActor        = "X-Iris-Actor"
	CabeceraConversacion = "X-Iris-Conversacion"
)

// AtencionIris identifica quién pide y desde qué conversación (§7.1).
type AtencionIris struct {
	// Actor: «usuario:<id en Iris>» o «asistente_ia» (fase 2).
	Actor string
	// Conversacion: id de la conversación en Iris.
	Conversacion string
	// EmpresaID: la empresa activa en Argos (X-Empresa-Id; 0 = sin cabecera).
	EmpresaID int64
}

// Forma exacta de las cabeceras de §7.1 (contrato 0.7.1). Fuera de formato Argos responde 400
// cabecera_iris_requerida: Iris no llega a mandarlas.
var (
	reCabeceraActor        = regexp.MustCompile(`^(asistente_ia|usuario:[A-Za-z0-9_-]{1,64})$`)
	reCabeceraConversacion = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)
)

// ErrCabecerasIris: el actor o la conversación no tienen la forma de §7.1 (un error de Iris, no de Argos).
var ErrCabecerasIris = errors.New("argos: X-Iris-Actor o X-Iris-Conversacion fuera del formato del contrato (§7.1)")

// validar: el actor y la conversación tienen la forma de §7.1.
func (a AtencionIris) validar() error {
	if !reCabeceraActor.MatchString(a.Actor) || !reCabeceraConversacion.MatchString(a.Conversacion) {
		return ErrCabecerasIris
	}
	return nil
}

func (m *Modulo) clienteAtencion(ctx context.Context, a AtencionIris) (*Cliente, error) {
	if err := a.validar(); err != nil {
		return nil, err
	}
	bd := nucleo.BDDe(ctx)
	if bd == nil {
		return nil, nucleo.ErrSinBD
	}
	cli, err := m.ClienteActivo(ctx, bd)
	if err != nil {
		return nil, err
	}
	c := cli.Cliente.ConCabecera(CabeceraActor, a.Actor).ConCabecera(CabeceraConversacion, a.Conversacion)
	if a.EmpresaID != 0 {
		c = c.ConCabecera(CabeceraEmpresaArgos, strconv.FormatInt(a.EmpresaID, 10))
	}
	return &Cliente{Cliente: c}, nil
}

// ClienteTelefono es un cliente de GET /clientes?telefono= con dónde coincide el número.
type ClienteTelefono struct {
	ClienteArgos
	// CoincideEn: telefono, movil y/o contacto_servicio (§7.6).
	CoincideEn []string `json:"coincide_en"`
}

// BuscarPorTelefono: GET /clientes?telefono=<E.164> (§7.6), coincidencia exacta en Argos.
func (m *Modulo) BuscarPorTelefono(ctx context.Context, a AtencionIris, telefono string) ([]ClienteTelefono, error) {
	cli, err := m.clienteAtencion(ctx, a)
	if err != nil {
		return nil, err
	}
	p, err := conector.ListaDe[ClienteTelefono](ctx, cli.Cliente, "/clientes", url.Values{"telefono": {telefono}, "limit": {"10"}})
	if err != nil {
		return nil, err
	}
	return p.Data, nil
}

// EntradaComprobante es la boleta que Iris manda a Argos (§7.2).
type EntradaComprobante struct {
	ClienteID        int64
	ServicioID       *int64
	Metodo           string
	CuentaBancariaID int64
	BancoOrigenID    *int64
	Referencia       string
	// Monto: decimal con 2 decimales.
	Monto string
	// FechaDeposito: AAAA-MM-DD.
	FechaDeposito string
	Archivo       conector.Archivo
	Nota          *string
	// Clave: Idempotency-Key (iris:boleta:<id>).
	Clave string
}

// ComprobanteArgos es la respuesta 201 de POST /comprobantes.
type ComprobanteArgos struct {
	ID        int64  `json:"id"`
	Estado    string `json:"estado"`
	EmpresaID *int64 `json:"empresa_id"`
}

// RegistrarComprobante: POST /comprobantes (multipart; canal_contacto whatsapp, leido_por persona).
func (m *Modulo) RegistrarComprobante(ctx context.Context, a AtencionIris, in EntradaComprobante) (ComprobanteArgos, error) {
	cli, err := m.clienteAtencion(ctx, a)
	if err != nil {
		return ComprobanteArgos{}, err
	}
	campos := map[string]string{"cliente_id": strconv.FormatInt(in.ClienteID, 10), "metodo": in.Metodo,
		"cuenta_bancaria_id": strconv.FormatInt(in.CuentaBancariaID, 10), "referencia": in.Referencia, "monto": in.Monto,
		"fecha_deposito": in.FechaDeposito, "canal_contacto": "whatsapp", "leido_por": "persona"}
	if in.ServicioID != nil {
		campos["servicio_id"] = strconv.FormatInt(*in.ServicioID, 10)
	}
	if in.BancoOrigenID != nil {
		campos["banco_origen_id"] = strconv.FormatInt(*in.BancoOrigenID, 10)
	}
	if in.Nota != nil {
		campos["nota"] = *in.Nota
	}
	if a.EmpresaID != 0 {
		campos["empresa_id"] = strconv.FormatInt(a.EmpresaID, 10)
	}
	var r ComprobanteArgos
	err = cli.EscribirMultipart(ctx, "/comprobantes", campos, in.Archivo, in.Clave, &r)
	return r, err
}

// CuentaBancariaArgos es una cuenta de la empresa que recibe depósitos (GET /cuentas-bancarias).
// Argos manda id, empresa_id, banco_id, banco_nombre, numero, alias, moneda y activa (más campos
// contables que Iris no usa). No manda «nombre» ni «banco»: Nombre se arma al leer.
type CuentaBancariaArgos struct {
	ID int64 `json:"id"`
	// EmpresaID: la empresa dueña. Argos siempre la manda; sin ella la cuenta no se ofrece.
	EmpresaID *int64 `json:"empresa_id"`
	// Banco: el nombre del banco (banco_nombre).
	Banco  string `json:"banco_nombre"`
	Numero string `json:"numero"`
	// Alias: el nombre que la empresa le puso a la cuenta; puede venir vacío.
	Alias  string `json:"alias"`
	Moneda string `json:"moneda"`
	Activa bool   `json:"activa"`
	// Nombre: el alias o, si viene vacío, el banco y los últimos 4 dígitos del número.
	Nombre string `json:"-"`
}

// UnmarshalJSON lee la cuenta y arma Nombre.
func (c *CuentaBancariaArgos) UnmarshalJSON(crudo []byte) error {
	type plano CuentaBancariaArgos
	var p plano
	if err := json.Unmarshal(crudo, &p); err != nil {
		return err
	}
	*c = CuentaBancariaArgos(p)
	c.Alias, c.Banco = strings.TrimSpace(c.Alias), strings.TrimSpace(c.Banco)
	c.Nombre = c.Alias
	if c.Nombre == "" {
		c.Nombre = strings.TrimSpace(c.Banco + " " + ultimosDigitos(c.Numero, 4))
	}
	return nil
}

// ultimosDigitos: «…1234» con los últimos n dígitos del número (vacío si no tiene dígitos).
func ultimosDigitos(numero string, n int) string {
	d := make([]rune, 0, len(numero))
	for _, r := range numero {
		if r >= '0' && r <= '9' {
			d = append(d, r)
		}
	}
	if len(d) == 0 {
		return ""
	}
	if len(d) > n {
		d = d[len(d)-n:]
	}
	return "…" + string(d)
}

// RecibeDe: la cuenta está activa y es de esa empresa. Falla cerrado: sin empresa_id, de otra
// empresa o sin empresa en la conversación (0), la cuenta no se ofrece ni se acepta.
func (c CuentaBancariaArgos) RecibeDe(empresa int64) bool {
	return empresa != 0 && c.EmpresaID != nil && *c.EmpresaID == empresa && c.Activa
}

// BancoArgos es un banco de origen (GET /bancos).
type BancoArgos struct {
	ID     int64  `json:"id"`
	Nombre string `json:"nombre"`
}

// CuentasBancarias: GET /cuentas-bancarias?activa=true (§7.2, cuentas_bancarias.ver). Argos responde
// un arreglo plano sin paginar (ignora limit): no se manda.
func (m *Modulo) CuentasBancarias(ctx context.Context, a AtencionIris) ([]CuentaBancariaArgos, error) {
	cli, err := m.clienteAtencion(ctx, a)
	if err != nil {
		return nil, err
	}
	p, err := conector.ListaDe[CuentaBancariaArgos](ctx, cli.Cliente, "/cuentas-bancarias", url.Values{"activa": {"true"}})
	return p.Data, err
}

// Bancos: GET /bancos (§7.2).
func (m *Modulo) Bancos(ctx context.Context, a AtencionIris) ([]BancoArgos, error) {
	cli, err := m.clienteAtencion(ctx, a)
	if err != nil {
		return nil, err
	}
	p, err := conector.ListaDe[BancoArgos](ctx, cli.Cliente, "/bancos", url.Values{"limit": {"200"}})
	return p.Data, err
}

// SimulacionPromesa es la respuesta de POST /promesas/simular (§7.4), tal cual para el panel.
type SimulacionPromesa struct {
	Resultado string          `json:"resultado"`
	Regla     *string         `json:"regla"`
	Motivo    *string         `json:"motivo"`
	Monto     json.RawMessage `json:"monto"`
	Limites   json.RawMessage `json:"limites"`
}

// SimularPromesa: POST /promesas/simular (lectura, sin auditar en Argos).
func (m *Modulo) SimularPromesa(ctx context.Context, a AtencionIris, servicioID int64, venceEn *time.Time) (SimulacionPromesa, error) {
	cli, err := m.clienteAtencion(ctx, a)
	if err != nil {
		return SimulacionPromesa{}, err
	}
	cuerpo := map[string]any{"servicio_id": servicioID}
	if venceEn != nil {
		cuerpo["vence_en"] = venceEn.UTC().Format(time.RFC3339)
	}
	var r SimulacionPromesa
	err = cli.Pedir(ctx, "POST", "/promesas/simular", nil, cuerpo, true, &r)
	return r, err
}

// PromesaArgos es la respuesta 201 de POST /promesas (también la rechazada).
type PromesaArgos struct {
	ID                  int64           `json:"id"`
	Estado              string          `json:"estado"`
	Monto               json.RawMessage `json:"monto"`
	ResultadoEvaluacion *struct {
		Regla  *string `json:"regla"`
		Motivo *string `json:"motivo"`
	} `json:"resultado_evaluacion"`
}

// CrearPromesa: POST /promesas { servicio_id, vence_en } con Idempotency-Key iris:promesa:<id>.
func (m *Modulo) CrearPromesa(ctx context.Context, a AtencionIris, servicioID int64, venceEn time.Time, clave string) (PromesaArgos, error) {
	cli, err := m.clienteAtencion(ctx, a)
	if err != nil {
		return PromesaArgos{}, err
	}
	var r PromesaArgos
	err = cli.Escribir(ctx, "POST", "/promesas", map[string]any{"servicio_id": servicioID, "vence_en": venceEn.UTC().Format(time.RFC3339)}, clave, &r)
	return r, err
}

// EstadoCuentaCliente: GET /clientes/:id/estado-cuenta de la empresa de la conversación (§2.1), en
// vivo y sin guardar, para el asistente de IA (contratos/conversaciones.md §16.10). Solo el saldo,
// la cantidad de facturas vencidas y el monto vencido. Nunca va al log.
func (m *Modulo) EstadoCuentaCliente(ctx context.Context, a AtencionIris, clienteID int64) (EstadoCuenta, error) {
	cli, err := m.clienteAtencion(ctx, a)
	if err != nil {
		return EstadoCuenta{}, err
	}
	e, err := cli.EstadoCuenta(ctx, clienteID, a.EmpresaID)
	if err != nil {
		return EstadoCuenta{}, err
	}
	monto := e.MontoVencido.Texto()
	if monto == nil {
		monto = e.TotalVencido.Texto()
	}
	return EstadoCuenta{Saldo: e.Saldo.Texto(), Moneda: e.Moneda, FacturasVencidas: e.CantidadVencidas(), MontoVencido: monto}, nil
}
