// Package argos es el conector de lectura con ArgosISP (contratos/argosisp.md §1–§2):
//
//   - enlace de conexión en los dos sentidos, token atk_… y secreto del webhook cifrados, conector
//     apagado hasta que un administrador lo activa y receptor de webhooks firmados: la base común
//     de internal/integracion/conector (este módulo crea el Nucleo y le agrega Argos);
//   - cliente HTTP de Argos sobre conector.Cliente (timeouts, reintentos con espera en lecturas,
//     problem+json mapeado, protección contra SSRF);
//   - instantáneas de lectura (argos_clientes, argos_servicios, argos_zonas, argos_routers)
//     refrescadas al abrir la ficha (más de 24 h) y por webhook; si Argos no responde se
//     devuelve la última instantánea marcada stale;
//   - procesamiento de los webhooks guardados en integracion.entrantes (job de River).
//
// Cada acción es un caso de uso de internal/auditoria. Las lecturas que llaman a Argos no
// sostienen una transacción abierta durante la llamada HTTP: verifican permiso y conector en un
// caso corto, llaman a Argos y guardan la instantánea y la fila de auditoría en otro.
package argos

import (
	"context"
	"errors"
	"net/netip"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	conectorbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/conector/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Sistema es el valor de integracion.conectores.sistema para Argos.
const Sistema = "argosisp"

// Remoto identifica a Argos en los errores (argos_no_disponible, argos_rechazo…).
var Remoto = conector.Remoto{Codigo: "argos", Nombre: "Argos", VariableRedes: "IRIS_ARGOS_REDES_PERMITIDAS", VariableOrigenes: "IRIS_ARGOS_ORIGENES"}

// Encolador inserta un job de River dentro de la transacción del caso de uso (atómico con lo
// que el caso escribe). En cmd/iris es river.Client.InsertTx; nil = no se encola (OpenAPI).
type Encolador = conector.Encolador

// Config del módulo.
type Config struct {
	// Cifrador para el token y el secreto del webhook. nil = no se puede conectar ni recibir
	// webhooks (422 cifrado_no_disponible).
	Cifrador *nucleo.Cifrador
	// BaseURL pública de Iris (IRIS_BASE_URL): URL del webhook y de los enlaces de conexión.
	BaseURL string
	// RedesPermitidas (IRIS_ARGOS_REDES_PERMITIDAS): redes internas a las que sí se puede
	// conectar (Argos en la red local de dev o lab). Todo lo demás interno se bloquea
	// (conector/red.go).
	// Origenes de los que se acepta un enlace de conexión (IRIS_*_ORIGENES, normalizados).
	Origenes        []string
	RedesPermitidas []netip.Prefix
	// Timeout de cada llamada a Argos (5 s).
	Timeout time.Duration
	// Reintentos de las lecturas idempotentes (3 intentos en total) y espera base (200 ms,
	// se duplica en cada intento).
	Intentos int
	Espera   time.Duration
	// VentanaWebhook: desfase máximo de X-Webhook-Timestamp (5 min); vale para todos los
	// conectores del núcleo.
	VentanaWebhook time.Duration
	// VigenciaInstantanea: edad tras la cual la ficha se refresca al abrirla (24 h, §2.2).
	VigenciaInstantanea time.Duration
	// VigenciaEnlace de los enlaces que genera Iris (15 min, §1.3).
	VigenciaEnlace time.Duration
	// PermitirHTTP acepta base_url http:// (solo desarrollo y pruebas).
	PermitirHTTP bool
	// Encolar los jobs de River (procesar webhooks, sincronizar catálogos).
	Encolar Encolador
	// Version de Iris para el User-Agent.
	Version string
}

// Modulo reúne los casos de uso del conector Argos.
type Modulo struct {
	// verificacion: caché y cupos de la verificación de clientes en la búsqueda.
	verificacion *verificaciones
	cfg          Config
	nucleo       *conector.Nucleo
	conexion     *conector.Conexion

	Conectar             *conector.Conectar
	Previsualizar        *auditoria.Caso[conector.EntradaConectar, conector.Previsualizacion]
	GenerarEnlace        *auditoria.Caso[struct{}, EnlaceGenerado]
	CanjearConexion      *auditoria.Caso[EntradaCanjear, ConexionCanjeada]
	ActivarConector      *auditoria.Caso[EntradaCambioConector, Conector]
	DesactivarConector   *auditoria.Caso[EntradaCambioConector, Conector]
	ListarConectores     *auditoria.Caso[nucleo.ParamsPagina, nucleo.Pagina[Conector]]
	RecibirWebhook       *auditoria.Caso[EntradaWebhook, WebhookRecibido]
	ProcesarEntrante     *ProcesarEntrante
	BuscarClientes       *BuscarClientes
	ObtenerFicha         *ObtenerFicha
	ResolverIP           *ResolverIP
	SincronizarCatalogos *SincronizarCatalogos
}

// Alias de los tipos comunes de conexión (el OpenAPI los publica con estos nombres).
type (
	Conector              = conector.Conector
	EnlaceGenerado        = conector.EnlaceGenerado
	EntradaConectar       = conector.EntradaConectar
	EntradaCanjear        = conector.EntradaCanjear
	ConexionCanjeada      = conector.ConexionCanjeada
	EntradaCambioConector = conector.EntradaCambioConector
	EntradaWebhook        = conector.EntradaWebhook
	WebhookRecibido       = conector.WebhookRecibido
	CuerpoWebhook         = conector.CuerpoWebhook
)

// Nuevo arma el módulo: crea el núcleo de conectores y le agrega Argos.
func Nuevo(cfg Config) *Modulo {
	if cfg.VigenciaInstantanea <= 0 {
		cfg.VigenciaInstantanea = 24 * time.Hour
	}
	m := &Modulo{cfg: cfg, verificacion: nuevasVerificaciones()}
	m.nucleo = conector.Nuevo(conector.Config{
		Cifrador: cfg.Cifrador, BaseURL: cfg.BaseURL, VentanaWebhook: cfg.VentanaWebhook, VigenciaEnlace: cfg.VigenciaEnlace,
		PermitirHTTP: cfg.PermitirHTTP, Encolar: cfg.Encolar, Version: cfg.Version,
	})
	m.conexion = m.nucleo.Agregar(conector.Sistema{
		Nombre: Sistema, Remoto: Remoto, PrefijoToken: "atk_", RutaCanje: "/integraciones/iris/conexion/canjear",
		RegistrarEmpresas: true, RedesPermitidas: cfg.RedesPermitidas, Origenes: cfg.Origenes, Etiqueta: "Argos",
		Timeout: cfg.Timeout, Intentos: cfg.Intentos, Espera: cfg.Espera,
		// Primera carga de zonas y routers, en la transacción de la activación.
		AlActivar: func(ctx context.Context, tx pgx.Tx) error {
			if cfg.Encolar == nil {
				return nil
			}
			return cfg.Encolar(ctx, tx, ArgsSincronizarCatalogos{Motivo: MotivoActivacion})
		},
		TrabajoEntrante: func(id int64) river.JobArgs { return ArgsProcesarEntrante{EntranteID: id} },
	})
	m.Conectar = m.conexion.Conectar
	m.Previsualizar = m.conexion.Previsualizar
	m.GenerarEnlace = m.conexion.GenerarEnlace
	m.CanjearConexion = m.conexion.CanjearConexion
	m.RecibirWebhook = m.conexion.RecibirWebhook
	m.ActivarConector = m.nucleo.ActivarConector
	m.DesactivarConector = m.nucleo.DesactivarConector
	m.ListarConectores = m.nucleo.ListarConectores
	m.ProcesarEntrante = nuevoProcesarEntrante(m)
	m.BuscarClientes = nuevoBuscarClientes(m)
	m.ObtenerFicha = nuevoObtenerFicha(m)
	m.ResolverIP = nuevoResolverIP(m)
	m.SincronizarCatalogos = nuevoSincronizarCatalogos(m)
	return m
}

// Config devuelve la configuración.
func (m *Modulo) Config() Config { return m.cfg }

// Nucleo devuelve el núcleo de conectores (Horus se agrega a él: horus.Nuevo).
func (m *Modulo) Nucleo() *conector.Nucleo { return m.nucleo }

// clienteDe arma el cliente HTTP de Argos de un conector leído (token descifrado).
func (m *Modulo) clienteDe(c conector.Leido) *Cliente {
	return &Cliente{Cliente: m.conexion.Cliente(c)}
}

// conectorActivo devuelve el conector Argos activo o 503 conector_apagado.
func (m *Modulo) conectorActivo(ctx context.Context, q conector.DBTX) (conector.Leido, error) {
	return m.conexion.Activo(ctx, q)
}

func soloLecturas[Out any](Out) bool { return false }

// RechazosSinAuditar devuelve cuántos 401 anónimos (webhooks y canjes de cualquier conector) se
// rechazaron sin auditar desde el arranque.
func (m *Modulo) RechazosSinAuditar() int64 { return m.nucleo.RechazosSinAuditar() }

// ClienteActivo devuelve el cliente HTTP (con la protección contra SSRF) del conector Argos
// activo, o 503 conector_apagado. Lo usa la cola de pendientes para escribir en Argos.
func (m *Modulo) ClienteActivo(ctx context.Context, q conector.DBTX) (*Cliente, error) {
	c, err := m.conectorActivo(ctx, q)
	if err != nil {
		return nil, err
	}
	return m.clienteDe(c), nil
}

// HayConectorActivo indica si hay un conector Argos activo (sin descifrar su token).
func (m *Modulo) HayConectorActivo(ctx context.Context, q conector.DBTX) (bool, error) {
	_, err := conectorbd.New(q).ConectorActivo(ctx, Sistema)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// HayConectorRegistrado indica si alguna vez se conectó Argos (hay un conector, activo o
// apagado). Sin ninguno, Iris está en modo «sin conector» y no encola acciones hacia Argos.
func (m *Modulo) HayConectorRegistrado(ctx context.Context, q conector.DBTX) (bool, error) {
	var hay bool
	err := q.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM integracion.conectores WHERE sistema = $1)", Sistema).Scan(&hay)
	return hay, err
}
