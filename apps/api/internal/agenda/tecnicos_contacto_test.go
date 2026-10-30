package agenda_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/agenda"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// Teléfono de la ficha (crear, editar, validar, auditar y emitir) y candidatos a ficha.
func TestTelefonoYCandidatosDeTecnico(t *testing.T) {
	e := nuevoEntorno()
	sup := usuarioCon(t, rol("supervisor"))
	cs := como(t, sup, "supervisor")
	c := deber(e.ag.CrearCuadrilla.Ejecutar(cs, agenda.EntradaCrearCuadrilla{Nombre: "Cuadrilla contacto " + pruebas.Unico()}))(t)

	tec := usuarioCon(t, rol("tecnico"))
	ventas := usuarioCon(t, rol("ventas"))

	// Candidatos: el técnico sin ficha sí; ventas (sin permisos de órdenes) no.
	tiene := func(p []agenda.CandidatoTecnico, id int64) bool {
		for _, x := range p {
			if x.UsuarioID == id {
				return true
			}
		}
		return false
	}
	todos := func(q string) []agenda.CandidatoTecnico {
		var salida []agenda.CandidatoTecnico
		cursor := ""
		for {
			p := deber(e.ag.CandidatosTecnico.Ejecutar(cs, agenda.EntradaCandidatosTecnico{Q: q, ParamsPagina: paginaCon(cursor)}))(t)
			salida = append(salida, p.Data...)
			if p.NextCursor == nil {
				return salida
			}
			cursor = *p.NextCursor
		}
	}
	lista := todos("")
	if !tiene(lista, tec) || tiene(lista, ventas) {
		t.Fatalf("candidatos: técnico %v, ventas %v", tiene(lista, tec), tiene(lista, ventas))
	}
	// Sin el permiso de crear fichas → 403.
	_, err := e.ag.CandidatosTecnico.Ejecutar(como(t, tec, "tecnico"), agenda.EntradaCandidatosTecnico{})
	problema(t, err, 403, "prohibido")

	// Teléfono inválido → 422 en el campo.
	_, err = e.ag.CrearTecnico.Ejecutar(cs, agenda.EntradaCrearTecnico{UsuarioID: tec, CuadrillaID: &c.ID, Telefono: "llámame"})
	if p := problema(t, err, 422, "validacion"); p.Errors[0].Field != "telefono" {
		t.Fatalf("teléfono inválido: %+v", p.Errors)
	}
	_, err = e.ag.CrearTecnico.Ejecutar(cs, agenda.EntradaCrearTecnico{UsuarioID: tec, CuadrillaID: &c.ID, Telefono: "123"})
	problema(t, err, 422, "validacion")

	f := deber(e.ag.CrearTecnico.Ejecutar(cs, agenda.EntradaCrearTecnico{UsuarioID: tec, CuadrillaID: &c.ID, Telefono: " +502 5555-1234 "}))(t)
	if f.Telefono != "+502 5555-1234" {
		t.Fatalf("teléfono al crear: %q", f.Telefono)
	}
	// Ya tiene ficha: deja de ser candidato.
	if tiene(todos(""), tec) {
		t.Fatal("con ficha sigue como candidato")
	}

	// Editar solo el teléfono: emite tecnico.actualizado con campos [telefono] y lo audita.
	f2 := deber(e.ag.EditarTecnico.Ejecutar(cs, agenda.EntradaEditarTecnico{ID: f.ID, Version: f.Version, CuadrillaID: &c.ID,
		Habilidades: f.Habilidades, Telefono: "(502) 4444 0000"}))(t)
	if f2.Telefono != "(502) 4444 0000" || f2.Version != f.Version+1 {
		t.Fatalf("teléfono editado: %+v", f2)
	}
	if n := contar(t, `SELECT count(*) FROM integracion.outbox WHERE evento = 'tecnico.actualizado'
		AND (payload->>'tecnico_id')::bigint = $1 AND payload->'campos' = '["telefono"]'::jsonb`, f.ID); n != 1 {
		t.Fatalf("evento con campos [telefono]: %d", n)
	}
	if n := contar(t, `SELECT count(*) FROM auditoria.cambios WHERE entidad = 'tecnico' AND entidad_id = $1::bigint::text AND campo ILIKE 'telefono'`, f.ID); n < 1 {
		t.Fatalf("auditoría del teléfono: %d", n)
	}
	// La lista también lo trae.
	l := deber(e.ag.ListarTecnicos.Ejecutar(cs, agenda.EntradaListarTecnicos{UsuarioID: tec}))(t)
	if len(l.Data) != 1 || l.Data[0].Telefono != "(502) 4444 0000" {
		t.Fatalf("lista: %+v", l.Data)
	}
	// Mandar el mismo teléfono (y lo demás igual) → sin cambios.
	_, err = e.ag.EditarTecnico.Ejecutar(cs, agenda.EntradaEditarTecnico{ID: f.ID, Version: f2.Version, CuadrillaID: &c.ID,
		Habilidades: f.Habilidades, Telefono: "(502) 4444 0000"})
	problema(t, err, 422, "sin_cambios")
}

func paginaCon(cursor string) nucleo.ParamsPagina {
	return nucleo.ParamsPagina{Cursor: cursor, Limit: 200}
}

// Candidatos: solo técnicos de campo (ordenes.ver_propias), con el alcance del permiso de crear,
// y la acción de auditoría propia.
func TestCandidatosSoloTecnicosYConAlcance(t *testing.T) {
	e := nuevoEntorno()
	sup := usuarioCon(t, rol("supervisor"))
	cs := como(t, sup, "supervisor")
	z := deber(e.ag.CrearZona.Ejecutar(cs, agenda.EntradaCrearZona{Nombre: "Zona candidatos " + pruebas.Unico()}))(t)
	otra := deber(e.ag.CrearZona.Ejecutar(cs, agenda.EntradaCrearZona{Nombre: "Otra zona candidatos " + pruebas.Unico()}))(t)

	global := usuarioCon(t, rol("tecnico"))
	deZona := usuarioCon(t, identidad.AsignacionRol{Rol: "tecnico", ZonaID: &z.ID})
	deOtra := usuarioCon(t, identidad.AsignacionRol{Rol: "tecnico", ZonaID: &otra.ID})
	soloVer := usuarioCon(t, rol("lectura")) // ordenes.ver, sin ordenes.ver_propias

	todos := func(ctx context.Context) map[int64]bool {
		salida := map[int64]bool{}
		cursor := ""
		for {
			p := deber(e.ag.CandidatosTecnico.Ejecutar(ctx, agenda.EntradaCandidatosTecnico{ParamsPagina: paginaCon(cursor)}))(t)
			for _, c := range p.Data {
				salida[c.UsuarioID] = true
			}
			if p.NextCursor == nil {
				return salida
			}
			cursor = *p.NextCursor
		}
	}
	g := todos(cs)
	if !g[global] || !g[deZona] || !g[deOtra] || g[soloVer] {
		t.Fatalf("alcance global: global %v, zona %v, otra %v, solo ordenes.ver %v", g[global], g[deZona], g[deOtra], g[soloVer])
	}
	limitado := conAlcance(t, sup, map[string][]contexto.Alcance{"agenda.crear_tecnico": {{ZonaID: z.ID}}})
	l := todos(limitado)
	if !l[deZona] || l[deOtra] || l[global] {
		t.Fatalf("alcance de zona: zona %v, otra %v, global %v", l[deZona], l[deOtra], l[global])
	}
	// Sin el permiso: 403 auditado con su acción propia.
	_, err := e.ag.CandidatosTecnico.Ejecutar(como(t, soloVer, "lectura"), agenda.EntradaCandidatosTecnico{})
	problema(t, err, 403, "prohibido")
	if n := contar(t, "SELECT count(*) FROM auditoria.acciones WHERE accion = 'agenda.candidatos_tecnico' AND resultado <> 'ok' AND actor_id = $1", soloVer); n < 1 {
		t.Fatalf("auditoría de candidatos: %d", n)
	}
}

// Calendario: `truncado` al pasar el límite y conflictos marcados en el servidor, aunque la
// otra cita o la ausencia queden fuera del filtro.
func TestCalendarioTruncadoYConflictos(t *testing.T) {
	e := nuevoEntorno()
	sup := usuarioCon(t, rol("supervisor"))
	cs := como(t, sup, "supervisor")
	z1 := deber(e.ag.CrearZona.Ejecutar(cs, agenda.EntradaCrearZona{Nombre: "Zona cal 1 " + pruebas.Unico()}))(t)
	z2 := deber(e.ag.CrearZona.Ejecutar(cs, agenda.EntradaCrearZona{Nombre: "Zona cal 2 " + pruebas.Unico()}))(t)
	c := deber(e.ag.CrearCuadrilla.Ejecutar(cs, agenda.EntradaCrearCuadrilla{Nombre: "Cuadrilla cal " + pruebas.Unico(), ZonaIDs: []int64{z1.ID, z2.ID}}))(t)
	tec := usuarioCon(t, rol("tecnico"))
	f := fichaCon(t, e, cs, tec, &c.ID)

	o1 := deber(programar(e, cs, orden(t, e, cs, "visita", z1.ID), tec, gt(2, 9, 0)))(t)
	o2 := deber(programar(e, cs, orden(t, e, cs, "visita", z2.ID), tec, gt(2, 13, 0)))(t)
	o3 := deber(programar(e, cs, orden(t, e, cs, "visita", z1.ID), tec, gt(2, 15, 0)))(t)
	// o2 se cruza con o1 (se mueve por SQL: la agenda no deja crear un solape) y una ausencia cae en o3.
	if _, err := pruebas.Pool(t).Exec(context.Background(), "UPDATE agenda.citas SET inicio = $2, fin = $3 WHERE orden_id = $1", o2.ID, gt(2, 9, 30), gt(2, 10, 30)); err != nil {
		t.Fatal(err)
	}
	deber(e.ag.RegistrarAusencia.Ejecutar(cs, agenda.EntradaRegistrarAusencia{ID: f.ID, Inicio: gt(2, 15, 0).Format(time.RFC3339), Fin: gt(2, 16, 0).Format(time.RFC3339), Motivo: "permiso"}))(t)

	rango := agenda.EntradaCalendario{Desde: gt(2, 0, 0).Format(time.RFC3339), Hasta: gt(3, 0, 0).Format(time.RFC3339), TecnicoID: tec}
	cal := deber(e.ag.Calendario.Ejecutar(cs, rango))(t)
	if cal.Truncado || len(cal.Citas) != 3 {
		t.Fatalf("calendario: truncado %v, %d citas", cal.Truncado, len(cal.Citas))
	}
	conflictos := map[int64][]string{}
	for _, x := range cal.Citas {
		conflictos[x.OrdenID] = x.Conflictos
	}
	if !slices.Equal(conflictos[o1.ID], []string{"solape"}) || !slices.Equal(conflictos[o2.ID], []string{"solape"}) || !slices.Equal(conflictos[o3.ID], []string{"ausencia"}) {
		t.Fatalf("conflictos: %+v", conflictos)
	}
	// Con el filtro de zona 1, o2 no viene, pero o1 sigue en conflicto con ella.
	filtrado := rango
	filtrado.ZonaID = z1.ID
	cal = deber(e.ag.Calendario.Ejecutar(cs, filtrado))(t)
	for _, x := range cal.Citas {
		if x.OrdenID == o2.ID {
			t.Fatal("o2 no es de la zona 1")
		}
		if x.OrdenID == o1.ID && !slices.Equal(x.Conflictos, []string{"solape"}) {
			t.Fatalf("o1 filtrada: %+v", x.Conflictos)
		}
	}

	// Con un límite de 2 citas, la respuesta sale truncada.
	corto := agenda.Nuevo(agenda.Config{Reloj: e.reloj.ahora, LimiteCalendario: 2})
	cal = deber(corto.Calendario.Ejecutar(cs, rango))(t)
	if !cal.Truncado || len(cal.Citas) != 2 {
		t.Fatalf("truncado: %v, %d citas", cal.Truncado, len(cal.Citas))
	}
}
