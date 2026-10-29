package soporte_test

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/quotedprintable"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/servidor"
	"github.com/HCDestroyer/iris/apps/api/internal/soporte"
)

const baseEncuestas = "https://iris.prueba.gt"

func clavesPrueba(t *testing.T) [][]byte {
	t.Helper()
	k := make([]byte, 32)
	_, _ = rand.Read(k)
	claves, err := soporte.ClavesEncuestas(base64.StdEncoding.EncodeToString(k), "")
	if err != nil {
		t.Fatal(err)
	}
	return claves
}

func moduloEncuestas(t *testing.T, inicio time.Time, ajustar ...func(*soporte.ConfigEncuestas)) (*soporte.Modulo, *reloj) {
	t.Helper()
	r := &reloj{t: inicio}
	ce := soporte.ConfigEncuestas{Claves: clavesPrueba(t), BaseURL: baseEncuestas}
	for _, a := range ajustar {
		a(&ce)
	}
	return soporte.Nuevo(soporte.Config{Reloj: r.ahora, Encuestas: ce}), r
}

// anonimo: una petición pública sin credenciales.
func anonimo(t *testing.T) context.Context {
	ctx := contexto.ConRequestID(context.Background(), contexto.NuevoRequestID())
	return nucleo.ConBD(ctx, pruebas.Pool(t))
}

var conEncuestas = append(append([]nucleo.Permiso{}, permisosTickets...), nucleo.PermEncuestasVer, nucleo.PermEncuestasCompartir)

func resolverNuevo(t *testing.T, m *soporte.Modulo, ctx context.Context, entrada soporte.EntradaCrearTicket) soporte.Ticket {
	t.Helper()
	tk, err := m.CrearTicket.Ejecutar(ctx, entrada)
	if err != nil {
		t.Fatal(err)
	}
	tk, err = m.ResolverTicket.Ejecutar(ctx, soporte.EntradaResolverTicket{ID: tk.ID, Version: tk.Version, Solucion: "Listo"})
	if err != nil {
		t.Fatal(err)
	}
	return tk
}

func encuestaDe(t *testing.T, m *soporte.Modulo, ctx context.Context, ticketID int64) soporte.Encuesta {
	t.Helper()
	return encuestaEn(t, m, ctx, ticketID, "")
}

// encuestaEn: la única encuesta del ticket con ese estado ("" = cualquiera).
func encuestaEn(t *testing.T, m *soporte.Modulo, ctx context.Context, ticketID int64, estado string) soporte.Encuesta {
	t.Helper()
	p, err := m.ListarEncuestas.Ejecutar(ctx, soporte.EntradaListarEncuestas{TicketID: ticketID, Estado: estado})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Data) != 1 {
		t.Fatalf("una encuesta por ticket: %d", len(p.Data))
	}
	return p.Data[0]
}

// tokenDe pide el enlace (encuestas.compartir) y devuelve su token.
func tokenDe(t *testing.T, m *soporte.Modulo, ctx context.Context, e soporte.Encuesta) string {
	t.Helper()
	enl, err := m.CompartirEncuesta.Ejecutar(ctx, soporte.EntradaCompartirEncuesta{ID: e.ID})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(enl.Enlace, baseEncuestas+"/encuesta#") {
		t.Fatalf("enlace: %v", enl.Enlace)
	}
	return strings.TrimPrefix(enl.Enlace, baseEncuestas+"/encuesta#")
}

// eventosEncuesta: los eventos del outbox de una encuesta, en orden.
func eventosEncuesta(t *testing.T, id int64) []evento {
	t.Helper()
	filas, err := pruebas.Pool(t).Query(context.Background(),
		"SELECT evento, empresa_id, payload FROM integracion.outbox WHERE (payload->>'encuesta_id')::bigint = $1 ORDER BY id", id)
	if err != nil {
		t.Fatal(err)
	}
	salida, err := pgx.CollectRows(filas, func(f pgx.CollectableRow) (evento, error) {
		var e evento
		var crudo []byte
		err := f.Scan(&e.Evento, &e.Empresa, &crudo)
		_ = json.Unmarshal(crudo, &e.Payload)
		return e, err
	})
	if err != nil {
		t.Fatal(err)
	}
	return salida
}

func TestEncuestaTicket(t *testing.T) {
	ag := agente(t)
	m, _ := moduloEncuestas(t, time.Now())
	ctx := como(t, ag, conEncuestas...)
	tk := resolverNuevo(t, m, ctx, soporte.EntradaCrearTicket{Asunto: "Sin internet", Prioridad: "alta"})

	e := encuestaDe(t, m, ctx, tk.ID)
	if e.Entidad != "ticket" || e.Canal != "enlace" || e.Estado != "pendiente" || e.Numero != tk.Numero || !e.EnlaceDisponible {
		t.Fatalf("encuesta: %+v", e)
	}
	ev := eventosEncuesta(t, e.ID)
	if len(ev) != 1 || ev[0].Evento != "encuesta.enviada" || ev[0].Payload["entidad"] != "ticket" ||
		ev[0].Payload["entidad_id"] != float64(tk.ID) || ev[0].Payload["canal"] != "enlace" {
		t.Fatalf("encuesta.enviada: %+v", ev)
	}
	// La lista no trae el enlace; se pide aparte (auditado, token como ***).
	if !e.EnlaceDisponible {
		t.Fatalf("enlace disponible: %+v", e)
	}
	anulado := tokenDe(t, m, ctx, e)
	var entradaCompartir string
	if err := pruebas.Pool(t).QueryRow(context.Background(), `SELECT entrada::text FROM auditoria.acciones
		WHERE accion = 'encuestas.compartir' AND entidad_id = $1::text AND actor_id = $2 AND resultado = 'ok'`,
		strconv.FormatInt(e.ID, 10), ag).Scan(&entradaCompartir); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(entradaCompartir, anulado) || !strings.Contains(entradaCompartir, `"token": "***"`) {
		t.Fatalf("auditoría de compartir: %s", entradaCompartir)
	}

	// Reabrir anula la pendiente (encuesta.anulada); resolver de nuevo genera otra.
	tk, err := m.ReabrirTicket.Ejecutar(ctx, soporte.EntradaReabrirTicket{ID: tk.ID, Version: tk.Version, Motivo: "Volvió"})
	if err != nil {
		t.Fatal(err)
	}
	if a := encuestaEn(t, m, ctx, tk.ID, "anulada"); a.ID != e.ID || a.EnlaceDisponible {
		t.Fatalf("anulada: %+v", a)
	}
	if ev := eventosEncuesta(t, e.ID); ev[len(ev)-1].Evento != "encuesta.anulada" || ev[len(ev)-1].Payload["motivo"] != "reabierto" {
		t.Fatalf("encuesta.anulada: %+v", ev)
	}
	_, err = m.ResponderEncuesta.Ejecutar(anonimo(t), soporte.EntradaResponderEncuesta{Token: anulado, Calificacion: 5})
	problema(t, err, 410, "encuesta_anulada")
	if pub, err := m.VerEncuestaPublica.Ejecutar(anonimo(t), soporte.EntradaEncuestaPublica{Token: anulado}); err != nil || pub.Estado != "anulada" {
		t.Fatalf("pública anulada: %+v %v", pub, err)
	}
	_, err = m.CompartirEncuesta.Ejecutar(ctx, soporte.EntradaCompartirEncuesta{ID: e.ID})
	problema(t, err, 410, "encuesta_anulada")
	if _, err := m.ResolverTicket.Ejecutar(ctx, soporte.EntradaResolverTicket{ID: tk.ID, Version: tk.Version, Solucion: "Ahora sí"}); err != nil {
		t.Fatal(err)
	}
	e = encuestaEn(t, m, ctx, tk.ID, "pendiente")

	// Página pública: datos mínimos.
	token := tokenDe(t, m, ctx, e)
	pub, err := m.VerEncuestaPublica.Ejecutar(anonimo(t), soporte.EntradaEncuestaPublica{Token: token})
	if err != nil {
		t.Fatal(err)
	}
	if pub.Estado != "pendiente" || pub.Numero != tk.Numero || pub.Entidad != "ticket" || pub.Empresa != nil || pub.Calificacion != nil {
		t.Fatalf("pública: %+v", pub)
	}

	// Responder: alerta con calificación ≤ 2.
	comentario := "Tardaron mucho"
	pub, err = m.ResponderEncuesta.Ejecutar(anonimo(t), soporte.EntradaResponderEncuesta{Token: token, Calificacion: 2, Comentario: &comentario})
	if err != nil {
		t.Fatal(err)
	}
	if pub.Estado != "respondida" || pub.Calificacion == nil || *pub.Calificacion != 2 {
		t.Fatalf("respondida: %+v", pub)
	}
	// La misma respuesta otra vez es idempotente (sin otro evento); otra distinta, 409.
	if _, err := m.ResponderEncuesta.Ejecutar(anonimo(t), soporte.EntradaResponderEncuesta{Token: token, Calificacion: 2, Comentario: ptrS("  Tardaron mucho ")}); err != nil {
		t.Fatal(err)
	}
	_, err = m.ResponderEncuesta.Ejecutar(anonimo(t), soporte.EntradaResponderEncuesta{Token: token, Calificacion: 5})
	problema(t, err, 409, "encuesta_respondida")
	ev = eventosEncuesta(t, e.ID)
	if len(ev) != 2 || ev[1].Evento != "encuesta.respondida" || ev[1].Payload["calificacion"] != float64(2) ||
		ev[1].Payload["alerta"] != true || ev[1].Payload["entidad_id"] != float64(tk.ID) {
		t.Fatalf("encuesta.respondida: %+v", ev)
	}

	e = encuestaEn(t, m, ctx, tk.ID, "respondida")
	if e.Estado != "respondida" || !e.Alerta || e.EnlaceDisponible || e.Comentario == nil || *e.Comentario != comentario {
		t.Fatalf("en el panel: %+v", e)
	}

	// Auditoría: el token y el comentario nunca en claro.
	var entrada, cambio string
	if err := pruebas.Pool(t).QueryRow(context.Background(), `
		SELECT a.entrada::text, c.despues::text FROM auditoria.acciones a JOIN auditoria.cambios c ON c.accion_id = a.id
		WHERE a.accion = 'encuestas.responder' AND a.entidad_id = $1::text AND c.campo = 'comentario'`, strconv.FormatInt(e.ID, 10)).Scan(&entrada, &cambio); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(entrada, token) || strings.Contains(entrada, comentario) || !strings.Contains(entrada, `"token": "***"`) ||
		cambio != `"***"` {
		t.Fatalf("auditoría: %s / %s", entrada, cambio)
	}
	var sinToken int
	if err := pruebas.Pool(t).QueryRow(context.Background(),
		"SELECT count(*) FROM auditoria.acciones WHERE entrada::text LIKE '%' || $1 || '%'", token).Scan(&sinToken); err != nil || sinToken != 0 {
		t.Fatalf("el token no queda en la auditoría: %d %v", sinToken, err)
	}

	// Un token inválido: 404 sin auditar.
	antes := m.RechazosEncuestaSinAuditar()
	_, err = m.VerEncuestaPublica.Ejecutar(anonimo(t), soporte.EntradaEncuestaPublica{Token: strings.Repeat("A", 43)})
	problema(t, err, 404, "encuesta_no_encontrada")
	_, err = m.ResponderEncuesta.Ejecutar(anonimo(t), soporte.EntradaResponderEncuesta{Token: "corto", Calificacion: 3})
	problema(t, err, 404, "encuesta_no_encontrada")
	if m.RechazosEncuestaSinAuditar() != antes+2 {
		t.Fatal("los rechazos se cuentan")
	}

	// El maestro de un incidente de red no tiene encuesta.
	var n int
	if err := pruebas.Pool(t).QueryRow(context.Background(),
		"SELECT count(*) FROM soporte.encuestas e JOIN soporte.tickets t ON t.id = e.ticket_id WHERE t.maestro IS NOT NULL").Scan(&n); err != nil || n != 0 {
		t.Fatalf("maestros con encuesta: %d %v", n, err)
	}
}

func TestEncuestaVencidaYValidaciones(t *testing.T) {
	ag := agente(t)
	m, r := moduloEncuestas(t, time.Now())
	ctx := como(t, ag, conEncuestas...)
	tk := resolverNuevo(t, m, ctx, soporte.EntradaCrearTicket{Asunto: "Lento"})
	token := tokenDe(t, m, ctx, encuestaDe(t, m, ctx, tk.ID))

	_, err := m.ResponderEncuesta.Ejecutar(anonimo(t), soporte.EntradaResponderEncuesta{Token: token, Calificacion: 6})
	problema(t, err, 422, "validacion")
	largo := strings.Repeat("x", 1001)
	_, err = m.ResponderEncuesta.Ejecutar(anonimo(t), soporte.EntradaResponderEncuesta{Token: token, Calificacion: 4, Comentario: &largo})
	problema(t, err, 422, "validacion")

	r.avanzar(7*24*time.Hour + time.Minute)
	pub, err := m.VerEncuestaPublica.Ejecutar(anonimo(t), soporte.EntradaEncuestaPublica{Token: token})
	if err != nil || pub.Estado != "vencida" {
		t.Fatalf("vencida: %+v %v", pub, err)
	}
	_, err = m.ResponderEncuesta.Ejecutar(anonimo(t), soporte.EntradaResponderEncuesta{Token: token, Calificacion: 4})
	problema(t, err, 410, "encuesta_vencida")
	e := encuestaDe(t, m, ctx, tk.ID)
	if e.Estado != "vencida" || e.EnlaceDisponible {
		t.Fatalf("vencida en el panel: %+v", e)
	}
	p, err := m.ListarEncuestas.Ejecutar(ctx, soporte.EntradaListarEncuestas{Estado: "vencida", TicketID: tk.ID})
	if err != nil || len(p.Data) != 1 {
		t.Fatalf("filtro vencida: %+v %v", p, err)
	}
	p, err = m.ListarEncuestas.Ejecutar(ctx, soporte.EntradaListarEncuestas{Estado: "pendiente", TicketID: tk.ID})
	if err != nil || len(p.Data) != 0 {
		t.Fatalf("filtro pendiente: %+v %v", p, err)
	}
	// Sin encuestas.ver, 403.
	_, err = m.ListarEncuestas.Ejecutar(como(t, ag), soporte.EntradaListarEncuestas{})
	problema(t, err, 403, "prohibido")
}

func TestEncuestaTopeYSinClave(t *testing.T) {
	m, _ := moduloEncuestas(t, time.Now(), func(c *soporte.ConfigEncuestas) { c.TopeFallidos = 2 })
	for i := range 3 {
		_, err := m.VerEncuestaPublica.Ejecutar(anonimo(t), soporte.EntradaEncuestaPublica{Token: strings.Repeat("B", 43)})
		if i < 2 {
			problema(t, err, 404, "encuesta_no_encontrada")
		} else {
			problema(t, err, 429, "demasiados_intentos")
		}
	}
	// Con la ventana excedida, los handlers responden 429 antes de abrir la transacción.
	problema(t, m.TopeEncuestasExcedido(), 429, "demasiados_intentos")

	// Sin clave: la encuesta se crea, pero el enlace no se puede volver a mostrar.
	ag := agente(t)
	sin, _ := moduloEncuestas(t, time.Now(), func(c *soporte.ConfigEncuestas) { c.Claves = nil })
	ctx := como(t, ag, conEncuestas...)
	tk := resolverNuevo(t, sin, ctx, soporte.EntradaCrearTicket{Asunto: "Sin clave"})
	e := encuestaDe(t, sin, ctx, tk.ID)
	if e.EnlaceDisponible || e.Estado != "pendiente" {
		t.Fatalf("sin clave: %+v", e)
	}
	// Otra clave (rotada sin la anterior): tampoco.
	otra, _ := moduloEncuestas(t, time.Now())
	tk = resolverNuevo(t, m, ctx, soporte.EntradaCrearTicket{Asunto: "Clave rotada"})
	if e := encuestaDe(t, otra, ctx, tk.ID); e.EnlaceDisponible {
		t.Fatalf("otra clave no deriva el enlace: %+v", e)
	}
}

func TestEncuestaEmpresaYAlcance(t *testing.T) {
	registrarEmpresas(t, 71, 72)
	ag := agente(t)
	asignarEmpresa(t, ag, 71)
	asignarEmpresa(t, ag, 72)
	m, _ := moduloEncuestas(t, time.Now())
	ctx71 := comoEnEmpresa(t, ag, 71, conEncuestas...)
	ctx72 := comoEnEmpresa(t, ag, 72, conEncuestas...)
	zona := int64(3)
	// La zona del ticket debe ser de su empresa (se devuelve al catálogo común al terminar).
	if _, err := pruebas.Pool(t).Exec(context.Background(), "UPDATE agenda.zonas SET empresa_id = 71 WHERE id = 3"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pruebas.Pool(t).Exec(context.Background(), "UPDATE agenda.zonas SET empresa_id = NULL WHERE id = 3")
	})
	tk71 := resolverNuevo(t, m, ctx71, soporte.EntradaCrearTicket{Asunto: "Empresa 71", ZonaID: &zona})
	tk72 := resolverNuevo(t, m, ctx72, soporte.EntradaCrearTicket{Asunto: "Empresa 72"})

	p, err := m.ListarEncuestas.Ejecutar(ctx72, soporte.EntradaListarEncuestas{TicketID: tk71.ID})
	if err != nil || len(p.Data) != 0 {
		t.Fatalf("otra empresa no la ve: %+v %v", p, err)
	}
	encuestaDe(t, m, ctx72, tk72.ID)
	// Con alcance por zona: solo las de su zona.
	porZona := contexto.ConActor(ctx71, contexto.ActorDe(ctx71), contexto.NuevosPermisos(map[string][]contexto.Alcance{
		string(nucleo.PermEncuestasVer): {{ZonaID: 3}},
	}))
	if e := encuestaDe(t, m, porZona, tk71.ID); e.Numero != tk71.Numero {
		t.Fatalf("zona 3: %+v", e)
	}
	porZona = contexto.ConActor(ctx71, contexto.ActorDe(ctx71), contexto.NuevosPermisos(map[string][]contexto.Alcance{
		string(nucleo.PermEncuestasVer): {{ZonaID: 4}},
	}))
	if p, err := m.ListarEncuestas.Ejecutar(porZona, soporte.EntradaListarEncuestas{TicketID: tk71.ID}); err != nil || len(p.Data) != 0 {
		t.Fatalf("zona 4 no la ve: %+v %v", p, err)
	}

	// La página pública dice el nombre de la empresa; la encuesta queda en la empresa.
	e := encuestaDe(t, m, ctx71, tk71.ID)
	pub, err := m.VerEncuestaPublica.Ejecutar(anonimo(t), soporte.EntradaEncuestaPublica{Token: tokenDe(t, m, ctx71, e)})
	if err != nil || pub.Empresa == nil || *pub.Empresa != "Empresa 71" {
		t.Fatalf("empresa: %+v %v", pub, err)
	}
	if ev := eventosEncuesta(t, e.ID); ev[0].Empresa == nil || *ev[0].Empresa != 71 {
		t.Fatalf("evento con la empresa: %+v", ev)
	}
}

// encolador falso: guarda los jobs encolados.
type encolador struct {
	mu   sync.Mutex
	jobs []river.JobArgs
}

func (e *encolador) encolar(_ context.Context, _ pgx.Tx, args river.JobArgs) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.jobs = append(e.jobs, args)
	return nil
}

func TestEncuestaPorCorreo(t *testing.T) {
	ag := agente(t)
	ctx := como(t, ag, append(append([]nucleo.Permiso{}, conEncuestas...), permisosBuzones...)...)
	cfg, raices := tlsPrueba(t)
	cola := &encolador{}
	claves := clavesPrueba(t)
	e := escenario{
		m: moduloCorreoAjustado(t, raices, []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}, func(c *soporte.Config) {
			c.Encuestas = soporte.ConfigEncuestas{Claves: claves, BaseURL: baseEncuestas}
			c.Encolar = cola.encolar
		}),
		raices: raices, imap: nuevoIMAPFalso(t, cfg, "tls", "soporte", claveIMAP),
		smtp: nuevoSMTPFalso(t, cfg, "starttls"), sis: pruebas.ContextoSistema(pruebas.Pool(t)),
	}
	b, err := e.m.CrearBuzon.Ejecutar(ctx, soporte.EntradaCrearBuzon{DatosBuzon: datosBuzon(pruebas.Correo("soporte"), e.imap.puerto, e.smtp.puerto, "tls")})
	if err != nil {
		t.Fatal(err)
	}
	if e.buzon, err = e.m.ActivarBuzon.Ejecutar(ctx, soporte.EntradaCambiarBuzon{ID: b.ID, Version: b.Version}); err != nil {
		t.Fatal(err)
	}
	cliente := pruebas.Correo("cliente")
	id1 := "enc." + pruebas.Unico() + "@cliente"
	e.imap.agregar(t, crudo(cliente, "Sin señal", id1, "No tengo internet"))
	e.leerYProcesar(t)
	tkID := *entranteDe(t, e.buzon.ID, id1).ticketID
	tk := ticketBD(t, e.m, ctx, tkID)
	if _, err := e.m.ResolverTicket.Ejecutar(ctx, soporte.EntradaResolverTicket{ID: tkID, Version: tk.Version, Solucion: "Cambiamos el ONT"}); err != nil {
		t.Fatal(err)
	}
	enc := encuestaDe(t, e.m, ctx, tkID)
	if enc.Canal != "correo" {
		t.Fatalf("canal: %+v", enc)
	}
	var salienteID int64
	if err := pruebas.Pool(t).QueryRow(context.Background(),
		"SELECT id FROM soporte.correos_salientes WHERE encuesta_id = $1 AND mensaje_id IS NULL", enc.ID).Scan(&salienteID); err != nil {
		t.Fatal(err)
	}
	encolado := false
	for _, j := range cola.jobs {
		if a, ok := j.(soporte.ArgsEnviarCorreo); ok && a.SalienteID == salienteID {
			encolado = true
		}
	}
	if !encolado {
		t.Fatalf("se encola el envío: %+v", cola.jobs)
	}
	// El worker envía (dos veces el mismo job: un solo correo) con el enlace derivado.
	antes := len(e.smtp.recibidos())
	for range 2 {
		if err := e.m.EnviarCorreo(e.sis, salienteID); err != nil {
			t.Fatal(err)
		}
	}
	enviados := e.smtp.recibidos()
	if len(enviados) != antes+1 {
		t.Fatalf("un solo envío: %d", len(enviados)-antes)
	}
	crudoCorreo := enviados[len(enviados)-1]
	_, cuerpoQP, _ := strings.Cut(crudoCorreo, "\r\n\r\n")
	cuerpo, err := io.ReadAll(quotedprintable.NewReader(strings.NewReader(cuerpoQP)))
	if err != nil {
		t.Fatal(err)
	}
	// Salió por correo: el enlace no se comparte desde el panel.
	_, err = e.m.CompartirEncuesta.Ejecutar(ctx, soporte.EntradaCompartirEncuesta{ID: enc.ID})
	problema(t, err, 409, "encuesta_por_correo")
	if enc.EnlaceDisponible {
		t.Fatal("con canal correo no hay enlace para compartir")
	}
	_, token, _ := strings.Cut(string(cuerpo), baseEncuestas+"/encuesta#")
	token = token[:43]
	if !strings.Contains(string(cuerpo), baseEncuestas+"/encuesta#"+token) || !strings.Contains(crudoCorreo, "To: <"+cliente+">") ||
		!strings.Contains(crudoCorreo, "In-Reply-To: <"+id1+">") {
		t.Fatalf("correo de la encuesta:\n%s\n%s", crudoCorreo, cuerpo)
	}
	// El enlace del correo funciona.
	if _, err := e.m.ResponderEncuesta.Ejecutar(anonimo(t), soporte.EntradaResponderEncuesta{Token: token, Calificacion: 5}); err != nil {
		t.Fatal(err)
	}
	if ev := eventosEncuesta(t, enc.ID); ev[0].Payload["canal"] != "correo" || ev[1].Payload["alerta"] != false {
		t.Fatalf("eventos: %+v", ev)
	}
}

// Fusionar un ticket resuelto anula su encuesta pendiente.
func TestEncuestaAnuladaAlFusionar(t *testing.T) {
	ag := agente(t)
	m, _ := moduloEncuestas(t, time.Now())
	ctx := como(t, ag, conEncuestas...)
	dup := resolverNuevo(t, m, ctx, soporte.EntradaCrearTicket{Asunto: "Duplicado"})
	destino, err := m.CrearTicket.Ejecutar(ctx, soporte.EntradaCrearTicket{Asunto: "Original"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.FusionarTickets.Ejecutar(ctx, soporte.EntradaFusionarTickets{ID: dup.ID, Version: dup.Version, DestinoID: destino.ID}); err != nil {
		t.Fatal(err)
	}
	e := encuestaEn(t, m, ctx, dup.ID, "anulada")
	if ev := eventosEncuesta(t, e.ID); ev[len(ev)-1].Evento != "encuesta.anulada" || ev[len(ev)-1].Payload["motivo"] != "fusionado" ||
		ev[len(ev)-1].Payload["entidad_id"] != float64(dup.ID) {
		t.Fatalf("encuesta.anulada: %+v", ev)
	}
}

// Por HTTP: la lista no trae el enlace; compartir lo da; el cliente consulta y responde con el
// token en el cuerpo, sin sesión, y también con la sesión del panel abierta (la cookie no cuenta).
func TestHTTPEncuesta(t *testing.T) {
	m, _ := moduloEncuestas(t, time.Now())
	h, _ := servidor.Nuevo(servidor.Deps{
		BD: pruebas.Pool(t), Identidad: ident, Version: "prueba", Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Modulos: []func(huma.API){m.Registrar},
	})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	agenteHTTP := entrar(t, srv.URL, nil, "soporte")
	nuevaEncuesta := func() (int64, int64) {
		t.Helper()
		r := agenteHTTP.pedir(http.MethodPost, "/api/v1/tickets", map[string]any{"asunto": "Sin señal"})
		id := int64(r.cuerpo["id"].(float64))
		r = agenteHTTP.pedir(http.MethodPost, fmt.Sprintf("/api/v1/tickets/%d/resolver", id), map[string]any{"version": r.cuerpo["version"], "solucion": "Listo"})
		if r.status != http.StatusOK {
			t.Fatalf("resolver: %d %s", r.status, r.crudo)
		}
		r = agenteHTTP.pedir(http.MethodGet, fmt.Sprintf("/api/v1/encuestas?ticketId=%d", id), nil)
		if r.status != http.StatusOK || len(r.cuerpo["data"].([]any)) != 1 {
			t.Fatalf("lista: %d %s", r.status, r.crudo)
		}
		fila := r.cuerpo["data"].([]any)[0].(map[string]any)
		if _, ok := fila["enlace"]; ok || fila["enlaceDisponible"] != true || strings.Contains(string(r.crudo), "encuesta#") {
			t.Fatalf("la lista no trae el enlace: %s", r.crudo)
		}
		return id, int64(fila["id"].(float64))
	}
	_, encuestaID := nuevaEncuesta()
	r := agenteHTTP.pedir(http.MethodPost, fmt.Sprintf("/api/v1/encuestas/%d/enlace", encuestaID), nil)
	if r.status != http.StatusOK || r.h.Get("Cache-Control") != "no-store" {
		t.Fatalf("compartir: %d %s", r.status, r.crudo)
	}
	token := strings.TrimPrefix(r.cuerpo["enlace"].(string), baseEncuestas+"/encuesta#")

	cliente := &clienteHTTP{t: t, base: srv.URL, http: &http.Client{}}
	r = cliente.pedir(http.MethodPost, "/api/v1/encuestas/publica/consultar", map[string]any{"token": token})
	if r.status != http.StatusOK || r.cuerpo["estado"] != "pendiente" || r.cuerpo["entidad"] != "ticket" {
		t.Fatalf("pública: %d %s", r.status, r.crudo)
	}
	for _, clave := range []string{"comentario", "calificacion", "id"} {
		if _, ok := r.cuerpo[clave]; ok {
			t.Fatalf("la página pública no trae %s: %s", clave, r.crudo)
		}
	}
	for range 2 { // la segunda, igual: idempotente
		r = cliente.pedir(http.MethodPost, "/api/v1/encuestas/publica/responder", map[string]any{"token": token, "calificacion": 4, "comentario": "Bien"},
			"User-Agent", "Navegador de prueba")
		if r.status != http.StatusOK || r.cuerpo["estado"] != "respondida" {
			t.Fatalf("responder: %d %s", r.status, r.crudo)
		}
	}
	var ip, ua string
	if err := pruebas.Pool(t).QueryRow(context.Background(),
		"SELECT respondida_ip, respondida_user_agent FROM soporte.encuestas WHERE id = $1", encuestaID).Scan(&ip, &ua); err != nil ||
		ip != "127.0.0.1" || ua != "Navegador de prueba" {
		t.Fatalf("ip y user agent: %q %q %v", ip, ua, err)
	}
	cliente.pedir(http.MethodPost, "/api/v1/encuestas/publica/responder", map[string]any{"token": token, "calificacion": 1}).
		problema(t, http.StatusConflict, "encuesta_respondida")
	cliente.pedir(http.MethodPost, "/api/v1/encuestas/publica/responder", map[string]any{"token": token, "calificacion": 9}).
		problema(t, http.StatusUnprocessableEntity, "validacion")
	cliente.pedir(http.MethodPost, "/api/v1/encuestas/publica/consultar", map[string]any{"token": strings.Repeat("C", 43)}).
		problema(t, http.StatusNotFound, "encuesta_no_encontrada")
	// Las rutas viejas con el token en la ruta ya no existen.
	if r := cliente.pedir(http.MethodGet, "/api/v1/encuestas/publica/"+token, nil); r.status != http.StatusNotFound && r.status != http.StatusMethodNotAllowed {
		t.Fatalf("ruta vieja: %d", r.status)
	}

	// Con la sesión del panel abierta y sin X-CSRF-Token: la cookie no cuenta (ni 403
	// csrf_invalido ni el usuario del panel como actor).
	_, otra := nuevaEncuesta()
	r = agenteHTTP.pedir(http.MethodPost, fmt.Sprintf("/api/v1/encuestas/%d/enlace", otra), nil)
	token2 := strings.TrimPrefix(r.cuerpo["enlace"].(string), baseEncuestas+"/encuesta#")
	conCookie := &clienteHTTP{t: t, base: srv.URL, http: agenteHTTP.http}
	r = conCookie.pedir(http.MethodPost, "/api/v1/encuestas/publica/responder", map[string]any{"token": token2, "calificacion": 5})
	if r.status != http.StatusOK {
		t.Fatalf("con la sesión del panel: %d %s", r.status, r.crudo)
	}
	var actor string
	if err := pruebas.Pool(t).QueryRow(context.Background(), `SELECT actor_tipo FROM auditoria.acciones
		WHERE accion = 'encuestas.responder' AND entidad_id = $1::text`, strconv.FormatInt(otra, 10)).Scan(&actor); err != nil || actor != "anonimo" {
		t.Fatalf("actor de la respuesta: %q %v", actor, err)
	}

	// La lista exige encuestas.ver (lectura no lo tiene) y compartir, encuestas.compartir.
	lectura := entrar(t, srv.URL, nil, "lectura")
	lectura.pedir(http.MethodGet, "/api/v1/encuestas", nil).problema(t, http.StatusForbidden, "prohibido")
	lectura.pedir(http.MethodPost, fmt.Sprintf("/api/v1/encuestas/%d/enlace", otra), nil).problema(t, http.StatusForbidden, "prohibido")
}
