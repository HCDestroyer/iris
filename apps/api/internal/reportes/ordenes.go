package reportes

import (
	"context"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	reportesbd "github.com/HCDestroyer/iris/apps/api/internal/reportes/bd"
)

// Reportes de órdenes: por técnico y primera visita resuelta. El técnico es el usuario de la
// orden (campo.ordenes.tecnico_id).

// FilaTecnico: órdenes finalizadas (cerradas o fallidas) de un técnico en el período.
type FilaTecnico struct {
	TecnicoID             int64    `json:"tecnicoId" doc:"Usuario del técnico (0 en el total)"`
	Nombre                string   `json:"nombre"`
	Finalizadas           int64    `json:"finalizadas" doc:"Cerradas + fallidas"`
	Exitosas              int64    `json:"exitosas" doc:"Cerradas con resultado exitosa"`
	Parciales             int64    `json:"parciales" doc:"Cerradas con resultado parcial"`
	Fallidas              int64    `json:"fallidas"`
	PorcentajeExito       *float64 `json:"porcentajeExito" nullable:"true" doc:"exitosas / finalizadas × 100"`
	ConTiempoEnSitio      int64    `json:"conTiempoEnSitio" doc:"Órdenes con llegada registrada (base de la duración media)"`
	DuracionMediaSitioMin *float64 `json:"duracionMediaSitioMin" nullable:"true" doc:"Minutos desde la llegada hasta el cierre, sin las pausas tomadas en sitio; null sin base"`
	TiempoPausadoMin      float64  `json:"tiempoPausadoMin" doc:"Minutos pausados en total (todas las pausas de esas órdenes)"`
	TiempoPausadoMedioMin *float64 `json:"tiempoPausadoMedioMin" nullable:"true" doc:"Minutos pausados por orden finalizada; null sin órdenes"`
	ConPausa              int64    `json:"conPausa" doc:"Órdenes que se pausaron al menos una vez"`
}

// ReporteTecnicos: órdenes por técnico.
type ReporteTecnicos struct {
	Periodo Periodo       `json:"periodo"`
	Total   FilaTecnico   `json:"total"`
	Data    []FilaTecnico `json:"data" doc:"Un técnico por fila, más órdenes primero"`
}

func (m *Modulo) nuevoTecnicos() *auditoria.Caso[FiltrosOrdenes, ReporteTecnicos] {
	return caso(m, "reportes.tecnicos", func(ctx context.Context, t *auditoria.Tx, in FiltrosOrdenes) (ReporteTecnicos, error) {
		a, err := m.resolver(ctx, in.FiltrosPeriodo)
		if err != nil {
			return ReporteTecnicos{}, err
		}
		filas, err := reportesbd.New(t).ReporteTecnicos(ctx, reportesbd.ReporteTecnicosParams{
			Inicio: a.inicio(), Fin: a.fin(), TodasEmpresas: a.todas, EmpresaID: a.empresa,
			AlcanceGlobal: a.global, Zonas: a.zonas, Cuadrillas: a.cuadrillas,
			ZonaID: a.zona, CuadrillaID: a.cuadrilla, TecnicoID: a.tecnico,
		})
		if err != nil {
			return ReporteTecnicos{}, err
		}
		r := ReporteTecnicos{Periodo: a.periodo, Data: make([]FilaTecnico, 0, len(filas))}
		var total reportesbd.ReporteTecnicosRow
		for _, f := range filas {
			r.Data = append(r.Data, filaTecnico(f))
			total.Finalizadas += f.Finalizadas
			total.Exitosas += f.Exitosas
			total.Parciales += f.Parciales
			total.Fallidas += f.Fallidas
			total.ConSitio += f.ConSitio
			total.SitioSeg += f.SitioSeg
			total.PausadoSeg += f.PausadoSeg
			total.ConPausa += f.ConPausa
		}
		total.Nombre = "Total"
		r.Total = filaTecnico(total)
		return r, nil
	})
}

func filaTecnico(f reportesbd.ReporteTecnicosRow) FilaTecnico {
	fila := FilaTecnico{
		TecnicoID: f.TecnicoID, Nombre: f.Nombre, Finalizadas: f.Finalizadas, Exitosas: f.Exitosas,
		Parciales: f.Parciales, Fallidas: f.Fallidas, PorcentajeExito: porcentaje(f.Exitosas, f.Finalizadas),
		ConTiempoEnSitio: f.ConSitio, TiempoPausadoMin: minutos(float64(f.PausadoSeg)), ConPausa: f.ConPausa,
	}
	if f.ConSitio > 0 {
		fila.DuracionMediaSitioMin = ptr(minutos(f.SitioSeg / float64(f.ConSitio)))
	}
	if f.Finalizadas > 0 {
		fila.TiempoPausadoMedioMin = ptr(minutos(float64(f.PausadoSeg) / float64(f.Finalizadas)))
	}
	return fila
}

// EntradaPrimeraVisita: filtros del reporte de primera visita resuelta.
type EntradaPrimeraVisita struct {
	FiltrosPeriodo
	Dias int `query:"dias" minimum:"7" maximum:"90" default:"30" doc:"Ventana tras el cierre en que otra orden o ticket del mismo servicio cuenta como retorno (7–90)"`
}

// FilaPrimeraVisita: órdenes de instalación o visita cerradas con éxito de un grupo.
type FilaPrimeraVisita struct {
	Tipo          *string  `json:"tipo,omitempty" enum:"instalacion,visita" doc:"Solo en porTipo"`
	TecnicoID     *int64   `json:"tecnicoId,omitempty" doc:"Solo en porTecnico"`
	Tecnico       *string  `json:"tecnico,omitempty" doc:"Nombre del técnico (solo en porTecnico)"`
	Cerradas      int64    `json:"cerradas" doc:"Cerradas con éxito en el período"`
	Evaluadas     int64    `json:"evaluadas" doc:"Con servicio y con la ventana ya cumplida: resueltas + conRetorno"`
	Resueltas     int64    `json:"resueltas" doc:"Sin otra orden (no cancelada) ni ticket del mismo servicio en la ventana"`
	ConRetorno    int64    `json:"conRetorno"`
	Porcentaje    *float64 `json:"porcentaje" nullable:"true" doc:"resueltas / evaluadas × 100; null sin base"`
	EnObservacion int64    `json:"enObservacion" doc:"Su ventana todavía no termina (no cuentan en el porcentaje)"`
	SinServicio   int64    `json:"sinServicio" doc:"Sin servicio de Argos: no se pueden evaluar"`
}

// ReportePrimeraVisita: primera visita resuelta.
type ReportePrimeraVisita struct {
	Periodo    Periodo             `json:"periodo"`
	Dias       int                 `json:"dias"`
	Total      FilaPrimeraVisita   `json:"total"`
	PorTipo    []FilaPrimeraVisita `json:"porTipo"`
	PorTecnico []FilaPrimeraVisita `json:"porTecnico"`
}

func (m *Modulo) nuevoPrimeraVisita() *auditoria.Caso[EntradaPrimeraVisita, ReportePrimeraVisita] {
	return caso(m, "reportes.primera_visita", func(ctx context.Context, t *auditoria.Tx, in EntradaPrimeraVisita) (ReportePrimeraVisita, error) {
		dias, err := diasEntre("dias", in.Dias, 30, 7, 90)
		if err != nil {
			return ReportePrimeraVisita{}, err
		}
		a, err := m.resolver(ctx, in.FiltrosPeriodo)
		if err != nil {
			return ReportePrimeraVisita{}, err
		}
		filas, err := reportesbd.New(t).ReportePrimeraVisita(ctx, reportesbd.ReportePrimeraVisitaParams{
			Dias: int32(dias), Ahora: m.ahora(), //nolint:gosec // 7–90
			Inicio: a.inicio(), Fin: a.fin(), TodasEmpresas: a.todas, EmpresaID: a.empresa,
			AlcanceGlobal: a.global, Zonas: a.zonas, Cuadrillas: a.cuadrillas,
			ZonaID: a.zona, CuadrillaID: a.cuadrilla, TecnicoID: a.tecnico,
		})
		if err != nil {
			return ReportePrimeraVisita{}, err
		}
		r := ReportePrimeraVisita{Periodo: a.periodo, Dias: dias, PorTipo: []FilaPrimeraVisita{}, PorTecnico: []FilaPrimeraVisita{}}
		for _, f := range filas {
			evaluadas := f.Resueltas + f.ConRetorno
			fila := FilaPrimeraVisita{
				Cerradas: f.Cerradas, Evaluadas: evaluadas, Resueltas: f.Resueltas, ConRetorno: f.ConRetorno,
				Porcentaje: porcentaje(f.Resueltas, evaluadas), EnObservacion: f.EnObservacion, SinServicio: f.SinServicio,
			}
			switch {
			case f.PorTipoG == 0:
				fila.Tipo = ptr(f.Tipo)
				r.PorTipo = append(r.PorTipo, fila)
			case f.PorTecnicoG == 0:
				if f.TecnicoID != 0 { // sin técnico: ambos ausentes
					fila.TecnicoID, fila.Tecnico = ptr(f.TecnicoID), ptr(f.Tecnico)
				}
				r.PorTecnico = append(r.PorTecnico, fila)
			default:
				r.Total = fila
			}
		}
		return r, nil
	})
}

// diasEntre valida una ventana en días (0 = el valor por defecto).
func diasEntre(campo string, v, defecto, minimo, maximo int) (int, error) {
	if v == 0 {
		return defecto, nil
	}
	if v < minimo || v > maximo {
		return 0, errorDias(campo, minimo, maximo)
	}
	return v, nil
}
