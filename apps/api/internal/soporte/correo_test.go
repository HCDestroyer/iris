package soporte_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"crypto/tls"
	"crypto/x509"

	"github.com/danielgtaylor/huma/v2"
	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"

	integracionpkg "github.com/HCDestroyer/iris/apps/api/internal/integracion"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/servidor"
	"github.com/HCDestroyer/iris/apps/api/internal/soporte"
)

var permisosBuzones = []nucleo.Permiso{
	nucleo.PermBuzonesVer, nucleo.PermBuzonesCrear, nucleo.PermBuzonesEditar, nucleo.PermBuzonesActivar,
	nucleo.PermBuzonesDesactivar,
}

const claveIMAP = "clave-imap-Secreta-123"
const claveSMTP = "clave-smtp-Secreta-456"

// authservPrueba es el authserv-id del «servidor de correo de confianza» de los buzones de
// prueba: crudo pone su Authentication-Results con dmarc=pass para el dominio del From.
const authservPrueba = "mx.prueba.iris.gt"

// escenario: servidores falsos, módulo y un buzón activo de la empresa del contexto.
type escenario struct {
	m      *soporte.Modulo
	raices *x509.CertPool
	imap   *imapFalso
	smtp   *smtpFalso
	buzon  soporte.Buzon
	sis    context.Context
}

func datosBuzon(correoBuzon string, imapPuerto, smtpPuerto int, tlsIMAP string) soporte.DatosBuzon {
	d := soporte.DatosBuzon{
		Nombre: "Soporte", Correo: correoBuzon, AuthservID: ptrS(authservPrueba),
		IMAP: soporte.EntradaIMAP{Host: "127.0.0.1", Puerto: puerto(imapPuerto), TLS: tlsIMAP, Usuario: "soporte", Contrasena: ptrS(claveIMAP)},
	}
	if smtpPuerto != 0 {
		d.SMTP = &soporte.EntradaSMTP{Host: "127.0.0.1", Puerto: puerto(smtpPuerto), TLS: "starttls", Usuario: ptrS("soporte"), Contrasena: ptrS(claveSMTP)}
	}
	return d
}

func ptrS(s string) *string { return &s }

// nuevoEscenario crea el buzón con el contexto dado (sin conector o en una empresa) y lo activa.
func nuevoEscenario(t *testing.T, ctx context.Context) escenario {
	t.Helper()
	cfg, raices := tlsPrueba(t)
	e := escenario{
		m: moduloCorreo(t, raices), raices: raices, imap: nuevoIMAPFalso(t, cfg, "tls", "soporte", claveIMAP),
		smtp: nuevoSMTPFalso(t, cfg, "starttls"), sis: pruebas.ContextoSistema(pruebas.Pool(t)),
	}
	b, err := e.m.CrearBuzon.Ejecutar(ctx, soporte.EntradaCrearBuzon{DatosBuzon: datosBuzon(pruebas.Correo("soporte"), e.imap.puerto, e.smtp.puerto, "tls")})
	if err != nil {
		t.Fatal(err)
	}
	if b.Activo {
		t.Fatal("el buzón nace inactivo")
	}
	if e.buzon, err = e.m.ActivarBuzon.Ejecutar(ctx, soporte.EntradaCambiarBuzon{ID: b.ID, Version: b.Version}); err != nil {
		t.Fatal(err)
	}
	return e
}

// leerYProcesar corre la lectura y el proceso como lo haría el worker.
func (e escenario) leerYProcesar(t *testing.T) soporte.ResultadoLectura {
	t.Helper()
	r, err := e.m.LeerBuzon(e.sis, e.buzon.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.m.ProcesarCorreos(e.sis, e.buzon.ID); err != nil {
		t.Fatal(err)
	}
	return r
}

// crudo arma un correo de texto con el remitente verificado por el servidor de confianza
// (Authentication-Results de authservPrueba con dmarc=pass). Ver crudoSinVerificar.
func crudo(de, asunto, messageID, cuerpo string, cabeceras ...string) string {
	_, dominio, _ := strings.Cut(de, "@")
	return "Authentication-Results: " + authservPrueba + "; dkim=pass header.d=" + dominio + "; dmarc=pass header.from=" + dominio + "\n" +
		crudoSinVerificar(de, asunto, messageID, cuerpo, cabeceras...)
}

// crudoSinVerificar: como crudo, sin Authentication-Results (el From no se puede verificar).
func crudoSinVerificar(de, asunto, messageID, cuerpo string, cabeceras ...string) string {
	extra := ""
	if len(cabeceras) > 0 {
		extra = strings.Join(cabeceras, "\n") + "\n"
	}
	return "From: Ana Pérez <" + de + ">\nTo: soporte@isp.gt\nSubject: " + asunto + "\nMessage-ID: <" + messageID +
		">\nDate: Mon, 26 Oct 2026 10:00:00 -0600\n" + extra + "MIME-Version: 1.0\nContent-Type: text/plain; charset=utf-8\n\n" + cuerpo + "\n"
}

type entrante struct {
	estado    string
	resultado *string
	ticketID  *int64
	motivo    *string
}

func entranteDe(t *testing.T, buzon int64, messageID string) entrante {
	t.Helper()
	var e entrante
	err := pruebas.Pool(t).QueryRow(context.Background(),
		"SELECT estado, resultado, ticket_id, motivo FROM soporte.correos_entrantes WHERE buzon_id = $1 AND message_id = $2",
		buzon, messageID).Scan(&e.estado, &e.resultado, &e.ticketID, &e.motivo)
	if err != nil {
		t.Fatalf("correo %s: %v", messageID, err)
	}
	return e
}

type mensajeBD struct {
	autorTipo, autorNombre, canal, cuerpo string
	publico                               bool
}

func mensajesDe(t *testing.T, ticket int64) []mensajeBD {
	t.Helper()
	filas, err := pruebas.Pool(t).Query(context.Background(),
		"SELECT autor_tipo, autor_nombre, canal, cuerpo, publico FROM soporte.ticket_mensajes WHERE ticket_id = $1 ORDER BY id", ticket)
	if err != nil {
		t.Fatal(err)
	}
	defer filas.Close()
	var salida []mensajeBD
	for filas.Next() {
		var m mensajeBD
		if err := filas.Scan(&m.autorTipo, &m.autorNombre, &m.canal, &m.cuerpo, &m.publico); err != nil {
			t.Fatal(err)
		}
		salida = append(salida, m)
	}
	return salida
}

func ticketBD(t *testing.T, m *soporte.Modulo, ctx context.Context, id int64) soporte.Ticket {
	t.Helper()
	tk, err := m.VerTicket.Ejecutar(ctx, soporte.EntradaVerTicket{ID: id})
	if err != nil {
		t.Fatal(err)
	}
	return tk
}

func TestCorreoTicketNuevoYRespuestaPorNumero(t *testing.T) {
	ag := agente(t)
	ctx := como(t, ag, append(permisosTickets, permisosBuzones...)...)
	e := nuevoEscenario(t, ctx)
	cliente := pruebas.Correo("cliente")

	// Un correo ya leído antes de activar el buzón no se importa (primera lectura: no leídos).
	e.imap.agregar(t, crudo(cliente, "Viejo", "viejo."+pruebas.Unico()+"@x", "historial"), imap.FlagSeen)
	id1 := "uno." + pruebas.Unico() + "@cliente"
	e.imap.agregar(t, crudo(cliente, "Sin internet desde ayer", id1,
		"Hola, no tengo servicio.\n\nEl lun, 26 oct 2026 a las 9:00, Ana escribió:\n> algo citado"))
	if r := e.leerYProcesar(t); r.Nuevos != 1 || r.Duplicados != 0 {
		t.Fatalf("lectura: %+v", r)
	}
	if n, sinLeer := e.imap.estado(t, "INBOX"); n != 2 || sinLeer != 0 {
		t.Fatalf("el correo se marca leído después de guardarlo: %d mensajes, %d sin leer", n, sinLeer)
	}
	c1 := entranteDe(t, e.buzon.ID, id1)
	if c1.estado != "procesado" || *c1.resultado != "ticket_creado" || c1.ticketID == nil {
		t.Fatalf("correo 1: %+v", c1)
	}
	tk := ticketBD(t, e.m, ctx, *c1.ticketID)
	if tk.Origen != "correo" || tk.Asunto != "Sin internet desde ayer" || tk.Contacto.Correo == nil ||
		*tk.Contacto.Correo != cliente || tk.Contacto.Nombre == nil || *tk.Contacto.Nombre != "Ana Pérez" ||
		tk.EmpresaID != nil || tk.Prioridad != "media" || tk.Estado != "nuevo" {
		t.Fatalf("ticket: %+v", tk)
	}
	msjs := mensajesDe(t, tk.ID)
	if len(msjs) != 1 || msjs[0].autorTipo != "cliente" || msjs[0].canal != "correo" || !msjs[0].publico ||
		!strings.Contains(msjs[0].cuerpo, "no tengo servicio") || !strings.Contains(msjs[0].autorNombre, cliente) {
		t.Fatalf("mensajes: %+v", msjs)
	}
	if ev := eventosDe(t, tk.ID); contar(ev, "ticket.creado") != 1 || contar(ev, "correo.recibido") != 1 {
		t.Fatalf("eventos del ticket: %v", nombres(ev))
	}

	// En espera → la respuesta del cliente por número (mismo remitente) lo reanuda.
	tk, err := e.m.AsignarTicket.Ejecutar(ctx, soporte.EntradaAsignarTicket{ID: tk.ID, Version: tk.Version, UsuarioID: &ag})
	if err != nil {
		t.Fatal(err)
	}
	tk, err = e.m.PonerTicketEnEspera.Ejecutar(ctx, soporte.EntradaPonerEnEspera{ID: tk.ID, Version: tk.Version, Motivo: "Esperando al cliente"})
	if err != nil {
		t.Fatal(err)
	}
	id2 := "dos." + pruebas.Unico() + "@cliente"
	e.imap.agregar(t, crudo(cliente, "RE: ["+tk.Numero+"] Sin internet desde ayer", id2,
		"Ya reinicié el router.\n\nOn Mon, Oct 26, 2026 at 9:00 AM Soporte wrote:\n> Hola"))
	e.leerYProcesar(t)
	if c := entranteDe(t, e.buzon.ID, id2); c.estado != "procesado" || *c.resultado != "respuesta" || *c.ticketID != tk.ID {
		t.Fatalf("respuesta por número: %+v", c)
	}
	tk = ticketBD(t, e.m, ctx, tk.ID)
	if tk.Estado != "abierto" {
		t.Fatalf("en_espera → abierto, quedó %s", tk.Estado)
	}
	msjs = mensajesDe(t, tk.ID)
	if len(msjs) != 2 || msjs[1].autorTipo != "cliente" || msjs[1].cuerpo != "Ya reinicié el router." {
		t.Fatalf("la cita se quita: %+v", msjs)
	}

	// El número en el asunto desde otro remitente no entra al ticket: abre otro.
	id3 := "tres." + pruebas.Unico() + "@otro"
	e.imap.agregar(t, crudo(pruebas.Correo("intruso"), "["+tk.Numero+"] meto texto", id3, "hola"))
	e.leerYProcesar(t)
	if c := entranteDe(t, e.buzon.ID, id3); *c.resultado != "ticket_creado" || *c.ticketID == tk.ID {
		t.Fatalf("otro remitente: %+v", c)
	}
	if n := len(mensajesDe(t, tk.ID)); n != 2 {
		t.Fatalf("el ticket no cambió: %d mensajes", n)
	}
}

func TestCorreoRespuestaPorInReplyToYEnvioIdempotente(t *testing.T) {
	ag := agente(t)
	ctx := como(t, ag, append(permisosTickets, permisosBuzones...)...)
	e := nuevoEscenario(t, ctx)
	cliente := pruebas.Correo("cliente")
	id1 := "a." + pruebas.Unico() + "@cliente"
	e.imap.agregar(t, crudo(cliente, "Lentitud", id1, "Va lento"))
	e.leerYProcesar(t)
	tkID := *entranteDe(t, e.buzon.ID, id1).ticketID

	// El agente responde: ticket.respondido → se encola una sola vez y se envía una sola vez.
	r, err := e.m.ResponderTicket.Ejecutar(ctx, soporte.EntradaResponderTicket{ID: tkID, Cuerpo: "Revisamos tu nodo."})
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]any{"ticket_id": tkID, "mensaje_id": r.Mensaje.ID})
	for range 2 { // el evento puede llegar dos veces
		if err := e.m.AlResponderTicket(e.sis, integracionpkg.Evento{Evento: "ticket.respondido", Payload: payload}); err != nil {
			t.Fatal(err)
		}
	}
	var n int
	var salienteID int64
	if err := pruebas.Pool(t).QueryRow(context.Background(), "SELECT count(*), max(id) FROM soporte.correos_salientes WHERE ticket_id = $1", tkID).Scan(&n, &salienteID); err != nil || n != 1 {
		t.Fatalf("salientes: %d %v", n, err)
	}
	for range 2 {
		if err := e.m.EnviarCorreo(e.sis, salienteID); err != nil {
			t.Fatal(err)
		}
	}
	enviados := e.smtp.recibidos()
	if len(enviados) != 1 {
		t.Fatalf("un solo envío: %d", len(enviados))
	}
	tk := ticketBD(t, e.m, ctx, tkID)
	env := enviados[0]
	if !strings.Contains(env, "Subject: Re: ["+tk.Numero+"] Lentitud") || !strings.Contains(env, "In-Reply-To: <"+id1+">") ||
		!strings.Contains(env, "Revisamos tu nodo.") || !strings.Contains(env, "To: <"+cliente+">") ||
		strings.Contains(env, "Auto-Submitted") {
		t.Fatalf("correo enviado:\n%s", env)
	}
	if auth := e.smtp.auth; len(auth) != 1 || auth[0] != "soporte|"+claveSMTP {
		t.Fatalf("AUTH sobre STARTTLS con la contraseña descifrada: %v", auth)
	}
	var messageID string
	if err := pruebas.Pool(t).QueryRow(context.Background(), "SELECT message_id FROM soporte.correos_salientes WHERE id = $1", salienteID).Scan(&messageID); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(env, "Message-ID: <"+messageID+">") {
		t.Fatalf("Message-ID fijo: %s", messageID)
	}

	// Una nota interna o un ticket que no es de correo no salen.
	nota, err := e.m.AgregarNotaInterna.Ejecutar(ctx, soporte.EntradaNotaInterna{ID: tkID, Cuerpo: "nota"})
	if err != nil {
		t.Fatal(err)
	}
	if res, err := e.m.EncolarRespuesta.Ejecutar(e.sis, soporte.EntradaEncolarRespuesta{TicketID: tkID, MensajeID: nota.Mensaje.ID}); err != nil || res.SalienteID != 0 {
		t.Fatalf("nota interna: %+v %v", res, err)
	}

	// Resuelto → el cliente contesta al correo enviado (In-Reply-To, sin número) y se reabre.
	tk, err = e.m.ResolverTicket.Ejecutar(ctx, soporte.EntradaResolverTicket{ID: tkID, Version: tk.Version, Solucion: "Cambiamos el puerto"})
	if err != nil {
		t.Fatal(err)
	}
	id2 := "b." + pruebas.Unico() + "@cliente"
	e.imap.agregar(t, crudo(cliente, "Re: Lentitud", id2, "Sigue lento", "In-Reply-To: <"+messageID+">", "References: <"+id1+"> <"+messageID+">"))
	e.leerYProcesar(t)
	if c := entranteDe(t, e.buzon.ID, id2); *c.resultado != "respuesta" || *c.ticketID != tkID {
		t.Fatalf("respuesta por In-Reply-To: %+v", c)
	}
	if tk = ticketBD(t, e.m, ctx, tkID); tk.Estado != "abierto" {
		t.Fatalf("resuelto → abierto, quedó %s", tk.Estado)
	}
}

func TestCorreoDuplicadoAutorespuestaYReanudacion(t *testing.T) {
	ag := agente(t)
	ctx := como(t, ag, append(permisosTickets, permisosBuzones...)...)
	e := nuevoEscenario(t, ctx)
	cliente := pruebas.Correo("cliente")

	// El mismo Message-ID dos veces (dos UID) → un solo correo guardado y un solo ticket.
	dup := "dup." + pruebas.Unico() + "@cliente"
	e.imap.agregar(t, crudo(cliente, "Duplicado", dup, "uno"))
	e.imap.agregar(t, crudo(cliente, "Duplicado", dup, "uno"))
	// Autorespuesta y rebote: se guardan pero no abren ticket.
	auto := "auto." + pruebas.Unico() + "@cliente"
	e.imap.agregar(t, crudo(cliente, "Fuera de oficina", auto, "Vuelvo el lunes", "Auto-Submitted: auto-replied"))
	rebote := "rebote." + pruebas.Unico() + "@mta"
	e.imap.agregar(t, "From: MAILER-DAEMON@isp.gt\nSubject: Undelivered\nMessage-ID: <"+rebote+">\n\nfalló\n")
	html := "html." + pruebas.Unico() + "@cliente"
	e.imap.agregar(t, "From: <"+cliente+">\nSubject: Con HTML\nMessage-ID: <"+html+">\nMIME-Version: 1.0\nContent-Type: text/html; charset=utf-8\n\n"+
		`<html><head><style>p{color:red}</style><script>alert('x')</script></head><body><p>Hola &amp; <b>gracias</b></p><a href="https://isp.gt/x">aquí</a><img src=x onerror=alert(1)></body></html>`)
	r := e.leerYProcesar(t)
	if r.Nuevos != 4 || r.Duplicados != 1 {
		t.Fatalf("lectura: %+v", r)
	}
	if c := entranteDe(t, e.buzon.ID, auto); c.estado != "ignorado" || c.ticketID != nil || !strings.Contains(*c.motivo, "auto_submitted") {
		t.Fatalf("autorespuesta: %+v", c)
	}
	if c := entranteDe(t, e.buzon.ID, rebote); c.estado != "ignorado" || c.ticketID != nil {
		t.Fatalf("rebote: %+v", c)
	}
	c := entranteDe(t, e.buzon.ID, html)
	m := mensajesDe(t, *c.ticketID)
	if len(m) != 1 || m[0].cuerpo != "Hola & gracias\naquí (https://isp.gt/x)" {
		t.Fatalf("HTML a texto seguro: %q", m[0].cuerpo)
	}
	var tickets int
	if err := pruebas.Pool(t).QueryRow(context.Background(),
		"SELECT count(DISTINCT ticket_id) FROM soporte.correos_entrantes WHERE buzon_id = $1 AND ticket_id IS NOT NULL", e.buzon.ID).Scan(&tickets); err != nil || tickets != 2 {
		t.Fatalf("tickets abiertos: %d %v", tickets, err)
	}

	// Reanudación: el UID guardado manda. Aunque los correos vuelvan a quedar sin leer (p. ej.
	// el proceso cayó antes de marcarlos), no se vuelven a pedir; solo el nuevo.
	desmarcar(t, e)
	nuevo := "nuevo." + pruebas.Unico() + "@cliente"
	e.imap.agregar(t, crudo(cliente, "Otro tema", nuevo, "hola"))
	antes := registros(t, e.buzon.ID)
	if r := e.leerYProcesar(t); r.Nuevos != 1 || r.Duplicados != 0 {
		t.Fatalf("reanudación: %+v", r)
	}
	if d := registros(t, e.buzon.ID) - antes; d != 1 {
		t.Fatalf("solo se pidió el UID nuevo: %d registros", d)
	}
	// Repetir el proceso no hace nada (idempotente).
	if n, err := e.m.ProcesarCorreos(e.sis, e.buzon.ID); err != nil || n != 0 {
		t.Fatalf("proceso repetido: %d %v", n, err)
	}
}

// desmarcar quita \Seen de todo INBOX con un cliente IMAP de prueba.
func desmarcar(t *testing.T, e escenario) {
	t.Helper()
	ses, err := imapclient.DialTLS(fmt.Sprintf("127.0.0.1:%d", e.imap.puerto), &imapclient.Options{TLSConfig: &tls.Config{RootCAs: e.raices, MinVersion: tls.VersionTLS12}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ses.Close() }()
	if err := ses.Login("soporte", claveIMAP).Wait(); err != nil {
		t.Fatal(err)
	}
	if _, err := ses.Select("INBOX", nil).Wait(); err != nil {
		t.Fatal(err)
	}
	if err := ses.Store(imap.UIDSet{{Start: 1, Stop: 0}}, &imap.StoreFlags{Op: imap.StoreFlagsDel, Silent: true, Flags: []imap.Flag{imap.FlagSeen}}, nil).Close(); err != nil {
		t.Fatal(err)
	}
	if _, sinLeer := e.imap.estado(t, "INBOX"); sinLeer == 0 {
		t.Fatal("no se desmarcó")
	}
}

func registros(t *testing.T, buzon int64) int {
	t.Helper()
	var n int
	if err := pruebas.Pool(t).QueryRow(context.Background(),
		"SELECT count(*) FROM auditoria.acciones WHERE accion = 'correos.registrar' AND (entrada->>'buzonId')::bigint = $1", buzon).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestCorreoBuzonDeOtraEmpresaNoTocaTicketsAjenos(t *testing.T) {
	registrarEmpresas(t, 71, 72)
	ag := agente(t)
	asignarEmpresa(t, ag, 71)
	asignarEmpresa(t, ag, 72)
	todos := append(permisosTickets, permisosBuzones...)
	ctxA := comoEnEmpresa(t, ag, 71, todos...)
	ctxB := comoEnEmpresa(t, ag, 72, todos...)
	cliente := pruebas.Correo("cliente")
	// Ticket de la empresa A con el cliente como contacto.
	tkA, err := soporte.Nuevo(soporte.Config{}).CrearTicket.Ejecutar(ctxA, soporte.EntradaCrearTicket{
		Asunto: "Ticket de A", Contacto: &soporte.EntradaContacto{Correo: &cliente},
	})
	if err != nil {
		t.Fatal(err)
	}
	e := nuevoEscenario(t, ctxB)
	if e.buzon.EmpresaID == nil || *e.buzon.EmpresaID != 72 {
		t.Fatalf("el buzón es de la empresa activa: %+v", e.buzon.EmpresaID)
	}
	// Desde A no se ve el buzón de B.
	if _, err := e.m.VerBuzon.Ejecutar(ctxA, soporte.EntradaVerBuzon{ID: e.buzon.ID}); !esProblema(err, "no_encontrado") {
		t.Fatalf("buzón de otra empresa: %v", err)
	}
	id := "x." + pruebas.Unico() + "@cliente"
	e.imap.agregar(t, crudo(cliente, "Re: ["+tkA.Numero+"] Ticket de A", id, "hola"))
	e.leerYProcesar(t)
	c := entranteDe(t, e.buzon.ID, id)
	if *c.resultado != "ticket_creado" || *c.ticketID == tkA.ID {
		t.Fatalf("no toca el ticket de A: %+v", c)
	}
	nuevo := ticketBD(t, e.m, ctxB, *c.ticketID)
	if nuevo.EmpresaID == nil || *nuevo.EmpresaID != 72 {
		t.Fatalf("el ticket nuevo es de B: %+v", nuevo.EmpresaID)
	}
	if n := len(mensajesDe(t, tkA.ID)); n != 0 {
		t.Fatalf("el ticket de A no recibió mensajes: %d", n)
	}
}

func esProblema(err error, codigo string) bool {
	var p *nucleo.Problema
	return errors.As(err, &p) && p.Codigo() == codigo
}

func TestCorreoContrasenaCifradaYNuncaExpuesta(t *testing.T) {
	ag := agente(t)
	ctx := como(t, ag, append(permisosTickets, permisosBuzones...)...)
	e := nuevoEscenario(t, ctx)
	pool := pruebas.Pool(t)

	// En la base va cifrada.
	var imapCif, smtpCif []byte
	if err := pool.QueryRow(context.Background(), "SELECT imap_contrasena, smtp_contrasena FROM soporte.buzones WHERE id = $1", e.buzon.ID).Scan(&imapCif, &smtpCif); err != nil {
		t.Fatal(err)
	}
	if len(imapCif) == 0 || bytes.Contains(imapCif, []byte(claveIMAP)) || len(smtpCif) == 0 || bytes.Contains(smtpCif, []byte(claveSMTP)) {
		t.Fatal("las contraseñas deben ir cifradas")
	}
	// Ni en la auditoría, ni en el outbox, ni en la respuesta.
	for _, sql := range []string{
		"SELECT count(*) FROM auditoria.acciones WHERE entrada::text LIKE '%' || $1 || '%' OR coalesce(error::text, '') LIKE '%' || $1 || '%'",
		"SELECT count(*) FROM auditoria.cambios WHERE antes::text LIKE '%' || $1 || '%' OR despues::text LIKE '%' || $1 || '%'",
		"SELECT count(*) FROM integracion.outbox WHERE payload::text LIKE '%' || $1 || '%'",
	} {
		for _, clave := range []string{claveIMAP, claveSMTP} {
			var n int
			if err := pool.QueryRow(context.Background(), sql, clave).Scan(&n); err != nil || n != 0 {
				t.Fatalf("contraseña expuesta (%s): %d %v", sql, n, err)
			}
		}
	}
	var enmascarada bool
	if err := pool.QueryRow(context.Background(),
		"SELECT entrada->'imap'->>'contrasena' = '***' FROM auditoria.acciones WHERE accion = 'buzones.crear' AND entidad_id = $1", strconv.FormatInt(e.buzon.ID, 10)).Scan(&enmascarada); err != nil || !enmascarada {
		t.Fatalf("la entrada auditada lleva *** : %v %v", enmascarada, err)
	}
	b, _ := json.Marshal(e.buzon)
	if bytes.Contains(b, []byte("contrasena")) || bytes.Contains(b, []byte(claveIMAP)) {
		t.Fatalf("la respuesta no trae contraseñas: %s", b)
	}

	// Probar: OK con la correcta; con una incorrecta, error resumido sin la contraseña.
	if r, err := e.m.ProbarBuzon.Ejecutar(ctx, soporte.EntradaVerBuzon{ID: e.buzon.ID}); err != nil || !r.IMAP.OK || r.SMTP == nil || !r.SMTP.OK {
		t.Fatalf("probar: %+v %v", r, err)
	}
	d := datosBuzon(e.buzon.Correo, e.imap.puerto, e.smtp.puerto, "tls")
	d.IMAP.Contrasena = ptrS("incorrecta-XYZ-987")
	d.SMTP.Contrasena = nil // misma cuenta SMTP: se conserva
	b2, err := e.m.EditarBuzon.Ejecutar(ctx, soporte.EntradaEditarBuzon{ID: e.buzon.ID, Version: e.buzon.Version, DatosBuzon: d})
	if err != nil {
		t.Fatal(err)
	}
	r, err := e.m.ProbarBuzon.Ejecutar(ctx, soporte.EntradaVerBuzon{ID: e.buzon.ID})
	if err != nil || r.IMAP.OK || *r.IMAP.Codigo != "autenticacion" || strings.Contains(*r.IMAP.Detalle, "incorrecta") {
		t.Fatalf("probar con la incorrecta: %+v %v", r, err)
	}
	if _, err := e.m.LeerBuzon(e.sis, e.buzon.ID); err == nil {
		t.Fatal("la lectura falla con la contraseña incorrecta")
	}
	if v, err := e.m.VerBuzon.Ejecutar(ctx, soporte.EntradaVerBuzon{ID: e.buzon.ID}); err != nil || v.UltimoError == nil ||
		strings.Contains(*v.UltimoError, "incorrecta") {
		t.Fatalf("último error sin la contraseña: %+v %v", v.UltimoError, err)
	}

	// Cambiar el host sin volver a escribir la contraseña: 422 (no viaja a otro servidor).
	d2 := datosBuzon(e.buzon.Correo, e.imap.puerto, e.smtp.puerto, "tls")
	d2.IMAP.Host, d2.IMAP.Contrasena, d2.SMTP.Contrasena = "localhost", nil, nil
	if _, err := e.m.EditarBuzon.Ejecutar(ctx, soporte.EntradaEditarBuzon{ID: e.buzon.ID, Version: b2.Version, DatosBuzon: d2}); !esProblema(err, "validacion") {
		t.Fatalf("contraseña requerida al cambiar el host: %v", err)
	}
	// Versión vieja → 409.
	if _, err := e.m.EditarBuzon.Ejecutar(ctx, soporte.EntradaEditarBuzon{ID: e.buzon.ID, Version: e.buzon.Version, DatosBuzon: d}); !esProblema(err, "conflicto_version") {
		t.Fatalf("versión vieja: %v", err)
	}
}

func TestCorreoTLSObligatorioYDestinos(t *testing.T) {
	ag := agente(t)
	ctx := como(t, ag, permisosBuzones...)
	cfg, raices := tlsPrueba(t)
	m := moduloCorreo(t, raices)

	// Sin TLS no se puede ni registrar.
	d := datosBuzon(pruebas.Correo("b"), 143, 0, "ninguno")
	if _, err := m.CrearBuzon.Ejecutar(ctx, soporte.EntradaCrearBuzon{DatosBuzon: d}); !esProblema(err, "validacion") {
		t.Fatalf("tls inválido: %v", err)
	}

	// STARTTLS contra un servidor que no lo ofrece: se corta sin mandar LOGIN.
	claro := nuevoIMAPFalso(t, cfg, "claro", "soporte", claveIMAP)
	b, err := m.CrearBuzon.Ejecutar(ctx, soporte.EntradaCrearBuzon{DatosBuzon: datosBuzon(pruebas.Correo("b"), claro.puerto, 0, "starttls")})
	if err != nil {
		t.Fatal(err)
	}
	r, err := m.ProbarBuzon.Ejecutar(ctx, soporte.EntradaVerBuzon{ID: b.ID})
	if err != nil || r.IMAP.OK || *r.IMAP.Codigo != "sin_tls" {
		t.Fatalf("sin STARTTLS: %+v %v", r, err)
	}
	if claro.vioEnClaro("LOGIN") || claro.vioEnClaro(claveIMAP) {
		t.Fatal("las credenciales viajaron en claro")
	}

	// STARTTLS real funciona.
	st := nuevoIMAPFalso(t, cfg, "starttls", "soporte", claveIMAP)
	b, err = m.CrearBuzon.Ejecutar(ctx, soporte.EntradaCrearBuzon{DatosBuzon: datosBuzon(pruebas.Correo("b"), st.puerto, 0, "starttls")})
	if err != nil {
		t.Fatal(err)
	}
	if r, err := m.ProbarBuzon.Ejecutar(ctx, soporte.EntradaVerBuzon{ID: b.ID}); err != nil || !r.IMAP.OK || r.SMTP != nil {
		t.Fatalf("starttls: %+v %v", r, err)
	}

	// Certificado de otra CA: error de TLS (nunca se acepta sin verificar).
	otro := moduloCorreo(t, nil)
	b, err = otro.CrearBuzon.Ejecutar(ctx, soporte.EntradaCrearBuzon{DatosBuzon: datosBuzon(pruebas.Correo("b"), st.puerto, 0, "starttls")})
	if err != nil {
		t.Fatal(err)
	}
	if r, err := otro.ProbarBuzon.Ejecutar(ctx, soporte.EntradaVerBuzon{ID: b.ID}); err != nil || r.IMAP.OK || *r.IMAP.Codigo != "tls" {
		t.Fatalf("CA desconocida: %+v %v", r, err)
	}

	// SSRF: sin redes permitidas, una IP interna escrita se rechaza al registrar y un nombre que
	// resuelve a loopback se corta al conectar.
	estricto := moduloCorreoCon(t, nil, nil)
	if _, err := estricto.CrearBuzon.Ejecutar(ctx, soporte.EntradaCrearBuzon{DatosBuzon: datosBuzon(pruebas.Correo("b"), st.puerto, 0, "tls")}); !esProblema(err, "destino_no_permitido") {
		t.Fatalf("IP interna: %v", err)
	}
	dl := datosBuzon(pruebas.Correo("b"), st.puerto, 0, "tls")
	dl.IMAP.Host = "localhost"
	b, err = estricto.CrearBuzon.Ejecutar(ctx, soporte.EntradaCrearBuzon{DatosBuzon: dl})
	if err != nil {
		t.Fatal(err)
	}
	if r, err := estricto.ProbarBuzon.Ejecutar(ctx, soporte.EntradaVerBuzon{ID: b.ID}); err != nil || r.IMAP.OK || *r.IMAP.Codigo != "destino_no_permitido" {
		t.Fatalf("localhost: %+v %v", r, err)
	}

	// Sin IRIS_CLAVE_CIFRADO no se registran buzones.
	if _, err := soporte.Nuevo(soporte.Config{}).CrearBuzon.Ejecutar(ctx, soporte.EntradaCrearBuzon{DatosBuzon: dl}); !esProblema(err, "cifrado_no_configurado") {
		t.Fatalf("sin cifrador: %v", err)
	}
}

func TestHTTPBuzones(t *testing.T) {
	_, raices := tlsPrueba(t)
	m := moduloCorreo(t, raices)
	h, _ := servidor.Nuevo(servidor.Deps{
		BD: pruebas.Pool(t), Identidad: ident, Version: "prueba", Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Modulos: []func(huma.API){m.Registrar},
	})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	admin := entrar(t, srv.URL, nil, "admin")
	cuerpo := map[string]any{
		"nombre": "Soporte", "correo": pruebas.Correo("buzon"),
		"imap": map[string]any{"host": "imap.isp.gt", "puerto": 993, "tls": "tls", "usuario": "soporte", "contrasena": claveIMAP},
		"smtp": map[string]any{"host": "smtp.isp.gt", "puerto": 587, "tls": "starttls", "usuario": "soporte", "contrasena": claveSMTP},
	}
	r := admin.pedir(http.MethodPost, "/api/v1/buzones", cuerpo, "Idempotency-Key", "b-"+pruebas.Unico())
	if r.status != http.StatusCreated || bytes.Contains(r.crudo, []byte("contrasena")) || bytes.Contains(r.crudo, []byte(claveIMAP)) {
		t.Fatalf("crear: %d %s", r.status, r.crudo)
	}
	id := int64(r.cuerpo["id"].(float64))
	ruta := fmt.Sprintf("/api/v1/buzones/%d", id)
	if g := admin.pedir(http.MethodGet, ruta, nil); g.status != http.StatusOK || bytes.Contains(g.crudo, []byte(claveIMAP)) ||
		g.cuerpo["activo"] != false {
		t.Fatalf("ver: %d %s", g.status, g.crudo)
	}
	if l := admin.pedir(http.MethodGet, "/api/v1/buzones?limit=200", nil); l.status != http.StatusOK || len(l.cuerpo["data"].([]any)) == 0 {
		t.Fatalf("listar: %d %s", l.status, l.crudo)
	}
	a := admin.pedir(http.MethodPost, ruta+"/activar", map[string]any{"version": 1})
	if a.status != http.StatusOK || a.cuerpo["activo"] != true {
		t.Fatalf("activar: %d %s", a.status, a.crudo)
	}
	admin.pedir(http.MethodPost, ruta+"/activar", map[string]any{"version": 1}).problema(t, http.StatusConflict, "conflicto_version")
	if d := admin.pedir(http.MethodPost, ruta+"/desactivar", map[string]any{"version": 2}); d.status != http.StatusOK || d.cuerpo["activo"] != false {
		t.Fatalf("desactivar: %d %s", d.status, d.crudo)
	}
	sinTLS := map[string]any{"nombre": "x", "correo": pruebas.Correo("b"),
		"imap": map[string]any{"host": "imap.isp.gt", "puerto": 143, "tls": "ninguno", "usuario": "u", "contrasena": "c"}}
	admin.pedir(http.MethodPost, "/api/v1/buzones", sinTLS).problema(t, http.StatusUnprocessableEntity, "validacion")

	// El rol soporte no administra buzones.
	sop := entrar(t, srv.URL, nil, "soporte")
	sop.pedir(http.MethodGet, "/api/v1/buzones", nil).problema(t, http.StatusForbidden, "prohibido")
}
