package reportes

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"reflect"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Handlers delgados: leen los filtros, llaman al caso de uso y escriben JSON o CSV.

const etiqueta = "reportes"

var seguridad = []map[string][]string{{"sesion": {}}, {"bearer": {}}}

// conCSV es un resultado que también se escribe como CSV.
type conCSV interface {
	csv(zona *time.Location) tablaCSV
}

// reporte describe un endpoint de reporte.
type reporte[In any, Out conCSV] struct {
	id, ruta, resumen, detalle, archivo string
	ejecutar                            func(context.Context, In) (Out, error)
	filtros                             func(*In) *FiltrosPeriodo
	// preparar ajusta la entrada cuando se pide CSV (reincidencias: todas las filas).
	preparar func(*In)
}

const descComun = " Permiso `reportes.ver` (con su alcance por zona o cuadrilla). Respeta la empresa activa. " +
	"`desde` y `hasta` son días en la hora de Guatemala (el período va de `desde` 00:00 al final de `hasta`); " +
	"como mucho 366 días (422 `rango_excedido`). JSON por defecto; CSV con encabezados en español con " +
	"`?formato=csv` o `Accept: text/csv`. Si algo no se puede leer, el reporte entero falla: nunca hay totales parciales. " +
	"Cada consulta tiene un tiempo máximo (15 s): si vence, 503 `reporte_demasiado_largo` (acotar el rango o los filtros)."

func registrar[In any, Out conCSV](api huma.API, m *Modulo, r reporte[In, Out]) {
	esquema := api.OpenAPI().Components.Schemas.Schema(reflect.TypeFor[Out](), true, "")
	op := huma.Operation{
		OperationID: r.id,
		Method:      http.MethodGet,
		Path:        "/api/v1/reportes/" + r.ruta,
		Summary:     r.resumen,
		Description: r.detalle + descComun,
		Tags:        []string{etiqueta},
		Errors:      []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusUnprocessableEntity, http.StatusServiceUnavailable},
		Security:    seguridad,
		Extensions:  map[string]any{"x-permiso": string(nucleo.PermReportesVer)},
		Responses: map[string]*huma.Response{"200": {
			Description: "El reporte (JSON) o su CSV",
			Content: map[string]*huma.MediaType{
				"application/json": {Schema: esquema},
				"text/csv":         {Schema: &huma.Schema{Type: huma.TypeString}},
			},
		}},
	}
	huma.Register(api, op, func(ctx context.Context, in *In) (*huma.StreamResponse, error) {
		csv := r.filtros(in).quiereCSV()
		if csv && r.preparar != nil {
			r.preparar(in)
		}
		out, err := r.ejecutar(ctx, *in)
		if err != nil {
			return nil, err
		}
		f := r.filtros(in)
		return &huma.StreamResponse{Body: func(hctx huma.Context) {
			hctx.SetHeader("Cache-Control", "no-store")
			if csv {
				hctx.SetHeader("Content-Type", "text/csv; charset=utf-8")
				hctx.SetHeader("Content-Disposition", `attachment; filename="reporte-`+r.archivo+`-`+f.Desde+`_`+f.Hasta+`.csv"`)
				hctx.SetStatus(http.StatusOK)
				if err := out.csv(m.cfg.Zona).escribir(hctx.BodyWriter()); err != nil {
					slog.WarnContext(ctx, "reportes: CSV interrumpido", "reporte", r.id, "err", err)
				}
				return
			}
			hctx.SetHeader("Content-Type", "application/json")
			hctx.SetStatus(http.StatusOK)
			enc := json.NewEncoder(hctx.BodyWriter())
			enc.SetEscapeHTML(false)
			if err := enc.Encode(out); err != nil {
				slog.WarnContext(ctx, "reportes: respuesta interrumpida", "reporte", r.id, "err", err)
			}
		}}, nil
	})
}

// Registrar publica las rutas de reportes.
func (m *Modulo) Registrar(api huma.API) {
	registrar(api, m, reporte[FiltrosTickets, ReporteSLA]{
		id: "reporteSLA", ruta: "sla", archivo: "sla", resumen: "SLA cumplido",
		detalle: "Tickets resueltos en el período (sin los fusionados): % con la primera respuesta y % con la resolución " +
			"dentro del vencimiento (los relojes sin SLA no cuentan en su porcentaje). Total, por prioridad y por categoría. " +
			"`tecnicoId` filtra por el responsable asignado.",
		ejecutar: m.SLA.Ejecutar, filtros: func(in *FiltrosTickets) *FiltrosPeriodo { return &in.FiltrosPeriodo },
	})
	registrar(api, m, reporte[FiltrosTickets, ReporteResolucion]{
		id: "reporteResolucion", ruta: "resolucion", archivo: "resolucion", resumen: "Tiempo medio de resolución",
		detalle: "Tickets resueltos en el período (sin los fusionados): promedio y mediana en minutos de reloj desde " +
			"la creación hasta la resolución. Total, por prioridad y por categoría.",
		ejecutar: m.Resolucion.Ejecutar, filtros: func(in *FiltrosTickets) *FiltrosPeriodo { return &in.FiltrosPeriodo },
	})
	registrar(api, m, reporte[FiltrosOrdenes, ReporteTecnicos]{
		id: "reporteTecnicos", ruta: "tecnicos", archivo: "tecnicos", resumen: "Órdenes por técnico",
		detalle: "Órdenes finalizadas en el período (cerradas o fallidas) por técnico: resultado, duración media en " +
			"sitio (de la llegada al cierre, sin las pausas en sitio) y tiempo pausado.",
		ejecutar: m.Tecnicos.Ejecutar, filtros: func(in *FiltrosOrdenes) *FiltrosPeriodo { return &in.FiltrosPeriodo },
	})
	registrar(api, m, reporte[EntradaPrimeraVisita, ReportePrimeraVisita]{
		id: "reportePrimeraVisita", ruta: "primera-visita", archivo: "primera-visita", resumen: "Primera visita resuelta",
		detalle: "Órdenes de instalación o visita cerradas con éxito en el período: % sin otra orden (no cancelada) ni " +
			"ticket del mismo servicio en los `dias` siguientes al cierre (30 por defecto, 7–90). Las que aún no " +
			"cumplen la ventana van en `enObservacion` y las que no tienen servicio en `sinServicio`; ninguna de las dos " +
			"cuenta en el porcentaje. Total, por tipo y por técnico.",
		ejecutar: m.PrimeraVisita.Ejecutar, filtros: func(in *EntradaPrimeraVisita) *FiltrosPeriodo { return &in.FiltrosPeriodo },
	})
	registrar(api, m, reporte[EntradaReincidencias, ReporteReincidencias]{
		id: "reporteReincidencias", ruta: "reincidencias", archivo: "reincidencias", resumen: "Reincidencias",
		detalle: "Tickets resueltos en el período cuyo mismo servicio (o, sin servicio, el mismo cliente) abrió otro " +
			"ticket dentro de `dias` (30 por defecto, 1–90). Totales sobre todos y la lista de reincidentes paginada " +
			"por cursor (más recientes primero). El CSV trae todas las filas (sin cursor), hasta 10 000 (si no, 422 " +
			"`exportacion_excedida`).",
		ejecutar: m.Reincidencias.Ejecutar, filtros: func(in *EntradaReincidencias) *FiltrosPeriodo { return &in.FiltrosPeriodo },
		preparar: func(in *EntradaReincidencias) { in.todas = true },
	})
}
