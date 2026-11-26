package campo

import (
	"context"
	"errors"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
)

// Planes permitidos en el paquete del día (contrato argosisp.md 0.10.0 §3.4.5 y §3.4.8, pedido
// B21): en una instalación cuyo servicio está pendiente_instalacion, el técnico puede cambiar el
// plan sin señal, solo entre los planes de GET /planes?router_id=&modo= (mismo modo, residencial o
// empresarial, y asignados a todos los routers o al del servicio). Se leen de Argos al armar el
// paquete (fuera de la transacción, como el comodato) y viajan en la orden. Argos vuelve a
// comprobar el plan al cambiarlo y al activar (422 plan_no_permitido).

// Motivos de planes no disponibles.
const (
	MotivoSinRouter = "sin_router"
)

// PlanesArgos lee los planes aplicables (argos.Modulo.PlanesPermitidos).
type PlanesArgos interface {
	PlanesPermitidos(ctx context.Context, routerID int64, modo string) ([]argos.PlanPermitido, error)
}

// PlanOrden es un plan que el técnico puede elegir.
type PlanOrden struct {
	ID     int64   `json:"id"`
	Nombre string  `json:"nombre"`
	Precio *string `json:"precio" nullable:"true" doc:"Precio mensual como lo da Argos (texto decimal); la orden lo muestra antes de confirmar"`
	Moneda *string `json:"moneda" nullable:"true"`
}

// PlanesOrden son los planes permitidos de una instalación pendiente. disponible = false (y
// motivo) si no se pudieron leer: el teléfono no ofrece cambiar el plan (no supone «ninguno»).
type PlanesOrden struct {
	Disponible bool        `json:"disponible"`
	Motivo     *string     `json:"motivo" nullable:"true" enum:"sin_conector,argos_no_disponible,sin_router,tipo_sin_router"`
	Modo       string      `json:"modo" enum:"residencial,empresarial" doc:"Modo de la venta: el de los planes y el del titular (empresarial: NIT de la empresa y representante con DPI)"`
	Data       []PlanOrden `json:"data"`
}

// datosPlanes es lo que la orden del paquete necesita para leer sus planes (no va en el JSON).
type datosPlanes struct {
	pendiente bool
	routerID  *int64
	modo      string
	// sinRouter: servicio de cable (0.15.0 §13.1, A1): no hay planes por router (Argos respondería
	// 422 tipo_sin_router a GET /planes?router_id=).
	sinRouter bool
}

// completarPlanes lee de Argos los planes de las instalaciones pendientes (fuera de toda
// transacción), una vez por router y modo.
func (m *Modulo) completarPlanes(ctx context.Context, ordenes []*OrdenCampo) {
	type clave struct {
		router int64
		modo   string
	}
	leidos := map[clave]*PlanesOrden{}
	for _, o := range ordenes {
		if !o.planes.pendiente {
			continue
		}
		p := &PlanesOrden{Modo: o.planes.modo, Data: []PlanOrden{}}
		o.Planes = p
		switch {
		case m.cfg.Planes == nil:
			p.Motivo = ptr(MotivoSinConector)
			continue
		case o.planes.sinRouter:
			p.Motivo = ptr(argos.CodigoTipoSinRouter)
			continue
		case o.planes.routerID == nil:
			p.Motivo = ptr(MotivoSinRouter)
			continue
		}
		k := clave{*o.planes.routerID, o.planes.modo}
		if ya, ok := leidos[k]; ok {
			*p = *ya
			continue
		}
		ps, err := m.cfg.Planes.PlanesPermitidos(ctx, k.router, k.modo)
		if err != nil {
			if errors.Is(err, argos.ErrInventarioNoDisponible) {
				p.Motivo = ptr(MotivoSinConector)
			} else {
				p.Motivo = ptr(MotivoArgosNoDisponible)
			}
		} else {
			p.Disponible = true
			for _, pl := range ps {
				p.Data = append(p.Data, PlanOrden{ID: pl.ID, Nombre: pl.Nombre, Precio: pl.Precio.Texto(), Moneda: pl.Moneda})
			}
		}
		copia := *p
		leidos[k] = &copia
	}
}

// modoDe: el modo de la venta de la orden (residencial sin prospecto).
func modoDe(modos map[int64]string, prospecto *int64) string {
	if prospecto != nil {
		if m, ok := modos[*prospecto]; ok && m == argos.ModoEmpresarial {
			return argos.ModoEmpresarial
		}
	}
	return argos.ModoResidencial
}
