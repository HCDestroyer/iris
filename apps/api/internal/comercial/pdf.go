package comercial

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/go-pdf/fpdf"
)

// PDF de la cotización y del contrato (go-pdf/fpdf: Go puro, sin cgo, fuentes base con
// cp1252 para los acentos). La salida es determinista para los mismos datos (fecha de creación
// fija y catálogo ordenado): volver a generar un PDF da los mismos bytes y el mismo SHA-256, así
// el alta del adjunto es idempotente.

// datosCotizacionPDF: lo que se imprime en la cotización.
type datosCotizacionPDF struct {
	Numero, Moneda, Notas    string
	Revision                 int32
	Creada, VigenteHasta     time.Time
	Cliente                  clientePDF
	Lineas                   []LineaCotizacion
	TotalMensual, TotalUnico string
	AdjuntoID                string
	CotizacionID             int64
}

type clientePDF struct {
	Prospecto string `json:"prospecto"`
	Nombre    string `json:"nombre"`
	Documento string `json:"documento"`
	Direccion string `json:"direccion"`
	Telefono  string `json:"telefono"`
	Correo    string `json:"correo"`
}

// ContenidoContrato es la plantilla llena (comercial.contratos.contenido): con ella se vuelve a
// generar el mismo PDF.
type ContenidoContrato struct {
	Plantilla    string            `json:"plantilla"`
	Numero       string            `json:"numero"`
	Fecha        time.Time         `json:"fecha"`
	Cliente      clientePDF        `json:"cliente"`
	Cotizacion   string            `json:"cotizacion"`
	Moneda       string            `json:"moneda"`
	Plan         string            `json:"plan"`
	TotalMensual string            `json:"totalMensual"`
	TotalUnico   string            `json:"totalUnico"`
	Lineas       []LineaCotizacion `json:"lineas"`
	Clausulas    []string          `json:"clausulas"`
	RequiereObra bool              `json:"requiereObra"`
}

// firmaPDF: la firma capturada (solo en el PDF firmado).
type firmaPDF struct {
	Nombre, Documento string
	SVG               string
	En                time.Time
}

type datosContratoPDF struct {
	ContratoID int64
	Contenido  ContenidoContrato
	Firma      *firmaPDF
	AdjuntoID  string
	Creado     time.Time
}

// PlantillaEstandar es la plantilla de contrato vigente.
const PlantillaEstandar = "estandar_v1"

// clausulasEstandar: texto de la plantilla estandar_v1.
func clausulasEstandar(plazoInstalacion int) []string {
	return []string{
		"OBJETO. El proveedor presta al cliente el servicio de acceso a internet del plan indicado, en la dirección de instalación, a partir de la fecha de instalación.",
		fmt.Sprintf("INSTALACIÓN. El proveedor programará la instalación dentro de los %d días hábiles siguientes a la firma, sujeta a la factibilidad técnica evaluada.", plazoInstalacion),
		"FACTURACIÓN. La mensualidad se factura desde la fecha en que la instalación queda activa. Los cargos únicos se facturan al instalar.",
		"EQUIPOS. Los equipos entregados en comodato son propiedad del proveedor y se devuelven al terminar el contrato en el estado en que se recibieron, salvo el desgaste normal.",
		"DATOS. El cliente autoriza el tratamiento de sus datos para la prestación del servicio y la atención de soporte.",
		"ACEPTACIÓN. El cliente declara que leyó y acepta este contrato y la cotización que lo acompaña, y lo firma de forma manuscrita digital.",
	}
}

func nuevoDocumento(creado time.Time) (*fpdf.Fpdf, func(string) string) {
	pdf := fpdf.New("P", "mm", "Letter", "")
	pdf.SetCreationDate(creado.UTC())
	pdf.SetModificationDate(creado.UTC())
	pdf.SetCatalogSort(true)
	pdf.SetCompression(true)
	pdf.SetMargins(18, 18, 18)
	pdf.SetAutoPageBreak(true, 18)
	pdf.SetCreator("Iris", true)
	tr := pdf.UnicodeTranslatorFromDescriptor("")
	return pdf, tr
}

func salida(pdf *fpdf.Fpdf) ([]byte, error) {
	var b bytes.Buffer
	if err := pdf.Output(&b); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func bloqueCliente(pdf *fpdf.Fpdf, tr func(string) string, c clientePDF) {
	pdf.SetFont("Helvetica", "B", 11)
	pdf.CellFormat(0, 6, tr("Cliente"), "", 1, "", false, 0, "")
	pdf.SetFont("Helvetica", "", 10)
	for _, f := range [][2]string{{"Nombre", c.Nombre}, {"NIT/DPI", c.Documento}, {"Dirección", c.Direccion},
		{"Teléfono", c.Telefono}, {"Correo", c.Correo}, {"Prospecto", c.Prospecto}} {
		if strings.TrimSpace(f[1]) == "" {
			continue
		}
		pdf.CellFormat(28, 5, tr(f[0]+":"), "", 0, "", false, 0, "")
		pdf.MultiCell(0, 5, tr(f[1]), "", "", false)
	}
	pdf.Ln(3)
}

func tablaLineas(pdf *fpdf.Fpdf, tr func(string) string, moneda string, lineas []LineaCotizacion, mensual, unico string) {
	pdf.SetFont("Helvetica", "B", 9)
	pdf.SetFillColor(230, 240, 240)
	anchos := []float64{80, 20, 18, 30, 32}
	for i, h := range []string{"Descripción", "Tipo", "Cant.", "P. unitario", "Total"} {
		pdf.CellFormat(anchos[i], 6, tr(h), "1", 0, "C", true, 0, "")
	}
	pdf.Ln(-1)
	pdf.SetFont("Helvetica", "", 9)
	for _, l := range lineas {
		tipo := l.Tipo
		if l.Recurrente {
			tipo += " (mes)"
		}
		desc := l.Descripcion
		if len([]rune(desc)) > 52 {
			desc = string([]rune(desc)[:51]) + "…"
		}
		pdf.CellFormat(anchos[0], 6, tr(desc), "1", 0, "", false, 0, "")
		pdf.CellFormat(anchos[1], 6, tr(tipo), "1", 0, "", false, 0, "")
		pdf.CellFormat(anchos[2], 6, l.Cantidad, "1", 0, "R", false, 0, "")
		pdf.CellFormat(anchos[3], 6, l.PrecioUnitario, "1", 0, "R", false, 0, "")
		pdf.CellFormat(anchos[4], 6, l.Total, "1", 0, "R", false, 0, "")
		pdf.Ln(-1)
	}
	pdf.SetFont("Helvetica", "B", 10)
	pdf.CellFormat(148, 7, tr("Total mensual ("+moneda+")"), "1", 0, "R", false, 0, "")
	pdf.CellFormat(32, 7, mensual, "1", 1, "R", false, 0, "")
	pdf.CellFormat(148, 7, tr("Total único: instalación, equipos y otros ("+moneda+")"), "1", 0, "R", false, 0, "")
	pdf.CellFormat(32, 7, unico, "1", 1, "R", false, 0, "")
	pdf.Ln(3)
}

func (m *Modulo) pdfCotizacion(d datosCotizacionPDF) ([]byte, error) {
	pdf, tr := nuevoDocumento(d.Creada)
	pdf.SetTitle("Cotización "+d.Numero, true)
	pdf.AddPage()
	pdf.SetFont("Helvetica", "B", 16)
	pdf.CellFormat(0, 9, tr(fmt.Sprintf("Cotización %s (revisión %d)", d.Numero, d.Revision)), "", 1, "", false, 0, "")
	pdf.SetFont("Helvetica", "", 10)
	pdf.CellFormat(0, 5, tr("Fecha: "+m.diaLocal(d.Creada)+" · Vigente hasta: "+m.diaLocal(d.VigenteHasta)), "", 1, "", false, 0, "")
	pdf.Ln(4)
	bloqueCliente(pdf, tr, d.Cliente)
	tablaLineas(pdf, tr, d.Moneda, d.Lineas, d.TotalMensual, d.TotalUnico)
	if strings.TrimSpace(d.Notas) != "" {
		pdf.SetFont("Helvetica", "B", 10)
		pdf.CellFormat(0, 6, tr("Notas"), "", 1, "", false, 0, "")
		pdf.SetFont("Helvetica", "", 9)
		pdf.MultiCell(0, 5, tr(d.Notas), "", "", false)
	}
	pdf.Ln(4)
	pdf.SetFont("Helvetica", "I", 8)
	pdf.MultiCell(0, 4, tr("La instalación está sujeta a la factibilidad técnica. Precios en "+d.Moneda+
		". Documento generado por Iris; id "+d.AdjuntoID+"."), "", "", false)
	return salida(pdf)
}

// firmaValida: la firma SVG se puede dibujar en el PDF (fpdf.SVGBasicParse: <svg width height>
// con <path d> de comandos M, L, C, Q, H, V, Z).
func firmaValida(svg string) error {
	sig, err := fpdf.SVGBasicParse([]byte(svg))
	if err != nil {
		return err
	}
	if len(sig.Segments) == 0 {
		return fmt.Errorf("la firma no tiene trazos")
	}
	return nil
}

func (m *Modulo) pdfContrato(d datosContratoPDF) ([]byte, error) {
	creado := d.Creado
	if d.Firma != nil {
		creado = d.Firma.En
	}
	c := d.Contenido
	pdf, tr := nuevoDocumento(creado)
	pdf.SetTitle("Contrato "+c.Numero, true)
	pdf.AddPage()
	pdf.SetFont("Helvetica", "B", 16)
	titulo := "Contrato de servicio " + c.Numero
	if d.Firma == nil {
		titulo += " (borrador)"
	}
	pdf.CellFormat(0, 9, tr(titulo), "", 1, "", false, 0, "")
	pdf.SetFont("Helvetica", "", 10)
	pdf.CellFormat(0, 5, tr("Fecha: "+m.diaLocal(c.Fecha)+" · Cotización: "+c.Cotizacion+" · Plantilla: "+c.Plantilla), "", 1, "", false, 0, "")
	pdf.Ln(4)
	bloqueCliente(pdf, tr, c.Cliente)
	if c.Plan != "" {
		pdf.SetFont("Helvetica", "B", 11)
		pdf.CellFormat(0, 6, tr("Plan: "+c.Plan), "", 1, "", false, 0, "")
	}
	tablaLineas(pdf, tr, c.Moneda, c.Lineas, c.TotalMensual, c.TotalUnico)
	if c.RequiereObra {
		pdf.SetFont("Helvetica", "B", 9)
		pdf.MultiCell(0, 5, tr("La instalación requiere obra adicional (factibilidad: requiere obra), confirmada por el cliente."), "", "", false)
		pdf.Ln(2)
	}
	pdf.SetFont("Helvetica", "B", 11)
	pdf.CellFormat(0, 6, tr("Cláusulas"), "", 1, "", false, 0, "")
	pdf.SetFont("Helvetica", "", 9)
	for i, cl := range c.Clausulas {
		pdf.MultiCell(0, 5, tr(fmt.Sprintf("%d. %s", i+1, cl)), "", "", false)
		pdf.Ln(1)
	}
	pdf.Ln(6)
	if d.Firma == nil {
		pdf.SetFont("Helvetica", "I", 9)
		pdf.MultiCell(0, 5, tr("Borrador para lectura: sin firma del cliente."), "", "", false)
	} else {
		sig, err := fpdf.SVGBasicParse([]byte(d.Firma.SVG))
		if err != nil {
			return nil, err
		}
		// La firma en un recuadro de 70 mm de ancho como máximo.
		escala := 70 / sig.Wd
		if h := sig.Ht * escala; h > 35 {
			escala = 35 / sig.Ht
		}
		if pdf.GetY()+sig.Ht*escala+20 > 260 {
			pdf.AddPage()
		}
		x, y := pdf.GetX(), pdf.GetY()
		pdf.SetLineWidth(0.4)
		pdf.SetDrawColor(10, 10, 10)
		pdf.SVGBasicWrite(&sig, escala) // origen: la posición actual
		pdf.SetXY(x, y+sig.Ht*escala+2)
		pdf.Line(x, pdf.GetY(), x+80, pdf.GetY())
		pdf.Ln(2)
		pdf.SetFont("Helvetica", "", 9)
		firmante := d.Firma.Nombre
		if d.Firma.Documento != "" {
			firmante += " · " + d.Firma.Documento
		}
		pdf.CellFormat(0, 5, tr("Firma del cliente: "+firmante), "", 1, "", false, 0, "")
		pdf.CellFormat(0, 5, tr("Firmado el "+d.Firma.En.In(m.cfg.ZonaHoraria).Format("02/01/2006 15:04")+" (hora de Guatemala)"), "", 1, "", false, 0, "")
	}
	pdf.Ln(4)
	pdf.SetFont("Helvetica", "I", 8)
	pdf.MultiCell(0, 4, tr("Documento generado por Iris; id "+d.AdjuntoID+"."), "", "", false)
	return salida(pdf)
}
