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
// (una vez, y el 204 lo termina), igual que 429 (con su Retry-After); 401 termina sin reintento; 403
// queda cancelado sin reintento. El
// token nunca queda en claro en el job, la auditoría ni el log.
func TestCanjeRechazadoRevocaElAtkEnArgos(t *testing.T) {
	e := nuevoEntorno(t)
	admin := e.usuario("admin")
	u := pruebas.Unico()
	tok204, tok503, tok401, tok403 := "atk_063_ok_"+u, "atk_063_caido_"+u, "atk_063_revocado_"+u, "atk_063_otro_"+u
	// 429: con Retry-After corto (1 s) y luego 204; sin Retry-After (espera de River) y luego 204; con
	// Retry-After largo (120 s): queda programado para entonces.
	tok429, tok429sin, tok429largo := "atk_063_429_"+u, "atk_063_429sin_"+u, "atk_063_429largo_"+u
	todos := []string{tok204, tok503, tok401, tok403, tok429, tok429sin, tok429largo}
	e.argos.con(func() {
		// Cada canje reemplaza al pendiente anterior (en Argos, su atk_ ya no autentica): las
		// respuestas van fijas por token.
		e.argos.respuestasRevocar[tok204] = []int{204}
		e.argos.respuestasRevocar[tok503] = []int{503, 204}
		e.argos.respuestasRevocar[tok401] = []int{401}
		e.argos.respuestasRevocar[tok403] = []int{403}
		e.argos.respuestasRevocar[tok429], e.argos.retryAfter[tok429] = []int{429, 204}, "1"
		e.argos.respuestasRevocar[tok429sin] = []int{429, 204}
		e.argos.respuestasRevocar[tok429largo], e.argos.retryAfter[tok429largo] = []int{429}, "120"
	})
	ids := map[string]int64{}
	for _, tok := range todos {
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
	esperado := map[string]string{tok204: "completed", tok503: "completed", tok401: "completed", tok403: "cancelled",
		tok429: "completed", tok429sin: "completed", tok429largo: "retryable"}
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
		{tok429, []int{429, 204}}, // Retry-After 1 s, un reintento
		{tok429sin, []int{429, 204}},
		{tok429largo, []int{429}}, // esperando sus 120 s
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
	// El 429 con Retry-After 120 queda programado para entonces (no con la espera de River, ~1 s).
	if n := e.contar(`SELECT count(*) FROM river.river_job WHERE kind = $1 AND (args->>'conector_id')::bigint = $2
		AND scheduled_at > now() + interval '100 seconds'`, kindRevocarCanje, ids[tok429largo]); n != 1 {
		t.Fatal("el 429 debía respetar Retry-After")
	}
	if s := log.String(); !strings.Contains(s, "token del canje no completado revocado") {
		t.Fatalf("el worker no dejó rastro en el log: %s", s)
	}
	for _, tok := range todos {
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
	e.argos.con(func() { e.argos.respuestasRevocar[tok] = []int{503, 500, 429, 429, 400, 403, 401, 204} })
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
	// 429: se reintenta; con Retry-After, ese plazo (EsperaDe429, lo usa NextRetry del worker).
	e.argos.con(func() { e.argos.retryAfter[tok] = "30" })
	err429 := n.RevocarCanje(ctx, a)
	if err429 == nil || esCancelado(err429) || conector.EsperaDe429(err429) != 30*time.Second {
		t.Fatalf("429 con Retry-After: debía reintentarse a los 30 s: %v (%v)", err429, conector.EsperaDe429(err429))
	}
	e.argos.con(func() { e.argos.retryAfter[tok] = "" })
	if err := n.RevocarCanje(ctx, a); err == nil || esCancelado(err) || conector.EsperaDe429(err) != 0 {
		t.Fatalf("429 sin Retry-After: debía reintentarse con la espera de River: %v", err)
	}
	if err := n.RevocarCanje(ctx, a); !esCancelado(err) {
		t.Fatalf("400 (otro 4xx): debía cancelarse: %v", err)
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
	// Sin respuesta (Argos caído): se reintenta. El token va cifrado para esa base.
	caido := a
	caido.BaseURL = "http://127.0.0.1:1"
	cifrado, err := e.cfg.Cifrador.Cifrar([]byte(tok), conector.DatosTokenCanje(caido.ConectorID, caido.BaseURL))
	if err != nil {
		t.Fatal(err)
	}
	caido.TokenCifrado = cifrado
	if err := n.RevocarCanje(ctx, caido); err == nil || esCancelado(err) {
		t.Fatalf("sin respuesta: debía reintentarse: %v", err)
	}
	// Token cifrado atado a su base: si alguien cambia base_url en river_job, no descifra, el job se
	// cancela y no sale ninguna petición (ni a la base nueva ni a la de antes).
	movido := a
	movido.BaseURL = strings.Replace(a.BaseURL, "127.0.0.1", "localhost", 1)
	antesMovido := len(e.argos.revocacionesDe(tok))
	if err := n.RevocarCanje(ctx, movido); !esCancelado(err) {
		t.Fatalf("base cambiada: debía cancelarse: %v", err)
	}
	if len(e.argos.revocacionesDe(tok)) != antesMovido {
		t.Fatal("con la base cambiada no debía llamarse a revocar")
	}
	// Token cifrado atado a su conector: con otro id no se descifra (y no se reintenta).
	otro := a
	otro.ConectorID++
	if err := n.RevocarCanje(ctx, otro); !esCancelado(err) {
		t.Fatalf("token de otro conector: debía cancelarse: %v", err)
	}
	if got := len(e.argos.revocacionesDe(tok)); got != 8 {
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
	admin, viejo, secretoViejo := e.conectado()
	patViejo := e.patDelUltimoCanje()
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

	// Ventana entre la activación y la confirmación: el conector viejo ya no llama a Argos, pero
	// Argos sigue leyendo de Iris con su iris_pat_ y sus webhooks firmados con el secreto viejo se
	// aceptan (se guardan en el conector nuevo).
	if r := e.leerConPat(patViejo); r.status != 200 {
		t.Fatalf("Argos debía seguir leyendo de Iris con el iris_pat_ anterior: %d %s", r.status, r.crudo)
	}
	if _, err := e.mod.InformarContrato.Ejecutar(ctx, viejo); !esProblema(err, "conector_apagado") {
		t.Fatalf("el conector viejo no llama a Argos: %v", err)
	}
	wh := e.enviar(viejo, secretoViejo, "wh-063-"+pruebas.Unico(), "cliente.actualizado", map[string]any{"empresa_id": 1, "cliente_id": 71}, time.Now())
	if wh.status >= 300 {
		t.Fatalf("el webhook firmado con el secreto viejo debía aceptarse: %d %s", wh.status, wh.crudo)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.entrantes WHERE id = $1 AND conector_id = $2`, e.entranteDe(wh), nuevo); n != 1 {
		t.Fatal("el webhook del conector reemplazado se guarda en el activo")
	}
	if e.contar(`SELECT count(*) FROM integracion.conectores WHERE id = $1 AND reemplaza_conector_id = $2 AND reemplaza_token_id IS NOT NULL`, nuevo, viejo) != 1 {
		t.Fatal("el conector nuevo debía guardar el reemplazo pendiente")
	}
	if n := e.contar(`SELECT count(*) FROM river.river_job WHERE kind = 'integracion.vencer_reemplazo_conector'
		AND (args->>'conector_viejo_id')::bigint = $1 AND (args->>'conector_nuevo_id')::bigint = $2
		AND scheduled_at BETWEEN now() + interval '23 hours 59 minutes' AND now() + interval '24 hours 1 minute'`, viejo, nuevo); n != 1 {
		t.Fatalf("la activación debía encolar el vencimiento a las 24 h: %d", n)
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
	// Confirmada: el iris_pat_ anterior se revoca y los webhooks del conector viejo vuelven a 503.
	if r := e.leerConPat(patViejo); r.status != 401 {
		t.Fatalf("tras confirmar, el iris_pat_ anterior debía dar 401: %d", r.status)
	}
	if e.contar(`SELECT count(*) FROM integracion.conectores WHERE id = $1 AND reemplaza_conector_id IS NULL AND reemplaza_token_id IS NULL`, nuevo) != 1 {
		t.Fatal("la confirmación debía cerrar el reemplazo")
	}
	if e.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'integraciones.confirmar_reconexion' AND resultado = 'ok' AND entidad_id = $1`, fmt.Sprint(nuevo)) != 1 {
		t.Fatal("la confirmación se audita")
	}
	e.enviar(viejo, secretoViejo, "wh-063-"+pruebas.Unico(), "cliente.actualizado", map[string]any{"empresa_id": 1, "cliente_id": 71}, time.Now()).
		problema(t, 503, "conector_apagado")
	if e.contar(`SELECT count(*) FROM river.river_job WHERE kind = $1 AND (args->>'conector_id')::bigint IN ($2, $3)`, kindRevocarCanje, viejo, nuevo) != 0 {
		t.Fatal("una reconexión completa no revoca ningún atk_")
	}
}

// patDelUltimoCanje: el iris_pat_ que Iris entregó a Argos en el último canje.
func (e *entorno) patDelUltimoCanje() string {
	e.t.Helper()
	var pat string
	e.argos.con(func() { pat, _ = e.argos.canjes[len(e.argos.canjes)-1]["token"].(string) })
	if !strings.HasPrefix(pat, "iris_pat_") {
		e.t.Fatalf("iris_pat_ del canje: %q", pat)
	}
	return pat
}

// leerConPat lee de Iris como Argos (GET /yo con el iris_pat_).
func (e *entorno) leerConPat(pat string) respuesta {
	e.t.Helper()
	return e.anonimo().pedir(http.MethodGet, "/api/v1/yo", nil, "Authorization", "Bearer "+pat)
}

// reconectado: conexión activa con Argos 1.6.0, otro canje (atk_ nuevo) y su activación, sin
// ninguna llamada con el atk_ nuevo. Devuelve el admin, los dos conectores y el iris_pat_ anterior.
func (e *entorno) reconectado() (admin *clienteHTTP, viejo, nuevo int64, patViejo string) {
	e.t.Helper()
	e.argos.con(func() { e.argos.version = "1.6.0" })
	admin, viejo, _ = e.conectado()
	patViejo = e.patDelUltimoCanje()
	return admin, viejo, e.reconectar(admin, false), patViejo
}

// reconectar: otro canje (atk_ nuevo) sobre el conector activo y su activación (con
// revocarAnterior si se pide). Devuelve el conector nuevo.
func (e *entorno) reconectar(admin *clienteHTTP, revocarAnterior bool) int64 {
	e.t.Helper()
	e.argos.con(func() {
		e.argos.codigo = "codigo-reconexion-" + pruebas.Unico() + "-0123456789"
		e.argos.tokenCanje = "atk_063_reconexion_" + pruebas.Unico()
	})
	r := admin.pedir(http.MethodPost, "/api/v1/integraciones/argosisp/conectar", map[string]any{"enlace": e.argos.enlace()})
	if r.status != 201 {
		e.t.Fatalf("reconectar: %d %s", r.status, r.crudo)
	}
	nuevo := int64(r.cuerpo["id"].(float64))
	cuerpo := map[string]any{"version": 1}
	if revocarAnterior {
		cuerpo["revocarAnterior"] = true
	}
	if a := admin.pedir(http.MethodPost, fmt.Sprintf("/api/v1/integraciones/conectores/%d/activar", nuevo), cuerpo); a.status != 200 {
		e.t.Fatalf("activar: %d %s", a.status, a.crudo)
	}
	return nuevo
}

// Un webhook que Argos ya entregó al conector anterior antes de activar el nuevo, reenviado con el
// secreto anterior durante la reconexión, es un duplicado: no crea un segundo entrante (ni en el viejo
// ni en el nuevo).
func TestReconexionWebhookDuplicadoEntreConectores(t *testing.T) {
	e := nuevoEntorno(t)
	e.argos.con(func() { e.argos.version = "1.6.0" })
	admin, viejo, secretoViejo := e.conectado()
	id := "wh-063-dup-" + pruebas.Unico()
	payload := map[string]any{"empresa_id": 1, "cliente_id": 71}
	primero := e.entranteDe(e.enviar(viejo, secretoViejo, id, "cliente.actualizado", payload, time.Now()))
	nuevo := e.reconectar(admin, false)
	r := e.enviar(viejo, secretoViejo, id, "cliente.actualizado", payload, time.Now())
	if r.status >= 300 || r.cuerpo["duplicado"] != true || e.entranteDe(r) != primero {
		t.Fatalf("el reenvío debía ser un duplicado del primero (%d): %d %s", primero, r.status, r.crudo)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.entrantes WHERE webhook_id = $1`, id); n != 1 {
		t.Fatalf("entrantes con ese id: %d", n)
	}
	// Uno nuevo por el conector viejo sí entra, en el nuevo; su reenvío también es duplicado.
	otro := "wh-063-dup2-" + pruebas.Unico()
	ent := e.entranteDe(e.enviar(viejo, secretoViejo, otro, "cliente.actualizado", payload, time.Now()))
	if e.contar(`SELECT count(*) FROM integracion.entrantes WHERE id = $1 AND conector_id = $2`, ent, nuevo) != 1 {
		t.Fatal("el webhook nuevo del conector reemplazado se guarda en el activo")
	}
	if r := e.enviar(viejo, secretoViejo, otro, "cliente.actualizado", payload, time.Now()); r.cuerpo["duplicado"] != true || e.contar(`SELECT count(*) FROM integracion.entrantes WHERE webhook_id = $1`, otro) != 1 {
		t.Fatalf("reenvío: %s", r.crudo)
	}
}

// revocarAnterior (ante una filtración): reconectar corta en el acto el iris_pat_ anterior, sin
// reemplazo pendiente ni vencimiento, y los webhooks del conector anterior ya no entran.
func TestReconexionRevocarAnteriorCortaYa(t *testing.T) {
	e := nuevoEntorno(t)
	e.argos.con(func() { e.argos.version = "1.6.0" })
	admin, viejo, secretoViejo := e.conectado()
	patViejo := e.patDelUltimoCanje()
	nuevo := e.reconectar(admin, true)
	if r := e.leerConPat(patViejo); r.status != 401 {
		t.Fatalf("con revocarAnterior el iris_pat_ anterior debía dar 401 enseguida: %d", r.status)
	}
	if e.contar(`SELECT count(*) FROM integracion.conectores WHERE id = $1 AND reemplaza_conector_id IS NULL AND reemplaza_token_id IS NULL`, nuevo) != 1 ||
		e.contar(`SELECT count(*) FROM river.river_job WHERE kind = 'integracion.vencer_reemplazo_conector' AND (args->>'conector_nuevo_id')::bigint = $1`, nuevo) != 0 {
		t.Fatal("con revocarAnterior no queda nada diferido")
	}
	if e.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'integraciones.activar' AND entidad_id = $1 AND entrada::text LIKE '%revocarAnterior%'`, fmt.Sprint(nuevo)) != 1 {
		t.Fatal("la auditoría de activar registra revocarAnterior")
	}
	e.enviar(viejo, secretoViejo, "wh-063-"+pruebas.Unico(), "cliente.actualizado", map[string]any{"empresa_id": 1, "cliente_id": 71}, time.Now()).
		problema(t, 503, "conector_apagado")
}

// Sin confirmación (Argos nunca recibe una llamada del atk_ nuevo), el job de las 24 h revoca el
// iris_pat_ anterior; una confirmación tardía ya no hace nada.
func TestReconexionSinConfirmarVenceA24h(t *testing.T) {
	e := nuevoEntorno(t)
	_, viejo, nuevo, patViejo := e.reconectado()
	if r := e.leerConPat(patViejo); r.status != 200 {
		t.Fatalf("antes del vencimiento el iris_pat_ anterior sigue valiendo: %d", r.status)
	}
	var crudo []byte
	if err := e.pool.QueryRow(context.Background(), `SELECT args FROM river.river_job WHERE kind = 'integracion.vencer_reemplazo_conector'
		AND (args->>'conector_nuevo_id')::bigint = $1 AND scheduled_at > now() + interval '23 hours'`, nuevo).Scan(&crudo); err != nil {
		t.Fatal(err)
	}
	var args conector.ArgsVencerReemplazo
	if err := json.Unmarshal(crudo, &args); err != nil || args.ConectorViejoID != viejo {
		t.Fatalf("args: %s %v", crudo, err)
	}
	// Lo que corre el worker a las 24 h.
	ctx := pruebas.ContextoSistema(e.pool)
	r, err := e.mod.Nucleo().VencerReemplazo.Ejecutar(ctx, args)
	if err != nil || !r.Cerrado || r.TokenRevocado == 0 {
		t.Fatalf("vencer: %+v %v", r, err)
	}
	if r := e.leerConPat(patViejo); r.status != 401 {
		t.Fatalf("a las 24 h el iris_pat_ anterior debía quedar revocado: %d", r.status)
	}
	if e.contar(`SELECT count(*) FROM integracion.conectores WHERE id = $1 AND reemplaza_conector_id IS NULL`, nuevo) != 1 {
		t.Fatal("el vencimiento cierra el reemplazo")
	}
	// Repetirlo, o una confirmación tardía, no hace nada.
	if r, err := e.mod.Nucleo().VencerReemplazo.Ejecutar(ctx, args); err != nil || r.Cerrado {
		t.Fatalf("vencer otra vez: %+v %v", r, err)
	}
	if r, err := e.mod.Nucleo().ConfirmarReemplazo.Ejecutar(ctx, nuevo); err != nil || r.Cerrado {
		t.Fatalf("confirmar tarde: %+v %v", r, err)
	}
}

// Desactivar el conector nuevo antes de confirmar revoca también el iris_pat_ anterior (nadie más
// lo cierra a tiempo y la integración queda apagada).
func TestReconexionDesactivadaRevocaElAnterior(t *testing.T) {
	e := nuevoEntorno(t)
	admin, _, nuevo, patViejo := e.reconectado()
	if d := admin.pedir(http.MethodPost, fmt.Sprintf("/api/v1/integraciones/conectores/%d/desactivar", nuevo), map[string]any{"version": 2, "motivo": "prueba"}); d.status != 200 {
		t.Fatalf("desactivar: %d %s", d.status, d.crudo)
	}
	if r := e.leerConPat(patViejo); r.status != 401 {
		t.Fatalf("desactivar debía revocar el iris_pat_ anterior: %d", r.status)
	}
}
