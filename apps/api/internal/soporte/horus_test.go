package soporte_test

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	integracionpkg "github.com/HCDestroyer/iris/apps/api/internal/integracion"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/horus"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
	"github.com/HCDestroyer/iris/apps/api/internal/soporte"
)

// Tickets desde Horus: maestro único por router (eventos repetidos y workers concurrentes),
// vinculación de hijos por la instantánea de Argos, propuesta al volver, aviso a los hijos al
// resolver, reapertura, hallazgos con y sin regla y con y sin cliente, y empresa.

var entranteSec atomic.Int64

func init() { entranteSec.Store(time.Now().UnixNano() % 1_000_000_000) }

func nuevoEntrante() int64 { return entranteSec.Add(1) }

func eventoHorus(t *testing.T, nombre string, payload any) integracionpkg.Evento {
	t.Helper()
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return integracionpkg.Evento{ID: uuid.Must(uuid.NewV7()), Evento: nombre, OcurridoEn: time.Now(), Payload: b}
}

func router(empresa *int64, routerID int64, estado string) (int64, horus.RouterEstadoCambiado) {
	e := nuevoEntrante()
	return e, horus.RouterEstadoCambiado{RouterID: routerID, Nombre: "RT-Norte", Estado: estado, OcurridoEn: time.Now().UTC(), EmpresaID: empresa, EntranteID: e}
}

func aplicarHorus(t *testing.T, m *soporte.Modulo, e integracionpkg.Evento) {
	t.Helper()
	if err := m.AlEventoHorus(pruebas.ContextoSistema(pruebas.Pool(t)), e); err != nil {
		t.Fatal(err)
	}
}

// instantanea guarda un cliente y su servicio (en un router) en la instantánea de Argos. Las
// zonas son de la empresa (en Argos un id de zona es de una sola empresa): 10·empresa + 7 la
// del cliente y 10·empresa + 8 la del servicio.
func instantanea(t *testing.T, empresa, cliente, servicio, routerID int64) {
	t.Helper()
	pool := pruebas.Pool(t)
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO integracion.argos_clientes (id, empresa_id, nombre, telefono, zona_id, sincronizado_en)
		VALUES ($1::bigint, $2::bigint, 'Cliente ' || $1::bigint::text, '5555-0000', $2::bigint * 10 + 7, now()) ON CONFLICT (id) DO NOTHING`, cliente, empresa); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO integracion.argos_servicios (id, empresa_id, cliente_id, router_id, zona_id, sincronizado_en)
		VALUES ($1, $2::bigint, $3, $4, $2::bigint * 10 + 8, now())`, servicio, empresa, cliente, routerID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM integracion.argos_servicios WHERE id = $1", servicio)
		_, _ = pool.Exec(context.Background(), "DELETE FROM integracion.argos_clientes WHERE id = $1", cliente)
	})
}

type ticketHorus struct {
	ID        int64
	Estado    string
	Maestro   *string
	Empresa   *int64
	Prioridad string
	Origen    string
	Categoria *string
	Propuesta *string
	Padre     *int64
}

func ticketsDe(t *testing.T, where string, args ...any) []ticketHorus {
	t.Helper()
	filas, err := pruebas.Pool(t).Query(context.Background(), `
		SELECT t.id, t.estado, t.maestro, t.empresa_id, t.prioridad, t.origen, c.codigo, t.propuesta, t.ticket_padre_id
		FROM soporte.tickets t LEFT JOIN soporte.categorias c ON c.id = t.categoria_id
		WHERE `+where+` ORDER BY t.id`, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer filas.Close()
	var salida []ticketHorus
	for filas.Next() {
		var x ticketHorus
		if err := filas.Scan(&x.ID, &x.Estado, &x.Maestro, &x.Empresa, &x.Prioridad, &x.Origen, &x.Categoria, &x.Propuesta, &x.Padre); err != nil {
			t.Fatal(err)
		}
		salida = append(salida, x)
	}
	return salida
}

func resultadoHorus(t *testing.T, entrante int64) (string, *int64) {
	t.Helper()
	var r string
	var tk *int64
	if err := pruebas.Pool(t).QueryRow(context.Background(),
		"SELECT resultado, ticket_id FROM soporte.horus_eventos WHERE entrante_id = $1", entrante).Scan(&r, &tk); err != nil {
		t.Fatalf("horus_eventos %d: %v", entrante, err)
	}
	return r, tk
}

func descartados(t *testing.T, entrante int64) int {
	t.Helper()
	var n int
	if err := pruebas.Pool(t).QueryRow(context.Background(),
		"SELECT count(*) FROM integracion.outbox WHERE evento = 'soporte.horus_descartado' AND (payload->>'entrante_id')::bigint = $1", entrante).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestHorusCaidaMaestroUnicoHijosYPropuesta(t *testing.T) {
	registrarEmpresas(t, 61)
	instantanea(t, 61, 6101, 610101, 6100)
	instantanea(t, 61, 6102, 610201, 6100)
	instantanea(t, 61, 6103, 610301, 6199) // otro router, sin incidente
	inicio := time.Now().UTC()
	m, reloj, jobs := nuevoModuloConCola(inicio)
	emp := ptr(int64(61))

	// Dos webhooks distintos de la misma caída, cada uno entregado dos veces y en paralelo.
	e1, p1 := router(emp, 6100, horus.EstadoCaido)
	e2, p2 := router(emp, 6100, horus.EstadoCaido)
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for _, p := range []horus.RouterEstadoCambiado{p1, p1, p2, p2} {
		wg.Add(1)
		go func(p horus.RouterEstadoCambiado) {
			defer wg.Done()
			errs <- m.AlEventoHorus(pruebas.ContextoSistema(pruebas.Pool(t)), eventoHorus(t, horus.EventoRouterEstadoCambiado, p))
		}(p)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	maestros := ticketsDe(t, "t.maestro = 'router' AND t.router_id = 6100")
	if len(maestros) != 1 {
		t.Fatalf("un solo maestro: %+v", maestros)
	}
	ma := maestros[0]
	if ma.Estado != "nuevo" || ma.Prioridad != "alta" || ma.Origen != "horus" || ma.Categoria == nil || *ma.Categoria != "incidente_red" ||
		ma.Empresa == nil || *ma.Empresa != 61 {
		t.Fatalf("maestro: %+v", ma)
	}
	r1, t1 := resultadoHorus(t, e1)
	r2, t2 := resultadoHorus(t, e2)
	resultados := []string{r1, r2}
	slices.Sort(resultados)
	// El segundo webhook deja una nota o, si llegó después el más nuevo, queda obsoleto.
	mismo := func(id *int64) bool { return id == nil || *id == ma.ID }
	if resultados[0] != "maestro_creado" || (resultados[1] != "maestro_existente" && resultados[1] != "obsoleto") ||
		!mismo(t1) || !mismo(t2) {
		t.Fatalf("resultados: %s %v / %s %v", r1, t1, r2, t2)
	}
	if n := contar(eventosDe(t, ma.ID), "ticket.creado"); n != 1 {
		t.Fatalf("ticket.creado del maestro: %d", n)
	}
	sis := pruebas.ContextoSistema(pruebas.Pool(t))
	tk, err := m.VerTicket.Ejecutar(sis, soporte.EntradaVerTicket{ID: ma.ID})
	if err != nil || tk.Incidente == nil || tk.Incidente.Tipo != "router" || tk.Incidente.RouterID == nil || *tk.Incidente.RouterID != 6100 ||
		tk.Incidente.Nombre == nil || *tk.Incidente.Nombre != "RT-Norte" || !strings.Contains(tk.Asunto, "RT-Norte") {
		t.Fatalf("ticket maestro por la API: %+v %v", tk, err)
	}

	// Hijos: el ticket de un cliente con servicio en ese router se vincula (por servicio o, sin
	// servicio, por su único router); uno de otro router o sin instantánea, no.
	yo := agente(t)
	asignarEmpresa(t, yo, 61)
	ctx := comoEnEmpresa(t, yo, 61)
	h1 := crear(t, m, ctx, soporte.EntradaCrearTicket{ClienteID: ptr(int64(6101)), ServicioID: ptr(int64(610101)), Contacto: &soporte.EntradaContacto{Correo: ptr("hijo@correo.gt")}})
	h2 := crear(t, m, ctx, soporte.EntradaCrearTicket{ClienteID: ptr(int64(6102))})
	otro := crear(t, m, ctx, soporte.EntradaCrearTicket{ClienteID: ptr(int64(6103))})
	sinInst := crear(t, m, ctx, soporte.EntradaCrearTicket{ClienteID: ptr(int64(6177))})
	for _, h := range []soporte.Ticket{h1, h2} {
		if h.TicketPadreID == nil || *h.TicketPadreID != ma.ID {
			t.Fatalf("hijo sin vincular: %+v", h)
		}
		msjs, err := m.ListarMensajes.Ejecutar(ctx, soporte.EntradaListarMensajes{ID: h.ID})
		if err != nil || len(msjs.Data) != 1 || msjs.Data[0].Publico || !strings.Contains(msjs.Data[0].Cuerpo, "Vinculado automáticamente") {
			t.Fatalf("nota de vinculación: %+v %v", msjs.Data, err)
		}
	}
	// También desde el portal (actor cliente): el cliente ve que hay una falla general.
	porPortal, err := m.CrearTicketPortal.Ejecutar(pruebas.ContextoCliente(pruebas.Pool(t), 6101, 61),
		soporte.EntradaCrearTicketPortal{Asunto: "No tengo internet", Descripcion: "Desde hace una hora"})
	if err != nil || !porPortal.AfectadoPorIncidente {
		t.Fatalf("hijo por el portal: %+v %v", porPortal, err)
	}
	if otro.TicketPadreID != nil || sinInst.TicketPadreID != nil {
		t.Fatalf("no debía vincular: %+v %+v", otro.TicketPadreID, sinInst.TicketPadreID)
	}

	// Arriba: propuesta de resolver (no resuelve solo); repetido, nada más.
	e3, p3 := router(emp, 6100, horus.EstadoArriba)
	ev3 := eventoHorus(t, horus.EventoRouterEstadoCambiado, p3)
	aplicarHorus(t, m, ev3)
	aplicarHorus(t, m, ev3)
	if r, _ := resultadoHorus(t, e3); r != "propuesta" {
		t.Fatalf("arriba: %s", r)
	}
	tk, _ = m.VerTicket.Ejecutar(sis, soporte.EntradaVerTicket{ID: ma.ID})
	if tk.Estado != "nuevo" || tk.Propuesta == nil || tk.Propuesta.Accion != "resolver" {
		t.Fatalf("propuesta: %+v", tk)
	}
	if n := contar(eventosDe(t, ma.ID), "ticket.resolucion_propuesta"); n != 1 {
		t.Fatalf("ticket.resolucion_propuesta: %d", n)
	}

	// Un agente resuelve el maestro: la propuesta se descarta y se encola el aviso a los hijos
	// (job soporte.avisar_hijos); el job avisa a cada uno con un mensaje público.
	res, err := m.ResolverTicket.Ejecutar(ctx, soporte.EntradaResolverTicket{ID: ma.ID, Version: tk.Version, Solucion: "Se cambió la fuente del router"})
	if err != nil || res.Propuesta != nil {
		t.Fatalf("resolver maestro: %+v %v", res, err)
	}
	if v, _ := m.VerTicket.Ejecutar(sis, soporte.EntradaVerTicket{ID: h1.ID}); v.Version != h1.Version {
		t.Fatal("resolver no toca a los hijos en su transacción")
	}
	avisos := jobs.tomar()
	if len(avisos) != 1 || avisos[0].Aviso != soporte.AvisoResuelto || avisos[0].MaestroID != ma.ID {
		t.Fatalf("job de aviso: %+v", avisos)
	}
	for range 2 { // el job repetido no duplica
		if n, err := m.AvisarHijos(sis, avisos[0]); err != nil {
			t.Fatal(err)
		} else if n != 0 && n != 3 {
			t.Fatalf("hijos avisados: %d", n)
		}
	}
	if v, _ := m.VerTicket.Ejecutar(sis, soporte.EntradaVerTicket{ID: h1.ID}); v.Version != h1.Version+1 {
		t.Fatalf("el aviso toca al hijo (versión): %d", v.Version)
	}
	var hist int
	if err := pruebas.Pool(t).QueryRow(context.Background(),
		"SELECT count(*) FROM soporte.ticket_eventos WHERE ticket_id = $1 AND tipo = 'aviso_incidente'", h1.ID).Scan(&hist); err != nil || hist != 1 {
		t.Fatalf("historial del aviso: %d %v", hist, err)
	}
	// …y sale en GET /tickets/{id}/eventos (en el enum de EventoTicket).
	evs, err := m.ListarEventos.Ejecutar(sis, soporte.EntradaListarEventos{ID: h1.ID})
	if err != nil {
		t.Fatal(err)
	}
	aviso := false
	for _, ev := range evs.Data {
		aviso = aviso || ev.Tipo == "aviso_incidente"
	}
	if !aviso {
		t.Fatalf("aviso_incidente en el historial: %+v", evs.Data)
	}
	// El maestro ya no está abierto: el cliente deja de ver «falla general».
	if v, err := m.VerTicketPortal.Ejecutar(pruebas.ContextoCliente(pruebas.Pool(t), 6101, 61), soporte.EntradaVerTicketPortal{ID: porPortal.ID}); err != nil || v.AfectadoPorIncidente {
		t.Fatalf("afectadoPorIncidente con el maestro resuelto: %+v %v", v, err)
	}
	if l, _ := m.ListarTicketsPortal.Ejecutar(pruebas.ContextoCliente(pruebas.Pool(t), 6101, 61), soporte.EntradaListarTicketsPortal{}); len(l.Data) == 0 || l.Data[0].AfectadoPorIncidente {
		t.Fatalf("lista con el maestro resuelto: %+v", l.Data)
	}
	for _, h := range []soporte.Ticket{h1, h2} {
		msjs, _ := m.ListarMensajes.Ejecutar(ctx, soporte.EntradaListarMensajes{ID: h.ID, SoloPublicos: true})
		if len(msjs.Data) != 1 || msjs.Data[0].AutorTipo != "sistema" || !strings.Contains(msjs.Data[0].Cuerpo, "Se cambió la fuente") {
			t.Fatalf("aviso al hijo %d: %+v", h.ID, msjs.Data)
		}
		if contar(eventosDe(t, h.ID), "ticket.respondido") != 1 {
			t.Fatalf("ticket.respondido del hijo %d", h.ID)
		}
		if v, _ := m.VerTicket.Ejecutar(sis, soporte.EntradaVerTicket{ID: h.ID}); v.Estado != "nuevo" {
			t.Fatalf("el hijo no cambia de estado: %s", v.Estado)
		}
	}
	if len(eventosDe(t, otro.ID)) != 1 {
		t.Fatal("el ticket no vinculado no recibe aviso")
	}

	// Vuelve a caer antes de 24 h (según Horus): se reabre el mismo maestro y se avisa a los hijos.
	reloj.avanzar(2 * time.Hour)
	e4, p4 := router(emp, 6100, horus.EstadoCaido)
	p4.OcurridoEn = inicio.Add(2 * time.Hour)
	aplicarHorus(t, m, eventoHorus(t, horus.EventoRouterEstadoCambiado, p4))
	if r, id := resultadoHorus(t, e4); r != "maestro_reabierto" || *id != ma.ID {
		t.Fatalf("reabrir: %s", r)
	}
	if x := ticketsDe(t, "t.id = $1", ma.ID); x[0].Estado != "abierto" {
		t.Fatalf("estado tras reabrir: %+v", x)
	}
	avisos = jobs.tomar()
	if len(avisos) != 1 || avisos[0].Aviso != soporte.AvisoReabierto {
		t.Fatalf("aviso de reapertura: %+v", avisos)
	}
	if _, err := m.AvisarHijos(sis, avisos[0]); err != nil {
		t.Fatal(err)
	}
	if msjs, _ := m.ListarMensajes.Ejecutar(sis, soporte.EntradaListarMensajes{ID: h1.ID, SoloPublicos: true}); len(msjs.Data) != 2 ||
		!strings.Contains(msjs.Data[1].Cuerpo, "de nuevo la falla") {
		t.Fatalf("aviso de nueva caída: %+v", msjs.Data)
	}

	// Se resuelve otra vez y cae pasadas 24 h: el viejo se cierra y se abre otro maestro.
	tk, _ = m.VerTicket.Ejecutar(sis, soporte.EntradaVerTicket{ID: ma.ID})
	if _, err := m.ResolverTicket.Ejecutar(ctx, soporte.EntradaResolverTicket{ID: ma.ID, Version: tk.Version, Solucion: "Listo de nuevo"}); err != nil {
		t.Fatal(err)
	}
	jobs.tomar()
	reloj.avanzar(soporte.VentanaReapertura + time.Hour)
	e5, p5 := router(emp, 6100, horus.EstadoCaido)
	p5.OcurridoEn = inicio.Add(3*time.Hour + soporte.VentanaReapertura)
	aplicarHorus(t, m, eventoHorus(t, horus.EventoRouterEstadoCambiado, p5))
	r5, id5 := resultadoHorus(t, e5)
	if r5 != "maestro_creado" || *id5 == ma.ID {
		t.Fatalf("nuevo maestro: %s %v", r5, id5)
	}
	todos := ticketsDe(t, "t.maestro = 'router' AND t.router_id = 6100")
	if len(todos) != 2 || todos[0].Estado != "cerrado" || todos[1].Estado != "nuevo" {
		t.Fatalf("maestros tras 24 h: %+v", todos)
	}
	notas, _ := m.ListarMensajes.Ejecutar(sis, soporte.EntradaListarMensajes{ID: *id5})
	if len(notas.Data) != 1 || !strings.Contains(notas.Data[0].Cuerpo, "Incidente anterior") {
		t.Fatalf("nota del nuevo maestro: %+v", notas.Data)
	}
	// La base no admite un segundo maestro no cerrado del mismo router.
	if _, err := pruebas.Pool(t).Exec(context.Background(), `
		INSERT INTO soporte.tickets (empresa_id, prioridad, origen, asunto, sla_inicio, maestro, router_id)
		VALUES (61, 'alta', 'horus', 'Duplicado', now(), 'router', 6100)`); err == nil || !strings.Contains(err.Error(), "tickets_maestro_router_uq") {
		t.Fatalf("índice único de maestros: %v", err)
	}
}

// colaJobs guarda los jobs que el módulo encolaría en River.
type colaJobs struct {
	mu   sync.Mutex
	args []soporte.ArgsAvisarHijos
}

func (c *colaJobs) tomar() []soporte.ArgsAvisarHijos {
	c.mu.Lock()
	defer c.mu.Unlock()
	a := c.args
	c.args = nil
	return a
}

func nuevoModuloConCola(inicio time.Time) (*soporte.Modulo, *reloj, *colaJobs) {
	r := &reloj{t: inicio}
	c := &colaJobs{}
	m := soporte.Nuevo(soporte.Config{Reloj: r.ahora, Encolar: func(_ context.Context, _ pgx.Tx, args river.JobArgs) error {
		if a, ok := args.(soporte.ArgsAvisarHijos); ok {
			c.mu.Lock()
			c.args = append(c.args, a)
			c.mu.Unlock()
		}
		return nil
	}})
	return m, r, c
}

func TestHorusServicioInfra(t *testing.T) {
	registrarEmpresas(t, 64)
	instantanea(t, 64, 6401, 640101, 6400)
	m, _ := nuevoModulo(time.Now())
	emp := ptr(int64(64))
	e := nuevoEntrante()
	aplicarHorus(t, m, eventoHorus(t, horus.EventoServicioInfraCambiado, horus.ServicioInfraCambiado{
		InfraID: "olt-64", Nombre: "OLT Centro", Tipo: "olt", Estado: horus.EstadoCaido, RouterID: ptr(int64(6400)),
		OcurridoEn: time.Now(), EmpresaID: emp, EntranteID: e,
	}))
	r, id := resultadoHorus(t, e)
	if r != "maestro_creado" {
		t.Fatalf("infra: %s", r)
	}
	if x := ticketsDe(t, "t.id = $1", *id); x[0].Maestro == nil || *x[0].Maestro != "infra" || x[0].Categoria == nil || *x[0].Categoria != "incidente_red" {
		t.Fatalf("maestro infra: %+v", x)
	}
	yo := agente(t)
	asignarEmpresa(t, yo, 64)
	h := crear(t, m, comoEnEmpresa(t, yo, 64), soporte.EntradaCrearTicket{ClienteID: ptr(int64(6401)), ServicioID: ptr(int64(640101))})
	if h.TicketPadreID == nil || *h.TicketPadreID != *id {
		t.Fatalf("hijo del maestro infra: %+v", h.TicketPadreID)
	}
	// Arriba con otro webhook: propuesta.
	e2 := nuevoEntrante()
	aplicarHorus(t, m, eventoHorus(t, horus.EventoServicioInfraCambiado, horus.ServicioInfraCambiado{
		InfraID: "olt-64", Estado: horus.EstadoArriba, OcurridoEn: time.Now(), EmpresaID: emp, EntranteID: e2,
	}))
	if r, _ := resultadoHorus(t, e2); r != "propuesta" {
		t.Fatalf("infra arriba: %s", r)
	}
}

func TestHorusEmpresa(t *testing.T) {
	m, _ := nuevoModulo(time.Now())

	// Sin conector (sin empresas registradas): el maestro va sin empresa.
	e, p := router(ptr(int64(77)), 6500, horus.EstadoCaido)
	aplicarHorus(t, m, eventoHorus(t, horus.EventoRouterEstadoCambiado, p))
	r, id := resultadoHorus(t, e)
	if x := ticketsDe(t, "t.id = $1", *id); r != "maestro_creado" || x[0].Empresa != nil {
		t.Fatalf("sin conector: %s %+v", r, x)
	}

	// Con empresas registradas: sin empresa en el evento, o con una que Iris no tiene, no se
	// crea nada; queda registrado y sale la alerta interna.
	registrarEmpresas(t, 65)
	e, p = router(nil, 6501, horus.EstadoCaido)
	aplicarHorus(t, m, eventoHorus(t, horus.EventoRouterEstadoCambiado, p))
	if r, id := resultadoHorus(t, e); r != "sin_empresa" || id != nil || descartados(t, e) != 1 {
		t.Fatalf("sin empresa: %s %v", r, id)
	}
	e, p = router(ptr(int64(999)), 6501, horus.EstadoCaido)
	aplicarHorus(t, m, eventoHorus(t, horus.EventoRouterEstadoCambiado, p))
	if r, _ := resultadoHorus(t, e); r != "empresa_no_registrada" || descartados(t, e) != 1 {
		t.Fatalf("empresa no registrada: %s", r)
	}
	if x := ticketsDe(t, "t.router_id = 6501"); len(x) != 0 {
		t.Fatalf("no debía crear: %+v", x)
	}
	// Con su empresa, sí.
	e, p = router(ptr(int64(65)), 6501, horus.EstadoCaido)
	aplicarHorus(t, m, eventoHorus(t, horus.EventoRouterEstadoCambiado, p))
	if r, id := resultadoHorus(t, e); r != "maestro_creado" || ticketsDe(t, "t.id = $1", *id)[0].Empresa == nil {
		t.Fatalf("con empresa: %s", r)
	}
	// Basura: no se reintenta ni se registra.
	if err := m.AlEventoHorus(pruebas.ContextoSistema(pruebas.Pool(t)), integracionpkg.Evento{Evento: horus.EventoRouterEstadoCambiado, Payload: []byte("{")}); err != nil {
		t.Fatal(err)
	}
	// Solo el sistema ejecuta estos casos.
	_, err := m.IncidenteHorus.Ejecutar(como(t, agente(t)), soporte.EntradaIncidenteHorus{EntranteID: nuevoEntrante(), Tipo: "router", RouterID: ptr(int64(1)), Estado: "caido"})
	problema(t, err, 403, "prohibido")
}

// regla agrega una regla de hallazgo (y la borra al terminar).
func regla(t *testing.T, empresa *int64, categoriaHorus, categoria, prioridad string, activa bool) {
	t.Helper()
	pool := pruebas.Pool(t)
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO soporte.reglas_hallazgos (empresa_id, categoria_horus, categoria_id, prioridad, activa)
		SELECT $1, $2, id, $4, $5 FROM soporte.categorias WHERE codigo = $3 AND empresa_id IS NULL`,
		empresa, categoriaHorus, categoria, prioridad, activa); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM soporte.reglas_hallazgos WHERE categoria_horus = $1", categoriaHorus)
	})
}

func hallazgo(empresa *int64, id, categoria string, cliente, servicio *int64, motivo *string) (int64, horus.HallazgoAbierto) {
	e := nuevoEntrante()
	ip := "100.64.3.9"
	url := "https://horus.prueba/hallazgos/" + id
	return e, horus.HallazgoAbierto{
		HallazgoID: id, Categoria: categoria, Severidad: "alta", IP: &ip, Resumen: "Tráfico a C2 conocido", URLHorus: &url,
		ClienteID: cliente, ServicioID: servicio, EmpresaID: empresa, MotivoSinCliente: motivo, OcurridoEn: time.Now(), EntranteID: e,
	}
}

func TestHorusHallazgos(t *testing.T) {
	registrarEmpresas(t, 63)
	instantanea(t, 63, 6301, 630101, 6300)
	m, _ := nuevoModulo(time.Now())
	emp := ptr(int64(63))
	sis := pruebas.ContextoSistema(pruebas.Pool(t))
	h := "h-" + pruebas.Unico()

	// Sin regla (o con la semilla inactiva): no abre ticket.
	e, p := hallazgo(emp, h, "equipo_infectado", ptr(int64(6301)), ptr(int64(630101)), nil)
	aplicarHorus(t, m, eventoHorus(t, horus.EventoHallazgoAbierto, p))
	if r, id := resultadoHorus(t, e); r != "sin_regla" || id != nil {
		t.Fatalf("semilla inactiva: %s", r)
	}

	// Regla activa: ticket del cliente, uno por hallazgo.
	regla(t, nil, "malware_i2", "equipo", "alta", true)
	e, p = hallazgo(emp, h, "malware_i2", ptr(int64(6301)), ptr(int64(630101)), nil)
	aplicarHorus(t, m, eventoHorus(t, horus.EventoHallazgoAbierto, p))
	r, id := resultadoHorus(t, e)
	if r != "ticket_creado" {
		t.Fatalf("hallazgo: %s", r)
	}
	tk, err := m.VerTicket.Ejecutar(sis, soporte.EntradaVerTicket{ID: *id})
	if err != nil || tk.ClienteID == nil || *tk.ClienteID != 6301 || tk.ServicioID == nil || tk.Prioridad != "alta" || tk.Origen != "horus" ||
		tk.TicketPadreID != nil || tk.Incidente == nil || tk.Incidente.Tipo != "hallazgo" || tk.Incidente.HallazgoID == nil ||
		*tk.Incidente.HallazgoID != h || tk.Incidente.URLHorus == nil || *tk.Incidente.URLHorus != "https://horus.prueba/hallazgos/"+h {
		t.Fatalf("ticket del hallazgo: %+v %v", tk, err)
	}
	msjs, _ := m.ListarMensajes.Ejecutar(sis, soporte.EntradaListarMensajes{ID: tk.ID})
	if len(msjs.Data) != 2 || !msjs.Data[0].Publico || strings.Contains(msjs.Data[0].Cuerpo, "100.64") ||
		msjs.Data[1].Publico || !strings.Contains(msjs.Data[1].Cuerpo, "Ver en Horus") || !strings.Contains(msjs.Data[1].Cuerpo, "100.64.3.9") {
		t.Fatalf("mensajes del hallazgo: %+v", msjs.Data)
	}
	e, p = hallazgo(emp, h, "malware_i2", ptr(int64(6301)), ptr(int64(630101)), nil)
	aplicarHorus(t, m, eventoHorus(t, horus.EventoHallazgoAbierto, p))
	if r, id2 := resultadoHorus(t, e); r != "ticket_existente" || *id2 != tk.ID {
		t.Fatalf("hallazgo repetido: %s", r)
	}

	// Sin cliente: ticket sin cliente con el motivo en la descripción.
	h2 := "h-" + pruebas.Unico()
	e, p = hallazgo(emp, h2, "malware_i2", nil, nil, ptr("ip_sin_cliente"))
	aplicarHorus(t, m, eventoHorus(t, horus.EventoHallazgoAbierto, p))
	_, id = resultadoHorus(t, e)
	sinCliente, _ := m.VerTicket.Ejecutar(sis, soporte.EntradaVerTicket{ID: *id})
	desc, _ := m.ListarMensajes.Ejecutar(sis, soporte.EntradaListarMensajes{ID: *id, SoloPublicos: true})
	if sinCliente.ClienteID != nil || len(desc.Data) != 1 || !strings.Contains(desc.Data[0].Cuerpo, "ip_sin_cliente") {
		t.Fatalf("sin cliente: %+v %+v", sinCliente, desc.Data)
	}

	// Una regla de la empresa (inactiva) apaga la global para esa empresa.
	regla(t, emp, "malware_i2b", "equipo", "media", false)
	regla(t, nil, "malware_i2b", "equipo", "media", true)
	e, p = hallazgo(emp, "h-"+pruebas.Unico(), "malware_i2b", nil, nil, ptr("sin_ip"))
	aplicarHorus(t, m, eventoHorus(t, horus.EventoHallazgoAbierto, p))
	if r, _ := resultadoHorus(t, e); r != "sin_regla" {
		t.Fatalf("regla de empresa: %s", r)
	}

	// Sin empresa con empresas registradas: no crea, alerta.
	e, p = hallazgo(nil, "h-"+pruebas.Unico(), "malware_i2", nil, nil, ptr("empresa_desconocida"))
	aplicarHorus(t, m, eventoHorus(t, horus.EventoHallazgoAbierto, p))
	if r, _ := resultadoHorus(t, e); r != "sin_empresa" || descartados(t, e) != 1 {
		t.Fatalf("hallazgo sin empresa: %s", r)
	}

	// Regla con una categoría inactiva: error de negocio registrado, sin reintentos.
	if _, err := pruebas.Pool(t).Exec(context.Background(),
		"INSERT INTO soporte.categorias (codigo, nombre, activa) VALUES ('inactiva_i2', 'Inactiva', false) ON CONFLICT DO NOTHING"); err != nil {
		t.Fatal(err)
	}
	regla(t, nil, "malware_i2c", "inactiva_i2", "media", true)
	e, p = hallazgo(emp, "h-"+pruebas.Unico(), "malware_i2c", nil, nil, ptr("sin_ip"))
	aplicarHorus(t, m, eventoHorus(t, horus.EventoHallazgoAbierto, p))
	if r, _ := resultadoHorus(t, e); r != "error" || descartados(t, e) != 1 {
		t.Fatalf("regla con categoría inactiva: %s", r)
	}

	// Resuelto en Horus: nota y propuesta de resolver; repetido, nada más.
	er := nuevoEntrante()
	ev := eventoHorus(t, horus.EventoHallazgoResuelto, horus.HallazgoResuelto{HallazgoID: h, Categoria: "malware_i2", Resumen: "Equipo limpio", OcurridoEn: time.Now(), EmpresaID: emp, EntranteID: er})
	aplicarHorus(t, m, ev)
	aplicarHorus(t, m, ev)
	if r, _ := resultadoHorus(t, er); r != "propuesta" {
		t.Fatalf("hallazgo resuelto: %s", r)
	}
	tk, _ = m.VerTicket.Ejecutar(sis, soporte.EntradaVerTicket{ID: tk.ID})
	if tk.Propuesta == nil || tk.Propuesta.Accion != "resolver" || contar(eventosDe(t, tk.ID), "ticket.resolucion_propuesta") != 1 {
		t.Fatalf("propuesta del hallazgo: %+v", tk.Propuesta)
	}
	// Resuelto por el agente y Horus lo vuelve a avisar (otro webhook): propuesta de cerrar.
	yo := agente(t)
	asignarEmpresa(t, yo, 63)
	if _, err := m.ResolverTicket.Ejecutar(comoEnEmpresa(t, yo, 63), soporte.EntradaResolverTicket{ID: tk.ID, Version: tk.Version, Solucion: "Equipo limpiado"}); err != nil {
		t.Fatal(err)
	}
	er2 := nuevoEntrante()
	aplicarHorus(t, m, eventoHorus(t, horus.EventoHallazgoResuelto, horus.HallazgoResuelto{HallazgoID: h, OcurridoEn: time.Now(), EmpresaID: emp, EntranteID: er2}))
	tk, _ = m.VerTicket.Ejecutar(sis, soporte.EntradaVerTicket{ID: tk.ID})
	if tk.Propuesta == nil || tk.Propuesta.Accion != "cerrar" {
		t.Fatalf("propuesta de cerrar: %+v", tk.Propuesta)
	}
	// Hallazgo desconocido: nada.
	er3 := nuevoEntrante()
	aplicarHorus(t, m, eventoHorus(t, horus.EventoHallazgoResuelto, horus.HallazgoResuelto{HallazgoID: "no-existe", OcurridoEn: time.Now(), EmpresaID: emp, EntranteID: er3}))
	if r, _ := resultadoHorus(t, er3); r != "sin_ticket" {
		t.Fatalf("hallazgo desconocido: %s", r)
	}
}

// Por el camino real del worker: el conector publica horus.* en el outbox y el relay lo
// entrega a soporte (cmd/worker: SuscribirHorus). Dos entregas no duplican.
func TestHorusPorElRelay(t *testing.T) {
	pool := pruebas.Pool(t)
	m, _ := nuevoModulo(time.Now())
	bus := integracionpkg.NuevoBus()
	m.SuscribirHorus(bus, pool)
	relay := &integracionpkg.Relay{BD: pool, Bus: bus}
	e, p := router(nil, 6600, horus.EstadoCaido)
	for range 2 { // el conector lo publicó dos veces (caso extremo): un solo maestro
		if err := nucleo.EnTx(nucleo.ConBD(context.Background(), pool), func(ctx context.Context, tx pgx.Tx) error {
			_, err := integracionpkg.Insertar(ctx, tx, horus.EventoRouterEstadoCambiado, nil, "prueba", p)
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := relay.Ejecutar(context.Background()); err != nil {
		t.Fatal(err)
	}
	if r, _ := resultadoHorus(t, e); r != "maestro_creado" {
		t.Fatalf("por el relay: %s", r)
	}
	if x := ticketsDe(t, "t.maestro = 'router' AND t.router_id = 6600"); len(x) != 1 {
		t.Fatalf("maestros: %+v", x)
	}
}
