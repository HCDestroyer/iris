package argos

import (
	"context"
	"net/url"
	"strconv"
	"strings"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
)

// Planes permitidos para cambiar el plan antes de activar (contrato 0.10.0 §3.4.5, A40; pedido
// B21): GET /planes?router_id=&modo= devuelve solo los aplicables (activos, del mismo modo y
// asignados a todos los routers o a ese router). Iris los guarda en el paquete del día para que
// el técnico elija sin señal; Argos vuelve a comprobar el plan al cambiarlo y al activar
// (422 plan_no_permitido).

// Modos de un plan (§3.4.5).
const (
	ModoResidencial = "residencial"
	ModoEmpresarial = "empresarial"
)

// PlanPermitido es un plan de GET /planes (0.10.0 suma modo).
type PlanPermitido struct {
	ID     int64    `json:"id"`
	Nombre string   `json:"nombre"`
	Precio *Decimal `json:"precio"`
	Moneda *string  `json:"moneda"`
	Activo *bool    `json:"activo"`
	Modo   string   `json:"modo"`
}

// PlanesPermitidos lee los planes aplicables al router y al modo (fuera de toda transacción).
// Sin conector activo: ErrInventarioNoDisponible. Un plan inactivo o de otro modo (un Argos que
// no filtra) no se devuelve.
func (m *Modulo) PlanesPermitidos(ctx context.Context, routerID int64, modo string) ([]PlanPermitido, error) {
	c, err := m.clienteInventario(ctx)
	if err != nil {
		return nil, err
	}
	q := url.Values{"activo": {"true"}, "router_id": {strconv.FormatInt(routerID, 10)}}
	if modo != "" {
		q.Set("modo", modo)
	}
	ps, err := conector.Todas[PlanPermitido](ctx, c.Cliente, "/planes", q)
	if err != nil {
		return nil, Problema(err)
	}
	salida := make([]PlanPermitido, 0, len(ps))
	for _, p := range ps {
		if p.Activo != nil && !*p.Activo {
			continue
		}
		if modo != "" && p.Modo != "" && !strings.EqualFold(p.Modo, modo) {
			continue
		}
		salida = append(salida, p)
	}
	return salida, nil
}
