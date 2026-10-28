package campo_test

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	auditoriabd "github.com/HCDestroyer/iris/apps/api/internal/auditoria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/campo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
	"github.com/HCDestroyer/iris/apps/api/internal/soporte"
)

func TestCicloCompletoDeInstalacion(t *testing.T) {
	e := nuevoEntorno()
	d, tec := despacho(t), tecnico(t)
	cd, ct := como(t, d, "despacho"), como(t, tec, "tecnico")

	o := crearOrden(t, e, cd, "instalacion")
	if o.Estado != "pendiente" || o.Tipo != "instalacion" || o.Version != 1 || o.Numero[:3] != "OT-" || o.PlantillaID == nil {
		t.Fatalf("orden creada: %+v", o)
	}
	acciones, _ := auditoriabd.New(pruebas.Pool(t)).AccionesPorRequest(context.Background(), contexto.RequestIDDe(cd))
	if len(acciones) != 1 || acciones[0].Accion != "ordenes.crear" || acciones[0].Resultado != "ok" {
		t.Fatalf("auditoría de crear: %+v", acciones)
	}

	// Sin técnico no se programa; programar exige pendiente.
	_, err := e.m.ProgramarOrden.Ejecutar(cd, campo.EntradaProgramarOrden{ID: o.ID, Version: 1, Inicio: inicio})
	problema(t, err, 422, "validacion")
	o = deber(e.m.ProgramarOrden.Ejecutar(cd, campo.EntradaProgramarOrden{ID: o.ID, Version: 1, TecnicoID: &tec, BodegaID: ptr(int64(9)), Inicio: inicio.Add(time.Hour)}))(t)
	if o.Estado != "programada" || o.ProgramadaFin == nil || !o.ProgramadaFin.Equal(inicio.Add(3*time.Hour)) || *o.TecnicoID != tec {
		t.Fatalf("programada (fin = inicio + 120 min): %+v", o)
	}
	// Versión vieja → 409.
	_, err = e.m.IniciarCamino.Ejecutar(ct, campo.EntradaPosicion{ID: o.ID, Version: 1})
	problema(t, err, 409, "conflicto_version")
	// El técnico no puede cerrar desde programada ni programar.
	_, err = e.m.CerrarOrden.Ejecutar(ct, campo.EntradaCerrarOrden{ID: o.ID, Version: o.Version})
	problema(t, err, 422, "transicion_invalida")
	_, err = e.m.ReprogramarOrden.Ejecutar(ct, campo.EntradaProgramarOrden{ID: o.ID, Version: o.Version, Inicio: inicio, Motivo: ptr("x")})
	problema(t, err, 403, "prohibido")

	o = deber(e.m.IniciarCamino.Ejecutar(ct, campo.EntradaPosicion{ID: o.ID, Version: o.Version, Lat: ptr(14.61), Lon: ptr(-90.51), Precision: ptr(8.0)}))(t)
	o = deber(e.m.Llegar.Ejecutar(ct, campo.EntradaPosicion{ID: o.ID, Version: o.Version}))(t)
	if o.Estado != "en_sitio" || o.EnCaminoEn == nil || o.EnSitioEn == nil {
		t.Fatalf("en sitio: %+v", o)
	}

	// Cerrar sin requisitos: la lista completa de lo que falta.
	_, err = e.m.CerrarOrden.Ejecutar(ct, campo.EntradaCerrarOrden{ID: o.ID, Version: o.Version})
	campos := []string{}
	for _, c := range problema(t, err, 422, "cierre_incompleto") {
		campos = append(campos, c.Field)
	}
	// Los del checklist, formulario por formulario: formularios.<id>.checklist.<código>.
	for _, f := range []string{".checklist.potencia_rx", ".checklist.foto_onu", "equipos.instalar", "firma"} {
		if !slices.ContainsFunc(campos, func(c string) bool { return strings.HasSuffix(c, f) }) {
			t.Fatalf("falta %s en %v", f, campos)
		}
	}
	if slices.ContainsFunc(campos, func(c string) bool { return strings.HasSuffix(c, ".checklist.observaciones") }) {
		t.Fatal("observaciones no es obligatorio")
	}

	completarChecklist(t, e, ct, o.ID)
	o = deber(e.m.InstalarEquipo.Ejecutar(ct, campo.EntradaEquipo{ID: o.ID, Serie: ptr("zte123"), MAC: ptr("aabbccddeeff"), Modelo: "F660"}))(t)
	_, err = e.m.InstalarEquipo.Ejecutar(ct, campo.EntradaEquipo{ID: o.ID, Serie: ptr("ZTE123")})
	problema(t, err, 422, "equipo_duplicado")
	_, err = e.m.InstalarEquipo.Ejecutar(ct, campo.EntradaEquipo{ID: o.ID, Serie: ptr("X1"), Condicion: ptr("buena")})
	problema(t, err, 422, "validacion")
	_, err = e.m.RetirarEquipo.Ejecutar(ct, campo.EntradaEquipo{ID: o.ID, Serie: ptr("VIEJO")})
	problema(t, err, 422, "validacion")
	o = deber(e.m.UsarMaterial.Ejecutar(ct, campo.EntradaUsarMaterial{ID: o.ID, ProductoID: 40, Cantidad: 30.5}))(t)
	o = deber(e.m.UsarMaterial.Ejecutar(ct, campo.EntradaUsarMaterial{ID: o.ID, ProductoID: 40, Cantidad: 4.25}))(t)
	firma := uuid.Must(uuid.NewV7())
	o = deber(e.m.CapturarFirma.Ejecutar(ct, campo.EntradaCapturarFirma{ID: o.ID, Nombre: "Ana López", Documento: "1234",
		SVG: `<svg xmlns="http://www.w3.org/2000/svg"><path d="M0 0L10 10"/></svg>`, AdjuntoID: &firma}))(t)

	// El checklist subió la versión: con la vieja, 409.
	_, err = e.m.CerrarOrden.Ejecutar(ct, campo.EntradaCerrarOrden{ID: o.ID, Version: o.Version - 1})
	problema(t, err, 409, "conflicto_version")
	o = deber(e.m.CerrarOrden.Ejecutar(ct, campo.EntradaCerrarOrden{ID: o.ID, Version: o.Version, Lat: ptr(14.6), Lon: ptr(-90.5)}))(t)
	if o.Estado != "cerrada" || o.Resultado == nil || *o.Resultado != "exitosa" || o.FinalizadaEn == nil {
		t.Fatalf("cerrada: %+v", o)
	}
	_, err = e.m.CerrarOrden.Ejecutar(ct, campo.EntradaCerrarOrden{ID: o.ID, Version: o.Version})
	problema(t, err, 422, "transicion_invalida")
	_, err = e.m.ReabrirOrden.Ejecutar(cd, campo.EntradaMotivo{ID: o.ID, Version: o.Version, Motivo: "Otra vez"})
	problema(t, err, 422, "transicion_invalida")

	es := eventosDe(t, o.ID)
	for _, n := range []string{"orden.creada", "orden.programada", "orden.asignada", "orden.estado_cambiada", "orden.cerrada"} {
		if !slices.Contains(nombres(es), n) {
			t.Fatalf("falta %s: %v", n, nombres(es))
		}
	}
	c := ultimo(t, es, "orden.cerrada")
	if c["tipo"] != "instalacion" || c["resultado"] != "exitosa" || c["tecnico_id"] != float64(tec) || c["empresa_id"] != nil {
		t.Fatalf("orden.cerrada: %v", c)
	}
	mats := c["materiales"].([]any)
	if len(mats) != 1 || mats[0].(map[string]any)["cantidad"] != 34.75 || mats[0].(map[string]any)["bodega_id"] != float64(9) {
		t.Fatalf("materiales sumados por producto y bodega: %v", mats)
	}
	if c["retirar_servicio"] != false {
		t.Fatal("una instalación no retira el servicio")
	}
	eqs := c["equipos"].([]any)
	if len(eqs) != 1 || eqs[0].(map[string]any)["serie"] != "ZTE123" || eqs[0].(map[string]any)["mac"] != "AA:BB:CC:DD:EE:FF" ||
		eqs[0].(map[string]any)["accion"] != "instalar" {
		t.Fatalf("equipos: %v", eqs)
	}

	// Historial: creada, programada, estados (con GPS) y la firma nunca en la auditoría.
	hist := deber(e.m.ListarEventos.Ejecutar(cd, campo.EntradaListarEventos{ID: o.ID}))(t)
	var gps bool
	for _, h := range hist.Data {
		if h.Tipo == "estado" && h.A != nil && *h.A == "en_camino" && h.Datos["lat"] == 14.61 && h.Datos["precision"] == 8.0 {
			gps = true
		}
	}
	if !gps {
		t.Fatalf("GPS con hora en el historial: %+v", hist.Data)
	}
	if n := contar(t, "SELECT count(*) FROM auditoria.acciones WHERE entrada::text LIKE '%M0 0L10%'"); n != 0 {
		t.Fatal("el SVG de la firma quedó en la auditoría")
	}
	if n := contar(t, "SELECT count(*) FROM auditoria.acciones WHERE accion = 'ordenes.cerrar' AND entidad_id = $1 AND resultado = 'ok'", strconv.FormatInt(o.ID, 10)); n != 1 {
		t.Fatalf("auditoría de cerrar: %d", n)
	}
	det := deber(e.m.VerOrden.Ejecutar(cd, campo.EntradaVerOrden{ID: o.ID}))(t)
	if det.Firma == nil || det.Firma.Nombre != "Ana López" || len(det.Materiales) != 2 || len(det.Equipos) != 1 || !det.RequiereFirma {
		t.Fatalf("detalle: %+v", det)
	}
}

func TestFallarReprogramarCancelarReabrir(t *testing.T) {
	e := nuevoEntorno()
	d, tec := despacho(t), tecnico(t)
	cd, ct := como(t, d, "despacho"), como(t, tec, "tecnico")
	o := programada(t, e, cd, "visita", tec)
	o = deber(e.m.Llegar.Ejecutar(ct, campo.EntradaPosicion{ID: o.ID, Version: o.Version}))(t)
	_, err := e.m.FallarOrden.Ejecutar(ct, campo.EntradaFallarOrden{ID: o.ID, Version: o.Version, MotivoFalla: "otro"})
	problema(t, err, 422, "validacion")
	o = deber(e.m.FallarOrden.Ejecutar(ct, campo.EntradaFallarOrden{ID: o.ID, Version: o.Version, MotivoFalla: "cliente_ausente", Detalle: "Nadie abrió"}))(t)
	if o.Estado != "fallida" || *o.MotivoFalla != "cliente_ausente" || *o.Resultado != "fallida" {
		t.Fatalf("fallida: %+v", o)
	}
	_, err = e.m.ReprogramarOrden.Ejecutar(cd, campo.EntradaProgramarOrden{ID: o.ID, Version: o.Version, Inicio: inicio.Add(24 * time.Hour)})
	problema(t, err, 422, "validacion") // motivo obligatorio
	o = deber(e.m.ReprogramarOrden.Ejecutar(cd, campo.EntradaProgramarOrden{ID: o.ID, Version: o.Version,
		Inicio: inicio.Add(24 * time.Hour), Motivo: ptr("El cliente pidió mañana")}))(t)
	if o.Estado != "programada" || o.Resultado != nil || o.FinalizadaEn != nil || o.EnSitioEn != nil {
		t.Fatalf("reprogramada: %+v", o)
	}
	o = deber(e.m.CancelarOrden.Ejecutar(cd, campo.EntradaMotivo{ID: o.ID, Version: o.Version, Motivo: "Cliente desistió"}))(t)
	if o.Estado != "cancelada" {
		t.Fatalf("cancelada: %+v", o)
	}
	_, err = e.m.Llegar.Ejecutar(ct, campo.EntradaPosicion{ID: o.ID, Version: o.Version})
	problema(t, err, 422, "transicion_invalida")
	// Notas en cualquier estado (evidencia).
	o = deber(e.m.AgregarNota.Ejecutar(ct, campo.EntradaAgregarNota{ID: o.ID, Texto: "Dejé aviso en la puerta"}))(t)
	o = deber(e.m.ReabrirOrden.Ejecutar(cd, campo.EntradaMotivo{ID: o.ID, Version: o.Version, Motivo: "Volvió a llamar"}))(t)
	if o.Estado != "pendiente" || o.TecnicoID == nil {
		t.Fatalf("reabierta: %+v", o)
	}
	estados := []string{}
	for _, ev := range eventosDe(t, o.ID) {
		if ev.Evento == "orden.estado_cambiada" {
			estados = append(estados, ev.Payload["a"].(string))
		}
	}
	if !slices.Equal(estados, []string{"programada", "en_sitio", "fallida", "programada", "cancelada", "pendiente"}) {
		t.Fatalf("estados emitidos: %v", estados)
	}
	// Asignar sin técnico la deja pendiente; un técnico inexistente o que no ve la orden → 422.
	_, err = e.m.AsignarOrden.Ejecutar(cd, campo.EntradaAsignarOrden{ID: o.ID, Version: o.Version, TecnicoID: ptr(int64(99999999))})
	problema(t, err, 422, "validacion")
	ventas := usuarioCon(t)
	_, err = e.m.AsignarOrden.Ejecutar(cd, campo.EntradaAsignarOrden{ID: o.ID, Version: o.Version, TecnicoID: &ventas})
	problema(t, err, 422, "validacion")
	_, err = e.m.AsignarOrden.Ejecutar(cd, campo.EntradaAsignarOrden{ID: o.ID, Version: o.Version, TecnicoID: o.TecnicoID})
	problema(t, err, 422, "sin_cambios")
}

func TestAlcanceYPermisos(t *testing.T) {
	e := nuevoEntorno()
	d, t1, t2 := despacho(t), tecnico(t), tecnico(t)
	cd := como(t, d, "despacho")
	o1 := programada(t, e, cd, "visita", t1)
	o2 := programada(t, e, cd, "visita", t2)

	// El técnico solo ve las suyas.
	c1 := como(t, t1, "tecnico")
	_, err := e.m.VerOrdenPropia.Ejecutar(c1, campo.EntradaVerOrden{ID: o2.ID})
	problema(t, err, 404, "no_encontrado")
	_, err = e.m.Llegar.Ejecutar(c1, campo.EntradaPosicion{ID: o2.ID, Version: o2.Version})
	problema(t, err, 404, "no_encontrado")
	mias := deber(e.m.ListarMisOrdenes.Ejecutar(c1, campo.EntradaListarOrdenes{ParamsPagina: nucleo.ParamsPagina{Limit: 200}}))(t)
	for _, o := range mias.Data {
		if o.TecnicoID == nil || *o.TecnicoID != t1 {
			t.Fatalf("el técnico vio una ajena: %+v", o)
		}
	}
	if !slices.ContainsFunc(mias.Data, func(o campo.Orden) bool { return o.ID == o1.ID }) {
		t.Fatal("el técnico ve la suya")
	}
	_, err = e.m.ListarOrdenes.Ejecutar(c1, campo.EntradaListarOrdenes{})
	problema(t, err, 403, "prohibido")

	// Despacho de la zona 3 (lectura de zona 3, cerrar solo zona 4): ve, pero no cierra.
	cz := conAlcance(t, d, map[string][]contexto.Alcance{
		"ordenes.ver": {{ZonaID: 3}}, "ordenes.cancelar": {{ZonaID: 4}}, "ordenes.crear": {{ZonaID: 4}},
	})
	deber(e.m.VerOrden.Ejecutar(cz, campo.EntradaVerOrden{ID: o1.ID}))(t)
	_, err = e.m.CancelarOrden.Ejecutar(cz, campo.EntradaMotivo{ID: o1.ID, Version: o1.Version, Motivo: "Duplicada"})
	problema(t, err, 403, "fuera_de_alcance")
	_, err = e.m.CrearOrden.Ejecutar(cz, campo.EntradaCrearOrden{Tipo: "visita", Direccion: ptr("x"), ZonaID: ptr(int64(3))})
	problema(t, err, 403, "fuera_de_alcance")
	otraZona := conAlcance(t, d, map[string][]contexto.Alcance{"ordenes.ver": {{ZonaID: 7}}})
	_, err = e.m.VerOrden.Ejecutar(otraZona, campo.EntradaVerOrden{ID: o1.ID})
	problema(t, err, 404, "no_encontrado")

	// Lectura ve pero no actúa.
	lec := usuarioCon(t)
	cl := como(t, lec, "lectura")
	deber(e.m.VerOrden.Ejecutar(cl, campo.EntradaVerOrden{ID: o2.ID}))(t)
	_, err = e.m.CancelarOrden.Ejecutar(cl, campo.EntradaMotivo{ID: o2.ID, Version: o2.Version, Motivo: "No"})
	problema(t, err, 403, "prohibido")

	// Validación de la entrada.
	_, err = e.m.CrearOrden.Ejecutar(cd, campo.EntradaCrearOrden{Tipo: "visita"})
	problema(t, err, 422, "validacion") // sin dirección ni instantánea
	_, err = e.m.CrearOrden.Ejecutar(cd, campo.EntradaCrearOrden{Tipo: "visita", Direccion: ptr("x"), ServicioID: ptr(int64(5))})
	problema(t, err, 422, "validacion") // servicio sin cliente
}

func TestOrdenDesdeTicketEIdempotencia(t *testing.T) {
	e := nuevoEntorno()
	d := despacho(t)
	cd := como(t, d, "despacho")
	sop := soporte.Nuevo(soporte.Config{})
	tk := deber(sop.CrearTicket.Ejecutar(cd, soporte.EntradaCrearTicket{Asunto: "Sin señal", ClienteID: ptr(int64(801)),
		ServicioID: ptr(int64(9801)), ZonaID: ptr(int64(3)), Contacto: &soporte.EntradaContacto{Nombre: ptr("Luis"), Telefono: ptr("5555")}}))(t)

	_, err := e.m.CrearOrden.Ejecutar(cd, campo.EntradaCrearOrden{Tipo: "visita", TicketID: &tk.ID, ClienteID: ptr(int64(1)), Direccion: ptr("x")})
	problema(t, err, 422, "no_coincide_con_ticket")
	_, err = e.m.CrearOrden.Ejecutar(cd, campo.EntradaCrearOrden{Tipo: "visita", TicketID: ptr(int64(99999999)), Direccion: ptr("x")})
	problema(t, err, 422, "ticket_invalido")

	entrada := campo.EntradaCrearOrden{Tipo: "visita", TicketID: &tk.ID, Direccion: ptr("Colonia El Carmen"), ClaveIdempotencia: "clave-" + pruebas.Unico()}
	o := deber(e.m.CrearOrden.Ejecutar(cd, entrada))(t)
	if *o.ClienteID != 801 || *o.ServicioID != 9801 || *o.ZonaID != 3 || *o.Contacto.Nombre != "Luis" || *o.TicketID != tk.ID {
		t.Fatalf("hereda del ticket: %+v", o)
	}
	otra := deber(e.m.CrearOrden.Ejecutar(como(t, d, "despacho"), entrada))(t)
	if otra.ID != o.ID {
		t.Fatal("la misma clave devuelve la misma orden")
	}
	if n := contar(t, "SELECT count(*) FROM campo.ordenes WHERE ticket_id = $1", tk.ID); n != 1 {
		t.Fatalf("órdenes del ticket: %d", n)
	}
	if n := contar(t, "SELECT count(*) FROM soporte.ticket_mensajes WHERE ticket_id = $1 AND NOT publico AND cuerpo LIKE '%'||$2||'%'", tk.ID, o.Numero); n != 1 {
		t.Fatalf("nota en el ticket: %d", n)
	}
	if p := ultimo(t, eventosDe(t, o.ID), "orden.creada"); p["ticket_id"] != float64(tk.ID) || p["tipo"] != "visita" {
		t.Fatalf("orden.creada: %v", p)
	}
	// Un técnico sin tickets.ver no crea desde un ticket.
	_, err = e.m.CrearOrden.Ejecutar(pruebas.ContextoUsuario(pruebas.Pool(t), d, nucleo.PermOrdenesCrear, nucleo.PermOrdenesVer),
		campo.EntradaCrearOrden{Tipo: "visita", TicketID: &tk.ID})
	problema(t, err, 422, "ticket_invalido")
}

func TestEmpresaDeLaOrden(t *testing.T) {
	registrarEmpresas(t, 61, 62)
	e := nuevoEntorno()
	d, tec, tecOtra := despacho(t), tecnico(t), tecnico(t)
	asignarEmpresa(t, d, 61)
	asignarEmpresa(t, tec, 61)
	asignarEmpresa(t, tecOtra, 62)
	c61 := pruebas.ContextoUsuarioEnEmpresa(pruebas.Pool(t), d, 61, permisosDe("despacho")...)
	c62 := pruebas.ContextoUsuarioEnEmpresa(pruebas.Pool(t), d, 62, permisosDe("despacho")...)

	// Con empresas registradas, la zona debe ser de la empresa (una sin empresa ya no sirve).
	_, err := e.m.CrearOrden.Ejecutar(c61, campo.EntradaCrearOrden{Tipo: "visita", Direccion: ptr("x"), ZonaID: ptr(int64(3))})
	problema(t, err, 422, "validacion")
	zonaDe(t, 61003, 61)
	o := deber(e.m.CrearOrden.Ejecutar(c61, campo.EntradaCrearOrden{Tipo: "visita", Direccion: ptr("Zona 10"), ZonaID: ptr(int64(61003))}))(t)
	if o.EmpresaID == nil || *o.EmpresaID != 61 {
		t.Fatalf("empresa: %+v", o.EmpresaID)
	}
	_, err = e.m.VerOrden.Ejecutar(c62, campo.EntradaVerOrden{ID: o.ID})
	problema(t, err, 404, "no_encontrado")
	_, err = e.m.CrearOrden.Ejecutar(c61, campo.EntradaCrearOrden{Tipo: "visita", Direccion: ptr("x"), EmpresaID: ptr(int64(62))})
	problema(t, err, 422, "empresa_no_coincide")
	_, err = e.m.ProgramarOrden.Ejecutar(c61, campo.EntradaProgramarOrden{ID: o.ID, Version: o.Version, TecnicoID: &tecOtra, Inicio: inicio})
	problema(t, err, 422, "tecnico_fuera_de_empresa")
	deber(e.m.ProgramarOrden.Ejecutar(c61, campo.EntradaProgramarOrden{ID: o.ID, Version: o.Version, TecnicoID: &tec, Inicio: inicio}))(t)
	if p := ultimo(t, eventosDe(t, o.ID), "orden.creada"); p["empresa_id"] != float64(61) {
		t.Fatalf("orden.creada con la empresa: %v", p)
	}
	if es := eventosDe(t, o.ID); es[0].Empresa == nil || *es[0].Empresa != 61 {
		t.Fatal("el sobre del evento lleva la empresa")
	}

	// Instantánea de Argos: cliente de otra empresa → 422; completa la dirección.
	pool := pruebas.Pool(t)
	if _, err := pool.Exec(context.Background(), `INSERT INTO integracion.argos_clientes (id, empresa_id, nombre, telefono, direccion, lat, lon, zona_id, sincronizado_en)
		VALUES (7701, 61, 'María', '4444', 'Mixco, zona 1', 14.63, -90.6, 5, now()), (7702, 62, 'Otro', '', 'x', null, null, null, now())`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM integracion.argos_clientes WHERE id IN (7701, 7702)")
	})
	_, err = e.m.CrearOrden.Ejecutar(c61, campo.EntradaCrearOrden{Tipo: "visita", ClienteID: ptr(int64(7702))})
	problema(t, err, 422, "empresa_no_coincide")
	o2 := deber(e.m.CrearOrden.Ejecutar(c61, campo.EntradaCrearOrden{Tipo: "visita", ClienteID: ptr(int64(7701))}))(t)
	if o2.Direccion != "Mixco, zona 1" || *o2.ZonaID != 5 || *o2.Contacto.Nombre != "María" || *o2.Lat != 14.63 {
		t.Fatalf("completada desde la instantánea: %+v", o2)
	}
	if n := contar(t, "SELECT count(*) FROM campo.ordenes WHERE empresa_id IS NULL AND id = $1", o2.ID); n != 0 {
		t.Fatal("con empresa activa no se guarda null")
	}
}

func TestRetiroConEquipoPerdido(t *testing.T) {
	e := nuevoEntorno()
	d, tec := despacho(t), tecnico(t)
	cd, ct := como(t, d, "despacho"), como(t, tec, "tecnico")
	o := programada(t, e, cd, "retiro", tec)
	o = deber(e.m.Llegar.Ejecutar(ct, campo.EntradaPosicion{ID: o.ID, Version: o.Version}))(t)
	completarChecklist(t, e, ct, o.ID)
	o = deber(e.m.CapturarFirma.Ejecutar(ct, campo.EntradaCapturarFirma{ID: o.ID, Nombre: "Pedro", SVG: "<svg><path/></svg>"}))(t)
	_, err := e.m.CerrarOrden.Ejecutar(ct, campo.EntradaCerrarOrden{ID: o.ID, Version: o.Version})
	if errs := problema(t, err, 422, "cierre_incompleto"); len(errs) != 1 || errs[0].Field != "equipos.retirar" {
		t.Fatalf("falta el equipo retirado: %+v", errs)
	}
	o = deber(e.m.RetirarEquipo.Ejecutar(ct, campo.EntradaEquipo{ID: o.ID, UnidadID: ptr(int64(55)), Condicion: ptr("perdido")}))(t)
	o = deber(e.m.CerrarOrden.Ejecutar(ct, campo.EntradaCerrarOrden{ID: o.ID, Version: o.Version}))(t)
	c := ultimo(t, eventosDe(t, o.ID), "orden.cerrada")
	eq := c["equipos"].([]any)[0].(map[string]any)
	if c["retirar_servicio"] != true || eq["condicion"] != "perdido" || eq["unidad_id"] != 55.0 || eq["accion"] != "retirar" {
		t.Fatalf("orden.cerrada de un retiro: %v", c)
	}
}
