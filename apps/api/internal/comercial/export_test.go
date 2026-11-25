package comercial

import "context"

// PDFCotizacionDePrueba arma el PDF de una cotización con sus datos actuales (sin guardarlo).
func (m *Modulo) PDFCotizacionDePrueba(ctx context.Context, id int64) ([]byte, error) {
	d, err := m.datosPDFCotizacion.Ejecutar(ctx, EntradaVerCotizacion{ID: id})
	if err != nil {
		return nil, err
	}
	return m.pdfCotizacion(d)
}
