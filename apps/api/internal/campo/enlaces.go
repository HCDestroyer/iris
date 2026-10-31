package campo

import (
	"context"
	"errors"
	"strconv"
	"sync"

	"github.com/google/uuid"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/adjuntos"
)

// Enlaces con los módulos de I1: adjuntos (nucleo/adjuntos) e inventario de Argos
// (integracion/argos).

// EntidadAdjuntos es el nombre de la orden en el módulo de adjuntos.
const EntidadAdjuntos = "orden"

var registroAdjuntos sync.Once

// RegistrarAdjuntos registra la orden como entidad adjuntable (cmd/iris y cmd/worker;
// idempotente): ver = la orden es visible; subir = además ordenes.registrar con alcance
// (AccesoAdjuntos). Si no, 404 como la orden.
func RegistrarAdjuntos() {
	registroAdjuntos.Do(func() {
		adjuntos.RegistrarEntidadAdjuntable(EntidadAdjuntos, func(ctx context.Context, q adjuntos.DBTX, ordenID string, uso adjuntos.Uso) (int64, error) {
			return AccesoAdjuntos(ctx, q, ordenID, uso == adjuntos.UsoSubir)
		})
	})
}

// AdjuntosNucleo implementa Adjuntos sobre el módulo de adjuntos de I1 (por defecto en Nuevo):
// solo cuentan los adjuntos completos (subidos y verificados por sha256), en la transacción de
// la acción.
type AdjuntosNucleo struct{}

func txDe(ctx context.Context) (adjuntos.DBTX, error) {
	if tx := nucleo.TxDe(ctx); tx != nil {
		return tx, nil
	}
	if bd := nucleo.BDDe(ctx); bd != nil {
		return bd, nil
	}
	return nil, nucleo.ErrSinBD
}

// Faltantes implementa Adjuntos.
func (AdjuntosNucleo) Faltantes(ctx context.Context, ordenID int64, requisitos []Requisito) ([]string, error) {
	q, err := txDe(ctx)
	if err != nil {
		return nil, err
	}
	rs := make([]adjuntos.Requisito, 0, len(requisitos))
	for _, r := range requisitos {
		rs = append(rs, adjuntos.Requisito{Tipo: r.Tipo, ClaveChecklist: r.ClaveChecklist, Minimo: r.Minimo})
	}
	return adjuntos.Faltantes(ctx, q, EntidadAdjuntos, strconv.FormatInt(ordenID, 10), rs)
}

// AdjuntoDeOrden implementa Adjuntos: el adjunto existe y es de esta orden (basta con el alta).
func (AdjuntosNucleo) AdjuntoDeOrden(ctx context.Context, adjuntoID uuid.UUID, ordenID int64) error {
	q, err := txDe(ctx)
	if err != nil {
		return err
	}
	_, err = adjuntos.DeEntidad(ctx, q, adjuntoID, EntidadAdjuntos, strconv.FormatInt(ordenID, 10))
	return err
}

// ErrSinConector: no hay conector Argos activo; el inventario no está disponible.
var ErrSinConector = errors.New("campo: sin conector con Argos")

// inventarioArgos implementa Inventario sobre el conector de Argos.
type inventarioArgos struct{ a *argos.Modulo }

// InventarioDeArgos adapta el conector de Argos (cliente anti-SSRF, filtrado por empresa).
func InventarioDeArgos(a *argos.Modulo) Inventario { return inventarioArgos{a: a} }

func sinConector(err error) error {
	if errors.Is(err, argos.ErrInventarioNoDisponible) {
		return ErrSinConector
	}
	return err
}

func (i inventarioArgos) EquiposEnComodato(ctx context.Context, empresaID, servicioID int64) ([]EquipoComodato, error) {
	eqs, err := i.a.EquiposEnComodato(ctx, empresaID, servicioID)
	if err != nil {
		return nil, sinConector(err)
	}
	salida := make([]EquipoComodato, 0, len(eqs))
	for _, e := range eqs {
		salida = append(salida, EquipoComodato{UnidadID: e.Unidad(), Serie: e.Serie, MAC: e.MAC, Modelo: e.Modelo})
	}
	return salida, nil
}

func (i inventarioArgos) Existencias(ctx context.Context, empresaID, bodegaID int64) ([]Existencia, error) {
	ls, err := i.a.Existencias(ctx, empresaID, bodegaID)
	if err != nil {
		return nil, sinConector(err)
	}
	salida := make([]Existencia, 0, len(ls))
	for _, l := range ls {
		cant, _ := l.Cantidad.Float64()
		desc := l.Descripcion
		if desc == "" {
			desc = l.Nombre
		}
		if desc == "" && l.Producto != nil {
			desc = l.Producto.Nombre
		}
		salida = append(salida, Existencia{ProductoID: l.IDProducto(), Descripcion: desc, Cantidad: cant, Unidad: l.Unidad, Modalidad: l.ModalidadValida()})
	}
	return salida, nil
}

func (i inventarioArgos) BodegaDeEmpresa(ctx context.Context, empresaID, bodegaID int64) (bool, error) {
	ok, err := i.a.BodegaDeEmpresa(ctx, empresaID, bodegaID)
	return ok, sinConector(err)
}
