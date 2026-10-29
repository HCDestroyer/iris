// Package horus es el conector con Horus Flow (contratos/horus.md):
//
//   - conexión con el mismo mecanismo que Argos (internal/integracion/conector): enlace en los
//     dos sentidos bajo /api/v1/integraciones/horus, token hf_pat_… y secreto del webhook
//     cifrados, apagado hasta activar, uno activo por sistema, permisos integraciones.*;
//   - webhooks firmados de §2 (POST /api/v1/integraciones/horus/webhook/{conector_id}) con las
//     mismas garantías que los de Argos; el job valida y normaliza el payload y lo publica como
//     evento interno por el outbox (horus.router_estado_cambiado, horus.servicio_infra_cambiado,
//     horus.hallazgo_abierto, horus.hallazgo_resuelto). No crea tickets: eso es de soporte
//     (I2), que se suscribe a esos eventos. Para un hallazgo con IP, resuelve el cliente con el
//     conector Argos (argos.Modulo.ResolverIPInterno);
//   - cliente HTTP de Horus (salud del servicio del cliente y estado del router, §1.1) detrás de
//     la misma protección contra SSRF.
package horus

import (
	"net/netip"
	"time"

	"github.com/riverqueue/river"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
)

// Sistema es el valor de integracion.conectores.sistema para Horus.
const Sistema = "horus"

// Remoto identifica a Horus en los errores (horus_no_disponible, horus_rechazo…).
var Remoto = conector.Remoto{Codigo: "horus", Nombre: "Horus", VariableRedes: "IRIS_HORUS_REDES_PERMITIDAS", VariableOrigenes: "IRIS_HORUS_ORIGENES"}

// Rutas de la API de Horus que usa Iris (bajo /api/v1). Supuesto, a confirmar con el equipo de
// Horus: el prefijo del conector Iris en Horus es /api/v1/integrations/iris (contrato §0) y el
// canje y las lecturas cuelgan de él con los nombres del contrato Argos ↔ Iris.
const (
	RutaCanje         = "/integrations/iris/conexion/canjear"
	rutaSaludServicio = "/integrations/iris/servicios/%d/salud"
	rutaRouter        = "/integrations/iris/routers/%d"
)

// Config del módulo.
type Config struct {
	// Argos es el conector Argos: Horus se agrega a su núcleo de conectores y le pide la
	// resolución de IPs. Obligatorio.
	Argos *argos.Modulo
	// RedesPermitidas (IRIS_HORUS_REDES_PERMITIDAS): redes internas a las que sí se puede
	// conectar (Horus en la red local de dev o lab).
	// Origenes de los que se acepta un enlace de conexión (IRIS_*_ORIGENES, normalizados).
	Origenes        []string
	RedesPermitidas []netip.Prefix
	// Timeout de cada llamada a Horus (5 s), reintentos de las lecturas (3) y espera base.
	Timeout  time.Duration
	Intentos int
	Espera   time.Duration
}

// Modulo reúne los casos de uso del conector Horus.
type Modulo struct {
	cfg      Config
	argos    *argos.Modulo
	conexion *conector.Conexion

	Conectar               *conector.Conectar
	Previsualizar          *auditoria.Caso[conector.EntradaConectar, conector.Previsualizacion]
	GenerarEnlace          *auditoria.Caso[struct{}, conector.EnlaceGenerado]
	CanjearConexion        *auditoria.Caso[conector.EntradaCanjear, conector.ConexionCanjeada]
	RecibirWebhook         *auditoria.Caso[conector.EntradaWebhook, conector.WebhookRecibido]
	ProcesarEntrante       *ProcesarEntrante
	ConsultarSaludServicio *ConsultarSaludServicio
	ConsultarRouter        *ConsultarRouter
}

// Nuevo arma el módulo y agrega Horus al núcleo de conectores de cfg.Argos.
func Nuevo(cfg Config) *Modulo {
	if cfg.Argos == nil {
		panic("horus.Nuevo: falta Config.Argos (núcleo de conectores y resolución de IP)")
	}
	m := &Modulo{cfg: cfg, argos: cfg.Argos}
	m.conexion = cfg.Argos.Nucleo().Agregar(conector.Sistema{
		Nombre: Sistema, Remoto: Remoto, PrefijoToken: "hf_pat_", RutaCanje: RutaCanje, Etiqueta: "Horus",
		RedesPermitidas: cfg.RedesPermitidas, Origenes: cfg.Origenes, Timeout: cfg.Timeout, Intentos: cfg.Intentos, Espera: cfg.Espera,
		TrabajoEntrante: func(id int64) river.JobArgs { return ArgsProcesarEntrante{EntranteID: id} },
	})
	m.Conectar = m.conexion.Conectar
	m.Previsualizar = m.conexion.Previsualizar
	m.GenerarEnlace = m.conexion.GenerarEnlace
	m.CanjearConexion = m.conexion.CanjearConexion
	m.RecibirWebhook = m.conexion.RecibirWebhook
	m.ProcesarEntrante = nuevoProcesarEntrante(m)
	m.ConsultarSaludServicio = nuevoConsultarSaludServicio(m)
	m.ConsultarRouter = nuevoConsultarRouter(m)
	return m
}

// clienteDe arma el cliente HTTP de Horus de un conector leído (token descifrado).
func (m *Modulo) clienteDe(c conector.Leido) *Cliente {
	return &Cliente{Cliente: m.conexion.Cliente(c)}
}

func soloLecturas[Out any](Out) bool { return false }
