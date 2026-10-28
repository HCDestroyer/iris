// Comando worker: workers de River. El relay del outbox (integracion.outbox → suscriptores
// internos), que corre por NOTIFY y cada IRIS_RELAY_INTERVALO, y el cron de SLA de soporte
// (cada minuto).
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/config"
	"github.com/HCDestroyer/iris/apps/api/internal/soporte"
	"github.com/HCDestroyer/iris/apps/api/migraciones"
)

var version = "0.0.0-dev"

func main() {
	if err := correr(); err != nil {
		fmt.Fprintln(os.Stderr, "worker:", err)
		os.Exit(1)
	}
}

func correr() error {
	ctx, cancelar := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancelar()

	cfg, err := config.Cargar()
	if err != nil {
		return err
	}
	log := cfg.Logger()
	pool, err := pgxpool.New(ctx, cfg.DBURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	bus := integracion.NuevoBus()
	// Suscriptores internos de cada módulo: bus.Suscribir("soporte.x", "orden.cerrada", fn).
	bus.Suscribir("registro", "*", func(ctx context.Context, e integracion.Evento) error {
		log.DebugContext(ctx, "evento publicado", "evento", e.Evento, "id", e.ID, "request_id", e.RequestID)
		return nil
	})
	relay := &integracion.Relay{BD: pool, Bus: bus, Log: log}

	sop := soporte.Nuevo(soporte.Config{})
	cliente, err := integracion.NuevoClienteRiver(pool, relay, integracion.OpcionesWorker{
		Esquema: migraciones.EsquemaRiver, RelayIntervalo: cfg.RelayIntervalo, Log: log,
		Registrar: sop.RegistrarTrabajos(pool),
	})
	if err != nil {
		return err
	}
	cliente.PeriodicJobs().Add(soporte.TrabajoPeriodicoSLA(time.Minute))
	if err := cliente.Start(ctx); err != nil {
		return err
	}
	go integracion.EscucharOutbox(ctx, pool, cliente, log)
	log.Info("worker en marcha", "version", version)

	<-ctx.Done()
	parar, cancelarParada := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelarParada()
	log.Info("apagando worker")
	return cliente.Stop(parar)
}
