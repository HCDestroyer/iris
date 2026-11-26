package comercial_test

import (
	"context"
	"strings"
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/comercial"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// Contrato argosisp.md 0.15.0 §13.1 (B33): planes por tipo, factibilidad indeterminada de cable
// (A4) y alta de un servicio de cable sin router (A1).
func TestTipoServicioComercial(t *testing.T) {
	limpiarConector(t)
	e := nuevoEntorno(t, nil)
	e.falso.fijar(func(a *argosFalso) { a.conCable = true })
	e.conectar()
	t.Cleanup(func() { _, _ = pruebas.Pool(t).Exec(context.Background(), "DELETE FROM integracion.argos_planes") })
	conContrato := func(c string) {
		if _, err := pruebas.Pool(t).Exec(context.Background(), "UPDATE integracion.conectores SET contrato_remoto = $1 WHERE id = $2", c, e.conID); err != nil {
			t.Fatal(err)
		}
	}
	conContrato("0.15.0")
	// La relectura de catálogos guardó la instantánea de planes con su tipo.
	if n := contar(t, "SELECT count(*) FROM integracion.argos_planes WHERE id = 310 AND tipo_servicio = 'cable'"); n != 1 {
		t.Fatal("la relectura de catálogos debía guardar el plan de cable")
	}
	v := usuarioCon(t, rol("ventas"))
	asignarEmpresa(t, v, 1)
	ctx := enEmpresa(t, v, 1, "ventas")

	// GET /comercial/planes?tipoServicio=cable → GET /planes?tipo_servicio=cable en Argos.
	pl := deber(e.com.Planes(ctx, comercial.EntradaPlanes{TipoServicio: "cable"}))(t)
	if !pl.Disponible || len(pl.Data) != 1 || pl.Data[0].ID != 310 || pl.Data[0].TipoServicio != "cable" || !pl.Data[0].SinRouter {
		t.Fatalf("planes de cable: %+v", pl)
	}
	e.falso.fijar(func(a *argosFalso) {
		if q := a.consultas[len(a.consultas)-1]; !strings.Contains(q, "tipo_servicio=cable") {
			t.Errorf("el filtro no viajó a Argos: %s", q)
		}
	})
	todos := deber(e.com.Planes(ctx, comercial.EntradaPlanes{}))(t)
	if len(todos.Data) != 3 || todos.Data[0].TipoServicio != "internet" || todos.Data[0].SinRouter || todos.Data[1].TipoServicio != "" {
		t.Fatalf("todos los planes: %+v", todos)
	}

	// Factibilidad de un prospecto con plan de cable: indeterminado con tipo_sin_router, aunque esté
	// junto al router.
	cerca := 14.6000 + 50.0/111_195
	p := deber(e.com.CrearProspecto.Ejecutar(ctx, comercial.EntradaCrearProspecto{Nombre: "Cable Las Flores", Direccion: "Calzada 5-10",
		Lat: &cerca, Lon: ptr(-90.5), ZonaID: ptr(int64(7001)), PlanInteresID: ptr(int64(310))}))(t)
	fa := deber(e.com.EvaluarFactibilidad(ctx, comercial.EntradaFactibilidad{ProspectoID: p.ID, Version: p.Version}))(t)
	if fa.Resultado != comercial.FactibleIndeterminado || fa.Motivo == nil || *fa.Motivo != argos.CodigoTipoSinRouter || fa.RouterID != nil {
		t.Fatalf("factibilidad de cable: %+v", fa)
	}
	if n := contar(t, `SELECT count(*) FROM integracion.outbox WHERE evento = 'factibilidad.evaluada' AND payload->>'prospecto_id' = $1
		AND payload->>'resultado' = 'indeterminado' AND payload->>'motivo' = 'tipo_sin_router'`, p.ID); n != 1 {
		t.Fatal("factibilidad.evaluada con motivo tipo_sin_router")
	}

	// Con la visita técnica, se cotiza y se gana sin router: POST /servicios con router_id null.
	lineas := []comercial.EntradaLinea{{Tipo: "plan", PlanID: ptr(int64(310))}}
	d := hastaContrato(t, e.com, ctx, p.ID, lineas, comercial.FactibleViable)
	deber(e.com.Ganar.Ejecutar(ctx, comercial.EntradaGanar{ID: p.ID, Version: d.Version}))(t)
	e.procesar()
	srv := e.falso.cuerpo(comercial.ClaveServicio(p.ID))
	if srv == nil || srv["plan_id"] != float64(310) {
		t.Fatalf("POST /servicios de cable: %v", srv)
	}
	if r, ok := srv["router_id"]; !ok || r != nil {
		t.Fatalf("un servicio de cable va con router_id null: %v", srv)
	}

	// Con un Argos 0.14.3 el bloque va apagado: el mismo plan se evalúa por distancia, como hoy.
	conContrato("0.14.3")
	p2 := deber(e.com.CrearProspecto.Ejecutar(ctx, comercial.EntradaCrearProspecto{Nombre: "Cable 0.14", Direccion: "Calzada 5-12",
		Lat: &cerca, Lon: ptr(-90.5), ZonaID: ptr(int64(7001)), PlanInteresID: ptr(int64(310))}))(t)
	if fa := deber(e.com.EvaluarFactibilidad(ctx, comercial.EntradaFactibilidad{ProspectoID: p2.ID, Version: p2.Version}))(t); fa.Resultado != comercial.FactibleViable {
		t.Fatalf("con Argos anterior, factibilidad por distancia: %+v", fa)
	}
	e.falso.fijar(func(a *argosFalso) { a.consultas = nil })
	deber(e.com.Planes(ctx, comercial.EntradaPlanes{TipoServicio: "cable"}))(t)
	e.falso.fijar(func(a *argosFalso) {
		if q := a.consultas[len(a.consultas)-1]; strings.Contains(q, "tipo_servicio") {
			t.Errorf("con Argos < 0.15.0 el filtro no viaja: %s", q)
		}
	})
}
