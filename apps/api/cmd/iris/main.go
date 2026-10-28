// Comando iris: servidor HTTP de la API y tareas de administración.
//
//	iris                    servidor HTTP (también `iris servir`)
//	iris migrar             migraciones goose + River y sincroniza permisos y roles base
//	iris salud [--url …] [--timeout 3s]   GET a /api/v1/salud local; sale 0 o 1 (healthcheck)
//	iris clave rotar [--simular] [--ignorar-ilegibles]   re-cifra con IRIS_CLAVE_CIFRADO lo
//	                        cifrado con IRIS_CLAVE_CIFRADO_ANTERIOR (una transacción, auditada)
//	iris admin crear --correo admin@isp.gt --nombre "Admin" [--contrasena-stdin]
//	iris portal acceso --cliente <id> --empresa <id> [--nombre "Ana"]   URL de entrada al portal
//	iris integracion crear --sistema argosisp|horus --empresa 1,2 [--correo …] [--nombre …]
//	                        usuario de integración con su rol base y un token iris_pat_ nuevo
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
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HCDestroyer/iris/apps/api/internal/campo"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/horus"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/pendientes"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/adjuntos"
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

	// El healthcheck solo necesita el puerto: no carga ni exige el resto de la configuración.
	if len(args) > 0 && args[0] == "salud" {
		return salud(ctx, args[1:], os.Stdout)
	}

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
	case "portal":
		if len(args) == 0 || args[0] != "acceso" {
			return errors.New(usoPortal)
		}
		return accesoPortal(ctx, cfg, args[1:])
	case "integracion":
		if len(args) == 0 || args[0] != "crear" {
			return errors.New(usoIntegracion)
		}
		return crearIntegracion(ctx, cfg, args[1:])
	case "clave":
		if len(args) == 0 || args[0] != "rotar" {
			return errors.New(usoClave)
		}
		return rotarClave(ctx, cfg, args[1:], os.Stdout)
	case "campo":
		if len(args) == 0 || args[0] != "semilla" {
			return errors.New(usoSemillaCampo)
		}
		return semillaCampo(ctx, cfg, args[1:])
	}
	return fmt.Errorf("orden desconocida %q (servir, migrar, salud, admin crear, portal acceso, integracion crear, campo semilla, clave rotar)", orden)
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
	// Con IRIS_CLAVE_CIFRADO_ANTERIOR (rotación en curso) descifra con las dos claves.
	return nucleo.NuevoCifrador(cfg.ClaveCifrado, cfg.ClaveCifradoAnterior)
}

func modulo(cfg config.Config) (*identidad.Modulo, error) {
	return moduloConArgos(cfg, nil)
}

// moduloConArgos arma identidad; con el conector Argos, el portal valida en Argos los clientes
// que Iris no conoce al emitir un acceso.
func moduloConArgos(cfg config.Config, a *argos.Modulo) (*identidad.Modulo, error) {
	c, err := cifrador(cfg)
	if err != nil {
		return nil, err
	}
	icfg := identidad.Config{
		DuracionSesion: cfg.DuracionSesion, CookieSegura: cfg.CookieSegura, Cifrador: c,
		BaseURL: cfg.BaseURL, DuracionSesionPortal: cfg.DuracionSesionPortal,
	}
	if a != nil {
		icfg.ValidarCliente = func(ctx context.Context, clienteID int64) (identidad.ValidacionCliente, error) {
			v, err := a.ValidarCliente(ctx, clienteID)
			return identidad.ValidacionCliente{Validado: v.Validado, Existe: v.Existe, EmpresaID: v.EmpresaID, Motivo: v.Motivo}, err
		}
	}
	return identidad.Nuevo(icfg), nil
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
	nucleo.AvisarClaveAnterior(log, acfg.Cifrador)
	cola, err := integracion.NuevoClienteInsercion(pool, migraciones.EsquemaRiver, log)
	if err != nil {
		return err
	}
	acfg.Encolar = integracion.Encolar(cola)
	conectorArgos := argos.Nuevo(acfg)
	id, err := moduloConArgos(cfg, conectorArgos)
	if err != nil {
		return err
	}
	ccfg, err := soporte.ConfigCorreoDesdeEntorno()
	if err != nil {
		return err
	}
	lportal, err := soporte.LimitesPortalDesdeEntorno()
	if err != nil {
		return err
	}
	sop := soporte.Nuevo(soporte.Config{Cifrador: acfg.Cifrador, Correo: ccfg, Portal: lportal, Encolar: integracion.Encolar(cola)})
	conectorHorus := horus.Nuevo(configHorus(cfg, conectorArgos))
	almacen, err := almacenAdjuntos(cfg)
	if err != nil {
		return err
	}
	// La orden acepta adjuntos (campo decide quién ve y sube); el cierre cuenta los completos.
	campo.RegistrarAdjuntos()
	adj := adjuntos.Nuevo(configAdjuntos(cfg, almacen))
	pend := pendientes.Nuevo(pendientes.Config{Argos: conectorArgos, Encolar: integracion.Encolar(cola)})
	lpos, err := campo.LimitesPosicionesDesdeEntorno()
	if err != nil {
		return err
	}
	// Campo programa con su agenda (la misma registra las rutas de agenda).
	cmp := campo.Nuevo(campo.Config{Inventario: campo.InventarioDeArgos(conectorArgos), Posiciones: lpos})
	handler, _ := servidor.Nuevo(servidor.Deps{
		BD: pool, Log: log, Version: cfg.Version, Identidad: id,
		Idempotencia: nucleo.OpcionesIdempotencia{Reclamo: cfg.IdempotenciaReclamo},
		Modulos: []func(huma.API){
			conectorArgos.Registrar,
			conectorHorus.Registrar,
			sop.Registrar,
			adj.RegistrarRutas,
			pend.Registrar,
			cmp.Registrar,
			cmp.Agenda().Registrar,
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

// configAdjuntos arma la configuración del módulo de adjuntos desde el entorno.
func configAdjuntos(cfg config.Config, almacen adjuntos.Almacen) adjuntos.Config {
	return adjuntos.Config{
		Almacen: almacen, TamanoMax: cfg.AdjuntosTamanoMax, MIMEPermitidos: cfg.AdjuntosMIME,
		MaxEnCursoUsuario: cfg.AdjuntosMaxEnCursoUsuario, MaxBytesUsuario: cfg.AdjuntosMaxBytesUsuario,
		MaxEnCursoEntidad: cfg.AdjuntosMaxEnCursoEntidad, Abandono: cfg.AdjuntosAbandono,
	}
}

// almacenAdjuntos arma el almacenamiento de adjuntos: S3 compatible (IRIS_S3_*, MinIO en dev)
// o un directorio local (IRIS_ADJUNTOS_DIR).
func almacenAdjuntos(cfg config.Config) (adjuntos.Almacen, error) {
	if cfg.AdjuntosAlmacen == "s3" {
		return adjuntos.NuevoS3(adjuntos.ConfigS3{
			Endpoint: cfg.S3Endpoint, Region: cfg.S3Region, Bucket: cfg.S3Bucket, AccessKey: cfg.S3AccessKey,
			SecretKey: cfg.S3SecretKey, EstiloRuta: cfg.S3EstiloRuta,
		})
	}
	return adjuntos.NuevoDisco(cfg.AdjuntosDir)
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

const usoPortal = "uso: iris portal acceso --cliente <id de Argos> --empresa <id de Argos> [--nombre <nombre>]"

// accesoPortal emite la URL de entrada al portal de soporte para un cliente de Argos, como lo
// haría Argos con portal.emitir (pruebas locales sin Argos). Solo con acceso a la base: corre
// como actor sistema, por el mismo caso de uso (auditado). La empresa debe estar registrada.
func accesoPortal(ctx context.Context, cfg config.Config, args []string) error {
	fs := flag.NewFlagSet("portal acceso", flag.ContinueOnError)
	cliente := fs.Int64("cliente", 0, "id del cliente en Argos")
	empresa := fs.Int64("empresa", 0, "id de la empresa en Argos (registrada en Iris)")
	nombre := fs.String("nombre", "", "nombre a mostrar en el portal")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *cliente <= 0 || *empresa <= 0 {
		return errors.New(usoPortal)
	}
	pool, err := conectar(ctx, cfg)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := identidad.SincronizarCatalogo(ctx, pool); err != nil {
		return err
	}
	acfg, err := configArgos(cfg)
	if err != nil {
		return err
	}
	id, err := moduloConArgos(cfg, argos.Nuevo(acfg))
	if err != nil {
		return err
	}
	cctx := nucleo.ConBD(contexto.Sistema(ctx, contexto.CanalConsola), pool)
	a, err := id.EmitirAccesoPortal.Ejecutar(cctx, identidad.EntradaEmitirAccesoPortal{ClienteID: *cliente, EmpresaID: *empresa, Nombre: *nombre})
	if err != nil {
		return err
	}
	fmt.Printf("Acceso al portal (un solo uso, vence %s):\n%s\n", a.ExpiraEn.Format(time.RFC3339), a.URL)
	return nil
}

const usoIntegracion = "uso: iris integracion crear --sistema argosisp|horus [--empresa 1,2] [--correo <correo>] [--nombre <nombre>]"

// crearIntegracion crea (o reutiliza) el usuario de tipo integración de Argos u Horus con su rol
// base (integracion_argos, integracion_horus) y sus empresas, y emite un token iris_pat_ nuevo
// para entregar al otro sistema. Solo con acceso a la base (actor sistema), sin pasar por admin.
func crearIntegracion(ctx context.Context, cfg config.Config, args []string) error {
	fs := flag.NewFlagSet("integracion crear", flag.ContinueOnError)
	sistema := fs.String("sistema", "", "argosisp u horus")
	empresas := fs.String("empresa", "", "ids de empresas de Argos separados por coma (obligatorio si hay empresas registradas)")
	correo := fs.String("correo", "", "correo del usuario (por defecto integracion.<sistema>@iris.local)")
	nombre := fs.String("nombre", "", "nombre del usuario")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *sistema == "" {
		return errors.New(usoIntegracion)
	}
	var ids []int64
	for _, parte := range strings.Split(*empresas, ",") {
		if parte = strings.TrimSpace(parte); parte == "" {
			continue
		}
		id, err := strconv.ParseInt(parte, 10, 64)
		if err != nil || id <= 0 {
			return fmt.Errorf("--empresa: %q no es un id", parte)
		}
		ids = append(ids, id)
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
	r, err := id.PrepararIntegracion(cctx, identidad.EntradaPrepararIntegracion{Sistema: *sistema, Empresas: ids, Correo: *correo, Nombre: *nombre})
	if err != nil {
		return err
	}
	estado := "existente"
	if r.Creado {
		estado = "creado"
	}
	fmt.Printf("Usuario de integración %s: id %d, %s, rol %s, empresas %v\n", estado, r.Usuario.ID, r.Usuario.Correo, identidad.RolesIntegracion[*sistema], r.Usuario.Empresas)
	fmt.Printf("Token (no se vuelve a mostrar; entrégalo a %s): %s\n", *sistema, r.Token.Valor)
	return nil
}

const usoSemillaCampo = "uso: iris campo semilla [--correo tecnico@iris.local] [--empresa <id de Argos>] [--ordenes 3]"

// semillaCampo prepara datos de desarrollo para la app de campo (I2): un técnico (rol tecnico;
// se crea si no existe, con una contraseña que se muestra una vez) y órdenes del día
// programadas para él (con ficha de agenda y turnos si no la tenía), por los casos de uso (actor sistema, canal consola, auditado). Con
// empresas registradas, --empresa es obligatoria (el técnico recibe esa empresa).
func semillaCampo(ctx context.Context, cfg config.Config, args []string) error {
	fs := flag.NewFlagSet("campo semilla", flag.ContinueOnError)
	correo := fs.String("correo", "tecnico@iris.local", "correo del técnico")
	empresa := fs.Int64("empresa", 0, "empresa de Argos (obligatoria si hay empresas registradas)")
	cuantas := fs.Int("ordenes", 3, "órdenes del día")
	if err := fs.Parse(args); err != nil {
		return err
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
	var empresaID *int64
	empresas := []int64{}
	if *empresa > 0 {
		empresaID = empresa
		empresas = append(empresas, *empresa)
	}
	var tecnicoID int64
	err = pool.QueryRow(ctx, "SELECT id FROM nucleo.usuarios WHERE lower(correo) = lower($1)", *correo).Scan(&tecnicoID)
	if errors.Is(err, pgx.ErrNoRows) {
		s, err := identidad.NuevoSecreto()
		if err != nil {
			return err
		}
		u, err := id.CrearUsuario.Ejecutar(cctx, identidad.EntradaCrearUsuario{
			Nombre: "Técnico de prueba", Correo: *correo, Contrasena: s[:24], Tipo: "persona",
			Roles: []identidad.AsignacionRol{{Rol: "tecnico"}}, Empresas: empresas,
		})
		if err != nil {
			return err
		}
		tecnicoID = u.ID
		fmt.Printf("Técnico creado: id %d, %s\nContraseña (no se vuelve a mostrar): %s\n", u.ID, u.Correo, s[:24])
	} else if err != nil {
		return err
	} else {
		fmt.Printf("Técnico existente: id %d, %s\n", tecnicoID, *correo)
	}
	ordenes, err := campo.Nuevo(campo.Config{}).SemillaDesarrollo(cctx, campo.OpcionesSemilla{
		TecnicoID: tecnicoID, EmpresaID: empresaID, Ordenes: *cuantas,
	})
	for _, o := range ordenes {
		fmt.Printf("Orden %s (%s) programada %s\n", o.Numero, o.Tipo, o.ProgramadaInicio.Format(time.RFC3339))
	}
	return err
}
