package reportes

import (
	"context"
	"fmt"
	"math"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// RangoMaximoDias: un reporte cubre como mucho 366 días (un año, también bisiesto).
const RangoMaximoDias = 366

// FiltrosPeriodo son los filtros de todo reporte. desde y hasta son días (YYYY-MM-DD) en la hora
// de Guatemala; el período va de desde a las 00:00 hasta el fin del día hasta (incluido).
type FiltrosPeriodo struct {
	Desde       string `query:"desde" required:"true" format:"date" example:"2026-10-01" doc:"Primer día (YYYY-MM-DD, hora de Guatemala)"`
	Hasta       string `query:"hasta" required:"true" format:"date" example:"2026-10-31" doc:"Último día, incluido (YYYY-MM-DD, hora de Guatemala). Como mucho 366 días desde desde (422 rango_excedido)"`
	ZonaID      int64  `query:"zonaId" minimum:"0" doc:"Zona"`
	CuadrillaID int64  `query:"cuadrillaId" minimum:"0" doc:"Cuadrilla"`
	TecnicoID   int64  `query:"tecnicoId" minimum:"0" doc:"Usuario: el técnico de la orden o, en los reportes de tickets, el responsable asignado"`
	Formato     string `query:"formato" json:"-" enum:"json,csv" doc:"csv: text/csv con encabezados en español (también con Accept: text/csv)"`
	Accept      string `header:"Accept" json:"-"`
}

// FiltrosTickets: los de los reportes de tickets.
type FiltrosTickets struct {
	FiltrosPeriodo
	CategoriaID int64    `query:"categoriaId" minimum:"0" doc:"Categoría del ticket"`
	Prioridad   []string `query:"prioridad" maxItems:"4" enum:"baja,media,alta,critica" doc:"Una o varias prioridades (prioridad=alta,critica)"`
}

// FiltrosOrdenes: los de los reportes de órdenes.
type FiltrosOrdenes struct {
	FiltrosPeriodo
}

// quiereCSV: ?formato=csv o Accept con text/csv.
func (f FiltrosPeriodo) quiereCSV() bool {
	if f.Formato != "" {
		return f.Formato == "csv"
	}
	for _, parte := range strings.Split(f.Accept, ",") {
		tipo, params, err := mime.ParseMediaType(strings.TrimSpace(parte))
		if err == nil && tipo == "text/csv" && params["q"] != "0" {
			return true
		}
	}
	return false
}

// Periodo es el período resuelto de un reporte.
type Periodo struct {
	Desde       string    `json:"desde" format:"date" doc:"Primer día (hora de Guatemala)"`
	Hasta       string    `json:"hasta" format:"date" doc:"Último día, incluido (hora de Guatemala)"`
	Inicio      time.Time `json:"inicio" doc:"Inicio en UTC: desde a las 00:00 de Guatemala"`
	Fin         time.Time `json:"fin" doc:"Fin en UTC, exclusivo: el día siguiente a hasta a las 00:00 de Guatemala"`
	ZonaHoraria string    `json:"zonaHoraria" example:"America/Guatemala"`
}

// ambito es lo que un reporte puede leer: período, empresa, alcance y filtros opcionales.
type ambito struct {
	periodo    Periodo
	todas      bool
	empresa    *int64
	global     bool
	zonas      []int64
	cuadrillas []int64
	zona       *int64
	cuadrilla  *int64
	tecnico    *int64
}

func (a ambito) inicio() *time.Time { return &a.periodo.Inicio }
func (a ambito) fin() *time.Time    { return &a.periodo.Fin }

func rangoExcedido() *nucleo.Problema {
	return nucleo.NuevoProblema(http.StatusUnprocessableEntity, "rango_excedido", "Rango demasiado largo",
		fmt.Sprintf("Un reporte cubre como mucho %d días", RangoMaximoDias),
		nucleo.ErrorCampo{Field: "hasta", Message: fmt.Sprintf("Como mucho %d días desde desde", RangoMaximoDias), Rule: "rango"})
}

// periodo interpreta desde y hasta en la zona del módulo.
func (m *Modulo) periodo(desde, hasta string) (Periodo, error) {
	d, errD := time.ParseInLocation(time.DateOnly, desde, m.cfg.Zona)
	h, errH := time.ParseInLocation(time.DateOnly, hasta, m.cfg.Zona)
	var errores []nucleo.ErrorCampo
	if errD != nil {
		errores = append(errores, nucleo.ErrorCampo{Field: "desde", Message: "Fecha inválida (YYYY-MM-DD)", Rule: "date"})
	}
	if errH != nil {
		errores = append(errores, nucleo.ErrorCampo{Field: "hasta", Message: "Fecha inválida (YYYY-MM-DD)", Rule: "date"})
	}
	if len(errores) > 0 {
		return Periodo{}, nucleo.Validacion(errores...)
	}
	if h.Before(d) {
		return Periodo{}, nucleo.Validacion(nucleo.ErrorCampo{Field: "hasta", Message: "Debe ser igual o posterior a desde", Rule: "rango"})
	}
	// Días de calendario (AddDate respeta la zona; Guatemala no tiene horario de verano, pero no
	// se asume).
	if !d.AddDate(0, 0, RangoMaximoDias).After(h) {
		return Periodo{}, rangoExcedido()
	}
	return Periodo{
		Desde: d.Format(time.DateOnly), Hasta: h.Format(time.DateOnly),
		Inicio: d.UTC(), Fin: h.AddDate(0, 0, 1).UTC(), ZonaHoraria: m.cfg.Zona.String(),
	}, nil
}

// resolver arma el ámbito de un reporte: período, empresa activa y alcance de reportes.ver.
func (m *Modulo) resolver(ctx context.Context, f FiltrosPeriodo) (ambito, error) {
	p, err := m.periodo(f.Desde, f.Hasta)
	if err != nil {
		return ambito{}, err
	}
	a := ambito{periodo: p, zona: filtro(f.ZonaID), cuadrilla: filtro(f.CuadrillaID), tecnico: filtro(f.TecnicoID)}
	a.todas, a.empresa = filtroEmpresa(ctx)
	global, zonas, cuadrillas := contexto.PermisosDe(ctx).Alcances(string(nucleo.PermReportesVer))
	if contexto.ActorDe(ctx).Tipo == contexto.ActorSistema {
		global = true // el sistema (consola, pruebas de carga) no tiene alcance limitado
	}
	a.global, a.zonas, a.cuadrillas = global, noNulo(zonas), noNulo(cuadrillas)
	return a, nil
}

// filtroEmpresa dice qué filas ve el contexto según cómo se resolvió la empresa activa (las
// mismas reglas que soporte y campo):
//   - EmpresaActiva: solo las de esa empresa.
//   - EmpresaSinConector: solo las guardadas con empresa_id null (nunca «sin filtro»).
//   - EmpresaNoResuelta: solo llega el actor sistema; ve todas.
//   - Cualquier otro estado no llega a la lógica; por si acaso, nada.
func filtroEmpresa(ctx context.Context) (todas bool, id *int64) {
	switch contexto.EmpresasDe(ctx).Estado {
	case contexto.EmpresaActiva:
		if activa := contexto.EmpresaDe(ctx); activa != 0 {
			return false, &activa
		}
	case contexto.EmpresaSinConector:
		return false, nil
	case contexto.EmpresaNoResuelta:
		if contexto.ActorDe(ctx).EsUsuario() {
			break
		}
		if activa := contexto.EmpresaDe(ctx); activa != 0 {
			return false, &activa
		}
		return true, nil
	case contexto.EmpresaPorElegir, contexto.EmpresaSinAsignar:
	}
	cero := int64(0) // ninguna empresa tiene id 0: no ve nada
	return false, &cero
}

func filtro(v int64) *int64 {
	if v <= 0 {
		return nil
	}
	return &v
}

func noNulo[T any](v []T) []T {
	if v == nil {
		return []T{}
	}
	return v
}

// porcentaje con dos decimales; nil si no hay base (nunca 0 % ni 100 % inventados).
func porcentaje(parte, base int64) *float64 {
	if base <= 0 {
		return nil
	}
	v := math.Round(float64(parte)*10000/float64(base)) / 100
	return &v
}

// redondear a dos decimales.
func redondear(v float64) float64 { return math.Round(v*100) / 100 }
