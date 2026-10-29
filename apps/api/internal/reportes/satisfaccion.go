package reportes

import (
	"context"
	"strconv"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	reportesbd "github.com/HCDestroyer/iris/apps/api/internal/reportes/bd"
)

// Satisfacción: las encuestas creadas (enviadas) en el período (soporte.encuestas). zona,
// cuadrilla, técnico (usuario: técnico de la orden o responsable del ticket) y categoría son los
// copiados al crear la encuesta.

// EntradaSatisfaccion: filtros del reporte de satisfacción.
type EntradaSatisfaccion struct {
	FiltrosPeriodo
	CategoriaID int64  `query:"categoriaId" minimum:"0" doc:"Categoría del ticket"`
	Entidad     string `query:"entidad" enum:"ticket,orden" doc:"Solo las de tickets o las de órdenes"`
}

// Distribucion: cuántas respuestas con cada calificación.
type Distribucion struct {
	Calificacion int   `json:"calificacion" minimum:"1" maximum:"5"`
	Cantidad     int64 `json:"cantidad"`
}

// FilaSatisfaccion: encuestas de un grupo.
type FilaSatisfaccion struct {
	Entidad               *string        `json:"entidad,omitempty" enum:"ticket,orden" doc:"Solo en porEntidad"`
	Enviadas              int64          `json:"enviadas" doc:"Encuestas creadas en el período"`
	Respondidas           int64          `json:"respondidas"`
	Pendientes            int64          `json:"pendientes" doc:"Sin responder y aún vigentes"`
	Vencidas              int64          `json:"vencidas" doc:"Sin responder y vencidas"`
	PorcentajeRespondidas *float64       `json:"porcentajeRespondidas" nullable:"true" doc:"respondidas / enviadas × 100; null sin base"`
	Promedio              *float64       `json:"promedio" nullable:"true" doc:"Calificación media (1–5); null sin respuestas"`
	Alertas               int64          `json:"alertas" doc:"Respuestas con calificación 1 o 2"`
	Distribucion          []Distribucion `json:"distribucion" doc:"Siempre las 5 calificaciones, de 1 a 5"`
}

// ReporteSatisfaccion: satisfacción de los clientes.
type ReporteSatisfaccion struct {
	Periodo    Periodo            `json:"periodo"`
	Total      FilaSatisfaccion   `json:"total"`
	PorEntidad []FilaSatisfaccion `json:"porEntidad"`
}

func (m *Modulo) nuevoSatisfaccion() *auditoria.Caso[EntradaSatisfaccion, ReporteSatisfaccion] {
	return caso("reportes.satisfaccion", func(ctx context.Context, t *auditoria.Tx, in EntradaSatisfaccion) (ReporteSatisfaccion, error) {
		a, err := m.resolver(ctx, in.FiltrosPeriodo)
		if err != nil {
			return ReporteSatisfaccion{}, err
		}
		p := reportesbd.ReporteSatisfaccionParams{
			Ahora: m.ahora(), Inicio: a.periodo.Inicio, Fin: a.periodo.Fin, TodasEmpresas: a.todas, EmpresaID: a.empresa,
			AlcanceGlobal: a.global, Zonas: a.zonas, Cuadrillas: a.cuadrillas,
			ZonaID: a.zona, CuadrillaID: a.cuadrilla, TecnicoID: a.tecnico, CategoriaID: filtro(in.CategoriaID),
		}
		if in.Entidad != "" {
			p.Entidad = &in.Entidad
		}
		filas, err := reportesbd.New(t).ReporteSatisfaccion(ctx, p)
		if err != nil {
			return ReporteSatisfaccion{}, err
		}
		r := ReporteSatisfaccion{Periodo: a.periodo, Total: filaSatisfaccion(reportesbd.ReporteSatisfaccionRow{}), PorEntidad: []FilaSatisfaccion{}}
		for _, f := range filas {
			fila := filaSatisfaccion(f)
			if f.PorEntidadG == 0 {
				fila.Entidad = ptr(f.Entidad)
				r.PorEntidad = append(r.PorEntidad, fila)
				continue
			}
			r.Total = fila
		}
		return r, nil
	})
}

func filaSatisfaccion(f reportesbd.ReporteSatisfaccionRow) FilaSatisfaccion {
	fila := FilaSatisfaccion{
		Enviadas: f.Enviadas, Respondidas: f.Respondidas, Pendientes: f.Pendientes, Vencidas: f.Vencidas,
		PorcentajeRespondidas: porcentaje(f.Respondidas, f.Enviadas), Alertas: f.C1 + f.C2,
		Distribucion: []Distribucion{{1, f.C1}, {2, f.C2}, {3, f.C3}, {4, f.C4}, {5, f.C5}},
	}
	if f.Respondidas > 0 {
		fila.Promedio = ptr(redondear(f.Promedio))
	}
	return fila
}

func (r ReporteSatisfaccion) csv(*time.Location) tablaCSV {
	t := tablaCSV{encabezados: []string{"Grupo", "Entidad", "Enviadas", "Respondidas", "Pendientes", "Vencidas",
		"% respondidas", "Promedio", "Alertas (1 o 2)", "Calificación 1", "Calificación 2", "Calificación 3",
		"Calificación 4", "Calificación 5"}}
	nombres := map[string]string{"ticket": "Tickets", "orden": "Órdenes"}
	for _, f := range append([]FilaSatisfaccion{r.Total}, r.PorEntidad...) {
		grupo := "Total"
		if f.Entidad != nil {
			grupo = "Entidad"
		}
		celdas := []string{grupo, nombreDe(nombres, f.Entidad), celdaEntero(f.Enviadas), celdaEntero(f.Respondidas),
			celdaEntero(f.Pendientes), celdaEntero(f.Vencidas), celdaDecimalP(f.PorcentajeRespondidas),
			celdaDecimalP(f.Promedio), celdaEntero(f.Alertas)}
		for _, d := range f.Distribucion {
			celdas = append(celdas, strconv.FormatInt(d.Cantidad, 10))
		}
		t.fila(celdas...)
	}
	return t
}
