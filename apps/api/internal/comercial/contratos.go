package comercial

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/campo"
	comercialbd "github.com/HCDestroyer/iris/apps/api/internal/comercial/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Firma del contrato: la del cliente capturada como imagen (SVG de trazos, igual que la firma de
// la orden de campo) en el teléfono o tableta del vendedor, frente al cliente. Se eligió sobre
// la aceptación por enlace con token: no abre un endpoint público sin sesión (ni su canal de
// envío, que Iris no tiene hacia el cliente), y deja la firma dibujada en el PDF.

// EntradaCrearContrato genera el contrato con la cotización aceptada.
type EntradaCrearContrato struct {
	ProspectoID int64 `json:"-"`
	Version     int32 `json:"version" minimum:"1" doc:"Versión del prospecto"`
}

// EntradaVerContrato: GET /contratos/{id}.
type EntradaVerContrato struct {
	ID int64 `path:"id" minimum:"1"`
}

// EntradaFirmar registra la firma del cliente.
type EntradaFirmar struct {
	ContratoID        int64    `json:"-"`
	Version           int32    `json:"version" minimum:"1" doc:"Versión del contrato"`
	FirmanteNombre    string   `json:"firmanteNombre" minLength:"1" maxLength:"200"`
	FirmanteDocumento *string  `json:"firmanteDocumento,omitempty" maxLength:"40" doc:"DPI o NIT de quien firma"`
	SVG               string   `json:"svg" minLength:"10" maxLength:"200000" doc:"Firma en SVG: <svg width=\"…\" height=\"…\"> con <path d=\"…\"> (comandos M, L, C, Q, H, V, Z; como jSignature o un pad de firma)" auditoria:"sensible"`
	Lat               *float64 `json:"lat,omitempty" minimum:"-90" maximum:"90"`
	Lon               *float64 `json:"lon,omitempty" minimum:"-180" maximum:"180"`
}

// entradaFirmaLista: la firma con el PDF firmado ya guardado.
type entradaFirmaLista struct {
	EntradaFirmar
	adjunto uuid.UUID
	sha     string
	en      time.Time
}

// MarshalJSON de la auditoría: sin el SVG (dato sensible), con el adjunto y su SHA-256.
func (e entradaFirmaLista) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{"contrato_id": e.ContratoID, "version": e.Version, "firmante_nombre": e.FirmanteNombre,
		"firmante_documento": e.FirmanteDocumento, "svg": auditoria.Mascara, "lat": e.Lat, "lon": e.Lon,
		"pdf_adjunto_id": e.adjunto, "pdf_sha256": e.sha})
}

func aContrato(ctx context.Context, q *comercialbd.Queries, f comercialbd.ComercialContrato) (Contrato, error) {
	c := Contrato{ID: f.ID, Numero: f.Numero, ProspectoID: f.ProspectoID, CotizacionID: f.CotizacionID, Plantilla: f.Plantilla,
		Estado: f.Estado, FirmanteNombre: f.FirmanteNombre, FirmanteDocumento: f.FirmanteDocumento, FirmadoEn: f.FirmadoEn,
		FirmadoPor: f.FirmadoPor, MotivoAnulacion: f.MotivoAnulacion, CreadoEn: f.CreatedAt, Version: f.Version}
	var err error
	if c.PDF, err = documento(ctx, q, f.PdfAdjuntoID); err != nil {
		return c, err
	}
	c.PDFFirmado, err = documento(ctx, q, f.PdfFirmadoID)
	return c, err
}

// anularContratoVivo anula el contrato borrador o firmado del prospecto, si hay.
func anularContratoVivo(ctx context.Context, t *auditoria.Tx, q *comercialbd.Queries, prospecto int64, motivo string) error {
	c, err := q.ContratoVivoDeProspecto(ctx, prospecto)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := anular(ctx, t, q, c, prospecto, motivo); err != nil {
		return err
	}
	// Un regenerado de la instalación por firmar convive con el firmado: también se anula.
	return anularPorFirmarEnOrden(ctx, t, q, prospecto, motivo)
}

// anularPorFirmarEnOrden anula el contrato regenerado en la instalación que aún no se firmó (B20).
func anularPorFirmarEnOrden(ctx context.Context, t *auditoria.Tx, q *comercialbd.Queries, prospecto int64, motivo string) error {
	c, err := q.ContratoPorFirmarEnOrden(ctx, prospecto)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return anular(ctx, t, q, c, prospecto, motivo)
}

func anular(ctx context.Context, t *auditoria.Tx, q *comercialbd.Queries, c comercialbd.ComercialContrato, prospecto int64, motivo string) error {
	f, err := q.AnularContrato(ctx, comercialbd.AnularContratoParams{Motivo: &motivo, ID: c.ID})
	if err != nil {
		return err
	}
	t.RegistrarCambios("contrato", f.ID, map[string]any{"estado": c.Estado}, map[string]any{"estado": f.Estado, "motivo_anulacion": motivo})
	return t.Emitir("contrato.anulado", map[string]any{"contrato_id": f.ID, "prospecto_id": prospecto, "de": c.Estado, "motivo": motivo})
}

// ordenDeContrato: la orden de instalación donde se firma un contrato regenerado (B20); nil en
// los demás.
func ordenDeContrato(c comercialbd.ComercialContrato) *int64 {
	var v struct {
		OrdenID *int64 `json:"ordenId"`
	}
	if err := json.Unmarshal(c.Contenido, &v); err != nil {
		return nil
	}
	return v.OrdenID
}

func (m *Modulo) nuevoCrearContratoCaso() *auditoria.Caso[EntradaCrearContrato, Contrato] {
	return auditoria.Nuevo(auditoria.Caso[EntradaCrearContrato, Contrato]{
		Accion: "contratos.crear", Permiso: nucleo.PermContratosCrear,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaCrearContrato) (Contrato, error) {
			q := comercialbd.New(t)
			antes, err := tomar(ctx, t, q, in.ProspectoID, nucleo.PermContratosCrear, &in.Version)
			if err != nil {
				return Contrato{}, err
			}
			if err := exigirEstado(antes, "generar el contrato de", EstadoCotizado); err != nil {
				return Contrato{}, err
			}
			fa, err := exigirFactible(ctx, q, antes.ID)
			if err != nil {
				return Contrato{}, err
			}
			cot, err := q.CotizacionViva(ctx, antes.ID)
			if errors.Is(err, pgx.ErrNoRows) || (err == nil && cot.Estado != "aceptada") {
				return Contrato{}, nucleo.ReglaNegocio("cotizacion_no_aceptada", "El contrato se genera con la cotización aceptada por el cliente")
			}
			if err != nil {
				return Contrato{}, err
			}
			if err := m.exigirVigente(cot); err != nil {
				return Contrato{}, err
			}
			c, err := m.aCotizacion(ctx, q, cot)
			if err != nil {
				return Contrato{}, err
			}
			contenido := ContenidoContrato{
				Plantilla: PlantillaEstandar, Fecha: m.ahora(), Cliente: clienteDe(antes), Cotizacion: cot.Numero, Moneda: cot.Moneda,
				TotalMensual: c.TotalMensual, TotalUnico: c.TotalUnico, Lineas: c.Lineas, Clausulas: clausulasEstandar(10),
				RequiereObra: fa.Resultado == FactibleRequiereObra,
			}
			for _, l := range c.Lineas {
				if l.Tipo == "plan" {
					contenido.Plan = l.Descripcion
				}
			}
			adjunto, err := uuid.NewV7()
			if err != nil {
				return Contrato{}, err
			}
			var pdfID *uuid.UUID
			if m.cfg.Documentos != nil {
				pdfID = &adjunto
			}
			crudo, err := json.Marshal(contenido)
			if err != nil {
				return Contrato{}, err
			}
			f, err := q.CrearContrato(ctx, comercialbd.CrearContratoParams{EmpresaID: antes.EmpresaID, ProspectoID: antes.ID,
				CotizacionID: cot.ID, Plantilla: PlantillaEstandar, Contenido: crudo, PdfAdjuntoID: pdfID})
			if err != nil {
				return Contrato{}, err
			}
			// El número se conoce al insertar: queda también en el contenido.
			contenido.Numero = f.Numero
			if crudo, err = json.Marshal(contenido); err != nil {
				return Contrato{}, err
			}
			if _, err := t.Exec(ctx, "UPDATE comercial.contratos SET contenido = $1 WHERE id = $2", crudo, f.ID); err != nil {
				return Contrato{}, err
			}
			despues := antes
			despues.Estado = EstadoContrato
			p, err := guardar(ctx, t, q, antes, despues)
			if err != nil {
				return Contrato{}, err
			}
			t.FijarEntidad("contrato", f.ID)
			t.RegistrarCambios("contrato", f.ID, nil, map[string]any{"numero": f.Numero, "cotizacion_id": f.CotizacionID, "plantilla": f.Plantilla, "estado": f.Estado})
			if err := historial(ctx, t, q, p.ID, "contrato", nil, nil, nil, map[string]any{"contrato_id": f.ID, "numero": f.Numero}); err != nil {
				return Contrato{}, err
			}
			if err := t.Emitir("contrato.creado", map[string]any{"contrato_id": f.ID, "prospecto_id": p.ID, "cotizacion_id": cot.ID, "numero": f.Numero}); err != nil {
				return Contrato{}, err
			}
			if err := cambioDeEstado(ctx, t, q, antes, p, nil); err != nil {
				return Contrato{}, err
			}
			return aContrato(ctx, q, f)
		},
	})
}

// CrearContrato genera el contrato y su PDF borrador (para leer antes de firmar).
func (m *Modulo) CrearContrato(ctx context.Context, in EntradaCrearContrato) (Contrato, error) {
	c, err := m.crearContrato.Ejecutar(ctx, in)
	if err != nil {
		return Contrato{}, err
	}
	return m.conPDFBorrador(ctx, c), nil
}

func (m *Modulo) conPDFBorrador(ctx context.Context, c Contrato) Contrato {
	if c.PDF == nil || c.PDF.Estado == "completo" || m.cfg.Documentos == nil {
		return c
	}
	d, err := m.datosPDFBorrador.Ejecutar(ctx, EntradaVerContrato{ID: c.ID})
	if err == nil {
		var contenido []byte
		if contenido, err = m.pdfContrato(d); err == nil {
			pdf := PDF{ID: c.PDF.AdjuntoID, Entidad: EntidadContrato, EntidadID: idTexto(c.ID), Contenido: contenido}
			if err = m.guardarPDF(ctx, pdf); err == nil {
				sha := pdf.SHA256()
				c.PDF.Estado, c.PDF.SHA256 = "completo", &sha
			}
		}
	}
	if err != nil {
		slog.WarnContext(ctx, "comercial: no se pudo guardar el PDF del contrato", "contrato", c.ID, "err", err)
	}
	return c
}

// contratoVisible: el contrato y su prospecto, visibles para el actor (404 si no).
func contratoVisible(ctx context.Context, t *auditoria.Tx, q *comercialbd.Queries, id int64) (comercialbd.ComercialContrato, comercialbd.ComercialProspecto, error) {
	c, err := q.ContratoPorID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, comercialbd.ComercialProspecto{}, nucleo.NoEncontrado("El contrato no existe")
	}
	if err != nil {
		return c, comercialbd.ComercialProspecto{}, err
	}
	p, err := leer(ctx, t, q, c.ProspectoID)
	var pr *nucleo.Problema
	if errors.As(err, &pr) && pr.Status == 404 {
		return c, p, nucleo.NoEncontrado("El contrato no existe")
	}
	t.FijarEntidad("contrato", c.ID)
	return c, p, err
}

func (m *Modulo) nuevoVerContrato() *auditoria.Caso[EntradaVerContrato, Contrato] {
	return auditoria.Nuevo(auditoria.Caso[EntradaVerContrato, Contrato]{
		Accion: "prospectos.ver", Permiso: nucleo.PermProspectosVer, AuditarExito: soloLecturas[Contrato],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaVerContrato) (Contrato, error) {
			q := comercialbd.New(t)
			c, _, err := contratoVisible(ctx, t, q, in.ID)
			if err != nil {
				return Contrato{}, err
			}
			return aContrato(ctx, q, c)
		},
	})
}

func datosContrato(f comercialbd.ComercialContrato) (datosContratoPDF, error) {
	d := datosContratoPDF{ContratoID: f.ID, Creado: f.CreatedAt}
	if f.PdfAdjuntoID != nil {
		d.AdjuntoID = f.PdfAdjuntoID.String()
	}
	return d, json.Unmarshal(f.Contenido, &d.Contenido)
}

func (m *Modulo) nuevoDatosPDFBorrador() *auditoria.Caso[EntradaVerContrato, datosContratoPDF] {
	return auditoria.Nuevo(auditoria.Caso[EntradaVerContrato, datosContratoPDF]{
		Accion: "contratos.crear", Permiso: nucleo.PermContratosCrear, AuditarExito: soloLecturas[datosContratoPDF],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaVerContrato) (datosContratoPDF, error) {
			q := comercialbd.New(t)
			f, _, err := contratoVisible(ctx, t, q, in.ID)
			if err != nil {
				return datosContratoPDF{}, err
			}
			return datosContrato(f)
		},
	})
}

// GenerarPDFContrato vuelve a generar el PDF borrador si no quedó guardado (POST
// /contratos/{id}/pdf).
func (m *Modulo) GenerarPDFContrato(ctx context.Context, in EntradaVerContrato) (Contrato, error) {
	c, err := m.VerContrato.Ejecutar(ctx, in)
	if err != nil {
		return Contrato{}, err
	}
	if c.PDF == nil {
		return c, nucleo.NoDisponible("No hay almacenamiento de documentos configurado")
	}
	if c.PDF.Estado != "completo" {
		d, err := m.datosPDFBorrador.Ejecutar(ctx, in)
		if err != nil {
			return Contrato{}, err
		}
		contenido, err := m.pdfContrato(d)
		if err != nil {
			return Contrato{}, err
		}
		if err := m.guardarPDF(ctx, PDF{ID: c.PDF.AdjuntoID, Entidad: EntidadContrato, EntidadID: idTexto(c.ID), Contenido: contenido}); err != nil {
			return Contrato{}, err
		}
	}
	return m.VerContrato.Ejecutar(ctx, in)
}

// --- Firma ---------------------------------------------------------------------------------

// exigirFirmable: el contrato y su prospecto admiten la firma (contratos.firmar con alcance).
func (m *Modulo) exigirFirmable(ctx context.Context, t *auditoria.Tx, q *comercialbd.Queries, in EntradaFirmar, candado bool) (comercialbd.ComercialContrato, comercialbd.ComercialProspecto, error) {
	c, p, err := contratoVisible(ctx, t, q, in.ContratoID)
	if err != nil {
		return c, p, err
	}
	if err := verificarAcceso(ctx, string(nucleo.PermContratosFirmar), p); err != nil {
		return c, p, err
	}
	if candado {
		if p, err = tomar(ctx, t, q, p.ID, nucleo.PermContratosFirmar, nil); err != nil {
			return c, p, err
		}
		if c, err = q.ContratoPorIDParaActualizar(ctx, c.ID); err != nil {
			return c, p, err
		}
		t.FijarEntidad("contrato", c.ID)
	}
	if err := nucleo.ExigirVersion(in.Version, c.Version, "contrato", c.ID); err != nil {
		return c, p, err
	}
	// El contrato regenerado en la instalación (B20) solo lo firma el titular en sitio, con la
	// operación del lote orden.firmar_contrato (técnico asignado, orden en sitio): nunca desde aquí.
	if ordenDeContrato(c) != nil {
		return c, p, nucleo.Conflicto("se_firma_en_la_orden",
			"Este contrato se regeneró en la instalación: lo firma el titular en sitio, en la orden "+strconv.FormatInt(*ordenDeContrato(c), 10))
	}
	return c, p, m.contratoFirmable(ctx, q, c, p)
}

// contratoFirmable: el contrato está en borrador y su prospecto en estado contrato; o es el
// contrato regenerado en la instalación (contenido.ordenId, B20) de un prospecto ganado, con su
// revisión aún por aceptar (la firma la acepta; solo por FirmarEnOrdenEnTx: /firmar lo rechaza con
// se_firma_en_la_orden). La cotización, vigente.
func (m *Modulo) contratoFirmable(ctx context.Context, q *comercialbd.Queries, c comercialbd.ComercialContrato, p comercialbd.ComercialProspecto) error {
	enOrden := ordenDeContrato(c) != nil
	switch {
	case c.Estado != "borrador":
		return transicionInvalida("Solo se firma un contrato en borrador")
	case enOrden && (p.Estado != EstadoGanado || p.OrdenID == nil || *p.OrdenID != *ordenDeContrato(c)):
		return transicionInvalida("El contrato regenerado en la instalación se firma con el prospecto ganado y su orden")
	case !enOrden && p.Estado != EstadoContrato:
		return transicionInvalida("Solo se firma un contrato en borrador de un prospecto en estado contrato")
	}
	cot, err := q.CotizacionPorID(ctx, c.CotizacionID)
	if err != nil {
		return err
	}
	if enOrden && cot.Estado != "vigente" {
		return transicionInvalida("La revisión de este contrato ya no está vigente (otra la reemplazó)")
	}
	return m.exigirVigente(cot)
}

func (m *Modulo) nuevoPrepararFirma() *auditoria.Caso[EntradaFirmar, datosContratoPDF] {
	return auditoria.Nuevo(auditoria.Caso[EntradaFirmar, datosContratoPDF]{
		Accion: "contratos.firmar", Permiso: nucleo.PermContratosFirmar, AuditarExito: soloLecturas[datosContratoPDF],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaFirmar) (datosContratoPDF, error) {
			q := comercialbd.New(t)
			c, _, err := m.exigirFirmable(ctx, t, q, in, false)
			if err != nil {
				return datosContratoPDF{}, err
			}
			nombre, documento, svg, err := validarFirma(in.FirmanteNombre, in.FirmanteDocumento, in.SVG, in.Lat, in.Lon)
			if err != nil {
				return datosContratoPDF{}, err
			}
			d, err := datosContrato(c)
			if err != nil {
				return d, err
			}
			d.Firma = &firmaPDF{Nombre: nombre, Documento: deref(documento), SVG: svg, En: m.ahora()}
			return d, nil
		},
	})
}

// FirmarContrato valida la firma, genera el PDF firmado, lo guarda como adjunto (fuera de la
// transacción) y marca el contrato firmado con el SHA-256 del PDF en la auditoría.
func (m *Modulo) FirmarContrato(ctx context.Context, in EntradaFirmar) (Contrato, error) {
	d, err := m.prepararFirma.Ejecutar(ctx, in)
	if err != nil {
		return Contrato{}, err
	}
	if m.cfg.Documentos == nil {
		return Contrato{}, nucleo.NoDisponible("No hay almacenamiento de documentos configurado: el contrato firmado debe guardarse")
	}
	id, err := uuid.NewV7()
	if err != nil {
		return Contrato{}, err
	}
	d.AdjuntoID = id.String()
	contenido, err := m.pdfContrato(d)
	if err != nil {
		return Contrato{}, err
	}
	pdf := PDF{ID: id, Entidad: EntidadContrato, EntidadID: idTexto(d.ContratoID), Contenido: contenido}
	if err := m.guardarPDF(ctx, pdf); err != nil {
		return Contrato{}, err
	}
	in.SVG = d.Firma.SVG
	return m.firmar.Ejecutar(ctx, entradaFirmaLista{EntradaFirmar: in, adjunto: id, sha: pdf.SHA256(), en: d.Firma.En})
}

func (m *Modulo) nuevoFirmar() *auditoria.Caso[entradaFirmaLista, Contrato] {
	return auditoria.Nuevo(auditoria.Caso[entradaFirmaLista, Contrato]{
		Accion: "contratos.firmar", Permiso: nucleo.PermContratosFirmar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaFirmaLista) (Contrato, error) {
			q := comercialbd.New(t)
			c, p, err := m.exigirFirmable(ctx, t, q, in.EntradaFirmar, true)
			if err != nil {
				return Contrato{}, err
			}
			f, err := firmarEnTx(ctx, t, q, c, p, in)
			if err != nil {
				return Contrato{}, err
			}
			return aContrato(ctx, q, f)
		},
	})
}

// exigirVigente: la cotización sigue vigente (422 cotizacion_vencida si no).
func (m *Modulo) exigirVigente(c comercialbd.ComercialCotizacion) error {
	if c.VigenteHasta.After(m.ahora()) {
		return nil
	}
	return nucleo.ReglaNegocio("cotizacion_vencida", "La cotización "+c.Numero+" venció el "+m.diaLocal(c.VigenteHasta)+"; crea una revisión nueva")
}

// firmarEnTx marca el contrato firmado (con el PDF firmado ya guardado). Con el contrato regenerado
// en la instalación (B20) es el cambio de contrato: antes de firmar, el firmado anterior se anula y
// su cotización aceptada queda reemplazada; después, la revisión queda aceptada (la firma en sitio
// del titular cubre la subida de precio, B6).
func firmarEnTx(ctx context.Context, t *auditoria.Tx, q *comercialbd.Queries, c comercialbd.ComercialContrato,
	p comercialbd.ComercialProspecto, in entradaFirmaLista) (comercialbd.ComercialContrato, error) {
	a, err := q.EstadoAdjunto(ctx, in.adjunto)
	if err != nil || a.Estado != "completo" || a.Sha256 != in.sha {
		return c, nucleo.NoDisponible("El PDF firmado no quedó guardado; vuelve a firmar")
	}
	orden := ordenDeContrato(c)
	if orden != nil {
		motivo := "Reemplazado por el contrato " + c.Numero + " firmado en la instalación"
		if viejo, err := q.ContratoFirmadoDe(ctx, p.ID); err == nil {
			if err := anular(ctx, t, q, viejo, p.ID, motivo); err != nil {
				return c, err
			}
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return c, err
		}
		if aceptada, err := q.CotizacionAceptadaDe(ctx, p.ID); err == nil {
			if err := q.MarcarReemplazada(ctx, aceptada.ID); err != nil {
				return c, err
			}
			if err := q.EnlazarReemplazo(ctx, comercialbd.EnlazarReemplazoParams{ReemplazadaPor: &c.CotizacionID, ID: aceptada.ID}); err != nil {
				return c, err
			}
			t.RegistrarCambios("cotizacion", aceptada.ID, map[string]any{"estado": aceptada.Estado},
				map[string]any{"estado": "reemplazada", "reemplazada_por": c.CotizacionID})
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return c, err
		}
	}
	nombre, _ := texto("firmanteNombre", in.FirmanteNombre, 1, 200)
	documento, _ := textoOpcional("firmanteDocumento", in.FirmanteDocumento, 40)
	f, err := q.FirmarContrato(ctx, comercialbd.FirmarContratoParams{
		FirmanteNombre: &nombre, FirmanteDocumento: documento, FirmaSvg: &in.SVG, FirmaLat: in.Lat, FirmaLon: in.Lon,
		FirmadoEn: &in.en, FirmadoPor: actorUsuario(t), PdfFirmadoID: &in.adjunto, PdfFirmadoSha256: &in.sha,
		ID: c.ID, Version: c.Version,
	})
	if err != nil {
		return f, nucleo.VerificarVersion(err, "contrato", c.ID)
	}
	t.RegistrarCambios("contrato", f.ID, c, f)
	t.Anotar(map[string]any{"pdf_firmado_id": in.adjunto, "pdf_firmado_sha256": in.sha})
	if orden != nil {
		cot, err := q.CotizacionPorIDParaActualizar(ctx, c.CotizacionID)
		if err != nil {
			return f, err
		}
		aceptada, err := q.AceptarCotizacion(ctx, comercialbd.AceptarCotizacionParams{Ahora: &in.en, AceptadaPor: actorUsuario(t), ID: cot.ID, Version: cot.Version})
		if err != nil {
			return f, nucleo.VerificarVersion(err, "cotizacion", cot.ID)
		}
		t.RegistrarCambios("cotizacion", aceptada.ID, map[string]any{"estado": cot.Estado}, map[string]any{"estado": aceptada.Estado, "aceptada_en": aceptada.AceptadaEn})
		if err := t.Emitir("cotizacion.aceptada", map[string]any{"cotizacion_id": aceptada.ID, "prospecto_id": p.ID, "numero": aceptada.Numero}); err != nil {
			return f, err
		}
	}
	if _, err := guardar(ctx, t, q, p, p); err != nil {
		return f, err
	}
	if err := historial(ctx, t, q, p.ID, "contrato_firmado", nil, nil, nil, map[string]any{
		"contrato_id": f.ID, "numero": f.Numero, "pdf_sha256": in.sha, "orden_id": orden,
	}); err != nil {
		return f, err
	}
	return f, t.Emitir("contrato.firmado", map[string]any{
		"contrato_id": f.ID, "prospecto_id": p.ID, "numero": f.Numero, "adjunto_id": in.adjunto, "sha256": in.sha, "orden_id": orden,
	})
}

// --- Firma del contrato regenerado en la orden (B20) --------------------------------------------

// La firma del titular es la del contrato regenerado a su nombre, en la orden (contrato argosisp.md
// 0.10.0 §3.4.8, ajuste e de §10.5): el técnico la toma en sitio con la operación del lote
// orden.firmar_contrato (campo.ContratosEnOrden), sin señal como la firma de la orden. Comercial
// arma y guarda el PDF firmado fuera de la transacción (PrepararFirmaEnOrden) y marca el contrato
// firmado en la transacción de la operación (FirmarEnOrdenEnTx).

func (m *Modulo) nuevoPrepararFirmaEnOrden() *auditoria.Caso[campo.EntradaFirmaContratoSync, datosContratoPDF] {
	return auditoria.Nuevo(auditoria.Caso[campo.EntradaFirmaContratoSync, datosContratoPDF]{
		Accion: "ordenes.firmar_contrato", Permiso: nucleo.PermOrdenesCambiarTitular, AuditarExito: soloLecturas[datosContratoPDF],
		Logica: func(ctx context.Context, t *auditoria.Tx, in campo.EntradaFirmaContratoSync) (datosContratoPDF, error) {
			q := comercialbd.New(t)
			c, _, err := contratoDeOrden(ctx, t, q, in.OrdenID, in.ContratoID, false)
			if err != nil {
				return datosContratoPDF{}, err
			}
			nombre, documento, svg, err := validarFirma(in.FirmanteNombre, in.FirmanteDocumento, in.SVG, in.Lat, in.Lon)
			if err != nil {
				return datosContratoPDF{}, err
			}
			d, err := datosContrato(c)
			if err != nil {
				return d, err
			}
			d.Firma = &firmaPDF{Nombre: nombre, Documento: deref(documento), SVG: svg, En: m.ahora()}
			return d, nil
		},
	})
}

// contratoDeOrden: el contrato regenerado de esa orden, del técnico asignado (404 si no), firmable.
// candado: con el prospecto y el contrato tomados (en la transacción de la operación).
func contratoDeOrden(ctx context.Context, t *auditoria.Tx, q *comercialbd.Queries, orden, contrato int64, candado bool) (comercialbd.ComercialContrato, comercialbd.ComercialProspecto, error) {
	var c comercialbd.ComercialContrato
	var p comercialbd.ComercialProspecto
	o, err := q.OrdenParaFirmarContrato(ctx, orden)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, p, nucleo.NoEncontrado("El contrato no existe")
	}
	if err != nil {
		return c, p, err
	}
	a := contexto.ActorDe(ctx)
	if !a.EsUsuario() || o.TecnicoID == nil || *o.TecnicoID != a.ID || !enEmpresa(ctx, o.EmpresaID) {
		return c, p, nucleo.NoEncontrado("El contrato no existe")
	}
	c, err = q.ContratoPorID(ctx, contrato)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && (ordenDeContrato(c) == nil || *ordenDeContrato(c) != orden)) {
		return c, p, nucleo.NoEncontrado("El contrato no existe")
	}
	if err != nil {
		return c, p, err
	}
	if candado {
		// Como /firmar: primero el prospecto y después el contrato (mismo orden de bloqueo).
		if p, err = q.ProspectoPorIDParaActualizar(ctx, c.ProspectoID); err != nil {
			return c, p, err
		}
		if c, err = q.ContratoPorIDParaActualizar(ctx, contrato); err != nil {
			return c, p, err
		}
	} else if p, err = q.ProspectoPorID(ctx, c.ProspectoID); err != nil {
		return c, p, err
	}
	t.FijarEntidad("contrato", c.ID)
	t.FijarEmpresa(deref(p.EmpresaID))
	if o.Estado != "en_sitio" {
		return c, p, transicionInvalida("El contrato regenerado se firma con la orden en sitio")
	}
	return c, p, nil
}

// validarFirma: nombre, documento, coordenadas y el SVG normalizado (mismas reglas que /firmar).
func validarFirma(nombre string, documento *string, svg string, lat, lon *float64) (string, *string, string, error) {
	n, err := texto("firmanteNombre", nombre, 1, 200)
	if err != nil {
		return "", nil, "", err
	}
	d, err := textoOpcional("firmanteDocumento", documento, 40)
	if err != nil {
		return "", nil, "", err
	}
	if err := validarCoordenadas(lat, lon); err != nil {
		return "", nil, "", err
	}
	s := strings.TrimSpace(svg)
	if strings.ContainsRune(s, 0) || len(s) > 200_000 {
		return "", nil, "", campoInvalido("svg", "Firma inválida", "svg")
	}
	s, err = normalizarFirma(s)
	if err != nil {
		return "", nil, "", nucleo.Validacion(nucleo.ErrorCampo{Field: "svg", Rule: "firma",
			Message: "La firma debe ser un SVG con width y height (0–10000) y trazos <path d> (M, L, C, Q, H, V, Z): " + err.Error()})
	}
	return n, d, s, nil
}

// PrepararFirmaEnOrden valida la firma del contrato regenerado de la orden, arma el PDF firmado y
// lo guarda (fuera de la transacción de la operación; implementa campo.ContratosEnOrden).
func (m *Modulo) PrepararFirmaEnOrden(ctx context.Context, in campo.EntradaFirmaContratoSync) (campo.FirmaContratoPreparada, error) {
	d, err := m.prepararFirmaEnOrden.Ejecutar(ctx, in)
	if err != nil {
		return campo.FirmaContratoPreparada{}, err
	}
	if m.cfg.Documentos == nil {
		return campo.FirmaContratoPreparada{}, nucleo.NoDisponible("No hay almacenamiento de documentos configurado: el contrato firmado debe guardarse")
	}
	id, err := uuid.NewV7()
	if err != nil {
		return campo.FirmaContratoPreparada{}, err
	}
	d.AdjuntoID = id.String()
	contenido, err := m.pdfContrato(d)
	if err != nil {
		return campo.FirmaContratoPreparada{}, err
	}
	pdf := PDF{ID: id, Entidad: EntidadContrato, EntidadID: idTexto(d.ContratoID), Contenido: contenido}
	if err := m.guardarPDF(ctx, pdf); err != nil {
		return campo.FirmaContratoPreparada{}, err
	}
	in.SVG = d.Firma.SVG
	return campo.FirmaContratoPreparada{Entrada: in, AdjuntoID: id, SHA256: pdf.SHA256(), En: d.Firma.En}, nil
}

// FirmarEnOrdenEnTx marca firmado el contrato regenerado en la transacción de la operación del lote
// (la orden ya está tomada por campo; implementa campo.ContratosEnOrden).
func (m *Modulo) FirmarEnOrdenEnTx(ctx context.Context, t *auditoria.Tx, l campo.FirmaContratoPreparada) (int64, error) {
	q := comercialbd.New(t)
	c, p, err := contratoDeOrden(ctx, t, q, l.Entrada.OrdenID, l.Entrada.ContratoID, true)
	if err != nil {
		return 0, err
	}
	if err := m.contratoFirmable(ctx, q, c, p); err != nil {
		return 0, err
	}
	in := entradaFirmaLista{EntradaFirmar: EntradaFirmar{ContratoID: c.ID, Version: c.Version, FirmanteNombre: l.Entrada.FirmanteNombre,
		FirmanteDocumento: l.Entrada.FirmanteDocumento, SVG: l.Entrada.SVG, Lat: l.Entrada.Lat, Lon: l.Entrada.Lon},
		adjunto: l.AdjuntoID, sha: l.SHA256, en: l.En}
	f, err := firmarEnTx(ctx, t, q, c, p, in)
	if err != nil {
		return 0, err
	}
	return f.ID, nil
}
