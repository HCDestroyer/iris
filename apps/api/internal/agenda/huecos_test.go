package agenda_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/agenda"
	"github.com/HCDestroyer/iris/apps/api/internal/campo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// Huecos ofrecibles (contratos/conversaciones.md §17.10): ventanas en las que una visita cabe en la
// agenda de la zona, con las reglas de ReservarCita y las tareas pendientes; sin técnicos en la salida.
// Las pruebas usan el sábado: las fichas de otras pruebas del paquete (sin cuadrilla, atienden todas
// las zonas) trabajan de lunes a viernes.
func TestHuecosOfrecibles(t *testing.T) {
	e := nuevoEntorno()
	bd := pruebas.Pool(t)
	ctx := context.Background()
	sup := usuarioCon(t, rol("supervisor"))
	cs := como(t, sup, "supervisor")
	cd := como(t, usuarioCon(t, rol("despacho")), "despacho")
	z := deber(e.ag.CrearZona.Ejecutar(cs, agenda.EntradaCrearZona{Nombre: "Zona huecos " + pruebas.Unico()}))(t)
	c := deber(e.ag.CrearCuadrilla.Ejecutar(cs, agenda.EntradaCrearCuadrilla{Nombre: "Cuadrilla huecos " + pruebas.Unico(), ZonaIDs: []int64{z.ID}}))(t)
	sabado := func(usuario int64) agenda.TecnicoDetalle {
		t.Helper()
		f := deber(e.ag.CrearTecnico.Ejecutar(cs, agenda.EntradaCrearTecnico{UsuarioID: usuario, CuadrillaID: &c.ID}))(t)
		return deber(e.ag.FijarTurnos.Ejecutar(cs, agenda.EntradaFijarTurnos{ID: f.ID, Version: f.Version, Turnos: []agenda.Turno{{Dia: 6, Inicio: "08:00", Fin: "17:00"}}}))(t)
	}
	bloques := []agenda.BloqueHora{{Inicio: "08:00", Fin: "12:00"}, {Inicio: "13:00", Fin: "17:00"}}
	pedir := func(zona int64, desde, hasta time.Time) agenda.Huecos {
		t.Helper()
		return deber(e.ag.HuecosOfrecibles(ctx, bd, agenda.EntradaHuecos{ZonaID: zona, Tipo: "visita", Desde: desde, Hasta: hasta,
			NoAntesDe: e.reloj.ahora().Add(12 * time.Hour), Bloques: bloques, Max: 3}))(t)
	}
	ventanas := func(h agenda.Huecos) string {
		s := ""
		for _, v := range h.Data {
			s += v.Inicio.In(time.FixedZone("GT", -6*3600)).Format("Mon15") + "-" + v.Fin.In(time.FixedZone("GT", -6*3600)).Format("15") + " "
		}
		return s
	}
	sab, dom := gt(5, 0, 0), gt(6, 0, 0)

	// Nadie atiende la zona el sábado todavía: sin ventanas. Y una orden sin zona no la cubre nadie.
	if h := pedir(z.ID, sab, dom); len(h.Data) != 0 {
		t.Fatalf("sin técnicos con turno: %s", ventanas(h))
	}
	if h := pedir(0, sab, dom); !h.SinCobertura || len(h.Data) != 0 {
		t.Fatalf("sin zona: %+v", h)
	}

	// Un técnico con turno el sábado: las dos ventanas del sábado (el domingo no trabaja).
	a := usuarioCon(t, rol("tecnico"))
	sabado(a)
	if h := pedir(z.ID, sab, dom); ventanas(h) != "Sat08-12 Sat13-17 " || h.SinCobertura {
		t.Fatalf("un técnico libre: %q", ventanas(h))
	}
	// La anticipación: nada antes de «no antes de».
	if h := deber(e.ag.HuecosOfrecibles(ctx, bd, agenda.EntradaHuecos{ZonaID: z.ID, Tipo: "visita", Desde: sab, Hasta: sab,
		NoAntesDe: gt(5, 9, 0), Bloques: bloques, Max: 3}))(t); ventanas(h) != "Sat13-17 " {
		t.Fatalf("anticipación: %q", ventanas(h))
	}
	// El tope de opciones.
	if h := deber(e.ag.HuecosOfrecibles(ctx, bd, agenda.EntradaHuecos{ZonaID: z.ID, Tipo: "visita", Desde: sab, Hasta: sab, Bloques: bloques, Max: 1}))(t); len(h.Data) != 1 {
		t.Fatalf("tope de opciones: %q", ventanas(h))
	}

	// Una cita que le ocupa toda la mañana: queda la tarde.
	o := orden(t, e, cd, "mantenimiento", z.ID)
	deber(e.cmp.ProgramarOrden.Ejecutar(cd, campo.EntradaProgramarOrden{ID: o.ID, Version: o.Version, TecnicoID: &a, Inicio: gt(5, 8, 0), Fin: ptr(gt(5, 12, 0))}))(t)
	if h := pedir(z.ID, sab, dom); ventanas(h) != "Sat13-17 " {
		t.Fatalf("con la mañana ocupada: %q", ventanas(h))
	}
	// ElegirTecnicoEnVentana: en la tarde cabe (a las 13:00); en la mañana ya no.
	en := agenda.EntradaHuecos{ZonaID: z.ID, Tipo: "visita"}
	el := deber(e.ag.ElegirTecnicoEnVentana(ctx, bd, en, agenda.Hueco{Inicio: gt(5, 13, 0), Fin: gt(5, 17, 0)}))(t)
	if el.UsuarioID != a || !el.Inicio.Equal(gt(5, 13, 0)) || !el.Fin.Equal(gt(5, 14, 0)) {
		t.Fatalf("técnico elegido: %+v", el)
	}
	if _, err := e.ag.ElegirTecnicoEnVentana(ctx, bd, en, agenda.Hueco{Inicio: gt(5, 8, 0), Fin: gt(5, 12, 0)}); err == nil {
		t.Fatal("la mañana ocupada no debía tener técnico")
	} else {
		problema(t, err, 422, "ventana_no_disponible")
	}
	// La cita de la propia orden no cuenta (reprogramarla en su misma ventana).
	en.ExcluirOrden = o.ID
	deber(e.ag.ElegirTecnicoEnVentana(ctx, bd, en, agenda.Hueco{Inicio: gt(5, 8, 0), Fin: gt(5, 12, 0)}))(t)

	// Una tarea pendiente que le ocupa la tarde: sin ventanas.
	if _, err := bd.Exec(ctx, `INSERT INTO agenda.tareas (id, tecnico_id, que, inicio, duracion_min, fin) VALUES (gen_random_uuid(), $1, 'Inventario', $2, 240, $3)`,
		a, gt(5, 13, 0), gt(5, 17, 0)); err != nil {
		t.Fatal(err)
	}
	if h := pedir(z.ID, sab, dom); len(h.Data) != 0 || h.SinCobertura {
		t.Fatalf("con la tarde en una tarea: %q (sin cobertura %v)", ventanas(h), h.SinCobertura)
	}

	// Otro técnico, ausente todo el sábado: sigue sin ventanas.
	b := usuarioCon(t, rol("tecnico"))
	fb := sabado(b)
	deber(e.ag.RegistrarAusencia.Ejecutar(cs, agenda.EntradaRegistrarAusencia{ID: fb.ID, Inicio: gt(5, 0, 0).Format(time.RFC3339),
		Fin: gt(6, 0, 0).Format(time.RFC3339), Motivo: "permiso"}))(t)
	if h := pedir(z.ID, sab, dom); len(h.Data) != 0 {
		t.Fatalf("con el otro técnico ausente: %q", ventanas(h))
	}

	// Un tercero con capacidad de una visita al día, ya usada: tampoco suma ventanas.
	k := usuarioCon(t, rol("tecnico"))
	fk := sabado(k)
	deber(e.ag.CapacidadTecnico.Ejecutar(cs, agenda.EntradaFijarCapacidad{ID: fk.ID, Version: fk.Version,
		Capacidad: []agenda.Capacidad{{Tipo: ptr("visita"), Cupos: ptr(int32(1))}}}))(t)
	deber(programar(e, cd, orden(t, e, cd, "visita", z.ID), k, gt(5, 16, 0)))(t)
	if h := pedir(z.ID, sab, dom); len(h.Data) != 0 {
		t.Fatalf("con la capacidad del día usada: %q", ventanas(h))
	}

	// Un cuarto, libre: vuelven las dos ventanas, y es él quien se elige en las dos.
	d := usuarioCon(t, rol("tecnico"))
	sabado(d)
	if h := pedir(z.ID, sab, dom); ventanas(h) != "Sat08-12 Sat13-17 " {
		t.Fatalf("con un técnico libre más: %q", ventanas(h))
	}
	en.ExcluirOrden = 0
	if el := deber(e.ag.ElegirTecnicoEnVentana(ctx, bd, en, agenda.Hueco{Inicio: gt(5, 8, 0), Fin: gt(5, 12, 0)}))(t); el.UsuarioID != d {
		t.Fatalf("debía elegir al libre: %+v", el)
	}
	// La salida no trae técnicos, cuadrillas ni cuántos hay libres.
	if s := fmt.Sprintf("%+v", pedir(z.ID, sab, dom)); s == "" || len(pedir(z.ID, sab, dom).Data) != 2 {
		t.Fatalf("salida: %s", s)
	}
	// Tipo desconocido.
	if _, err := e.ag.HuecosOfrecibles(ctx, bd, agenda.EntradaHuecos{ZonaID: z.ID, Tipo: "paseo", Desde: sab, Hasta: sab, Bloques: bloques}); err == nil {
		t.Fatal("tipo desconocido")
	}
}
