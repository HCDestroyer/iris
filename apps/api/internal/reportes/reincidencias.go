package reportes

import (
	"context"
	"fmt"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	reportesbd "github.com/HCDestroyer/iris/apps/api/internal/reportes/bd"
)

// Reincidencias: tickets resueltos en el período cuyo mismo servicio (o, sin servicio, el mismo
// cliente) abrió otro ticket (no fusionado, de la misma empresa) dentro de los días siguientes a
// la resolución. Los totales se calculan sobre todos; la lista de reincidentes va paginada por
// cursor (más recientes primero).

// MaxFilasCSV: el CSV de reincidencias trae todas las filas, hasta este tope; con más, 422
// exportacion_excedida (nunca un archivo recortado en silencio).
const MaxFilasCSV = 10000

// EntradaReincidencias: filtros y página del reporte de reincidencias.
type EntradaReincidencias struct {
	FiltrosTickets
	nucleo.ParamsPagina
	Dias int `query:"dias" minimum:"1" maximum:"90" default:"30" doc:"Ventana tras la resolución (1–90, 30 por defecto)"`
	// todas: el CSV pide todas las filas (hasta MaxFilasCSV), sin cursor.
	todas bool
}

// Reincidencia: un ticket resuelto que volvió.
type Reincidencia struct {
	TicketID          int64     `json:"ticketId"`
	Numero            string    `json:"numero"`
	Asunto            string    `json:"asunto"`
	Prioridad         string    `json:"prioridad" enum:"baja,media,alta,critica"`
	CategoriaID       *int64    `json:"categoriaId" nullable:"true"`
	Categoria         *string   `json:"categoria" nullable:"true"`
	ClienteID         *int64    `json:"clienteId" nullable:"true" doc:"Cliente de Argos"`
	ServicioID        *int64    `json:"servicioId" nullable:"true" doc:"Servicio de Argos (null: se comparó por cliente)"`
	ResueltoEn        time.Time `json:"resueltoEn"`
	SiguienteID       int64     `json:"siguienteId" doc:"Primer ticket que se abrió después"`
	SiguienteNumero   string    `json:"siguienteNumero"`
	SiguienteCreadoEn time.Time `json:"siguienteCreadoEn"`
	HorasHastaVolver  float64   `json:"horasHastaVolver" doc:"Horas de reloj entre la resolución y el siguiente ticket"`
	NuevosEnVentana   int64     `json:"nuevosEnVentana" doc:"Tickets abiertos en la ventana por el mismo servicio o cliente"`
}

// ReporteReincidencias: totales y una página de reincidentes.
type ReporteReincidencias struct {
	Periodo      Periodo        `json:"periodo"`
	Dias         int            `json:"dias"`
	Resueltos    int64          `json:"resueltos" doc:"Tickets resueltos en el período"`
	Evaluables   int64          `json:"evaluables" doc:"Con servicio o cliente (los demás no se pueden comparar)"`
	Reincidentes int64          `json:"reincidentes"`
	Porcentaje   *float64       `json:"porcentaje" nullable:"true" doc:"reincidentes / evaluables × 100; null sin base"`
	Data         []Reincidencia `json:"data"`
	NextCursor   *string        `json:"nextCursor" nullable:"true" doc:"Cursor de la página siguiente; null si no hay más"`
}

func errorDias(campo string, minimo, maximo int) *nucleo.Problema {
	return nucleo.Validacion(nucleo.ErrorCampo{Field: campo, Message: fmt.Sprintf("Debe estar entre %d y %d", minimo, maximo), Rule: "rango"})
}

func exportacionExcedida() *nucleo.Problema {
	return nucleo.ReglaNegocio("exportacion_excedida",
		fmt.Sprintf("El CSV tendría más de %d filas; acorta el período o agrega filtros", MaxFilasCSV))
}

func (m *Modulo) nuevoReincidencias() *auditoria.Caso[EntradaReincidencias, ReporteReincidencias] {
	return caso("reportes.reincidencias", func(ctx context.Context, t *auditoria.Tx, in EntradaReincidencias) (ReporteReincidencias, error) {
		dias, err := diasEntre("dias", in.Dias, 30, 1, 90)
		if err != nil {
			return ReporteReincidencias{}, err
		}
		var antesEn time.Time
		var antesID int64
		hay, err := nucleo.DecodificarCursor(in.Cursor, &antesEn, &antesID)
		if err != nil {
			return ReporteReincidencias{}, err
		}
		a, err := m.resolver(ctx, in.FiltrosPeriodo)
		if err != nil {
			return ReporteReincidencias{}, err
		}
		q := reportesbd.New(t)
		base := paramsTickets(a, in.FiltrosTickets)
		res, err := q.ResumenReincidencias(ctx, reportesbd.ResumenReincidenciasParams{
			Inicio: base.Inicio, Fin: base.Fin, TodasEmpresas: base.TodasEmpresas, EmpresaID: base.EmpresaID,
			AlcanceGlobal: base.AlcanceGlobal, Zonas: base.Zonas, Cuadrillas: base.Cuadrillas, ZonaID: base.ZonaID,
			CuadrillaID: base.CuadrillaID, TecnicoID: base.TecnicoID, CategoriaID: base.CategoriaID,
			Prioridades: base.Prioridades, Dias: int32(dias), //nolint:gosec // 1–90
		})
		if err != nil {
			return ReporteReincidencias{}, err
		}
		limite := in.Limite()
		if in.todas {
			if res.Reincidentes > MaxFilasCSV {
				return ReporteReincidencias{}, exportacionExcedida()
			}
			limite, hay = MaxFilasCSV, false
		}
		p := reportesbd.ListarReincidenciasParams{
			Inicio: base.Inicio, Fin: base.Fin, TodasEmpresas: base.TodasEmpresas, EmpresaID: base.EmpresaID,
			AlcanceGlobal: base.AlcanceGlobal, Zonas: base.Zonas, Cuadrillas: base.Cuadrillas, ZonaID: base.ZonaID,
			CuadrillaID: base.CuadrillaID, TecnicoID: base.TecnicoID, CategoriaID: base.CategoriaID,
			Prioridades: base.Prioridades, Dias: int32(dias), //nolint:gosec // 1–90
			Limite: int32(limite + 1), //nolint:gosec // ≤ MaxFilasCSV + 1
		}
		if hay {
			p.AntesEn, p.AntesID = &antesEn, &antesID
		}
		filas, err := q.ListarReincidencias(ctx, p)
		if err != nil {
			return ReporteReincidencias{}, err
		}
		pagina := nucleo.Paginar(filas, limite, aReincidencia,
			func(f reportesbd.ListarReincidenciasRow) []any { return []any{f.ResueltoEn, f.ID} })
		return ReporteReincidencias{
			Periodo: a.periodo, Dias: dias, Resueltos: res.Resueltos, Evaluables: res.Evaluables,
			Reincidentes: res.Reincidentes, Porcentaje: porcentaje(res.Reincidentes, res.Evaluables),
			Data: pagina.Data, NextCursor: pagina.NextCursor,
		}, nil
	})
}

func aReincidencia(f reportesbd.ListarReincidenciasRow) Reincidencia {
	r := Reincidencia{
		TicketID: f.ID, Numero: f.Numero, Asunto: f.Asunto, Prioridad: f.Prioridad, CategoriaID: f.CategoriaID,
		ClienteID: f.ClienteID, ServicioID: f.ServicioID, ResueltoEn: f.ResueltoEn,
		SiguienteID: f.SiguienteID, SiguienteNumero: f.SiguienteNumero, SiguienteCreadoEn: f.SiguienteCreadoEn,
		HorasHastaVolver: redondear(f.SiguienteCreadoEn.Sub(f.ResueltoEn).Hours()), NuevosEnVentana: f.Nuevos,
	}
	if f.CategoriaID != nil {
		r.Categoria = &f.Categoria
	}
	return r
}
