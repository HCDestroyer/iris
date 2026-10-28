package integracion_test

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

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

func TestRelayConRiver(t *testing.T) {
	pool := pruebas.Pool(t)
	r := &receptor{}
	bus := integracion.NuevoBus()
	bus.Suscribir("prueba", "prueba.river", r.fn)
	relay := &integracion.Relay{BD: pool, Bus: bus}

	cliente, err := integracion.NuevoClienteRiver(pool, relay, integracion.OpcionesWorker{
		Esquema: migraciones.EsquemaRiver, RelayIntervalo: time.Hour,
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
	go integracion.EscucharOutbox(ctx, pool, cliente, nil)
	time.Sleep(300 * time.Millisecond) // que el LISTEN quede activo

	// El COMMIT dispara NOTIFY → job de River → relay.
	if _, err := emitir.Ejecutar(pruebas.ContextoSistema(pool), "prueba.river"); err != nil {
		t.Fatal(err)
	}
	limite := time.Now().Add(15 * time.Second)
	for r.cantidad() == 0 && time.Now().Before(limite) {
		time.Sleep(50 * time.Millisecond)
	}
	if r.cantidad() != 1 {
		t.Fatalf("River no entregó el evento (entregas=%d)", r.cantidad())
	}
}
