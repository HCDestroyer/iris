package campo_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/HCDestroyer/iris/apps/api/internal/campo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// itemsPrueba: una plantilla con un sí/no obligatorio, una selección obligatoria, una foto
// obligatoria y observaciones (mismo código que en las plantillas de la semilla).
func itemsPrueba() []campo.EntradaItemPlantilla {
	return []campo.EntradaItemPlantilla{
		{Codigo: "poste_ok", Etiqueta: "Poste en buen estado", Tipo: campo.ItemSiNo, Obligatorio: true},
		{Codigo: "estado_caja", Etiqueta: "Estado de la caja NAP", Tipo: campo.ItemSeleccion, Obligatorio: true,
			Opciones: []string{"Buena", "Dañada", "Sin tapa"}},
		{Codigo: "foto_caja", Etiqueta: "Foto de la caja NAP", Tipo: campo.ItemFoto, Obligatorio: true},
		{Codigo: "observaciones", Etiqueta: "Observaciones", Tipo: campo.ItemTexto},
	}
}

func crearPlantilla(t *testing.T, e entorno, ctx context.Context, firma bool) campo.PlantillaFormulario {
	t.Helper()
	return deber(e.m.CrearPlantilla.Ejecutar(ctx, campo.EntradaCrearPlantilla{
		Codigo: "revision_nap_" + strings.ReplaceAll(pruebas.Unico(), "-", "_"), Nombre: "Revisión de caja NAP",
		RequiereFirma: firma, Items: itemsPrueba(),
	}))(t)
}

func supervisor(t *testing.T) context.Context {
	return como(t, usuarioCon(t, rolDe("supervisor")), "supervisor")
}

func detalle(t *testing.T, e entorno, id int64) campo.OrdenDetalle {
	t.Helper()
	return deber(e.m.VerOrden.Ejecutar(pruebas.ContextoSistema(pruebas.Pool(t)), campo.EntradaVerOrden{ID: id}))(t)
}

// formularioDe: el formulario de la orden con esa plantilla.
func formularioDe(t *testing.T, d campo.OrdenDetalle, plantilla int64) campo.FormularioOrden {
	t.Helper()
	for _, f := range d.Formularios {
		if f.PlantillaID == plantilla {
			return f
		}
	}
	t.Fatalf("la orden %d no tiene la plantilla %d: %+v", d.ID, plantilla, d.Formularios)
	return campo.FormularioOrden{}
}

func itemDe(t *testing.T, d campo.OrdenDetalle, formulario uuid.UUID, codigo string) campo.ItemChecklist {
	t.Helper()
	for _, it := range d.Checklist {
		if it.FormularioID == formulario && it.Codigo == codigo {
			return it
		}
	}
	t.Fatalf("falta %s en el formulario %s", codigo, formulario)
	return campo.ItemChecklist{}
}

func eventosPlantilla(t *testing.T, id int64) []string {
	t.Helper()
	filas, err := pruebas.Pool(t).Query(context.Background(),
		"SELECT evento FROM integracion.outbox WHERE (payload->>'plantilla_id')::bigint = $1 AND evento LIKE 'plantilla.%' ORDER BY id", id)
	if err != nil {
		t.Fatal(err)
	}
	defer filas.Close()
	salida := []string{}
	for filas.Next() {
		var s string
		if err := filas.Scan(&s); err != nil {
			t.Fatal(err)
		}
		salida = append(salida, s)
	}
	return salida
}

// CRUD con acciones explícitas y versiones: editar sin uso reescribe la versión; con uso crea
// otra y las órdenes conservan la suya (y lo respondido).
func TestPlantillasCrudYVersiones(t *testing.T) {
	e := nuevoEntorno()
	cs := supervisor(t)
	cd := como(t, despacho(t), "despacho")
	ct := como(t, tecnico(t), "tecnico")

	// Validaciones.
	malos := itemsPrueba()
	malos[1].Opciones = nil
	_, err := e.m.CrearPlantilla.Ejecutar(cs, campo.EntradaCrearPlantilla{Codigo: "mala_seleccion", Nombre: "X", Items: malos})
	if c := problema(t, err, 422, "validacion"); c[0].Field != "items[1].opciones" {
		t.Fatalf("selección sin opciones: %+v", c)
	}
	malos = itemsPrueba()
	malos[3].Codigo = "poste_ok"
	_, err = e.m.CrearPlantilla.Ejecutar(cs, campo.EntradaCrearPlantilla{Codigo: "mala_repetida", Nombre: "X", Items: malos})
	problema(t, err, 422, "validacion")
	_, err = e.m.CrearPlantilla.Ejecutar(cd, campo.EntradaCrearPlantilla{Codigo: "de_despacho", Nombre: "X", Items: itemsPrueba()})
	problema(t, err, 403, "prohibido")

	p := crearPlantilla(t, e, cs, false)
	if p.NumeroVersion != 1 || p.VersionActual != 1 || len(p.Items) != 4 || p.Items[1].Opciones[2] != "Sin tapa" || p.EmpresaID != nil || !p.Activa {
		t.Fatalf("creada: %+v", p)
	}
	_, err = e.m.CrearPlantilla.Ejecutar(cs, campo.EntradaCrearPlantilla{Codigo: p.Codigo, Nombre: "Otra", Items: itemsPrueba()})
	problema(t, err, 422, "codigo_duplicado")

	// Despacho y técnico la ven (plantillas.ver); el técnico no edita.
	for _, ctx := range []context.Context{cd, ct} {
		lista := deber(e.m.ListarPlantillas.Ejecutar(ctx, campo.EntradaListarPlantillas{}))(t)
		if !slices.ContainsFunc(lista.Data, func(x campo.PlantillaFormulario) bool { return x.ID == p.ID }) {
			t.Fatalf("la lista no trae la plantilla nueva")
		}
	}
	_, err = e.m.EditarPlantilla.Ejecutar(ct, campo.EntradaEditarPlantilla{ID: p.ID, Version: p.Version, Nombre: "X", Items: itemsPrueba()})
	problema(t, err, 403, "prohibido")

	// Un borrador (nunca activado) se reescribe en el lugar; activado una vez, toda edición crea
	// una versión (la actual ya viajó en el catálogo de los teléfonos).
	b := deber(e.m.CrearPlantilla.Ejecutar(cs, campo.EntradaCrearPlantilla{Codigo: "borrador_" + strings.ReplaceAll(pruebas.Unico(), "-", "_"),
		Nombre: "Borrador", Borrador: true, Items: itemsPrueba()}))(t)
	if b.Activa || !b.Borrador {
		t.Fatalf("borrador: %+v", b)
	}
	items := itemsPrueba()
	items[0].Etiqueta = "Poste firme y aterrizado"
	b = deber(e.m.EditarPlantilla.Ejecutar(cs, campo.EntradaEditarPlantilla{ID: b.ID, Version: b.Version, Nombre: "Borrador 2", Items: items}))(t)
	if b.NumeroVersion != 1 || b.Nombre != "Borrador 2" || b.Items[0].Etiqueta != "Poste firme y aterrizado" {
		t.Fatalf("borrador editado en el lugar: %+v", b)
	}
	b = deber(e.m.ActivarPlantilla.Ejecutar(cs, campo.EntradaVersionPlantilla{ID: b.ID, Version: b.Version}))(t)
	b = deber(e.m.DesactivarPlantilla.Ejecutar(cs, campo.EntradaVersionPlantilla{ID: b.ID, Version: b.Version}))(t)
	b = deber(e.m.EditarPlantilla.Ejecutar(cs, campo.EntradaEditarPlantilla{ID: b.ID, Version: b.Version, Nombre: "Borrador 3", Items: items}))(t)
	if b.NumeroVersion != 2 || b.Borrador {
		t.Fatalf("activada alguna vez (aunque hoy esté inactiva y sin uso): versión nueva: %+v", b)
	}
	_, err = e.m.EditarPlantilla.Ejecutar(cs, campo.EntradaEditarPlantilla{ID: p.ID, Version: p.Version + 1, Nombre: "X", Items: items})
	problema(t, err, 409, "conflicto_version")

	// Con uso: una orden la tiene (versión 1) y respondió un ítem; editar crea la versión 2.
	o1 := crearOrden(t, e, cd, "visita")
	deber(e.m.AsignarFormulario.Ejecutar(cd, campo.EntradaAgregarFormulario{ID: o1.ID, Version: o1.Version, PlantillaID: p.ID}))(t)
	d1 := detalle(t, e, o1.ID)
	f1 := formularioDe(t, d1, p.ID)
	tec := tecnico(t)
	deber(e.m.ProgramarOrden.Ejecutar(cd, campo.EntradaProgramarOrden{ID: o1.ID, Version: d1.Version, TecnicoID: &tec, Inicio: inicio}))(t)
	it := itemDe(t, d1, f1.ID, "estado_caja")
	ctec := como(t, tec, "tecnico")
	_, err = e.m.MarcarChecklist.Ejecutar(ctec, campo.EntradaMarcarChecklist{ID: o1.ID, ItemID: &it.ItemID, Valor: "Rota"})
	problema(t, err, 422, "validacion") // no es una de las opciones
	deber(e.m.MarcarChecklist.Ejecutar(ctec, campo.EntradaMarcarChecklist{ID: o1.ID, ItemID: &it.ItemID, Valor: "Dañada"}))(t)

	items[1].Opciones = []string{"Buena", "Mala"}
	items = append(items, campo.EntradaItemPlantilla{Codigo: "precinto", Etiqueta: "Precinto colocado", Tipo: campo.ItemSiNo})
	p2 := deber(e.m.EditarPlantilla.Ejecutar(cs, campo.EntradaEditarPlantilla{ID: p.ID, Version: p.Version, Nombre: "Revisión NAP v2",
		RequiereFirma: true, Items: items}))(t)
	if p2.NumeroVersion != 2 || p2.VersionActual != 2 || len(p2.Items) != 5 || !p2.RequiereFirma || p2.VersionID == f1.VersionID {
		t.Fatalf("versión nueva: %+v", p2)
	}
	// La orden conserva la versión 1, con su respuesta y sus opciones.
	d1 = detalle(t, e, o1.ID)
	f1b := formularioDe(t, d1, p.ID)
	it = itemDe(t, d1, f1.ID, "estado_caja")
	if f1b.NumeroVersion != 1 || f1b.Nombre != "Revisión de caja NAP" || f1b.RequiereFirma || it.Valor != "Dañada" ||
		len(it.Opciones) != 3 || !f1b.ConRespuestas {
		t.Fatalf("la orden conserva su versión: %+v %+v", f1b, it)
	}
	// Una orden nueva toma la versión 2.
	o2 := crearOrden(t, e, cd, "visita")
	deber(e.m.AsignarFormulario.Ejecutar(cd, campo.EntradaAgregarFormulario{ID: o2.ID, Version: o2.Version, PlantillaID: p.ID}))(t)
	if f := formularioDe(t, detalle(t, e, o2.ID), p.ID); f.NumeroVersion != 2 || !f.RequiereFirma || f.Nombre != "Revisión NAP v2" {
		t.Fatalf("orden nueva: %+v", f)
	}
	// Ver una versión anterior y el historial.
	v1 := deber(e.m.VerPlantilla.Ejecutar(cd, campo.EntradaVerPlantilla{ID: p.ID, NumeroVersion: 1}))(t)
	if v1.NumeroVersion != 1 || v1.VersionActual != 2 || len(v1.Items) != 4 || len(v1.Versiones) != 2 ||
		v1.Versiones[0].Numero != 2 || v1.Versiones[1].Formularios != 1 {
		t.Fatalf("ver versión 1: %+v", v1)
	}
	_, err = e.m.VerPlantilla.Ejecutar(cd, campo.EntradaVerPlantilla{ID: p.ID, NumeroVersion: 9})
	problema(t, err, 404, "no_encontrado")

	// Desactivar: no se ofrece más (agregar → 422), los formularios ya agregados siguen.
	p2 = deber(e.m.DesactivarPlantilla.Ejecutar(cs, campo.EntradaVersionPlantilla{ID: p.ID, Version: p2.Version}))(t)
	_, err = e.m.DesactivarPlantilla.Ejecutar(cs, campo.EntradaVersionPlantilla{ID: p.ID, Version: p2.Version})
	problema(t, err, 422, "sin_cambios")
	o3 := crearOrden(t, e, cd, "visita")
	_, err = e.m.AsignarFormulario.Ejecutar(cd, campo.EntradaAgregarFormulario{ID: o3.ID, Version: o3.Version, PlantillaID: p.ID})
	problema(t, err, 422, "plantilla_inactiva")
	if lista := deber(e.m.ListarPlantillas.Ejecutar(cd, campo.EntradaListarPlantillas{}))(t); slices.ContainsFunc(lista.Data,
		func(x campo.PlantillaFormulario) bool { return x.ID == p.ID }) {
		t.Fatal("una desactivada no sale en la lista por defecto")
	}
	if lista := deber(e.m.ListarPlantillas.Ejecutar(cd, campo.EntradaListarPlantillas{Todas: true}))(t); !slices.ContainsFunc(lista.Data,
		func(x campo.PlantillaFormulario) bool { return x.ID == p.ID }) {
		t.Fatal("con todas sí")
	}
	formularioDe(t, detalle(t, e, o2.ID), p.ID)
	p2 = deber(e.m.ActivarPlantilla.Ejecutar(cs, campo.EntradaVersionPlantilla{ID: p.ID, Version: p2.Version}))(t)
	if !p2.Activa {
		t.Fatal("activada")
	}

	// La plantilla por defecto de un tipo no se desactiva.
	tipos := deber(e.m.ListarTipos.Ejecutar(cd, struct{}{}))(t).Data
	visita := tipos[slices.IndexFunc(tipos, func(x campo.TipoOrden) bool { return x.Codigo == "visita" })]
	def := deber(e.m.VerPlantilla.Ejecutar(cs, campo.EntradaVerPlantilla{ID: *visita.PlantillaID}))(t)
	if !def.PorDefecto || def.EmpresaID != nil {
		t.Fatalf("por defecto: %+v", def.PlantillaFormulario)
	}
	_, err = e.m.DesactivarPlantilla.Ejecutar(cs, campo.EntradaVersionPlantilla{ID: def.ID, Version: def.Version})
	problema(t, err, 422, "plantilla_por_defecto")

	if ev := eventosPlantilla(t, p.ID); !slices.Equal(ev, []string{"plantilla.creada", "plantilla.actualizada",
		"plantilla.desactivada", "plantilla.activada"}) {
		t.Fatalf("eventos: %v", ev)
	}
	if n := contar(t, "SELECT count(*) FROM auditoria.acciones WHERE accion = 'plantillas.editar' AND entidad_id = $1::bigint::text AND resultado = 'ok'", p.ID); n != 1 {
		t.Fatalf("auditoría de editar: %d", n)
	}
}

// Despacho agrega y quita formularios; las respuestas quedan ligadas al formulario.
func TestFormulariosAsignarYQuitar(t *testing.T) {
	e := nuevoEntorno()
	cs := supervisor(t)
	d, tec := despacho(t), tecnico(t)
	cd, ct := como(t, d, "despacho"), como(t, tec, "tecnico")
	p := crearPlantilla(t, e, cs, false)

	o := programada(t, e, cd, "instalacion", tec)
	if dt := detalle(t, e, o.ID); len(dt.Formularios) != 1 || dt.Formularios[0].Origen != campo.OrigenTipo || dt.Formularios[0].PlantillaID != *o.PlantillaID {
		t.Fatalf("formulario del tipo: %+v", dt.Formularios)
	}
	// El técnico no asigna formularios (ordenes.asignar_formulario).
	_, err := e.m.AsignarFormulario.Ejecutar(ct, campo.EntradaAgregarFormulario{ID: o.ID, Version: o.Version, PlantillaID: p.ID})
	problema(t, err, 403, "prohibido")
	_, err = e.m.AsignarFormulario.Ejecutar(cd, campo.EntradaAgregarFormulario{ID: o.ID, Version: o.Version, PlantillaID: 99999999})
	problema(t, err, 422, "validacion")
	o = deber(e.m.AsignarFormulario.Ejecutar(cd, campo.EntradaAgregarFormulario{ID: o.ID, Version: o.Version, PlantillaID: p.ID}))(t)
	_, err = e.m.AsignarFormulario.Ejecutar(cd, campo.EntradaAgregarFormulario{ID: o.ID, Version: o.Version, PlantillaID: p.ID})
	problema(t, err, 409, "formulario_duplicado")
	dt := detalle(t, e, o.ID)
	f := formularioDe(t, dt, p.ID)
	if len(dt.Formularios) != 2 || f.Origen != campo.OrigenDespacho || f.AgregadoPor == nil || *f.AgregadoPor != d || f.ConRespuestas {
		t.Fatalf("formulario de despacho: %+v", dt.Formularios)
	}
	if ev := ultimo(t, eventosDe(t, o.ID), "orden.formulario_agregado"); ev["origen"] != "despacho" || ev["formulario_id"] != f.ID.String() {
		t.Fatalf("orden.formulario_agregado: %v", ev)
	}

	// El formulario del tipo no se quita.
	_, err = e.m.QuitarFormulario.Ejecutar(cd, campo.EntradaQuitarFormulario{ID: o.ID, FormularioID: dt.Formularios[0].ID, Version: o.Version})
	problema(t, err, 422, "formulario_del_tipo")
	// Quitar sin respuestas; otra vez → 409.
	o = deber(e.m.QuitarFormulario.Ejecutar(cd, campo.EntradaQuitarFormulario{ID: o.ID, FormularioID: f.ID, Version: o.Version}))(t)
	_, err = e.m.QuitarFormulario.Ejecutar(cd, campo.EntradaQuitarFormulario{ID: o.ID, FormularioID: f.ID, Version: o.Version})
	problema(t, err, 409, "formulario_ya_quitado")
	_, err = e.m.QuitarFormulario.Ejecutar(cd, campo.EntradaQuitarFormulario{ID: o.ID, FormularioID: uuid.Must(uuid.NewV7()), Version: o.Version})
	problema(t, err, 422, "validacion")
	if len(detalle(t, e, o.ID).Formularios) != 1 {
		t.Fatal("quitado: ya no sale en el detalle")
	}
	nombresEv := nombres(eventosDe(t, o.ID))
	if !slices.Contains(nombresEv, "orden.formulario_quitado") {
		t.Fatalf("eventos: %v", nombresEv)
	}

	// Se vuelve a agregar (otro formulario) y, con una respuesta, ya no se quita.
	o = deber(e.m.AsignarFormulario.Ejecutar(cd, campo.EntradaAgregarFormulario{ID: o.ID, Version: o.Version, PlantillaID: p.ID}))(t)
	dt = detalle(t, e, o.ID)
	f2 := formularioDe(t, dt, p.ID)
	if f2.ID == f.ID {
		t.Fatal("es un formulario nuevo")
	}
	// observaciones está en los dos formularios: por código hace falta formularioId.
	_, err = e.m.MarcarChecklist.Ejecutar(ct, campo.EntradaMarcarChecklist{ID: o.ID, Codigo: ptr("observaciones"), Valor: "Todo bien"})
	if c := problema(t, err, 422, "validacion"); c[0].Field != "formularioId" {
		t.Fatalf("código ambiguo: %+v", c)
	}
	o = deber(e.m.MarcarChecklist.Ejecutar(ct, campo.EntradaMarcarChecklist{ID: o.ID, Codigo: ptr("observaciones"), FormularioID: &f2.ID, Valor: "Todo bien"}))(t)
	// Un ítem del formulario quitado: 409 formulario_quitado.
	viejo := contar(t, "SELECT count(*) FROM campo.plantilla_items WHERE version_id = $1", f.VersionID)
	if viejo == 0 {
		t.Fatal("ítems de la versión")
	}
	dt = detalle(t, e, o.ID)
	if itemDe(t, dt, f2.ID, "observaciones").Valor != "Todo bien" || itemDe(t, dt, dt.Formularios[0].ID, "observaciones").Valor != nil {
		t.Fatal("la marca es del formulario elegido")
	}
	_, err = e.m.QuitarFormulario.Ejecutar(cd, campo.EntradaQuitarFormulario{ID: o.ID, FormularioID: f2.ID, Version: o.Version})
	problema(t, err, 422, "formulario_con_respuestas")
	if n := contar(t, "SELECT count(*) FROM campo.orden_checklist WHERE formulario_id = $1", f2.ID); n != 1 {
		t.Fatalf("la respuesta está ligada al formulario: %d", n)
	}

	// Historial de la orden con los formularios.
	evs := deber(e.m.ListarEventos.Ejecutar(cd, campo.EntradaListarEventos{ID: o.ID}))(t).Data
	formularios := 0
	for _, ev := range evs {
		if ev.Tipo == "formulario" {
			formularios++
		}
	}
	if formularios != 4 { // tipo, agregado, quitado, agregado
		t.Fatalf("historial de formularios: %d", formularios)
	}

	// Una orden cerrada o cancelada no admite formularios.
	o = deber(e.m.CancelarOrden.Ejecutar(cd, campo.EntradaMotivo{ID: o.ID, Version: o.Version, Motivo: "Cliente desistió"}))(t)
	_, err = e.m.AsignarFormulario.Ejecutar(cd, campo.EntradaAgregarFormulario{ID: o.ID, Version: o.Version, PlantillaID: p.ID})
	problema(t, err, 422, "transicion_invalida")
}

// Cerrar exige los obligatorios de todos los formularios (uno por uno), las fotos de todos y la
// firma si alguno la pide.
func TestCierreConVariosFormularios(t *testing.T) {
	e := nuevoEntorno()
	cs := supervisor(t)
	d, tec := despacho(t), tecnico(t)
	cd, ct := como(t, d, "despacho"), como(t, tec, "tecnico")
	p := crearPlantilla(t, e, cs, true)

	// Mantenimiento: su plantilla no pide firma; la agregada sí.
	o := programada(t, e, cd, "mantenimiento", tec)
	o = deber(e.m.AsignarFormulario.Ejecutar(cd, campo.EntradaAgregarFormulario{ID: o.ID, Version: o.Version, PlantillaID: p.ID}))(t)
	o = deber(e.m.Llegar.Ejecutar(ct, campo.EntradaPosicion{ID: o.ID, Version: o.Version}))(t)
	dt := detalle(t, e, o.ID)
	tipoF, extra := dt.Formularios[0], formularioDe(t, dt, p.ID)
	if tipoF.RequiereFirma || !extra.RequiereFirma || !dt.RequiereFirma {
		t.Fatalf("firma: %+v", dt.Formularios)
	}
	// Solo el formulario del tipo completo.
	it := itemDe(t, dt, tipoF.ID, "trabajo_realizado")
	o = deber(e.m.MarcarChecklist.Ejecutar(ct, campo.EntradaMarcarChecklist{ID: o.ID, ItemID: &it.ItemID, Valor: "Empalme"}))(t)
	_, err := e.m.CerrarOrden.Ejecutar(ct, campo.EntradaCerrarOrden{ID: o.ID, Version: o.Version})
	campos := []string{}
	mensajes := []string{}
	for _, c := range problema(t, err, 422, "cierre_incompleto") {
		campos = append(campos, c.Field)
		mensajes = append(mensajes, c.Message)
	}
	pref := "formularios." + extra.ID.String() + ".checklist."
	if !slices.Equal(campos, []string{pref + "poste_ok", pref + "estado_caja", pref + "foto_caja", "firma"}) {
		t.Fatalf("lo que falta, formulario por formulario: %v", campos)
	}
	if !strings.Contains(mensajes[0], "Revisión de caja NAP") || !strings.Contains(mensajes[3], "Revisión de caja NAP") {
		t.Fatalf("mensajes con el nombre del formulario: %v", mensajes)
	}

	completarChecklist(t, e, ct, o.ID)
	o = detalle(t, e, o.ID).Orden
	firma := uuid.Must(uuid.NewV7())
	o = deber(e.m.CapturarFirma.Ejecutar(ct, campo.EntradaCapturarFirma{ID: o.ID, Nombre: "Ana López",
		SVG: `<svg xmlns="http://www.w3.org/2000/svg"><path d="M0 0L10 10"/></svg>`, AdjuntoID: &firma}))(t)
	// Las fotos obligatorias de todos los formularios y la firma van al módulo de adjuntos.
	e.adjuntos.fijar(o.ID, "foto:foto_caja")
	_, err = e.m.CerrarOrden.Ejecutar(ct, campo.EntradaCerrarOrden{ID: o.ID, Version: o.Version})
	problema(t, err, 422, "faltan_adjuntos")
	req := e.adjuntos.requisitos[o.ID]
	if !slices.ContainsFunc(req, func(r campo.Requisito) bool { return r.ClaveChecklist == "foto_caja" && r.Minimo == 1 }) ||
		!slices.ContainsFunc(req, func(r campo.Requisito) bool { return r.Tipo == "firma" }) {
		t.Fatalf("requisitos de adjuntos: %+v", req)
	}
	e.adjuntos.fijar(o.ID)
	o = deber(e.m.CerrarOrden.Ejecutar(ct, campo.EntradaCerrarOrden{ID: o.ID, Version: o.Version}))(t)
	if o.Estado != "cerrada" {
		t.Fatalf("cerrada: %+v", o)
	}
	// Ya cerrada, no se quita ni agrega nada.
	_, err = e.m.QuitarFormulario.Ejecutar(cd, campo.EntradaQuitarFormulario{ID: o.ID, FormularioID: extra.ID, Version: o.Version})
	problema(t, err, 422, "transicion_invalida")
}

// El técnico agrega y quita formularios sin señal (orden.agregar_formulario y
// orden.quitar_formulario), con sus conflictos (contratos/sync.md §3).
func TestFormulariosPorSync(t *testing.T) {
	e := nuevoEntorno()
	cs := supervisor(t)
	d, tec := despacho(t), tecnico(t)
	cd, ct := como(t, d, "despacho"), como(t, tec, "tecnico")
	p := crearPlantilla(t, e, cs, false)
	o := programada(t, e, cd, "visita", tec)

	// El paquete trae el catálogo (versión actual) y los formularios de cada orden.
	paq := deber(e.m.Paquete(ct, campo.EntradaPaquete{Fecha: "2026-10-28"}))(t)
	i := slices.IndexFunc(paq.Plantillas, func(x campo.PlantillaFormulario) bool { return x.ID == p.ID })
	if i < 0 || paq.Plantillas[i].VersionID != p.VersionID || len(paq.Plantillas[i].Items) != 4 {
		t.Fatalf("catálogo del paquete: %+v", paq.Plantillas)
	}
	j := slices.IndexFunc(paq.Ordenes, func(x campo.OrdenCampo) bool { return x.ID == o.ID })
	if j < 0 || len(paq.Ordenes[j].Formularios) != 1 {
		t.Fatalf("formularios en el paquete: %+v", paq.Ordenes)
	}

	// Agrega el formulario y marca sus ítems en el mismo lote (por itemId del catálogo).
	agregar := op(campo.OpAgregarForm, o.ID, map[string]any{"plantillaId": p.ID, "versionId": p.VersionID})
	marca := op(campo.OpChecklistMarcar, o.ID, map[string]any{"itemId": p.Items[0].ID, "valor": true})
	r := lote(t, e, ct, agregar, marca)
	esperar(t, r, "aplicada", "aplicada")
	dt := detalle(t, e, o.ID)
	f := formularioDe(t, dt, p.ID)
	if f.ID.String() != agregar.ID || f.Origen != campo.OrigenTecnico || itemDe(t, dt, f.ID, "poste_ok").Valor != true {
		t.Fatalf("formulario del técnico (id = el de la operación): %+v", f)
	}
	esperar(t, lote(t, e, ct, agregar), "duplicada")

	// Despacho ya agregó la misma plantilla mientras el técnico estaba sin señal: 409.
	p2 := crearPlantilla(t, e, cs, false)
	o = detalle(t, e, o.ID).Orden
	deber(e.m.AsignarFormulario.Ejecutar(cd, campo.EntradaAgregarFormulario{ID: o.ID, Version: o.Version, PlantillaID: p2.ID}))(t)
	r = lote(t, e, ct, op(campo.OpAgregarForm, o.ID, map[string]any{"plantillaId": p2.ID}),
		op(campo.OpNotaAgregar, o.ID, map[string]any{"texto": "después"}))
	esperar(t, r, "rechazada:formulario_duplicado", "rechazada:bloqueada_por_rechazo")
	// Y el técnico no quita uno que no agregó él.
	f2 := formularioDe(t, detalle(t, e, o.ID), p2.ID)
	esperar(t, lote(t, e, ct, op(campo.OpQuitarForm, o.ID, map[string]any{"formularioId": f2.ID})), "rechazada:formulario_ajeno")

	// La plantilla se desactivó mientras el técnico estaba sin señal: rechazada, nunca en silencio.
	p3 := crearPlantilla(t, e, cs, false)
	deber(e.m.DesactivarPlantilla.Ejecutar(cs, campo.EntradaVersionPlantilla{ID: p3.ID, Version: p3.Version}))(t)
	esperar(t, lote(t, e, ct, op(campo.OpAgregarForm, o.ID, map[string]any{"plantillaId": p3.ID})), "rechazada:plantilla_inactiva")
	// El catálogo de cambios ya no la trae.
	c := deber(e.m.Cambios(ct, campo.EntradaCambios{Cursor: paq.Cursor}))(t)
	if slices.ContainsFunc(c.Plantillas, func(x campo.PlantillaFormulario) bool { return x.ID == p3.ID }) ||
		!slices.ContainsFunc(c.Plantillas, func(x campo.PlantillaFormulario) bool { return x.ID == p.ID }) {
		t.Fatal("catálogo en cambios")
	}
	if k := slices.IndexFunc(c.Cambios, func(x campo.CambioOrden) bool { return x.OrdenID == o.ID }); k < 0 || len(c.Cambios[k].Orden.Formularios) != 3 {
		t.Fatalf("la orden en cambios trae sus formularios")
	}

	// La plantilla se editó (con uso → versión 2) mientras el técnico estaba sin señal: se agrega
	// la versión que veía (sus marcas son de esos ítems), con aviso version_anterior.
	p4 := crearPlantilla(t, e, cs, false)
	otra := crearOrden(t, e, cd, "visita")
	deber(e.m.AsignarFormulario.Ejecutar(cd, campo.EntradaAgregarFormulario{ID: otra.ID, Version: otra.Version, PlantillaID: p4.ID}))(t)
	vioV1 := e.reloj.ahora().Add(10 * time.Minute) // el teléfono la vio y la agregó antes de la edición
	e.reloj.avanzar(30 * time.Minute)
	p4b := deber(e.m.EditarPlantilla.Ejecutar(cs, campo.EntradaEditarPlantilla{ID: p4.ID, Version: p4.Version, Nombre: "NAP v2", Items: itemsPrueba()[:2]}))(t)
	if p4b.NumeroVersion != 2 {
		t.Fatal("versión 2")
	}
	ag := op(campo.OpAgregarForm, o.ID, map[string]any{"plantillaId": p4.ID, "versionId": p4.VersionID})
	ag.OcurridoEn = &vioV1
	r = lote(t, e, ct, ag, op(campo.OpChecklistMarcar, o.ID, map[string]any{"itemId": p4.Items[2].ID, "valor": uuid.Must(uuid.NewV7()).String()}))
	esperar(t, r, "aplicada", "aplicada")
	if r.Resultados[0].Aviso == nil || *r.Resultados[0].Aviso != campo.AvisoVersionAnterior {
		t.Fatalf("aviso: %+v", r.Resultados[0])
	}
	if f4 := formularioDe(t, detalle(t, e, o.ID), p4.ID); f4.NumeroVersion != 1 {
		t.Fatalf("la versión que veía el teléfono: %+v", f4)
	}
	// Una versión que no es de esa plantilla: 422.
	esperar(t, lote(t, e, ct, op(campo.OpAgregarForm, o.ID, map[string]any{"plantillaId": p.ID, "versionId": p4.VersionID})),
		"rechazada:validacion")

	// Quitar el suyo sin respuestas: sí; con respuestas: no.
	p5 := crearPlantilla(t, e, cs, false)
	ag5 := op(campo.OpAgregarForm, o.ID, map[string]any{"plantillaId": p5.ID})
	esperar(t, lote(t, e, ct, ag5, op(campo.OpQuitarForm, o.ID, map[string]any{"formularioId": ag5.ID})), "aplicada", "aplicada")
	esperar(t, lote(t, e, ct, op(campo.OpQuitarForm, o.ID, map[string]any{"formularioId": agregar.ID})), "rechazada:formulario_con_respuestas")

	// Despacho quitó (sin respuestas en el servidor) un formulario que el técnico llenaba sin
	// señal: su marca llega después y se rechaza con formulario_quitado.
	p6 := crearPlantilla(t, e, cs, false)
	o = detalle(t, e, o.ID).Orden
	o = deber(e.m.AsignarFormulario.Ejecutar(cd, campo.EntradaAgregarFormulario{ID: o.ID, Version: o.Version, PlantillaID: p6.ID}))(t)
	f6 := formularioDe(t, detalle(t, e, o.ID), p6.ID)
	deber(e.m.QuitarFormulario.Ejecutar(cd, campo.EntradaQuitarFormulario{ID: o.ID, FormularioID: f6.ID, Version: o.Version}))(t)
	esperar(t, lote(t, e, ct, op(campo.OpChecklistMarcar, o.ID, map[string]any{"itemId": p6.Items[0].ID, "valor": true})),
		"rechazada:formulario_quitado")

	// Solo en una orden propia y abierta: la de otro técnico no la ve (404); una cancelada, 409.
	ajena := programada(t, e, cd, "visita", tecnico(t))
	esperar(t, lote(t, e, ct, op(campo.OpAgregarForm, ajena.ID, map[string]any{"plantillaId": p.ID})), "rechazada:no_encontrado")
	cancelada := programada(t, e, cd, "visita", tec)
	deber(e.m.CancelarOrden.Ejecutar(cd, campo.EntradaMotivo{ID: cancelada.ID, Version: cancelada.Version, Motivo: "Ya no"}))(t)
	esperar(t, lote(t, e, ct, op(campo.OpAgregarForm, cancelada.ID, map[string]any{"plantillaId": p.ID})), "rechazada:orden_cancelada")
	// Despacho (con ordenes.registrar) tampoco agrega por el lote en una orden que no es suya.
	esperar(t, lote(t, e, como(t, d, "despacho"), op(campo.OpAgregarForm, ajena.ID, map[string]any{"plantillaId": p.ID})), "rechazada:prohibido")
	// Datos validados con el esquema.
	esperar(t, lote(t, e, ct, op(campo.OpAgregarForm, o.ID, map[string]any{"plantilla": 1})), "rechazada:validacion")
}

// Multiempresa: una plantilla de la empresa A no la ve ni la usa la B; las comunes, con empresas
// registradas, solo las modifica empresas.todas (regla de la #20).
func TestPlantillasMultiempresa(t *testing.T) {
	registrarEmpresas(t, 71, 72)
	e := nuevoEntorno()
	sup := usuarioCon(t, rolDe("supervisor"))
	asignarEmpresa(t, sup, 71)
	asignarEmpresa(t, sup, 72)
	permisos := permisosDe("supervisor")
	a := pruebas.ContextoUsuarioEnEmpresa(pruebas.Pool(t), sup, 71, permisos...)
	b := pruebas.ContextoUsuarioEnEmpresa(pruebas.Pool(t), sup, 72, permisos...)

	p := crearPlantilla(t, e, a, false)
	if p.EmpresaID == nil || *p.EmpresaID != 71 {
		t.Fatalf("de la empresa activa: %+v", p.EmpresaID)
	}
	// Mismo código en otra empresa: se puede.
	deber(e.m.CrearPlantilla.Ejecutar(b, campo.EntradaCrearPlantilla{Codigo: p.Codigo, Nombre: "Otra", Items: itemsPrueba()}))(t)
	if lista := deber(e.m.ListarPlantillas.Ejecutar(b, campo.EntradaListarPlantillas{}))(t); slices.ContainsFunc(lista.Data,
		func(x campo.PlantillaFormulario) bool { return x.ID == p.ID }) {
		t.Fatal("la B no ve la plantilla de la A")
	}
	_, err := e.m.VerPlantilla.Ejecutar(b, campo.EntradaVerPlantilla{ID: p.ID})
	problema(t, err, 404, "no_encontrado")
	_, err = e.m.EditarPlantilla.Ejecutar(b, campo.EntradaEditarPlantilla{ID: p.ID, Version: p.Version, Nombre: "X", Items: itemsPrueba()})
	problema(t, err, 404, "no_encontrado")

	// Una orden de la B no usa la plantilla de la A.
	zonaDe(t, 7172, 72)
	ob := deber(e.m.CrearOrden.Ejecutar(b, campo.EntradaCrearOrden{Tipo: "visita", Direccion: ptr("Zona B"), ZonaID: ptr(int64(7172))}))(t)
	_, err = e.m.AsignarFormulario.Ejecutar(b, campo.EntradaAgregarFormulario{ID: ob.ID, Version: ob.Version, PlantillaID: p.ID})
	problema(t, err, 422, "validacion")
	_, err = e.m.CrearOrden.Ejecutar(b, campo.EntradaCrearOrden{Tipo: "visita", PlantillaID: &p.ID, Direccion: ptr("Zona B"), ZonaID: ptr(int64(7172))})
	problema(t, err, 422, "validacion")

	// Comunes: sin empresas.todas no se crean ni editan; con empresas.todas, sí.
	_, err = e.m.CrearPlantilla.Ejecutar(a, campo.EntradaCrearPlantilla{Codigo: "comun_" + strings.ReplaceAll(pruebas.Unico(), "-", "_"),
		Nombre: "Común", Comun: true, Items: itemsPrueba()})
	problema(t, err, 403, "prohibido")
	semilla := deber(e.m.ListarPlantillas.Ejecutar(a, campo.EntradaListarPlantillas{Tipo: "mantenimiento"}))(t).Data
	k := slices.IndexFunc(semilla, func(x campo.PlantillaFormulario) bool { return x.Codigo == "mantenimiento" })
	if k < 0 || semilla[k].EmpresaID != nil {
		t.Fatalf("la semilla es común y la ven todas: %+v", semilla)
	}
	comun := semilla[k]
	_, err = e.m.EditarPlantilla.Ejecutar(a, campo.EntradaEditarPlantilla{ID: comun.ID, Version: comun.Version, Nombre: "X", Items: itemsPrueba()})
	problema(t, err, 403, "prohibido")
	admin := usuarioCon(t, rolDe("admin"))
	global := pruebas.ContextoUsuarioEnEmpresa(pruebas.Pool(t), admin, 71, permisosDe("admin")...)
	c := deber(e.m.CrearPlantilla.Ejecutar(global, campo.EntradaCrearPlantilla{Codigo: "comun_" + strings.ReplaceAll(pruebas.Unico(), "-", "_"),
		Nombre: "Común", Comun: true, Items: itemsPrueba()}))(t)
	if c.EmpresaID != nil {
		t.Fatalf("común: %+v", c.EmpresaID)
	}
	// La común sirve a las órdenes de cualquier empresa.
	deber(e.m.AsignarFormulario.Ejecutar(b, campo.EntradaAgregarFormulario{ID: ob.ID, Version: ob.Version, PlantillaID: c.ID}))(t)
	// Alcance limitado: administrar plantillas exige el permiso global.
	limitado := conAlcance(t, sup, map[string][]contexto.Alcance{"plantillas.crear": {{ZonaID: 3}}})
	_, err = e.m.CrearPlantilla.Ejecutar(limitado, campo.EntradaCrearPlantilla{Codigo: "limitada", Nombre: "X", Items: itemsPrueba()})
	problema(t, err, 403, "prohibido")
}

// Revisión de la PR #29, punto 1: el teléfono solo fija la versión actual o la vigente a la hora
// de la operación; una más vieja (v1 cuando a esa hora ya regía v2) dejaría la orden con una
// versión superada y bloquearía la actual.
func TestRevisionVersionVigenteAlAgregar(t *testing.T) {
	e := nuevoEntorno()
	cs := supervisor(t)
	d, tec := despacho(t), tecnico(t)
	cd, ct := como(t, d, "despacho"), como(t, tec, "tecnico")
	o := programada(t, e, cd, "visita", tec)
	o2 := programada(t, e, cd, "visita", tec)
	t0 := e.reloj.ahora()
	p := crearPlantilla(t, e, cs, false) // v1 a t0
	e.reloj.avanzar(time.Hour)
	p2 := deber(e.m.EditarPlantilla.Ejecutar(cs, campo.EntradaEditarPlantilla{ID: p.ID, Version: p.Version, Nombre: "v2", Items: itemsPrueba()}))(t)
	e.reloj.avanzar(time.Hour)
	p3 := deber(e.m.EditarPlantilla.Ejecutar(cs, campo.EntradaEditarPlantilla{ID: p.ID, Version: p2.Version, Nombre: "v3", Items: itemsPrueba()}))(t)
	if p2.NumeroVersion != 2 || p3.NumeroVersion != 3 {
		t.Fatalf("versiones: %d %d", p2.NumeroVersion, p3.NumeroVersion)
	}
	agregar := func(version int64, momento time.Time) campo.OperacionLote {
		x := op(campo.OpAgregarForm, o.ID, map[string]any{"plantillaId": p.ID, "versionId": version})
		x.OcurridoEn = &momento
		return x
	}
	// A t0+90 min regía v2: v1 se rechaza (422 versionId), aunque sea de la plantilla.
	r := lote(t, e, ct, agregar(p.VersionID, t0.Add(90*time.Minute)))
	esperar(t, r, "rechazada:validacion")
	if r.Resultados[0].Error.Errors[0].Field != "versionId" {
		t.Fatalf("campo: %+v", r.Resultados[0].Error)
	}
	// v1 a t0+30 min (la vigente entonces): aplicada con aviso.
	r = lote(t, e, ct, agregar(p.VersionID, t0.Add(30*time.Minute)))
	esperar(t, r, "aplicada")
	if *r.Resultados[0].Aviso != campo.AvisoVersionAnterior || formularioDe(t, detalle(t, e, o.ID), p.ID).NumeroVersion != 1 {
		t.Fatalf("vigente a esa hora: %+v", r.Resultados[0])
	}
	// En otra orden (asignada antes): v2 a t0+90 min sí; la actual (v3), siempre.
	x := op(campo.OpAgregarForm, o2.ID, map[string]any{"plantillaId": p.ID, "versionId": p2.VersionID})
	x.OcurridoEn = ptr(t0.Add(90 * time.Minute))
	esperar(t, lote(t, e, ct, x), "aplicada")
	o3 := programada(t, e, cd, "visita", tec)
	esperar(t, lote(t, e, ct, op(campo.OpAgregarForm, o3.ID, map[string]any{"plantillaId": p.ID, "versionId": p3.VersionID})), "aplicada")
}

// Punto 2: una plantilla activa sin uso en órdenes ya está en el catálogo de los teléfonos;
// editarla crea una versión, así lo que el técnico llena sin señal (ítems de la versión que vio)
// sigue siendo válido y los obligatorios nuevos no le cambian sin aviso.
func TestRevisionEdicionDeActivaSinUsoCreaVersion(t *testing.T) {
	e := nuevoEntorno()
	cs := supervisor(t)
	d, tec := despacho(t), tecnico(t)
	cd, ct := como(t, d, "despacho"), como(t, tec, "tecnico")
	o := programada(t, e, cd, "visita", tec)
	p := crearPlantilla(t, e, cs, false)
	paq := deber(e.m.Paquete(ct, campo.EntradaPaquete{Fecha: "2026-10-28"}))(t)
	visto := paq.Plantillas[slices.IndexFunc(paq.Plantillas, func(x campo.PlantillaFormulario) bool { return x.ID == p.ID })]
	sinSenal := e.reloj.ahora().Add(10 * time.Minute)
	e.reloj.avanzar(30 * time.Minute)

	// Sin señal, el técnico la agrega y marca; mientras, se edita (sin uso todavía) con un
	// obligatorio nuevo.
	items := append(itemsPrueba(), campo.EntradaItemPlantilla{Codigo: "nuevo", Etiqueta: "Nuevo obligatorio", Tipo: campo.ItemSiNo, Obligatorio: true})
	p2 := deber(e.m.EditarPlantilla.Ejecutar(cs, campo.EntradaEditarPlantilla{ID: p.ID, Version: p.Version, Nombre: "Con nuevo", Items: items}))(t)
	if p2.NumeroVersion != 2 || p2.VersionID == visto.VersionID {
		t.Fatalf("versión nueva: %+v", p2)
	}
	ag := op(campo.OpAgregarForm, o.ID, map[string]any{"plantillaId": p.ID, "versionId": visto.VersionID})
	ag.OcurridoEn = &sinSenal
	marca := op(campo.OpChecklistMarcar, o.ID, map[string]any{"itemId": visto.Items[0].ID, "valor": true})
	r := lote(t, e, ct, ag, marca)
	esperar(t, r, "aplicada", "aplicada")
	f := formularioDe(t, detalle(t, e, o.ID), p.ID)
	if f.NumeroVersion != 1 || itemDe(t, detalle(t, e, o.ID), f.ID, "poste_ok").Valor != true ||
		slices.ContainsFunc(detalle(t, e, o.ID).Checklist, func(it campo.ItemChecklist) bool { return it.Codigo == "nuevo" }) {
		t.Fatalf("el formulario conserva la versión que vio el teléfono: %+v", f)
	}
}

// Ajuste del panel: sin empresa activa (modo «todas», el sistema) una plantilla solo es común si
// se pide (comun: true); si no, 422 empresa_requerida.
func TestPlantillaComunSoloExplicita(t *testing.T) {
	e := nuevoEntorno()
	sis := pruebas.ContextoSistema(pruebas.Pool(t))
	codigo := "sin_empresa_" + strings.ReplaceAll(pruebas.Unico(), "-", "_")
	_, err := e.m.CrearPlantilla.Ejecutar(sis, campo.EntradaCrearPlantilla{Codigo: codigo, Nombre: "X", Items: itemsPrueba()})
	problema(t, err, 422, "empresa_requerida")
	p := deber(e.m.CrearPlantilla.Ejecutar(sis, campo.EntradaCrearPlantilla{Codigo: codigo, Nombre: "X", Comun: true, Items: itemsPrueba()}))(t)
	if p.EmpresaID != nil {
		t.Fatalf("común explícita: %+v", p.EmpresaID)
	}
	// Sin conector sigue siendo común sin pedirlo.
	q := crearPlantilla(t, e, supervisor(t), false)
	if q.EmpresaID != nil {
		t.Fatalf("sin conector: %+v", q.EmpresaID)
	}
}

// GET /plantillas paginado por cursor (por nombre), con los filtros.
func TestListarPlantillasPorCursor(t *testing.T) {
	e := nuevoEntorno()
	cs := supervisor(t)
	marca := strings.ReplaceAll(pruebas.Unico(), "-", "_")
	for i := range 5 {
		deber(e.m.CrearPlantilla.Ejecutar(cs, campo.EntradaCrearPlantilla{Codigo: fmt.Sprintf("pag_%s_%d", marca, i),
			Nombre: fmt.Sprintf("Paginada %s %d", marca, i), Borrador: i == 4, Items: itemsPrueba()}))(t)
	}
	vistas := []string{}
	in := campo.EntradaListarPlantillas{Q: "Paginada " + marca, Todas: true}
	in.Limit = 2
	for paginas := 0; ; paginas++ {
		pg := deber(e.m.ListarPlantillas.Ejecutar(cs, in))(t)
		for _, p := range pg.Data {
			vistas = append(vistas, p.Nombre)
		}
		if pg.NextCursor == nil {
			if paginas != 2 {
				t.Fatalf("páginas: %d", paginas+1)
			}
			break
		}
		in.Cursor = *pg.NextCursor
	}
	if len(vistas) != 5 || !slices.IsSorted(vistas) {
		t.Fatalf("todas, por nombre y sin repetir: %v", vistas)
	}
	// Sin todas, el borrador (inactivo) no sale.
	if pg := deber(e.m.ListarPlantillas.Ejecutar(cs, campo.EntradaListarPlantillas{Q: "Paginada " + marca}))(t); len(pg.Data) != 4 || pg.NextCursor != nil {
		t.Fatalf("activas: %d", len(pg.Data))
	}
	_, err := e.m.ListarPlantillas.Ejecutar(cs, campo.EntradaListarPlantillas{ParamsPagina: nucleo.ParamsPagina{Cursor: "@@"}})
	problema(t, err, 422, "validacion")
}
