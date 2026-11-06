// Comando mensajeria: el servicio de mensajería de Iris (contrato Iris ↔ ArgosISP 0.6.0 §6), en su
// propio proceso. Atiende /api/v1/mensajeria (la API se lo reenvía con IRIS_MENSAJERIA_URL) y corre
// sus jobs de River en la cola `mensajeria`: envío de avisos (con horario de silencio y límite por
// minuto), webhooks de Meta, webhooks de entrega hacia Argos y mantenimiento. También el chat
// interno del equipo (/api/v1/mensajeria/chat, con su flujo SSE).
//
//	mensajeria              servidor HTTP en IRIS_MENSAJERIA_PUERTO (3510) y sus workers
//	mensajeria salud        GET a /api/v1/salud local; sale 0 o 1 (healthcheck de la imagen)
//
// Usa la misma base de Iris (schema `mensajeria`), la misma identidad (tokens iris_pat_, sesiones y
// ACL) y el mismo outbox y auditoría: las migraciones las aplica `iris migrar`.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"github.com/HCDestroyer/iris/apps/api/internal/agenda"
	"github.com/HCDestroyer/iris/apps/api/internal/campo"
	"github.com/HCDestroyer/iris/apps/api/internal/comercial"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	"github.com/HCDestroyer/iris/apps/api/internal/mensajeria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/adjuntos"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/config"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/servidor"
	"github.com/HCDestroyer/iris/apps/api/internal/soporte"
	"github.com/HCDestroyer/iris/apps/api/migraciones"
)

var version = "0.0.0-dev"

func main() {
	if err := correr(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "mensajeria:", err)
		os.Exit(1)
	}
}

func correr(args []string) error {
	ctx, cancelar := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancelar()
	ent, err := mensajeria.EntornoDesdeVariables()
	if err != nil {
		return err
	}
	if len(args) > 0 && args[0] == "salud" {
		return salud(ctx, ent.Puerto, args[1:], os.Stdout)
	}
	if len(args) > 0 && args[0] != "servir" {
		return fmt.Errorf("orden desconocida %q (servir, salud)", args[0])
	}
	cfg, err := config.Cargar()
	if err != nil {
		return err
	}
	cfg.Version = version
	log := cfg.Logger().With("servicio", "mensajeria")
	slog.SetDefault(log)
	return servir(ctx, cfg, ent, log)
}

func servir(ctx context.Context, cfg config.Config, ent mensajeria.Entorno, log *slog.Logger) error {
	pool, err := pgxpool.New(ctx, cfg.DBURL)
	if err != nil {
		return fmt.Errorf("conectar a Postgres: %w", err)
	}
	defer pool.Close()
	if err := identidad.SincronizarCatalogo(ctx, pool); err != nil {
		return fmt.Errorf("sincronizar permisos (¿corriste `iris migrar`?): %w", err)
	}
	var cif *nucleo.Cifrador
	if cfg.ClaveCifrado != "" {
		if cif, err = nucleo.NuevoCifrador(cfg.ClaveCifrado, cfg.ClaveCifradoAnterior); err != nil {
			return err
		}
		nucleo.AvisarClaveAnterior(log, cif)
	} else {
		log.Warn("IRIS_CLAVE_CIFRADO vacía: no se pueden usar canales de WhatsApp ni firmar webhooks hacia Argos")
	}
	if err := mensajeria.ValidarSimulado(ent.Simulado, cfg.BaseURL); err != nil {
		return err
	}
	if ent.Simulado {
		log.Warn("IRIS_MENSAJERIA_SIMULADO=true: las empresas sin canal usan el canal simulado (nada sale a WhatsApp). Solo desarrollo y CI")
	}
	// El conector Argos de este proceso lee contactos (instantánea, §2.2) y, para la bandeja, busca por
	// teléfono y pide comprobantes y promesas (§7). No recibe webhooks.
	conectorArgos := argos.Nuevo(argos.Config{
		Cifrador: cif, BaseURL: cfg.BaseURL, Timeout: cfg.ArgosTimeout, VentanaWebhook: cfg.WebhookVentana,
		PermitirHTTP: cfg.ConectoresPermitirHTTP, Version: version, RedesPermitidas: cfg.ArgosRedesPermitidas, Origenes: cfg.ArgosOrigenes,
	})

	// El almacenamiento de adjuntos de la API: borrar la foto de un mensaje del chat eliminado.
	// Sin él (mal configurado), la foto solo queda rechazada: no se ve ni se descarga.
	almacen, err := almacenAdjuntos(cfg)
	if err != nil {
		log.Warn("mensajeria: sin almacenamiento de adjuntos; las fotos de mensajes eliminados no se borran del almacén", "err", err)
		almacen = nil
	}
	var cliente *river.Client[pgx.Tx]
	encolar := func(ctx context.Context, tx pgx.Tx, args river.JobArgs) error {
		return integracion.Encolar(cliente)(ctx, tx, args)
	}
	// Bandeja: crear ticket, orden y prospecto desde una conversación reutiliza los casos de soporte,
	// campo y comercial en la misma transacción. Sus jobs (correo, pendientes…) se insertan en sus
	// colas y los trabaja cmd/worker (por eso SkipUnknownJobCheck).
	sop := soporte.Nuevo(soporte.Config{AsegurarArgos: conectorArgos.AsegurarInstantanea, Cifrador: cif, Encolar: encolar})
	cmp := campo.Nuevo(campo.Config{AsegurarArgos: conectorArgos.AsegurarInstantanea, Inventario: campo.InventarioDeArgos(conectorArgos), Soporte: sop,
		Agenda: agenda.Nuevo(agenda.Config{Argos: agenda.TecnicosDeArgos(conectorArgos)})})
	com := comercial.Nuevo(comercial.Config{Planes: comercial.PlanesDeArgos(conectorArgos), Altas: comercial.AltasDeArgos(conectorArgos),
		Campo: cmp, Encolar: encolar})
	m := mensajeria.Nuevo(mensajeria.Config{
		Argos: conectorArgos, Soporte: sop, Campo: cmp, Comercial: com, BaseURL: cfg.BaseURL,
		Almacen: almacen, ChatSSEMax: ent.ChatSSEMax,
		Cifrador: cif, Contactos: conectorArgos, Simulado: ent.Simulado, LimiteDiario: ent.LimiteDiario, PorMinuto: ent.PorMinuto,
		Silencio: ent.Silencio, MetaURL: ent.MetaURL, HTTPArgos: conector.NuevoHTTP(cfg.ArgosRedesPermitidas), Version: version,
		Encolar: encolar,
	})
	workers := river.NewWorkers()
	m.RegistrarWorkers(workers, pool)
	cliente, err = river.NewClient(riverpgxv5.New(pool), &river.Config{
		Schema: migraciones.EsquemaRiver, Logger: log,
		Queues:  map[string]river.QueueConfig{mensajeria.Cola: {MaxWorkers: 5}},
		Workers: workers,
		// Los casos de soporte, campo y comercial que usa la bandeja encolan jobs que trabaja cmd/worker.
		SkipUnknownJobCheck: true,
	})
	if err != nil {
		return err
	}
	if err := cliente.Start(ctx); err != nil {
		return err
	}
	go mensajeria.Reloj(ctx, cliente, log)
	// Chat: LISTEN iris_chat despierta los flujos SSE (GET /chat/eventos) de este proceso.
	go m.EscucharChat(ctx, pool, log)
	// Bandeja: LISTEN iris_conversaciones despierta los flujos SSE (GET /conversaciones/eventos).
	go m.EscucharConversaciones(ctx, pool, log)

	clavesIdem, err := nucleo.ClavesIdempotencia(cfg.ClaveCifrado, cfg.ClaveCifradoAnterior)
	if err != nil {
		return err
	}
	handler, _ := servidor.Nuevo(servidor.Deps{
		BD: pool, Log: log, Version: cfg.Version,
		Idempotencia:      nucleo.OpcionesIdempotencia{Reclamo: cfg.IdempotenciaReclamo, Claves: clavesIdem, Omitir: mensajeria.IdempotenciaPropia},
		Modulos:           []func(huma.API){m.Registrar},
		ProxiesConfiables: cfg.ProxiesConfiables,
	})
	srv := &http.Server{
		Addr: ":" + strconv.Itoa(ent.Puerto), Handler: handler,
		ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 60 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: 120 * time.Second,
	}
	errs := make(chan error, 1)
	go func() {
		log.Info("mensajeria escuchando", "puerto", ent.Puerto, "version", version, "silencio", ent.Silencio.String())
		errs <- srv.ListenAndServe()
	}()
	select {
	case err := <-errs:
		return err
	case <-ctx.Done():
	}
	apagar, cancelarApagado := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancelarApagado()
	log.Info("apagando mensajeria")
	errSrv := srv.Shutdown(apagar)
	return errors.Join(errSrv, cliente.Stop(apagar))
}

// salud: GET a /api/v1/salud del proceso local (healthcheck de la imagen distroless).
func salud(ctx context.Context, puerto int, args []string, salida io.Writer) error {
	fs := flag.NewFlagSet("salud", flag.ContinueOnError)
	fs.SetOutput(salida)
	plazo := fs.Duration("timeout", 3*time.Second, "tiempo máximo de la consulta")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ctx, cancelar := context.WithTimeout(ctx, *plazo)
	defer cancelar()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:"+strconv.Itoa(puerto)+"/api/v1/salud", nil)
	if err != nil {
		return err
	}
	c := &http.Client{Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true}}
	res, err := c.Do(req)
	if err != nil {
		return fmt.Errorf("salud: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("salud: respondió %d", res.StatusCode)
	}
	_, _ = fmt.Fprintln(salida, "ok")
	return nil
}

// almacenAdjuntos: el mismo almacenamiento que la API (disco o S3 compatible).
func almacenAdjuntos(cfg config.Config) (adjuntos.Almacen, error) {
	if cfg.AdjuntosAlmacen == "s3" {
		return adjuntos.NuevoS3(adjuntos.ConfigS3{
			Endpoint: cfg.S3Endpoint, Region: cfg.S3Region, Bucket: cfg.S3Bucket, AccessKey: cfg.S3AccessKey,
			SecretKey: cfg.S3SecretKey, EstiloRuta: cfg.S3EstiloRuta,
		})
	}
	return adjuntos.NuevoDisco(cfg.AdjuntosDir)
}
