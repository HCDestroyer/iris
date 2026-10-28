package agenda_test

import (
	"context"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/agenda"
	"github.com/HCDestroyer/iris/apps/api/internal/campo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
	"github.com/HCDestroyer/iris/apps/api/internal/soporte"
)

// registrarEmpresas da de alta empresas (sale del modo sin conector) y las borra al terminar.
func registrarEmpresas(t *testing.T, ids ...int64) {
	t.Helper()
	pool := pruebas.Pool(t)
	for _, id := range ids {
		if _, err := pool.Exec(context.Background(), "INSERT INTO nucleo.empresas (id, nombre) VALUES ($1::bigint, 'Empresa ' || $1::bigint::text)", id); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		for _, sql := range []string{"DELETE FROM nucleo.usuarios_empresas", "DELETE FROM nucleo.empresas"} {
			if _, err := pool.Exec(context.Background(), sql); err != nil {
				t.Error(err)
			}
		}
	})
}

func asignarEmpresa(t *testing.T, usuario int64, empresas ...int64) {
	t.Helper()
	for _, e := range empresas {
		if _, err := pruebas.Pool(t).Exec(context.Background(), "INSERT INTO nucleo.usuarios_empresas (usuario_id, empresa_id) VALUES ($1, $2)", usuario, e); err != nil {
			t.Fatal(err)
		}
	}
}

func enEmpresa(t *testing.T, usuario, empresa int64, rol string) context.Context {
	return pruebas.ContextoUsuarioEnEmpresa(pruebas.Pool(t), usuario, empresa, permisosDe(rol)...)
}

// Punto 3: el catálogo sin empresa solo lo modifica quien tiene empresas.todas (o el modo sin
// conector) y, con empresas registradas, no se usa al asignar.
func TestRevisionCatalogoSinEmpresa(t *testing.T) {
	e := nuevoEntorno()
	sinConector := como(t, usuarioCon(t, rol("supervisor")), "supervisor")
	z := deber(e.ag.CrearZona.Ejecutar(sinConector, agenda.EntradaCrearZona{Nombre: "Común " + pruebas.Unico()}))(t)
	c := deber(e.ag.CrearCuadrilla.Ejecutar(sinConector, agenda.EntradaCrearCuadrilla{Nombre: "Común " + pruebas.Unico()}))(t)

	registrarEmpresas(t, 41, 42)
	sup, adm := usuarioCon(t, rol("supervisor")), usuarioCon(t, rol("admin"))
	asignarEmpresa(t, sup, 41)
	cs, ca := enEmpresa(t, sup, 41, "supervisor"), enEmpresa(t, adm, 41, "admin")
	_, err := e.ag.EditarZona.Ejecutar(cs, agenda.EntradaEditarZona{ID: z.ID, Version: z.Version, Nombre: ptr("Mía")})
	problema(t, err, 403, "prohibido")
	_, err = e.ag.DesactivarCuadrilla.Ejecutar(cs, agenda.EntradaVersion{ID: c.ID, Version: c.Version})
	problema(t, err, 403, "prohibido")
	_, err = e.ag.CapacidadCuadrilla.Ejecutar(cs, agenda.EntradaFijarCapacidad{ID: c.ID, Version: c.Version, Capacidad: []agenda.Capacidad{{Cupos: ptr(int32(1))}}})
	problema(t, err, 403, "prohibido")
	z = deber(e.ag.EditarZona.Ejecutar(ca, agenda.EntradaEditarZona{ID: z.ID, Version: z.Version, Nombre: ptr("Común editada " + pruebas.Unico())}))(t)

	// Con empresas registradas, una zona o cuadrilla sin empresa no sirve para tickets ni órdenes.
	sop := soporte.Nuevo(soporte.Config{})
	_, err = sop.CrearTicket.Ejecutar(cs, soporte.EntradaCrearTicket{Asunto: "Zona común", ZonaID: &z.ID})
	problema(t, err, 422, "validacion")
	propia := deber(e.ag.CrearZona.Ejecutar(cs, agenda.EntradaCrearZona{Nombre: "Zona 41 " + pruebas.Unico()}))(t)
	if propia.EmpresaID == nil || *propia.EmpresaID != 41 {
		t.Fatalf("la zona nace de la empresa activa: %+v", propia)
	}
	tk := deber(sop.CrearTicket.Ejecutar(cs, soporte.EntradaCrearTicket{Asunto: "Zona propia", ZonaID: &propia.ID}))(t)
	_, err = sop.AsignarTicket.Ejecutar(cs, soporte.EntradaAsignarTicket{ID: tk.ID, Version: tk.Version, CuadrillaID: &c.ID})
	problema(t, err, 422, "validacion")
	_, err = e.cmp.CrearOrden.Ejecutar(cs, campo.EntradaCrearOrden{Tipo: "visita", Direccion: ptr("x"), ZonaID: &propia.ID, CuadrillaID: &c.ID})
	problema(t, err, 422, "validacion")
	// La de otra empresa tampoco.
	otra := deber(e.ag.CrearZona.Ejecutar(enEmpresa(t, adm, 42, "admin"), agenda.EntradaCrearZona{Nombre: "Zona 42 " + pruebas.Unico()}))(t)
	_, err = sop.CrearTicket.Ejecutar(cs, soporte.EntradaCrearTicket{Asunto: "Zona ajena", ZonaID: &otra.ID})
	problema(t, err, 422, "validacion")
	_, err = e.ag.EditarZona.Ejecutar(cs, agenda.EntradaEditarZona{ID: otra.ID, Version: otra.Version, Nombre: ptr("x")})
	problema(t, err, 404, "no_encontrado")
}

// Puntos 4 y 5: la ficha se modifica solo desde la empresa de su cuadrilla (o con empresas.todas)
// y el detalle de sus ausencias solo lo ve esa empresa; una cita de otra empresa en asignables es
// solo «ocupado».
func TestRevisionFichaPorEmpresaDeSuCuadrilla(t *testing.T) {
	e := nuevoEntorno()
	registrarEmpresas(t, 41, 42)
	sup, adm, desp := usuarioCon(t, rol("supervisor")), usuarioCon(t, rol("admin")), usuarioCon(t, rol("despacho"))
	tec, sinCuadrilla := usuarioCon(t, rol("tecnico")), usuarioCon(t, rol("tecnico"))
	asignarEmpresa(t, sup, 41, 42)
	asignarEmpresa(t, desp, 41, 42)
	asignarEmpresa(t, tec, 41, 42)
	asignarEmpresa(t, sinCuadrilla, 41)
	s41, s42, a41 := enEmpresa(t, sup, 41, "supervisor"), enEmpresa(t, sup, 42, "supervisor"), enEmpresa(t, adm, 41, "admin")
	ca := deber(e.ag.CrearCuadrilla.Ejecutar(s41, agenda.EntradaCrearCuadrilla{Nombre: "A41 " + pruebas.Unico()}))(t)
	f := fichaCon(t, e, s41, tec, &ca.ID)

	// Desde la otra empresa del técnico: la ve, pero no la modifica.
	_, err := e.ag.FijarTurnos.Ejecutar(s42, agenda.EntradaFijarTurnos{ID: f.ID, Version: f.Version, Turnos: nil})
	problema(t, err, 403, "prohibido")
	_, err = e.ag.RegistrarAusencia.Ejecutar(s42, agenda.EntradaRegistrarAusencia{ID: f.ID, Inicio: gt(9, 0, 0).Format(time.RFC3339),
		Fin: gt(10, 0, 0).Format(time.RFC3339), Motivo: "otro", Detalle: "x"})
	problema(t, err, 403, "prohibido")
	_, err = e.ag.CapacidadTecnico.Ejecutar(s42, agenda.EntradaFijarCapacidad{ID: f.ID, Version: f.Version})
	problema(t, err, 403, "prohibido")
	r := deber(e.ag.RegistrarAusencia.Ejecutar(s41, agenda.EntradaRegistrarAusencia{ID: f.ID, Inicio: gt(9, 0, 0).Format(time.RFC3339),
		Fin: gt(10, 0, 0).Format(time.RFC3339), Motivo: "enfermedad", Detalle: "Diagnóstico privado"}))(t)
	_, err = e.ag.AnularAusencia.Ejecutar(s42, agenda.EntradaAnularAusencia{TecnicoID: f.ID, AusenciaID: r.Ausencia.ID, Version: 1})
	problema(t, err, 403, "prohibido")
	e.reloj.mu.Lock()
	e.reloj.t = gt(8, 9, 0)
	e.reloj.mu.Unlock()
	if d := deber(e.ag.VerTecnico.Ejecutar(s42, agenda.EntradaVerTecnico{ID: f.ID}))(t); len(d.Ausencias) != 1 || d.Ausencias[0].Detalle != "" {
		t.Fatalf("el detalle no sale a otra empresa: %+v", d.Ausencias)
	}
	if d := deber(e.ag.VerTecnico.Ejecutar(s41, agenda.EntradaVerTecnico{ID: f.ID}))(t); d.Ausencias[0].Detalle != "Diagnóstico privado" {
		t.Fatalf("la empresa de la ficha ve el detalle: %+v", d.Ausencias)
	}
	e.reloj.mu.Lock()
	e.reloj.t = gt(2, 9, 0)
	e.reloj.mu.Unlock()

	// Sin cuadrilla: solo con empresas.todas.
	_, err = e.ag.CrearTecnico.Ejecutar(s41, agenda.EntradaCrearTecnico{UsuarioID: sinCuadrilla})
	problema(t, err, 403, "prohibido")
	fs := deber(e.ag.CrearTecnico.Ejecutar(a41, agenda.EntradaCrearTecnico{UsuarioID: sinCuadrilla}))(t)
	_, err = e.ag.FijarTurnos.Ejecutar(s41, agenda.EntradaFijarTurnos{ID: fs.ID, Version: fs.Version})
	problema(t, err, 403, "prohibido")
	deber(e.ag.FijarTurnos.Ejecutar(a41, agenda.EntradaFijarTurnos{ID: fs.ID, Version: fs.Version, Turnos: []agenda.Turno{{Dia: 3, Inicio: "08:00", Fin: "17:00"}}}))(t)
	// Ni pasar la propia ficha a «sin cuadrilla».
	f = deber(e.ag.VerTecnico.Ejecutar(s41, agenda.EntradaVerTecnico{ID: f.ID}))(t)
	_, err = e.ag.EditarTecnico.Ejecutar(s41, agenda.EntradaEditarTecnico{ID: f.ID, Version: f.Version})
	problema(t, err, 403, "prohibido")

	// Punto 5: una cita de la empresa 42 se ve desde la 41 solo como «ocupado».
	d42 := enEmpresa(t, desp, 42, "despacho")
	z42 := deber(e.ag.CrearZona.Ejecutar(enEmpresa(t, adm, 42, "admin"), agenda.EntradaCrearZona{Nombre: "Z42 " + pruebas.Unico()}))(t)
	o := deber(e.cmp.CrearOrden.Ejecutar(d42, campo.EntradaCrearOrden{Tipo: "visita", Direccion: ptr("x"), ZonaID: &z42.ID}))(t)
	deber(programar(e, d42, o, tec, gt(2, 10, 0)))(t)
	as := deber(e.ag.Asignables.Ejecutar(enEmpresa(t, desp, 41, "despacho"), agenda.EntradaAsignables{Fecha: "2026-10-28"}))(t)
	visto := false
	for _, a := range as.Data {
		if a.UsuarioID == tec {
			for _, oc := range a.Ocupado {
				visto = true
				if oc.OrdenID != 0 || oc.Numero != "" || oc.Estado != "ocupado" || !oc.Inicio.Equal(gt(2, 10, 0)) {
					t.Fatalf("cita de otra empresa: %+v", oc)
				}
			}
		}
	}
	if !visto {
		t.Fatalf("la cita ocupa al técnico: %+v", as.Data)
	}
	// Desde la 42 sí se ve la orden.
	as = deber(e.ag.Asignables.Ejecutar(d42, agenda.EntradaAsignables{Fecha: "2026-10-28"}))(t)
	for _, a := range as.Data {
		if a.UsuarioID == tec && (len(a.Ocupado) != 1 || a.Ocupado[0].OrdenID != o.ID) {
			t.Fatalf("propia: %+v", a.Ocupado)
		}
	}
}

// Punto 6: una cuadrilla sin zonas no la cubre el alcance por zona (falla cerrado), y una
// cuadrilla de otra empresa no la cubre el alcance por cuadrilla.
func TestRevisionAlcanceCuadrillaSinZonas(t *testing.T) {
	e := nuevoEntorno()
	cs := como(t, usuarioCon(t, rol("supervisor")), "supervisor")
	z := deber(e.ag.CrearZona.Ejecutar(cs, agenda.EntradaCrearZona{Nombre: "Zona alcance " + pruebas.Unico()}))(t)
	sinZonas := deber(e.ag.CrearCuadrilla.Ejecutar(cs, agenda.EntradaCrearCuadrilla{Nombre: "Sin zonas " + pruebas.Unico()}))(t)
	conZona := deber(e.ag.CrearCuadrilla.Ejecutar(cs, agenda.EntradaCrearCuadrilla{Nombre: "Con zona " + pruebas.Unico(), ZonaIDs: []int64{z.ID}}))(t)
	t1, t2 := usuarioCon(t, rol("tecnico")), usuarioCon(t, rol("tecnico"))
	f1 := fichaCon(t, e, cs, t1, &sinZonas.ID)
	f2 := fichaCon(t, e, cs, t2, &conZona.ID)
	desp := usuarioCon(t, rol("despacho"))
	porZona := conAlcance(t, desp, map[string][]contexto.Alcance{"agenda.ver": {{ZonaID: z.ID}}, "agenda.fijar_turnos": {{ZonaID: z.ID}}})
	_, err := e.ag.VerTecnico.Ejecutar(porZona, agenda.EntradaVerTecnico{ID: f1.ID})
	problema(t, err, 404, "no_encontrado")
	deber(e.ag.VerTecnico.Ejecutar(porZona, agenda.EntradaVerTecnico{ID: f2.ID}))(t)
	lista := deber(e.ag.ListarTecnicos.Ejecutar(porZona, agenda.EntradaListarTecnicos{}))(t)
	for _, tc := range lista.Data {
		if tc.ID == f1.ID {
			t.Fatal("la ficha de una cuadrilla sin zonas no aparece con alcance por zona")
		}
	}
	if len(lista.Data) == 0 {
		t.Fatal("la de su zona sí")
	}

	// Cuadrilla de otra empresa en el alcance: no cubre.
	registrarEmpresas(t, 41, 42)
	adm := usuarioCon(t, rol("admin"))
	c42 := deber(e.ag.CrearCuadrilla.Ejecutar(enEmpresa(t, adm, 42, "admin"), agenda.EntradaCrearCuadrilla{Nombre: "C42 " + pruebas.Unico()}))(t)
	t3 := usuarioCon(t, rol("tecnico"))
	asignarEmpresa(t, t3, 41, 42)
	f3 := fichaCon(t, e, enEmpresa(t, adm, 42, "admin"), t3, &c42.ID)
	asignarEmpresa(t, desp, 41)
	ctx41 := contexto.ConActor(enEmpresa(t, desp, 41, "despacho"), contexto.ActorDe(enEmpresa(t, desp, 41, "despacho")),
		contexto.NuevosPermisos(map[string][]contexto.Alcance{"agenda.ver": {{CuadrillaID: c42.ID}}}))
	_, err = e.ag.VerTecnico.Ejecutar(ctx41, agenda.EntradaVerTecnico{ID: f3.ID})
	problema(t, err, 404, "no_encontrado")
	if l := deber(e.ag.ListarTecnicos.Ejecutar(ctx41, agenda.EntradaListarTecnicos{}))(t); len(l.Data) != 0 {
		t.Fatalf("cuadrilla de otra empresa: %+v", l.Data)
	}
}
