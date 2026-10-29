package reportes

import (
	"context"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	reportesbd "github.com/HCDestroyer/iris/apps/api/internal/reportes/bd"
)

// Reportes de tickets: SLA cumplido y tiempo de resolución. Base: los tickets resueltos en el
// período (resuelto_en; un ticket reabierto y no vuelto a resolver no cuenta), sin los
// fusionados en otro. La prioridad es la actual (tras escalar).

// GrupoTickets identifica una fila del desglose: el total (sin prioridad ni categoría), una
// prioridad o una categoría (categoriaId null = sin categoría).
type GrupoTickets struct {
	Prioridad   *string `json:"prioridad,omitempty" enum:"baja,media,alta,critica" doc:"Solo en porPrioridad"`
	CategoriaID *int64  `json:"categoriaId,omitempty" doc:"Solo en porCategoria; ausente = sin categoría"`
	Categoria   *string `json:"categoria,omitempty" doc:"Nombre de la categoría (solo en porCategoria)"`
}

// FilaSLA: cumplimiento del SLA de un grupo.
type FilaSLA struct {
	GrupoTickets
	Resueltos            int64    `json:"resueltos" doc:"Tickets resueltos en el período"`
	ConSLARespuesta      int64    `json:"conSlaRespuesta" doc:"Con objetivo de primera respuesta (los demás no cuentan en el porcentaje)"`
	RespuestaEnTiempo    int64    `json:"respuestaEnTiempo" doc:"Primera respuesta hasta el vencimiento"`
	PorcentajeRespuesta  *float64 `json:"porcentajeRespuesta" nullable:"true" doc:"respuestaEnTiempo / conSlaRespuesta × 100; null sin base"`
	ConSLAResolucion     int64    `json:"conSlaResolucion" doc:"Con objetivo de resolución"`
	ResolucionEnTiempo   int64    `json:"resolucionEnTiempo" doc:"Resueltos hasta el vencimiento (con las pausas descontadas)"`
	PorcentajeResolucion *float64 `json:"porcentajeResolucion" nullable:"true" doc:"resolucionEnTiempo / conSlaResolucion × 100; null sin base"`
}

// ReporteSLA: SLA cumplido de los tickets resueltos en el período.
type ReporteSLA struct {
	Periodo      Periodo   `json:"periodo"`
	Total        FilaSLA   `json:"total"`
	PorPrioridad []FilaSLA `json:"porPrioridad"`
	PorCategoria []FilaSLA `json:"porCategoria"`
}

// FilaResolucion: tiempo de resolución de un grupo, en minutos de reloj (no laborales) desde la
// creación hasta la resolución.
type FilaResolucion struct {
	GrupoTickets
	Resueltos   int64    `json:"resueltos"`
	PromedioMin *float64 `json:"promedioMin" nullable:"true" doc:"Promedio en minutos; null sin tickets"`
	MedianaMin  *float64 `json:"medianaMin" nullable:"true" doc:"Mediana en minutos; null sin tickets"`
}

// ReporteResolucion: tiempo medio de resolución de los tickets resueltos en el período.
type ReporteResolucion struct {
	Periodo      Periodo          `json:"periodo"`
	Total        FilaResolucion   `json:"total"`
	PorPrioridad []FilaResolucion `json:"porPrioridad"`
	PorCategoria []FilaResolucion `json:"porCategoria"`
}

// grupo arma la identidad de la fila según las banderas de GROUPING (0 = agrupado por esa
// columna).
func grupo(gPrioridad, gCategoria int32, prioridad string, categoriaID int64, categoria string) (GrupoTickets, int) {
	switch {
	case gPrioridad == 0:
		return GrupoTickets{Prioridad: &prioridad}, 1
	case gCategoria == 0:
		g := GrupoTickets{Categoria: &categoria}
		if categoriaID != 0 {
			g.CategoriaID = &categoriaID
		} else {
			sin := "Sin categoría"
			g.Categoria = &sin
		}
		return g, 2
	}
	return GrupoTickets{}, 0
}

func paramsTickets(a ambito, f FiltrosTickets) reportesbd.ReporteSLAParams {
	return reportesbd.ReporteSLAParams{
		Inicio: a.inicio(), Fin: a.fin(), TodasEmpresas: a.todas, EmpresaID: a.empresa,
		AlcanceGlobal: a.global, Zonas: a.zonas, Cuadrillas: a.cuadrillas,
		ZonaID: a.zona, CuadrillaID: a.cuadrilla, TecnicoID: a.tecnico,
		CategoriaID: filtro(f.CategoriaID), Prioridades: noNulo(f.Prioridad),
	}
}

func (m *Modulo) nuevoSLA() *auditoria.Caso[FiltrosTickets, ReporteSLA] {
	return caso("reportes.sla", func(ctx context.Context, t *auditoria.Tx, in FiltrosTickets) (ReporteSLA, error) {
		a, err := m.resolver(ctx, in.FiltrosPeriodo)
		if err != nil {
			return ReporteSLA{}, err
		}
		filas, err := reportesbd.New(t).ReporteSLA(ctx, paramsTickets(a, in))
		if err != nil {
			return ReporteSLA{}, err
		}
		r := ReporteSLA{Periodo: a.periodo, Total: FilaSLA{}, PorPrioridad: []FilaSLA{}, PorCategoria: []FilaSLA{}}
		for _, f := range filas {
			g, tipo := grupo(f.PorPrioridadG, f.PorCategoriaG, f.Prioridad, f.CategoriaID, f.Categoria)
			fila := FilaSLA{
				GrupoTickets: g, Resueltos: f.Resueltos,
				ConSLARespuesta: f.ConSlaRespuesta, RespuestaEnTiempo: f.RespuestaEnTiempo,
				PorcentajeRespuesta: porcentaje(f.RespuestaEnTiempo, f.ConSlaRespuesta),
				ConSLAResolucion:    f.ConSlaResolucion, ResolucionEnTiempo: f.ResolucionEnTiempo,
				PorcentajeResolucion: porcentaje(f.ResolucionEnTiempo, f.ConSlaResolucion),
			}
			switch tipo {
			case 1:
				r.PorPrioridad = append(r.PorPrioridad, fila)
			case 2:
				r.PorCategoria = append(r.PorCategoria, fila)
			default:
				r.Total = fila
			}
		}
		return r, nil
	})
}

func (m *Modulo) nuevoResolucion() *auditoria.Caso[FiltrosTickets, ReporteResolucion] {
	return caso("reportes.resolucion", func(ctx context.Context, t *auditoria.Tx, in FiltrosTickets) (ReporteResolucion, error) {
		a, err := m.resolver(ctx, in.FiltrosPeriodo)
		if err != nil {
			return ReporteResolucion{}, err
		}
		filas, err := reportesbd.New(t).ReporteResolucion(ctx, reportesbd.ReporteResolucionParams(paramsTickets(a, in)))
		if err != nil {
			return ReporteResolucion{}, err
		}
		r := ReporteResolucion{Periodo: a.periodo, PorPrioridad: []FilaResolucion{}, PorCategoria: []FilaResolucion{}}
		for _, f := range filas {
			g, tipo := grupo(f.PorPrioridadG, f.PorCategoriaG, f.Prioridad, f.CategoriaID, f.Categoria)
			fila := FilaResolucion{GrupoTickets: g, Resueltos: f.Resueltos}
			if f.Resueltos > 0 {
				fila.PromedioMin, fila.MedianaMin = ptr(redondear(f.PromedioMin)), ptr(redondear(f.MedianaMin))
			}
			switch tipo {
			case 1:
				r.PorPrioridad = append(r.PorPrioridad, fila)
			case 2:
				r.PorCategoria = append(r.PorCategoria, fila)
			default:
				r.Total = fila
			}
		}
		return r, nil
	})
}

func ptr[T any](v T) *T { return &v }

// minutos de una duración en segundos, con dos decimales.
func minutos(seg float64) float64 { return redondear(seg / float64(time.Minute/time.Second)) }
