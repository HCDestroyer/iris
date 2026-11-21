package mensajeria_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HCDestroyer/iris/apps/api/internal/mensajeria"
	"github.com/HCDestroyer/iris/apps/api/internal/mensajeria/ami/amifalso"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// Centralita Issabel, E4a (contratos/voz.md §3.12): Iris contra un AMI falso sobre TLS, de punta a
// punta: configurar, probar, encender, escuchar, la ventana por el feed de la bandeja y las acciones.

const secretoAMI = "s3cr3to-AMI-largo"

type bancoCentralita struct {
	*bandeja
	ami        *amifalso.Servidor
	admin      persona
	id         int64
	version    float64
	cancelar   context.CancelFunc
	motorHecho chan struct{}
}

func nuevoBancoCentralita(t *testing.T, ajustes ...func(*mensajeria.Config)) *bancoCentralita {
	t.Helper()
	falso, err := amifalso.Nuevo("iris_lectura", secretoAMI)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(falso.Cerrar)
	b := nuevaBandeja(t, func(c *mensajeria.Config) {
		c.CentralitaLoopback = true
		c.CentralitaPuertos = []int{falso.Puerto()}
		c.CentralitaRevision = 100 * time.Millisecond
		c.CentralitaGracia = 400 * time.Millisecond
		// El plazo tiene que ser menor que el latido (ValidarCentralitas): arriendo de 3 s, que se
		// renueva cada 600 ms y alcanza hasta 2,15 s sin renovar.
		c.CentralitaPing = 600 * time.Millisecond
		c.CentralitaPlazo = 250 * time.Millisecond
		c.ChatLatido = 150 * time.Millisecond
		for _, f := range ajustes {
			f(c)
		}
	})
	x := &bancoCentralita{bandeja: b, ami: falso}
	x.admin = b.persona([]int64{1}, rolBase("admin"))
	x.admin.empresa = 1
	// Una centralita por empresa: cada prueba empieza sin ninguna.
	x.limpiar()
	return x
}

// limpiar borra las centralitas y sus llamadas (al empezar; el teléfono también al terminar: sus
// salientes son de la empresa 1 y las pruebas de WhatsApp cuentan las salientes del día).
func (x *bancoCentralita) limpiar() {
	x.t.Helper()
	for _, s := range []string{
		`DELETE FROM mensajeria.conv_eventos WHERE conversacion_id IS NULL`,
		`DELETE FROM mensajeria.centralita_receptores`,
		`DELETE FROM mensajeria.telefono_tickets`,
		`DELETE FROM mensajeria.telefono_conexiones`,
		`DELETE FROM mensajeria.centralita_grupos`,
		`DELETE FROM mensajeria.llamadas WHERE origen = 'celular'`,
		`DELETE FROM mensajeria.llamadas WHERE origen = 'centralita'`,
		`DELETE FROM mensajeria.centralita_extensiones`,
		`DELETE FROM mensajeria.centralitas`,
	} {
		x.sql(s)
	}
}

func (x *bancoCentralita) sql(s string, args ...any) {
	x.t.Helper()
	if _, err := x.bd.Exec(x.ctx, s, args...); err != nil {
		x.t.Fatal(err)
	}
}

func (x *bancoCentralita) datos() map[string]any {
	return map[string]any{"host": "127.0.0.1", "puerto": x.ami.Puerto(), "usuario": "iris_lectura", "secreto": secretoAMI}
}

// conectar hace el camino del panel: probar, guardar con la huella, probar la guardada y encender.
func (x *bancoCentralita) conectar() {
	x.t.Helper()
	r := x.api(x.admin, http.MethodPost, "/centralitas/probar", x.datos())
	x.exigir(r, http.StatusOK)
	if r.cuerpo["estado"] != "ok" || r.cuerpo["huella"] != x.ami.Huella() {
		x.t.Fatalf("probar: %v", r.cuerpo)
	}
	c := x.datos()
	c["nombre"], c["huellaTls"] = "Oficina central", r.cuerpo["huella"]
	r = x.api(x.admin, http.MethodPost, "/centralitas", c)
	x.exigir(r, http.StatusCreated)
	x.id, x.version = int64(r.cuerpo["id"].(float64)), r.cuerpo["version"].(float64)
	if r.cuerpo["activa"] != false || r.cuerpo["secretoGuardadoEn"] == nil {
		x.t.Fatalf("crear: %v", r.cuerpo)
	}
	if strings.Contains(fmt.Sprint(r.cuerpo), secretoAMI) {
		x.t.Fatal("la respuesta trae la contraseña")
	}
	// Encender sin una prueba de la guardada: no.
	x.exigirProblema(x.api(x.admin, http.MethodPost, x.ruta("/activar"), map[string]any{"version": x.version}), http.StatusUnprocessableEntity, "sin_probar")
	r = x.api(x.admin, http.MethodPost, x.ruta("/probar"), map[string]any{})
	x.exigir(r, http.StatusOK)
	if r.cuerpo["estado"] != "ok" {
		x.t.Fatalf("probar guardada: %v", r.cuerpo)
	}
	x.version = r.cuerpo["centralita"].(map[string]any)["version"].(float64)
	r = x.api(x.admin, http.MethodPost, x.ruta("/activar"), map[string]any{"version": x.version})
	x.exigir(r, http.StatusOK)
	x.version = r.cuerpo["version"].(float64)
}

func (x *bancoCentralita) ruta(resto string) string {
	return fmt.Sprintf("/centralitas/%d%s", x.id, resto)
}

func (x *bancoCentralita) asignar(p persona, ext string) float64 {
	x.t.Helper()
	r := x.api(x.admin, http.MethodPost, x.ruta("/extensiones/asignar"), map[string]any{"usuarioId": p.id, "extension": ext})
	x.exigir(r, http.StatusOK)
	return r.cuerpo["id"].(float64)
}

// arrancar corre el motor (en el proceso de mensajería lo arranca main) y espera la conexión.
func (x *bancoCentralita) arrancar() {
	x.t.Helper()
	ctx, cancelar := context.WithCancel(context.Background())
	x.cancelar, x.motorHecho = cancelar, make(chan struct{})
	go func() {
		defer close(x.motorHecho)
		x.m.CorrerCentralitas(ctx, x.bd.(*pgxpool.Pool), nil)
	}()
	x.t.Cleanup(func() {
		cancelar()
		<-x.motorHecho
	})
	x.esperarEstado("conectada")
	x.hasta(func() bool { return x.ami.Conectados() == 1 }, "el motor no se conectó al AMI")
}

func (x *bancoCentralita) hasta(cond func() bool, msg string) {
	x.t.Helper()
	limite := time.Now().Add(8 * time.Second)
	for !cond() {
		if time.Now().After(limite) {
			x.t.Fatal(msg)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (x *bancoCentralita) esperarEstado(estado string) map[string]any {
	x.t.Helper()
	var c map[string]any
	x.hasta(func() bool {
		r := x.api(x.admin, http.MethodGet, "/centralita", nil)
		if r.status != http.StatusOK || r.cuerpo["centralita"] == nil {
			return false
		}
		c = r.cuerpo["centralita"].(map[string]any)
		return c["estado"] == estado
	}, "la centralita no quedó "+estado)
	return c
}

// cursor: la cabeza del feed de la bandeja para p.
func (x *bancoCentralita) cursor(p persona) string {
	x.t.Helper()
	r := x.api(p, http.MethodGet, "/conversaciones/cambios", nil)
	x.exigir(r, http.StatusOK)
	return r.cuerpo["nextCursor"].(string)
}

// eventos lee el feed de p desde *cursor hasta que llega uno del tipo pedido (y avanza el cursor).
func (x *bancoCentralita) eventos(p persona, cursor *string, tipo string) []map[string]any {
	x.t.Helper()
	var todos []map[string]any
	x.hasta(func() bool {
		r := x.api(p, http.MethodGet, "/conversaciones/cambios?cursor="+*cursor, nil)
		x.exigir(r, http.StatusOK)
		*cursor = r.cuerpo["nextCursor"].(string)
		hay := false
		for _, e := range r.cuerpo["data"].([]any) {
			ev := e.(map[string]any)
			todos = append(todos, ev)
			if ev["tipo"] == tipo {
				hay = true
			}
		}
		return hay
	}, "no llegó "+tipo)
	return todos
}

func (x *bancoCentralita) sinEventos(p persona, cursor string) {
	x.t.Helper()
	r := x.api(p, http.MethodGet, "/conversaciones/cambios?cursor="+cursor, nil)
	x.exigir(r, http.StatusOK)
	if n := len(r.cuerpo["data"].([]any)); n != 0 {
		x.t.Fatalf("recibió %d eventos que no son suyos: %v", n, r.cuerpo["data"])
	}
}

func ultimo(evs []map[string]any, tipo string) map[string]any {
	for i := len(evs) - 1; i >= 0; i-- {
		if evs[i]["tipo"] == tipo {
			return evs[i]
		}
	}
	return nil
}

// clienteConMovil siembra un cliente de la instantánea con ese móvil (8 dígitos) y un servicio.
func (x *bancoCentralita) clienteConMovil(movil, nombre string) int64 {
	x.t.Helper()
	id := siguienteCliente.Add(1)
	x.sql(`INSERT INTO integracion.argos_clientes (id, empresa_id, nombre, movil, codigo, sincronizado_en, empresas)
		VALUES ($1, 1, $2, $3, $4, now(), '{1}')`, id, nombre, movil, fmt.Sprint("C-", id))
	x.sql(`INSERT INTO integracion.argos_servicios (id, empresa_id, cliente_id, codigo, plan_nombre, estado, zona_nombre, sincronizado_en)
		VALUES ($1, 1, $2, 'S-1', 'Fibra 50 Mbps', 'activo', 'Zona Norte', now())`, id*10, id)
	return id
}

func movilUnico() string { return fmt.Sprintf("5%07d", siguienteCliente.Add(1)%10000000) }

func (x *bancoCentralita) outbox(evento, llamada string) map[string]any {
	x.t.Helper()
	var p map[string]any
	if err := x.bd.QueryRow(x.ctx, `SELECT payload FROM integracion.outbox WHERE evento = $1 AND payload->>'llamada_id' = $2 ORDER BY id DESC LIMIT 1`,
		evento, llamada).Scan(&p); err != nil {
		x.t.Fatalf("outbox %s: %v", evento, err)
	}
	return p
}

func TestCentralitaDePuntaAPunta(t *testing.T) {
	x := nuevoBancoCentralita(t)
	x.conectar()
	x.asignar(x.agente, "201")
	x.arrancar()
	movil := movilUnico()
	cliente := x.clienteConMovil(movil, "María Fernanda Pérez")
	cur, curOtro := x.cursor(x.agente), x.cursor(x.otro)

	// Suena la extensión 201 (PJSIP) desde la troncal.
	x.ami.Emitir("Event", "DialBegin", "Channel", "PJSIP/claro-00000031", "CallerIDNum", movil, "CallerIDName", "<b>Falso</b>",
		"DestChannel", "PJSIP/201-0000001a", "Linkedid", "1700000000.100", "Uniqueid", "1700000000.100")
	ev := ultimo(x.eventos(x.agente, &cur, "llamada_entrante"), "llamada_entrante")
	l := ev["llamada"].(map[string]any)
	if l["origen"] != "centralita" || l["estado"] != "sonando" || l["titulo"] != "María Fernanda Pérez" || l["numero"] != "+502"+movil ||
		l["contacto"].(map[string]any)["clienteId"] != float64(cliente) || ev["conversacionId"] != float64(0) {
		t.Fatalf("ventana: %v", ev)
	}
	c := l["centralita"].(map[string]any)
	if c["tuExtension"] != "201" || l["puede"].(map[string]any)["contestar"] != false || l["puede"].(map[string]any)["anotar"] != true {
		t.Fatalf("centralita: %v", l)
	}
	if l["senales"] == nil || l["senales"].(map[string]any)["servicio"].(map[string]any)["plan"] != "Fibra 50 Mbps" {
		t.Fatalf("la línea de servicio: %v", l["senales"])
	}
	if strings.Contains(fmt.Sprint(ev), "Falso") {
		t.Fatal("el CallerIDName (no confiable) no se muestra")
	}
	id := l["id"].(string)
	// A quien no le sonó no le llega nada.
	x.sinEventos(x.otro, curOtro)
	// El mismo DialBegin otra vez (Asterisk lo repite en reintentos): una sola llamada.
	x.ami.Emitir("Event", "DialBegin", "Channel", "PJSIP/claro-00000031", "CallerIDNum", movil, "DestChannel", "PJSIP/201-0000001b",
		"Linkedid", "1700000000.100")

	x.ami.Emitir("Event", "DialEnd", "Channel", "PJSIP/claro-00000031", "DestChannel", "PJSIP/201-0000001a", "DialStatus", "ANSWER", "Linkedid", "1700000000.100")
	ev = ultimo(x.eventos(x.agente, &cur, "llamada_actualizada"), "llamada_actualizada")
	if l := ev["llamada"].(map[string]any); l["estado"] != "en_curso" || l["usuario"].(map[string]any)["id"] != float64(x.agente.id) {
		t.Fatalf("contestada: %v", ev)
	}
	x.ami.Emitir("Event", "Hangup", "Channel", "PJSIP/claro-00000031", "Uniqueid", "1700000000.100", "Linkedid", "1700000000.100")
	ev = ultimo(x.eventos(x.agente, &cur, "llamada_terminada"), "llamada_terminada")
	if l := ev["llamada"].(map[string]any); l["estado"] != "terminada" {
		t.Fatalf("terminada: %v", ev)
	}
	x.ami.Emitir("Event", "Cdr", "UniqueID", "1700000000.100", "Disposition", "ANSWERED", "BillableSeconds", "245")
	x.hasta(func() bool {
		var d *int32
		_ = x.bd.QueryRow(x.ctx, `SELECT duracion_seg FROM mensajeria.llamadas WHERE id = $1`, id).Scan(&d)
		return d != nil && *d == 245
	}, "el CDR no dejó la duración")
	var n int
	if err := x.bd.QueryRow(x.ctx, `SELECT count(*) FROM mensajeria.llamadas WHERE origen = 'centralita'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("llamadas: %d %v", n, err)
	}
	// Eventos por outbox (los mismos llamada.* que WhatsApp, con origen centralita).
	if p := x.outbox("llamada.entrante", id); p["origen"] != "centralita" || p["conversacion_id"] != nil || p["cliente_id"] != float64(cliente) {
		t.Fatalf("llamada.entrante: %v", p)
	}
	x.outbox("llamada.contestada", id)
	x.outbox("llamada.terminada", id)

	// La ficha del cliente: la llamada en su historial.
	r := x.api(x.agente, http.MethodGet, fmt.Sprintf("/centralitas/llamadas?clienteId=%d", cliente), nil)
	x.exigir(r, http.StatusOK)
	if d := r.cuerpo["data"].([]any); len(d) != 1 || d[0].(map[string]any)["id"] != id {
		t.Fatalf("historial: %v", r.cuerpo)
	}
	// Anotar: la nota no va a la auditoría.
	r = x.api(x.agente, http.MethodPost, "/centralitas/llamadas/"+id+"/anotar", map[string]any{"version": d0(r)["version"], "nota": "Se reinició la ONT; DPI 1234",
		"resultado": "resuelto"})
	x.exigir(r, http.StatusOK)
	if r.cuerpo["nota"] != "Se reinició la ONT; DPI 1234" || r.cuerpo["resultado"] != "resuelto" {
		t.Fatalf("anotar: %v", r.cuerpo)
	}
	var auditado int
	if err := x.bd.QueryRow(x.ctx, `SELECT count(*) FROM auditoria.acciones WHERE entrada::text LIKE '%DPI 1234%' OR resultado::text LIKE '%DPI 1234%'`).Scan(&auditado); err != nil || auditado != 0 {
		t.Fatalf("la nota quedó en la auditoría (%d) %v", auditado, err)
	}
	// Otra persona sin ser receptora ni supervisar: 404.
	x.exigir(x.api(x.otro, http.MethodPost, "/centralitas/llamadas/"+id+"/anotar", map[string]any{"version": r.cuerpo["version"], "nota": "x"}), http.StatusNotFound)
	// Las rutas de llamadas de WhatsApp no ven las de la centralita.
	x.exigir(x.api(x.agente, http.MethodGet, "/llamadas/"+id, nil), http.StatusNotFound)

	// La contraseña: cifrada en la base y nunca en la auditoría ni en el outbox.
	var cif []byte
	if err := x.bd.QueryRow(x.ctx, `SELECT secreto_cifrado FROM mensajeria.centralitas WHERE id = $1`, x.id).Scan(&cif); err != nil || strings.Contains(string(cif), secretoAMI) {
		t.Fatal("el secreto no está cifrado")
	}
	for _, q := range []string{`SELECT count(*) FROM auditoria.acciones WHERE entrada::text LIKE '%' || $1 || '%'`,
		`SELECT count(*) FROM auditoria.cambios WHERE antes::text LIKE '%' || $1 || '%' OR despues::text LIKE '%' || $1 || '%'`,
		`SELECT count(*) FROM integracion.outbox WHERE payload::text LIKE '%' || $1 || '%'`} {
		if err := x.bd.QueryRow(x.ctx, q, secretoAMI).Scan(&n); err != nil || n != 0 {
			t.Fatalf("el secreto apareció (%d) en %s: %v", n, q, err)
		}
	}
	// Iris nunca mandó una acción de escritura al AMI.
	for _, a := range x.ami.Recibido() {
		switch strings.ToLower(a) {
		case "login", "ping", "listcommands", "logoff":
		default:
			t.Fatalf("Iris mandó %q al AMI", a)
		}
	}
}

func d0(r respuesta) map[string]any { return r.cuerpo["data"].([]any)[0].(map[string]any) }

func TestCentralitaGrupoDeTimbradoYCola(t *testing.T) {
	x := nuevoBancoCentralita(t)
	x.conectar()
	x.asignar(x.agente, "201")
	x.asignar(x.otro, "202")
	x.arrancar()
	curA, curB := x.cursor(x.agente), x.cursor(x.otro)
	// Cola de Issabel: primero los Local (no hacen nada), después los teléfonos (chan_sip y PJSIP).
	for _, ext := range []string{"201", "202"} {
		x.ami.Emitir("Event", "DialBegin", "Channel", "PJSIP/claro-1", "CallerIDNum", "", "DestChannel", "Local/"+ext+"@from-queue-0000002b;1", "Linkedid", "1700.200")
	}
	x.ami.Emitir("Event", "DialBegin", "Channel", "Local/201@from-queue-0000002b;2", "CallerIDNum", "anonymous", "DestChannel", "PJSIP/201-0000003c", "Linkedid", "1700.200")
	x.ami.Emitir("Event", "DialBegin", "Channel", "Local/202@from-queue-0000002c;2", "CallerIDNum", "anonymous", "DestChannel", "SIP/202-0000003d", "Linkedid", "1700.200")
	a := ultimo(x.eventos(x.agente, &curA, "llamada_entrante"), "llamada_entrante")["llamada"].(map[string]any)
	b := ultimo(x.eventos(x.otro, &curB, "llamada_entrante"), "llamada_entrante")["llamada"].(map[string]any)
	if a["id"] != b["id"] || a["titulo"] != "Número oculto" || a["numero"] != "" {
		t.Fatalf("misma llamada a los dos, número oculto: %v / %v", a, b)
	}
	// Contesta 202: a 201 le llega «Contestó …» con el nombre de pila.
	x.ami.Emitir("Event", "DialEnd", "Channel", "Local/202@from-queue-0000002c;2", "DestChannel", "SIP/202-0000003d", "DialStatus", "ANSWER", "Linkedid", "1700.200")
	ev := ultimo(x.eventos(x.agente, &curA, "llamada_actualizada"), "llamada_actualizada")["llamada"].(map[string]any)
	u := ev["usuario"].(map[string]any)
	if u["id"] != float64(x.otro.id) || strings.Contains(u["nombre"].(string), " ") || ev["puede"].(map[string]any)["anotar"] != true {
		t.Fatalf("contestó otro: %v", ev)
	}
}

func TestCentralitaIgnoraInternasNoRegistradasYAvalancha(t *testing.T) {
	x := nuevoBancoCentralita(t)
	x.conectar()
	x.asignar(x.agente, "201")
	x.arrancar()
	// Interna (de otro teléfono), a una extensión no registrada, y una avalancha de eventos ajenos.
	x.ami.Emitir("Event", "DialBegin", "Channel", "PJSIP/205-1", "CallerIDNum", "205", "DestChannel", "PJSIP/201-2", "Linkedid", "1700.300")
	x.ami.Emitir("Event", "DialBegin", "Channel", "PJSIP/claro-1", "CallerIDNum", "55123480", "DestChannel", "PJSIP/299-2", "Linkedid", "1700.301")
	for i := range 600 {
		x.ami.Emitir("Event", "Newstate", "Channel", fmt.Sprintf("PJSIP/3%02d-1", i%100), "Linkedid", fmt.Sprintf("1700.%d", 400+i))
	}
	// Una línea con salto inyectado en el CallerID se lee como otro campo, nunca como acción.
	x.ami.Crudo("Event: DialBegin\r\nChannel: PJSIP/claro-9\r\nCallerIDNum: 55\r\nAction: Originate\r\nDestChannel: PJSIP/299-9\r\nLinkedid: 1700.999\r\n\r\n")
	time.Sleep(500 * time.Millisecond)
	var n int
	if err := x.bd.QueryRow(x.ctx, `SELECT count(*) FROM mensajeria.llamadas WHERE origen = 'centralita'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("se guardaron %d llamadas que no son de Iris: %v", n, err)
	}
	if x.ami.Conectados() != 1 {
		t.Fatal("la conexión se cayó con eventos ajenos")
	}
}

func TestCentralitaSalienteSoloAClientes(t *testing.T) {
	x := nuevoBancoCentralita(t)
	x.conectar()
	x.asignar(x.agente, "201")
	x.arrancar()
	movil := movilUnico()
	cliente := x.clienteConMovil(movil, "Roberto Gómez")
	// Personal (número sin cliente): no queda.
	x.ami.Emitir("Event", "DialBegin", "Channel", "PJSIP/201-1", "DestChannel", "PJSIP/claro-2", "DestExten", "944556677", "Linkedid", "1700.500")
	// A un cliente (con prefijo de salida 9): queda sin ventana.
	x.ami.Emitir("Event", "DialBegin", "Channel", "PJSIP/201-3", "DestChannel", "PJSIP/claro-4", "DestExten", "9"+movil, "Linkedid", "1700.501")
	x.hasta(func() bool {
		var n int
		_ = x.bd.QueryRow(x.ctx, `SELECT count(*) FROM mensajeria.llamadas WHERE origen = 'centralita' AND direccion = 'saliente' AND cliente_id = $1 AND estado = 'marcando'`,
			cliente).Scan(&n)
		return n == 1
	}, "no quedó la saliente al cliente")
	time.Sleep(200 * time.Millisecond)
	var n int
	if err := x.bd.QueryRow(x.ctx, `SELECT count(*) FROM mensajeria.llamadas WHERE origen = 'centralita'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("la personal no debe quedar: %d %v", n, err)
	}
	if err := x.bd.QueryRow(x.ctx, `SELECT count(*) FROM mensajeria.conv_eventos WHERE conversacion_id IS NULL`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("una saliente no abre ventana: %d", n)
	}
}

func TestCentralitaPermisosDeMasNoSeEnciende(t *testing.T) {
	x := nuevoBancoCentralita(t)
	x.ami.PermitirEscritura("Originate", "Command")
	r := x.api(x.admin, http.MethodPost, "/centralitas/probar", x.datos())
	x.exigir(r, http.StatusOK)
	if r.cuerpo["estado"] != "permisos_de_mas" || fmt.Sprint(r.cuerpo["accionesDeMas"]) != "[Originate Command]" {
		t.Fatalf("probar: %v", r.cuerpo)
	}
	c := x.datos()
	c["nombre"], c["huellaTls"] = "Oficina", x.ami.Huella()
	r = x.api(x.admin, http.MethodPost, "/centralitas", c)
	x.exigir(r, http.StatusCreated)
	x.id = int64(r.cuerpo["id"].(float64))
	r = x.api(x.admin, http.MethodPost, x.ruta("/probar"), map[string]any{})
	if r.cuerpo["estado"] != "permisos_de_mas" {
		t.Fatalf("probar guardada: %v", r.cuerpo)
	}
	x.exigirProblema(x.api(x.admin, http.MethodPost, x.ruta("/activar"), map[string]any{"version": r.cuerpo["centralita"].(map[string]any)["version"]}),
		http.StatusUnprocessableEntity, "sin_probar")
	// Una dirección que no responde: el mismo error para todo.
	d := x.datos()
	d["host"] = "10.255.255.1"
	r = x.api(x.admin, http.MethodPost, "/centralitas/probar", d)
	x.exigir(r, http.StatusOK)
	if r.cuerpo["estado"] != "no_conecta" || r.cuerpo["huella"] != nil {
		t.Fatalf("no conecta: %v", r.cuerpo)
	}
	// Reglas de red y de puerto.
	d["host"], d["puerto"] = "169.254.169.254", 5039
	x.exigirProblema(x.api(x.admin, http.MethodPost, "/centralitas/probar", d), http.StatusUnprocessableEntity, "url_no_permitida")
	d["host"], d["puerto"] = "10.0.0.5", 5038
	x.exigirProblema(x.api(x.admin, http.MethodPost, "/centralitas/probar", d), http.StatusUnprocessableEntity, "puerto_no_permitido")
	// La contraseña guardada no viaja a otro servidor.
	x.exigirProblema(x.api(x.admin, http.MethodPost, "/centralitas/probar", map[string]any{"centralitaId": x.id, "host": "10.0.0.9", "puerto": 5039,
		"usuario": "iris_lectura"}), http.StatusUnprocessableEntity, "clave_requerida")
	// Sin centralita.configurar (supervisor): 403.
	x.exigir(x.api(x.supervisor, http.MethodPost, "/centralitas/probar", x.datos()), http.StatusForbidden)
}

func TestCentralitaHuellaCambiadaSeDetiene(t *testing.T) {
	x := nuevoBancoCentralita(t)
	x.conectar()
	x.arrancar()
	logins, _ := x.ami.Logins()
	if err := x.ami.CambiarCertificado(); err != nil {
		t.Fatal(err)
	}
	nueva := x.ami.Huella()
	x.ami.Cortar()
	c := x.esperarEstado("error")
	if c["errorCodigo"] != "huella_cambiada" || c["huellaVista"] != nueva {
		t.Fatalf("huella: %v", c)
	}
	time.Sleep(500 * time.Millisecond)
	if ok, _ := x.ami.Logins(); ok != logins {
		t.Fatal("con otra huella no se debe mandar el Login (ni la contraseña)")
	}
	x.exigirProblema(x.api(x.admin, http.MethodPost, x.ruta("/confirmar-huella"), map[string]any{"version": c["version"], "huella": strings.Repeat("ab", 32)}),
		http.StatusUnprocessableEntity, "huella_no_coincide")
	x.exigir(x.api(x.admin, http.MethodPost, x.ruta("/confirmar-huella"), map[string]any{"version": c["version"], "huella": nueva}), http.StatusOK)
	x.esperarEstado("conectada")
}

func TestCentralitaReconectaSinCambiarEstado(t *testing.T) {
	x := nuevoBancoCentralita(t)
	x.conectar()
	x.arrancar()
	var cambios int
	contar := func() int {
		_ = x.bd.QueryRow(x.ctx, `SELECT count(*) FROM integracion.outbox WHERE evento = 'centralita.estado_cambiado' AND (payload->>'centralita_id')::bigint = $1`,
			x.id).Scan(&cambios)
		return cambios
	}
	antes := contar()
	x.ami.Cortar()
	x.hasta(func() bool { ok, _ := x.ami.Logins(); return ok >= 2 && x.ami.Conectados() == 1 }, "no se reconectó")
	if contar() != antes {
		t.Fatal("una caída breve no debe cambiar el estado")
	}
	// Una caída larga sí: el AMI deja de responder.
	x.ami.Cerrar()
	x.esperarEstado("desconectada")
}

// Seguridad del feed: quien entra solo por su extensión (sin conversaciones.ver) recibe sus
// ventanas y nunca eventos de conversaciones; sin extensión, 403; al quitársela, el flujo se corta.
func TestCentralitaFeedSoloExtension(t *testing.T) {
	x := nuevoBancoCentralita(t)
	x.conectar()
	tec := x.persona([]int64{1}, rolBase("tecnico"))
	tec.empresa = 1
	x.exigir(x.api(tec, http.MethodGet, "/conversaciones/cambios", nil), http.StatusForbidden)
	ext := x.asignar(tec, "210")
	x.arrancar()
	if r := x.api(tec, http.MethodGet, "/centralita/mi-extension", nil); r.status != http.StatusOK || r.cuerpo["extension"] != "210" {
		t.Fatalf("mi extensión: %d %v", r.status, r.cuerpo)
	}
	cur := x.cursor(tec)
	// Un mensaje de WhatsApp (evento de conversación de la cola) y una llamada a su extensión.
	x.texto(numeroCliente(), "wamid.centralita."+pruebas.Unico(), "hola")
	x.ami.Emitir("Event", "DialBegin", "Channel", "PJSIP/claro-1", "CallerIDNum", "55998877", "DestChannel", "PJSIP/210-1", "Linkedid", "1700.700")
	evs := x.eventos(tec, &cur, "llamada_entrante")
	for _, e := range evs {
		if e["conversacionId"] != float64(0) || e["llamada"] == nil || e["llamada"].(map[string]any)["origen"] != "centralita" {
			t.Fatalf("recibió un evento de conversaciones: %v", e)
		}
	}
	// Sin clientes.ver no ve candidatos; tampoco puede anotar (sin llamadas.anotar).
	l := ultimo(evs, "llamada_entrante")["llamada"].(map[string]any)
	if l["puede"].(map[string]any)["anotar"] != false {
		t.Fatalf("técnico: %v", l)
	}
	// Le quitan la extensión: el feed se cierra (403) y el SSE se corta en la revalidación.
	url, ctx := x.servirSSE(t)
	flujo := abrirSSEEn(t, ctx, url+rutaMsj+"/conversaciones/eventos", tec, cur)
	x.exigir(x.api(x.admin, http.MethodPost, fmt.Sprintf("%s/%d/quitar", x.ruta("/extensiones"), int64(ext)), nil), http.StatusOK)
	x.exigir(x.api(tec, http.MethodGet, "/conversaciones/cambios?cursor="+cur, nil), http.StatusForbidden)
	if r := x.api(tec, http.MethodGet, "/centralita/mi-extension", nil); r.cuerpo["extension"] != nil {
		t.Fatalf("sin extensión: %v", r.cuerpo)
	}
	x.ami.Emitir("Event", "DialBegin", "Channel", "PJSIP/claro-1", "CallerIDNum", "55998877", "DestChannel", "PJSIP/210-1", "Linkedid", "1700.701")
	limite := time.After(8 * time.Second)
	for {
		select {
		case ev, ok := <-flujo:
			if !ok {
				return
			}
			if ev.tipo != "latido" {
				t.Fatalf("entregó %s sin extensión: %v", ev.tipo, ev.datos)
			}
		case <-limite:
			t.Fatal("el flujo no se cortó al quitar la extensión")
		}
	}
}
