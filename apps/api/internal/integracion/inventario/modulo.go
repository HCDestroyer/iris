// Package inventario es el inventario del técnico en la app de campo (contrato Iris ↔ ArgosISP
// 0.5.0 §3.3; I1), sobre el vínculo de la ficha de agenda con su usuario de Argos:
//
//   - lecturas: su bodega en Argos (GET /inventario/mi-bodega, con caché corta) y sus órdenes de
//     traslado por recibir (modelo de lectura integracion.argos_traslados, de los webhooks);
//   - recibir una orden de traslado con firma: la operación traslado.recibir del lote de campo
//     encola el pendiente `recepcion` (Idempotency-Key iris:recepcion:<traslado_id>) bloqueado
//     hasta que la firma (adjunto de la entidad «traslado») termina de subir; entonces se arma el
//     cuerpo (PNG en base64 + sha256, dentro de los límites del contrato) y sale hacia Argos por la
//     cola de pendientes (un 409 traslado_ya_recibido cuenta como hecho);
//   - el estado de sus recepciones para GET /campo/cambios (los rechazos de negocio llegan así al
//     teléfono).
package inventario

import (
	"sync"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/pendientes"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/adjuntos"
)

// EntidadTraslado es la entidad de los adjuntos (la firma) y de los pendientes de una recepción.
const EntidadTraslado = "traslado"

// EsperandoFirma: el motivo del pendiente de recepción mientras la firma no termina de subir.
const EsperandoFirma = "esperando_firma"

// Config del módulo.
type Config struct {
	// Argos: el conector (cliente con protección SSRF, versión, instantánea). nil = sin conector.
	Argos *argos.Modulo
	// Almacen de los adjuntos: lee la firma PNG para armar el cuerpo (en cmd/worker). nil = no se
	// arma (queda esperando).
	Almacen adjuntos.Almacen
	// Encolar: el job que arma la firma (al encolar la recepción) y el que procesa la cola.
	Encolar pendientes.Encolador
	// Reloj (pruebas). time.Now por defecto.
	Reloj func() time.Time
	// CacheBodega: cuánto se reutiliza una lectura de mi-bodega por técnico y empresa (60 s).
	CacheBodega time.Duration
}

// Modulo reúne los casos de uso.
type Modulo struct {
	cfg Config

	MiBodega   *MiBodega
	Traslados  *auditoria.Caso[EntradaTraslados, PaginaTraslados]
	guardar    *auditoria.Caso[entradaArmar, ResultadoFirma]
	cacheMu    sync.Mutex
	cache      map[claveCache]bodegaEnCache
	sinArgosFn func() *nucleo.Problema
}

// Nuevo arma el módulo.
func Nuevo(cfg Config) *Modulo {
	if cfg.Reloj == nil {
		cfg.Reloj = time.Now
	}
	if cfg.CacheBodega <= 0 {
		cfg.CacheBodega = time.Minute
	}
	m := &Modulo{cfg: cfg, cache: map[claveCache]bodegaEnCache{}, sinArgosFn: argos.ConectorApagado}
	m.MiBodega = nuevoMiBodega(m)
	m.Traslados = nuevoTraslados(m)
	m.guardar = nuevoGuardarFirma(m)
	return m
}

func (m *Modulo) ahora() time.Time { return m.cfg.Reloj().UTC().Truncate(time.Microsecond) }

func soloLecturas[Out any](Out) bool { return false }
