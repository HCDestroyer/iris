// Package reportes: reportes de solo lectura sobre soporte y campo (Fase 5): SLA cumplido,
// tiempo de resolución, órdenes por técnico, primera visita resuelta y reincidencias.
//
// Cada reporte es un caso de uso de internal/auditoria con el permiso reportes.ver: corre en una
// transacción de lectura, respeta la empresa activa y el alcance por zona o cuadrilla del rol, y
// no deja fila de auditoría al terminar bien (sí al fallar), como las demás lecturas. Los
// handlers (rutas.go) solo leen los filtros, llaman al caso y escriben JSON o CSV.
//
// Nunca se devuelve un total calculado sobre datos que no se pudieron leer: si una consulta
// falla, el reporte entero falla (500/503), sin resultados parciales.
package reportes

import (
	"context"
	"time"
	_ "time/tzdata" // zonas horarias embebidas: la imagen distroless no trae /usr/share/zoneinfo

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// ZonaHorariaPorDefecto: los días de los filtros se interpretan en la hora de Guatemala.
const ZonaHorariaPorDefecto = "America/Guatemala"

// Config del módulo.
type Config struct {
	// Reloj da la hora actual (time.Now por defecto). Las pruebas lo fijan.
	Reloj func() time.Time
	// Zona en que se interpretan desde/hasta (America/Guatemala por defecto).
	Zona *time.Location
}

// Modulo reúne los reportes.
type Modulo struct {
	cfg Config

	SLA           *auditoria.Caso[FiltrosTickets, ReporteSLA]
	Resolucion    *auditoria.Caso[FiltrosTickets, ReporteResolucion]
	Tecnicos      *auditoria.Caso[FiltrosOrdenes, ReporteTecnicos]
	PrimeraVisita *auditoria.Caso[EntradaPrimeraVisita, ReportePrimeraVisita]
	Reincidencias *auditoria.Caso[EntradaReincidencias, ReporteReincidencias]
	Satisfaccion  *auditoria.Caso[EntradaSatisfaccion, ReporteSatisfaccion]
}

// Nuevo arma el módulo.
func Nuevo(cfg Config) *Modulo {
	if cfg.Reloj == nil {
		cfg.Reloj = time.Now
	}
	if cfg.Zona == nil {
		zona, err := time.LoadLocation(ZonaHorariaPorDefecto)
		if err != nil {
			panic("reportes: sin la zona " + ZonaHorariaPorDefecto + ": " + err.Error())
		}
		cfg.Zona = zona
	}
	m := &Modulo{cfg: cfg}
	m.SLA = m.nuevoSLA()
	m.Resolucion = m.nuevoResolucion()
	m.Tecnicos = m.nuevoTecnicos()
	m.PrimeraVisita = m.nuevoPrimeraVisita()
	m.Reincidencias = m.nuevoReincidencias()
	m.Satisfaccion = m.nuevoSatisfaccion()
	return m
}

// ahora es la hora del módulo en UTC, truncada a microsegundos (la precisión de Postgres).
func (m *Modulo) ahora() time.Time { return m.cfg.Reloj().UTC().Truncate(time.Microsecond) }

// soloLecturas: un reporte que termina bien no deja fila de auditoría.
func soloLecturas[Out any](Out) bool { return false }

// caso arma un reporte con el permiso reportes.ver.
func caso[In, Out any](accion string, logica func(ctx context.Context, t *auditoria.Tx, in In) (Out, error)) *auditoria.Caso[In, Out] {
	return auditoria.Nuevo(auditoria.Caso[In, Out]{
		Accion:       accion,
		Permiso:      nucleo.PermReportesVer,
		AuditarExito: soloLecturas[Out],
		Logica:       logica,
	})
}
