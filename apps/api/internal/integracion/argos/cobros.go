package argos

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

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
}

// FacturaArgos es la factura que un supervisor dice que existe para un cargo «por confirmar».
type FacturaArgos struct {
	ID         int64
	Numero     string
	EmpresaID  int64
	ServicioID int64
	OrigenTipo string
	OrigenID   string
	Estado     string
	Total      *string
	Moneda     *string
	Lineas     []LineaFactura
}

// ErrFacturaNoEncontrada: Argos no tiene una factura con ese número en la empresa.
var ErrFacturaNoEncontrada = errors.New("argos: factura no encontrada")

// LeerFacturaPorNumero lee la factura de la empresa con ese número (GET /facturas?numero=&empresa_id=).
// El contrato 0.14.3 pide validar «leyendo Argos» pero no nombra la ruta: Iris usa esta lectura y,
// si Argos no la tiene (404 o una lista vacía), la factura no se da por confirmada. Fuera de
// transacción.
func (m *Modulo) LeerFacturaPorNumero(ctx context.Context, empresa int64, numero string) (FacturaArgos, error) {
	var f FacturaArgos
	bd := nucleo.BDDe(ctx)
	if bd == nil {
		return f, nucleo.ErrSinBD
	}
	cli, err := m.ClienteActivo(ctx, bd)
	if err != nil {
		return f, err
	}
	type linea struct {
		ConceptoID     *conector.Numero `json:"concepto_id"`
		PrecioUnitario *conector.Texto  `json:"precio_unitario"`
		Cantidad       *conector.Numero `json:"cantidad"`
	}
	type crudo struct {
		ID         *conector.Numero `json:"id"`
		Numero     *conector.Texto  `json:"numero"`
		EmpresaID  *conector.Numero `json:"empresa_id"`
		ServicioID *conector.Numero `json:"servicio_id"`
		OrigenTipo *string          `json:"origen_tipo"`
		OrigenID   *conector.Texto  `json:"origen_id"`
		Estado     *string          `json:"estado"`
		Total      *conector.Texto  `json:"total"`
		Moneda     *string          `json:"moneda"`
		Lineas     []linea          `json:"lineas"`
	}
	v := url.Values{"numero": {numero}}
	if empresa > 0 {
		v.Set("empresa_id", strconv.FormatInt(empresa, 10))
	}
	p, err := conector.ListaDe[crudo](ctx, cli.Cliente, "/facturas", v)
	var er *conector.ErrorRemoto
	if errors.As(err, &er) && er.Status == http.StatusNotFound {
		return f, ErrFacturaNoEncontrada
	}
	if err != nil {
		return f, err
	}
	var hallada *crudo
	for i := range p.Data {
		c := &p.Data[i]
		if c.Numero != nil && strings.TrimSpace(string(*c.Numero)) == strings.TrimSpace(numero) {
			if hallada != nil {
				return f, errors.New("argos: más de una factura con ese número")
			}
			hallada = c
		}
	}
	if hallada == nil || hallada.ID.Entero() == nil {
		return f, ErrFacturaNoEncontrada
	}
	f = FacturaArgos{ID: *hallada.ID.Entero(), Numero: strings.TrimSpace(string(*hallada.Numero)), Total: texto(hallada.Total),
		Moneda: moneda(hallada.Moneda)}
	if e := hallada.EmpresaID.Entero(); e != nil {
		f.EmpresaID = *e
	}
	if s := hallada.ServicioID.Entero(); s != nil {
		f.ServicioID = *s
	}
	if hallada.OrigenTipo != nil {
		f.OrigenTipo = *hallada.OrigenTipo
	}
	if hallada.OrigenID != nil {
		f.OrigenID = string(*hallada.OrigenID)
	}
	if hallada.Estado != nil {
		f.Estado = *hallada.Estado
	}
	for _, l := range hallada.Lineas {
		c := l.ConceptoID.Entero()
		precio := ""
		if l.PrecioUnitario != nil {
			precio = PrecioCanonico(string(*l.PrecioUnitario))
		}
		if c == nil || precio == "" {
			return f, errors.New("argos: línea de factura sin concepto o precio")
		}
		n := int64(1)
		if q := l.Cantidad.Entero(); q != nil {
			n = *q
		}
		if n < 1 || n > 100 {
			return f, errors.New("argos: línea de factura con cantidad fuera de rango")
		}
		for range n {
			f.Lineas = append(f.Lineas, LineaFactura{ConceptoID: *c, PrecioUnitario: precio})
		}
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
