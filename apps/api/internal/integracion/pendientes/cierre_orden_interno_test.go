package pendientes

import (
	"encoding/json"
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

// B-271-5: sinNotas quita la nota del técnico y la del cobro propuesto, y deja lo demás.
func TestSinNotas(t *testing.T) {
	got := string(sinNotas([]byte(`{"nota":"a","motivo":"danado","cobro_propuesto":{"motivo":"golpe","nota":"b"}}`)))
	if got != `{"cobro_propuesto":{"motivo":"golpe"},"motivo":"danado"}` {
		t.Fatalf("sinNotas: %s", got)
	}
	if got := string(sinNotas([]byte(`{"motivo":"x"}`))); got != `{"motivo":"x"}` {
		t.Fatalf("sin notas queda igual: %s", got)
	}
	if got := string(sinNotas([]byte(`no-json`))); got != "no-json" {
		t.Fatalf("no JSON: %s", got)
	}
}

// A-271-1: el intento sale con X-Iris-Actor de quien lo registró, no del técnico de la orden; sin
// registrado_por queda incompleto.
func TestPlanificarEquiposConQuienLoRegistro(t *testing.T) {
	quien := int64(7)
	var o OrdenCerrada
	if err := json.Unmarshal([]byte(`{"orden_id":"0193f000-0000-7000-8000-0000000000f1","tipo":"retiro","resultado":"exitosa",
		"servicio_id":50,"cliente_id":10,"tecnico_id":8,"equipos":[
		{"accion":"no_recuperado","unidad_id":91,"motivo":"se_nego","foto_ref":"f1","ubicacion":{"lat":14.6,"lng":-90.5,"precision_m":10},"registrado_por":7},
		{"accion":"no_recuperado","unidad_id":92,"motivo":"se_nego","foto_ref":"f2","ubicacion":{"lat":14.6,"lng":-90.5,"precision_m":10}}]}`), &o); err != nil {
		t.Fatal(err)
	}
	planes, _ := planificar(o, opcionesPlan{equipos: true})
	vistos := 0
	for _, p := range planes {
		if p.operacion != OpNoRecuperado {
			continue
		}
		vistos++
		switch p.ruta {
		case "/servicios/50/equipos/91/no-recuperado":
			if p.actor == nil || *p.actor != quien || p.incompleto != "" {
				t.Fatalf("con registrado_por: %+v", p)
			}
		case "/servicios/50/equipos/92/no-recuperado":
			if p.incompleto == "" {
				t.Fatalf("sin registrado_por queda incompleto: %+v", p)
			}
		default:
			t.Fatalf("ruta: %s", p.ruta)
		}
	}
	if vistos != 2 {
		t.Fatalf("planes de no recuperado: %d", vistos)
	}
}
