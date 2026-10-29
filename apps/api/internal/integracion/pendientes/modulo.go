// Package pendientes es la cola de acciones de Iris hacia Argos (contratos/argosisp.md §0 y §3;
// CLAUDE.md §4): lo que cambia datos de Argos (activar la instalación, comodatos, consumos de
// inventario, retiros, cargos únicos) se encola en integracion.pendientes en la misma
// transacción que lo origina y un job de River lo ejecuta contra la API de Argos:
//
//   - con la Idempotency-Key del contrato (iris:<operacion>:<id>), por el cliente del conector
//     (protección contra SSRF);
//   - 2xx → hecho; 4xx de negocio → fallido_negocio sin reintento (guarda el detail de Argos,
//     sin credenciales) y avisa (integracion.pendiente_fallido); red, timeout, 401/403, 408, 409 idempotencia_en_curso,
//     425, 429 o 5xx → reintento a 1 min, 5 min, 30 min, 2 h y 12 h (Esperas); después agotado y
//     alerta (integracion.pendiente_agotado);
//   - dependencias: un pendiente espera a que sus depende_de estén hechos (cambio de equipo:
//     la asignación del nuevo espera a la devolución del viejo);
//   - reintento manual (POST /integraciones/pendientes/{id}/reintentar) y lista para el panel.
//
// El suscriptor de orden.cerrada (cierre_orden.go) arma los pendientes de cada orden.
package pendientes

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Estados de un pendiente.
const (
	EstadoPendiente      = "pendiente"
	EstadoEnCurso        = "en_curso"
	EstadoHecho          = "hecho"
	EstadoFallidoNegocio = "fallido_negocio"
	EstadoAgotado        = "agotado"
	// EstadoBloqueado: una dependencia quedó fallido_negocio o agotado; vuelve a pendiente
	// cuando todas sus dependencias están hechas (p. ej. tras reintentar la dependencia).
	EstadoBloqueado = "bloqueado"
)

// Eventos que emite el módulo (contratos/eventos.md, «Pendientes hacia Argos»).
const (
	EventoHecho        = "integracion.pendiente_hecho"
	EventoFallido      = "integracion.pendiente_fallido"
	EventoAgotado      = "integracion.pendiente_agotado"
	EventoOmitido      = "integracion.pendiente_omitido"
	EventoReintentado  = "integracion.pendiente_reintentado"
	EventoCorregido    = "integracion.pendiente_corregido"
	EventoConflicto    = "integracion.pendiente_conflicto"
	EventoBloqueado    = "integracion.pendiente_bloqueado"
	EventoDesbloqueado = "integracion.pendiente_desbloqueado"
	EventoOrdenCerrada = "orden.cerrada"
)

// Esperas entre intentos (contratos/argosisp.md §0): tras el n-ésimo intento fallido se espera
// Esperas[n-1]; tras el último (6 intentos en total) el pendiente queda agotado.
var Esperas = []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 12 * time.Hour}

// Encolador inserta un job de River en la transacción del caso de uso.
type Encolador func(ctx context.Context, tx pgx.Tx, args river.JobArgs) error

// Config del módulo.
type Config struct {
	// Argos es el conector (cliente con protección SSRF y conector activo). nil = no se puede
	// procesar (solo OpenAPI).
	Argos *argos.Modulo
	// Encolar el job que procesa la cola al crear o reintentar un pendiente. nil = lo toma el
	// periódico (cada minuto).
	Encolar Encolador
	// Reloj (pruebas). time.Now por defecto.
	Reloj func() time.Time
	// Reclamo: un pendiente en_curso más viejo que esto se considera abandonado (el worker cayó)
	// y se vuelve a tomar (15 min). La Idempotency-Key hace seguro repetir la llamada.
	Reclamo time.Duration
}

// Modulo reúne los casos de uso de la cola.
type Modulo struct {
	cfg Config

	Listar             *auditoria.Caso[EntradaListar, nucleo.Pagina[Pendiente]]
	Reintentar         *auditoria.Caso[EntradaReintentar, Pendiente]
	EncolarCierreOrden *auditoria.Caso[OrdenCerrada, ResultadoCierre]
	resolver           *auditoria.Caso[resultadoIntento, Pendiente]
	resolverListos     *auditoria.Caso[time.Time, int]
}

// Nuevo arma el módulo.
func Nuevo(cfg Config) *Modulo {
	if cfg.Reloj == nil {
		cfg.Reloj = time.Now
	}
	if cfg.Reclamo <= 0 {
		cfg.Reclamo = 15 * time.Minute
	}
	m := &Modulo{cfg: cfg}
	m.Listar = nuevoListar(m)
	m.Reintentar = nuevoReintentar(m)
	m.EncolarCierreOrden = nuevoEncolarCierreOrden(m)
	m.resolver = nuevoResolver(m)
	m.resolverListos = auditoria.Nuevo(auditoria.Caso[time.Time, int]{
		Accion: "integraciones.resolver_pendientes", Acceso: auditoria.Autenticado,
		AuditarExito: func(n int) bool { return n > 0 },
		Logica: func(ctx context.Context, t *auditoria.Tx, ahora time.Time) (int, error) {
			return resolverAtascados(ctx, t, ahora)
		},
	})
	return m
}

func (m *Modulo) ahora() time.Time { return m.cfg.Reloj().UTC().Truncate(time.Microsecond) }

// avisarCola encola el procesamiento en la transacción (si hay Encolar).
func (m *Modulo) avisarCola(ctx context.Context, tx pgx.Tx) error {
	if m.cfg.Encolar == nil {
		return nil
	}
	return m.cfg.Encolar(ctx, tx, ArgsProcesar{})
}
