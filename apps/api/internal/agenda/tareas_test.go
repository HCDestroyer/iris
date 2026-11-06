package agenda_test

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/HCDestroyer/iris/apps/api/internal/agenda"
	"github.com/HCDestroyer/iris/apps/api/internal/campo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

func paginaDe(limite int, cursor string) nucleo.ParamsPagina {
	return nucleo.ParamsPagina{Limit: limite, Cursor: cursor}
}

func nuevoID() uuid.UUID { return uuid.Must(uuid.NewV7()) }

func datosTarea(que string, inicio time.Time, minutos int32) agenda.DatosTarea {
	return agenda.DatosTarea{Que: que, Inicio: inicio, DuracionMin: minutos}
}

func eventosDeTarea(t *testing.T, id uuid.UUID) []string {
	t.Helper()
	filas, err := pruebas.Pool(t).Query(context.Background(),
		"SELECT evento FROM integracion.outbox WHERE payload->>'tarea_id' = $1 ORDER BY id", id.String())
	if err != nil {
		t.Fatal(err)
	}
	defer filas.Close()
	salida := []string{}
	for filas.Next() {
		var e string
		if err := filas.Scan(&e); err != nil {
			t.Fatal(err)
		}
		salida = append(salida, e)
	}
	return salida
}

func TestTareaPropiaCrearCompletarAnular(t *testing.T) {
	e := nuevoEntorno()
	sup := usuarioCon(t, rol("supervisor"))
	cs := como(t, sup, "supervisor")
	cu := deber(e.ag.CrearCuadrilla.Ejecutar(cs, agenda.EntradaCrearCuadrilla{Nombre: "Tareas " + pruebas.Unico()}))(t)
	tec := usuarioCon(t, rol("tecnico"))
	fichaCon(t, e, cs, tec, &cu.ID)
	ct := como(t, tec, "tecnico")

	// Una cita del técnico a las 10:00: la tarea a las 10:30 se cruza.
	z := deber(e.ag.CrearZona.Ejecutar(cs, agenda.EntradaCrearZona{Nombre: "Zona tareas " + pruebas.Unico()}))(t)
	desp := usuarioCon(t, rol("despacho"))
	cd := como(t, desp, "despacho")
	o := orden(t, e, cd, "visita", z.ID)
	o = deber(programar(e, cd, o, tec, gt(2, 10, 0)))(t)

	id := nuevoID()
	r := deber(e.ag.CrearTarea.Ejecutar(ct, agenda.EntradaCrearTarea{ID: id, DatosTarea: datosTarea("  Recoger material en bodega central ", gt(2, 10, 30), 30)}))(t)
	if r.Tarea.Que != "Recoger material en bodega central" || r.Tarea.TecnicoID != tec || r.Tarea.Estado != agenda.TareaPendiente ||
		!r.Tarea.Fin.Equal(gt(2, 11, 0)) || r.Tarea.Version != 1 || r.Tarea.Origen != "panel" || r.Duplicada {
		t.Fatalf("tarea: %+v", r.Tarea)
	}
	if len(r.SolapaCon) != 1 || r.SolapaCon[0].OrdenID != o.ID {
		t.Fatalf("solapa con la cita: %+v", r.SolapaCon)
	}
	// Reintento con el mismo id: la misma tarea, sin otro evento.
	r2 := deber(e.ag.CrearTarea.Ejecutar(ct, agenda.EntradaCrearTarea{ID: id, DatosTarea: datosTarea("Otra cosa", gt(2, 15, 0), 15)}))(t)
	if !r2.Duplicada || r2.Tarea.Que != r.Tarea.Que {
		t.Fatalf("reintento: %+v", r2)
	}
	// El mismo id usado por otro: 409.
	otro := usuarioCon(t, rol("tecnico"))
	_, err := e.ag.CrearTarea.Ejecutar(como(t, otro, "tecnico"), agenda.EntradaCrearTarea{ID: id, DatosTarea: datosTarea("x", gt(2, 15, 0), 15)})
	problema(t, err, 409, "tarea_ajena")

	// Validación espejo del formulario (qué, duración, inicio).
	for _, mal := range []agenda.DatosTarea{
		datosTarea("   ", gt(2, 15, 0), 30),
		datosTarea(strings.Repeat("a", 201), gt(2, 15, 0), 30),
		datosTarea("x", gt(2, 15, 0), 4),
		datosTarea("x", gt(2, 15, 0), 481),
		datosTarea("x", gt(2, 9, 0).AddDate(0, 0, -8), 30),
	} {
		_, err := e.ag.CrearTarea.Ejecutar(ct, agenda.EntradaCrearTarea{ID: nuevoID(), DatosTarea: mal})
		problema(t, err, 422, "validacion")
	}
	// Ligada a su orden (la ve) sí; a una que no ve, 422 en ordenId (sin revelar si existe).
	ligada := datosTarea("Llevar ONU de repuesto", gt(2, 14, 0), 15)
	ligada.OrdenID = &o.ID
	rl := deber(e.ag.CrearTarea.Ejecutar(ct, agenda.EntradaCrearTarea{ID: nuevoID(), DatosTarea: ligada}))(t)
	if rl.Tarea.OrdenID == nil || *rl.Tarea.OrdenID != o.ID || len(rl.SolapaCon) != 0 {
		t.Fatalf("ligada a la orden: %+v", rl)
	}
	ajena := orden(t, e, cd, "visita", z.ID)
	ligada.OrdenID = &ajena.ID
	_, err = e.ag.CrearTarea.Ejecutar(ct, agenda.EntradaCrearTarea{ID: nuevoID(), DatosTarea: ligada})
	if p := problema(t, err, 422, "validacion"); p.Errors[0].Field != "ordenId" {
		t.Fatalf("orden ajena: %+v", p.Errors)
	}
	// Sin conector no hay clientes que ligar.
	conCliente := datosTarea("Revisar la caja", gt(2, 15, 0), 30)
	conCliente.ClienteID = ptr(int64(123))
	_, err = e.ag.CrearTarea.Ejecutar(ct, agenda.EntradaCrearTarea{ID: nuevoID(), DatosTarea: conCliente})
	problema(t, err, 422, "validacion")

	// Otro técnico no la ve ni la completa (no tiene agenda.ver).
	_, err = e.ag.CompletarTarea.Ejecutar(como(t, otro, "tecnico"), agenda.EntradaCompletarTarea{ID: id, Version: 1})
	problema(t, err, 404, "no_encontrado")
	// El dueño la completa; versión vieja → 409; otra vez → aviso sin cambios; anular una hecha → 409.
	c := deber(e.ag.CompletarTarea.Ejecutar(ct, agenda.EntradaCompletarTarea{ID: id, Version: 1}))(t)
	if c.Tarea.Estado != agenda.TareaCompletada || c.Tarea.CompletadaEn == nil || c.Tarea.Version != 2 {
		t.Fatalf("completada: %+v", c.Tarea)
	}
	_, err = e.ag.CompletarTarea.Ejecutar(ct, agenda.EntradaCompletarTarea{ID: id, Version: 1})
	problema(t, err, 409, "conflicto_version")
	ya := deber(e.ag.CompletarTarea.Ejecutar(ct, agenda.EntradaCompletarTarea{ID: id, Version: 2}))(t)
	if ya.Aviso == nil || *ya.Aviso != agenda.AvisoTareaYaCompletada || ya.Tarea.Version != 2 {
		t.Fatalf("ya completada: %+v", ya)
	}
	_, err = e.ag.AnularTarea.Ejecutar(ct, agenda.EntradaAnularTarea{ID: id, Version: 2})
	problema(t, err, 409, "tarea_completada")

	// El supervisor anula la ligada (supervisa su cuadrilla); despacho con alcance de otra cuadrilla no la ve.
	otraCu := deber(e.ag.CrearCuadrilla.Ejecutar(cs, agenda.EntradaCrearCuadrilla{Nombre: "Otra " + pruebas.Unico()}))(t)
	limitado := conAlcance(t, desp, map[string][]contexto.Alcance{"agenda.ver": {{CuadrillaID: otraCu.ID}}, "agenda.anular_tarea": {{CuadrillaID: otraCu.ID}}})
	_, err = e.ag.AnularTarea.Ejecutar(limitado, agenda.EntradaAnularTarea{ID: rl.Tarea.ID, Version: 1})
	problema(t, err, 404, "no_encontrado")
	a := deber(e.ag.AnularTarea.Ejecutar(cs, agenda.EntradaAnularTarea{ID: rl.Tarea.ID, Version: 1, Motivo: ptr("Ya no hace falta")}))(t)
	if a.Tarea.Estado != agenda.TareaAnulada || a.Tarea.Motivo == nil || a.Tarea.AnuladaEn == nil {
		t.Fatalf("anulada: %+v", a.Tarea)
	}
	_, err = e.ag.CompletarTarea.Ejecutar(ct, agenda.EntradaCompletarTarea{ID: rl.Tarea.ID, Version: a.Tarea.Version})
	problema(t, err, 409, "tarea_anulada")

	// Eventos por outbox y auditoría en la misma transacción.
	if ev := eventosDeTarea(t, id); !slices.Equal(ev, []string{"tarea.creada", "tarea.completada"}) {
		t.Fatalf("eventos de la tarea: %v", ev)
	}
	if ev := eventosDeTarea(t, rl.Tarea.ID); !slices.Equal(ev, []string{"tarea.creada", "tarea.anulada"}) {
		t.Fatalf("eventos de la ligada: %v", ev)
	}
	if n := contar(t, "SELECT count(*) FROM auditoria.acciones WHERE accion = 'agenda.crear_tarea' AND resultado = 'ok' AND entidad_id = $1", id.String()); n != 2 {
		t.Fatalf("auditoría de crear (con el reintento): %d", n)
	}
	if n := contar(t, "SELECT count(*) FROM auditoria.cambios c JOIN auditoria.acciones a ON a.id = c.accion_id WHERE a.accion = 'agenda.completar_tarea' AND c.entidad_id = $1 AND c.campo = 'estado'", id.String()); n != 1 {
		t.Fatalf("cambios de completar: %d", n)
	}

	// El supervisor crea una para el técnico; un técnico no crea para otro.
	paraTec := tec
	rs := deber(e.ag.CrearTarea.Ejecutar(cs, agenda.EntradaCrearTarea{ID: nuevoID(), TecnicoID: &paraTec, DatosTarea: datosTarea("Pasar por la oficina", gt(3, 8, 0), 60)}))(t)
	if rs.Tarea.TecnicoID != tec || rs.Tarea.CreadaPor == nil || *rs.Tarea.CreadaPor != sup {
		t.Fatalf("creada por el supervisor: %+v", rs.Tarea)
	}
	_, err = e.ag.CrearTarea.Ejecutar(como(t, otro, "tecnico"), agenda.EntradaCrearTarea{ID: nuevoID(), TecnicoID: &paraTec, DatosTarea: datosTarea("x", gt(3, 8, 0), 60)})
	problema(t, err, 422, "validacion")

	// Lista de agenda (supervisor): por inicio, sin las anuladas salvo que se pidan.
	lista := deber(e.ag.ListarTareas.Ejecutar(cs, agenda.EntradaListarTareas{Desde: gt(2, 0, 0).Format(time.RFC3339), Hasta: gt(4, 0, 0).Format(time.RFC3339), TecnicoID: tec}))(t)
	if len(lista.Data) != 2 || lista.Data[0].ID != id || lista.Data[1].ID != rs.Tarea.ID || lista.Data[0].TecnicoNombre == "" {
		t.Fatalf("lista: %+v", lista.Data)
	}
	todas := deber(e.ag.ListarTareas.Ejecutar(cs, agenda.EntradaListarTareas{Desde: gt(2, 0, 0).Format(time.RFC3339), Hasta: gt(4, 0, 0).Format(time.RFC3339),
		TecnicoID: tec, IncluirAnuladas: true}))(t)
	if len(todas.Data) != 3 {
		t.Fatalf("con anuladas: %d", len(todas.Data))
	}
	pagina := deber(e.ag.ListarTareas.Ejecutar(cs, agenda.EntradaListarTareas{Desde: gt(2, 0, 0).Format(time.RFC3339), Hasta: gt(4, 0, 0).Format(time.RFC3339),
		TecnicoID: tec, IncluirAnuladas: true, ParamsPagina: paginaDe(2, "")}))(t)
	if len(pagina.Data) != 2 || pagina.NextCursor == nil {
		t.Fatalf("página 1: %+v", pagina)
	}
	resto := deber(e.ag.ListarTareas.Ejecutar(cs, agenda.EntradaListarTareas{Desde: gt(2, 0, 0).Format(time.RFC3339), Hasta: gt(4, 0, 0).Format(time.RFC3339),
		TecnicoID: tec, IncluirAnuladas: true, ParamsPagina: paginaDe(2, *pagina.NextCursor)}))(t)
	if len(resto.Data) != 1 || resto.NextCursor != nil || resto.Data[0].ID != rs.Tarea.ID {
		t.Fatalf("página 2: %+v", resto)
	}
	// Con alcance de otra cuadrilla no ve las de este técnico.
	if l := deber(e.ag.ListarTareas.Ejecutar(limitado, agenda.EntradaListarTareas{Desde: gt(2, 0, 0).Format(time.RFC3339), Hasta: gt(4, 0, 0).Format(time.RFC3339)}))(t); len(l.Data) != 0 {
		t.Fatalf("fuera de alcance: %+v", l.Data)
	}
	// El técnico no tiene agenda.ver: la lista de despacho no es para él.
	_, err = e.ag.ListarTareas.Ejecutar(ct, agenda.EntradaListarTareas{Desde: gt(2, 0, 0).Format(time.RFC3339), Hasta: gt(4, 0, 0).Format(time.RFC3339)})
	problema(t, err, 403, "prohibido")
}

func TestTareaPropiaSinConexion(t *testing.T) {
	e := nuevoEntorno()
	sup := usuarioCon(t, rol("supervisor"))
	cs := como(t, sup, "supervisor")
	tec := usuarioCon(t, rol("tecnico"))
	fichaCon(t, e, cs, tec, nil)
	ct := contexto.ConCanal(como(t, tec, "tecnico"), contexto.CanalCampo)
	z := deber(e.ag.CrearZona.Ejecutar(cs, agenda.EntradaCrearZona{Nombre: "Zona lote " + pruebas.Unico()}))(t)
	desp := usuarioCon(t, rol("despacho"))
	o := orden(t, e, como(t, desp, "despacho"), "visita", z.ID)
	deber(programar(e, como(t, desp, "despacho"), o, tec, gt(2, 14, 0)))(t)

	dispositivo := "tel-" + pruebas.Unico()
	lote := func(ops ...campo.OperacionLote) campo.RespuestaLote {
		t.Helper()
		r, err := e.cmp.Lote(ct, campo.EntradaLote{DispositivoID: dispositivo, Plataforma: "android", Operaciones: ops})
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	crear := campo.OperacionLote{ID: nuevoID().String(), Tipo: campo.OpTareaCrear, Datos: map[string]any{
		"que": "Recoger material en bodega central", "inicio": gt(2, 14, 30).Format(time.RFC3339), "duracionMin": 30}}
	completar := campo.OperacionLote{ID: nuevoID().String(), Tipo: campo.OpTareaCompletar, OcurridoEn: ptr(gt(2, 9, 0)),
		Datos: map[string]any{"tareaId": crear.ID}}
	r := lote(crear, completar)
	if r.Resultados[0].Estado != "aplicada" || r.Resultados[0].Tarea == nil || r.Resultados[0].Aviso == nil ||
		*r.Resultados[0].Aviso != campo.AvisoSolapaCon || r.Resultados[0].Tarea.Tarea.ID.String() != crear.ID ||
		r.Resultados[0].Tarea.Tarea.Origen != "campo" {
		t.Fatalf("tarea.crear: %+v", r.Resultados[0])
	}
	if r.Resultados[1].Estado != "aplicada" || r.Resultados[1].Tarea.Tarea.Estado != agenda.TareaCompletada {
		t.Fatalf("tarea.completar: %+v", r.Resultados[1])
	}
	// Reenviar el mismo lote (sin respuesta la primera vez): duplicadas, sin tocar nada.
	r = lote(crear, completar)
	if r.Resultados[0].Estado != "duplicada" || r.Resultados[1].Estado != "duplicada" || r.Resultados[0].Tarea == nil {
		t.Fatalf("idempotencia: %+v", r.Resultados)
	}
	if n := contar(t, "SELECT count(*) FROM integracion.outbox WHERE evento = 'tarea.creada' AND payload->>'tarea_id' = $1", crear.ID); n != 1 {
		t.Fatalf("un solo tarea.creada: %d", n)
	}
	// Un rechazo bloquea lo que sigue de esa tarea, no lo de otra.
	mala := campo.OperacionLote{ID: nuevoID().String(), Tipo: campo.OpTareaCrear, Datos: map[string]any{
		"que": "", "inicio": gt(2, 16, 0).Format(time.RFC3339), "duracionMin": 30}}
	trasMala := campo.OperacionLote{ID: nuevoID().String(), Tipo: campo.OpTareaAnular, Datos: map[string]any{"tareaId": mala.ID}}
	buena := campo.OperacionLote{ID: nuevoID().String(), Tipo: campo.OpTareaCrear, Datos: map[string]any{
		"que": "Revisar caja NAP 12", "inicio": gt(2, 17, 0).Format(time.RFC3339), "duracionMin": 15}}
	anularBuena := campo.OperacionLote{ID: nuevoID().String(), Tipo: campo.OpTareaAnular, Datos: map[string]any{"tareaId": buena.ID, "motivo": "Lo hizo otro"}}
	r = lote(mala, trasMala, buena, anularBuena)
	got := []string{}
	for _, x := range r.Resultados {
		s := x.Estado
		if x.Error != nil {
			s += ":" + x.Error.Codigo()
		}
		got = append(got, s)
	}
	if !slices.Equal(got, []string{"rechazada:validacion", "rechazada:bloqueada_por_rechazo", "aplicada", "aplicada"}) {
		t.Fatalf("bloqueo por tarea: %v", got)
	}
	// Completar sin señal una que el supervisor anuló entretanto: rechazada tarea_anulada.
	completarAnulada := campo.OperacionLote{ID: nuevoID().String(), Tipo: campo.OpTareaCompletar, Datos: map[string]any{"tareaId": buena.ID}}
	r = lote(completarAnulada)
	if r.Resultados[0].Estado != "rechazada" || r.Resultados[0].Error.Codigo() != "tarea_anulada" {
		t.Fatalf("completar una anulada: %+v", r.Resultados[0])
	}
	// Una tarea de otro técnico: 404 (no se revela).
	otro := usuarioCon(t, rol("tecnico"))
	r2, err := e.cmp.Lote(contexto.ConCanal(como(t, otro, "tecnico"), contexto.CanalCampo), campo.EntradaLote{DispositivoID: "tel-" + pruebas.Unico(),
		Operaciones: []campo.OperacionLote{{ID: nuevoID().String(), Tipo: campo.OpTareaAnular, Datos: map[string]any{"tareaId": crear.ID}}}})
	if err != nil || r2.Resultados[0].Error == nil || r2.Resultados[0].Error.Codigo() != "no_encontrado" {
		t.Fatalf("tarea ajena: %+v %v", r2.Resultados, err)
	}

	// El paquete del día trae sus tareas vivas (no la anulada) y los cambios también.
	p := deber(e.cmp.Paquete(ct, campo.EntradaPaquete{}))(t)
	ids := []string{}
	for _, x := range p.Tareas {
		ids = append(ids, x.ID.String())
	}
	if !slices.Equal(ids, []string{crear.ID}) || p.TomarLibres {
		t.Fatalf("tareas del paquete: %v (tomarLibres %v)", ids, p.TomarLibres)
	}
	c := deber(e.cmp.Cambios(ct, campo.EntradaCambios{Cursor: p.Cursor}))(t)
	if len(c.Tareas) != 1 || c.Tareas[0].ID.String() != crear.ID {
		t.Fatalf("tareas en cambios: %+v", c.Tareas)
	}
}

// Revisión de seguridad: el mismo id de tarea reutilizado desde otra empresa (mismo autor y
// técnico) no devuelve la de la otra empresa: 409 tarea_ajena.
func TestTareaPropiaIDDeOtraEmpresa(t *testing.T) {
	registrarEmpresas(t, 47, 48)
	e := nuevoEntorno()
	tec := usuarioCon(t, rol("tecnico"))
	asignarEmpresa(t, tec, 47, 48)
	id := nuevoID()
	en47 := contexto.ConCanal(enEmpresa(t, tec, 47, "tecnico"), contexto.CanalCampo)
	r := deber(e.ag.CrearTarea.Ejecutar(en47, agenda.EntradaCrearTarea{ID: id, DatosTarea: datosTarea("Revisar caja", gt(2, 9, 0), 30)}))(t)
	if r.Tarea.EmpresaID == nil || *r.Tarea.EmpresaID != 47 {
		t.Fatalf("tarea en 47: %+v", r.Tarea)
	}
	en48 := contexto.ConCanal(enEmpresa(t, tec, 48, "tecnico"), contexto.CanalCampo)
	got, err := e.ag.CrearTarea.Ejecutar(en48, agenda.EntradaCrearTarea{ID: id, DatosTarea: datosTarea("Otra", gt(2, 9, 0), 30)})
	problema(t, err, 409, "tarea_ajena")
	if got.Tarea.Que != "" {
		t.Fatalf("devolvió la tarea de la otra empresa: %+v", got.Tarea)
	}
}
