package mensajeria_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HCDestroyer/iris/apps/api/internal/mensajeria"
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
