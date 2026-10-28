package horus

import (
	"context"
	"fmt"
	"net/http"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
)

// Cliente HTTP de la API de Horus (§1.1: token hf_pat_… con traffic.customer.read,
// security.findings.read, devices.read y sso.issue), sobre el cliente común de los conectores:
// timeout, reintentos en lecturas, sin redirecciones, protección contra SSRF y problem+json
// mapeado (horus_no_disponible, horus_sin_permiso, horus_rechazo). Lee JSON en snake_case o
// camelCase, envuelto o no en { data }.
type Cliente struct {
	*conector.Cliente
}

// NuevoCliente: base es el origen de Horus (sin /api/v1); token, el hf_pat_…. Sin
// OpcionesCliente.HTTP usa http.DefaultClient (sin la protección contra SSRF: solo pruebas).
func NuevoCliente(base, token string, op conector.OpcionesCliente) *Cliente {
	return &Cliente{Cliente: conector.NuevoCliente(Remoto, base, token, op)}
}

// SaludServicioHorus es el consumo y la salud del servicio de un cliente según Horus (§3.1 del
// contrato Argos ↔ Horus, llamado con el servicio_id de Argos). Supuesto de nombres: los
// campos que no vengan quedan en nil.
type SaludServicioHorus struct {
	ServicioID    *conector.Numero `json:"servicio_id"`
	Estado        string           `json:"estado"`
	BajadaBps     *conector.Numero `json:"bajada_bps"`
	SubidaBps     *conector.Numero `json:"subida_bps"`
	LatenciaMs    *conector.Numero `json:"latencia_ms"`
	PerdidaPct    *conector.Numero `json:"perdida_pct"`
	ActualizadoEn string           `json:"actualizado_en"`
}

// RouterHorus es el estado de un router según Horus (devices.read).
type RouterHorus struct {
	ID     *conector.Numero `json:"id"`
	Nombre string           `json:"nombre"`
	Estado string           `json:"estado"`
	Desde  string           `json:"desde"`
}

// SaludServicio: GET /integrations/iris/servicios/{servicio_id}/salud (supuesto de ruta).
func (c *Cliente) SaludServicio(ctx context.Context, servicioID int64) (SaludServicioHorus, error) {
	var s SaludServicioHorus
	err := c.Pedir(ctx, http.MethodGet, fmt.Sprintf(rutaSaludServicio, servicioID), nil, nil, true, &conector.Recurso{Destino: &s})
	return s, err
}

// Router: GET /integrations/iris/routers/{router_id} (supuesto de ruta).
func (c *Cliente) Router(ctx context.Context, routerID int64) (RouterHorus, error) {
	var r RouterHorus
	err := c.Pedir(ctx, http.MethodGet, fmt.Sprintf(rutaRouter, routerID), nil, nil, true, &conector.Recurso{Destino: &r})
	return r, err
}
