package pendientes

import (
	"testing"
)

func idArgos(n int64) *IDArgos { v := IDArgos(n); return &v }

func consumoDe(t *testing.T, planes []plan) plan {
	t.Helper()
	for _, p := range planes {
		if p.operacion == OpConsumo {
			return p
		}
	}
	t.Fatalf("sin consumo: %+v", planes)
	return plan{}
}

// M-268-1: el material que registró un técnico desde el lote sale siempre con tecnico, bodega_id y
// el X-Iris-Actor de quien lo registró (con Argos ≥ 0.11.0), también desde su bodega personal; solo
// lo que indicó despacho en el panel sale con bodega_id.
func TestPlanificarMaterialDeLaApp(t *testing.T) {
	tec, quien := int64(10), int64(11)
	base := OrdenCerrada{OrdenID: "900", Tipo: "visita", Resultado: ResultadoExitosa, ServicioID: idArgos(20), TecnicoID: &tec,
		BodegaID: idArgos(3), Tecnico: &TecnicoArgos{IDExterno: "u-10"}}

	// Desde la app, desde una bodega personal distinta de la de la orden.
	o := base
	o.Materiales = []Material{{ProductoID: idArgos(5), Cantidad: "2", BodegaID: idArgos(4), Modalidad: "consumible_gasto", DesdeApp: true, RegistradoPor: &quien}}
	p := consumoDe(t, mustPlanes(planificar(o, opcionesPlan{porTecnico: true, bodegaApp: true})))
	if !p.conTecnico || p.actor == nil || *p.actor != quien || p.payload["tecnico"] == nil || p.payload["bodega_id"] == nil ||
		*(p.payload["bodega_id"].(*IDArgos)) != 4 || p.incompleto != "" {
		t.Fatalf("app: tecnico + bodega_id + actor: %+v", p)
	}
	// Desde la app, de la bodega de la orden: también tecnico + bodega_id.
	o.Materiales[0].BodegaID = idArgos(3)
	if p := consumoDe(t, mustPlanes(planificar(o, opcionesPlan{porTecnico: true, bodegaApp: true}))); !p.conTecnico || p.payload["bodega_id"] == nil {
		t.Fatalf("app desde la de la orden: %+v", p)
	}
	// Desde el panel (despacho indicó otra bodega): solo bodega_id, sin actor.
	o.Materiales = []Material{{ProductoID: idArgos(5), Cantidad: "2", BodegaID: idArgos(8), Modalidad: "consumible_gasto"}}
	if p := consumoDe(t, mustPlanes(planificar(o, opcionesPlan{porTecnico: true, bodegaApp: true}))); p.conTecnico || p.actor != nil || p.payload["tecnico"] != nil {
		t.Fatalf("panel: solo bodega_id: %+v", p)
	}
	// La app y una bodega del panel en una orden: incompleto (un consumo por orden).
	o.Materiales = append(o.Materiales, Material{ProductoID: idArgos(6), Cantidad: "1", BodegaID: idArgos(3), Modalidad: "consumible_gasto", DesdeApp: true, RegistradoPor: &quien})
	if p := consumoDe(t, mustPlanes(planificar(o, opcionesPlan{porTecnico: true, bodegaApp: true}))); p.incompleto == "" {
		t.Fatalf("mezcla app y panel: %+v", p)
	}
	// Con un Argos anterior a 0.11.0, como en 0.5.0: la de la orden por tecnico.
	o.Materiales = []Material{{ProductoID: idArgos(5), Cantidad: "2", BodegaID: idArgos(3), Modalidad: "consumible_gasto", DesdeApp: true, RegistradoPor: &quien}}
	if p := consumoDe(t, mustPlanes(planificar(o, opcionesPlan{porTecnico: true}))); !p.conTecnico || p.payload["bodega_id"] != nil {
		t.Fatalf("Argos anterior: %+v", p)
	}
}

func mustPlanes(planes []plan, _ []omision) []plan { return planes }
