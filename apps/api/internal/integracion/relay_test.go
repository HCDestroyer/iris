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
	"github.com/riverqueue/river"

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

// rio es River sin sondeo (RelayIntervalo de una hora) con el listener del outbox y ganchos
// que la prueba puede cambiar mientras corre.
type rio struct {
	cliente *river.Client[pgx.Tx]
	listos  chan uint32 // pid de cada conexión que hace LISTEN (tras encolar su barrido)
	pid     uint32      // la conexión que escucha ahora

	mu                sync.Mutex
	alTerminar        func()
	antesDeReconectar func(espera time.Duration)
}

func (o *rio) fijar(alTerminar func(), antesDeReconectar func(time.Duration)) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.alTerminar, o.antesDeReconectar = alTerminar, antesDeReconectar
}

func (o *rio) ganchos() (func(), func(time.Duration)) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.alTerminar, o.antesDeReconectar
}

// arrancarRiver arranca River y el listener con los suscriptores que agrega suscribir; devuelve
// cuando el listener escucha y ya terminaron el relay periódico de arranque (RunOnStart) y el
// barrido del listener: lo que se emita después solo llega por su NOTIFY. El listener arranca
// cuando ya terminó el periódico: si su barrido quedara disponible antes, el periódico de
// arranque no se encolaría y no habría forma de saber que ya pasó.
func arrancarRiver(t *testing.T, pool *pgxpool.Pool, suscribir func(*integracion.Bus)) *rio {
	t.Helper()
	o := &rio{listos: make(chan uint32, 8)}
	bus := integracion.NuevoBus()
	suscribir(bus)
	relay := &integracion.Relay{BD: pool, Bus: bus}
	relay.AlTerminar(func() {
		if f, _ := o.ganchos(); f != nil {
			f()
		}
	})

	var err error
	o.cliente, err = integracion.NuevoClienteRiver(pool, relay, integracion.OpcionesWorker{
		Esquema: migraciones.EsquemaRiver, RelayIntervalo: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Un relay disponible que dejó otra prueba haría que el periódico de arranque no se encole.
	if _, err := pool.Exec(context.Background(),
		`DELETE FROM river.river_job WHERE kind = $1 AND state = 'available'`, integracion.ArgsRelay{}.Kind()); err != nil {
		t.Fatal(err)
	}
	antes := ultimoJob(t, pool)
	ctx, cancelar := context.WithCancel(context.Background())
	if err := o.cliente.Start(ctx); err != nil {
		cancelar()
		t.Fatal(err)
	}
	termino := make(chan struct{})
	t.Cleanup(func() {
		cancelar()
		<-termino
		parar, c := context.WithTimeout(context.Background(), 10*time.Second)
		defer c()
		_ = o.cliente.Stop(parar)
	})
	esperarRelaysTerminados(t, pool, antes)
	go func() {
		defer close(termino)
		integracion.EscucharOutboxConGanchos(ctx, pool, o.cliente, migraciones.EsquemaRiver, nil, integracion.GanchosEscucha{
			Listo: func(pid uint32) { o.listos <- pid },
			AntesDeReconectar: func(espera time.Duration) {
				if _, f := o.ganchos(); f != nil {
					f(espera)
				}
			},
		})
	}()
	o.pid = esperarListo(t, o)
	esperarRelaysTerminados(t, pool, antes)
	return o
}

func esperarListo(t *testing.T, o *rio) uint32 {
	t.Helper()
	select {
	case pid := <-o.listos:
		return pid
	case <-time.After(15 * time.Second):
		t.Fatal("el LISTEN del outbox no quedó activo")
		return 0
	}
}

func ultimoJob(t *testing.T, pool *pgxpool.Pool) int64 {
	t.Helper()
	var id int64
	if err := pool.QueryRow(context.Background(), `SELECT coalesce(max(id), 0) FROM river.river_job`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// relaysDesde cuenta los jobs del relay con id mayor que antes: todos, los periódicos ya
// terminados y los que no terminaron.
func relaysDesde(t *testing.T, pool *pgxpool.Pool, antes int64) (todos, periodicos, abiertos int) {
	t.Helper()
	err := pool.QueryRow(context.Background(), `
		SELECT count(*),
		       count(*) FILTER (WHERE metadata->>'periodic' = 'true' AND finalized_at IS NOT NULL),
		       count(*) FILTER (WHERE finalized_at IS NULL)
		FROM river.river_job WHERE kind = $1 AND id > $2`,
		integracion.ArgsRelay{}.Kind(), antes).Scan(&todos, &periodicos, &abiertos)
	if err != nil {
		t.Fatal(err)
	}
	return todos, periodicos, abiertos
}

// esperarRelaysTerminados espera a que el relay periódico de arranque haya terminado y no quede
// ningún relay por correr (jobs con id mayor que antes).
func esperarRelaysTerminados(t *testing.T, pool *pgxpool.Pool, antes int64) {
	t.Helper()
	limite := time.Now().Add(15 * time.Second)
	for time.Now().Before(limite) {
		if _, periodicos, abiertos := relaysDesde(t, pool, antes); periodicos > 0 && abiertos == 0 {
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

// Sin sondeo: el COMMIT dispara NOTIFY → job de River → relay. El evento se confirma en cuanto
// termina un relay (gancho al final de Ejecutar, con su job aún en curso para River): regresión
// de la unicidad de River, que descartaba el job del NOTIFY por ese relay.
func TestRelayConRiver(t *testing.T) {
	pool := pruebas.Pool(t)
	r := &receptor{}
	o := arrancarRiver(t, pool, func(b *integracion.Bus) { b.Suscribir("prueba", "prueba.river", r.fn) })

	ctx := pruebas.ContextoSistema(pool)
	emitido := make(chan error, 1)
	var una sync.Once
	o.fijar(func() {
		una.Do(func() {
			_, err := emitir.Ejecutar(ctx, "prueba.river")
			emitido <- err
		})
	}, nil)
	if _, err := o.cliente.Insert(context.Background(), integracion.ArgsRelay{}, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-emitido:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("el relay encolado no terminó")
	}
	esperarEntrega(t, r, contexto.RequestIDDe(ctx))
}

// Regresión: un NOTIFY emitido mientras el listener se reconecta se pierde; tras el nuevo LISTEN
// el listener encola un barrido que entrega lo pendiente, sin esperar al sondeo. El evento se
// confirma en el gancho previo a la espera de reconexión, así que nadie escucha su NOTIFY. Tras
// cada LISTEN correcto la espera vuelve a 1 s (antes crecía con cada corte: 1, 2, 4 s…).
func TestRelayConRiverTrasReconectar(t *testing.T) {
	pool := pruebas.Pool(t)
	r := &receptor{}
	o := arrancarRiver(t, pool, func(b *integracion.Bus) { b.Suscribir("prueba", "prueba.reconecta", r.fn) })

	for corte := 1; corte <= 3; corte++ {
		ctx := pruebas.ContextoSistema(pool)
		emitido := make(chan error, 1)
		esperas := make(chan time.Duration, 1)
		o.fijar(nil, func(e time.Duration) {
			esperas <- e
			_, err := emitir.Ejecutar(ctx, "prueba.reconecta")
			emitido <- err
		})
		var cortada bool
		if err := pool.QueryRow(context.Background(), `SELECT pg_terminate_backend($1)`, o.pid).Scan(&cortada); err != nil || !cortada {
			t.Fatalf("corte %d: cortar el listener %d: %v", corte, o.pid, err)
		}
		if err := <-emitido; err != nil {
			t.Fatal(err)
		}
		if espera := <-esperas; espera != time.Second {
			t.Fatalf("corte %d: esperó %s antes de reconectar; tras un LISTEN correcto debía ser 1s", corte, espera)
		}
		anterior := o.pid
		o.pid = esperarListo(t, o)
		if o.pid == anterior {
			t.Fatalf("corte %d: el listener no reconectó (pid %d)", corte, o.pid)
		}
		esperarEntrega(t, r, contexto.RequestIDDe(ctx))
	}
}

// Una ráfaga de commits mientras un relay corre (bloqueado en un suscriptor) no llena River de
// jobs: el relay tiene su cola de un worker, y el listener no encola si ya hay uno disponible.
func TestRelayRafagaAcotaJobs(t *testing.T) {
	const n = 200
	pool := pruebas.Pool(t)
	r := &receptor{}
	entro := make(chan struct{}, 1)
	soltar := make(chan struct{})
	var soltarUna sync.Once
	liberar := func() { soltarUna.Do(func() { close(soltar) }) }
	defer liberar()
	arrancarRiver(t, pool, func(b *integracion.Bus) {
		b.Suscribir("rafaga", "prueba.rafaga", r.fn)
		b.Suscribir("bloqueo", "prueba.bloqueo", func(context.Context, integracion.Evento) error {
			select {
			case entro <- struct{}{}:
			default:
			}
			<-soltar
			return nil
		})
	})
	antes := ultimoJob(t, pool)
	if _, err := emitir.Ejecutar(pruebas.ContextoSistema(pool), "prueba.bloqueo"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entro:
	case <-time.After(15 * time.Second):
		t.Fatal("el relay no tomó el evento de bloqueo")
	}
	ids := make([]string, 0, n)
	for range n {
		ctx := pruebas.ContextoSistema(pool)
		if _, err := emitir.Ejecutar(ctx, "prueba.rafaga"); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, contexto.RequestIDDe(ctx))
	}
	liberar()

	limite := time.Now().Add(30 * time.Second)
	for r.cantidad() < n && time.Now().Before(limite) {
		time.Sleep(50 * time.Millisecond)
	}
	for _, id := range ids {
		if r.de(id) != 1 {
			t.Fatalf("la ráfaga no se entregó entera: %d de %d", r.cantidad(), n)
		}
	}
	limite = time.Now().Add(15 * time.Second)
	for time.Now().Before(limite) {
		if _, _, abiertos := relaysDesde(t, pool, antes); abiertos == 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	// El relay bloqueado, el que quedó disponible durante la ráfaga y, como mucho, dos más por
	// avisos que el listener procesó cuando ese ya corría. Sin las dos medidas: hasta uno por aviso.
	if todos, _, abiertos := relaysDesde(t, pool, antes); todos > 4 || abiertos != 0 {
		t.Fatalf("la ráfaga de %d commits creó %d jobs del relay (abiertos=%d); se esperaban 4 o menos", n, todos, abiertos)
	}
}

// El periódico no encola mientras ya hay un relay disponible: con una entrega atascada no se
// juntan jobs cada intervalo.
func TestRelayPeriodicoNoAcumula(t *testing.T) {
	pool := pruebas.Pool(t)
	entro := make(chan struct{}, 1)
	soltar := make(chan struct{})
	var soltarUna sync.Once
	liberar := func() { soltarUna.Do(func() { close(soltar) }) }
	defer liberar()
	bus := integracion.NuevoBus()
	bus.Suscribir("atascado", "prueba.atascado", func(context.Context, integracion.Evento) error {
		select {
		case entro <- struct{}{}:
		default:
		}
		<-soltar
		return nil
	})
	relay := &integracion.Relay{BD: pool, Bus: bus}
	cliente, err := integracion.NuevoClienteRiver(pool, relay, integracion.OpcionesWorker{
		Esquema: migraciones.EsquemaRiver, RelayIntervalo: 100 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(),
		`DELETE FROM river.river_job WHERE kind = $1 AND state = 'available'`, integracion.ArgsRelay{}.Kind()); err != nil {
		t.Fatal(err)
	}
	if _, err := emitir.Ejecutar(pruebas.ContextoSistema(pool), "prueba.atascado"); err != nil {
		t.Fatal(err)
	}
	antes := ultimoJob(t, pool)
	ctx, cancelar := context.WithCancel(context.Background())
	defer cancelar()
	if err := cliente.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() {
		liberar()
		parar, c := context.WithTimeout(context.Background(), 10*time.Second)
		defer c()
		_ = cliente.Stop(parar)
	}()
	select {
	case <-entro:
	case <-time.After(15 * time.Second):
		t.Fatal("el relay de arranque no tomó el evento")
	}
	// Con el relay atascado (la cola tiene un worker), el primer tic deja uno disponible y los
	// siguientes no encolan. Se espera a que haya uno disponible y luego unos 15 intervalos.
	limite := time.Now().Add(15 * time.Second)
	for {
		hay, err := integracion.HayRelayDisponible(context.Background(), pool, migraciones.EsquemaRiver)
		if err != nil {
			t.Fatal(err)
		}
		if hay {
			break
		}
		if time.Now().After(limite) {
			t.Fatal("el periódico no encoló ningún relay")
		}
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(1500 * time.Millisecond)
	if todos, _, _ := relaysDesde(t, pool, antes); todos != 2 {
		t.Fatalf("con un relay atascado y otro disponible el periódico creó %d jobs; debían ser 2", todos)
	}
}

// Si la comprobación de «ya hay uno disponible» falla, el listener encola igual.
func TestRelayEncolaSiFallaLaComprobacion(t *testing.T) {
	pool := pruebas.Pool(t)
	cliente, err := integracion.NuevoClienteInsercion(pool, migraciones.EsquemaRiver, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := integracion.HayRelayDisponible(context.Background(), pool, "no_existe"); err == nil {
		t.Fatal("la comprobación con un esquema inexistente debía fallar")
	}
	antes := ultimoJob(t, pool)
	if err := integracion.EncolarRelay(context.Background(), pool, cliente, "no_existe"); err != nil {
		t.Fatal(err)
	}
	if todos, _, _ := relaysDesde(t, pool, antes); todos != 1 {
		t.Fatalf("debía encolar un relay aunque fallara la comprobación: %d", todos)
	}
}

// Regresión: una fila pendiente bloqueada por otra transacción no hace girar al relay (la
// segunda mirada la contaba como pendiente y el barrido la saltaba).
func TestRelayNoGiraConFilaBloqueada(t *testing.T) {
	pool := pruebas.Pool(t)
	ctx := pruebas.ContextoSistema(pool)
	if _, err := emitir.Ejecutar(ctx, "prueba.bloqueada"); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var id string
	if err := tx.QueryRow(context.Background(),
		`SELECT id::text FROM integracion.outbox WHERE request_id = $1 FOR UPDATE`, contexto.RequestIDDe(ctx)).Scan(&id); err != nil {
		t.Fatal(err)
	}

	relay := &integracion.Relay{BD: pool, Bus: integracion.NuevoBus()}
	corrida, cortar := context.WithTimeout(context.Background(), 15*time.Second)
	defer cortar()
	vueltas := 0
	relay.AlVaciar(func() {
		vueltas++
		if vueltas > 5 {
			cortar()
		}
	})
	if _, err := relay.Ejecutar(corrida); err != nil {
		t.Fatal(err)
	}
	if vueltas != 1 {
		t.Fatalf("el relay dio %d vueltas con una fila bloqueada; debía dar 1", vueltas)
	}
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
