package campo_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/HCDestroyer/iris/apps/api/internal/campo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
	"github.com/HCDestroyer/iris/apps/api/internal/soporte"
)

// tecnicoConTickets: un técnico (con agenda) que además ve tickets (rol lectura), para que el
// ticket se le pueda asignar.
func tecnicoConTickets(t *testing.T) int64 {
	id := usuarioCon(t, identidad.AsignacionRol{Rol: "tecnico"}, identidad.AsignacionRol{Rol: "lectura"})
	conAgenda(t, id)
	return id
}

// nuevaCuadrilla deja una cuadrilla activa sin empresa en el catálogo de agenda.
func nuevaCuadrilla(t *testing.T) int64 {
	t.Helper()
	var id int64
	if err := pruebas.Pool(t).QueryRow(context.Background(),
		"INSERT INTO agenda.cuadrillas (nombre) VALUES ('Cuadrilla ' || $1) RETURNING id", pruebas.Unico()).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// ticketDePrueba crea un ticket de la zona 3 con cliente y servicio de Argos (sin instantánea).
func ticketDePrueba(t *testing.T, sop *soporte.Modulo, ctx context.Context) soporte.Ticket {
	t.Helper()
	asegurarZona(t, 3)
	return deber(sop.CrearTicket.Ejecutar(ctx, soporte.EntradaCrearTicket{Asunto: "Sin internet desde ayer", ClienteID: ptr(int64(802)),
		ServicioID: ptr(int64(9802)), ZonaID: ptr(int64(3)), Contacto: &soporte.EntradaContacto{Nombre: ptr("Ana"), Telefono: ptr("5551")}}))(t)
}

// eventosDeTicket: eventos del outbox con ese ticket_id, en orden.
func eventosDeTicket(t *testing.T, ticketID int64) []evento {
	t.Helper()
	filas, err := pruebas.Pool(t).Query(context.Background(),
		"SELECT evento, empresa_id, payload FROM integracion.outbox WHERE (payload->>'ticket_id')::bigint = $1 ORDER BY id", ticketID)
	if err != nil {
		t.Fatal(err)
	}
	defer filas.Close()
	var salida []evento
	for filas.Next() {
		var e evento
		var crudo []byte
		if err := filas.Scan(&e.Evento, &e.Empresa, &crudo); err != nil {
			t.Fatal(err)
		}
		_ = json.Unmarshal(crudo, &e.Payload)
		salida = append(salida, e)
	}
	return salida
}

func TestCrearOrdenDesdeTicketAsignadaYProgramada(t *testing.T) {
	sop := soporte.Nuevo(soporte.Config{})
	e := nuevoEntorno(func(c *campo.Config) { c.Soporte = sop })
	d, tec := despacho(t), tecnicoConTickets(t)
	cd := como(t, d, "despacho")
	cuadrilla := nuevaCuadrilla(t)
	tk := ticketDePrueba(t, sop, cd)
	tk = deber(sop.AsignarTicket.Ejecutar(cd, soporte.EntradaAsignarTicket{ID: tk.ID, Version: tk.Version, UsuarioID: &tec, CuadrillaID: &cuadrilla}))(t)
	ordenesDelTicket := func() int {
		return contar(t, "SELECT count(*) FROM campo.ordenes WHERE ticket_id = $1", tk.ID)
	}

	// Sin técnico ni cuadrilla no se crea nada (el modal tampoco deja).
	_, err := e.m.CrearOrdenDeTicket.Ejecutar(cd, campo.EntradaCrearOrdenDeTicket{TicketID: tk.ID})
	errores := problema(t, err, 422, "validacion")
	if len(errores) != 2 || errores[0].Rule != "asignacion_requerida" || errores[1].Field != "cuadrillaId" {
		t.Fatalf("errores: %+v", errores)
	}
	// Con fecha pero sin técnico, tampoco.
	_, err = e.m.CrearOrdenDeTicket.Ejecutar(cd, campo.EntradaCrearOrdenDeTicket{TicketID: tk.ID, CuadrillaID: &cuadrilla, Inicio: ptr(inicio.Add(2 * time.Hour))})
	if errs := problema(t, err, 422, "validacion"); errs[0].Field != "tecnicoId" {
		t.Fatalf("errores: %+v", errs)
	}
	// Un técnico sin agenda: la cita falla y no queda orden a medias (todo o nada).
	sinAgenda := usuarioCon(t, identidad.AsignacionRol{Rol: "tecnico"})
	_, err = e.m.CrearOrdenDeTicket.Ejecutar(cd, campo.EntradaCrearOrdenDeTicket{TicketID: tk.ID, TecnicoID: &sinAgenda, Direccion: ptr("x"), Inicio: ptr(inicio.Add(2 * time.Hour))})
	problema(t, err, 422, "tecnico_sin_agenda")
	if n := ordenesDelTicket(); n != 0 {
		t.Fatalf("una cita rechazada no deja orden: %d", n)
	}
	// Sin ordenes.programar, con fecha: 403 del caso anidado, sin orden.
	sinProgramar := pruebas.ContextoUsuario(pruebas.Pool(t), d, nucleo.PermTicketsCrearOrden, nucleo.PermTicketsVer,
		nucleo.PermOrdenesCrear, nucleo.PermOrdenesAsignar, nucleo.PermOrdenesVer)
	_, err = e.m.CrearOrdenDeTicket.Ejecutar(sinProgramar, campo.EntradaCrearOrdenDeTicket{TicketID: tk.ID, TecnicoID: &tec, Direccion: ptr("x"), Inicio: ptr(inicio.Add(2 * time.Hour))})
	problema(t, err, 403, "prohibido")
	if n := ordenesDelTicket(); n != 0 {
		t.Fatalf("sin permiso no queda orden: %d", n)
	}

	// Un paso: visita del ticket, asignada al mismo técnico y cuadrilla, y programada.
	entrada := campo.EntradaCrearOrdenDeTicket{TicketID: tk.ID, TecnicoID: &tec, CuadrillaID: &cuadrilla,
		Direccion: ptr("Colonia Las Flores, casa 4"), Inicio: ptr(inicio.Add(2 * time.Hour)), ClaveIdempotencia: "clave-" + pruebas.Unico()}
	o := deber(e.m.CrearOrdenDeTicket.Ejecutar(cd, entrada))(t)
	if o.Tipo != "visita" || o.Estado != campo.EstadoProgramada || *o.TicketID != tk.ID || *o.TecnicoID != tec || *o.CuadrillaID != cuadrilla ||
		*o.ClienteID != 802 || *o.ServicioID != 9802 || o.Descripcion != tk.Asunto || *o.Contacto.Nombre != "Ana" || o.ProgramadaInicio == nil {
		t.Fatalf("orden: %+v", o)
	}
	if got := nombres(eventosDe(t, o.ID)); strings.Join(got, ",") != "orden.creada,orden.asignada,orden.programada,orden.estado_cambiada" {
		t.Fatalf("eventos: %v", got)
	}
	if p := ultimo(t, eventosDe(t, o.ID), "orden.creada"); p["ticket_id"] != float64(tk.ID) {
		t.Fatalf("orden.creada: %v", p)
	}
	if p := ultimo(t, eventosDe(t, o.ID), "orden.asignada"); p["tecnico_id"] != float64(tec) || p["cuadrilla_id"] != float64(cuadrilla) {
		t.Fatalf("orden.asignada: %v", p)
	}
	// Auditoría: la acción compuesta y cada caso anidado.
	if n := contar(t, "SELECT count(*) FROM auditoria.acciones WHERE accion = 'tickets.crear_orden' AND entidad_id = $1::bigint::text AND error IS NULL", tk.ID); n != 1 {
		t.Fatalf("auditoría tickets.crear_orden: %d", n)
	}
	for _, a := range []string{"ordenes.crear", "ordenes.asignar", "ordenes.programar"} {
		if n := contar(t, "SELECT count(*) FROM auditoria.acciones WHERE accion = $1 AND entidad_id = $2::bigint::text AND error IS NULL", a, o.ID); n != 1 {
			t.Fatalf("auditoría %s: %d", a, n)
		}
	}
	if n := contar(t, "SELECT count(*) FROM soporte.ticket_mensajes WHERE ticket_id = $1 AND NOT publico AND cuerpo LIKE '%'||$2||'%'", tk.ID, o.Numero); n != 1 {
		t.Fatalf("nota en el ticket: %d", n)
	}
	// La misma petición repetida devuelve la misma orden.
	otra := deber(e.m.CrearOrdenDeTicket.Ejecutar(como(t, d, "despacho"), entrada))(t)
	if otra.ID != o.ID || ordenesDelTicket() != 1 {
		t.Fatalf("idempotencia: %d vs %d (%d órdenes)", otra.ID, o.ID, ordenesDelTicket())
	}

	// Solo con cuadrilla: queda pendiente, sin técnico, y avisa orden.asignada igual. La cuadrilla
	// no se hereda en silencio: sin cuadrillaId la orden queda sin cuadrilla.
	solo := deber(e.m.CrearOrdenDeTicket.Ejecutar(cd, campo.EntradaCrearOrdenDeTicket{TicketID: tk.ID, Tipo: "mantenimiento", CuadrillaID: &cuadrilla,
		Direccion: ptr("Colonia Las Flores, casa 4"), Descripcion: "Revisar la caja"}))(t)
	if solo.Tipo != "mantenimiento" || solo.Estado != campo.EstadoPendiente || solo.TecnicoID != nil || *solo.CuadrillaID != cuadrilla || solo.Descripcion != "Revisar la caja" {
		t.Fatalf("solo cuadrilla: %+v", solo)
	}
	if p := ultimo(t, eventosDe(t, solo.ID), "orden.asignada"); p["tecnico_id"] != nil || p["cuadrilla_id"] != float64(cuadrilla) {
		t.Fatalf("orden.asignada (cuadrilla): %v", p)
	}
	sinCuadrilla := deber(e.m.CrearOrdenDeTicket.Ejecutar(cd, campo.EntradaCrearOrdenDeTicket{TicketID: tk.ID, TecnicoID: &tec, Direccion: ptr("x")}))(t)
	if sinCuadrilla.CuadrillaID != nil || *sinCuadrilla.TecnicoID != tec {
		t.Fatalf("sin cuadrilla: %+v", sinCuadrilla)
	}

	// Un ticket que el actor no ve: 404; uno cerrado: 422.
	_, err = e.m.CrearOrdenDeTicket.Ejecutar(cd, campo.EntradaCrearOrdenDeTicket{TicketID: 99999999, TecnicoID: &tec})
	problema(t, err, 404, "no_encontrado")
	tk = deber(sop.VerTicket.Ejecutar(cd, soporte.EntradaVerTicket{ID: tk.ID}))(t)
	deber(sop.CerrarTicket.Ejecutar(como(t, usuarioCon(t, identidad.AsignacionRol{Rol: "supervisor"}), "supervisor"),
		soporte.EntradaCerrarTicket{ID: tk.ID, Version: tk.Version, Motivo: ptr("Duplicado del reporte")}))(t)
	_, err = e.m.CrearOrdenDeTicket.Ejecutar(cd, campo.EntradaCrearOrdenDeTicket{TicketID: tk.ID, TecnicoID: &tec})
	problema(t, err, 422, "ticket_cerrado")
}

func TestIgualarAsignacionTicketOrden(t *testing.T) {
	sop := soporte.Nuevo(soporte.Config{})
	e := nuevoEntorno(func(c *campo.Config) { c.Soporte = sop })
	d, tec := despacho(t), tecnicoConTickets(t)
	cd := como(t, d, "despacho")
	cuadrilla := nuevaCuadrilla(t)

	// El ticket tiene técnico y cuadrilla; su orden (creada a la antigua) solo la cuadrilla.
	tk := ticketDePrueba(t, sop, cd)
	tk = deber(sop.AsignarTicket.Ejecutar(cd, soporte.EntradaAsignarTicket{ID: tk.ID, Version: tk.Version, UsuarioID: &tec, CuadrillaID: &cuadrilla}))(t)
	o := deber(e.m.CrearOrden.Ejecutar(cd, campo.EntradaCrearOrden{Tipo: "visita", TicketID: &tk.ID, Direccion: ptr("x")}))(t)
	if o.TecnicoID != nil || *o.CuadrillaID != cuadrilla {
		t.Fatalf("orden: %+v", o)
	}
	// Versión vieja de cualquiera de los dos: 409.
	_, err := e.m.IgualarAsignacion.Ejecutar(cd, campo.EntradaIgualarAsignacion{ID: o.ID, Desde: "ticket", Version: o.Version, VersionTicket: tk.Version - 1})
	problema(t, err, 409, "conflicto_version")
	_, err = e.m.IgualarAsignacion.Ejecutar(cd, campo.EntradaIgualarAsignacion{ID: o.ID, Desde: "ticket", Version: o.Version + 1, VersionTicket: tk.Version})
	problema(t, err, 409, "conflicto_version")
	// Sin igualar, la orden de un ticket de técnico distinto: el botón lo pide.
	r := deber(e.m.IgualarAsignacion.Ejecutar(cd, campo.EntradaIgualarAsignacion{ID: o.ID, Desde: "ticket", Version: o.Version, VersionTicket: tk.Version}))(t)
	if *r.Orden.TecnicoID != tec || *r.Orden.CuadrillaID != cuadrilla || r.Ticket.ID != tk.ID || r.Ticket.Version != tk.Version {
		t.Fatalf("igualada desde el ticket: %+v", r)
	}
	if p := ultimo(t, eventosDe(t, o.ID), "orden.asignada"); p["tecnico_id"] != float64(tec) {
		t.Fatalf("orden.asignada: %v", p)
	}
	if n := contar(t, "SELECT count(*) FROM auditoria.acciones WHERE accion = 'ordenes.igualar_asignacion' AND entidad_id = $1::bigint::text AND error IS NULL", o.ID); n != 1 {
		t.Fatalf("auditoría: %d", n)
	}
	_, err = e.m.IgualarAsignacion.Ejecutar(cd, campo.EntradaIgualarAsignacion{ID: o.ID, Desde: "ticket", Version: r.Orden.Version, VersionTicket: tk.Version})
	problema(t, err, 422, "sin_cambios")

	// Al revés: el ticket sin asignar toma el técnico y la cuadrilla de su orden.
	tk2 := ticketDePrueba(t, sop, cd)
	o2 := deber(e.m.CrearOrden.Ejecutar(cd, campo.EntradaCrearOrden{Tipo: "visita", TicketID: &tk2.ID, Direccion: ptr("x")}))(t)
	_, err = e.m.IgualarAsignacion.Ejecutar(cd, campo.EntradaIgualarAsignacion{ID: o2.ID, Desde: "orden", Version: o2.Version, VersionTicket: tk2.Version})
	problema(t, err, 422, "sin_cambios") // ninguno de los dos tiene asignación
	o2 = deber(e.m.AsignarOrden.Ejecutar(cd, campo.EntradaAsignarOrden{ID: o2.ID, Version: o2.Version, TecnicoID: &tec, CuadrillaID: &cuadrilla}))(t)
	_, err = e.m.IgualarAsignacion.Ejecutar(cd, campo.EntradaIgualarAsignacion{ID: o2.ID, Desde: "ticket", Version: o2.Version, VersionTicket: tk2.Version})
	problema(t, err, 422, "ticket_sin_asignar")
	// Sin tickets.asignar no se toca el ticket (el caso anidado lo exige).
	sinAsignarTicket := pruebas.ContextoUsuario(pruebas.Pool(t), d, nucleo.PermOrdenesIgualarAsignacion, nucleo.PermOrdenesVer, nucleo.PermTicketsVer)
	_, err = e.m.IgualarAsignacion.Ejecutar(sinAsignarTicket, campo.EntradaIgualarAsignacion{ID: o2.ID, Desde: "orden", Version: o2.Version, VersionTicket: tk2.Version})
	problema(t, err, 403, "prohibido")
	r = deber(e.m.IgualarAsignacion.Ejecutar(cd, campo.EntradaIgualarAsignacion{ID: o2.ID, Desde: "orden", Version: o2.Version, VersionTicket: tk2.Version}))(t)
	if *r.Ticket.AsignadoA != tec || *r.Ticket.CuadrillaID != cuadrilla || r.Ticket.Estado != soporte.EstadoAbierto || r.Orden.Version != o2.Version {
		t.Fatalf("igualado desde la orden: %+v", r)
	}
	if p := ultimo(t, eventosDeTicket(t, tk2.ID), "ticket.asignado"); p["usuario_id"] != float64(tec) || p["cuadrilla_id"] != float64(cuadrilla) {
		t.Fatalf("ticket.asignado: %v", p)
	}

	// Una orden sin ticket no tiene con qué igualar.
	suelta := crearOrden(t, e, cd, "visita")
	_, err = e.m.IgualarAsignacion.Ejecutar(cd, campo.EntradaIgualarAsignacion{ID: suelta.ID, Desde: "orden", Version: suelta.Version, VersionTicket: 1})
	problema(t, err, 422, "sin_ticket")
}

func TestCierreDeOrdenProponeResolverTicket(t *testing.T) {
	sop := soporte.Nuevo(soporte.Config{})
	e := nuevoEntorno(func(c *campo.Config) { c.Soporte = sop })
	d, tec := despacho(t), tecnico(t)
	cd, ct := como(t, d, "despacho"), como(t, tec, "tecnico")
	tk := ticketDePrueba(t, sop, cd)
	nueva := func(hora int) campo.Orden {
		t.Helper()
		return deber(e.m.CrearOrdenDeTicket.Ejecutar(cd, campo.EntradaCrearOrdenDeTicket{TicketID: tk.ID, TecnicoID: &tec,
			Direccion: ptr("Colonia Las Flores"), Inicio: ptr(inicio.Add(time.Duration(hora) * time.Hour))}))(t)
	}
	cerrar := func(o campo.Orden) {
		t.Helper()
		o = deber(e.m.Llegar.Ejecutar(ct, campo.EntradaPosicion{ID: o.ID, Version: o.Version}))(t)
		completarChecklist(t, e, ct, o.ID)
		firma := uuid.Must(uuid.NewV7())
		deber(e.m.CapturarFirma.Ejecutar(ct, campo.EntradaCapturarFirma{ID: o.ID, Nombre: "Cliente",
			SVG: `<svg xmlns="http://www.w3.org/2000/svg"><path d="M0 0L10 10"/></svg>`, AdjuntoID: &firma}))(t)
		o = deber(e.m.VerOrden.Ejecutar(cd, campo.EntradaVerOrden{ID: o.ID}))(t).Orden
		deber(e.m.CerrarOrden.Ejecutar(ct, campo.EntradaCerrarOrden{ID: o.ID, Version: o.Version}))(t)
	}
	ver := func() soporte.Ticket {
		return deber(sop.VerTicket.Ejecutar(cd, soporte.EntradaVerTicket{ID: tk.ID}))(t)
	}
	nota := func(orden campo.Orden, contiene string) {
		t.Helper()
		if n := contar(t, "SELECT count(*) FROM soporte.ticket_mensajes WHERE ticket_id = $1 AND NOT publico AND cuerpo LIKE '%'||$2||' se cerró%'||$3||'%'",
			tk.ID, orden.Numero, contiene); n != 1 {
			t.Fatalf("nota de cierre de %s con %q: %d", orden.Numero, contiene, n)
		}
	}

	o1, o2 := nueva(1), nueva(3)
	// Cierra la primera con otra abierta: nota, sin propuesta.
	cerrar(o1)
	nota(o1, "Siguen abiertas: "+o2.Numero)
	if v := ver(); v.Propuesta != nil || v.Estado != soporte.EstadoNuevo {
		t.Fatalf("con otra orden abierta no se propone: %+v", v.Propuesta)
	}
	// Cierra la última: nota y propuesta de resolver (origen orden); el ticket no cambia de estado.
	cerrar(o2)
	nota(o2, "Propuesta: resolver")
	v := ver()
	if v.Propuesta == nil || v.Propuesta.Accion != "resolver" || v.Propuesta.Origen != soporte.PropuestaDeOrden ||
		v.Propuesta.OrdenID == nil || *v.Propuesta.OrdenID != o2.ID || v.Estado != soporte.EstadoNuevo {
		t.Fatalf("propuesta: %+v (estado %s)", v.Propuesta, v.Estado)
	}
	p := ultimo(t, eventosDeTicket(t, tk.ID), "ticket.resolucion_propuesta")
	if p["origen"] != "orden" || p["orden_id"] != float64(o2.ID) || p["accion"] != "resolver" || p["entrante_id"] != nil {
		t.Fatalf("ticket.resolucion_propuesta: %v", p)
	}
	if n := contar(t, "SELECT count(*) FROM integracion.outbox WHERE evento = 'ticket.resolucion_propuesta' AND (payload->>'ticket_id')::bigint = $1", tk.ID); n != 1 {
		t.Fatalf("una sola propuesta: %d", n)
	}
	// Un agente confirma: al resolver, la propuesta se descarta.
	sup := como(t, usuarioCon(t, identidad.AsignacionRol{Rol: "supervisor"}), "supervisor")
	r := deber(sop.ResolverTicket.Ejecutar(sup, soporte.EntradaResolverTicket{ID: tk.ID, Version: v.Version, Solucion: "Se cambió el conector en sitio"}))(t)
	if r.Propuesta != nil || r.Estado != soporte.EstadoResuelto {
		t.Fatalf("resuelto: %+v", r)
	}

	// Ticket ya resuelto: una orden que se cierra después solo deja la nota.
	o3 := deber(e.m.CrearOrden.Ejecutar(cd, campo.EntradaCrearOrden{Tipo: "visita", TicketID: &tk.ID, Direccion: ptr("x")}))(t)
	o3 = deber(e.m.ProgramarOrden.Ejecutar(cd, campo.EntradaProgramarOrden{ID: o3.ID, Version: o3.Version, TecnicoID: &tec, Inicio: inicio.Add(5 * time.Hour)}))(t)
	cerrar(o3)
	nota(o3, "")
	if v := ver(); v.Propuesta != nil || v.Estado != soporte.EstadoResuelto {
		t.Fatalf("resuelto: sin propuesta nueva: %+v", v.Propuesta)
	}
}

func TestHTTPTicketYOrdenDeLaMano(t *testing.T) {
	base := nuevoServidor(t)
	d := entrar(t, base, "despacho")
	tec := entrar(t, base, "tecnico")

	r := d.pedir(http.MethodPost, "/api/v1/tickets", map[string]any{"asunto": "Lento por las noches", "zonaId": 3}, "Idempotency-Key", "tk-"+pruebas.Unico())
	if r.status != http.StatusCreated {
		t.Fatalf("ticket: %d %s", r.status, r.crudo)
	}
	ticket := int64(r.cuerpo["id"].(float64))
	ruta := fmt.Sprintf("/api/v1/tickets/%d/crear-orden", ticket)

	// Sin técnico ni cuadrilla: 422 con los dos campos señalados.
	r = d.pedir(http.MethodPost, ruta, map[string]any{"direccion": "Zona 1"}, "Idempotency-Key", "co-"+pruebas.Unico())
	r.problema(t, 422, "validacion")
	if errs, _ := r.cuerpo["errors"].([]any); len(errs) != 2 {
		t.Fatalf("errores: %s", r.crudo)
	}
	// Con técnico y fecha: 201, programada; la misma clave repite la respuesta.
	clave := "co-" + pruebas.Unico()
	cuerpo := map[string]any{"direccion": "Zona 1", "tecnicoId": tec.id, "inicio": "2026-11-20T16:00:00Z"}
	r = d.pedir(http.MethodPost, ruta, cuerpo, "Idempotency-Key", clave)
	if r.status != http.StatusCreated || r.cuerpo["estado"] != "programada" || r.cuerpo["tipo"] != "visita" || r.cuerpo["ticketId"] != float64(ticket) {
		t.Fatalf("crear-orden: %d %s", r.status, r.crudo)
	}
	orden := int64(r.cuerpo["id"].(float64))
	if otra := d.pedir(http.MethodPost, ruta, cuerpo, "Idempotency-Key", clave); otra.status != http.StatusCreated || otra.cuerpo["id"] != float64(orden) {
		t.Fatalf("repetida: %d %s", otra.status, otra.crudo)
	}
	// El técnico no tiene el permiso de la acción compuesta.
	tec.pedir(http.MethodPost, ruta, cuerpo, "Idempotency-Key", "co-"+pruebas.Unico()).problema(t, 403, "prohibido")

	// Igualar el ticket a la orden: el técnico base no ve tickets (tickets.ver), así que soporte
	// lo rechaza con su validación (nunca se asigna a medias).
	tk := d.pedir(http.MethodGet, fmt.Sprintf("/api/v1/tickets/%d", ticket), nil)
	r = d.pedir(http.MethodPost, fmt.Sprintf("/api/v1/ordenes/%d/igualar-asignacion", orden),
		map[string]any{"desde": "orden", "version": int(r.cuerpo["version"].(float64)), "versionTicket": tk.cuerpo["version"]},
		"Idempotency-Key", "ig-"+pruebas.Unico())
	r.problema(t, 422, "validacion")
	if !strings.Contains(string(r.crudo), "usuarioId") {
		t.Fatalf("igualar: %s", r.crudo)
	}
}

// lista: la orden programada del técnico, en sitio con checklist y firma, lista para cerrar.
func lista(t *testing.T, e entorno, cd, ct context.Context, o campo.Orden) campo.Orden {
	t.Helper()
	o = deber(e.m.Llegar.Ejecutar(ct, campo.EntradaPosicion{ID: o.ID, Version: o.Version}))(t)
	completarChecklist(t, e, ct, o.ID)
	firma := uuid.Must(uuid.NewV7())
	deber(e.m.CapturarFirma.Ejecutar(ct, campo.EntradaCapturarFirma{ID: o.ID, Nombre: "Cliente",
		SVG: `<svg xmlns="http://www.w3.org/2000/svg"><path d="M0 0L10 10"/></svg>`, AdjuntoID: &firma}))(t)
	return deber(e.m.VerOrden.Ejecutar(cd, campo.EntradaVerOrden{ID: o.ID}))(t).Orden
}

// conTicketBloqueado tiene el ticket con FOR UPDATE en otra transacción mientras corren las dos
// acciones: espera a que las dos queden esperando ese bloqueo y lo suelta. Así se reproduce el
// peor caso (las dos ya hicieron todo lo anterior al ticket) sin sleeps en el código.
func conTicketBloqueado(t *testing.T, ticketID int64, acciones ...func() error) []error {
	t.Helper()
	ctx := context.Background()
	pool := pruebas.Pool(t)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "SELECT 1 FROM soporte.tickets WHERE id = $1 FOR UPDATE", ticketID); err != nil {
		t.Fatal(err)
	}
	errores := make([]error, len(acciones))
	var wg sync.WaitGroup
	for i, a := range acciones {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errores[i] = a()
		}()
	}
	limite := time.Now().Add(15 * time.Second)
	for {
		var n int
		// Esperando el bloqueo del ticket: la primera, detrás de esta transacción; la segunda,
		// detrás de la primera.
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname = current_database()
			AND cardinality(pg_blocking_pids(pid)) > 0 AND query ILIKE '%soporte.tickets%'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n >= len(acciones) {
			break
		}
		if time.Now().After(limite) {
			filas, _ := pool.Query(ctx, `SELECT query FROM pg_stat_activity WHERE datname = current_database() AND cardinality(pg_blocking_pids(pid)) > 0`)
			for filas.Next() {
				var q string
				_ = filas.Scan(&q)
				t.Log(q)
			}
			filas.Close()
			t.Fatalf("solo %d de %d acciones llegaron al bloqueo del ticket", n, len(acciones))
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	return errores
}

// Dos órdenes del mismo ticket que se cierran a la vez: una de las dos propone resolver (antes
// las dos contaban la otra como abierta y el ticket se quedaba sin propuesta).
func TestCierresSimultaneosDelTicketProponenUnaVez(t *testing.T) {
	sop := soporte.Nuevo(soporte.Config{})
	e := nuevoEntorno(func(c *campo.Config) { c.Soporte = sop })
	// Dos técnicos: los cierres de un mismo técnico ya se serializan (candado del feed de cambios).
	d, tecs := despacho(t), []int64{tecnico(t), tecnico(t)}
	cd := como(t, d, "despacho")
	tk := ticketDePrueba(t, sop, cd)
	var ordenes []campo.Orden
	for _, tec := range tecs {
		o := deber(e.m.CrearOrdenDeTicket.Ejecutar(cd, campo.EntradaCrearOrdenDeTicket{TicketID: tk.ID, TecnicoID: &tec,
			Direccion: ptr("Colonia Las Flores"), Inicio: ptr(inicio.Add(time.Hour))}))(t)
		ordenes = append(ordenes, lista(t, e, cd, como(t, tec, "tecnico"), o))
	}
	errores := conTicketBloqueado(t, tk.ID,
		func() error {
			_, err := e.m.CerrarOrden.Ejecutar(como(t, tecs[0], "tecnico"), campo.EntradaCerrarOrden{ID: ordenes[0].ID, Version: ordenes[0].Version})
			return err
		},
		func() error {
			_, err := e.m.CerrarOrden.Ejecutar(como(t, tecs[1], "tecnico"), campo.EntradaCerrarOrden{ID: ordenes[1].ID, Version: ordenes[1].Version})
			return err
		})
	for _, err := range errores {
		if err != nil {
			t.Fatalf("cierres: %v", errores)
		}
	}
	v := deber(sop.VerTicket.Ejecutar(cd, soporte.EntradaVerTicket{ID: tk.ID}))(t)
	if v.Propuesta == nil || v.Propuesta.Origen != soporte.PropuestaDeOrden {
		t.Fatalf("el último cierre propone: %+v", v.Propuesta)
	}
	if n := contar(t, "SELECT count(*) FROM integracion.outbox WHERE evento = 'ticket.resolucion_propuesta' AND (payload->>'ticket_id')::bigint = $1", tk.ID); n != 1 {
		t.Fatalf("propuestas: %d", n)
	}
}

// Dos igualaciones a la vez sobre el mismo ticket (dos de sus órdenes): ni deadlock ni 500; la
// segunda ve la versión nueva del ticket y responde 409.
func TestIgualarSimultaneoSobreElMismoTicket(t *testing.T) {
	sop := soporte.Nuevo(soporte.Config{})
	e := nuevoEntorno(func(c *campo.Config) { c.Soporte = sop })
	d, tec := despacho(t), tecnicoConTickets(t)
	cd := como(t, d, "despacho")
	cuadrilla := nuevaCuadrilla(t)
	tk := ticketDePrueba(t, sop, cd)
	o1 := deber(e.m.CrearOrdenDeTicket.Ejecutar(cd, campo.EntradaCrearOrdenDeTicket{TicketID: tk.ID, TecnicoID: &tec, Direccion: ptr("x")}))(t)
	o2 := deber(e.m.CrearOrdenDeTicket.Ejecutar(cd, campo.EntradaCrearOrdenDeTicket{TicketID: tk.ID, CuadrillaID: &cuadrilla, Direccion: ptr("x")}))(t)
	igualar := func(o campo.Orden) func() error {
		return func() error {
			_, err := e.m.IgualarAsignacion.Ejecutar(como(t, d, "despacho"), campo.EntradaIgualarAsignacion{ID: o.ID, Desde: "orden", Version: o.Version, VersionTicket: tk.Version})
			return err
		}
	}
	errores := conTicketBloqueado(t, tk.ID, igualar(o1), igualar(o2))
	ok, conflictos := 0, 0
	for _, err := range errores {
		var p *nucleo.Problema
		switch {
		case err == nil:
			ok++
		case errors.As(err, &p) && p.Status == 409:
			conflictos++
		default:
			t.Fatalf("igualar a la vez: %v", errores)
		}
	}
	if ok != 1 || conflictos != 1 {
		t.Fatalf("una gana y la otra 409: %v", errores)
	}
}

// La propuesta por cierre no pisa la de Horus, y se descarta cuando el ticket vuelve a tener una
// orden abierta (nueva o reabierta).
func TestPropuestaDeOrdenConHorusYOrdenesNuevas(t *testing.T) {
	sop := soporte.Nuevo(soporte.Config{})
	e := nuevoEntorno(func(c *campo.Config) { c.Soporte = sop })
	d, tec := despacho(t), tecnico(t)
	cd, ct := como(t, d, "despacho"), como(t, tec, "tecnico")
	tk := ticketDePrueba(t, sop, cd)
	hora := 0
	nueva := func() campo.Orden {
		t.Helper()
		hora += 2
		return deber(e.m.CrearOrdenDeTicket.Ejecutar(cd, campo.EntradaCrearOrdenDeTicket{TicketID: tk.ID, TecnicoID: &tec,
			Direccion: ptr("x"), Inicio: ptr(inicio.Add(time.Duration(hora) * time.Hour))}))(t)
	}
	cerrar := func(o campo.Orden) {
		t.Helper()
		o = lista(t, e, cd, ct, o)
		deber(e.m.CerrarOrden.Ejecutar(ct, campo.EntradaCerrarOrden{ID: o.ID, Version: o.Version}))(t)
	}
	ver := func() soporte.Ticket {
		return deber(sop.VerTicket.Ejecutar(cd, soporte.EntradaVerTicket{ID: tk.ID}))(t)
	}
	propuestas := func() int {
		return contar(t, "SELECT count(*) FROM integracion.outbox WHERE evento = 'ticket.resolucion_propuesta' AND (payload->>'ticket_id')::bigint = $1", tk.ID)
	}

	// Una propuesta de Horus escrita por la versión anterior (sin origen; la restricción lo tolera
	// durante el despliegue): el cierre deja la nota y no la pisa.
	if _, err := pruebas.Pool(t).Exec(context.Background(),
		"UPDATE soporte.tickets SET propuesta = 'resolver', propuesta_en = now(), propuesta_motivo = 'El router volvió' WHERE id = $1", tk.ID); err != nil {
		t.Fatal(err)
	}
	cerrar(nueva())
	if v := ver(); v.Propuesta == nil || v.Propuesta.Origen != soporte.PropuestaDeHorus || *v.Propuesta.Motivo != "El router volvió" || propuestas() != 0 {
		t.Fatalf("la de Horus sigue: %+v (%d eventos)", v.Propuesta, propuestas())
	}
	if n := contar(t, "SELECT count(*) FROM soporte.ticket_mensajes WHERE ticket_id = $1 AND cuerpo LIKE '%Ya hay una propuesta de Horus%'", tk.ID); n != 1 {
		t.Fatalf("nota: %d", n)
	}
	// Sin la de Horus, el cierre propone; una orden nueva del ticket la descarta.
	if _, err := pruebas.Pool(t).Exec(context.Background(),
		"UPDATE soporte.tickets SET propuesta = NULL, propuesta_en = NULL, propuesta_motivo = NULL WHERE id = $1", tk.ID); err != nil {
		t.Fatal(err)
	}
	cerrar(nueva())
	if v := ver(); v.Propuesta == nil || v.Propuesta.Origen != soporte.PropuestaDeOrden {
		t.Fatalf("propuesta por cierre: %+v", v.Propuesta)
	}
	otra := deber(e.m.CrearOrden.Ejecutar(cd, campo.EntradaCrearOrden{Tipo: "visita", TicketID: &tk.ID, Direccion: ptr("x")}))(t)
	if v := ver(); v.Propuesta != nil {
		t.Fatalf("una orden nueva descarta la propuesta: %+v", v.Propuesta)
	}
	if n := contar(t, "SELECT count(*) FROM soporte.ticket_mensajes WHERE ticket_id = $1 AND cuerpo LIKE '%Se descarta la propuesta de resolver: la orden '||$2||'%'", tk.ID, otra.Numero); n != 1 {
		t.Fatalf("nota de descarte: %d", n)
	}
	// La cancela, cierra otra (propone) y reabre la cancelada: se descarta otra vez.
	otra = deber(e.m.CancelarOrden.Ejecutar(cd, campo.EntradaMotivo{ID: otra.ID, Version: otra.Version, Motivo: "Duplicada"}))(t)
	cerrar(nueva())
	if v := ver(); v.Propuesta == nil {
		t.Fatal("la cancelada no cuenta como abierta")
	}
	deber(e.m.ReabrirOrden.Ejecutar(cd, campo.EntradaMotivo{ID: otra.ID, Version: otra.Version, Motivo: "El cliente sigue sin señal"}))(t)
	if v := ver(); v.Propuesta != nil {
		t.Fatalf("reabrir una orden descarta la propuesta: %+v", v.Propuesta)
	}
}

func TestCrearOrdenDeTicketPermisoYClave(t *testing.T) {
	sop := soporte.Nuevo(soporte.Config{})
	e := nuevoEntorno(func(c *campo.Config) { c.Soporte = sop })
	d := despacho(t)
	cd := como(t, d, "despacho")
	cuadrilla := nuevaCuadrilla(t)
	tk := ticketDePrueba(t, sop, cd)

	// Solo con cuadrilla también es asignar: sin ordenes.asignar, 403 y sin orden.
	sinAsignar := pruebas.ContextoUsuario(pruebas.Pool(t), d, nucleo.PermTicketsCrearOrden, nucleo.PermTicketsVer, nucleo.PermOrdenesCrear, nucleo.PermOrdenesVer)
	_, err := e.m.CrearOrdenDeTicket.Ejecutar(sinAsignar, campo.EntradaCrearOrdenDeTicket{TicketID: tk.ID, CuadrillaID: &cuadrilla, Direccion: ptr("x")})
	problema(t, err, 403, "prohibido")
	if n := contar(t, "SELECT count(*) FROM campo.ordenes WHERE ticket_id = $1", tk.ID); n != 0 {
		t.Fatalf("sin permiso no queda orden: %d", n)
	}

	// La misma clave con otro ticket no devuelve la orden del primero.
	clave := "clave-" + pruebas.Unico()
	deber(e.m.CrearOrdenDeTicket.Ejecutar(cd, campo.EntradaCrearOrdenDeTicket{TicketID: tk.ID, CuadrillaID: &cuadrilla, Direccion: ptr("x"), ClaveIdempotencia: clave}))(t)
	otro := ticketDePrueba(t, sop, cd)
	_, err = e.m.CrearOrdenDeTicket.Ejecutar(cd, campo.EntradaCrearOrdenDeTicket{TicketID: otro.ID, CuadrillaID: &cuadrilla, Direccion: ptr("x"), ClaveIdempotencia: clave})
	problema(t, err, 422, "idempotencia_reutilizada")
}

// escalonadas: con el bloqueo de `sqlBloqueo` tomado en otra transacción, arranca las acciones
// una por una (cada una cuando la anterior ya espera un bloqueo) y lo suelta. Reproduce un
// orden de llegada exacto sin sleeps en el código.
func escalonadas(t *testing.T, sqlBloqueo string, arg any, acciones ...func() error) []error {
	t.Helper()
	ctx := context.Background()
	pool := pruebas.Pool(t)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, sqlBloqueo, arg); err != nil {
		t.Fatal(err)
	}
	esperando := func(n int) {
		t.Helper()
		limite := time.Now().Add(15 * time.Second)
		for {
			var c int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname = current_database()
				AND cardinality(pg_blocking_pids(pid)) > 0`).Scan(&c); err != nil {
				t.Fatal(err)
			}
			if c >= n {
				return
			}
			if time.Now().After(limite) {
				t.Fatalf("solo %d de %d acciones esperan un bloqueo", c, n)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	errores := make([]error, len(acciones))
	var wg sync.WaitGroup
	for i, a := range acciones {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errores[i] = a()
		}()
		esperando(i + 1)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	return errores
}

// Cerrar una orden y crear otra del mismo ticket para el mismo técnico, a la vez: el cierre
// tomaba orden → técnico → ticket y crear-orden ticket → técnico (deadlock, 500). Ahora el cierre
// toma el ticket antes de guardar: orden → ticket → técnico en los dos.
func TestCerrarYCrearOrdenDelTicketSinDeadlock(t *testing.T) {
	sop := soporte.Nuevo(soporte.Config{})
	e := nuevoEntorno(func(c *campo.Config) { c.Soporte = sop })
	d, tec := despacho(t), tecnico(t)
	cd, ct := como(t, d, "despacho"), como(t, tec, "tecnico")
	tk := ticketDePrueba(t, sop, cd)
	o := deber(e.m.CrearOrdenDeTicket.Ejecutar(cd, campo.EntradaCrearOrdenDeTicket{TicketID: tk.ID, TecnicoID: &tec,
		Direccion: ptr("x"), Inicio: ptr(inicio.Add(time.Hour))}))(t)
	o = lista(t, e, cd, ct, o)
	// El candado del feed del técnico, tomado por otra transacción: primero llega el cierre (ya con
	// su orden), después crear-orden.
	errores := escalonadas(t, "SELECT pg_advisory_xact_lock(hashtextextended(concat('campo.cambios:', $1::bigint), 0))", tec,
		func() error {
			_, err := e.m.CerrarOrden.Ejecutar(como(t, tec, "tecnico"), campo.EntradaCerrarOrden{ID: o.ID, Version: o.Version})
			return err
		},
		func() error {
			_, err := e.m.CrearOrdenDeTicket.Ejecutar(como(t, d, "despacho"), campo.EntradaCrearOrdenDeTicket{TicketID: tk.ID, TecnicoID: &tec, Direccion: ptr("x")})
			return err
		})
	if errores[0] != nil || errores[1] != nil {
		t.Fatalf("cerrar y crear a la vez: %v", errores)
	}
	if n := contar(t, "SELECT count(*) FROM campo.ordenes WHERE ticket_id = $1", tk.ID); n != 2 {
		t.Fatalf("órdenes: %d", n)
	}
	// Y lo mismo con reabrir: una cancelada del ticket se reabre mientras se crea otra.
	c := deber(e.m.CrearOrdenDeTicket.Ejecutar(cd, campo.EntradaCrearOrdenDeTicket{TicketID: tk.ID, TecnicoID: &tec, Direccion: ptr("x")}))(t)
	c = deber(e.m.CancelarOrden.Ejecutar(cd, campo.EntradaMotivo{ID: c.ID, Version: c.Version, Motivo: "Duplicada"}))(t)
	errores = escalonadas(t, "SELECT pg_advisory_xact_lock(hashtextextended(concat('campo.cambios:', $1::bigint), 0))", tec,
		func() error {
			_, err := e.m.ReabrirOrden.Ejecutar(como(t, d, "despacho"), campo.EntradaMotivo{ID: c.ID, Version: c.Version, Motivo: "Sigue sin señal"})
			return err
		},
		func() error {
			_, err := e.m.CrearOrdenDeTicket.Ejecutar(como(t, d, "despacho"), campo.EntradaCrearOrdenDeTicket{TicketID: tk.ID, TecnicoID: &tec, Direccion: ptr("x")})
			return err
		})
	if errores[0] != nil || errores[1] != nil {
		t.Fatalf("reabrir y crear a la vez: %v", errores)
	}
}

// crear-orden desde el ticket pasa por la validación de cliente y empresa de Argos (#70), como
// POST /ordenes con ticketId: toma los datos de instalación del servicio y, si el cliente del
// ticket ya no es de la empresa según la instantánea, 422 empresa_no_coincide sin crear nada.
func TestCrearOrdenDeTicketValidaClienteEnEmpresa(t *testing.T) {
	registrarEmpresas(t, 65)
	sop := soporte.Nuevo(soporte.Config{})
	e := nuevoEntorno(func(c *campo.Config) { c.Soporte = sop })
	d := despacho(t)
	asignarEmpresa(t, d, 65)
	ctx := pruebas.ContextoUsuarioEnEmpresa(pruebas.Pool(t), d, 65, append(permisosDe("despacho"), permisosDe("soporte")...)...)
	pool := pruebas.Pool(t)
	exec := func(sql string) {
		t.Helper()
		if _, err := pool.Exec(context.Background(), sql); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO integracion.argos_clientes (id, empresa_id, empresas, nombre, codigo, telefono, movil, direccion, lat, lon,
			nit_cui, tipo_documento_fiscal, sincronizado_en, version_datos)
		VALUES (7821, 65, '{65}', 'Juan Cux', 'C-007821', '2233-0000', '', 'Casa de Juan', 14.6, -90.5, '1234567', 'nit', now(), 1)`)
	exec(`INSERT INTO integracion.argos_servicios (id, empresa_id, cliente_id, plan_nombre, ip, direccion, lat, lon, estado, sincronizado_en)
		VALUES (78210, 65, 7821, 'Fibra 100', '100.64.2.1', 'Lote 3, colonia El Prado', 14.7, -90.6, 'activo', now())`)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM integracion.argos_servicios WHERE cliente_id = 7821")
		_, _ = pool.Exec(context.Background(), "DELETE FROM integracion.argos_clientes WHERE id = 7821")
	})
	var cuadrilla int64
	if err := pool.QueryRow(context.Background(), "INSERT INTO agenda.cuadrillas (empresa_id, nombre) VALUES (65, 'Cuadrilla ' || $1) RETURNING id",
		pruebas.Unico()).Scan(&cuadrilla); err != nil {
		t.Fatal(err)
	}
	tk := deber(sop.CrearTicket.Ejecutar(ctx, soporte.EntradaCrearTicket{Asunto: "Sin señal", ClienteID: ptr(int64(7821)), ServicioID: ptr(int64(78210))}))(t)

	o := deber(e.m.CrearOrdenDeTicket.Ejecutar(ctx, campo.EntradaCrearOrdenDeTicket{TicketID: tk.ID, CuadrillaID: &cuadrilla}))(t)
	if o.Direccion != "Lote 3, colonia El Prado" || *o.ClienteID != 7821 || *o.ServicioID != 78210 || *o.EmpresaID != 65 {
		t.Fatalf("orden desde el ticket con datos de Argos: %+v", o)
	}

	// Argos pasó el cliente y su servicio a otra empresa: la validación de #70 lo rechaza.
	exec(`UPDATE integracion.argos_servicios SET empresa_id = 66 WHERE id = 78210`)
	exec(`UPDATE integracion.argos_clientes SET empresa_id = 66, empresas = '{66}' WHERE id = 7821`)
	_, err := e.m.CrearOrdenDeTicket.Ejecutar(ctx, campo.EntradaCrearOrdenDeTicket{TicketID: tk.ID, CuadrillaID: &cuadrilla})
	problema(t, err, 422, "empresa_no_coincide")
	if n := contar(t, "SELECT count(*) FROM campo.ordenes WHERE ticket_id = $1", tk.ID); n != 1 {
		t.Fatalf("órdenes: %d", n)
	}
}
