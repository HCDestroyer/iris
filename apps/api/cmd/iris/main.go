// Comando iris: servidor HTTP de la API y tareas de administración.
//
//	iris                    servidor HTTP (también `iris servir`)
//	iris migrar             migraciones goose + River y sincroniza permisos y roles base
//	iris admin crear --correo admin@isp.gt --nombre "Admin" [--contrasena-stdin]
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/horus"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/config"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/servidor"
	"github.com/HCDestroyer/iris/apps/api/internal/soporte"
	"github.com/HCDestroyer/iris/apps/api/migraciones"
)

var version = "0.0.0-dev"

func main() {
	if err := correr(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "iris:", err)
		os.Exit(1)
	}
}

func correr(args []string) error {
	ctx, cancelar := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancelar()

	cfg, err := config.Cargar()
	if err != nil {
		return err
	}
	cfg.Version = version
	log := cfg.Logger()
	slog.SetDefault(log)

	orden := "servir"
	if len(args) > 0 {
		orden, args = args[0], args[1:]
	}
	switch orden {
	case "servir":
		return servir(ctx, cfg, log)
	case "migrar":
		return migrar(ctx, cfg, log)
	case "admin":
		if len(args) == 0 || args[0] != "crear" {
			return errors.New("uso: iris admin crear --correo <correo> --nombre <nombre> [--contrasena-stdin]")
		}
		return crearAdmin(ctx, cfg, args[1:])
	}
	return fmt.Errorf("orden desconocida %q (servir, migrar, admin crear)", orden)
}

func conectar(ctx context.Context, cfg config.Config) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, cfg.DBURL)
	if err != nil {
		return nil, fmt.Errorf("conectar a Postgres: %w", err)
	}
	return pool, nil
}

func cifrador(cfg config.Config) (*nucleo.Cifrador, error) {
	if cfg.ClaveCifrado == "" {
		return nil, nil
	}
	return nucleo.NuevoCifrador(cfg.ClaveCifrado)
}

func modulo(cfg config.Config) (*identidad.Modulo, error) {
	c, err := cifrador(cfg)
	if err != nil {
		return nil, err
	}
	return identidad.Nuevo(identidad.Config{DuracionSesion: cfg.DuracionSesion, CookieSegura: cfg.CookieSegura, Cifrador: c}), nil
}

// configArgos arma la configuración del conector Argos desde el entorno.
func configArgos(cfg config.Config) (argos.Config, error) {
	c, err := cifrador(cfg)
	if err != nil {
		return argos.Config{}, err
	}
	return argos.Config{
		Cifrador: c, BaseURL: cfg.BaseURL, Timeout: cfg.ArgosTimeout, VentanaWebhook: cfg.WebhookVentana,
		PermitirHTTP: cfg.ConectoresPermitirHTTP, Version: cfg.Version, RedesPermitidas: cfg.ArgosRedesPermitidas,
	}, nil
}

// configHorus arma la configuración del conector Horus (se agrega al núcleo de conectores de
// Argos).
func configHorus(cfg config.Config, a *argos.Modulo) horus.Config {
	return horus.Config{Argos: a, RedesPermitidas: cfg.HorusRedesPermitidas, Timeout: cfg.HorusTimeout}
}

func servir(ctx context.Context, cfg config.Config, log *slog.Logger) error {
	pool, err := conectar(ctx, cfg)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := identidad.SincronizarCatalogo(ctx, pool); err != nil {
		return fmt.Errorf("sincronizar permisos (¿corriste `iris migrar`?): %w", err)
	}
	id, err := modulo(cfg)
	if err != nil {
		return err
	}
	if cfg.ClaveCifrado == "" {
		log.Warn("IRIS_CLAVE_CIFRADO vacía: el MFA (TOTP) y los conectores quedan deshabilitados")
	}
	if cfg.BaseURL == "" {
		log.Warn("IRIS_BASE_URL vacía: no se pueden conectar Argos ni Horus (no hay URL para sus webhooks)")
	}
	acfg, err := configArgos(cfg)
	if err != nil {
		return err
	}
	cola, err := integracion.NuevoClienteInsercion(pool, migraciones.EsquemaRiver, log)
	if err != nil {
		return err
	}
	acfg.Encolar = integracion.Encolar(cola)
	conectorArgos := argos.Nuevo(acfg)
	conectorHorus := horus.Nuevo(configHorus(cfg, conectorArgos))
	handler, _ := servidor.Nuevo(servidor.Deps{
		BD: pool, Log: log, Version: cfg.Version, Identidad: id,
		Idempotencia: nucleo.OpcionesIdempotencia{Reclamo: cfg.IdempotenciaReclamo},
		Modulos: []func(huma.API){
			conectorArgos.Registrar,
			conectorHorus.Registrar,
			soporte.Nuevo(soporte.Config{}).Registrar,
		},
		ProxiesConfiables: cfg.ProxiesConfiables,
	})

	srv := &http.Server{
		Addr:              ":" + strconv.Itoa(cfg.Puerto),
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	errs := make(chan error, 1)
	go func() {
		log.Info("iris escuchando", "puerto", cfg.Puerto, "version", cfg.Version)
		errs <- srv.ListenAndServe()
	}()
	select {
	case err := <-errs:
		return err
	case <-ctx.Done():
	}
	apagar, cancelar := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancelar()
	log.Info("apagando iris")
	return srv.Shutdown(apagar)
}

func migrar(ctx context.Context, cfg config.Config, log *slog.Logger) error {
	pool, err := conectar(ctx, cfg)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := migraciones.Migrar(ctx, pool, log); err != nil {
		return err
	}
	if err := identidad.SincronizarCatalogo(ctx, pool); err != nil {
		return err
	}
	log.Info("base al día")
	return nil
}

func crearAdmin(ctx context.Context, cfg config.Config, args []string) error {
	fs := flag.NewFlagSet("admin crear", flag.ContinueOnError)
	correo := fs.String("correo", "", "correo del administrador")
	nombre := fs.String("nombre", "", "nombre del administrador")
	desdeStdin := fs.Bool("contrasena-stdin", false, "leer la contraseña de la entrada estándar (si no, se genera una)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *correo == "" || *nombre == "" {
		return errors.New("--correo y --nombre son obligatorios")
	}
	contrasena, generada := "", false
	if *desdeStdin {
		linea, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && linea == "" {
			return fmt.Errorf("leer contraseña: %w", err)
		}
		contrasena = strings.TrimRight(linea, "\r\n")
	} else {
		s, err := identidad.NuevoSecreto()
		if err != nil {
			return err
		}
		contrasena, generada = s[:24], true
	}

	pool, err := conectar(ctx, cfg)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := identidad.SincronizarCatalogo(ctx, pool); err != nil {
		return err
	}
	id, err := modulo(cfg)
	if err != nil {
		return err
	}
	cctx := nucleo.ConBD(contexto.Sistema(ctx, contexto.CanalConsola), pool)
	u, err := id.CrearUsuario.Ejecutar(cctx, identidad.EntradaCrearUsuario{
		Nombre: *nombre, Correo: *correo, Contrasena: contrasena, Tipo: "persona",
		Roles: []identidad.AsignacionRol{{Rol: "admin"}},
	})
	if err != nil {
		return err
	}
	fmt.Printf("Administrador creado: id %d, %s\n", u.ID, u.Correo)
	if generada {
		fmt.Printf("Contraseña (no se vuelve a mostrar): %s\n", contrasena)
	}
	return nil
}
