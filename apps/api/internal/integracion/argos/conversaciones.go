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
	// Contacto: X-Iris-Contacto (§7.1, 0.10.1): «titular» o el contacto_id de Argos de quien habla.
	// Vacío = titular. Solo se manda a un Argos con contrato ≥ 0.10.1, donde es obligatoria.
	Contacto string
}

// Forma exacta de las cabeceras de §7.1 (contrato 0.7.1). Fuera de formato Argos responde 400
// cabecera_iris_requerida: Iris no llega a mandarlas.
var (
	reCabeceraActor        = regexp.MustCompile(`^(asistente_ia|usuario:[A-Za-z0-9_-]{1,64})$`)
	reCabeceraConversacion = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)
)

// ErrCabecerasIris: el actor o la conversación no tienen la forma de §7.1 (un error de Iris, no de Argos).
var ErrCabecerasIris = errors.New("argos: X-Iris-Actor o X-Iris-Conversacion fuera del formato del contrato (§7.1)")

// validar: el actor, la conversación y el contacto tienen la forma de §7.1.
func (a AtencionIris) validar() error {
	if !reCabeceraActor.MatchString(a.Actor) || !reCabeceraConversacion.MatchString(a.Conversacion) {
		return ErrCabecerasIris
	}
	if a.Contacto != "" && !reCabeceraContacto.MatchString(a.Contacto) {
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
	con, err := m.conectorActivo(ctx, bd)
	if err != nil {
		return nil, err
	}
	return &Cliente{Cliente: m.clienteDe(con).conAtencion(a), contrato: con.ContratoAcordado()}, nil
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

// CuentaBancariaArgos es una cuenta de la empresa que recibe depósitos (GET /cuentas-bancarias y
// GET /cuentas-bancarias/:id, §7.8). Argos manda id, empresa_id, banco_id, banco_nombre, numero,
// alias, moneda y activa, y desde 0.8.0 titular y tipo (más campos contables que Iris no guarda). No
// manda «nombre» ni «banco»: Nombre se arma al leer.
type CuentaBancariaArgos struct {
	ID int64 `json:"id"`
	// EmpresaID: la empresa dueña. Argos siempre la manda; sin ella la cuenta no se ofrece.
	EmpresaID *int64 `json:"empresa_id"`
	BancoID   *int64 `json:"banco_id"`
	// Banco: el nombre del banco (banco_nombre).
	Banco  string `json:"banco_nombre"`
	Numero string `json:"numero"`
	// Alias: el nombre que la empresa le puso a la cuenta; puede venir vacío.
	Alias string `json:"alias"`
	// Titular: a nombre de quién está (0.8.0); nil con un Argos anterior o en una cuenta vieja.
	Titular *string `json:"titular"`
	// Tipo: monetaria o ahorro (0.8.0); nil si no vino.
	Tipo   *string `json:"tipo"`
	Moneda string  `json:"moneda"`
	Activa bool    `json:"activa"`
	// Nombre: el alias o, si viene vacío, el banco y los últimos 4 dígitos del número.
	Nombre string `json:"-"`
	// SincronizadoEn: cuándo se leyó de Argos (instantánea).
	SincronizadoEn time.Time `json:"-"`
}

// UnmarshalJSON lee la cuenta y arma Nombre.
func (c *CuentaBancariaArgos) UnmarshalJSON(crudo []byte) error {
	type plano CuentaBancariaArgos
	var p plano
	if err := json.Unmarshal(crudo, &p); err != nil {
		return err
	}
	*c = CuentaBancariaArgos(p)
	c.armarNombre()
	return nil
}

// armarNombre: el alias o, sin alias, el banco y los últimos 4 dígitos.
func (c *CuentaBancariaArgos) armarNombre() {
	c.Alias, c.Banco = strings.TrimSpace(c.Alias), strings.TrimSpace(c.Banco)
	c.Nombre = c.Alias
	if c.Nombre == "" {
		c.Nombre = strings.TrimSpace(c.Banco + " " + ultimosDigitos(c.Numero, 4))
	}
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
	return cli.simularPromesa(ctx, servicioID, venceEn)
}

// SimularPromesa: POST /promesas/simular con las cabeceras de §7.1, con este cliente (pruebas contra
// un Argos falso; en producción va por Modulo.SimularPromesa con el conector activo).
func (c *Cliente) SimularPromesa(ctx context.Context, a AtencionIris, servicioID int64, venceEn *time.Time) (SimulacionPromesa, error) {
	if err := a.validar(); err != nil {
		return SimulacionPromesa{}, err
	}
	return (&Cliente{Cliente: c.conAtencion(a)}).simularPromesa(ctx, servicioID, venceEn)
}

func (c *Cliente) simularPromesa(ctx context.Context, servicioID int64, venceEn *time.Time) (SimulacionPromesa, error) {
	cuerpo := map[string]any{"servicio_id": servicioID}
	if venceEn != nil {
		cuerpo["vence_en"] = venceEn.UTC().Format(time.RFC3339)
	}
	var r SimulacionPromesa
	err := c.Pedir(ctx, "POST", "/promesas/simular", nil, cuerpo, true, &r)
	return r, err
}

// PromesaArgos es la respuesta 201 de POST /promesas (también la rechazada).
type PromesaArgos struct {
	ID     int64           `json:"id"`
	Estado string          `json:"estado"`
	Monto  json.RawMessage `json:"monto"`
	// MotivoReconexion (0.8.0, §7.3): fuera_de_horario | sin_personal en una reconexion_temporal;
	// null en una promesa normal o con un Argos anterior (que no lo manda). Leer con
	// MotivoReconexionValido.
	MotivoReconexion    *string `json:"motivo_reconexion"`
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
	return cli.crearPromesa(ctx, servicioID, venceEn, clave)
}

// CrearPromesa: POST /promesas con las cabeceras de §7.1, con este cliente (pruebas contra un Argos
// falso; en producción va por Modulo.CrearPromesa con el conector activo).
func (c *Cliente) CrearPromesa(ctx context.Context, a AtencionIris, servicioID int64, venceEn time.Time, clave string) (PromesaArgos, error) {
	if err := a.validar(); err != nil {
		return PromesaArgos{}, err
	}
	return (&Cliente{Cliente: c.conAtencion(a)}).crearPromesa(ctx, servicioID, venceEn, clave)
}

// crearPromesa: el cuerpo es siempre el mismo para el mismo pedido (Argos guarda la huella del cuerpo
// crudo y del X-Iris-Actor con la Idempotency-Key).
func (c *Cliente) crearPromesa(ctx context.Context, servicioID int64, venceEn time.Time, clave string) (PromesaArgos, error) {
	var r PromesaArgos
	err := c.Escribir(ctx, "POST", "/promesas", map[string]any{"servicio_id": servicioID, "vence_en": venceEn.UTC().Format(time.RFC3339)}, clave, &r)
	return r, err
}

// EstadoCuentaCliente: GET /clientes/:id/estado-cuenta de la empresa de la conversación (§2.1), en
// vivo y sin guardar, para el asistente de IA (contratos/conversaciones.md §16.10). Solo el saldo,
// la cantidad de facturas vencidas y el monto vencido. Nunca va al log.
func (m *Modulo) EstadoCuentaCliente(ctx context.Context, a AtencionIris, clienteID int64) (EstadoCuenta, error) {
	return m.EstadoCuentaServicio(ctx, a, clienteID, nil)
}

// EstadoCuentaServicio: como EstadoCuentaCliente, con ?servicio_id= cuando quien pregunta es un
// contacto con servicios acotados (§3.5, decisión 11 del PM): solo el estado de ese servicio, sin
// el desglose de los demás. Con <contacto_id> en X-Iris-Contacto, Argos valida que el servicio sea
// del contacto (403 servicio_fuera_del_contacto) y su permiso consultar (403 permiso_de_contacto).
func (m *Modulo) EstadoCuentaServicio(ctx context.Context, a AtencionIris, clienteID int64, servicioID *int64) (EstadoCuenta, error) {
	cli, err := m.clienteAtencion(ctx, a)
	if err != nil {
		return EstadoCuenta{}, err
	}
	e, err := cli.estadoCuenta(ctx, clienteID, a.EmpresaID, servicioID)
	if err != nil {
		return EstadoCuenta{}, err
	}
	monto := e.MontoVencido.Texto()
	if monto == nil {
		monto = e.TotalVencido.Texto()
	}
	return EstadoCuenta{Saldo: e.Saldo.Texto(), Moneda: e.Moneda, FacturasVencidas: e.CantidadVencidas(), MontoVencido: monto}, nil
}
