package argos

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Lecturas de inventario para la app de campo (I2, contratos/argosisp.md §2.1): equipos en
// comodato de un servicio, existencias de una bodega y la empresa de una bodega. Sin
// transacción (llamadas HTTP), por el cliente anti-SSRF del conector activo, y filtradas por la
// empresa que pide campo (la de la orden, que es la activa).

// ErrInventarioNoDisponible: no hay conector Argos activo (apagado o nunca conectado). Campo lo
// trata como «sin conector»: no valida contra el comodato y lo marca no verificado.
var ErrInventarioNoDisponible = errors.New("argos: conector apagado; inventario no disponible")

// ErrOtraEmpresa: el servicio o la bodega es de otra empresa en Argos.
var ErrOtraEmpresa = errors.New("argos: el recurso es de otra empresa")

// EquipoComodatoArgos es un equipo de Argos asignado en comodato a un servicio.
type EquipoComodatoArgos struct {
	UnidadID  int64   `json:"unidad_id"`
	ID        int64   `json:"id"`
	Serie     string  `json:"serie"`
	MAC       *string `json:"mac"`
	Modelo    string  `json:"modelo"`
	EmpresaID int64   `json:"empresa_id"`
}

// Unidad: unidad_id o, si Argos manda el equipo con su propio id, ese.
func (e EquipoComodatoArgos) Unidad() int64 {
	if e.UnidadID != 0 {
		return e.UnidadID
	}
	return e.ID
}

// ExistenciaArgos es la existencia de un producto en una bodega.
type ExistenciaArgos struct {
	ProductoID  int64       `json:"producto_id"`
	Producto    *Ref        `json:"producto"`
	Descripcion string      `json:"descripcion"`
	Nombre      string      `json:"nombre"`
	Cantidad    json.Number `json:"cantidad"`
	Unidad      string      `json:"unidad"`
	BodegaID    int64       `json:"bodega_id"`
	EmpresaID   int64       `json:"empresa_id"`
	// Modalidad (0.5.0): comodato, consumible_gasto o consumible_cobro; vacía en Argos < 1.6.0.
	Modalidad string `json:"modalidad"`
}

// Modalidades del producto en Argos (contrato 0.5.0 §3.3).
const (
	ModalidadComodato        = "comodato"
	ModalidadConsumibleGasto = "consumible_gasto"
	ModalidadConsumibleCobro = "consumible_cobro"
)

// EsModalidad indica si m es una modalidad del contrato.
func EsModalidad(m string) bool {
	return m == ModalidadComodato || m == ModalidadConsumibleGasto || m == ModalidadConsumibleCobro
}

// ModalidadValida: la modalidad si es del contrato; vacía si no.
func (e ExistenciaArgos) ModalidadValida() string {
	if EsModalidad(e.Modalidad) {
		return e.Modalidad
	}
	return ""
}

// IDProducto devuelve el id del producto (plano o anidado).
func (e ExistenciaArgos) IDProducto() int64 {
	if e.ProductoID != 0 {
		return e.ProductoID
	}
	if e.Producto != nil && e.Producto.ID != nil {
		return *e.Producto.ID
	}
	return 0
}

// BodegaArgos es una bodega de Argos (supuesto: GET /bodegas/:id con empresa_id; pendiente con
// Argos, ver .claude/integration/i1.md).
type BodegaArgos struct {
	ID        int64 `json:"id"`
	EmpresaID int64 `json:"empresa_id"`
}

// EquiposDeServicio: GET /servicios/:id/equipos (todas las páginas).
func (c *Cliente) EquiposDeServicio(ctx context.Context, servicioID int64) ([]EquipoComodatoArgos, error) {
	return conector.Todas[EquipoComodatoArgos](ctx, c.Cliente, "/servicios/"+strconv.FormatInt(servicioID, 10)+"/equipos", nil)
}

// ExistenciasDeBodega: GET /inventario/existencias?bodega_id= (todas las páginas).
func (c *Cliente) ExistenciasDeBodega(ctx context.Context, bodegaID int64) ([]ExistenciaArgos, error) {
	return conector.Todas[ExistenciaArgos](ctx, c.Cliente, "/inventario/existencias",
		url.Values{"bodega_id": {strconv.FormatInt(bodegaID, 10)}})
}

// ObtenerBodega: GET /bodegas/:id (supuesto).
func (c *Cliente) ObtenerBodega(ctx context.Context, id int64) (BodegaArgos, error) {
	var b BodegaArgos
	err := c.Pedir(ctx, http.MethodGet, "/bodegas/"+strconv.FormatInt(id, 10), nil, nil, true, &conector.Recurso{Destino: &b})
	return b, err
}

// clienteInventario: el cliente del conector activo o ErrInventarioNoDisponible.
func (m *Modulo) clienteInventario(ctx context.Context) (*Cliente, error) {
	bd := nucleo.BDDe(ctx)
	if bd == nil {
		return nil, nucleo.ErrSinBD
	}
	c, err := m.conectorActivo(ctx, bd)
	var p *nucleo.Problema
	if errors.As(err, &p) && p.Codigo() == "conector_apagado" {
		return nil, ErrInventarioNoDisponible
	}
	if err != nil {
		return nil, err
	}
	return m.clienteDe(c), nil
}

func ajenaInventario(empresa, suya int64) bool { return empresa != 0 && suya != 0 && suya != empresa }

// EquiposEnComodato devuelve los equipos en comodato del servicio, si el servicio es de la
// empresa (GET /servicios/:id para confirmarlo; ErrOtraEmpresa si no).
func (m *Modulo) EquiposEnComodato(ctx context.Context, empresaID, servicioID int64) ([]EquipoComodatoArgos, error) {
	c, err := m.clienteInventario(ctx)
	if err != nil {
		return nil, err
	}
	s, err := c.ObtenerServicio(ctx, servicioID)
	if err != nil {
		return nil, err
	}
	if ajenaInventario(empresaID, s.EmpresaID) || (empresaID != 0 && s.EmpresaID == 0) {
		return nil, ErrOtraEmpresa
	}
	equipos, err := c.EquiposDeServicio(ctx, servicioID)
	if err != nil {
		return nil, err
	}
	salida := make([]EquipoComodatoArgos, 0, len(equipos))
	for _, e := range equipos {
		if !ajenaInventario(empresaID, e.EmpresaID) {
			salida = append(salida, e)
		}
	}
	return salida, nil
}

// BodegaDeEmpresa indica si la bodega existe en Argos y es de la empresa (404 → false).
func (m *Modulo) BodegaDeEmpresa(ctx context.Context, empresaID, bodegaID int64) (bool, error) {
	c, err := m.clienteInventario(ctx)
	if err != nil {
		return false, err
	}
	b, err := c.ObtenerBodega(ctx, bodegaID)
	if EsNoEncontrado(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return b.EmpresaID != 0 && (empresaID == 0 || b.EmpresaID == empresaID), nil
}

// Existencias devuelve las existencias de la bodega, si es de la empresa (ErrOtraEmpresa si
// no). Las líneas de otra empresa (si Argos las manda) se descartan.
func (m *Modulo) Existencias(ctx context.Context, empresaID, bodegaID int64) ([]ExistenciaArgos, error) {
	ok, err := m.BodegaDeEmpresa(ctx, empresaID, bodegaID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("bodega %d: %w", bodegaID, ErrOtraEmpresa)
	}
	c, err := m.clienteInventario(ctx)
	if err != nil {
		return nil, err
	}
	lineas, err := c.ExistenciasDeBodega(ctx, bodegaID)
	if err != nil {
		return nil, err
	}
	salida := make([]ExistenciaArgos, 0, len(lineas))
	for _, l := range lineas {
		if !ajenaInventario(empresaID, l.EmpresaID) && (l.BodegaID == 0 || l.BodegaID == bodegaID) {
			salida = append(salida, l)
		}
	}
	return salida, nil
}
