package argos_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/riverqueue/river"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
	"github.com/HCDestroyer/iris/apps/api/migraciones"
)

// Contrato Iris ↔ ArgosISP 0.6.3 §1.3: revocar el atk_ de un canje que Iris no completa y reconectar
// sin corte.

const kindRevocarCanje = "integracion.revocar_canje_remoto"

// canjeRechazado hace un canje que Argos responde con 200 y el atk_ token pero sin receptor: Iris
// lo rechaza con 422 webhook_invalido. Devuelve el id de conector reservado.
func (e *entorno) canjeRechazado(admin *clienteHTTP, token string) int64 {
	e.t.Helper()
	antes := e.contar(`SELECT count(*) FROM river.river_job WHERE kind = $1`, kindRevocarCanje)
	e.argos.con(func() {
		e.argos.codigo = "codigo-063-" + pruebas.Unico() + "-0123456789"
		e.argos.tokenCanje, e.argos.sinWebhook = token, true
	})
	defer e.argos.con(func() { e.argos.sinWebhook = false })
	admin.pedir(http.MethodPost, "/api/v1/integraciones/argosisp/conectar", map[string]any{"enlace": e.argos.enlace()}).problema(e.t, 422, "webhook_invalido")
	if n := e.contar(`SELECT count(*) FROM river.river_job WHERE kind = $1`, kindRevocarCanje); n != antes+1 {
		e.t.Fatalf("el canje rechazado debía encolar un job de revocación: %d → %d", antes, n)
	}
	var id int64
	if err := e.pool.QueryRow(context.Background(), `SELECT (args->>'conector_id')::bigint FROM river.river_job WHERE kind = $1
		ORDER BY id DESC LIMIT 1`, kindRevocarCanje).Scan(&id); err != nil {
		e.t.Fatal(err)
	}
	return id
}

// argsRevocar lee los argumentos del job de revocación del conector reservado.
func (e *entorno) argsRevocar(conectorID int64) (conector.ArgsRevocarCanje, string) {
	e.t.Helper()
	var crudo []byte
	var estado string
	if err := e.pool.QueryRow(context.Background(), `SELECT args, state FROM river.river_job WHERE kind = $1
		AND (args->>'conector_id')::bigint = $2`, kindRevocarCanje, conectorID).Scan(&crudo, &estado); err != nil {
		e.t.Fatal(err)
	}
	var a conector.ArgsRevocarCanje
	if err := json.Unmarshal(crudo, &a); err != nil {
		e.t.Fatal(err)
	}
	return a, estado
}

// sinTokenEnJobs falla si el token aparece en claro (o en base64) en los jobs de River.
func (e *entorno) sinTokenEnJobs(token string) {
	e.t.Helper()
	todo := e.consultarTexto(`SELECT coalesce(string_agg(args::text, ' '), '') FROM river.river_job`)
	for _, v := range []string{token, base64.StdEncoding.EncodeToString([]byte(token)), base64.RawStdEncoding.EncodeToString([]byte(token))} {
		if strings.Contains(todo, v) {
			e.t.Fatalf("el token del canje quedó en claro en river.river_job")
		}
	}
}

// bufferSeguro es un io.Writer para capturar el log de los workers.
type bufferSeguro struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *bufferSeguro) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *bufferSeguro) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

// §1.3 «Canje rechazado o incompleto»: Argos responde 200 al canje pero Iris lo rechaza (422
// webhook_invalido): Iris encola la revocación y el worker de River llama a POST
// /integraciones/iris/conexion/revocar con ese atk_ como Bearer y sin cuerpo. 503 se reintenta
// (una vez, y el 204 lo termina); 401 termina sin reintento; 403 queda cancelado sin reintento. El
// token nunca queda en claro en el job, la auditoría ni el log.
func TestCanjeRechazadoRevocaElAtkEnArgos(t *testing.T) {
	e := nuevoEntorno(t)
	admin := e.usuario("admin")
	u := pruebas.Unico()
	tok204, tok503, tok401, tok403 := "atk_063_ok_"+u, "atk_063_caido_"+u, "atk_063_revocado_"+u, "atk_063_otro_"+u
	e.argos.con(func() {
		// Cada canje reemplaza al pendiente anterior (en Argos, su atk_ ya no autentica): las
		// respuestas van fijas por token.
		e.argos.respuestasRevocar[tok204] = []int{204}
		e.argos.respuestasRevocar[tok503] = []int{503, 204}
		e.argos.respuestasRevocar[tok401] = []int{401}
		e.argos.respuestasRevocar[tok403] = []int{403}
	})
	ids := map[string]int64{}
	for _, tok := range []string{tok204, tok503, tok401, tok403} {
		ids[tok] = e.canjeRechazado(admin, tok)
		e.sinTokenEnJobs(tok)
		e.sinSecretos(tok)
		a, estado := e.argsRevocar(ids[tok])
		if a.Sistema != argos.Sistema || a.BaseURL != e.argos.srv.URL || len(a.TokenCifrado) == 0 || estado != "available" {
			t.Fatalf("job de revocación: %+v %s", a, estado)
		}
	}
	// Único por conector: encolar otra vez el mismo no agrega otro job.
	cola, err := integracion.NuevoClienteInsercion(e.pool, migraciones.EsquemaRiver, nil)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := e.argsRevocar(ids[tok204])
	if r, err := cola.Insert(context.Background(), a, nil); err != nil || !r.UniqueSkippedAsDuplicate {
		t.Fatalf("el job debía ser único por conector: %+v %v", r, err)
	}

	// El worker de River (como cmd/worker), con el log capturado.
	log := &bufferSeguro{}
	anterior := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(log, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(anterior)
	relay := &integracion.Relay{BD: e.pool, Bus: integracion.NuevoBus()}
	cliente, err := integracion.NuevoClienteRiver(e.pool, relay, integracion.OpcionesWorker{
		Esquema: migraciones.EsquemaRiver, RelayIntervalo: time.Hour, Log: slog.Default(),
		Registrar: func(w *river.Workers) { e.mod.RegistrarWorkers(w, e.pool) },
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancelar := context.WithCancel(context.Background())
	defer cancelar()
	if err := cliente.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() {
		parar, c := context.WithTimeout(context.Background(), 10*time.Second)
		defer c()
		_ = cliente.Stop(parar)
	}()
	esperado := map[string]string{tok204: "completed", tok503: "completed", tok401: "completed", tok403: "cancelled"}
	limite := time.Now().Add(45 * time.Second)
	for time.Now().Before(limite) {
		listos := 0
		for tok, estado := range esperado {
			if _, s := e.argsRevocar(ids[tok]); s == estado {
				listos++
			}
		}
		if listos == len(esperado) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	for tok, estado := range esperado {
		if _, s := e.argsRevocar(ids[tok]); s != estado {
			t.Fatalf("%s: estado %s, esperaba %s", tok[:16], s, estado)
		}
	}
	intentos := func(tok string) int {
		return e.contar(`SELECT attempt FROM river.river_job WHERE kind = $1 AND (args->>'conector_id')::bigint = $2`, kindRevocarCanje, ids[tok])
	}
	casos := []struct {
		token    string
		statuses []int
	}{
		{tok204, []int{204}},
		{tok503, []int{503, 204}}, // un reintento y listo
		{tok401, []int{401}},      // ya no autenticaba: listo, sin reintento
		{tok403, []int{403}},      // otra credencial: cancelado, sin reintento
	}
	for _, c := range casos {
		rs := e.argos.revocacionesDe(c.token)
		if len(rs) != len(c.statuses) || intentos(c.token) != len(c.statuses) {
			t.Fatalf("%s: %d revocaciones y %d intentos, esperaba %d", c.token[:16], len(rs), intentos(c.token), len(c.statuses))
		}
		for i, r := range rs {
			if r.status != c.statuses[i] || r.cuerpo != 0 {
				t.Fatalf("%s: revocación %d: %+v", c.token[:16], i, r)
			}
		}
	}
	if s := log.String(); !strings.Contains(s, "token del canje no completado revocado") {
		t.Fatalf("el worker no dejó rastro en el log: %s", s)
	}
	for _, tok := range []string{tok204, tok503, tok401, tok403} {
		if strings.Contains(log.String(), tok) {
			t.Fatal("el token del canje quedó en claro en el log")
		}
		e.sinSecretos(tok)
	}
}

// RevocarCanje (lo que corre el worker) traduce las respuestas de Argos: 204 y 401 terminan, 5xx y
// sin respuesta se reintentan, 403 y otros 4xx se cancelan.
func TestRevocarCanjeRespuestas(t *testing.T) {
	e := nuevoEntorno(t)
	admin := e.usuario("admin")
	tok := "atk_063_respuestas_" + pruebas.Unico()
	id := e.canjeRechazado(admin, tok)
	a, _ := e.argsRevocar(id)
	ctx := context.Background()
	e.argos.con(func() { e.argos.respuestasRevocar[tok] = []int{503, 500, 429, 403, 401, 204} })
	esCancelado := func(err error) bool {
		var c *river.JobCancelError
		return err != nil && errors.As(err, &c)
	}
	n := e.mod.Nucleo()
	if err := n.RevocarCanje(ctx, a); err == nil || esCancelado(err) {
		t.Fatalf("503: debía reintentarse: %v", err)
	}
	if err := n.RevocarCanje(ctx, a); err == nil || esCancelado(err) {
		t.Fatalf("500: debía reintentarse: %v", err)
	}
	if err := n.RevocarCanje(ctx, a); !esCancelado(err) {
		t.Fatalf("429 (otro 4xx): debía cancelarse: %v", err)
	}
	if err := n.RevocarCanje(ctx, a); !esCancelado(err) {
		t.Fatalf("403: debía cancelarse: %v", err)
	}
	if err := n.RevocarCanje(ctx, a); err != nil {
		t.Fatalf("401: debía terminar: %v", err)
	}
	if err := n.RevocarCanje(ctx, a); err != nil {
		t.Fatalf("204: debía terminar: %v", err)
	}
	// Sin respuesta (Argos caído): se reintenta.
	caido := a
	caido.BaseURL = "http://127.0.0.1:1"
	if err := n.RevocarCanje(ctx, caido); err == nil || esCancelado(err) {
		t.Fatalf("sin respuesta: debía reintentarse: %v", err)
	}
	// Token cifrado atado a su conector: con otro id no se descifra (y no se reintenta).
	otro := a
	otro.ConectorID++
	if err := n.RevocarCanje(ctx, otro); !esCancelado(err) {
		t.Fatalf("token de otro conector: debía cancelarse: %v", err)
	}
	if got := len(e.argos.revocacionesDe(tok)); got != 6 {
		t.Fatalf("revocaciones: %d", got)
	}
	if (conector.ArgsRevocarCanje{}).InsertOpts().MaxAttempts != conector.IntentosRevocarCanje || conector.IntentosRevocarCanje > 15 {
		t.Fatal("los reintentos deben ser acotados")
	}
}

// Un canje que Argos no respondió con 200 (código inválido) no encola revocación: Iris no tiene token.
func TestCanjeFallidoSinTokenNoRevoca(t *testing.T) {
	e := nuevoEntorno(t)
	admin := e.usuario("admin")
	antes := e.contar(`SELECT count(*) FROM river.river_job WHERE kind = $1`, kindRevocarCanje)
	admin.pedir(http.MethodPost, "/api/v1/integraciones/argosisp/conectar", map[string]any{"enlace": e.argos.srv.URL + "/conectar#codigo-que-no-existe-0123456789"}).
		problema(t, 502, "argos_rechazo")
	if n := e.contar(`SELECT count(*) FROM river.river_job WHERE kind = $1`, kindRevocarCanje); n != antes {
		t.Fatalf("sin 200 no hay atk_ que revocar: %d → %d", antes, n)
	}
}

// §1.3 «Reconexión» (0.6.3): Argos deja vivo el atk_ anterior hasta la primera llamada autenticada
// con el nuevo. Iris no usa el atk_ nuevo hasta que el conector nuevo está activo de su lado: entre
// el canje y la activación sigue con el anterior, la activación no llama a Argos y la primera
// llamada con el nuevo es el PUT del contrato (job que encola la activación).
func TestReconexionSinCorte(t *testing.T) {
	e := nuevoEntorno(t)
	e.argos.con(func() { e.argos.version = "1.6.0" })
	admin, viejo, _ := e.conectado()
	ctx := pruebas.ContextoSistema(e.pool)
	if _, err := e.mod.InformarContrato.Ejecutar(ctx, viejo); err != nil {
		t.Fatal(err)
	}
	nuevoTok := "atk_063_reconexion_" + pruebas.Unico()
	e.argos.con(func() {
		e.argos.codigo = "codigo-reconexion-" + pruebas.Unico() + "-0123456789"
		e.argos.tokenCanje = nuevoTok
	})
	r := admin.pedir(http.MethodPost, "/api/v1/integraciones/argosisp/conectar", map[string]any{"enlace": e.argos.enlace()})
	if r.status != 201 || r.cuerpo["estado"] != "apagado" {
		t.Fatalf("reconectar: %d %s", r.status, r.crudo)
	}
	nuevo := int64(r.cuerpo["id"].(float64))
	usosNuevo := func() []string {
		var rutas []string
		e.argos.con(func() {
			for _, u := range e.argos.usos {
				if u.token == nuevoTok {
					rutas = append(rutas, u.ruta)
				}
			}
		})
		return rutas
	}
	// Entre el canje y la activación: el conector anterior sigue activo y es el que se usa.
	if e.contar(`SELECT count(*) FROM integracion.conectores WHERE id = $1 AND estado = 'activo'`, viejo) != 1 {
		t.Fatal("el conector anterior debía seguir activo hasta activar el nuevo")
	}
	if _, err := e.mod.InformarContrato.Ejecutar(ctx, viejo); err != nil {
		t.Fatalf("el conector anterior debía seguir funcionando: %v", err)
	}
	if _, err := e.mod.InformarContrato.Ejecutar(ctx, nuevo); !esProblema(err, "conector_apagado") {
		t.Fatalf("el conector nuevo apagado no llama a Argos: %v", err)
	}
	if u := usosNuevo(); len(u) != 0 {
		t.Fatalf("el atk_ nuevo no se usa antes de activar: %v", u)
	}
	e.argos.con(func() {
		if e.argos.pendiente != nuevoTok || e.argos.token != tokenArgos {
			t.Fatal("en Argos la reconexión debía seguir pendiente")
		}
	})

	// La activación reemplaza al anterior en la misma transacción, encola el PUT del contrato y no
	// llama a Argos.
	if a := admin.pedir(http.MethodPost, fmt.Sprintf("/api/v1/integraciones/conectores/%d/activar", nuevo), map[string]any{"version": 1}); a.status != 200 {
		t.Fatalf("activar: %d %s", a.status, a.crudo)
	}
	if e.contar(`SELECT count(*) FROM integracion.conectores WHERE id = $1 AND estado = 'apagado'`, viejo) != 1 ||
		e.contar(`SELECT count(*) FROM integracion.conectores WHERE id = $1 AND estado = 'activo'`, nuevo) != 1 {
		t.Fatal("la activación debía dejar activo solo el conector nuevo")
	}
	if n := e.contar(`SELECT count(*) FROM river.river_job WHERE kind = 'integracion.argos_informar_contrato' AND (args->>'conector_id')::bigint = $1`, nuevo); n != 1 {
		t.Fatalf("la activación debía encolar el PUT del contrato: %d", n)
	}
	if u := usosNuevo(); len(u) != 0 {
		t.Fatalf("activar no llama a Argos con el atk_ nuevo: %v", u)
	}

	// El job: la primera llamada con el atk_ nuevo es el PUT del contrato, con el conector nuevo ya
	// activo; Argos aplica la reconexión y el atk_ anterior deja de autenticar.
	if _, err := e.mod.InformarContrato.Ejecutar(ctx, nuevo); err != nil {
		t.Fatal(err)
	}
	if u := usosNuevo(); len(u) == 0 || u[0] != "PUT /api/v1/integraciones/iris/contrato" {
		t.Fatalf("la primera llamada con el atk_ nuevo debía ser el PUT del contrato: %v", u)
	}
	e.argos.con(func() {
		if e.argos.token != nuevoTok || e.argos.pendiente != "" {
			t.Fatal("Argos debía aplicar la reconexión en la primera llamada del atk_ nuevo")
		}
	})
	if e.contar(`SELECT count(*) FROM river.river_job WHERE kind = $1 AND (args->>'conector_id')::bigint IN ($2, $3)`, kindRevocarCanje, viejo, nuevo) != 0 {
		t.Fatal("una reconexión completa no revoca ningún atk_")
	}
}
