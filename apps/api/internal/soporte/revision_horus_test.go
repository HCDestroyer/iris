package soporte_test

import (
	"context"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/horus"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
	"github.com/HCDestroyer/iris/apps/api/internal/soporte"
)

// Revisión de la PR de Horus y portal: orden de los eventos de Horus, topes del portal,
// afectadoPorIncidente, versión sin cambio y aviso a los hijos en tandas.

func routerEn(emp *int64, id int64, estado string, cuando time.Time) (int64, horus.RouterEstadoCambiado) {
	e, p := router(emp, id, estado)
	p.OcurridoEn = cuando
	return e, p
}

// A: llega primero el «arriba» (más reciente) y después un «caído» viejo (reintento de Horus):
// no se abre nada.
func TestRevisionHorusArribaAntesQueCaidaVieja(t *testing.T) {
	registrarEmpresas(t, 91)
	m, _ := nuevoModulo(time.Now().UTC())
	emp := ptr(int64(91))
	t0 := time.Now().UTC().Add(-10 * time.Minute)
	eA, pA := routerEn(emp, 9100, horus.EstadoArriba, t0.Add(time.Minute))
	eC, pC := routerEn(emp, 9100, horus.EstadoCaido, t0)
	aplicarHorus(t, m, eventoHorus(t, horus.EventoRouterEstadoCambiado, pA))
	aplicarHorus(t, m, eventoHorus(t, horus.EventoRouterEstadoCambiado, pC))
	if r, _ := resultadoHorus(t, eA); r != "sin_maestro_abierto" {
		t.Fatalf("arriba: %s", r)
	}
	if r, _ := resultadoHorus(t, eC); r != "obsoleto" {
		t.Fatalf("caído viejo: %s", r)
	}
	if x := ticketsDe(t, "t.maestro = 'router' AND t.router_id = 9100"); len(x) != 0 {
		t.Fatalf("no debía abrir un maestro: %+v", x)
	}
	// Una caída posterior sí abre; la misma hora repetida (otro webhook) es obsoleta.
	e2, p2 := routerEn(emp, 9100, horus.EstadoCaido, t0.Add(2*time.Minute))
	aplicarHorus(t, m, eventoHorus(t, horus.EventoRouterEstadoCambiado, p2))
	e3, p3 := routerEn(emp, 9100, horus.EstadoCaido, t0.Add(2*time.Minute))
	aplicarHorus(t, m, eventoHorus(t, horus.EventoRouterEstadoCambiado, p3))
	if r, _ := resultadoHorus(t, e2); r != "maestro_creado" {
		t.Fatalf("caída nueva: %s", r)
	}
	if r, _ := resultadoHorus(t, e3); r != "obsoleto" {
		t.Fatalf("misma hora: %s", r)
	}
}

// B: un «arriba» más viejo que la caída que abrió el maestro no propone resolver.
func TestRevisionHorusArribaViejoNoPropone(t *testing.T) {
	registrarEmpresas(t, 92)
	m, _ := nuevoModulo(time.Now().UTC())
	emp := ptr(int64(92))
	t1 := time.Now().UTC().Add(-5 * time.Minute)
	_, pC := routerEn(emp, 9200, horus.EstadoCaido, t1)
	aplicarHorus(t, m, eventoHorus(t, horus.EventoRouterEstadoCambiado, pC))
	eA, pA := routerEn(emp, 9200, horus.EstadoArriba, t1.Add(-time.Minute))
	aplicarHorus(t, m, eventoHorus(t, horus.EventoRouterEstadoCambiado, pA))
	if r, _ := resultadoHorus(t, eA); r != "obsoleto" {
		t.Fatalf("arriba viejo: %s", r)
	}
	x := ticketsDe(t, "t.maestro = 'router' AND t.router_id = 9200")
	if len(x) != 1 || x[0].Propuesta != nil || x[0].Estado != "nuevo" {
		t.Fatalf("sin propuesta: %+v", x)
	}
}

// C: la ventana de 24 h se mide con ocurrido_en (la hora de Horus), no con la de proceso.
func TestRevisionHorusVentanaPorOcurrido(t *testing.T) {
	registrarEmpresas(t, 93)
	inicio := time.Now().UTC()
	m, reloj := nuevoModulo(inicio)
	emp := ptr(int64(93))
	_, pC := routerEn(emp, 9300, horus.EstadoCaido, inicio.Add(-time.Hour))
	aplicarHorus(t, m, eventoHorus(t, horus.EventoRouterEstadoCambiado, pC))
	ma := ticketsDe(t, "t.maestro = 'router' AND t.router_id = 9300")[0]
	yo := agente(t)
	asignarEmpresa(t, yo, 93)
	ctx := comoEnEmpresa(t, yo, 93)
	tk, _ := m.VerTicket.Ejecutar(ctx, soporte.EntradaVerTicket{ID: ma.ID})
	if _, err := m.ResolverTicket.Ejecutar(ctx, soporte.EntradaResolverTicket{ID: ma.ID, Version: tk.Version, Solucion: "Listo"}); err != nil {
		t.Fatal(err)
	}
	// La caída ocurrió 23 h después de resolver, pero se procesa 30 h después (cola atrasada):
	// se reabre el mismo maestro.
	reloj.avanzar(30 * time.Hour)
	e, p := routerEn(emp, 9300, horus.EstadoCaido, inicio.Add(23*time.Hour))
	aplicarHorus(t, m, eventoHorus(t, horus.EventoRouterEstadoCambiado, p))
	if r, id := resultadoHorus(t, e); r != "maestro_reabierto" || *id != ma.ID {
		t.Fatalf("ventana por ocurrido_en: %s", r)
	}
}

// Hallazgo fuera de orden: el resuelto llega antes que el abierto (más viejo) → no se abre
// ticket. Una apertura posterior al resuelto (Horus lo reabrió) sí abre.
func TestRevisionHorusHallazgoFueraDeOrden(t *testing.T) {
	registrarEmpresas(t, 94)
	m, _ := nuevoModulo(time.Now().UTC())
	emp := ptr(int64(94))
	regla(t, nil, "malware_rev", "equipo", "alta", true)
	h := "h-" + pruebas.Unico()
	t0 := time.Now().UTC().Add(-time.Hour)

	er := nuevoEntrante()
	aplicarHorus(t, m, eventoHorus(t, horus.EventoHallazgoResuelto, horus.HallazgoResuelto{
		HallazgoID: h, Categoria: "malware_rev", OcurridoEn: t0.Add(time.Minute), EmpresaID: emp, EntranteID: er,
	}))
	if r, _ := resultadoHorus(t, er); r != "sin_ticket" {
		t.Fatalf("resuelto sin ticket: %s", r)
	}
	ea, pa := hallazgo(emp, h, "malware_rev", nil, nil, ptr("sin_ip"))
	pa.OcurridoEn = t0
	aplicarHorus(t, m, eventoHorus(t, horus.EventoHallazgoAbierto, pa))
	if r, _ := resultadoHorus(t, ea); r != "obsoleto" {
		t.Fatalf("abierto viejo: %s", r)
	}
	if x := ticketsDe(t, "t.hallazgo_id = $1", h); len(x) != 0 {
		t.Fatalf("no debía abrir: %+v", x)
	}
	eb, pb := hallazgo(emp, h, "malware_rev", nil, nil, ptr("sin_ip"))
	pb.OcurridoEn = t0.Add(2 * time.Minute)
	aplicarHorus(t, m, eventoHorus(t, horus.EventoHallazgoAbierto, pb))
	if r, _ := resultadoHorus(t, eb); r != "ticket_creado" {
		t.Fatalf("reabierto en Horus: %s", r)
	}
	// Un resuelto más viejo que esa apertura no propone nada.
	ev := nuevoEntrante()
	aplicarHorus(t, m, eventoHorus(t, horus.EventoHallazgoResuelto, horus.HallazgoResuelto{
		HallazgoID: h, OcurridoEn: t0.Add(90 * time.Second), EmpresaID: emp, EntranteID: ev,
	}))
	if r, _ := resultadoHorus(t, ev); r != "obsoleto" {
		t.Fatalf("resuelto viejo: %s", r)
	}
	if x := ticketsDe(t, "t.hallazgo_id = $1", h); len(x) != 1 || x[0].Propuesta != nil {
		t.Fatalf("sin propuesta: %+v", x)
	}
}

// El aviso a los hijos se procesa en tandas de TandaAvisos y no se repite.
func TestRevisionAvisoHijosEnTandas(t *testing.T) {
	m, _, jobs := nuevoModuloConCola(time.Now().UTC())
	sis := pruebas.ContextoSistema(pruebas.Pool(t))
	_, p := router(nil, 9500, horus.EstadoCaido)
	aplicarHorus(t, m, eventoHorus(t, horus.EventoRouterEstadoCambiado, p))
	ma := ticketsDe(t, "t.maestro = 'router' AND t.router_id = 9500")[0]
	n := soporte.TandaAvisos + 5
	if _, err := pruebas.Pool(t).Exec(context.Background(), `
		INSERT INTO soporte.tickets (prioridad, origen, asunto, sla_inicio, ticket_padre_id)
		SELECT 'media', 'panel', 'Hijo ' || g, now(), $1 FROM generate_series(1, $2::int) g`, ma.ID, n); err != nil {
		t.Fatal(err)
	}
	tk, _ := m.VerTicket.Ejecutar(sis, soporte.EntradaVerTicket{ID: ma.ID})
	if _, err := m.ResolverTicket.Ejecutar(sis, soporte.EntradaResolverTicket{ID: ma.ID, Version: tk.Version, Solucion: "Fibra empalmada"}); err != nil {
		t.Fatal(err)
	}
	a := jobs.tomar()
	if len(a) != 1 {
		t.Fatalf("jobs: %+v", a)
	}
	if total, err := m.AvisarHijos(sis, a[0]); err != nil || total != n {
		t.Fatalf("avisados: %d %v", total, err)
	}
	if total, err := m.AvisarHijos(sis, a[0]); err != nil || total != 0 {
		t.Fatalf("repetido: %d %v", total, err)
	}
	var tandas int
	if err := pruebas.Pool(t).QueryRow(context.Background(),
		"SELECT count(*) FROM auditoria.acciones WHERE accion = 'tickets.avisar_hijos' AND entidad_id = $1::bigint::text", ma.ID).Scan(&tandas); err != nil || tandas != 2 {
		t.Fatalf("tandas auditadas: %d %v", tandas, err)
	}
}

func TestRevisionPortalTopes(t *testing.T) {
	registrarEmpresas(t, 96)
	r := &reloj{t: time.Now()}
	m := soporte.Nuevo(soporte.Config{Reloj: r.ahora, Portal: soporte.LimitesPortal{TicketsAbiertos: 2, TicketsPorHora: 3, MensajesPorMinuto: 2}})
	cli := pruebas.ContextoCliente(pruebas.Pool(t), 9601, 96)
	crearP := func(clave string) (soporte.TicketPortal, error) {
		return m.CrearTicketPortal.Ejecutar(cli, soporte.EntradaCrearTicketPortal{Asunto: "Sin señal", Descripcion: "Nada", ClaveIdempotencia: clave})
	}
	t1, err := crearP("a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := crearP("b"); err != nil {
		t.Fatal(err)
	}
	// Tercer ticket abierto: 429. La creación repetida con la misma clave no cuenta.
	_, err = crearP("c")
	problema(t, err, 429, "demasiados_tickets")
	if rep, err := crearP("a"); err != nil || rep.ID != t1.ID {
		t.Fatalf("repetida: %+v %v", rep, err)
	}
	// Otro cliente de la misma empresa no se ve afectado.
	if _, err := m.CrearTicketPortal.Ejecutar(pruebas.ContextoCliente(pruebas.Pool(t), 9602, 96),
		soporte.EntradaCrearTicketPortal{Asunto: "Otro", Descripcion: "Otro"}); err != nil {
		t.Fatal(err)
	}
	// Con uno cerrado baja el conteo de abiertos, pero el tope por hora sigue: 3 creados en la hora.
	if _, err := pruebas.Pool(t).Exec(context.Background(),
		"UPDATE soporte.tickets SET estado = 'cerrado', cerrado_en = now() WHERE id = $1", t1.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := crearP("d"); err != nil {
		t.Fatal(err)
	}
	if _, err := pruebas.Pool(t).Exec(context.Background(),
		"UPDATE soporte.tickets SET estado = 'cerrado', cerrado_en = now() WHERE cliente_id = 9601 AND empresa_id = 96"); err != nil {
		t.Fatal(err)
	}
	_, err = crearP("e")
	problema(t, err, 429, "demasiados_tickets")

	// Mensajes: 2 por minuto en todos sus tickets (la descripción del ticket cuenta: es un
	// mensaje del cliente por el portal).
	abierto, err := m.CrearTicketPortal.Ejecutar(pruebas.ContextoCliente(pruebas.Pool(t), 9603, 96), soporte.EntradaCrearTicketPortal{Asunto: "Lento", Descripcion: "Va lento"})
	if err != nil {
		t.Fatal(err)
	}
	cli3 := pruebas.ContextoCliente(pruebas.Pool(t), 9603, 96)
	if _, err := m.ResponderTicketPortal.Ejecutar(cli3, soporte.EntradaResponderTicketPortal{ID: abierto.ID, Cuerpo: "Uno"}); err != nil {
		t.Fatal(err)
	}
	_, err = m.ResponderTicketPortal.Ejecutar(cli3, soporte.EntradaResponderTicketPortal{ID: abierto.ID, Cuerpo: "Dos"})
	problema(t, err, 429, "demasiados_mensajes")
}

// Un mensaje del cliente en un ticket nuevo o abierto no cambia su versión (no hubo cambio).
func TestRevisionPortalMensajeNoSubeVersion(t *testing.T) {
	registrarEmpresas(t, 97)
	m, _ := nuevoModulo(time.Now())
	cli := pruebas.ContextoCliente(pruebas.Pool(t), 9701, 97)
	tk, err := m.CrearTicketPortal.Ejecutar(cli, soporte.EntradaCrearTicketPortal{Asunto: "Sin señal", Descripcion: "Nada"})
	if err != nil {
		t.Fatal(err)
	}
	r, err := m.ResponderTicketPortal.Ejecutar(cli, soporte.EntradaResponderTicketPortal{ID: tk.ID, Cuerpo: "¿Hay novedades?", Version: &tk.Version})
	if err != nil || r.Ticket.Version != tk.Version || r.Ticket.Estado != "nuevo" {
		t.Fatalf("versión tras el mensaje: %+v %v", r.Ticket, err)
	}
}
