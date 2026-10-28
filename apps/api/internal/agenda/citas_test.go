package agenda_test

import (
	"context"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/agenda"
	"github.com/HCDestroyer/iris/apps/api/internal/campo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// Programar y reprogramar delegan en la agenda: turno, ausencia, solape, capacidad del técnico y
// de su cuadrilla, ventana con el cliente y orden.programada (lo emite agenda).
func TestProgramarValidaLaAgenda(t *testing.T) {
	e := nuevoEntorno()
	sup := usuarioCon(t, rol("supervisor"))
	cs := como(t, sup, "supervisor")
	desp := usuarioCon(t, rol("despacho"))
	cd := como(t, desp, "despacho")
	z := deber(e.ag.CrearZona.Ejecutar(cs, agenda.EntradaCrearZona{Nombre: "Zona programar " + pruebas.Unico()}))(t)
	c := deber(e.ag.CrearCuadrilla.Ejecutar(cs, agenda.EntradaCrearCuadrilla{Nombre: "Cuadrilla programar " + pruebas.Unico(), ZonaIDs: []int64{z.ID}}))(t)
	tec := usuarioCon(t, rol("tecnico"))
	f := fichaCon(t, e, cs, tec, &c.ID)

	// Sin ficha de agenda no se programa.
	sinFicha := usuarioCon(t, rol("tecnico"))
	o1 := orden(t, e, cd, "visita", z.ID)
	_, err := programar(e, cd, o1, sinFicha, gt(2, 10, 0))
	problema(t, err, 422, "tecnico_sin_agenda")

	// Fuera de turno (07:00), sábado (sin turno) y cruzando el fin del turno.
	_, err = programar(e, cd, o1, tec, gt(2, 7, 0))
	problema(t, err, 422, "fuera_de_turno")
	_, err = programar(e, cd, o1, tec, gt(5, 10, 0))
	problema(t, err, 422, "fuera_de_turno")
	_, err = programar(e, cd, o1, tec, gt(2, 16, 30))
	problema(t, err, 422, "fuera_de_turno")

	// Ventana: el inicio debe caer en ella.
	_, err = e.cmp.ProgramarOrden.Ejecutar(cd, campo.EntradaProgramarOrden{ID: o1.ID, Version: o1.Version, TecnicoID: &tec,
		Inicio: gt(2, 12, 0), VentanaInicio: ptr(gt(2, 8, 0)), VentanaFin: ptr(gt(2, 11, 0))})
	problema(t, err, 422, "fuera_de_ventana")
	_, err = e.cmp.ProgramarOrden.Ejecutar(cd, campo.EntradaProgramarOrden{ID: o1.ID, Version: o1.Version, TecnicoID: &tec,
		Inicio: gt(2, 9, 0), VentanaInicio: ptr(gt(2, 8, 0))})
	problema(t, err, 422, "validacion")
	o1 = deber(e.cmp.ProgramarOrden.Ejecutar(cd, campo.EntradaProgramarOrden{ID: o1.ID, Version: o1.Version, TecnicoID: &tec,
		Inicio: gt(2, 9, 0), VentanaInicio: ptr(gt(2, 8, 0)), VentanaFin: ptr(gt(2, 12, 0))}))(t)
	if o1.Estado != "programada" || !o1.ProgramadaFin.Equal(gt(2, 10, 0)) || o1.BodegaID == nil || *o1.BodegaID != 9 {
		t.Fatalf("programada (bodega de la ficha): %+v", o1)
	}
	p := ultimoEvento(t, "orden.programada", o1.ID)
	if p["origen"] != "programar" || p["ventana_inicio"] == nil || p["tecnico_id"] != float64(tec) || p["reprogramada"] != false {
		t.Fatalf("orden.programada: %+v", p)
	}
	citas, err := agenda.CitasDeOrdenes(context.Background(), pruebas.Pool(t), []int64{o1.ID})
	if err != nil || citas[o1.ID].VentanaInicio == nil || !citas[o1.ID].Inicio.Equal(gt(2, 9, 0)) {
		t.Fatalf("cita: %+v %v", citas, err)
	}

	// Solape con la cita de o1.
	o2 := orden(t, e, cd, "visita", z.ID)
	_, err = programar(e, cd, o2, tec, gt(2, 9, 30))
	problema(t, err, 422, "solape")

	// Ausencia del jueves.
	deber(e.ag.RegistrarAusencia.Ejecutar(cs, agenda.EntradaRegistrarAusencia{ID: f.ID, Inicio: gt(3, 0, 0).Format(time.RFC3339),
		Fin: gt(4, 0, 0).Format(time.RFC3339), Motivo: "vacaciones"}))(t)
	_, err = programar(e, cd, o2, tec, gt(3, 10, 0))
	problema(t, err, 422, "tecnico_ausente")

	// Capacidad del técnico: una visita por día.
	f = deber(e.ag.VerTecnico.Ejecutar(cs, agenda.EntradaVerTecnico{ID: f.ID}))(t)
	f = deber(e.ag.CapacidadTecnico.Ejecutar(cs, agenda.EntradaFijarCapacidad{ID: f.ID, Version: f.Version,
		Capacidad: []agenda.Capacidad{{Tipo: ptr("visita"), Cupos: ptr(int32(1))}}}))(t)
	_, err = programar(e, cd, o2, tec, gt(2, 13, 0))
	problema(t, err, 422, "capacidad_excedida")
	// Otro tipo no cuenta para la línea de visitas; otro día tampoco.
	o2 = deber(programar(e, cd, o2, tec, gt(4, 13, 0)))(t)
	deber(e.ag.CapacidadTecnico.Ejecutar(cs, agenda.EntradaFijarCapacidad{ID: f.ID, Version: f.Version, Capacidad: nil}))(t)

	// Capacidad de la cuadrilla: 150 minutos por día en total (o1: 60).
	c = deber(e.ag.CapacidadCuadrilla.Ejecutar(cs, agenda.EntradaFijarCapacidad{ID: c.ID, Version: c.Version,
		Capacidad: []agenda.Capacidad{{Minutos: ptr(int32(150))}}}))(t)
	o3 := orden(t, e, cd, "instalacion", z.ID) // 120 min
	_, err = programar(e, cd, o3, tec, gt(2, 13, 0))
	if p := problema(t, err, 422, "capacidad_excedida"); p.Detail[:12] != "La cuadrilla" {
		t.Fatalf("capacidad de la cuadrilla: %s", p.Detail)
	}
	o3 = deber(e.cmp.ProgramarOrden.Ejecutar(cd, campo.EntradaProgramarOrden{ID: o3.ID, Version: o3.Version, TecnicoID: &tec,
		Inicio: gt(2, 13, 0), Fin: ptr(gt(2, 14, 30))}))(t)

	// Reprogramar: sin ventana nueva conserva la anterior si el inicio cae en ella; si no, la quita.
	o1 = deber(e.cmp.ReprogramarOrden.Ejecutar(cd, campo.EntradaProgramarOrden{ID: o1.ID, Version: o1.Version, Inicio: gt(2, 11, 0), Motivo: ptr("Cliente")}))(t)
	if c := deber(agenda.CitasDeOrdenes(context.Background(), pruebas.Pool(t), []int64{o1.ID}))(t)[o1.ID]; c.VentanaInicio == nil || c.Version != 2 {
		t.Fatalf("ventana conservada: %+v", c)
	}
	if p := ultimoEvento(t, "orden.programada", o1.ID); p["reprogramada"] != true || p["motivo"] != "Cliente" {
		t.Fatalf("reprogramada: %+v", p)
	}
	o1 = deber(e.cmp.ReprogramarOrden.Ejecutar(cd, campo.EntradaProgramarOrden{ID: o1.ID, Version: o1.Version, Inicio: gt(4, 9, 0), Motivo: ptr("Otro día")}))(t)
	if c := deber(agenda.CitasDeOrdenes(context.Background(), pruebas.Pool(t), []int64{o1.ID}))(t)[o1.ID]; c.VentanaInicio != nil {
		t.Fatalf("ventana descartada: %+v", c)
	}

	// Cambiar el técnico de una orden programada pasa por la agenda.
	tec2 := usuarioCon(t, rol("tecnico"))
	_, err = e.cmp.AsignarOrden.Ejecutar(cd, campo.EntradaAsignarOrden{ID: o3.ID, Version: o3.Version, TecnicoID: &tec2})
	problema(t, err, 422, "tecnico_sin_agenda")
	fichaCon(t, e, cs, tec2, nil)
	o3 = deber(e.cmp.AsignarOrden.Ejecutar(cd, campo.EntradaAsignarOrden{ID: o3.ID, Version: o3.Version, TecnicoID: &tec2}))(t)
	if p := ultimoEvento(t, "orden.programada", o3.ID); p["origen"] != "asignar" || p["tecnico_id"] != float64(tec2) {
		t.Fatalf("asignar con cita: %+v", p)
	}
	// Ya no ocupa al primer técnico: o2 cabe a esa hora.
	o4 := orden(t, e, cd, "visita", z.ID)
	o4 = deber(programar(e, cd, o4, tec, gt(2, 13, 0)))(t)

	// Sin técnico o cancelada: la cita se libera.
	deber(e.cmp.AsignarOrden.Ejecutar(cd, campo.EntradaAsignarOrden{ID: o3.ID, Version: o3.Version}))(t)
	deber(e.cmp.CancelarOrden.Ejecutar(cd, campo.EntradaMotivo{ID: o4.ID, Version: o4.Version, Motivo: "Desistió"}))(t)
	if n := contar(t, "SELECT count(*) FROM agenda.citas WHERE orden_id = ANY($1)", []int64{o3.ID, o4.ID}); n != 0 {
		t.Fatalf("citas liberadas: %d", n)
	}

	// El paquete del técnico trae la cita.
	e.reloj.mu.Lock()
	e.reloj.t = gt(4, 7, 0)
	e.reloj.mu.Unlock()
	paq := deber(e.cmp.Paquete(contexto.ConCanal(como(t, tec, "tecnico"), contexto.CanalCampo), campo.EntradaPaquete{}))(t)
	vistas := 0
	for _, o := range paq.Ordenes {
		if o.ID == o1.ID || o.ID == o2.ID {
			vistas++
			if o.Cita == nil || o.Cita.TecnicoID != tec {
				t.Fatalf("cita en el paquete: %+v", o.Cita)
			}
		}
	}
	if vistas != 2 {
		t.Fatalf("paquete del viernes: %d de 2", vistas)
	}
}

// Asignables (pedido de I3): técnicos de la zona con su disponibilidad del día.
func TestAsignablesConTurnoAusenciaYCapacidad(t *testing.T) {
	e := nuevoEntorno()
	sup := usuarioCon(t, rol("supervisor"))
	cs := como(t, sup, "supervisor")
	cd := como(t, usuarioCon(t, rol("despacho")), "despacho")
	z := deber(e.ag.CrearZona.Ejecutar(cs, agenda.EntradaCrearZona{Nombre: "Zona asignables " + pruebas.Unico()}))(t)
	otra := deber(e.ag.CrearZona.Ejecutar(cs, agenda.EntradaCrearZona{Nombre: "Otra zona " + pruebas.Unico()}))(t)
	c := deber(e.ag.CrearCuadrilla.Ejecutar(cs, agenda.EntradaCrearCuadrilla{Nombre: "Cuadrilla asignables " + pruebas.Unico(), ZonaIDs: []int64{z.ID}}))(t)
	lejos := deber(e.ag.CrearCuadrilla.Ejecutar(cs, agenda.EntradaCrearCuadrilla{Nombre: "Cuadrilla lejana " + pruebas.Unico(), ZonaIDs: []int64{otra.ID}}))(t)

	libre, ausente, sinTurno, lleno, ocupado := usuarioCon(t, rol("tecnico")), usuarioCon(t, rol("tecnico")), usuarioCon(t, rol("tecnico")),
		usuarioCon(t, rol("tecnico")), usuarioCon(t, rol("tecnico"))
	deLejos := usuarioCon(t, rol("tecnico"))
	fichaCon(t, e, cs, libre, &c.ID)
	fa := fichaCon(t, e, cs, ausente, &c.ID)
	deber(e.ag.RegistrarAusencia.Ejecutar(cs, agenda.EntradaRegistrarAusencia{ID: fa.ID, Inicio: gt(2, 0, 0).Format(time.RFC3339),
		Fin: gt(3, 0, 0).Format(time.RFC3339), Motivo: "enfermedad"}))(t)
	deber(e.ag.CrearTecnico.Ejecutar(cs, agenda.EntradaCrearTecnico{UsuarioID: sinTurno, CuadrillaID: &c.ID}))(t)
	fl := fichaCon(t, e, cs, lleno, &c.ID)
	deber(e.ag.CapacidadTecnico.Ejecutar(cs, agenda.EntradaFijarCapacidad{ID: fl.ID, Version: fl.Version,
		Capacidad: []agenda.Capacidad{{Tipo: ptr("visita"), Cupos: ptr(int32(1))}}}))(t)
	deber(programar(e, cd, orden(t, e, cd, "visita", z.ID), lleno, gt(2, 8, 0)))(t)
	fichaCon(t, e, cs, ocupado, &c.ID)
	// Ocupado de 08:00 a 16:30: le queda media hora (no cabe una visita de 60 min).
	o := orden(t, e, cd, "mantenimiento", z.ID)
	deber(e.cmp.ProgramarOrden.Ejecutar(cd, campo.EntradaProgramarOrden{ID: o.ID, Version: o.Version, TecnicoID: &ocupado, Inicio: gt(2, 8, 0), Fin: ptr(gt(2, 16, 30))}))(t)
	fichaCon(t, e, cs, deLejos, &lejos.ID)

	r := deber(e.ag.Asignables.Ejecutar(cd, agenda.EntradaAsignables{ZonaID: z.ID, Fecha: "2026-10-28", Tipo: "visita"}))(t)
	if r.DuracionMin != 60 || r.Fecha != "2026-10-28" {
		t.Fatalf("respuesta: %+v", r)
	}
	motivos := map[int64]string{}
	for _, a := range r.Data {
		m := "disponible"
		if a.Motivo != nil {
			m = *a.Motivo
		}
		motivos[a.UsuarioID] = m
	}
	esperado := map[int64]string{libre: "disponible", ausente: "ausente", sinTurno: "sin_turno", lleno: "capacidad_completa", ocupado: "sin_espacio"}
	for u, m := range esperado {
		if motivos[u] != m {
			t.Fatalf("técnico %d: %q (esperaba %q) en %+v", u, motivos[u], m, motivos)
		}
	}
	if _, ok := motivos[deLejos]; ok {
		t.Fatal("un técnico de una cuadrilla que no atiende la zona no es asignable")
	}
	// Los disponibles primero (también los técnicos sin cuadrilla de otras pruebas: atienden
	// todas las zonas).
	visto := false
	for _, a := range r.Data {
		if !a.Disponible {
			visto = true
		} else if visto {
			t.Fatalf("un disponible después de uno no disponible: %+v", r.Data)
		}
		if a.UsuarioID == libre && (a.MinutosLibres != 9*60 || len(a.Turnos) != 1 || len(a.Libres) != 1) {
			t.Fatalf("libre: %+v", a)
		}
	}
	for _, a := range r.Data {
		if a.UsuarioID == lleno && (len(a.Capacidad) != 1 || a.Capacidad[0].CuposUsados != 1 || len(a.Ocupado) != 1) {
			t.Fatalf("capacidad usada: %+v", a)
		}
	}
	// Sin tipo: el lleno vuelve a estar disponible (la línea es solo de visitas).
	r = deber(e.ag.Asignables.Ejecutar(cd, agenda.EntradaAsignables{ZonaID: z.ID, Fecha: "2026-10-28", Tipo: "instalacion"}))(t)
	for _, a := range r.Data {
		if a.UsuarioID == lleno && !a.Disponible {
			t.Fatalf("instalación con la línea de visitas llena: %+v", a)
		}
	}
	_, err := e.ag.Asignables.Ejecutar(cd, agenda.EntradaAsignables{Fecha: "28-10-2026"})
	problema(t, err, 422, "validacion")
}

// Calendario de despacho: citas y ausencias del rango, según el alcance.
func TestCalendarioDeDespacho(t *testing.T) {
	e := nuevoEntorno()
	sup := usuarioCon(t, rol("supervisor"))
	cs := como(t, sup, "supervisor")
	desp := usuarioCon(t, rol("despacho"))
	cd := como(t, desp, "despacho")
	z1 := deber(e.ag.CrearZona.Ejecutar(cs, agenda.EntradaCrearZona{Nombre: "Calendario 1 " + pruebas.Unico()}))(t)
	z2 := deber(e.ag.CrearZona.Ejecutar(cs, agenda.EntradaCrearZona{Nombre: "Calendario 2 " + pruebas.Unico()}))(t)
	tec := usuarioCon(t, rol("tecnico"))
	f := fichaCon(t, e, cs, tec, nil)
	a := deber(programar(e, cd, orden(t, e, cd, "visita", z1.ID), tec, gt(2, 9, 0)))(t)
	b := deber(programar(e, cd, orden(t, e, cd, "visita", z2.ID), tec, gt(2, 11, 0)))(t)
	deber(e.ag.RegistrarAusencia.Ejecutar(cs, agenda.EntradaRegistrarAusencia{ID: f.ID, Inicio: gt(2, 14, 0).Format(time.RFC3339),
		Fin: gt(2, 17, 0).Format(time.RFC3339), Motivo: "permiso"}))(t)

	rango := agenda.EntradaCalendario{Desde: gt(2, 0, 0).Format(time.RFC3339), Hasta: gt(3, 0, 0).Format(time.RFC3339), TecnicoID: tec}
	cal := deber(e.ag.Calendario.Ejecutar(cd, rango))(t)
	if len(cal.Citas) != 2 || cal.Citas[0].OrdenID != a.ID || cal.Citas[1].OrdenID != b.ID || cal.Citas[0].Tipo != "visita" ||
		cal.Citas[0].OrdenVersion != a.Version || len(cal.Ausencias) != 1 || cal.Ausencias[0].TecnicoID != tec {
		t.Fatalf("calendario: %+v", cal)
	}
	// Con alcance de la zona 1 solo ve esa cita.
	limitado := conAlcance(t, desp, map[string][]contexto.Alcance{"agenda.ver": {{ZonaID: z1.ID}}})
	cal = deber(e.ag.Calendario.Ejecutar(limitado, rango))(t)
	if len(cal.Citas) != 1 || cal.Citas[0].OrdenID != a.ID {
		t.Fatalf("calendario con alcance: %+v", cal.Citas)
	}
	porZona := rango
	porZona.ZonaID = z2.ID
	if cal := deber(e.ag.Calendario.Ejecutar(cd, porZona))(t); len(cal.Citas) != 1 || cal.Citas[0].OrdenID != b.ID {
		t.Fatalf("filtro por zona: %+v", cal.Citas)
	}
	largo := rango
	largo.Hasta = gt(40, 0, 0).Format(time.RFC3339)
	_, err := e.ag.Calendario.Ejecutar(cd, largo)
	problema(t, err, 422, "validacion")
}
