package mensajeria_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HCDestroyer/iris/apps/api/internal/mensajeria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// Revisión de seguridad de E4a (PR #235): cada hallazgo con su prueba.

// arrancarSinEsperar corre el motor sin esperar a que se conecte.
func (x *bancoCentralita) arrancarSinEsperar() {
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
}

// replica corre otro proceso de mensajería (otro dueño de arriendo) contra la misma base.
func (x *bancoCentralita) replica() {
	x.t.Helper()
	otro := mensajeria.Nuevo(x.m.Config())
	ctx, cancelar := context.WithCancel(context.Background())
	hecho := make(chan struct{})
	go func() {
		defer close(hecho)
		otro.CorrerCentralitas(ctx, x.bd.(*pgxpool.Pool), nil)
	}()
	x.t.Cleanup(func() {
		cancelar()
		<-hecho
	})
}

// Hallazgo 1: probar con la contraseña guardada (sin escribirla) solo contra el certificado fijado.
func TestCentralitaProbarConClaveGuardadaExigeLaHuella(t *testing.T) {
	x := nuevoBancoCentralita(t)
	x.conectar()
	sinClave := map[string]any{"centralitaId": x.id, "host": "127.0.0.1", "puerto": x.ami.Puerto(), "usuario": "iris_lectura"}
	// Con el mismo certificado: pasa.
	r := x.api(x.admin, http.MethodPost, "/centralitas/probar", sinClave)
	x.exigir(r, http.StatusOK)
	if r.cuerpo["estado"] != "ok" {
		t.Fatalf("probar con la guardada: %v", r.cuerpo)
	}
	// Otro certificado (alguien en medio): huella_distinta y la contraseña no se manda.
	logins, fallidos := x.ami.Logins()
	if err := x.ami.CambiarCertificado(); err != nil {
		t.Fatal(err)
	}
	r = x.api(x.admin, http.MethodPost, "/centralitas/probar", sinClave)
	x.exigir(r, http.StatusOK)
	if r.cuerpo["estado"] != "huella_distinta" || r.cuerpo["huella"] != x.ami.Huella() {
		t.Fatalf("con otro certificado: %v", r.cuerpo)
	}
	if ok, f := x.ami.Logins(); ok != logins || f != fallidos {
		t.Fatalf("la contraseña guardada viajó a un certificado no verificado (logins %d→%d, fallidos %d→%d)", logins, ok, fallidos, f)
	}
	// Escribiéndola sí se prueba un certificado nuevo (es lo que hace el panel la primera vez).
	r = x.api(x.admin, http.MethodPost, "/centralitas/probar", x.datos())
	x.exigir(r, http.StatusOK)
	if r.cuerpo["estado"] != "ok" {
		t.Fatalf("con la contraseña escrita: %v", r.cuerpo)
	}
	// Sin huella guardada, la guardada no se usa.
	x.sql(`UPDATE mensajeria.centralitas SET activa = false, huella_tls = NULL WHERE id = $1`, x.id)
	x.exigirProblema(x.api(x.admin, http.MethodPost, "/centralitas/probar", sinClave), http.StatusUnprocessableEntity, "clave_requerida")
}

// Hallazgo 2: una llamada de la centralita colgada (sin cierre) no deja a nadie sin WhatsApp.
func TestCentralitaLlamadaVivaNoBloqueaWhatsApp(t *testing.T) {
	x := nuevaBandejaVoz(t)
	for _, s := range []string{
		`DELETE FROM mensajeria.conv_eventos WHERE conversacion_id IS NULL`,
		`DELETE FROM mensajeria.centralita_receptores`,
		`DELETE FROM mensajeria.llamadas WHERE origen = 'centralita'`,
		`DELETE FROM mensajeria.centralita_extensiones`,
		`DELETE FROM mensajeria.centralitas`,
	} {
		x.sql(s)
	}
	var central int64
	if err := x.bd.QueryRow(x.ctx, `INSERT INTO mensajeria.centralitas (empresa_id, nombre, host, usuario) VALUES (1, 'Oficina', '10.0.0.5', 'iris_lectura') RETURNING id`).
		Scan(&central); err != nil {
		t.Fatal(err)
	}
	// El AMI se cortó con la llamada en curso del agente: queda en_curso (hasta 4 h).
	x.sql(`INSERT INTO mensajeria.llamadas (id, empresa_id, origen, centralita_id, id_externo, direccion, numero, estado, usuario_id, extension, atendida_por,
		sono_en, suena_hasta, contestada_en) VALUES (gen_random_uuid(), 1, 'centralita', $1, '1700.colgada', 'entrante', '+50255001122', 'en_curso', $2, '201',
		'persona', $3, $3, $3)`, central, x.agente.id, x.reloj.Add(-time.Minute))
	t.Cleanup(func() {
		_, _ = x.bd.Exec(context.Background(), `DELETE FROM mensajeria.llamadas WHERE origen = 'centralita' AND centralita_id = $1`, central)
		_, _ = x.bd.Exec(context.Background(), `DELETE FROM mensajeria.centralitas WHERE id = $1`, central)
	})
	x.recibe(x.agente)
	num, call := numeroCliente(), "wacid.centralita"+numeroCliente()
	x.connect(num, call)
	id, estado, _ := x.llamada(call)
	if estado != "sonando" {
		t.Fatalf("estado: %s", estado)
	}
	if n := x.contar(`SELECT count(*) FROM mensajeria.conv_eventos WHERE llamada_id = $1::uuid AND tipo = 'llamada_entrante' AND usuario_id = $2`, id, x.agente.id); n != 1 {
		t.Fatalf("la llamada de la centralita le quitó el timbre de WhatsApp al agente: %d", n)
	}
	r := x.exigir(x.api(x.agente, http.MethodPost, "/llamadas/"+id+"/reclamar", nil), http.StatusOK)
	if r.cuerpo["llamada"].(map[string]any)["estado"] != "conectando" {
		t.Fatalf("reclamar: %s", crudo(r))
	}
}

// Hallazgo 3: con dos réplicas, solo una conecta; la que tiene el arriendo lo renueva mientras
// reconecta y la otra nunca conecta.
func TestCentralitaArriendoConDosReplicas(t *testing.T) {
	x := nuevoBancoCentralita(t)
	x.conectar()
	x.arrancar()
	x.replica()
	var maximo atomic.Int64
	medir := func(d time.Duration) {
		fin := time.Now().Add(d)
		for time.Now().Before(fin) {
			if n := int64(x.ami.Conectados()); n > maximo.Load() {
				maximo.Store(n)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	medir(500 * time.Millisecond)
	// La centralita deja de responder más que el arriendo (3 latidos de 300 ms → 3 s): la réplica que
	// reconecta tiene que seguir siendo la dueña.
	x.ami.Pausar(true)
	x.ami.Cortar()
	medir(4500 * time.Millisecond)
	x.ami.Pausar(false)
	medir(9 * time.Second)
	if maximo.Load() > 1 {
		t.Fatalf("hubo %d conexiones AMI a la vez (dos réplicas conectadas)", maximo.Load())
	}
	if x.ami.Conectados() != 1 {
		t.Fatalf("no quedó conectada: %d", x.ami.Conectados())
	}

	// Si otra réplica tomó el arriendo, esta no reconecta: ni tras un corte.
	x.sql(`UPDATE mensajeria.centralitas SET motor_dueno = 'otra-replica', motor_hasta = now() + interval '1 hour' WHERE id = $1`, x.id)
	logins, _ := x.ami.Logins()
	x.ami.Cortar()
	time.Sleep(2 * time.Second)
	if ok, _ := x.ami.Logins(); ok != logins || x.ami.Conectados() != 0 {
		t.Fatalf("reconectó sin el arriendo: logins %d→%d, conectados %d", logins, ok, x.ami.Conectados())
	}
	// Al soltarlo, vuelve.
	x.sql(`UPDATE mensajeria.centralitas SET motor_dueno = NULL, motor_hasta = NULL WHERE id = $1`, x.id)
	x.hasta(func() bool { return x.ami.Conectados() == 1 }, "no volvió a conectar al quedar libre el arriendo")
}

// Hallazgo 4: el tope de eventos se aplica a lo que va a la base, después de filtrar; una avalancha
// (ajena o de llamadas nuevas) no hace descartar los cierres de las llamadas seguidas.
func TestCentralitaTopeDeEventosNoDescartaCierres(t *testing.T) {
	x := nuevoBancoCentralita(t, func(c *mensajeria.Config) { c.CentralitaEventosSeg = 5 })
	x.conectar()
	x.asignar(x.agente, "201")
	x.arrancar()
	movil := movilUnico()
	x.clienteConMovil(movil, "Lucía Ramírez")
	var b strings.Builder
	evento := func(campos ...string) {
		for i := 0; i+1 < len(campos); i += 2 {
			fmt.Fprintf(&b, "%s: %s\r\n", campos[i], campos[i+1])
		}
		b.WriteString("\r\n")
	}
	evento("Event", "DialBegin", "Channel", "PJSIP/claro-1", "CallerIDNum", movil, "DestChannel", "PJSIP/201-1", "Linkedid", "1800.1", "Uniqueid", "1800.1")
	// Avalancha ajena: no cuenta (ni toca la base).
	for i := range 80 {
		evento("Event", "Newstate", "Channel", fmt.Sprintf("PJSIP/3%02d-1", i%100), "Linkedid", fmt.Sprintf("1800.%d", 100+i))
		evento("Event", "DialBegin", "Channel", "PJSIP/claro-2", "CallerIDNum", "55123480", "DestChannel", "PJSIP/299-2", "Linkedid", fmt.Sprintf("1800.%d", 300+i))
	}
	// Avalancha de llamadas nuevas a la extensión: el tope las corta…
	for i := range 20 {
		evento("Event", "DialBegin", "Channel", "PJSIP/claro-3", "CallerIDNum", "55998877", "DestChannel", "PJSIP/201-3", "Linkedid", fmt.Sprintf("1800.%d", 500+i))
	}
	// …pero el cierre de la llamada seguida pasa.
	evento("Event", "Hangup", "Channel", "PJSIP/claro-1", "Uniqueid", "1800.1", "Linkedid", "1800.1")
	evento("Event", "Cdr", "UniqueID", "1800.1", "Disposition", "NO ANSWER", "BillableSeconds", "0")
	x.ami.Crudo(b.String())
	x.hasta(func() bool {
		var estado string
		_ = x.bd.QueryRow(x.ctx, `SELECT estado FROM mensajeria.llamadas WHERE origen = 'centralita' AND id_externo = '1800.1'`).Scan(&estado)
		return estado != "" && estado != "sonando"
	}, "el Hangup de la llamada seguida se descartó por el tope")
	var n int
	if err := x.bd.QueryRow(x.ctx, `SELECT count(*) FROM mensajeria.llamadas WHERE origen = 'centralita'`).Scan(&n); err != nil || n >= 21 {
		t.Fatalf("el tope no cortó la avalancha de llamadas nuevas: %d %v", n, err)
	}
}

// Hallazgo 6: ListCommands sin respuesta es un error de conexión (reconecta), nunca «conectada».
func TestCentralitaListCommandsSinRespuestaReconecta(t *testing.T) {
	x := nuevoBancoCentralita(t)
	x.conectar()
	x.ami.SinListCommands(true)
	antes, _ := x.ami.Logins()
	x.arrancarSinEsperar()
	// Entra, ListCommands no contesta en el plazo (3 s en la prueba) y vuelve a entrar.
	x.hasta(func() bool { ok, _ := x.ami.Logins(); return ok >= antes+2 }, "con ListCommands sin respuesta no reconectó")
	r := x.api(x.admin, http.MethodGet, "/centralita", nil)
	x.exigir(r, http.StatusOK)
	if c := r.cuerpo["centralita"].(map[string]any); c["estado"] == "conectada" {
		t.Fatalf("quedó conectada sin comprobar los permisos: %v", c)
	}
	x.ami.SinListCommands(false)
	x.esperarEstado("conectada")
}

// Hallazgo 6: el mínimo privilegio por las clases (Priv) de ListCommands, no solo por una lista de
// acciones conocidas.
func TestCentralitaPermisosDeMasPorClase(t *testing.T) {
	x := nuevoBancoCentralita(t)
	// Una acción de escritura que no está en ninguna lista (clase agent).
	x.ami.PermitirAccion("QueueChangePriorityCaller", "agent,all")
	r := x.api(x.admin, http.MethodPost, "/centralitas/probar", x.datos())
	x.exigir(r, http.StatusOK)
	if r.cuerpo["estado"] != "permisos_de_mas" || fmt.Sprint(r.cuerpo["accionesDeMas"]) != "[QueueChangePriorityCaller]" {
		t.Fatalf("clase agent: %v", r.cuerpo)
	}
	// Con write = reporting (CoreSettings y CoreStatus traen system): también de más.
	x.ami.PermitirEscritura()
	x.ami.PermitirAccion("CoreSettings", "system,reporting,all")
	r = x.api(x.admin, http.MethodPost, "/centralitas/probar", x.datos())
	if r.cuerpo["estado"] != "permisos_de_mas" {
		t.Fatalf("clase system: %v", r.cuerpo)
	}
	// Solo acciones sin clase (write vacío): ok.
	x.ami.PermitirEscritura()
	r = x.api(x.admin, http.MethodPost, "/centralitas/probar", x.datos())
	if r.cuerpo["estado"] != "ok" {
		t.Fatalf("sin clases de escritura: %v", r.cuerpo)
	}
}

// Hallazgo 5: el historial por ticket o por cliente no salta los permisos del ticket ni del cliente,
// y la nota solo la ve quien puede actuar sobre la llamada o supervisa.
func TestCentralitaHistorialRespetaPermisosYNota(t *testing.T) {
	x := nuevoBancoCentralita(t)
	x.conectar()
	x.asignar(x.agente, "201")
	x.arrancar()
	movil := movilUnico()
	cliente := x.clienteConMovil(movil, "Andrea López")
	cur := x.cursor(x.agente)
	x.ami.Emitir("Event", "DialBegin", "Channel", "PJSIP/claro-1", "CallerIDNum", movil, "DestChannel", "PJSIP/201-1", "Linkedid", "1900.1", "Uniqueid", "1900.1")
	l := ultimo(x.eventos(x.agente, &cur, "llamada_entrante"), "llamada_entrante")["llamada"].(map[string]any)
	id := l["id"].(string)
	x.ami.Emitir("Event", "Hangup", "Channel", "PJSIP/claro-1", "Uniqueid", "1900.1", "Linkedid", "1900.1")
	x.eventos(x.agente, &cur, "llamada_terminada")
	r := x.api(x.agente, http.MethodGet, "/centralitas/llamadas/"+id, nil)
	x.exigir(r, http.StatusOK)
	r = x.api(x.agente, http.MethodPost, "/centralitas/llamadas/"+id+"/anotar", map[string]any{"version": r.cuerpo["version"], "nota": "DPI 9876 y dirección"})
	x.exigir(r, http.StatusOK)
	var ticket int64
	if err := x.bd.QueryRow(x.ctx, `INSERT INTO soporte.tickets (prioridad, origen, asunto, sla_inicio, empresa_id, cliente_id)
		VALUES ('media', 'panel', 'Sin señal', now(), 1, $1) RETURNING id`, cliente).Scan(&ticket); err != nil {
		t.Fatal(err)
	}
	x.sql(`UPDATE mensajeria.llamadas SET ticket_id = $2 WHERE id = $1`, id, ticket)

	// Ventas tiene llamadas.ver y clientes.ver, no tickets.ver: por ticket, nada.
	ventas := x.persona([]int64{1}, rolBase("ventas"))
	ventas.empresa = 1
	r = x.api(ventas, http.MethodGet, fmt.Sprintf("/centralitas/llamadas?ticketId=%d", ticket), nil)
	if r.status == http.StatusOK || strings.Contains(fmt.Sprint(r.cuerpo), id) {
		t.Fatalf("sin tickets.ver listó las llamadas del ticket: %d %v", r.status, r.cuerpo)
	}
	// Un ticket de otra empresa: 404 (no se sabe si existe).
	var ajeno int64
	if err := x.bd.QueryRow(x.ctx, `INSERT INTO soporte.tickets (prioridad, origen, asunto, sla_inicio, empresa_id) VALUES ('media', 'panel', 'Otro', now(), 2)
		RETURNING id`).Scan(&ajeno); err != nil {
		t.Fatal(err)
	}
	x.exigir(x.api(x.otro, http.MethodGet, fmt.Sprintf("/centralitas/llamadas?ticketId=%d", ajeno), nil), http.StatusNotFound)
	// Solo llamadas.ver (sin clientes.ver): por cliente, 403.
	solo := x.persona([]int64{1}, rolPropio(x.bandeja, "llamadas.ver"))
	solo.empresa = 1
	x.exigir(x.api(solo, http.MethodGet, fmt.Sprintf("/centralitas/llamadas?clienteId=%d", cliente), nil), http.StatusForbidden)

	nota := func(p persona, ruta string) any {
		t.Helper()
		r := x.api(p, http.MethodGet, ruta, nil)
		x.exigir(r, http.StatusOK)
		if d, ok := r.cuerpo["data"].([]any); ok {
			if len(d) != 1 {
				t.Fatalf("%s: %v", ruta, r.cuerpo)
			}
			return d[0].(map[string]any)["nota"]
		}
		return r.cuerpo["nota"]
	}
	porCliente, porTicket, una := fmt.Sprintf("/centralitas/llamadas?clienteId=%d", cliente), fmt.Sprintf("/centralitas/llamadas?ticketId=%d", ticket),
		"/centralitas/llamadas/"+id
	for _, ruta := range []string{porCliente, porTicket, una} {
		// A quien le sonó y a quien supervisa las extensiones, sí.
		if n := nota(x.agente, ruta); n != "DPI 9876 y dirección" {
			t.Fatalf("receptor %s: %v", ruta, n)
		}
		if n := nota(x.admin, ruta); n != "DPI 9876 y dirección" {
			t.Fatalf("supervisa %s: %v", ruta, n)
		}
		// A otro de soporte que ve el cliente y el ticket, no.
		if n := nota(x.otro, ruta); n != nil {
			t.Fatalf("la nota llegó a quien no puede actuar sobre la llamada (%s): %v", ruta, n)
		}
	}
}

// Segunda revisión (A): el tope de llamadas nuevas no descarta lo de las llamadas ya seguidas (la
// contestación).
func TestCentralitaTopeNoDescartaLaContestacion(t *testing.T) {
	x := nuevoBancoCentralita(t, func(c *mensajeria.Config) { c.CentralitaEventosSeg = 2 })
	x.conectar()
	x.asignar(x.agente, "201")
	x.arrancar()
	movil := movilUnico()
	x.clienteConMovil(movil, "Marta Díaz")
	var b strings.Builder
	evento := func(campos ...string) {
		for i := 0; i+1 < len(campos); i += 2 {
			fmt.Fprintf(&b, "%s: %s\r\n", campos[i], campos[i+1])
		}
		b.WriteString("\r\n")
	}
	evento("Event", "DialBegin", "Channel", "PJSIP/claro-1", "CallerIDNum", movil, "DestChannel", "PJSIP/201-1", "Linkedid", "2000.1", "Uniqueid", "2000.1")
	evento("Event", "DialBegin", "Channel", "PJSIP/claro-2", "CallerIDNum", "55998877", "DestChannel", "PJSIP/201-2", "Linkedid", "2000.2", "Uniqueid", "2000.2")
	evento("Event", "DialEnd", "Channel", "PJSIP/claro-1", "DestChannel", "PJSIP/201-1", "DialStatus", "ANSWER", "Linkedid", "2000.1")
	x.ami.Crudo(b.String())
	x.hasta(func() bool {
		var estado string
		_ = x.bd.QueryRow(x.ctx, `SELECT estado FROM mensajeria.llamadas WHERE origen = 'centralita' AND id_externo = '2000.1'`).Scan(&estado)
		return estado == "en_curso"
	}, "la contestación de la llamada seguida se descartó por el tope")
}

// Segunda revisión (B): «Probar» contra una centralita muda no cuelga la petición.
func TestCentralitaProbarConPlazo(t *testing.T) {
	x := nuevoBancoCentralita(t)
	for _, caso := range []struct {
		nombre string
		poner  func(bool)
	}{{"Ping", x.ami.SinPing}, {"ListCommands", x.ami.SinListCommands}} {
		caso.poner(true)
		hecho := make(chan respuesta, 1)
		go func() { hecho <- x.api(x.admin, http.MethodPost, "/centralitas/probar", x.datos()) }()
		select {
		case r := <-hecho:
			x.exigir(r, http.StatusOK)
			if r.cuerpo["estado"] != "no_conecta" {
				t.Fatalf("sin %s: %v", caso.nombre, r.cuerpo)
			}
		case <-time.After(8 * time.Second):
			t.Fatalf("sin respuesta a %s, «Probar» se quedó colgado", caso.nombre)
		}
		caso.poner(false)
	}
}

// Segunda revisión (C): con la fila de la centralita bloqueada (rotación de claves), el motor no se
// traba: sigue haciendo Ping y no se desconecta.
func TestCentralitaFilaBloqueadaNoTrabaElMotor(t *testing.T) {
	x := nuevoBancoCentralita(t)
	x.conectar()
	x.arrancar()
	logins, _ := x.ami.Logins()
	pings := func() int {
		n := 0
		for _, a := range x.ami.Recibido() {
			if strings.EqualFold(a, "ping") {
				n++
			}
		}
		return n
	}
	tx, err := x.bd.(*pgxpool.Pool).Begin(x.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err := tx.Exec(x.ctx, `SELECT id FROM mensajeria.centralitas WHERE id = $1 FOR UPDATE`, x.id); err != nil {
		t.Fatal(err)
	}
	antes := pings()
	time.Sleep(2 * time.Second) // ~6 latidos de 300 ms, menos que el arriendo (3 s)
	durante := pings() - antes
	if err := tx.Commit(x.ctx); err != nil {
		t.Fatal(err)
	}
	if durante < 3 {
		t.Fatalf("con la fila bloqueada el motor se trabó: %d Ping en 2 s", durante)
	}
	if ok, _ := x.ami.Logins(); ok != logins || x.ami.Conectados() != 1 {
		t.Fatalf("se desconectó por la fila bloqueada: logins %d→%d, conectados %d", logins, ok, x.ami.Conectados())
	}
}

// Segunda revisión (E): ListCommands con «Response: Error» no es «sin permisos de más».
func TestCentralitaListCommandsConError(t *testing.T) {
	x := nuevoBancoCentralita(t)
	x.ami.ErrorListCommands(true)
	r := x.api(x.admin, http.MethodPost, "/centralitas/probar", x.datos())
	x.exigir(r, http.StatusOK)
	if r.cuerpo["estado"] != "sin_comprobar" {
		t.Fatalf("probar: %v", r.cuerpo)
	}
	x.ami.ErrorListCommands(false)
	x.conectar()
	x.ami.ErrorListCommands(true)
	antes, _ := x.ami.Logins()
	x.arrancarSinEsperar()
	x.hasta(func() bool { ok, _ := x.ami.Logins(); return ok >= antes+2 }, "con ListCommands en error no reconectó")
	r = x.api(x.admin, http.MethodGet, "/centralita", nil)
	x.exigir(r, http.StatusOK)
	if c := r.cuerpo["centralita"].(map[string]any); c["estado"] == "conectada" {
		t.Fatalf("quedó conectada sin comprobar los permisos: %v", c)
	}
	x.ami.ErrorListCommands(false)
	x.esperarEstado("conectada")
}

// proxyBD es un TCP entre una réplica y Postgres que se puede «congelar»: deja de pasar bytes sin
// cerrar nada (la base no contesta, sin bloquear ninguna fila).
type proxyBD struct {
	ln        net.Listener
	destino   string
	mu        sync.Mutex
	cond      *sync.Cond
	congelado bool
	conns     []net.Conn
}

func nuevoProxyBD(t *testing.T, destino string) *proxyBD {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &proxyBD{ln: ln, destino: destino}
	p.cond = sync.NewCond(&p.mu)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			d, err := net.Dial("tcp", destino)
			if err != nil {
				_ = c.Close()
				continue
			}
			p.mu.Lock()
			p.conns = append(p.conns, c, d)
			p.mu.Unlock()
			go p.copiar(d, c)
			go p.copiar(c, d)
		}
	}()
	t.Cleanup(func() {
		p.Congelar(false)
		_ = ln.Close()
		p.mu.Lock()
		for _, c := range p.conns {
			_ = c.Close()
		}
		p.mu.Unlock()
	})
	return p
}

func (p *proxyBD) copiar(dst, src net.Conn) {
	buf := make([]byte, 32<<10)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			p.mu.Lock()
			for p.congelado {
				p.cond.Wait()
			}
			p.mu.Unlock()
			if _, e := dst.Write(buf[:n]); e != nil {
				return
			}
		}
		if err != nil {
			_ = dst.Close()
			return
		}
	}
}

func (p *proxyBD) Congelar(v bool) {
	p.mu.Lock()
	p.congelado = v
	p.mu.Unlock()
	p.cond.Broadcast()
}

// poolPorProxy: un pool a la base de pruebas que pasa por un proxyBD.
func poolPorProxy(t *testing.T) (*pgxpool.Pool, *proxyBD) {
	t.Helper()
	u, err := pruebas.URL()
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(u)
	if err != nil {
		t.Fatal(err)
	}
	host := cfg.ConnConfig.Host
	if host == "" || strings.HasPrefix(host, "/") || host == "localhost" {
		host = "127.0.0.1"
	}
	px := nuevoProxyBD(t, net.JoinHostPort(host, strconv.Itoa(int(cfg.ConnConfig.Port))))
	cfg.ConnConfig.Host, cfg.ConnConfig.Port, cfg.ConnConfig.Fallbacks = "127.0.0.1", uint16(px.ln.Addr().(*net.TCPAddr).Port), nil
	cfg.ConnConfig.ConnectTimeout = 2 * time.Second
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool, px
}

// Tercera revisión (1): con la base de la réplica conectada sin responder (sin ninguna fila
// bloqueada) y su bucle colgado procesando un evento, el vigía cierra la sesión AMI antes de que
// venza el arriendo; la otra réplica conecta después y nunca coinciden.
func TestCentralitaBaseColgadaNoDejaDosConexiones(t *testing.T) {
	x := nuevoBancoCentralita(t)
	x.conectar()
	x.asignar(x.agente, "201")
	poolA, px := poolPorProxy(t)
	ctxA, cancelarA := context.WithCancel(context.Background())
	hechoA := make(chan struct{})
	go func() {
		defer close(hechoA)
		x.m.CorrerCentralitas(ctxA, poolA, nil)
	}()
	t.Cleanup(func() {
		px.Congelar(false)
		cancelarA()
		<-hechoA
	})
	x.hasta(func() bool { return x.ami.Conectados() == 1 }, "la réplica A no conectó")
	x.esperarEstado("conectada")
	x.replica()
	time.Sleep(500 * time.Millisecond)
	var resta float64
	if err := x.bd.QueryRow(x.ctx, `SELECT extract(epoch FROM motor_hasta - now()) FROM mensajeria.centralitas WHERE id = $1`, x.id).Scan(&resta); err != nil {
		t.Fatal(err)
	}
	px.Congelar(true)
	vence := time.Now().Add(time.Duration(resta * float64(time.Second)))
	// Un evento que A tiene que llevar a la base: su bucle se queda esperando.
	x.ami.Emitir("Event", "DialBegin", "Channel", "PJSIP/claro-1", "CallerIDNum", "55998877", "DestChannel", "PJSIP/201-1", "Linkedid", "2100.1", "Uniqueid", "2100.1")
	maximo := 0
	var cerro time.Time
	antes, _ := x.ami.Logins()
	fin := time.Now().Add(8 * time.Second)
	for time.Now().Before(fin) {
		n := x.ami.Conectados()
		maximo = max(maximo, n)
		if n == 0 && cerro.IsZero() {
			cerro = time.Now()
		}
		time.Sleep(5 * time.Millisecond)
	}
	logins, _ := x.ami.Logins()
	if maximo > 1 {
		t.Fatalf("las dos réplicas quedaron conectadas a la vez (%d)", maximo)
	}
	if cerro.IsZero() || !cerro.Before(vence) {
		t.Fatalf("A no soltó la sesión AMI antes de que venciera el arriendo (cerró %v, vencía %v)", cerro, vence)
	}
	if logins <= antes || x.ami.Conectados() != 1 {
		t.Fatalf("la réplica B no tomó la conexión: logins %d→%d, conectados %d", antes, logins, x.ami.Conectados())
	}
}

// Tercera revisión (2): si el estado «conectada» no se pudo guardar (fila bloqueada más que el plazo),
// no queda «desconectada» con el motor conectado.
func TestCentralitaEstadoPendienteSeReintenta(t *testing.T) {
	x := nuevoBancoCentralita(t)
	x.conectar()
	x.arrancar()
	// Una caída larga deja el estado «desconectada».
	x.ami.Pausar(true)
	x.ami.Cortar()
	x.esperarEstado("desconectada")
	// Al volver, la fila queda bloqueada justo mientras se comprueban los permisos.
	x.ami.DemorarListCommands(500 * time.Millisecond)
	antes, _ := x.ami.Logins()
	x.ami.Pausar(false)
	x.hasta(func() bool { ok, _ := x.ami.Logins(); return ok > antes }, "no volvió a entrar")
	tx, err := x.bd.(*pgxpool.Pool).Begin(x.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err := tx.Exec(x.ctx, `SELECT id FROM mensajeria.centralitas WHERE id = $1 FOR UPDATE`, x.id); err != nil {
		t.Fatal(err)
	}
	time.Sleep(3500 * time.Millisecond) // más que el plazo de la conexión (3 s)
	if err := tx.Commit(x.ctx); err != nil {
		t.Fatal(err)
	}
	x.ami.DemorarListCommands(0)
	x.esperarEstado("conectada")
	if x.ami.Conectados() != 1 {
		t.Fatalf("conectados: %d", x.ami.Conectados())
	}
}

// Tercera revisión (3): una prueba sin_comprobar también borra la prueba buena anterior.
func TestCentralitaSinComprobarBorraLaPrueba(t *testing.T) {
	x := nuevoBancoCentralita(t)
	x.conectar()
	x.exigir(x.api(x.admin, http.MethodPost, x.ruta("/desactivar"), map[string]any{"version": x.version}), http.StatusOK)
	r := x.api(x.admin, http.MethodPost, x.ruta("/probar"), map[string]any{})
	x.exigir(r, http.StatusOK)
	if r.cuerpo["estado"] != "ok" || r.cuerpo["centralita"].(map[string]any)["probadaEn"] == nil {
		t.Fatalf("probar: %v", r.cuerpo)
	}
	x.ami.ErrorListCommands(true)
	r = x.api(x.admin, http.MethodPost, x.ruta("/probar"), map[string]any{})
	x.exigir(r, http.StatusOK)
	c := r.cuerpo["centralita"].(map[string]any)
	if r.cuerpo["estado"] != "sin_comprobar" || c["probadaEn"] != nil {
		t.Fatalf("sin_comprobar dejó la prueba anterior: %v", r.cuerpo)
	}
	x.exigirProblema(x.api(x.admin, http.MethodPost, x.ruta("/activar"), map[string]any{"version": c["version"]}), http.StatusUnprocessableEntity, "sin_probar")
}
