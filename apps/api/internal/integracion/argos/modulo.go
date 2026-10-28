// Package argos es el conector de lectura con ArgosISP (contratos/argosisp.md §1–§2):
//
//   - enlace de conexión en los dos sentidos (canje servidor a servidor), con el token atk_… y el
//     secreto del webhook cifrados (IRIS_CLAVE_CIFRADO) y el conector apagado hasta que un
//     administrador lo activa;
//   - cliente HTTP de Argos (timeouts, reintentos con espera en lecturas, problem+json mapeado);
//   - instantáneas de lectura (argos_clientes, argos_servicios, argos_zonas, argos_routers)
//     refrescadas al abrir la ficha (más de 24 h) y por webhook; si Argos no responde se
//     devuelve la última instantánea marcada stale;
//   - receptor de webhooks firmados: se guardan en integracion.entrantes (deduplicados por
//     X-Webhook-Id) y se procesan en un job de River.
//
// Cada acción es un caso de uso de internal/auditoria. Las lecturas que llaman a Argos no
// sostienen una transacción abierta durante la llamada HTTP: verifican permiso y conector en un
// caso corto, llaman a Argos y guardan la instantánea y la fila de auditoría en otro.
package argos

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Sistema es el valor de integracion.conectores.sistema para Argos.
const Sistema = "argosisp"

// Encolador inserta un job de River dentro de la transacción del caso de uso (atómico con lo
// que el caso escribe). En cmd/iris es river.Client.InsertTx; nil = no se encola (OpenAPI).
type Encolador func(ctx context.Context, tx pgx.Tx, args river.JobArgs) error

// Config del módulo.
type Config struct {
	// Cifrador para el token y el secreto del webhook. nil = no se puede conectar ni recibir
	// webhooks (422 cifrado_no_disponible).
	Cifrador *nucleo.Cifrador
	// BaseURL pública de Iris (IRIS_BASE_URL): URL del webhook y de los enlaces de conexión.
	BaseURL string
	// HTTP para llamar a Argos (nil = uno propio). Las pruebas pasan el del servidor falso.
	HTTP *http.Client
	// Timeout de cada llamada a Argos (5 s).
	Timeout time.Duration
	// Reintentos de las lecturas idempotentes (3 intentos en total) y espera base (200 ms,
	// se duplica en cada intento).
	Intentos int
	Espera   time.Duration
	// VentanaWebhook: desfase máximo de X-Webhook-Timestamp (5 min).
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
	cfg Config
	// rechazos cuenta los 401 anónimos (webhook y canje) que no se auditan.
	rechazos atomic.Int64

	Conectar             *Conectar
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

// Nuevo arma el módulo.
func Nuevo(cfg Config) *Modulo {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 5 * time.Second
	}
	if cfg.Intentos <= 0 {
		cfg.Intentos = 3
	}
	if cfg.Espera <= 0 {
		cfg.Espera = 200 * time.Millisecond
	}
	if cfg.VentanaWebhook <= 0 {
		cfg.VentanaWebhook = 5 * time.Minute
	}
	if cfg.VigenciaInstantanea <= 0 {
		cfg.VigenciaInstantanea = 24 * time.Hour
	}
	if cfg.VigenciaEnlace <= 0 {
		cfg.VigenciaEnlace = 15 * time.Minute
	}
	if cfg.HTTP == nil {
		cfg.HTTP = &http.Client{
			// Sin seguir redirecciones: el token no debe viajar a otro host.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		}
	}
	m := &Modulo{cfg: cfg}
	m.Conectar = nuevoConectar(m)
	m.GenerarEnlace = nuevoGenerarEnlace(m)
	m.CanjearConexion = nuevoCanjearConexion(m)
	m.ActivarConector = nuevoActivarConector(m)
	m.DesactivarConector = nuevoDesactivarConector(m)
	m.ListarConectores = nuevoListarConectores(m)
	m.RecibirWebhook = nuevoRecibirWebhook(m)
	m.ProcesarEntrante = nuevoProcesarEntrante(m)
	m.BuscarClientes = nuevoBuscarClientes(m)
	m.ObtenerFicha = nuevoObtenerFicha(m)
	m.ResolverIP = nuevoResolverIP(m)
	m.SincronizarCatalogos = nuevoSincronizarCatalogos(m)
	return m
}

// Config devuelve la configuración.
func (m *Modulo) Config() Config { return m.cfg }

// clienteDe arma el cliente HTTP de Argos de un conector (descifra el token).
func (m *Modulo) clienteDe(c conectorLeido) *Cliente {
	return NuevoCliente(c.BaseUrl, c.token, OpcionesCliente{
		HTTP: m.cfg.HTTP, Timeout: m.cfg.Timeout, Intentos: m.cfg.Intentos, Espera: m.cfg.Espera,
		UserAgent: "Iris/" + m.cfg.Version,
	})
}

func soloLecturas[Out any](Out) bool { return false }

// auditarRechazo es el AuditarError de los endpoints públicos (webhook, canje): un 401 (firma
// inválida o vencida, código inválido) lo provoca cualquiera sin credenciales, así que no deja
// fila en auditoria.acciones (se podría inundar la tabla). Se cuenta (RechazosSinAuditar) y se
// registra en el log con muestreo: el primero y luego uno de cada 100. Lo demás se audita.
func (m *Modulo) auditarRechazo(accion string) func(error) bool {
	return func(err error) bool {
		p, _ := nucleo.AProblema(err)
		if p.Status != http.StatusUnauthorized {
			return true
		}
		if n := m.rechazos.Add(1); n == 1 || n%100 == 0 {
			slog.Warn("argos: rechazos anónimos sin auditar", "accion", accion, "codigo", p.Codigo(), "total", n)
		}
		return false
	}
}

// RechazosSinAuditar devuelve cuántos 401 anónimos se rechazaron sin auditar desde el arranque.
func (m *Modulo) RechazosSinAuditar() int64 { return m.rechazos.Load() }

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// actorArgos es el actor con que se auditan el canje y los webhooks de un conector.
func actorArgos(conectorID int64) contexto.Actor {
	return contexto.Actor{Tipo: contexto.ActorIntegracion, Nombre: "argosisp (conector " + itoa(conectorID) + ")"}
}
