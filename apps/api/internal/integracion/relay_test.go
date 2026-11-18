package integracion_test

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion"
	integracionbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
	"github.com/HCDestroyer/iris/apps/api/migraciones"
)

func TestMain(m *testing.M) { os.Exit(pruebas.Correr(m)) }

var emitir = auditoria.Nuevo(auditoria.Caso[string, struct{}]{
	Accion: "usuarios.crear",
	Logica: func(_ context.Context, t *auditoria.Tx, evento string) (struct{}, error) {
		return struct{}{}, t.Emitir(evento, map[string]any{"x": 1})
	},
})

type receptor struct {
	mu       sync.Mutex
	recibido []integracion.Evento
	fallar   bool
}

func (r *receptor) fn(ctx context.Context, e integracion.Evento) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if contexto.ActorDe(ctx).Tipo != contexto.ActorSistema || nucleo.TxDe(ctx) != nil || nucleo.BDDe(ctx) == nil {
		return errors.New("contexto del suscriptor inesperado")
	}
	if r.fallar {
		return errors.New("consumidor caído")
	}
	r.recibido = append(r.recibido, e)
	return nil
}

func (r *receptor) cantidad() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.recibido)
}

func TestRelayEntregaYMarcaPublicado(t *testing.T) {
	pool := pruebas.Pool(t)
	ctx := pruebas.ContextoSistema(pool)
	if _, err := emitir.Ejecutar(ctx, "prueba.relay"); err != nil {
		t.Fatal(err)
	}

	r := &receptor{}
	bus := integracion.NuevoBus()
	bus.Suscribir("prueba", "prueba.relay", r.fn)
	otro := &receptor{}
	bus.Suscribir("otro", "otro.evento", otro.fn)
	relay := &integracion.Relay{BD: pool, Bus: bus}

	n, err := relay.Ejecutar(context.Background())
	if err != nil || n < 1 {
		t.Fatalf("publicados=%d err=%v", n, err)
	}
	if r.cantidad() != 1 || otro.cantidad() != 0 {
		t.Fatalf("entregas: suscrito=%d otro=%d", r.cantidad(), otro.cantidad())
	}
	e := r.recibido[0]
	if e.Evento != "prueba.relay" || e.RequestID != contexto.RequestIDDe(ctx) || string(e.Payload) != `{"x": 1}` && string(e.Payload) != `{"x":1}` {
		t.Fatalf("evento: %+v payload=%s", e, e.Payload)
	}
	fila, err := integracionbd.New(pool).EventoPorID(context.Background(), e.ID)
	if err != nil || fila.PublicadoEn == nil || fila.Intentos != 1 {
		t.Fatalf("fila: %+v err=%v", fila, err)
	}

	// Una segunda corrida no repite.
	if n, _ := relay.Ejecutar(context.Background()); n != 0 || r.cantidad() != 1 {
		t.Fatalf("segunda corrida: publicados=%d entregas=%d", n, r.cantidad())
	}
}

func TestRelayReintentaConEspera(t *testing.T) {
	pool := pruebas.Pool(t)
	ctx := pruebas.ContextoSistema(pool)
	if _, err := emitir.Ejecutar(ctx, "prueba.falla"); err != nil {
		t.Fatal(err)
	}
	r := &receptor{fallar: true}
	bus := integracion.NuevoBus()
	bus.Suscribir("caido", "prueba.falla", r.fn)
	relay := &integracion.Relay{BD: pool, Bus: bus}
	if _, err := relay.Ejecutar(context.Background()); err != nil {
		t.Fatal(err)
	}
	filas, _ := integracionbd.New(pool).EventosPorRequest(context.Background(), contexto.RequestIDDe(ctx))
	if len(filas) != 1 {
		t.Fatalf("filas: %d", len(filas))
	}
	f := filas[0]
	if f.PublicadoEn != nil || f.Intentos != 1 || f.UltimoError == nil || !f.ProximoIntento.After(time.Now()) {
		t.Fatalf("fallo mal registrado: %+v", f)
	}
	// Antes de su próximo intento no se vuelve a tomar.
	r.fallar = false
	if _, err := relay.Ejecutar(context.Background()); err != nil {
		t.Fatal(err)
	}
	if r.cantidad() != 0 {
		t.Fatal("se reintentó antes de tiempo")
	}
}

func TestRelayRespetaElCandado(t *testing.T) {
	pool := pruebas.Pool(t)
	ctx := pruebas.ContextoSistema(pool)
	if _, err := emitir.Ejecutar(ctx, "prueba.candado"); err != nil {
		t.Fatal(err)
	}
	r := &receptor{}
	bus := integracion.NuevoBus()
	bus.Suscribir("prueba", "prueba.candado", r.fn)
	relay := &integracion.Relay{BD: pool, Bus: bus}

	// Otro relay tiene el candado: este no hace nada.
	err := nucleo.EnTx(nucleo.ConBD(context.Background(), pool), func(ctx context.Context, tx pgx.Tx) error {
		if _, err := integracionbd.New(tx).TomarCandadoRelay(ctx, 0x1715_0001); err != nil {
			return err
		}
		n, err := relay.Ejecutar(context.Background())
		if err != nil || n != 0 {
			return errors.New("el relay corrió con el candado tomado")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := relay.Ejecutar(context.Background()); n < 1 || r.cantidad() != 1 {
		t.Fatalf("liberado el candado debía publicar: %d", n)
	}
}

func (r *receptor) de(requestID string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, e := range r.recibido {
		if e.RequestID == requestID {
			n++
		}
	}
	return n
}

// arrancarRiver arranca River sin sondeo (RelayIntervalo de una hora) y el listener del outbox;
// devuelve cuando el listener escucha y ya corrieron el relay periódico de arranque (RunOnStart)
// y el barrido del listener: lo que se emita después solo llega por su NOTIFY. Por listos llega
// el pid de cada conexión que hace LISTEN.
func arrancarRiver(t *testing.T, pool *pgxpool.Pool, evento string) (*receptor, <-chan uint32) {
	t.Helper()
	r := &receptor{}
	bus := integracion.NuevoBus()
	bus.Suscribir("prueba", evento, r.fn)
	relay := &integracion.Relay{BD: pool, Bus: bus}

	cliente, err := integracion.NuevoClienteRiver(pool, relay, integracion.OpcionesWorker{
		Esquema: migraciones.EsquemaRiver, RelayIntervalo: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	var antes int64
	if err := pool.QueryRow(context.Background(), `SELECT coalesce(max(id), 0) FROM river.river_job`).Scan(&antes); err != nil {
		t.Fatal(err)
	}
	ctx, cancelar := context.WithCancel(context.Background())
	if err := cliente.Start(ctx); err != nil {
		cancelar()
		t.Fatal(err)
	}
	listos := make(chan uint32, 8)
	termino := make(chan struct{})
	go func() {
		defer close(termino)
		integracion.EscucharOutboxConListo(ctx, pool, cliente, nil, func(pid uint32) { listos <- pid })
	}()
	t.Cleanup(func() {
		cancelar()
		<-termino
		parar, c := context.WithTimeout(context.Background(), 10*time.Second)
		defer c()
		_ = cliente.Stop(parar)
	})
	select {
	case <-listos:
	case <-time.After(15 * time.Second):
		t.Fatal("el LISTEN del outbox no quedó activo")
	}
	esperarRelaysTerminados(t, pool, antes)
	return r, listos
}

// esperarRelaysTerminados espera a que el relay periódico de arranque haya terminado y no quede
// ningún relay por correr (jobs con id mayor que antes).
func esperarRelaysTerminados(t *testing.T, pool *pgxpool.Pool, antes int64) {
	t.Helper()
	limite := time.Now().Add(15 * time.Second)
	for time.Now().Before(limite) {
		var periodico, abiertos int
		err := pool.QueryRow(context.Background(), `
			SELECT count(*) FILTER (WHERE metadata->>'periodic' = 'true' AND finalized_at IS NOT NULL),
			       count(*) FILTER (WHERE finalized_at IS NULL)
			FROM river.river_job WHERE kind = $1 AND id > $2`,
			integracion.ArgsRelay{}.Kind(), antes).Scan(&periodico, &abiertos)
		if err != nil {
			t.Fatal(err)
		}
		if periodico > 0 && abiertos == 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("el relay de arranque no terminó")
}

func esperarEntrega(t *testing.T, r *receptor, requestID string) {
	t.Helper()
	limite := time.Now().Add(15 * time.Second)
	for r.de(requestID) == 0 && time.Now().Before(limite) {
		time.Sleep(50 * time.Millisecond)
	}
	if n := r.de(requestID); n != 1 {
		t.Fatalf("River no entregó el evento (entregas=%d)", n)
	}
}

// Sin sondeo: el COMMIT dispara NOTIFY → job de River → relay. El evento se emite justo después
// de que terminó otro relay (el de arranque): regresión de la unicidad por segundo de River, que
// descartaba el job del NOTIFY por el relay ya terminado en el mismo segundo.
func TestRelayConRiver(t *testing.T) {
	pool := pruebas.Pool(t)
	r, _ := arrancarRiver(t, pool, "prueba.river")

	ctx := pruebas.ContextoSistema(pool)
	if _, err := emitir.Ejecutar(ctx, "prueba.river"); err != nil {
		t.Fatal(err)
	}
	esperarEntrega(t, r, contexto.RequestIDDe(ctx))
}

// Regresión: un NOTIFY emitido mientras el listener se reconecta se pierde; tras el nuevo LISTEN
// el listener encola un barrido que entrega lo pendiente, sin esperar al sondeo.
func TestRelayConRiverTrasReconectar(t *testing.T) {
	pool := pruebas.Pool(t)
	r, listos := arrancarRiver(t, pool, "prueba.reconecta")

	var pid uint32
	if err := pool.QueryRow(context.Background(),
		`SELECT pid FROM pg_stat_activity WHERE datname = current_database() AND query = $1 AND pid <> pg_backend_pid()`,
		"LISTEN "+integracion.CanalNotificacion).Scan(&pid); err != nil {
		t.Fatal(err)
	}
	var cortada bool
	if err := pool.QueryRow(context.Background(), `SELECT pg_terminate_backend($1)`, pid).Scan(&cortada); err != nil || !cortada {
		t.Fatalf("cortar el listener %d: %v", pid, err)
	}
	// El listener espera al menos un segundo antes de reconectar: este NOTIFY no lo oye nadie.
	ctx := pruebas.ContextoSistema(pool)
	if _, err := emitir.Ejecutar(ctx, "prueba.reconecta"); err != nil {
		t.Fatal(err)
	}
	select {
	case nuevo := <-listos:
		if nuevo == pid {
			t.Fatalf("el listener no reconectó (pid %d)", nuevo)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("el listener no reconectó")
	}
	esperarEntrega(t, r, contexto.RequestIDDe(ctx))
}

// Regresión: un evento confirmado entre el último barrido vacío del relay y la liberación de su
// candado se entrega en la misma corrida (el relay que encoló su NOTIFY encontró el candado
// tomado y salió sin hacer nada).
func TestRelayMiraTrasSoltarCandado(t *testing.T) {
	pool := pruebas.Pool(t)
	r := &receptor{}
	bus := integracion.NuevoBus()
	bus.Suscribir("prueba", "prueba.tarde", r.fn)
	relay := &integracion.Relay{BD: pool, Bus: bus}
	otro := &integracion.Relay{BD: pool, Bus: bus}

	ctx := pruebas.ContextoSistema(pool)
	var errGancho error
	relay.AlVaciar(func() {
		relay.AlVaciar(nil)
		if _, errGancho = emitir.Ejecutar(ctx, "prueba.tarde"); errGancho != nil {
			return
		}
		// El relay de su NOTIFY corre ahora y encuentra el candado tomado.
		if n, err := otro.Ejecutar(context.Background()); err != nil || n != 0 {
			errGancho = errors.New("el segundo relay corrió con el candado tomado")
		}
	})
	if _, err := relay.Ejecutar(context.Background()); err != nil {
		t.Fatal(err)
	}
	if errGancho != nil {
		t.Fatal(errGancho)
	}
	if n := r.de(contexto.RequestIDDe(ctx)); n != 1 {
		t.Fatalf("el evento confirmado durante la corrida quedó pendiente (entregas=%d)", n)
	}
}

// Regresión: cada evento se confirma solo. Si la corrida se corta (timeout del job) a mitad,
// lo ya entregado queda publicado y el evento en curso cuenta su intento.
func TestRelayConfirmaPorEvento(t *testing.T) {
	pool := pruebas.Pool(t)
	sis := pruebas.ContextoSistema(pool)
	if _, err := emitir.Ejecutar(sis, "prueba.primero"); err != nil {
		t.Fatal(err)
	}
	sis2 := pruebas.ContextoSistema(pool)
	if _, err := emitir.Ejecutar(sis2, "prueba.corta"); err != nil {
		t.Fatal(err)
	}
	ctx, cancelar := context.WithCancel(context.Background())
	defer cancelar()
	primero := &receptor{}
	bus := integracion.NuevoBus()
	bus.Suscribir("primero", "prueba.primero", primero.fn)
	bus.Suscribir("corta", "prueba.corta", func(ctx context.Context, _ integracion.Evento) error {
		cancelar() // el job llega a su timeout mientras entrega este evento
		return ctx.Err()
	})
	relay := &integracion.Relay{BD: pool, Bus: bus}
	_, _ = relay.Ejecutar(ctx)

	q := integracionbd.New(pool)
	a, _ := q.EventosPorRequest(context.Background(), contexto.RequestIDDe(sis))
	b, _ := q.EventosPorRequest(context.Background(), contexto.RequestIDDe(sis2))
	if len(a) != 1 || a[0].PublicadoEn == nil || primero.cantidad() != 1 {
		t.Fatalf("el evento entregado antes del corte debía quedar publicado: %+v", a)
	}
	if len(b) != 1 || b[0].PublicadoEn != nil || b[0].Intentos != 1 || b[0].UltimoError == nil {
		t.Fatalf("el evento cortado debía contar su intento: %+v", b)
	}
	if _, err := relay.Ejecutar(context.Background()); err != nil || primero.cantidad() != 1 {
		t.Fatalf("no se reentrega lo ya publicado: entregas=%d err=%v", primero.cantidad(), err)
	}
}

// Regresión: al agotar MaxIntentos el evento queda fallido y no se vuelve a tomar.
func TestRelayAgotaIntentos(t *testing.T) {
	pool := pruebas.Pool(t)
	sis := pruebas.ContextoSistema(pool)
	if _, err := emitir.Ejecutar(sis, "prueba.agota"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(),
		`UPDATE integracion.outbox SET intentos = 19 WHERE request_id = $1`, contexto.RequestIDDe(sis)); err != nil {
		t.Fatal(err)
	}
	r := &receptor{fallar: true}
	bus := integracion.NuevoBus()
	bus.Suscribir("caido", "prueba.agota", r.fn)
	relay := &integracion.Relay{BD: pool, Bus: bus}
	if _, err := relay.Ejecutar(context.Background()); err != nil {
		t.Fatal(err)
	}
	filas, _ := integracionbd.New(pool).EventosPorRequest(context.Background(), contexto.RequestIDDe(sis))
	if len(filas) != 1 || filas[0].FallidoEn == nil || filas[0].Intentos != 20 || filas[0].PublicadoEn != nil {
		t.Fatalf("debía quedar fallido con 20 intentos: %+v", filas)
	}
	// Aunque llegue su próximo intento y el consumidor vuelva, no se toma.
	if _, err := pool.Exec(context.Background(),
		`UPDATE integracion.outbox SET proximo_intento = now() WHERE request_id = $1`, contexto.RequestIDDe(sis)); err != nil {
		t.Fatal(err)
	}
	r.fallar = false
	if _, err := relay.Ejecutar(context.Background()); err != nil || r.cantidad() != 0 {
		t.Fatalf("un evento fallido no se reintenta: entregas=%d err=%v", r.cantidad(), err)
	}
}
