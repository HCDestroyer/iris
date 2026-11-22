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

	// Cierre: el general se resuelve; los hijos pasan a «en espera» (verificar con el cliente).
	cerrada := f
	cerrada.EntranteID = nuevoEntrante()
	aplicarRed(t, m, argos.EventoInternoFallaCerrada, cerrada)
	exigirRed(t, cerrada.EntranteID, soporte.RedFallaCerrada)
	if g := ticketsDe(t, "t.id = $1", *general)[0]; g.Estado != "resuelto" {
		t.Fatalf("general: %s", g.Estado)
	}
	for _, h := range []int64{h1.ID, h2.ID} {
		if e := ticketsDe(t, "t.id = $1", h)[0].Estado; e != "en_espera" {
			t.Fatalf("hijo %d: %s", h, e)
		}
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
	if publicos(t, h1.ID) != 2 || publicos(t, h2.ID) != 2 {
		t.Fatal("un aviso de cierre por cliente")
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
	// Su cierre no repite el aviso de cierre antes de 6 h.
	c3 := f3
	c3.EntranteID = nuevoEntrante()
	aplicarRed(t, m, argos.EventoInternoFallaCerrada, c3)
	avisarCierre()
	if publicos(t, h1.ID) != 2 {
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
	x := abrir("inc-desconocido", "1", 820201)
	aplicarRed(t, m, argos.EventoInternoFallaCerrada, x)
	exigirRed(t, x.EntranteID, soporte.RedSinIncidente)
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
	// Al cerrarse, el maestro de Horus no se resuelve solo.
	c := f
	c.EntranteID = nuevoEntrante()
	aplicarRed(t, m, argos.EventoInternoFallaCerrada, c)
	if g := ticketsDe(t, "t.id = $1", *maestro)[0]; g.Estado != "nuevo" {
		t.Fatalf("maestro de Horus: %s", g.Estado)
	}
}
