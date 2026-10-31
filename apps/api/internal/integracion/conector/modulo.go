// Package conector es la base común de los conectores de Iris con otros sistemas (ArgosISP y
// Horus Flow), con los mecanismos del contrato Argos ↔ Horus §1 que reutilizan los dos:
//
//   - enlace de conexión en los dos sentidos (canje servidor a servidor), con el token del otro
//     sistema y el secreto del webhook cifrados (IRIS_CLAVE_CIFRADO, atados a la fila) y el
//     conector apagado hasta que un administrador lo activa; uno activo por sistema;
//   - receptor de webhooks firmados (HMAC-SHA256 en tiempo constante sobre el cuerpo crudo,
//     firma antes que ventana, id firmado obligatorio y deduplicación por él): se guardan en
//     integracion.entrantes y se procesan en un job de River del sistema;
//   - cliente HTTP con protección contra SSRF (red.go) y errores problem+json mapeados;
//   - activar, desactivar, listar, URL pública por conector y enlaces «Ver en …».
//
// Cada sistema se agrega al Nucleo con su Sistema (nombre, prefijo del token, rutas, ganchos) y
// recibe una Conexion con sus casos de uso. El Nucleo lo crea el conector Argos
// (argos.Modulo.Nucleo) y Horus se agrega a ese mismo (horus.Nuevo).
package conector

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	conectorbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/conector/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Encolador inserta un job de River dentro de la transacción del caso de uso (atómico con lo
// que el caso escribe). En cmd/iris es river.Client.InsertTx; nil = no se encola (OpenAPI).
type Encolador func(ctx context.Context, tx pgx.Tx, args river.JobArgs) error

// Config común de los conectores.
type Config struct {
	// Cifrador para el token y el secreto del webhook. nil = no se puede conectar ni recibir
	// webhooks (422 cifrado_no_disponible).
	Cifrador *nucleo.Cifrador
	// BaseURL pública de Iris (IRIS_BASE_URL): URL de los webhooks y de los enlaces de conexión.
	BaseURL string
	// VentanaWebhook: desfase máximo de X-Webhook-Timestamp (5 min).
	VentanaWebhook time.Duration
	// VigenciaEnlace de los enlaces que genera Iris (15 min).
	VigenciaEnlace time.Duration
	// PermitirHTTP acepta base_url http:// (solo desarrollo y pruebas).
	PermitirHTTP bool
	// Encolar los jobs de River (procesar webhooks, primera carga al activar).
	Encolar Encolador
	// Version de Iris para el User-Agent.
	Version string
}

// Sistema describe un conector concreto.
type Sistema struct {
	// Nombre es integracion.conectores.sistema y el segmento de las rutas:
	// /api/v1/integraciones/<Nombre>/…  ("argosisp", "horus").
	Nombre string
	// Remoto: prefijo de los códigos de error y nombre en los mensajes.
	Remoto Remoto
	// PrefijoToken del token que entrega el otro sistema ("atk_", "hf_pat_").
	PrefijoToken string
	// RutaCanje del canje en el otro sistema, bajo /api/v1 (enlace generado allá).
	RutaCanje string
	// RutaAutorizar: página del panel del otro sistema que aprueba «Conectar con un botón»
	// (contrato Iris ↔ ArgosISP 0.4.0 §1.4: /integraciones/iris/autorizar). Vacía = el sistema no
	// lo ofrece y no se publican sus rutas.
	RutaAutorizar string
	// RegistrarEmpresas: las empresas que informa el canje se registran en nucleo.empresas
	// (solo Argos: los ids de empresa son los suyos).
	RegistrarEmpresas bool
	// RedesPermitidas: redes internas a las que sí se puede llamar (dev y lab).
	RedesPermitidas []netip.Prefix
	// Origenes de los que se acepta un enlace de conexión (https://host[:puerto], normalizados con
	// nucleo.NormalizarOrigen; IRIS_ARGOS_ORIGENES, IRIS_HORUS_ORIGENES). Obligatoria también con
	// PermitirHTTP; fuera de ella solo vale el origen del conector activo del sistema.
	Origenes []string
	// Timeout de cada llamada (5 s), reintentos de las lecturas (3) y espera base (200 ms).
	Timeout  time.Duration
	Intentos int
	Espera   time.Duration
	// Etiqueta del OperationID de sus rutas: conectar<Etiqueta>, webhook<Etiqueta>…
	Etiqueta string
	// AlActivar corre en la transacción de integraciones.activar (p. ej. encolar la primera
	// carga de catálogos e informar el contrato). Opcional.
	AlActivar func(ctx context.Context, tx pgx.Tx, conectorID int64) error
	// Contrato que Iris habla con el sistema («0.5.0» con Argos); va en el canje (`contrato`) y en
	// PUT /integraciones/iris/contrato. Vacío = no se informa.
	Contrato string
	// Capacidades del conector según la versión que informó el otro sistema (p. ej.
	// inventario_por_tecnico con Argos ≥ 1.6.0). Opcional.
	Capacidades func(version, contrato *string) []string
	// TrabajoEntrante arma el job que procesa un webhook guardado. Obligatorio.
	TrabajoEntrante func(entranteID int64) river.JobArgs
}

// Nucleo reúne los sistemas y los casos de uso comunes (por id de conector).
type Nucleo struct {
	cfg Config
	// rechazos cuenta los 401 anónimos (webhooks y canjes) que no se auditan.
	rechazos atomic.Int64

	mu       sync.RWMutex
	sistemas map[string]*Conexion
	tokens   Tokens

	ListarConectores   *auditoria.Caso[nucleo.ParamsPagina, nucleo.Pagina[Conector]]
	ActivarConector    *auditoria.Caso[EntradaCambioConector, Conector]
	DesactivarConector *auditoria.Caso[EntradaCambioConector, Conector]
	CambiarURLPublica  *auditoria.Caso[EntradaURLPublica, Conector]
	ObtenerEnlaces     *auditoria.Caso[struct{}, EnlacesExternos]
}

// Tokens emite, confirma y revoca el iris_pat_ que Iris entrega al otro sistema en el canje
// (contratos §1.2). Lo implementa identidad.Modulo; cmd/iris lo conecta con FijarTokens (el
// núcleo de conectores no depende de identidad).
type Tokens interface {
	// EmitirTokenCanje: token del usuario de integración del sistema, con vencimiento (expira).
	EmitirTokenCanje(ctx context.Context, sistema string, expira time.Time) (id int64, valor string, err error)
	// ConfirmarTokenCanje le quita el vencimiento al activar el conector (422 si ya venció).
	ConfirmarTokenCanje(ctx context.Context, id int64) error
	// RevocarTokenIntegracion lo revoca (revocar uno ya revocado no es error).
	RevocarTokenIntegracion(ctx context.Context, id int64, motivo string) error
}

// VigenciaTokenCanje: el iris_pat_ entregado muere solo si el conector no se activa en este plazo.
const VigenciaTokenCanje = 15 * time.Minute

// FijarTokens conecta el emisor de tokens (cmd/iris y cmd/worker, después de armar identidad).
// Sin él, conectar con un enlace del otro sistema responde 422 emisor_token_no_configurado.
func (n *Nucleo) FijarTokens(t Tokens) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.tokens = t
}

func (n *Nucleo) tokensFijados() Tokens {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.tokens
}

// Nuevo arma el núcleo de conectores.
func Nuevo(cfg Config) *Nucleo {
	if cfg.VentanaWebhook <= 0 {
		cfg.VentanaWebhook = 5 * time.Minute
	}
	if cfg.VigenciaEnlace <= 0 {
		cfg.VigenciaEnlace = 15 * time.Minute
	}
	n := &Nucleo{cfg: cfg, sistemas: map[string]*Conexion{}}
	n.ListarConectores = nuevoListarConectores(n)
	n.ActivarConector = nuevoActivarConector(n)
	n.DesactivarConector = nuevoDesactivarConector(n)
	n.CambiarURLPublica = nuevoCambiarURLPublica(n)
	n.ObtenerEnlaces = nuevoObtenerEnlaces(n)
	return n
}

// Config devuelve la configuración común.
func (n *Nucleo) Config() Config { return n.cfg }

// Conexion es un sistema agregado al núcleo, con sus casos de uso de conexión y webhook.
type Conexion struct {
	n    *Nucleo
	s    Sistema
	http *http.Client

	Conectar        *Conectar
	Previsualizar   *auditoria.Caso[EntradaConectar, Previsualizacion]
	GenerarEnlace   *auditoria.Caso[struct{}, EnlaceGenerado]
	CanjearConexion *auditoria.Caso[EntradaCanjear, ConexionCanjeada]
	RecibirWebhook  *auditoria.Caso[EntradaWebhook, WebhookRecibido]
	// «Conectar con un botón» (§1.4); nil si el sistema no tiene RutaAutorizar.
	Autorizar             *auditoria.Caso[EntradaAutorizar, AutorizacionIniciada]
	CompletarAutorizacion *CompletarAutorizacion
}

// Agregar suma un sistema al núcleo. Entra en pánico si está mal definido o repetido (se
// descubre al arrancar).
func (n *Nucleo) Agregar(s Sistema) *Conexion {
	if s.Nombre == "" || s.PrefijoToken == "" || s.RutaCanje == "" || s.TrabajoEntrante == nil || s.Remoto.Codigo == "" {
		panic("conector.Agregar: el sistema necesita Nombre, Remoto, PrefijoToken, RutaCanje y TrabajoEntrante")
	}
	if s.Etiqueta == "" {
		s.Etiqueta = s.Remoto.Nombre
	}
	// Orígenes normalizados como los compara exigirOrigen.
	for i, o := range s.Origenes {
		if n, err := nucleo.NormalizarOrigen(o); err == nil {
			s.Origenes[i] = n
		}
	}
	if s.Timeout <= 0 {
		s.Timeout = 5 * time.Second
	}
	if s.Intentos <= 0 {
		s.Intentos = 3
	}
	if s.Espera <= 0 {
		s.Espera = 200 * time.Millisecond
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if _, ok := n.sistemas[s.Nombre]; ok {
		panic("conector.Agregar: sistema repetido " + s.Nombre)
	}
	c := &Conexion{n: n, s: s, http: NuevoHTTP(s.RedesPermitidas)}
	c.Conectar = nuevoConectar(c)
	c.Previsualizar = nuevoPrevisualizar(c)
	c.GenerarEnlace = nuevoGenerarEnlace(c)
	c.CanjearConexion = nuevoCanjearConexion(c)
	c.RecibirWebhook = nuevoRecibirWebhook(c)
	if s.RutaAutorizar != "" {
		c.Autorizar = nuevoAutorizar(c)
		c.CompletarAutorizacion = nuevoCompletarAutorizacion(c)
	}
	n.sistemas[s.Nombre] = c
	return c
}

// sistema devuelve la conexión de un sistema (nil si no se agregó).
func (n *Nucleo) sistema(nombre string) *Conexion {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.sistemas[nombre]
}

// Sistema devuelve la definición del sistema.
func (c *Conexion) Sistema() Sistema { return c.s }

// Nucleo devuelve el núcleo al que pertenece.
func (c *Conexion) Nucleo() *Nucleo { return c.n }

// Cliente arma el cliente HTTP del conector leído (descifrado), con la protección contra SSRF.
func (c *Conexion) Cliente(l Leido) *Cliente {
	return c.clienteHacia(l.BaseUrl, l.token)
}

func (c *Conexion) clienteHacia(base, token string) *Cliente {
	return NuevoCliente(c.s.Remoto, base, token, OpcionesCliente{
		HTTP: c.http, Timeout: c.s.Timeout, Intentos: c.s.Intentos, Espera: c.s.Espera,
		UserAgent: "Iris/" + c.n.cfg.Version,
	})
}

// RechazosSinAuditar devuelve cuántos 401 anónimos se rechazaron sin auditar desde el arranque.
func (n *Nucleo) RechazosSinAuditar() int64 { return n.rechazos.Load() }

// auditarRechazo es el AuditarError de los endpoints públicos (webhook, canje): un 401 (firma
// inválida o vencida, código inválido) lo provoca cualquiera sin credenciales, así que no deja
// fila en auditoria.acciones (se podría inundar la tabla). Se cuenta (RechazosSinAuditar) y se
// registra en el log con muestreo: el primero y luego uno de cada 100. Lo demás se audita.
func (n *Nucleo) auditarRechazo(accion string) func(error) bool {
	return func(err error) bool {
		p, _ := nucleo.AProblema(err)
		if p.Status != http.StatusUnauthorized {
			return true
		}
		if total := n.rechazos.Add(1); total == 1 || total%100 == 0 {
			slog.Warn("conector: rechazos anónimos sin auditar", "accion", accion, "codigo", p.Codigo(), "total", total)
		}
		return false
	}
}

// Leido es un conector leído y descifrado (solo dentro del proceso; nunca se serializa).
type Leido struct {
	conectorbd.IntegracionConectore
	token   string
	secreto string
}

// Token devuelve el token del otro sistema (atk_…, hf_pat_…).
func (l Leido) Token() string { return l.token }

// DatosAsociados ata un valor cifrado del conector a su fila y campo ("token",
// "secreto_webhook"). También lo usa la rotación de la clave (internal/nucleo/clave).
func DatosAsociados(id int64, campo string) []byte {
	return fmt.Appendf(nil, "conector:%d:%s", id, campo)
}

func (n *Nucleo) cifrar(id int64, campo, valor string) ([]byte, error) {
	if n.cfg.Cifrador == nil {
		return nil, SinCifrado()
	}
	return n.cfg.Cifrador.Cifrar([]byte(valor), DatosAsociados(id, campo))
}

// Leer descifra el token y el secreto de un conector.
func (n *Nucleo) Leer(f conectorbd.IntegracionConectore) (Leido, error) {
	if n.cfg.Cifrador == nil {
		return Leido{}, SinCifrado()
	}
	token, err := n.cfg.Cifrador.Descifrar(f.TokenCifrado, DatosAsociados(f.ID, "token"))
	if err != nil {
		return Leido{}, fmt.Errorf("conector %d: descifrar token: %w", f.ID, err)
	}
	secreto, err := n.cfg.Cifrador.Descifrar(f.SecretoWebhookCifrado, DatosAsociados(f.ID, "secreto_webhook"))
	if err != nil {
		return Leido{}, fmt.Errorf("conector %d: descifrar secreto: %w", f.ID, err)
	}
	return Leido{IntegracionConectore: f, token: string(token), secreto: string(secreto)}, nil
}

// Activo devuelve el conector activo del sistema, descifrado, o 503 conector_apagado. q puede
// ser una transacción o el pool.
func (c *Conexion) Activo(ctx context.Context, q conectorbd.DBTX) (Leido, error) {
	f, err := conectorbd.New(q).ConectorActivo(ctx, c.s.Nombre)
	if err != nil {
		if esSinFilas(err) {
			return Leido{}, c.Apagado()
		}
		return Leido{}, err
	}
	return c.n.Leer(f)
}

// PorID devuelve el conector del sistema con ese id, descifrado; debe estar activo (503 si no).
func (c *Conexion) PorID(ctx context.Context, q conectorbd.DBTX, id int64) (Leido, error) {
	f, err := conectorbd.New(q).ConectorPorID(ctx, id)
	if err != nil {
		return Leido{}, err
	}
	if f.Sistema != c.s.Nombre || f.Estado != "activo" {
		return Leido{}, c.Apagado()
	}
	return c.n.Leer(f)
}

// Apagado: no hay conector activo del sistema; Iris trabaja con datos locales.
func (c *Conexion) Apagado() *nucleo.Problema {
	return ConectorApagado(c.s.Remoto.Nombre)
}

// ConectorApagado es el 503 conector_apagado de un sistema («Argos», «Horus»).
func ConectorApagado(nombre string) *nucleo.Problema {
	sistema := nombre
	if nombre == "Argos" {
		sistema = "ArgosISP"
	}
	return nucleo.NuevoProblema(http.StatusServiceUnavailable, "conector_apagado", "Conector con "+nombre+" apagado",
		"No hay un conector con "+sistema+" activo; trabaja con los datos capturados en Iris")
}

// SinCifrado: falta IRIS_CLAVE_CIFRADO.
func SinCifrado() *nucleo.Problema {
	return nucleo.ReglaNegocio("cifrado_no_disponible", "Los conectores necesitan IRIS_CLAVE_CIFRADO para guardar el token cifrado")
}

// ActorDe es el actor con que se auditan el canje y los webhooks de un conector.
func ActorDe(sistema string, conectorID int64) contexto.Actor {
	return contexto.Actor{Tipo: contexto.ActorIntegracion, Nombre: sistema + " (conector " + strconv.FormatInt(conectorID, 10) + ")"}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func soloLecturas[Out any](Out) bool { return false }

// DBTX es una transacción o el pool (para leer conectores fuera de un caso de uso).
type DBTX = conectorbd.DBTX

// Fila lee un conector del sistema sin descifrarlo (para datos públicos: URL base y pública).
// pgx.ErrNoRows si no existe o es de otro sistema.
func (c *Conexion) Fila(ctx context.Context, q DBTX, id int64) (conectorbd.IntegracionConectore, error) {
	f, err := conectorbd.New(q).ConectorPorID(ctx, id)
	if err == nil && f.Sistema != c.s.Nombre {
		return conectorbd.IntegracionConectore{}, pgx.ErrNoRows
	}
	return f, err
}

// URLPublica es el origen de la interfaz del sistema del conector (la configurada o la base).
func URLPublica(f conectorbd.IntegracionConectore) string { return urlPublica(f) }
