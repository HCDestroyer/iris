package soporte_test

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	integracionpkg "github.com/HCDestroyer/iris/apps/api/internal/integracion"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
	"github.com/HCDestroyer/iris/apps/api/internal/soporte"
)

// Pruebas de regresión de la revisión de seguridad de la PR #8 (entrada por correo).

// responderYEncolar: el agente responde y corre el consumidor de ticket.respondido; devuelve el
// destinatario del correo saliente.
func responderYEncolar(t *testing.T, e escenario, ctx context.Context, tkID int64, cuerpo string) string {
	t.Helper()
	r, err := e.m.ResponderTicket.Ejecutar(ctx, soporte.EntradaResponderTicket{ID: tkID, Cuerpo: cuerpo})
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]any{"ticket_id": tkID, "mensaje_id": r.Mensaje.ID})
	if err := e.m.AlResponderTicket(e.sis, integracionpkg.Evento{Evento: "ticket.respondido", Payload: payload}); err != nil {
		t.Fatal(err)
	}
	var dest string
	if err := pruebas.Pool(t).QueryRow(context.Background(),
		"SELECT destinatario FROM soporte.correos_salientes WHERE mensaje_id = $1", r.Mensaje.ID).Scan(&dest); err != nil {
		t.Fatal(err)
	}
	return dest
}

// ultimoMensaje devuelve el último mensaje del ticket.
func ultimoMensaje(t *testing.T, tk int64) mensajeBD {
	t.Helper()
	m := mensajesDe(t, tk)
	if len(m) == 0 {
		t.Fatal("sin mensajes")
	}
	return m[len(m)-1]
}

// Punto 1 (alto): un tercero con un Message-ID del hilo (CC, reenvío) no escribe como el
// cliente, no cambia el estado y nunca recibe las respuestas.
func TestRevisionCorreoSecuestroDeHilo(t *testing.T) {
	ag := agente(t)
	ctx := como(t, ag, append(permisosTickets, permisosBuzones...)...)
	e := nuevoEscenario(t, ctx)
	cliente := pruebas.Correo("cliente")
	id1 := "a." + pruebas.Unico() + "@cliente"
	e.imap.agregar(t, crudo(cliente, "Lentitud", id1, "Va lento"))
	e.leerYProcesar(t)
	tkID := *entranteDe(t, e.buzon.ID, id1).ticketID
	tk := ticketBD(t, e.m, ctx, tkID)
	if _, err := e.m.ResolverTicket.Ejecutar(ctx, soporte.EntradaResolverTicket{ID: tkID, Version: tk.Version, Solucion: "Cambiamos el puerto"}); err != nil {
		t.Fatal(err)
	}
	antes := len(eventosDe(t, tkID))

	// Un tercero (verificado, pero no es el contacto) responde al hilo.
	intruso := pruebas.Correo("intruso")
	id2 := "b." + pruebas.Unico() + "@intruso"
	e.imap.agregar(t, crudo(intruso, "Re: Lentitud", id2, "texto del intruso", "In-Reply-To: <"+id1+">"))
	e.leerYProcesar(t)
	c := entranteDe(t, e.buzon.ID, id2)
	if c.estado != "procesado" || *c.resultado != "nota_interna" || *c.ticketID != tkID || *c.motivo != "remitente_no_es_contacto" {
		t.Fatalf("el intruso queda como nota interna: %+v %v %v", c, *c.resultado, *c.motivo)
	}
	if tk = ticketBD(t, e.m, ctx, tkID); tk.Estado != "resuelto" {
		t.Fatalf("el intruso no reabre: %s", tk.Estado)
	}
	if m := ultimoMensaje(t, tkID); m.publico || m.autorTipo == "cliente" || !strings.Contains(m.cuerpo, intruso) ||
		!strings.Contains(m.cuerpo, "no es el contacto") || !strings.Contains(m.cuerpo, "texto del intruso") {
		t.Fatalf("nota interna: %+v", m)
	}
	for _, ev := range eventosDe(t, tkID)[antes:] {
		if ev.Evento != "correo.recibido" {
			t.Fatalf("la nota no emite %s", ev.Evento)
		}
	}

	// Reabierto por el agente: la respuesta va al contacto, nunca al intruso.
	tk, err := e.m.ReabrirTicket.Ejecutar(ctx, soporte.EntradaReabrirTicket{ID: tkID, Version: tk.Version, Motivo: "Sigue"})
	if err != nil {
		t.Fatal(err)
	}
	if dest := responderYEncolar(t, e, ctx, tkID, "Datos privados del cliente"); dest != cliente {
		t.Fatalf("SECUESTRO: la respuesta va a %s (contacto %s)", dest, cliente)
	}

	// El contacto con el From falsificado (sin verificar) tampoco cambia el estado.
	tk = ticketBD(t, e.m, ctx, tkID)
	tk, err = e.m.PonerTicketEnEspera.Ejecutar(ctx, soporte.EntradaPonerEnEspera{ID: tkID, Version: tk.Version, Motivo: "Esperando"})
	if err != nil {
		t.Fatal(err)
	}
	id3 := "c." + pruebas.Unico() + "@falso"
	e.imap.agregar(t, crudoSinVerificar(cliente, "Re: ["+tk.Numero+"] Lentitud", id3, "soy el cliente, de verdad", "In-Reply-To: <"+id1+">"))
	e.leerYProcesar(t)
	if c := entranteDe(t, e.buzon.ID, id3); *c.resultado != "nota_interna" || *c.motivo != "remitente_no_verificado" {
		t.Fatalf("From sin verificar: %+v", c)
	}
	if tk = ticketBD(t, e.m, ctx, tkID); tk.Estado != "en_espera" {
		t.Fatalf("sin verificar no reanuda: %s", tk.Estado)
	}
	if m := ultimoMensaje(t, tkID); m.publico || !strings.Contains(m.cuerpo, "no verificado") {
		t.Fatalf("nota de no verificado: %+v", m)
	}

	// El contacto verificado sí: reanuda y es mensaje público del cliente.
	id4 := "d." + pruebas.Unico() + "@cliente"
	e.imap.agregar(t, crudo(cliente, "Re: Lentitud", id4, "Ya volvió", "References: <"+id1+"> <"+id2+">"))
	e.leerYProcesar(t)
	if c := entranteDe(t, e.buzon.ID, id4); *c.resultado != "respuesta" {
		t.Fatalf("contacto verificado: %+v", c)
	}
	if tk = ticketBD(t, e.m, ctx, tkID); tk.Estado != "abierto" {
		t.Fatalf("el contacto reanuda: %s", tk.Estado)
	}
	if m := ultimoMensaje(t, tkID); !m.publico || m.autorTipo != "cliente" || m.cuerpo != "Ya volvió" {
		t.Fatalf("mensaje del cliente: %+v", m)
	}
	if dest := responderYEncolar(t, e, ctx, tkID, "Perfecto"); dest != cliente {
		t.Fatalf("destinatario: %s", dest)
	}
}

// Punto 1, tras una fusión: se exige el contacto del ticket destino.
func TestRevisionCorreoFusionExigeContactoDelDestino(t *testing.T) {
	ag := agente(t)
	ctx := como(t, ag, append(permisosTickets, permisosBuzones...)...)
	e := nuevoEscenario(t, ctx)
	ana, beto := pruebas.Correo("ana"), pruebas.Correo("beto")
	idA, idB := "a."+pruebas.Unico()+"@ana", "b."+pruebas.Unico()+"@beto"
	e.imap.agregar(t, crudo(ana, "Sin servicio", idA, "Caído"))
	e.imap.agregar(t, crudo(beto, "Sin servicio también", idB, "Caído"))
	e.leerYProcesar(t)
	tkA, tkB := *entranteDe(t, e.buzon.ID, idA).ticketID, *entranteDe(t, e.buzon.ID, idB).ticketID
	a := ticketBD(t, e.m, ctx, tkA)
	if _, err := e.m.FusionarTickets.Ejecutar(ctx, soporte.EntradaFusionarTickets{ID: tkA, Version: a.Version, DestinoID: tkB}); err != nil {
		t.Fatal(err)
	}
	b := ticketBD(t, e.m, ctx, tkB)
	// Ana responde a su hilo: va al destino (B), del que no es contacto → nota interna.
	id := "r." + pruebas.Unico() + "@ana"
	e.imap.agregar(t, crudo(ana, "Re: Sin servicio", id, "¿novedades?", "In-Reply-To: <"+idA+">"))
	e.leerYProcesar(t)
	c := entranteDe(t, e.buzon.ID, id)
	if *c.ticketID != tkB || *c.resultado != "nota_interna" {
		t.Fatalf("tras la fusión: %+v %v", c, *c.resultado)
	}
	if despues := ticketBD(t, e.m, ctx, tkB); despues.Estado != b.Estado || despues.Version != b.Version {
		t.Fatalf("el destino no cambia: %s v%d → %s v%d", b.Estado, b.Version, despues.Estado, despues.Version)
	}
	if dest := responderYEncolar(t, e, ctx, tkB, "Hola"); dest != beto {
		t.Fatalf("la respuesta del destino va a su contacto: %s", dest)
	}
}

// Punto 2 (alto): un MIME anidado profundo no traba el buzón ni quema CPU.
func TestRevisionCorreoMIMEAnidadoNoTrabaElBuzon(t *testing.T) {
	ag := agente(t)
	ctx := como(t, ag, append(permisosTickets, permisosBuzones...)...)
	e := nuevoEscenario(t, ctx)
	cliente := pruebas.Correo("cliente")
	idHostil := "hostil." + pruebas.Unico() + "@x"
	var b strings.Builder
	b.WriteString("From: <" + cliente + ">\nSubject: Anidado\nMessage-ID: <" + idHostil + ">\nMIME-Version: 1.0\nContent-Type: multipart/mixed; boundary=b0\n\n")
	const niveles = 20000
	for i := 1; i <= niveles; i++ {
		b.WriteString("--b" + strconv.Itoa(i-1) + "\nContent-Type: multipart/mixed; boundary=b" + strconv.Itoa(i) + "\n\n")
	}
	b.WriteString("--b" + strconv.Itoa(niveles) + "\nContent-Type: text/plain\n\nfondo\n")
	for i := niveles; i >= 0; i-- {
		b.WriteString("--b" + strconv.Itoa(i) + "--\n")
	}
	e.imap.agregar(t, b.String())
	idNormal := "normal." + pruebas.Unico() + "@cliente"
	e.imap.agregar(t, crudo(cliente, "Otro", idNormal, "Correo normal"))

	inicio := time.Now()
	ctxPlazo, cancelar := context.WithTimeout(e.sis, 30*time.Second)
	defer cancelar()
	if _, err := e.m.LeerBuzon(ctxPlazo, e.buzon.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.m.ProcesarCorreos(ctxPlazo, e.buzon.ID); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(inicio); d > time.Second {
		t.Fatalf("leer y procesar tardó %s", d)
	}
	var truncado bool
	var limite *string
	if err := pruebas.Pool(t).QueryRow(context.Background(),
		"SELECT truncado, limite FROM soporte.correos_entrantes WHERE buzon_id = $1 AND message_id = $2", e.buzon.ID, idHostil).Scan(&truncado, &limite); err != nil {
		t.Fatal(err)
	}
	if !truncado || limite == nil || *limite != "mime_anidado" {
		t.Fatalf("guardado truncado: %v %v", truncado, limite)
	}
	c := entranteDe(t, e.buzon.ID, idHostil)
	if c.estado != "procesado" || *c.resultado != "ticket_creado" {
		t.Fatalf("el hostil entra igual: %+v", c)
	}
	if m := mensajesDe(t, *c.ticketID); len(m) != 1 || !strings.Contains(m[0].cuerpo, "estructura MIME demasiado compleja") {
		t.Fatalf("nota clara: %+v", m)
	}
	if c := entranteDe(t, e.buzon.ID, idNormal); c.estado != "procesado" || *c.resultado != "ticket_creado" {
		t.Fatalf("el siguiente no se bloquea: %+v", c)
	}
}

// Punto 3 (medio): sin Authentication-Results del servidor de confianza el From no está
// verificado; un ticket nuevo entra igual pero marcado.
func TestRevisionCorreoFromFalsificado(t *testing.T) {
	ag := agente(t)
	ctx := como(t, ag, append(permisosTickets, permisosBuzones...)...)
	e := nuevoEscenario(t, ctx)
	cliente := pruebas.Correo("cliente")
	_, dominio, _ := strings.Cut(cliente, "@")
	casos := []struct {
		nombre, cabecera string
		verificado       bool
	}{
		{"verificado", "Authentication-Results: " + authservPrueba + "; dmarc=pass header.from=" + dominio, true},
		{"sin cabecera", "", false},
		{"de otro servidor", "Authentication-Results: mx.atacante.gt; dmarc=pass header.from=" + dominio, false},
		{"dkim de otro dominio", "Authentication-Results: " + authservPrueba + "; dkim=pass header.d=atacante.gt; dmarc=fail", false},
	}
	for _, caso := range casos {
		id := "f." + pruebas.Unico() + "@x"
		cuerpo := crudoSinVerificar(cliente, "Caso "+caso.nombre, id, "hola")
		if caso.cabecera != "" {
			cuerpo = caso.cabecera + "\n" + cuerpo
		}
		e.imap.agregar(t, cuerpo)
		e.leerYProcesar(t)
		c := entranteDe(t, e.buzon.ID, id)
		if *c.resultado != "ticket_creado" {
			t.Fatalf("%s: un ticket nuevo entra sin verificar: %+v", caso.nombre, c)
		}
		if tk := ticketBD(t, e.m, ctx, *c.ticketID); tk.Contacto.Verificado != caso.verificado {
			t.Fatalf("%s: contacto verificado = %v", caso.nombre, tk.Contacto.Verificado)
		}
	}

	// Sin authserv_id en el buzón no se confía en ninguna cabecera.
	d := datosBuzon(e.buzon.Correo, e.imap.puerto, e.smtp.puerto, "tls")
	d.AuthservID, d.IMAP.Contrasena, d.SMTP.Contrasena = nil, nil, nil
	b, err := e.m.EditarBuzon.Ejecutar(ctx, soporte.EntradaEditarBuzon{ID: e.buzon.ID, Version: e.buzon.Version, DatosBuzon: d})
	if err != nil || b.AuthservID != nil {
		t.Fatalf("editar: %+v %v", b.AuthservID, err)
	}
	id := "g." + pruebas.Unico() + "@x"
	e.imap.agregar(t, crudo(cliente, "Sin authserv", id, "hola"))
	e.leerYProcesar(t)
	if tk := ticketBD(t, e.m, ctx, *entranteDe(t, e.buzon.ID, id).ticketID); tk.Contacto.Verificado {
		t.Fatal("sin authserv_id nada queda verificado")
	}
	// Un ticket capturado a mano queda verificado.
	tk, err := e.m.CrearTicket.Ejecutar(ctx, soporte.EntradaCrearTicket{Asunto: "A mano", Contacto: &soporte.EntradaContacto{Correo: &cliente}})
	if err != nil || !tk.Contacto.Verificado {
		t.Fatalf("a mano: %+v %v", tk.Contacto, err)
	}
}

// servidorMudo acepta TLS y nunca saluda (IMAP colgado).
func servidorMudo(t *testing.T, cfg *tls.Config) int {
	t.Helper()
	ln, err := tls.Listen("tcp", "127.0.0.1:0", cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				_ = c.(*tls.Conn).Handshake()
				time.Sleep(15 * time.Second)
				_ = c.Close()
			}(c)
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port
}

// Punto 4 (medio): probar no tiene una transacción abierta durante la red y tiene un plazo total.
func TestRevisionProbarBuzonFueraDeTransaccion(t *testing.T) {
	ag := agente(t)
	cfg, raices := tlsPrueba(t)
	m := moduloCorreoAjustado(t, raices, []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}, func(c *soporte.Config) {
		c.Correo.PlazoPrueba = 1500 * time.Millisecond
	})
	ctx := como(t, ag, permisosBuzones...)
	b, err := m.CrearBuzon.Ejecutar(ctx, soporte.EntradaCrearBuzon{DatosBuzon: datosBuzon(pruebas.Correo("b"), servidorMudo(t, cfg), 0, "tls")})
	if err != nil {
		t.Fatal(err)
	}
	// Un pool de una sola conexión: si la prueba la tuviera ocupada con su transacción, no se
	// podría tomar otra mientras el servidor no responde.
	url, err := pruebas.URL()
	if err != nil {
		t.Fatal(err)
	}
	pcfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	pcfg.MaxConns = 1
	uno, err := pgxpool.NewWithConfig(context.Background(), pcfg)
	if err != nil {
		t.Fatal(err)
	}
	defer uno.Close()
	ctxUno := nucleo.ConBD(como(t, ag, permisosBuzones...), uno)

	listo := make(chan soporte.ResultadoPrueba, 1)
	inicio := time.Now()
	go func() {
		r, err := m.ProbarBuzon.Ejecutar(ctxUno, soporte.EntradaVerBuzon{ID: b.ID})
		if err != nil {
			t.Error(err)
		}
		listo <- r
	}()
	time.Sleep(500 * time.Millisecond) // la prueba está esperando el saludo del servidor
	espera, cancelar := context.WithTimeout(context.Background(), 300*time.Millisecond)
	con, err := uno.Acquire(espera)
	cancelar()
	if err != nil {
		t.Fatalf("la conexión de la base quedó tomada durante la red: %v", err)
	}
	con.Release()
	r := <-listo
	// El plazo total (1,5 s) manda sobre el de cada paso (10 s).
	if d := time.Since(inicio); d > 5*time.Second || r.IMAP.OK || *r.IMAP.Codigo != "conexion" {
		t.Fatalf("plazo total: %s %+v %s", d, r.IMAP, *r.IMAP.Codigo)
	}
	var auditada int
	if err := pruebas.Pool(t).QueryRow(context.Background(),
		"SELECT count(*) FROM auditoria.acciones WHERE accion = 'buzones.probar' AND entidad_id = $1 AND resultado = 'ok'", strconv.FormatInt(b.ID, 10)).Scan(&auditada); err != nil || auditada != 1 {
		t.Fatalf("una fila de auditoría con el resultado: %d %v", auditada, err)
	}
}

// Punto 5 (medio): administrar y probar buzones exige el permiso con alcance global.
func TestRevisionBuzonesExigenAlcanceGlobal(t *testing.T) {
	ag := agente(t)
	_, raices := tlsPrueba(t)
	m := moduloCorreo(t, raices)
	b, err := m.CrearBuzon.Ejecutar(como(t, ag, permisosBuzones...), soporte.EntradaCrearBuzon{DatosBuzon: datosBuzon(pruebas.Correo("b"), 993, 0, "tls")})
	if err != nil {
		t.Fatal(err)
	}
	// Supervisor de zona: todos los permisos de buzones, limitados a la zona 7.
	mapa := map[string][]contexto.Alcance{}
	for _, p := range permisosBuzones {
		mapa[string(p)] = []contexto.Alcance{{ZonaID: 7}}
	}
	zona := conAlcance(t, ag, mapa)
	if _, err := m.CrearBuzon.Ejecutar(zona, soporte.EntradaCrearBuzon{DatosBuzon: datosBuzon(pruebas.Correo("b"), 993, 0, "tls")}); !esProblema(err, "prohibido") {
		t.Fatalf("crear: %v", err)
	}
	d := datosBuzon(b.Correo, 993, 0, "tls")
	if _, err := m.EditarBuzon.Ejecutar(zona, soporte.EntradaEditarBuzon{ID: b.ID, Version: b.Version, DatosBuzon: d}); !esProblema(err, "prohibido") {
		t.Fatalf("editar: %v", err)
	}
	if _, err := m.ActivarBuzon.Ejecutar(zona, soporte.EntradaCambiarBuzon{ID: b.ID, Version: b.Version}); !esProblema(err, "prohibido") {
		t.Fatalf("activar: %v", err)
	}
	if _, err := m.DesactivarBuzon.Ejecutar(zona, soporte.EntradaCambiarBuzon{ID: b.ID, Version: b.Version}); !esProblema(err, "prohibido") {
		t.Fatalf("desactivar: %v", err)
	}
	if _, err := m.ProbarBuzon.Ejecutar(zona, soporte.EntradaVerBuzon{ID: b.ID}); !esProblema(err, "prohibido") {
		t.Fatalf("probar: %v", err)
	}
	problema(t, func() error { _, err := m.ProbarBuzon.Ejecutar(zona, soporte.EntradaVerBuzon{ID: b.ID}); return err }(), 403, "prohibido")
	// Ver sí (lectura).
	if _, err := m.VerBuzon.Ejecutar(zona, soporte.EntradaVerBuzon{ID: b.ID}); err != nil {
		t.Fatalf("ver: %v", err)
	}
}
