package soporte_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
	"github.com/HCDestroyer/iris/apps/api/internal/soporte"
)

// Caídas repetidas y falla general (contrato Iris ↔ ArgosISP 0.13.0 §3.11; B29 y B30).

func caidas(empresa, cliente, servicio int64, sesiones int) soporte.EntradaCaidas {
	return soporte.EntradaCaidas{EntranteID: nuevoEntrante(), ServicioID: servicio, EmpresaID: empresa, ClienteID: &cliente, Sesiones: sesiones,
		VentanaMinutos: 60}
}

func aplicarRed(t *testing.T, m *soporte.Modulo, evento string, payload any) {
	t.Helper()
	if err := m.AlEventoArgosRed(pruebas.ContextoSistema(pruebas.Pool(t)), eventoHorus(t, evento, payload)); err != nil {
		t.Fatal(err)
	}
}

func resultadoRed(t *testing.T, entrante int64) (string, *int64) {
	t.Helper()
	var r string
	var tk *int64
	if err := pruebas.Pool(t).QueryRow(context.Background(),
		"SELECT resultado, ticket_id FROM soporte.red_eventos WHERE entrante_id = $1", entrante).Scan(&r, &tk); err != nil {
		t.Fatalf("red_eventos %d: %v", entrante, err)
	}
	return r, tk
}

func exigirRed(t *testing.T, entrante int64, resultado string) *int64 {
	t.Helper()
	r, tk := resultadoRed(t, entrante)
	if r != resultado {
		t.Fatalf("entrante %d: %s (esperaba %s)", entrante, r, resultado)
	}
	return tk
}

func categoriaID(t *testing.T, codigo string) int64 {
	t.Helper()
	var id int64
	if err := pruebas.Pool(t).QueryRow(context.Background(), "SELECT id FROM soporte.categorias WHERE empresa_id IS NULL AND codigo = $1", codigo).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func contarSQL(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := pruebas.Pool(t).QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func publicos(t *testing.T, ticket int64) int {
	return contarSQL(t, "SELECT count(*) FROM soporte.ticket_mensajes WHERE ticket_id = $1 AND publico", ticket)
}

func TestCaidasRepetidasYPosibleFalla(t *testing.T) {
	registrarEmpresas(t, 81)
	for i := int64(1); i <= 5; i++ {
		instantanea(t, 81, 8100+i, 810000+i, 8100)
	}
	m, _, _ := nuevoModuloConCola(time.Now().UTC())

	// Bajo el umbral de fábrica (3 en 60 min): nada.
	c := caidas(81, 8101, 810001, 2)
	aplicarRed(t, m, argos.EventoInternoCaidas, c)
	exigirRed(t, c.EntranteID, soporte.RedBajoUmbral)
	// Con 3: un ticket de caídas del servicio; repetido, nada más.
	c = caidas(81, 8101, 810001, 3)
	aplicarRed(t, m, argos.EventoInternoCaidas, c)
	aplicarRed(t, m, argos.EventoInternoCaidas, c)
	tk := exigirRed(t, c.EntranteID, soporte.RedTicketCreado)
	ts := ticketsDe(t, "t.servicio_id = 810001")
	if len(ts) != 1 || ts[0].ID != *tk || ts[0].Origen != "argos" || ts[0].Categoria == nil || *ts[0].Categoria != soporte.CategoriaCaidas {
		t.Fatalf("ticket de caídas: %+v", ts)
	}
	// Otro aviso del mismo servicio con el ticket abierto: nota.
	c = caidas(81, 8101, 810001, 5)
	aplicarRed(t, m, argos.EventoInternoCaidas, c)
	if got := exigirRed(t, c.EntranteID, soporte.RedNotaTicket); *got != *tk {
		t.Fatal("la nota va al ticket abierto")
	}
	// Ventana más larga que la de la empresa: no cuenta.
	c = caidas(81, 8102, 810002, 9)
	c.VentanaMinutos = 120
	aplicarRed(t, m, argos.EventoInternoCaidas, c)
	exigirRed(t, c.EntranteID, soporte.RedBajoUmbral)

	// Umbral de posible falla de la empresa: 2 tickets en 10 min.
	if _, err := pruebas.Pool(t).Exec(context.Background(), `INSERT INTO soporte.red_ajustes (empresa_id, falla_umbral) VALUES (81, 2)`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pruebas.Pool(t).Exec(context.Background(), "DELETE FROM soporte.red_ajustes WHERE empresa_id = 81")
	})
	c = caidas(81, 8102, 810002, 4)
	aplicarRed(t, m, argos.EventoInternoCaidas, c)
	exigirRed(t, c.EntranteID, soporte.RedTicketCreado)
	// El tercero ya no abre ticket: posible falla general, con los de los 10 min agrupados y aviso.
	c = caidas(81, 8103, 810003, 4)
	aplicarRed(t, m, argos.EventoInternoCaidas, c)
	posible := exigirRed(t, c.EntranteID, soporte.RedPosibleCreada)
	if ps := ticketsDe(t, "t.id = $1", *posible); ps[0].Maestro == nil || *ps[0].Maestro != soporte.MaestroPosibleFalla || ps[0].Prioridad != "alta" {
		t.Fatalf("posible falla: %+v", ps)
	}
	if hijos := ticketsDe(t, "t.ticket_padre_id = $1", *posible); len(hijos) != 2 {
		t.Fatalf("agrupados: %+v", hijos)
	}
	if n := contarSQL(t, "SELECT count(*) FROM integracion.outbox WHERE evento = 'soporte.posible_falla_general' AND (payload->>'ticket_id')::bigint = $1", *posible); n != 1 {
		t.Fatalf("aviso a supervisión: %d", n)
	}
	if n := len(ticketsDe(t, "t.servicio_id = 810003")); n != 0 {
		t.Fatalf("no se abre el tercero: %d", n)
	}
	// Mientras siga abierta: notas en ella.
	c = caidas(81, 8104, 810004, 4)
	aplicarRed(t, m, argos.EventoInternoCaidas, c)
	if got := exigirRed(t, c.EntranteID, soporte.RedNotaPosible); *got != *posible {
		t.Fatal("nota en la posible falla")
	}
	// Empresa que Iris no tiene: no hace nada.
	c = caidas(8999, 8101, 810001, 4)
	aplicarRed(t, m, argos.EventoInternoCaidas, c)
	exigirRed(t, c.EntranteID, soporte.RedEmpresaNoReg)
}

func TestFallaGeneral(t *testing.T) {
	registrarEmpresas(t, 82)
	instantanea(t, 82, 8201, 820101, 8200)
	instantanea(t, 82, 8202, 820201, 8200)
	instantanea(t, 82, 8203, 820301, 8200)
	inicio := time.Now().UTC()
	m, reloj, jobs := nuevoModuloConCola(inicio)
	yo := agente(t)
	asignarEmpresa(t, yo, 82)
	ctx := comoEnEmpresa(t, yo, 82)
	sinServicio, facturacion := categoriaID(t, "sin_servicio"), categoriaID(t, "facturacion")
	// Antes de la falla: uno de conectividad (se absorbe), uno de facturación (no) y uno viejo (no).
	h1 := crear(t, m, ctx, soporte.EntradaCrearTicket{ClienteID: ptr(int64(8201)), ServicioID: ptr(int64(820101)), CategoriaID: &sinServicio})
	fac := crear(t, m, ctx, soporte.EntradaCrearTicket{ClienteID: ptr(int64(8202)), ServicioID: ptr(int64(820201)), CategoriaID: &facturacion})
	viejo := crear(t, m, ctx, soporte.EntradaCrearTicket{ClienteID: ptr(int64(8202)), ServicioID: ptr(int64(820201)), CategoriaID: &sinServicio})
	desde := inicio.Add(-time.Hour)
	if _, err := pruebas.Pool(t).Exec(context.Background(), "UPDATE soporte.tickets SET created_at = $2 WHERE id = $1", viejo.ID, desde.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	abrir := func(incidente, alcanceID string, servicios ...int64) soporte.EntradaFalla {
		return soporte.EntradaFalla{EntranteID: nuevoEntrante(), IncidenteID: incidente, EmpresaID: 82, Alcance: "router", AlcanceID: alcanceID,
			Servicios: servicios, Desde: &desde}
	}
	f := abrir("inc-82a", "8200", 820101, 820201)
	aplicarRed(t, m, argos.EventoInternoFallaAbierta, f)
	aplicarRed(t, m, argos.EventoInternoFallaAbierta, f) // repetido
	general := exigirRed(t, f.EntranteID, soporte.RedFallaCreada)
	g := ticketsDe(t, "t.id = $1", *general)[0]
	if g.Maestro == nil || *g.Maestro != soporte.MaestroFallaGeneral || g.Origen != "argos" || g.Prioridad != "alta" {
		t.Fatalf("ticket general: %+v", g)
	}
	padreDe := func(id int64) *int64 { return ticketsDe(t, "t.id = $1", id)[0].Padre }
	if p := padreDe(h1.ID); p == nil || *p != *general {
		t.Fatal("absorbe el de conectividad creado después de desde")
	}
	if padreDe(fac.ID) != nil || padreDe(viejo.ID) != nil {
		t.Fatal("no absorbe otra categoría ni uno anterior a desde")
	}
	// El mismo incidente otra vez (otro webhook): uno solo.
	f2 := abrir("inc-82a", "8200", 820301)
	aplicarRed(t, m, argos.EventoInternoFallaAbierta, f2)
	if got := exigirRed(t, f2.EntranteID, soporte.RedFallaActualizada); *got != *general {
		t.Fatal("idempotente por incidente_id")
	}
	if n := len(ticketsDe(t, "t.maestro = 'falla_general' AND t.empresa_id = 82")); n != 1 {
		t.Fatalf("un ticket general: %d", n)
	}
	// Un ticket nuevo de conectividad de un servicio afectado nace hijo; el de facturación no.
	h2 := crear(t, m, ctx, soporte.EntradaCrearTicket{ClienteID: ptr(int64(8203)), ServicioID: ptr(int64(820301)), CategoriaID: &sinServicio})
	if h2.TicketPadreID == nil || *h2.TicketPadreID != *general {
		t.Fatalf("nace hijo: %+v", h2.TicketPadreID)
	}
	// Caídas de un servicio en la falla: nota en el ticket general.
	c := caidas(82, 8201, 820101, 5)
	aplicarRed(t, m, argos.EventoInternoCaidas, c)
	if got := exigirRed(t, c.EntranteID, soporte.RedNotaFalla); *got != *general {
		t.Fatal("nota en el ticket general")
	}
	// El aviso de falla: uno por cliente e incidente, sin servicio ni dirección.
	for _, a := range jobs.tomar() {
		if a.Incidente == 0 || a.Aviso != soporte.AvisoFallaAbierta {
			continue
		}
		if _, err := m.AvisarHijos(pruebas.ContextoSistema(pruebas.Pool(t)), a); err != nil {
			t.Fatal(err)
		}
		if _, err := m.AvisarHijos(pruebas.ContextoSistema(pruebas.Pool(t)), a); err != nil {
			t.Fatal(err)
		}
	}
	for _, h := range []int64{h1.ID, h2.ID} {
		if n := publicos(t, h); n != 1 {
			t.Fatalf("aviso de falla al hijo %d: %d", h, n)
		}
	}
	var texto string
	if err := pruebas.Pool(t).QueryRow(context.Background(), "SELECT cuerpo FROM soporte.ticket_mensajes WHERE ticket_id = $1 AND publico", h1.ID).Scan(&texto); err != nil ||
		!strings.Contains(texto, "falla en la red de tu zona") || strings.Contains(texto, "820101") {
		t.Fatalf("texto del aviso: %q %v", texto, err)
	}

	// Cada aviso por cliente pidió también el de WhatsApp (§6.5 lo aplica la mensajería).
	if n := contarSQL(t, "SELECT count(*) FROM integracion.outbox WHERE evento = 'soporte.aviso_falla' AND payload->>'tipo' = 'apertura' AND (payload->>'cliente_id')::bigint IN (8201, 8203)"); n != 2 {
		t.Fatalf("soporte.aviso_falla de apertura: %d", n)
	}
	// Un ticket vinculado a mano al general (no lo absorbió el incidente) no se toca al cerrar.
	ajeno := crear(t, m, ctx, soporte.EntradaCrearTicket{ClienteID: ptr(int64(8202)), ServicioID: ptr(int64(820201)), CategoriaID: &facturacion, TicketPadreID: general})
	// h1 lo atendió un agente (abierto); h2 sigue nuevo.
	if _, err := pruebas.Pool(t).Exec(context.Background(), "UPDATE soporte.tickets SET estado = 'abierto' WHERE id = $1", h1.ID); err != nil {
		t.Fatal(err)
	}

	// Cierre: el general se resuelve; el hijo abierto pasa a «en espera» (verificar con el cliente) y el
	// nuevo se queda igual, con una nota.
	cerrada := f
	cerrada.EntranteID = nuevoEntrante()
	aplicarRed(t, m, argos.EventoInternoFallaCerrada, cerrada)
	exigirRed(t, cerrada.EntranteID, soporte.RedFallaCerrada)
	if g := ticketsDe(t, "t.id = $1", *general)[0]; g.Estado != "resuelto" {
		t.Fatalf("general: %s", g.Estado)
	}
	if e := ticketsDe(t, "t.id = $1", h1.ID)[0].Estado; e != "en_espera" {
		t.Fatalf("hijo abierto: %s", e)
	}
	if e := ticketsDe(t, "t.id = $1", h2.ID)[0].Estado; e != "nuevo" {
		t.Fatalf("hijo nuevo: %s", e)
	}
	if e := ticketsDe(t, "t.id = $1", ajeno.ID)[0].Estado; e != "nuevo" || publicos(t, ajeno.ID) != 0 {
		t.Fatalf("ajeno: %s %d", e, publicos(t, ajeno.ID))
	}
	avisarCierre := func() {
		for _, a := range jobs.tomar() {
			if a.Aviso == soporte.AvisoFallaCerrada {
				if _, err := m.AvisarHijos(pruebas.ContextoSistema(pruebas.Pool(t)), a); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	avisarCierre()
	if publicos(t, h1.ID) != 2 || publicos(t, h2.ID) != 2 || publicos(t, ajeno.ID) != 0 {
		t.Fatal("un aviso de cierre por cliente, solo a los hijos del incidente")
	}

	// El mismo alcance vuelve antes de 30 min: se reusa el ticket general (reabierto).
	reloj.avanzar(10 * time.Minute)
	f3 := abrir("inc-82b", "8200", 820101)
	aplicarRed(t, m, argos.EventoInternoFallaAbierta, f3)
	if got := exigirRed(t, f3.EntranteID, soporte.RedFallaReusada); *got != *general {
		t.Fatal("reusa el ticket general")
	}
	if g := ticketsDe(t, "t.id = $1", *general)[0]; g.Estado != "abierto" {
		t.Fatalf("reabierto: %s", g.Estado)
	}
	// Un ticket nuevo del mismo cliente en este incidente: el aviso de apertura no se repite antes de 6 h.
	h3 := crear(t, m, ctx, soporte.EntradaCrearTicket{ClienteID: ptr(int64(8201)), ServicioID: ptr(int64(820101)), CategoriaID: &sinServicio})
	if h3.TicketPadreID == nil || *h3.TicketPadreID != *general {
		t.Fatal("nace hijo del reusado")
	}
	for _, a := range jobs.tomar() {
		if _, err := m.AvisarHijos(pruebas.ContextoSistema(pruebas.Pool(t)), a); err != nil {
			t.Fatal(err)
		}
	}
	if publicos(t, h3.ID) != 0 {
		t.Fatal("máximo un aviso de apertura cada 6 h por cliente")
	}
	// Su cierre no repite el aviso de cierre antes de 6 h.
	c3 := f3
	c3.EntranteID = nuevoEntrante()
	aplicarRed(t, m, argos.EventoInternoFallaCerrada, c3)
	avisarCierre()
	if publicos(t, h1.ID) != 2 || publicos(t, h3.ID) != 0 {
		t.Fatal("máximo un aviso de cierre cada 6 h")
	}
	// Pasados 30 min sin volver, el general se cierra.
	reloj.avanzar(31 * time.Minute)
	if ok, err := m.CerrarFallaGeneral.Ejecutar(pruebas.ContextoSistema(pruebas.Pool(t)), soporte.ArgsCerrarFallaGeneral{TicketID: *general}); err != nil || !ok {
		t.Fatalf("cerrar a los 30 min: %v %v", ok, err)
	}
	if g := ticketsDe(t, "t.id = $1", *general)[0]; g.Estado != "cerrado" {
		t.Fatalf("cerrado: %s", g.Estado)
	}
	// Otro alcance pasados los 30 min: ticket nuevo; un cierre sin incidente conocido, nada.
	f4 := abrir("inc-82c", "8299", 820201)
	aplicarRed(t, m, argos.EventoInternoFallaAbierta, f4)
	if got := exigirRed(t, f4.EntranteID, soporte.RedFallaCreada); *got == *general {
		t.Fatal("ticket nuevo")
	}
	// Un cierre de un incidente que Iris no conoce: queda la marca y su apertura atrasada no abre nada.
	x := abrir("inc-desconocido", "1", 820201)
	aplicarRed(t, m, argos.EventoInternoFallaCerrada, x)
	exigirRed(t, x.EntranteID, soporte.RedCierreAntes)
	x2 := abrir("inc-desconocido", "1", 820201)
	aplicarRed(t, m, argos.EventoInternoFallaAbierta, x2)
	if got := exigirRed(t, x2.EntranteID, soporte.RedYaCerrada); got != nil {
		t.Fatal("no abre")
	}
	// Una apertura que ya trae hasta tampoco abre.
	x3 := abrir("inc-con-hasta", "2", 820201)
	x3.Hasta = ptr(time.Now().UTC())
	aplicarRed(t, m, argos.EventoInternoFallaAbierta, x3)
	exigirRed(t, x3.EntranteID, soporte.RedYaCerrada)
	if n := len(ticketsDe(t, "t.asunto LIKE 'Falla general en router 1' OR t.asunto LIKE 'Falla general en router 2'")); n != 0 {
		t.Fatalf("sin ticket: %d", n)
	}

	// El cliente de h1 no respondió a la verificación: a las 24 h de fábrica vuelve a abierto con aviso.
	var desdeEspera time.Time
	if err := pruebas.Pool(t).QueryRow(context.Background(), "SELECT en_espera_desde FROM soporte.tickets WHERE id = $1", h1.ID).Scan(&desdeEspera); err != nil {
		t.Fatal(err)
	}
	venc := soporte.ArgsVencerVerificacion{TicketID: h1.ID, Desde: desdeEspera, Vence: desdeEspera.Add(24 * time.Hour)}
	if ok, err := m.VencerVerificacion.Ejecutar(pruebas.ContextoSistema(pruebas.Pool(t)), venc); err != nil || ok {
		t.Fatalf("antes de vencer: %v %v", ok, err)
	}
	reloj.avanzar(25 * time.Hour)
	if ok, err := m.VencerVerificacion.Ejecutar(pruebas.ContextoSistema(pruebas.Pool(t)), venc); err != nil || !ok {
		t.Fatalf("vencer: %v %v", ok, err)
	}
	if e := ticketsDe(t, "t.id = $1", h1.ID)[0].Estado; e != "abierto" {
		t.Fatalf("vuelve a abierto: %s", e)
	}
	if n := contar(eventosDe(t, h1.ID), "soporte.verificacion_vencida"); n != 1 {
		t.Fatalf("soporte.verificacion_vencida: %d", n)
	}
}

// Si Horus ya abrió el maestro del router, ese es el ticket general (no se crea otro).
func TestFallaGeneralConMaestroDeHorus(t *testing.T) {
	registrarEmpresas(t, 83)
	instantanea(t, 83, 8301, 830101, 8300)
	m, _, _ := nuevoModuloConCola(time.Now().UTC())
	e, p := router(ptr(int64(83)), 8300, "caido")
	aplicarHorus(t, m, eventoHorus(t, "horus.router_estado_cambiado", p))
	_, maestro := resultadoHorus(t, e)
	desde := time.Now().UTC().Add(-time.Hour)
	f := soporte.EntradaFalla{EntranteID: nuevoEntrante(), IncidenteID: "inc-83", EmpresaID: 83, Alcance: "router", AlcanceID: "8300",
		Servicios: []int64{830101}, Desde: &desde}
	aplicarRed(t, m, argos.EventoInternoFallaAbierta, f)
	if got := exigirRed(t, f.EntranteID, soporte.RedFallaHorus); *got != *maestro {
		t.Fatal("se vincula al maestro de Horus")
	}
	if n := len(ticketsDe(t, "t.maestro = 'falla_general' AND t.empresa_id = 83")); n != 0 {
		t.Fatalf("no se crea otro: %d", n)
	}
	hijo := ticketsDe(t, "t.servicio_id = 830101 AND t.maestro IS NULL")
	yo := agente(t)
	asignarEmpresa(t, yo, 83)
	ctx := comoEnEmpresa(t, yo, 83)
	sinServicio := categoriaID(t, "sin_servicio")
	h := crear(t, m, ctx, soporte.EntradaCrearTicket{ClienteID: ptr(int64(8301)), ServicioID: ptr(int64(830101)), CategoriaID: &sinServicio})
	if h.TicketPadreID == nil || *h.TicketPadreID != *maestro || len(hijo) != 0 {
		t.Fatalf("nace hijo del maestro: %+v", h.TicketPadreID)
	}
	if _, err := pruebas.Pool(t).Exec(context.Background(), "UPDATE soporte.tickets SET estado = 'abierto' WHERE id = $1", h.ID); err != nil {
		t.Fatal(err)
	}
	// Al cerrarse, ni el maestro de Horus ni sus hijos cambian, ni hay aviso de cierre: esperan a Horus.
	c := f
	c.EntranteID = nuevoEntrante()
	aplicarRed(t, m, argos.EventoInternoFallaCerrada, c)
	if g := ticketsDe(t, "t.id = $1", *maestro)[0]; g.Estado != "nuevo" {
		t.Fatalf("maestro de Horus: %s", g.Estado)
	}
	if e := ticketsDe(t, "t.id = $1", h.ID)[0].Estado; e != "abierto" {
		t.Fatalf("hijo: %s", e)
	}
	// Horus confirma que el router volvió: el hijo se resuelve solo, con un mensaje al cliente.
	_, arriba := router(ptr(int64(83)), 8300, "arriba")
	aplicarHorus(t, m, eventoHorus(t, "horus.router_estado_cambiado", arriba))
	if e := ticketsDe(t, "t.id = $1", h.ID)[0].Estado; e != "resuelto" || publicos(t, h.ID) != 1 {
		t.Fatalf("resuelto por Horus: %s %d", e, publicos(t, h.ID))
	}
}

// Los servicios que el conector verificó después de la apertura se suman y absorben sus tickets; un
// ticket general que el job de los 30 min ya cerró no se reusa: se abre otro.
func TestFallaGeneralServiciosVerificadosYReusoCerrado(t *testing.T) {
	registrarEmpresas(t, 84)
	instantanea(t, 84, 8401, 840101, 8400)
	instantanea(t, 84, 8402, 840201, 8400)
	inicio := time.Now().UTC()
	m, reloj, _ := nuevoModuloConCola(inicio)
	yo := agente(t)
	asignarEmpresa(t, yo, 84)
	ctx := comoEnEmpresa(t, yo, 84)
	sinServicio := categoriaID(t, "sin_servicio")
	h := crear(t, m, ctx, soporte.EntradaCrearTicket{ClienteID: ptr(int64(8402)), ServicioID: ptr(int64(840201)), CategoriaID: &sinServicio})
	desde := inicio.Add(-time.Hour)
	f := soporte.EntradaFalla{EntranteID: nuevoEntrante(), IncidenteID: "inc-84", EmpresaID: 84, Alcance: "olt", AlcanceID: "olt-1",
		Servicios: []int64{840101}, PorVerificar: 1, Desde: &desde}
	aplicarRed(t, m, argos.EventoInternoFallaAbierta, f)
	general := exigirRed(t, f.EntranteID, soporte.RedFallaCreada)
	if padre := ticketsDe(t, "t.id = $1", h.ID)[0].Padre; padre != nil {
		t.Fatal("todavía no estaba verificado")
	}
	aplicarRed(t, m, argos.EventoInternoFallaServicios, soporte.EntradaFallaServicios{EntranteID: f.EntranteID, IncidenteID: "inc-84", EmpresaID: 84,
		Servicios: []int64{840201}, Descartados: 2, NoVerificados: 1})
	if padre := ticketsDe(t, "t.id = $1", h.ID)[0].Padre; padre == nil || *padre != *general {
		t.Fatal("absorbe el verificado")
	}
	// Cierre, 31 min y el job cierra el general; el mismo alcance vuelve: ticket nuevo.
	c := f
	c.EntranteID = nuevoEntrante()
	aplicarRed(t, m, argos.EventoInternoFallaCerrada, c)
	reloj.avanzar(31 * time.Minute)
	if ok, err := m.CerrarFallaGeneral.Ejecutar(pruebas.ContextoSistema(pruebas.Pool(t)), soporte.ArgsCerrarFallaGeneral{TicketID: *general}); err != nil || !ok {
		t.Fatalf("cerrar: %v %v", ok, err)
	}
	// La ventana de reuso se mide con cerrada_en: se fuerza dentro de los 30 min para probar la carrera.
	if _, err := pruebas.Pool(t).Exec(context.Background(), "UPDATE soporte.incidentes_red SET cerrada_en = $1 WHERE incidente_id = 'inc-84' AND empresa_id = 84",
		reloj.ahora().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	f2 := soporte.EntradaFalla{EntranteID: nuevoEntrante(), IncidenteID: "inc-84b", EmpresaID: 84, Alcance: "olt", AlcanceID: "olt-1",
		Servicios: []int64{840101}, Desde: &desde}
	aplicarRed(t, m, argos.EventoInternoFallaAbierta, f2)
	if got := exigirRed(t, f2.EntranteID, soporte.RedFallaCreada); *got == *general {
		t.Fatal("un general cerrado no se reusa")
	}
}
