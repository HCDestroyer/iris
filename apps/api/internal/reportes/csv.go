package reportes

import (
	"encoding/csv"
	"io"
	"strconv"
	"strings"
	"time"
)

// CSV de los reportes: UTF-8 con BOM (Excel lo abre con acentos), separado por comas, una fila
// de encabezados en español. Fechas en la hora de Guatemala (YYYY-MM-DD HH:MM), números con punto
// decimal y celdas vacías donde el JSON trae null. Los textos que empiezan con = + - @ van con
// un apóstrofo delante (inyección de fórmulas en hojas de cálculo).

type tablaCSV struct {
	encabezados []string
	filas       [][]string
}

func (t *tablaCSV) fila(celdas ...string) { t.filas = append(t.filas, celdas) }

func (t tablaCSV) escribir(w io.Writer) error {
	if _, err := io.WriteString(w, "\xEF\xBB\xBF"); err != nil {
		return err
	}
	cw := csv.NewWriter(w)
	if err := cw.Write(t.encabezados); err != nil {
		return err
	}
	if err := cw.WriteAll(t.filas); err != nil {
		return err
	}
	cw.Flush()
	return cw.Error()
}

func celdaTexto(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

func celdaTextoP(s *string) string {
	if s == nil {
		return ""
	}
	return celdaTexto(*s)
}

func celdaEntero(v int64) string { return strconv.FormatInt(v, 10) }

func celdaEnteroP(v *int64) string {
	if v == nil {
		return ""
	}
	return celdaEntero(*v)
}

func celdaDecimal(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

func celdaDecimalP(v *float64) string {
	if v == nil {
		return ""
	}
	return celdaDecimal(*v)
}

func celdaFecha(t time.Time, zona *time.Location) string {
	return t.In(zona).Format("2006-01-02 15:04")
}

var nombresPrioridad = map[string]string{"baja": "Baja", "media": "Media", "alta": "Alta", "critica": "Crítica"}

var nombresTipo = map[string]string{"instalacion": "Instalación", "visita": "Visita técnica"}

func nombreDe(nombres map[string]string, v *string) string {
	if v == nil {
		return ""
	}
	if n, ok := nombres[*v]; ok {
		return n
	}
	return celdaTexto(*v)
}

func grupoCSV(g GrupoTickets) (grupo, prioridad, categoria string) {
	switch {
	case g.Prioridad != nil:
		return "Prioridad", nombreDe(nombresPrioridad, g.Prioridad), ""
	case g.Categoria != nil:
		return "Categoría", "", celdaTextoP(g.Categoria)
	}
	return "Total", "", ""
}

func (r ReporteSLA) csv(*time.Location) tablaCSV {
	t := tablaCSV{encabezados: []string{"Grupo", "Prioridad", "Categoría", "Resueltos", "Con SLA de respuesta",
		"Respuesta en tiempo", "% respuesta en tiempo", "Con SLA de resolución", "Resolución en tiempo", "% resolución en tiempo"}}
	for _, f := range append(append([]FilaSLA{r.Total}, r.PorPrioridad...), r.PorCategoria...) {
		g, p, c := grupoCSV(f.GrupoTickets)
		t.fila(g, p, c, celdaEntero(f.Resueltos), celdaEntero(f.ConSLARespuesta), celdaEntero(f.RespuestaEnTiempo),
			celdaDecimalP(f.PorcentajeRespuesta), celdaEntero(f.ConSLAResolucion), celdaEntero(f.ResolucionEnTiempo),
			celdaDecimalP(f.PorcentajeResolucion))
	}
	return t
}

func (r ReporteResolucion) csv(*time.Location) tablaCSV {
	t := tablaCSV{encabezados: []string{"Grupo", "Prioridad", "Categoría", "Resueltos", "Promedio (min)", "Mediana (min)"}}
	for _, f := range append(append([]FilaResolucion{r.Total}, r.PorPrioridad...), r.PorCategoria...) {
		g, p, c := grupoCSV(f.GrupoTickets)
		t.fila(g, p, c, celdaEntero(f.Resueltos), celdaDecimalP(f.PromedioMin), celdaDecimalP(f.MedianaMin))
	}
	return t
}

func (r ReporteTecnicos) csv(*time.Location) tablaCSV {
	t := tablaCSV{encabezados: []string{"Técnico (id)", "Técnico", "Finalizadas", "Exitosas", "Parciales", "Fallidas",
		"% éxito", "Con tiempo en sitio", "Duración media en sitio (min)", "Tiempo pausado (min)",
		"Tiempo pausado medio (min)", "Órdenes con pausa"}}
	for _, f := range append(append([]FilaTecnico{}, r.Data...), r.Total) {
		id := celdaEntero(f.TecnicoID)
		if f.TecnicoID == 0 {
			id = ""
		}
		t.fila(id, celdaTexto(f.Nombre), celdaEntero(f.Finalizadas), celdaEntero(f.Exitosas), celdaEntero(f.Parciales),
			celdaEntero(f.Fallidas), celdaDecimalP(f.PorcentajeExito), celdaEntero(f.ConTiempoEnSitio),
			celdaDecimalP(f.DuracionMediaSitioMin), celdaDecimal(f.TiempoPausadoMin), celdaDecimalP(f.TiempoPausadoMedioMin),
			celdaEntero(f.ConPausa))
	}
	return t
}

func (r ReportePrimeraVisita) csv(*time.Location) tablaCSV {
	t := tablaCSV{encabezados: []string{"Grupo", "Tipo", "Técnico (id)", "Técnico", "Cerradas con éxito", "Evaluadas",
		"Resueltas en la primera visita", "Con retorno", "% primera visita", "En observación", "Sin servicio", "Ventana (días)"}}
	for _, f := range append(append([]FilaPrimeraVisita{r.Total}, r.PorTipo...), r.PorTecnico...) {
		grupo := "Total"
		switch {
		case f.Tipo != nil:
			grupo = "Tipo"
		case f.TecnicoID != nil:
			grupo = "Técnico"
		}
		t.fila(grupo, nombreDe(nombresTipo, f.Tipo), celdaEnteroP(f.TecnicoID), celdaTextoP(f.Tecnico),
			celdaEntero(f.Cerradas), celdaEntero(f.Evaluadas), celdaEntero(f.Resueltas), celdaEntero(f.ConRetorno),
			celdaDecimalP(f.Porcentaje), celdaEntero(f.EnObservacion), celdaEntero(f.SinServicio), strconv.Itoa(r.Dias))
	}
	return t
}

func (r ReporteReincidencias) csv(zona *time.Location) tablaCSV {
	t := tablaCSV{encabezados: []string{"Ticket", "Asunto", "Prioridad", "Categoría", "Cliente (id)", "Servicio (id)",
		"Resuelto", "Siguiente ticket", "Abierto", "Horas hasta volver", "Tickets en la ventana"}}
	for _, f := range r.Data {
		t.fila(f.Numero, celdaTexto(f.Asunto), nombreDe(nombresPrioridad, &f.Prioridad), celdaTextoP(f.Categoria),
			celdaEnteroP(f.ClienteID), celdaEnteroP(f.ServicioID), celdaFecha(f.ResueltoEn, zona), f.SiguienteNumero,
			celdaFecha(f.SiguienteCreadoEn, zona), celdaDecimal(f.HorasHastaVolver), celdaEntero(f.NuevosEnVentana))
	}
	return t
}
