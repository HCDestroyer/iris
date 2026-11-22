package argos

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Cobro de la instalación (contrato 0.14.3 §3, «0.9.0:», y §4.3): Iris identifica la línea de
// instalación solo por el concepto con codigo_sistema = "instalacion" de la empresa (sembrado por
// Argos, que no deja cambiarlo ni dárselo a otro concepto), nunca por el nombre.

// CodigoSistemaInstalacion es el codigo_sistema del concepto de instalación.
const CodigoSistemaInstalacion = "instalacion"

// ErrSinConceptoInstalacion: la empresa no tiene (o Argos no informa) el concepto de instalación.
var ErrSinConceptoInstalacion = errors.New("argos: la empresa no tiene concepto con codigo_sistema instalacion")

type conceptoCargo struct {
	ID            *conector.Numero `json:"id"`
	EmpresaID     *conector.Numero `json:"empresa_id"`
	CodigoSistema *string          `json:"codigo_sistema"`
}

// ConceptoInstalacion lee GET /conceptos-cargo de la empresa y devuelve el id del concepto con
// codigo_sistema "instalacion". Fuera de transacción (usa la BD del contexto). Sin conector activo,
// el error del conector; sin ese concepto, ErrSinConceptoInstalacion.
func (m *Modulo) ConceptoInstalacion(ctx context.Context, empresa int64) (int64, error) {
	bd := nucleo.BDDe(ctx)
	if bd == nil {
		return 0, nucleo.ErrSinBD
	}
	// codigo_sistema existe desde la 0.14.3: con un Argos anterior el concepto es desconocido.
	var contrato *string
	if err := bd.QueryRow(ctx, `SELECT contrato_remoto FROM integracion.conectores WHERE sistema = $1 AND estado = 'activo'
		ORDER BY id DESC LIMIT 1`, Sistema).Scan(&contrato); err != nil {
		return 0, err
	}
	if !conector.VersionAlMenos(contrato, ContratoLecturaFactura) {
		return 0, ErrLecturaNoDisponible
	}
	cli, err := m.ClienteActivo(ctx, bd)
	if err != nil {
		return 0, err
	}
	v := url.Values{}
	if empresa > 0 {
		v.Set("empresa_id", strconv.FormatInt(empresa, 10))
	}
	conceptos, err := conector.Todas[conceptoCargo](ctx, cli.Cliente, "/conceptos-cargo", v)
	if err != nil {
		return 0, err
	}
	var encontrado int64
	for _, c := range conceptos {
		if c.CodigoSistema == nil || strings.TrimSpace(*c.CodigoSistema) != CodigoSistemaInstalacion {
			continue
		}
		if e := c.EmpresaID.Entero(); e != nil && empresa > 0 && *e != empresa {
			continue // de otra empresa (Argos filtra, pero no se confía)
		}
		id := c.ID.Entero()
		if id == nil || *id <= 0 {
			continue
		}
		if encontrado != 0 && encontrado != *id {
			// Dos conceptos de instalación en una empresa: no se adivina.
			return 0, errors.New("argos: más de un concepto con codigo_sistema instalacion en la empresa")
		}
		encontrado = *id
	}
	if encontrado == 0 {
		return 0, ErrSinConceptoInstalacion
	}
	return encontrado, nil
}

// LineaFactura es una línea de una factura de Argos (lo que Iris compara con el cargo).
type LineaFactura struct {
	ConceptoID     int64
	PrecioUnitario string
	Cantidad       int64
}

// FacturaArgos es una factura de origen Iris (GET /facturas/origen-iris, 0.14.3 §3).
type FacturaArgos struct {
	ID         int64
	Numero     string
	EmpresaID  int64
	ServicioID int64
	ClienteID  int64
	OrigenTipo string
	OrigenID   string
	Estado     string
	Total      *string
	Saldo      *string
	Moneda     *string
	EmitidaEn  time.Time
	Lineas     []LineaFactura
	// SinConcepto: alguna línea trae concepto_id null (cargo emitido antes de que Argos pasara a la
	// 0.14.3): no se concilia como «existe»; a lo más, factura por anular.
	SinConcepto bool
}

// ConPagos: saldo < total (la factura tiene pagos). Falla cerrado: si el saldo o el total no se
// conocen, cuenta como con pagos.
func (f FacturaArgos) ConPagos() bool {
	if f.Total == nil || f.Saldo == nil {
		return true
	}
	t, err1 := strconv.ParseFloat(*f.Total, 64)
	s, err2 := strconv.ParseFloat(*f.Saldo, 64)
	return err1 != nil || err2 != nil || s < t
}

// ErrFacturaNoEncontrada: 404 factura_no_encontrada (o 422 validacion) de GET /facturas/origen-iris.
// Solo rechaza la conciliación: nunca equivale a «no existe».
var ErrFacturaNoEncontrada = errors.New("argos: factura no encontrada")

// ErrLecturaNoDisponible: Argos < 0.14.3 (sin la ruta): Iris no concilia como «existe».
var ErrLecturaNoDisponible = errors.New("argos: la lectura de facturas de origen Iris no está disponible")

// ContratoLecturaFactura: desde la 0.14.3 Argos expone GET /facturas/origen-iris.
const ContratoLecturaFactura = "0.14.3"

// BuscarFactura: la «Factura #» del panel de Argos (= factura_id), o la serie y el número del DTE
// (excluyentes).
type BuscarFactura struct {
	FacturaID int64
	Serie     string
	NumeroDTE string
}

// LeerFacturaOrigenIris lee GET /api/v1/facturas/origen-iris?empresa_id=&origen_id=&numero= (o serie y
// numero_dte) con X-Iris-Actor: usuario:<id> (0.14.3 §3, permiso facturas.leer_origen_iris del token).
// Fuera de transacción. 404 con type factura_no_encontrada → ErrFacturaNoEncontrada; con un Argos
// anterior → ErrLecturaNoDisponible; cualquier otro error (otro 404, 429, 5xx, timeout) es de lectura.
func (m *Modulo) LeerFacturaOrigenIris(ctx context.Context, empresa int64, orden string, b BuscarFactura, usuario int64) (FacturaArgos, error) {
	var f FacturaArgos
	bd := nucleo.BDDe(ctx)
	if bd == nil {
		return f, nucleo.ErrSinBD
	}
	var contrato *string
	if err := bd.QueryRow(ctx, `SELECT contrato_remoto FROM integracion.conectores WHERE sistema = $1 AND estado = 'activo'
		ORDER BY id DESC LIMIT 1`, Sistema).Scan(&contrato); err != nil {
		return f, err
	}
	if !conector.VersionAlMenos(contrato, ContratoLecturaFactura) {
		return f, ErrLecturaNoDisponible
	}
	cli, err := m.ClienteActivo(ctx, bd)
	if err != nil {
		return f, err
	}
	v := url.Values{"empresa_id": {strconv.FormatInt(empresa, 10)}, "origen_id": {orden}}
	if b.FacturaID > 0 {
		v.Set("numero", strconv.FormatInt(b.FacturaID, 10))
	} else {
		v.Set("serie", b.Serie)
		v.Set("numero_dte", b.NumeroDTE)
	}
	var crudo struct {
		FacturaID  *conector.Numero `json:"factura_id"`
		Numero     *conector.Texto  `json:"numero"`
		EmpresaID  *conector.Numero `json:"empresa_id"`
		ServicioID *conector.Numero `json:"servicio_id"`
		ClienteID  *conector.Numero `json:"cliente_id"`
		OrigenTipo *string          `json:"origen_tipo"`
		OrigenID   *conector.Texto  `json:"origen_id"`
		Estado     *string          `json:"estado"`
		Total      *conector.Texto  `json:"total"`
		Saldo      *conector.Texto  `json:"saldo"`
		Moneda     *string          `json:"moneda"`
		EmitidaEn  *time.Time       `json:"emitida_en"`
		Lineas     []struct {
			ConceptoID     *conector.Numero `json:"concepto_id"`
			PrecioUnitario *conector.Texto  `json:"precio_unitario"`
			Cantidad       *conector.Numero `json:"cantidad"`
		} `json:"lineas"`
	}
	c := cli.ConCabecera(CabeceraActorIris, "usuario:"+strconv.FormatInt(usuario, 10))
	err = c.Pedir(ctx, http.MethodGet, "/facturas/origen-iris", v, nil, true, &conector.Recurso{Destino: &crudo})
	var er *conector.ErrorRemoto
	if errors.As(err, &er) && ((er.Status == http.StatusNotFound && strings.HasSuffix(strings.TrimSpace(er.Tipo), "factura_no_encontrada")) ||
		er.Status == http.StatusUnprocessableEntity) {
		return f, ErrFacturaNoEncontrada
	}
	if err != nil {
		return f, err
	}
	if crudo.FacturaID.Entero() == nil || crudo.EmitidaEn == nil {
		return f, errors.New("argos: factura sin factura_id o emitida_en")
	}
	f = FacturaArgos{ID: *crudo.FacturaID.Entero(), Total: decimal(crudo.Total), Saldo: decimal(crudo.Saldo), Moneda: moneda(crudo.Moneda),
		EmitidaEn: crudo.EmitidaEn.UTC()}
	if crudo.Numero != nil {
		f.Numero = strings.TrimSpace(string(*crudo.Numero))
	}
	for dst, src := range map[*int64]*conector.Numero{&f.EmpresaID: crudo.EmpresaID, &f.ServicioID: crudo.ServicioID, &f.ClienteID: crudo.ClienteID} {
		if n := src.Entero(); n != nil {
			*dst = *n
		}
	}
	if crudo.OrigenTipo != nil {
		f.OrigenTipo = *crudo.OrigenTipo
	}
	if crudo.OrigenID != nil {
		f.OrigenID = string(*crudo.OrigenID)
	}
	if crudo.Estado != nil {
		f.Estado = *crudo.Estado
	}
	for _, l := range crudo.Lineas {
		precio := ""
		if l.PrecioUnitario != nil {
			precio = PrecioCanonico(string(*l.PrecioUnitario))
		}
		if precio == "" {
			return f, errors.New("argos: línea de factura sin precio")
		}
		var concepto int64
		if c := l.ConceptoID.Entero(); c != nil {
			concepto = *c
		} else {
			f.SinConcepto = true
		}
		n := int64(1)
		if q := l.Cantidad.Entero(); q != nil {
			n = *q
		}
		f.Lineas = append(f.Lineas, LineaFactura{ConceptoID: concepto, PrecioUnitario: precio, Cantidad: n})
	}
	return f, nil
}

// PrecioCanonico deja un precio decimal con dos decimales («150» → «150.00»), o "" si no lo es.
func PrecioCanonico(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || f < 0 {
		return ""
	}
	return strconv.FormatFloat(f, 'f', 2, 64)
}
