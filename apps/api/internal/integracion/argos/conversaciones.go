package argos

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
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

func (m *Modulo) clienteAtencion(ctx context.Context, a AtencionIris) (*Cliente, error) {
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
type CuentaBancariaArgos struct {
	ID        int64           `json:"id"`
	Nombre    string          `json:"nombre"`
	Banco     json.RawMessage `json:"banco"`
	Numero    string          `json:"numero"`
	Moneda    string          `json:"moneda"`
	EmpresaID *int64          `json:"empresa_id"`
}

// BancoArgos es un banco de origen (GET /bancos).
type BancoArgos struct {
	ID     int64  `json:"id"`
	Nombre string `json:"nombre"`
}

// CuentasBancarias: GET /cuentas-bancarias?activa=true (§7.2, cuentas_bancarias.ver).
func (m *Modulo) CuentasBancarias(ctx context.Context, a AtencionIris) ([]CuentaBancariaArgos, error) {
	cli, err := m.clienteAtencion(ctx, a)
	if err != nil {
		return nil, err
	}
	p, err := conector.ListaDe[CuentaBancariaArgos](ctx, cli.Cliente, "/cuentas-bancarias", url.Values{"activa": {"true"}, "limit": {"100"}})
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
