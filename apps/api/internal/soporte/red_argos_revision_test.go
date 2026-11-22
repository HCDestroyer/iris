package soporte_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/horus"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
	"github.com/HCDestroyer/iris/apps/api/internal/soporte"
	"github.com/HCDestroyer/iris/apps/api/migraciones"
)

// Segunda revisión de la falla general (B29, B30): la confirmación de Horus, los avisos y la posible falla.

func ejecutarSQL(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := pruebas.Pool(t).Exec(context.Background(), sql, args...); err != nil {
		t.Fatal(err)
	}
}

func fallaRouter(empresa int64, incidente, router string, desde time.Time, servicios ...int64) soporte.EntradaFalla {
	return soporte.EntradaFalla{EntranteID: nuevoEntrante(), IncidenteID: incidente, EmpresaID: empresa, Alcance: "router", AlcanceID: router,
		Servicios: servicios, Desde: &desde}
}

func cerrarFallaRed(t *testing.T, m *soporte.Modulo, f soporte.EntradaFalla) {
	t.Helper()
	c := f
	c.EntranteID = nuevoEntrante()
	aplicarRed(t, m, argos.EventoInternoFallaCerrada, c)
	exigirRed(t, c.EntranteID, soporte.RedFallaCerrada)
}

func routerArriba(t *testing.T, m *soporte.Modulo, empresa, routerID int64, ocurrido time.Time) {
	t.Helper()
	aplicarHorus(t, m, eventoHorus(t, horus.EventoRouterEstadoCambiado, horus.RouterEstadoCambiado{RouterID: routerID, Nombre: "RT", Estado: horus.EstadoArriba,
		OcurridoEn: ocurrido, EmpresaID: &empresa, EntranteID: nuevoEntrante()}))
}

func estadoDe(t *testing.T, id int64) string {
	t.Helper()
	return ticketsDe(t, "t.id = $1", id)[0].Estado
}

// M1: Horus resuelve solo a los hijos que esperan verificar tras el cierre de su falla, del ticket
// general de ese incidente, dentro de las horas de verificación y si la falla empezó antes de su aviso.
func TestFallaGeneralConfirmacionDeHorusAcotada(t *testing.T) {
	registrarEmpresas(t, 85)
	for i := int64(1); i <= 4; i++ {
		instantanea(t, 85, 8500+i, 850000+i, 8500)
	}
	instantanea(t, 85, 8505, 850005, 8501)
	instantanea(t, 85, 8506, 850006, 8502)
	instantanea(t, 85, 8507, 850007, 8503)
	inicio := time.Now().UTC()
	m, reloj, jobs := nuevoModuloConCola(inicio)
	yo := agente(t)
	asignarEmpresa(t, yo, 85)
	ctx := comoEnEmpresa(t, yo, 85)
	sinServicio := categoriaID(t, "sin_servicio")
	hijo := func(cliente, servicio int64, estado string) int64 {
		h := crear(t, m, ctx, soporte.EntradaCrearTicket{ClienteID: &cliente, ServicioID: &servicio, CategoriaID: &sinServicio})
		ejecutarSQL(t, "UPDATE soporte.tickets SET estado = $2 WHERE id = $1", h.ID, estado)
		return h.ID
	}
	espera, vuelto, desvinculado := hijo(8501, 850001, "abierto"), hijo(8502, 850002, "abierto"), hijo(8503, 850003, "abierto")
	nuevo := hijo(8504, 850004, "nuevo")
	desde := inicio.Add(-time.Hour)
	f := fallaRouter(85, "inc-85a", "8500", desde, 850001, 850002, 850003, 850004)
	aplicarRed(t, m, argos.EventoInternoFallaAbierta, f)
	general := exigirRed(t, f.EntranteID, soporte.RedFallaCreada)
	cerrarFallaRed(t, m, f)
	for _, id := range []int64{espera, vuelto, desvinculado} {
		if e := estadoDe(t, id); e != "en_espera" {
			t.Fatalf("hijo %d tras el cierre: %s", id, e)
		}
	}
	// Uno volvió a abierto (la verificación venció) y otro lo desvinculó una persona.
	ejecutarSQL(t, "UPDATE soporte.tickets SET estado = 'abierto', en_espera_desde = NULL WHERE id = $1", vuelto)
	ejecutarSQL(t, "UPDATE soporte.tickets SET ticket_padre_id = NULL WHERE id = $1", desvinculado)
	routerArriba(t, m, 85, 8500, time.Now().UTC())
	if e := estadoDe(t, espera); e != "resuelto" {
		t.Fatalf("el que esperaba se resuelve: %s", e)
	}
	if estadoDe(t, vuelto) != "abierto" || estadoDe(t, desvinculado) != "en_espera" || estadoDe(t, nuevo) != "nuevo" {
		t.Fatalf("fuera del incidente: vuelto %s, desvinculado %s, nuevo %s", estadoDe(t, vuelto), estadoDe(t, desvinculado), estadoDe(t, nuevo))
	}
	if g := ticketsDe(t, "t.id = $1", *general)[0]; g.Estado != "resuelto" {
		t.Fatalf("general: %s", g.Estado)
	}

	// Horus confirmó antes del cierre: el hijo se resuelve al cerrar y no recibe también «falla resuelta».
	confirmado := hijo(8505, 850005, "abierto")
	fb := fallaRouter(85, "inc-85b", "8501", desde, 850005)
	aplicarRed(t, m, argos.EventoInternoFallaAbierta, fb)
	jobs.tomar()
	routerArriba(t, m, 85, 8501, time.Now().UTC())
	cerrarFallaRed(t, m, fb)
	for _, a := range jobs.tomar() {
		if _, err := m.AvisarHijos(pruebas.ContextoSistema(pruebas.Pool(t)), a); err != nil {
			t.Fatal(err)
		}
	}
	if estadoDe(t, confirmado) != "resuelto" || publicos(t, confirmado) != 1 {
		t.Fatalf("confirmado por Horus: %s, %d públicos", estadoDe(t, confirmado), publicos(t, confirmado))
	}

	// Un «arriba» de Horus anterior al inicio de la falla no la confirma.
	anterior := hijo(8506, 850006, "abierto")
	fc := fallaRouter(85, "inc-85c", "8502", inicio.Add(-5*time.Minute), 850006)
	aplicarRed(t, m, argos.EventoInternoFallaAbierta, fc)
	cerrarFallaRed(t, m, fc)
	routerArriba(t, m, 85, 8502, inicio.Add(-10*time.Minute))
	if e := estadoDe(t, anterior); e != "en_espera" {
		t.Fatalf("arriba anterior a la falla: %s", e)
	}

	// Pasadas las horas de verificación (24 de fábrica) desde el cierre, tampoco.
	tarde := hijo(8507, 850007, "abierto")
	fd := fallaRouter(85, "inc-85d", "8503", desde, 850007)
	aplicarRed(t, m, argos.EventoInternoFallaAbierta, fd)
	cerrarFallaRed(t, m, fd)
	reloj.avanzar(25 * time.Hour)
	routerArriba(t, m, 85, 8503, time.Now().UTC())
	if e := estadoDe(t, tarde); e != "en_espera" {
		t.Fatalf("fuera de la ventana de verificación: %s", e)
	}
}

// M2: una apertura no sale después del cierre (el job llega tarde, o el ticket se crea mientras se cierra).
func TestFallaGeneralAperturaDespuesDelCierre(t *testing.T) {
	registrarEmpresas(t, 86)
	instantanea(t, 86, 8601, 860001, 8600)
	instantanea(t, 86, 8602, 860002, 8601)
	m, _, jobs := nuevoModuloConCola(time.Now().UTC())
	yo := agente(t)
	asignarEmpresa(t, yo, 86)
	ctx := comoEnEmpresa(t, yo, 86)
	sinServicio := categoriaID(t, "sin_servicio")
	desde := time.Now().UTC().Add(-time.Hour)
	f := fallaRouter(86, "inc-86a", "8600", desde, 860001)
	aplicarRed(t, m, argos.EventoInternoFallaAbierta, f)
	h := crear(t, m, ctx, soporte.EntradaCrearTicket{ClienteID: ptr(int64(8601)), ServicioID: ptr(int64(860001)), CategoriaID: &sinServicio})
	if h.TicketPadreID == nil {
		t.Fatal("nace hijo")
	}
	ejecutarSQL(t, "UPDATE soporte.tickets SET estado = 'abierto' WHERE id = $1", h.ID)
	// La falla se cierra antes de que corra el job de la apertura.
	cerrarFallaRed(t, m, f)
	for _, a := range jobs.tomar() {
		if _, err := m.AvisarHijos(pruebas.ContextoSistema(pruebas.Pool(t)), a); err != nil {
			t.Fatal(err)
		}
	}
	if n := publicos(t, h.ID); n != 1 {
		t.Fatalf("solo el aviso de cierre: %d", n)
	}
	if n := contarSQL(t, "SELECT count(*) FROM soporte.avisos_hijos WHERE hijo_id = $1 AND aviso LIKE 'falla_abierta:%' AND mensaje_id IS NULL", h.ID); n != 1 {
		t.Fatalf("la apertura queda registrada sin mensaje: %d", n)
	}
	if n := contarSQL(t, "SELECT count(*) FROM integracion.outbox WHERE evento = 'soporte.aviso_falla' AND payload->>'tipo' = 'apertura' AND (payload->>'ticket_id')::bigint = $1", h.ID); n != 0 {
		t.Fatalf("sin aviso de apertura por WhatsApp: %d", n)
	}

	// Un ticket que se crea mientras el cierre está en curso espera al cierre y ya no nace hijo.
	fb := fallaRouter(86, "inc-86b", "8601", desde, 860002)
	aplicarRed(t, m, argos.EventoInternoFallaAbierta, fb)
	tx, err := pruebas.Pool(t).Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	// Como el cierre: primero IncidenteRed (FOR NO KEY UPDATE) y después el UPDATE.
	if _, err := tx.Exec(context.Background(), "SELECT id FROM soporte.incidentes_red WHERE incidente_id = 'inc-86b' AND empresa_id = 86 FOR NO KEY UPDATE"); err != nil {
		t.Fatal(err)
	}
	// Ese candado no frena a quien registra un aviso (FOR KEY SHARE de la llave foránea).
	fk, cancelar := context.WithTimeout(context.Background(), 3*time.Second)
	_, err = pruebas.Pool(t).Exec(fk, `INSERT INTO soporte.avisos_falla_clientes (incidente, cliente_id, tipo, ticket_id)
		SELECT id, 8699, 'cierre', $1 FROM soporte.incidentes_red WHERE incidente_id = 'inc-86b' AND empresa_id = 86`, h.ID)
	cancelar()
	if err != nil {
		t.Fatalf("la llave foránea no espera al cierre: %v", err)
	}
	if _, err := tx.Exec(context.Background(), "UPDATE soporte.incidentes_red SET estado = 'cerrada', cerrada_en = now() WHERE incidente_id = 'inc-86b' AND empresa_id = 86"); err != nil {
		t.Fatal(err)
	}
	type res struct {
		tk  soporte.Ticket
		err error
	}
	ch := make(chan res, 1)
	go func() {
		tk, err := m.CrearTicket.Ejecutar(ctx, soporte.EntradaCrearTicket{ClienteID: ptr(int64(8602)), ServicioID: ptr(int64(860002)), CategoriaID: &sinServicio,
			Asunto: "Sin internet"})
		ch <- res{tk, err}
	}()
	select {
	case r := <-ch:
		t.Fatalf("no espera al cierre: %+v %v", r.tk.TicketPadreID, r.err)
	case <-time.After(500 * time.Millisecond):
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	r := <-ch
	if r.err != nil || r.tk.TicketPadreID != nil {
		t.Fatalf("tras el cierre no nace hijo: %+v %v", r.tk.TicketPadreID, r.err)
	}
}

// M3: el aviso de una falla se encola aunque otro del mismo incidente esté corriendo.
func TestAvisoFallaSeEncolaAunqueOtroCorra(t *testing.T) {
	pool := pruebas.Pool(t)
	ctx := context.Background()
	cli, err := river.NewClient(riverpgxv5.New(pool), &river.Config{Schema: migraciones.EsquemaRiver})
	if err != nil {
		t.Fatal(err)
	}
	const cola = "pruebas_avisos_falla"
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM river.river_job WHERE queue = $1", cola) })
	id := time.Now().UnixNano()
	falla := soporte.ArgsAvisarHijos{MaestroID: id, Aviso: soporte.AvisoFallaAbierta, Incidente: id}
	horusAviso := soporte.ArgsAvisarHijos{MaestroID: id, Aviso: soporte.AvisoResuelto, Version: 1}
	opts := &river.InsertOpts{Queue: cola}
	for _, a := range []soporte.ArgsAvisarHijos{falla, horusAviso} {
		if _, err := cli.Insert(ctx, a, opts); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, "UPDATE river.river_job SET state = 'running', attempt = 1, attempted_at = now() WHERE queue = $1", cola); err != nil {
		t.Fatal(err)
	}
	r, err := cli.Insert(ctx, falla, opts)
	if err != nil || r.UniqueSkippedAsDuplicate {
		t.Fatalf("el aviso de falla se encola aunque otro corra: %+v %v", r, err)
	}
	r, err = cli.Insert(ctx, horusAviso, opts)
	if err != nil || !r.UniqueSkippedAsDuplicate {
		t.Fatalf("los demás avisos siguen siendo únicos: %+v %v", r, err)
	}
}

// Bajo 3: el tope de un aviso cada 6 h por cliente aguanta dos avisos de fallas distintas a la vez.
func TestAvisoFallaCandadoPorCliente(t *testing.T) {
	registrarEmpresas(t, 88)
	instantanea(t, 88, 8801, 880001, 8800)
	instantanea(t, 88, 8801, 880002, 8801)
	m, _, jobs := nuevoModuloConCola(time.Now().UTC())
	yo := agente(t)
	asignarEmpresa(t, yo, 88)
	ctx := comoEnEmpresa(t, yo, 88)
	sinServicio := categoriaID(t, "sin_servicio")
	desde := time.Now().UTC().Add(-time.Hour)
	aplicarRed(t, m, argos.EventoInternoFallaAbierta, fallaRouter(88, "inc-88a", "8800", desde, 880001))
	aplicarRed(t, m, argos.EventoInternoFallaAbierta, fallaRouter(88, "inc-88b", "8801", desde, 880002))
	a := crear(t, m, ctx, soporte.EntradaCrearTicket{ClienteID: ptr(int64(8801)), ServicioID: ptr(int64(880001)), CategoriaID: &sinServicio})
	b := crear(t, m, ctx, soporte.EntradaCrearTicket{ClienteID: ptr(int64(8801)), ServicioID: ptr(int64(880002)), CategoriaID: &sinServicio})
	var wg sync.WaitGroup
	errs := make(chan error, 10)
	for _, x := range jobs.tomar() {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := m.AvisarHijos(pruebas.ContextoSistema(pruebas.Pool(t)), x); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	if n := publicos(t, a.ID) + publicos(t, b.ID); n != 1 {
		t.Fatalf("un aviso de apertura por cliente en 6 h: %d", n)
	}
}

// M4: la posible falla general que confirmó una falla deja de tragarse las caídas fuera de su ventana y
// se resuelve al cerrarse la falla; después, una caída nueva abre su ticket por servicio (§3.11).
func TestPosibleFallaSeResuelveConLaFalla(t *testing.T) {
	registrarEmpresas(t, 87)
	for i := int64(1); i <= 5; i++ {
		instantanea(t, 87, 8700+i, 870000+i, 8700)
	}
	m, reloj, _ := nuevoModuloConCola(time.Now().UTC())
	ejecutarSQL(t, "INSERT INTO soporte.red_ajustes (empresa_id, falla_umbral) VALUES (87, 2)")
	t.Cleanup(func() {
		_, _ = pruebas.Pool(t).Exec(context.Background(), "DELETE FROM soporte.red_ajustes WHERE empresa_id = 87")
	})
	for i := int64(1); i <= 2; i++ {
		c := caidas(87, 8700+i, 870000+i, 4)
		aplicarRed(t, m, argos.EventoInternoCaidas, c)
		exigirRed(t, c.EntranteID, soporte.RedTicketCreado)
	}
	c := caidas(87, 8703, 870003, 4)
	aplicarRed(t, m, argos.EventoInternoCaidas, c)
	posible := exigirRed(t, c.EntranteID, soporte.RedPosibleCreada)
	f := fallaRouter(87, "inc-87", "8700", time.Now().UTC().Add(-time.Hour), 870001)
	aplicarRed(t, m, argos.EventoInternoFallaAbierta, f)
	general := exigirRed(t, f.EntranteID, soporte.RedFallaCreada)
	if p := ticketsDe(t, "t.id = $1", *posible)[0].Padre; p == nil || *p != *general {
		t.Fatal("la posible falla cuelga del general")
	}
	// Confirmada y fuera de su ventana: una caída de un servicio fuera de la falla abre su ticket.
	reloj.avanzar(15 * time.Minute)
	c = caidas(87, 8704, 870004, 4)
	aplicarRed(t, m, argos.EventoInternoCaidas, c)
	exigirRed(t, c.EntranteID, soporte.RedTicketCreado)
	// Al cerrarse la falla, la posible falla se resuelve.
	cerrarFallaRed(t, m, f)
	if e := estadoDe(t, *posible); e != "resuelto" {
		t.Fatalf("posible falla: %s", e)
	}
	c = caidas(87, 8705, 870005, 4)
	aplicarRed(t, m, argos.EventoInternoCaidas, c)
	exigirRed(t, c.EntranteID, soporte.RedTicketCreado)
}

// Bajo 5: si una persona reabre el general y lo vuelve a resolver, su cierre a los 30 min se programa otra vez.
func TestFallaGeneralCierreReprogramado(t *testing.T) {
	registrarEmpresas(t, 89)
	instantanea(t, 89, 8901, 890001, 8900)
	inicio := time.Now().UTC()
	m, reloj, jobs := nuevoModuloConCola(inicio)
	yo := agente(t)
	asignarEmpresa(t, yo, 89)
	ctx := comoEnEmpresa(t, yo, 89)
	f := fallaRouter(89, "inc-89", "8900", inicio.Add(-time.Hour), 890001)
	aplicarRed(t, m, argos.EventoInternoFallaAbierta, f)
	general := exigirRed(t, f.EntranteID, soporte.RedFallaCreada)
	cerrarFallaRed(t, m, f)
	if c := jobs.tomarCierres(); len(c) != 1 || c[0].ResueltoEn.IsZero() {
		t.Fatalf("cierre programado: %+v", c)
	}
	reloj.avanzar(5 * time.Minute)
	g, err := m.VerTicket.Ejecutar(ctx, soporte.EntradaVerTicket{ID: *general})
	if err != nil {
		t.Fatal(err)
	}
	g, err = m.ReabrirTicket.Ejecutar(ctx, soporte.EntradaReabrirTicket{ID: g.ID, Version: g.Version, Motivo: "Sigue fallando"})
	if err != nil {
		t.Fatal(err)
	}
	reloj.avanzar(5 * time.Minute)
	if _, err := m.ResolverTicket.Ejecutar(ctx, soporte.EntradaResolverTicket{ID: g.ID, Version: g.Version, Solucion: "Ya quedó"}); err != nil {
		t.Fatal(err)
	}
	c := jobs.tomarCierres()
	if len(c) != 1 || !c[0].ResueltoEn.Equal(reloj.ahora().Truncate(time.Microsecond)) {
		t.Fatalf("se reprograma: %+v", c)
	}
	reloj.avanzar(31 * time.Minute)
	if ok, err := m.CerrarFallaGeneral.Ejecutar(pruebas.ContextoSistema(pruebas.Pool(t)), c[0]); err != nil || !ok {
		t.Fatalf("cierre a los 30 min de la nueva resolución: %v %v", ok, err)
	}
	if e := estadoDe(t, *general); e != "cerrado" {
		t.Fatalf("general: %s", e)
	}
}

// Bajo 2 (2.ª revisión): con el general reusado, un «arriba» viejo (anterior a la falla nueva) no
// resuelve: solo cuenta el incidente más reciente del hijo. Bajo 4: al reusar, todos los hijos abiertos
// pasan al incidente nuevo, y el «falla resuelta» atrasado del incidente viejo no les llega.
func TestFallaGeneralReusoIncidenteMasReciente(t *testing.T) {
	registrarEmpresas(t, 90)
	instantanea(t, 90, 9001, 900001, 9000)
	instantanea(t, 90, 9002, 900002, 9000)
	inicio := time.Now().UTC()
	m, reloj, jobs := nuevoModuloConCola(inicio)
	yo := agente(t)
	asignarEmpresa(t, yo, 90)
	ctx := comoEnEmpresa(t, yo, 90)
	sinServicio := categoriaID(t, "sin_servicio")
	espera := crear(t, m, ctx, soporte.EntradaCrearTicket{ClienteID: ptr(int64(9001)), ServicioID: ptr(int64(900001)), CategoriaID: &sinServicio}).ID
	nuevo := crear(t, m, ctx, soporte.EntradaCrearTicket{ClienteID: ptr(int64(9002)), ServicioID: ptr(int64(900002)), CategoriaID: &sinServicio}).ID
	ejecutarSQL(t, "UPDATE soporte.tickets SET estado = 'abierto' WHERE id = $1", espera)
	a := fallaRouter(90, "inc-90a", "9000", inicio.Add(-2*time.Hour), 900001, 900002)
	aplicarRed(t, m, argos.EventoInternoFallaAbierta, a)
	general := exigirRed(t, a.EntranteID, soporte.RedFallaCreada)
	cerrarFallaRed(t, m, a)
	jobs.tomar() // el «falla resuelta» de inc-90a queda sin correr
	// Vuelve antes de 30 min: se reusa el general y los dos hijos pasan al incidente nuevo.
	reloj.avanzar(5 * time.Minute)
	b := fallaRouter(90, "inc-90b", "9000", inicio.Add(-10*time.Minute), 900001, 900002)
	aplicarRed(t, m, argos.EventoInternoFallaAbierta, b)
	if got := exigirRed(t, b.EntranteID, soporte.RedFallaReusada); *got != *general {
		t.Fatal("reusa el general")
	}
	for _, id := range []int64{espera, nuevo} {
		if n := contarSQL(t, `SELECT count(*) FROM soporte.incidente_red_hijos h JOIN soporte.incidentes_red i ON i.id = h.incidente
			WHERE h.ticket_id = $1 AND i.incidente_id = 'inc-90b'`, id); n != 1 {
			t.Fatalf("hijo %d en el incidente nuevo: %d", id, n)
		}
	}
	// El «falla resuelta» atrasado de inc-90a no les llega: están en un incidente más nuevo y abierto.
	var incA int64
	if err := pruebas.Pool(t).QueryRow(context.Background(), "SELECT id FROM soporte.incidentes_red WHERE incidente_id = 'inc-90a'").Scan(&incA); err != nil {
		t.Fatal(err)
	}
	if _, err := m.AvisarHijos(pruebas.ContextoSistema(pruebas.Pool(t)), soporte.ArgsAvisarHijos{MaestroID: *general, Aviso: soporte.AvisoFallaCerrada, Incidente: incA}); err != nil {
		t.Fatal(err)
	}
	if publicos(t, espera) != 0 || publicos(t, nuevo) != 0 {
		t.Fatalf("sin «falla resuelta» del incidente viejo: %d %d", publicos(t, espera), publicos(t, nuevo))
	}
	// Se cierra la falla nueva; un «arriba» de Horus posterior a inc-90a pero anterior a inc-90b no resuelve.
	ejecutarSQL(t, "UPDATE soporte.tickets SET estado = 'abierto' WHERE id = $1", espera)
	cerrarFallaRed(t, m, b)
	if e := estadoDe(t, espera); e != "en_espera" {
		t.Fatalf("espera tras el cierre: %s", e)
	}
	routerArriba(t, m, 90, 9000, inicio.Add(-30*time.Minute))
	if e := estadoDe(t, espera); e != "en_espera" {
		t.Fatalf("un arriba anterior a la falla reusada no resuelve: %s", e)
	}
	routerArriba(t, m, 90, 9000, time.Now().UTC())
	if e := estadoDe(t, espera); e != "resuelto" {
		t.Fatalf("el arriba nuevo sí: %s", e)
	}
}

// Bajo 3 (2.ª revisión): con un router compartido, los eventos del hijo salen con su empresa.
func TestConfirmacionRouterCompartidoConEmpresaDelHijo(t *testing.T) {
	registrarEmpresas(t, 91)
	instantanea(t, 91, 9101, 910001, 9100)
	m, _, _ := nuevoModuloConCola(time.Now().UTC())
	yo := agente(t)
	asignarEmpresa(t, yo, 91)
	ctx := comoEnEmpresa(t, yo, 91)
	sinServicio := categoriaID(t, "sin_servicio")
	h := crear(t, m, ctx, soporte.EntradaCrearTicket{ClienteID: ptr(int64(9101)), ServicioID: ptr(int64(910001)), CategoriaID: &sinServicio}).ID
	ejecutarSQL(t, "UPDATE soporte.tickets SET estado = 'abierto' WHERE id = $1", h)
	f := fallaRouter(91, "inc-91", "9100", time.Now().UTC().Add(-time.Hour), 910001)
	aplicarRed(t, m, argos.EventoInternoFallaAbierta, f)
	cerrarFallaRed(t, m, f)
	if n, err := m.ResolverConfirmadosCompartido(pruebas.ContextoSistema(pruebas.Pool(t)), 9100, time.Now().UTC()); err != nil || n != 1 {
		t.Fatalf("compartido: %d %v", n, err)
	}
	if n := contarSQL(t, `SELECT count(*) FROM integracion.outbox WHERE (payload->>'ticket_id')::bigint = $1
		AND evento IN ('ticket.estado_cambiado', 'ticket.respondido') AND created_at > now() - interval '1 minute' AND empresa_id IS DISTINCT FROM 91`, h); n != 0 {
		t.Fatalf("eventos sin la empresa del hijo: %d", n)
	}
	if n := contarSQL(t, `SELECT count(*) FROM integracion.outbox WHERE (payload->>'ticket_id')::bigint = $1 AND evento = 'ticket.respondido' AND empresa_id = 91`, h); n != 1 {
		t.Fatalf("ticket.respondido con la empresa del hijo: %d", n)
	}
	if n := contarSQL(t, `SELECT count(*) FROM auditoria.acciones WHERE accion = 'tickets.horus_incidente' AND entrada->'auditoria'->'hijos_falla_por_empresa'->>'91' = '1'`); n < 1 {
		t.Fatalf("la auditoría anota la empresa del hijo: %d", n)
	}
}
