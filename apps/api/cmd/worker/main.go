// Comando worker: workers de River. El relay del outbox (integracion.outbox → suscriptores
// internos), que corre por NOTIFY y cada IRIS_RELAY_INTERVALO, los jobs del conector Argos
// (webhooks entrantes y refresco periódico de zonas y routers), los del conector Horus
// (webhooks entrantes → eventos internos horus.*), el cron de SLA de soporte (cada minuto) y el
// correo de soporte (lectura de buzones IMAP cada minuto, proceso de correos y envío de
// respuestas por SMTP), los tickets que abre Horus (suscriptor de los eventos horus.*) y la cola
// de pendientes hacia Argos (suscriptor de orden.cerrada y su job, cada minuto) y la purga de
// subidas de adjuntos abandonadas (cada hora), la orden de retiro automática (suscriptor de
// argos.servicio_retirado), el mantenimiento de las particiones de posiciones (cada 6 h) y el
// espejo de zonas de Argos en la agenda (cada 10 min) y el alta en Argos de los prospectos
// ganados (suscriptor de integracion.pendiente_hecho y barrido cada 5 min).
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/riverqueue/river"

	"github.com/HCDestroyer/iris/apps/api/internal/agenda"
	"github.com/HCDestroyer/iris/apps/api/internal/campo"
	"github.com/HCDestroyer/iris/apps/api/internal/comercial"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/horus"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/pendientes"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/adjuntos"
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

	// Conector Argos: procesa los webhooks entrantes y refresca zonas y routers.
	acfg := argos.Config{
		BaseURL: cfg.BaseURL, Timeout: cfg.ArgosTimeout, PermitirHTTP: cfg.ConectoresPermitirHTTP, Version: version,
		RedesPermitidas: cfg.ArgosRedesPermitidas,
	}
	if cfg.ClaveCifrado != "" {
		if acfg.Cifrador, err = nucleo.NuevoCifrador(cfg.ClaveCifrado, cfg.ClaveCifradoAnterior); err != nil {
			return err
		}
		nucleo.AvisarClaveAnterior(log, acfg.Cifrador)
	} else {
		log.Warn("IRIS_CLAVE_CIFRADO vacía: los webhooks de Argos no se podrán procesar")
	}
	conectorArgos := argos.Nuevo(acfg)
	// Conector Horus: procesa sus webhooks (eventos internos horus.*; resuelve IPs con Argos).
	conectorHorus := horus.Nuevo(horus.Config{Argos: conectorArgos, RedesPermitidas: cfg.HorusRedesPermitidas, Timeout: cfg.HorusTimeout})
	ccfg, err := soporte.ConfigCorreoDesdeEntorno()
	if err != nil {
		return err
	}
	// Encolar se resuelve al llamar: el cliente de River se arma después (lo necesita el módulo).
	var cliente *river.Client[pgx.Tx]
	// Encuestas: el worker de correo deriva el enlace con la misma clave que la API.
	clavesEnc, err := soporte.ClavesEncuestas(cfg.ClaveCifrado, cfg.ClaveCifradoAnterior)
	if err != nil {
		return err
	}
	sop := soporte.Nuevo(soporte.Config{Cifrador: acfg.Cifrador, Correo: ccfg,
		Encuestas: soporte.ConfigEncuestas{Claves: clavesEnc, BaseURL: cfg.BaseURL},
		Encolar: func(ctx context.Context, tx pgx.Tx, args river.JobArgs) error {
			return integracion.Encolar(cliente)(ctx, tx, args)
		}})
	registrarSoporte := sop.RegistrarTrabajos(pool)
	// Salida por correo: respuestas de agentes en tickets de origen correo.
	sop.SuscribirCorreo(bus, pool)
	// Tickets desde Horus: incidentes maestros por router o servicio de infraestructura y
	// tickets por hallazgo (eventos internos horus.* del conector).
	sop.SuscribirHorus(bus, pool)
	// Acciones en Argos al cerrar una orden (contrato §3): suscriptor de orden.cerrada y cola de
	// pendientes con reintentos (1 min, 5 min, 30 min, 2 h, 12 h).
	pend := pendientes.Nuevo(pendientes.Config{Argos: conectorArgos,
		Encolar: func(ctx context.Context, tx pgx.Tx, args river.JobArgs) error {
			return integracion.Encolar(cliente)(ctx, tx, args)
		}})
	pend.Suscribir(bus)
	// Adjuntos: purga de subidas abandonadas (IRIS_ADJUNTOS_ABANDONO) cada hora. No necesita el
	// almacenamiento: solo marca y borra las partes en la base.
	adj := adjuntos.Nuevo(adjuntos.Config{Abandono: cfg.AdjuntosAbandono})
	// La orden como entidad adjuntable (mismo registro que la API).
	campo.RegistrarAdjuntos()
	// Campo: orden de retiro automática desde argos.servicio_retirado (si el servicio tiene equipos
	// en comodato según Argos) y mantenimiento de las particiones de posiciones (retención). Su
	// agenda refresca el espejo de zonas desde el catálogo de Argos.
	lpos, err := campo.LimitesPosicionesDesdeEntorno()
	if err != nil {
		return err
	}
	cmp := campo.Nuevo(campo.Config{Inventario: campo.InventarioDeArgos(conectorArgos), Posiciones: lpos,
		Encuestas: sop.GenerarEncuestaOrden})
	cmp.SuscribirRetiros(bus, pool, log)
	// Comercial: al quedar hechos los pendientes del alta de un prospecto ganado (POST /clientes,
	// POST /servicios), encola el servicio y crea la orden de instalación; barrido cada 5 min.
	comercial.RegistrarAdjuntos()
	com := comercial.Nuevo(comercial.Config{Altas: comercial.AltasDeArgos(conectorArgos), Campo: cmp,
		Encolar: func(ctx context.Context, tx pgx.Tx, args river.JobArgs) error {
			return integracion.Encolar(cliente)(ctx, tx, args)
		}})
	com.SuscribirAltas(bus, pool, log)

	cliente, err = integracion.NuevoClienteRiver(pool, relay, integracion.OpcionesWorker{
		Esquema: migraciones.EsquemaRiver, RelayIntervalo: cfg.RelayIntervalo, Log: log,
		Registrar: func(w *river.Workers) {
			conectorArgos.RegistrarWorkers(w, pool)
			conectorHorus.RegistrarWorkers(w, pool)
			registrarSoporte(w)
			pend.RegistrarWorkers(w, pool)
			adj.RegistrarWorkers(w, pool)
			nucleo.RegistrarPurgaIdempotencia(w, pool)
			cmp.RegistrarTrabajos(pool)(w)
			cmp.Agenda().RegistrarTrabajos(pool)(w)
			com.RegistrarTrabajos(pool)(w)
		},
		Periodicos: append(argos.TrabajosPeriodicos(0), soporte.TrabajoPeriodicoSLA(time.Minute),
			soporte.TrabajoPeriodicoBuzones(time.Minute), pendientes.TrabajoPeriodico(time.Minute),
			adjuntos.TrabajoPeriodico(time.Hour), campo.TrabajoPeriodicoPosiciones(6*time.Hour),
			agenda.TrabajoPeriodicoZonas(10*time.Minute),
			// Claves de Idempotency-Key vencidas (y las de SHA-256 plano de antes del HMAC).
			nucleo.TrabajoPeriodicoPurgaIdempotencia(time.Hour), comercial.TrabajoPeriodicoAltas(5*time.Minute)),
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
