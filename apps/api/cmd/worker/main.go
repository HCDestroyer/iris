// Comando worker: workers de River. El relay del outbox (integracion.outbox → suscriptores
// internos), que corre por NOTIFY y cada IRIS_RELAY_INTERVALO, y los jobs del conector Argos
// (webhooks entrantes y refresco periódico de zonas y routers).
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/riverqueue/river"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/config"
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

	// Conector Argos: procesa los webhooks entrantes y refresca zonas y routers.
	acfg := argos.Config{BaseURL: cfg.BaseURL, Timeout: cfg.ArgosTimeout, PermitirHTTP: cfg.ConectoresPermitirHTTP, Version: version}
	if cfg.ClaveCifrado != "" {
		if acfg.Cifrador, err = nucleo.NuevoCifrador(cfg.ClaveCifrado); err != nil {
			return err
		}
	} else {
		log.Warn("IRIS_CLAVE_CIFRADO vacía: los webhooks de Argos no se podrán procesar")
	}
	conectorArgos := argos.Nuevo(acfg)

	cliente, err := integracion.NuevoClienteRiver(pool, relay, integracion.OpcionesWorker{
		Esquema: migraciones.EsquemaRiver, RelayIntervalo: cfg.RelayIntervalo, Log: log,
		Registrar:  func(w *river.Workers) { conectorArgos.RegistrarWorkers(w, pool) },
		Periodicos: argos.TrabajosPeriodicos(0),
	})
	if err != nil {
		return err
	}
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
