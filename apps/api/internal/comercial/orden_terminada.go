package comercial

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	comercialbd "github.com/HCDestroyer/iris/apps/api/internal/comercial/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Orden de instalación cancelada o fallida (B20, revisión de #280): el contrato regenerado en
// borrador ya no se puede firmar en sitio, así que se anula. La revisión de la cotización queda sin
// aceptar y el contrato firmado de la venta sigue vivo. Igual con la orden cerrada: ya no se firma
// en sitio (no queda «por firmar»). Se dispara con orden.estado_cambiada (a = cancelada, fallida o
// cerrada); idempotente.
//
// Orden reabierta o reprogramada (de cancelada o fallida a otro estado abierto): Argos ya tiene el
// plan o el titular nuevo, así que el contrato se regenera en borrador para esa orden con la última
// revisión vigente sin aceptar (con vigencia nueva si venció, y su PDF de nuevo): la subida de
// precio vuelve a esperar la firma en sitio. La revisión puede no tener contrato todavía: Argos
// pudo aceptar el cambio con la orden ya terminada (RevisarVenta la crea sin contrato).
//
// Los dos leen el estado de la orden con candado compartido antes de tomar el prospecto (como
// RevisarVenta): no se cruzan con cancelar ni reabrir.

// ordenTerminal: cancelada o fallida (se puede reabrir o reprogramar).
func ordenTerminal(estado string) bool { return estado == "cancelada" || estado == "fallida" }

// sinFirmaEnSitio: estados de la orden en los que el contrato regenerado no se puede firmar en
// sitio: cancelada o fallida, y cerrada (que no se reabre).
func sinFirmaEnSitio(estado string) bool { return ordenTerminal(estado) || estado == "cerrada" }

// EntradaOrdenTerminada: la orden que cambió de estado (orden.estado_cambiada).
type EntradaOrdenTerminada struct {
	OrdenID int64  `json:"orden_id"`
	Estado  string `json:"a"`
	// De: el estado anterior; de cancelada o fallida a un estado abierto = reabierta (→ pendiente)
	// o reprogramada (fallida → programada).
	De string `json:"de,omitempty"`
}

// ResultadoOrdenTerminada: el contrato regenerado anulado (nil si no había).
type ResultadoOrdenTerminada struct {
	ContratoID *int64 `json:"contrato_id"`
}

func (m *Modulo) nuevoOrdenTerminada() *auditoria.Caso[EntradaOrdenTerminada, ResultadoOrdenTerminada] {
	return auditoria.Nuevo(auditoria.Caso[EntradaOrdenTerminada, ResultadoOrdenTerminada]{
		Accion: "prospectos.revision_sin_firma", Acceso: auditoria.Autenticado,
		AuditarExito: func(r ResultadoOrdenTerminada) bool { return r.ContratoID != nil },
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaOrdenTerminada) (ResultadoOrdenTerminada, error) {
			r := ResultadoOrdenTerminada{}
			if err := exigirSistema(ctx); err != nil {
				return r, err
			}
			q := comercialbd.New(t)
			o, err := q.OrdenEstadoCompartido(ctx, in.OrdenID)
			if errors.Is(err, pgx.ErrNoRows) {
				return r, nil
			}
			if err != nil {
				return r, err
			}
			// Con el estado leído de la orden, no el del evento (pudo reabrirse después).
			if !sinFirmaEnSitio(o.Estado) {
				return r, nil
			}
			id, err := q.ProspectoGanadoDeOrden(ctx, &in.OrdenID)
			if errors.Is(err, pgx.ErrNoRows) {
				return r, nil
			}
			if err != nil {
				return r, err
			}
			p, err := tomar(ctx, t, q, id, nucleo.PermProspectosGanar, nil)
			if err != nil {
				return r, err
			}
			c, err := q.ContratoPorFirmarEnOrden(ctx, p.ID)
			if errors.Is(err, pgx.ErrNoRows) {
				return r, nil
			}
			if err != nil {
				return r, err
			}
			if orden := ordenDeContrato(c); orden == nil || *orden != in.OrdenID {
				return r, nil
			}
			t.FijarEntidad("contrato", c.ID)
			t.FijarEmpresa(deref(p.EmpresaID))
			anulado, err := anularSinFirma(ctx, t, q, p, c, o, MotivoCierreSinFirma)
			if err != nil || !anulado {
				return r, err
			}
			r.ContratoID = &c.ID
			return r, nil
		},
	})
}

// EventoContratoSinFirma: la orden quedó cerrada con el contrato regenerado sin firmar (un
// supervisor la cerró sin la firma, o Argos aceptó un cambio tarde): Argos tiene el plan o el
// titular nuevo sin la firma de B6. Va a la campana de los supervisores de la orden.
const EventoContratoSinFirma = "orden.contrato_sin_firma"

// Motivos de orden.contrato_sin_firma.
const (
	MotivoCierreSinFirma   = "cierre_sin_firma"   // un supervisor cerró la orden sin la firma
	MotivoCambioTrasCierre = "cambio_tras_cierre" // Argos aceptó el cambio con la orden ya cerrada
)

// anularSinFirma anula el contrato regenerado solo si sigue en borrador (false si ya se firmó o se
// anuló: nada que hacer). Con la orden cerrada, emite orden.contrato_sin_firma en la misma
// transacción.
func anularSinFirma(ctx context.Context, t *auditoria.Tx, q *comercialbd.Queries, p comercialbd.ComercialProspecto,
	c comercialbd.ComercialContrato, o comercialbd.OrdenEstadoCompartidoRow, motivoCodigo string) (bool, error) {
	motivo := "La orden " + o.Numero + " quedó " + o.Estado + ": el contrato regenerado ya no se firma en sitio"
	f, err := q.AnularBorrador(ctx, comercialbd.AnularBorradorParams{Motivo: &motivo, ID: c.ID})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	t.RegistrarCambios("contrato", f.ID, map[string]any{"estado": c.Estado}, map[string]any{"estado": f.Estado, "motivo_anulacion": motivo})
	if err := t.Emitir("contrato.anulado", map[string]any{"contrato_id": f.ID, "prospecto_id": p.ID, "de": c.Estado, "motivo": motivo}); err != nil {
		return false, err
	}
	if err := historial(ctx, t, q, p.ID, "contrato_anulado", nil, nil, &motivo, map[string]any{"contrato_id": c.ID, "orden_id": o.ID}); err != nil {
		return false, err
	}
	if o.Estado != "cerrada" {
		return true, nil
	}
	cot, err := q.CotizacionPorID(ctx, c.CotizacionID)
	if err != nil {
		return false, err
	}
	operacion := operacionDeRevision(cot.Idempotencia)
	return true, emitirContratoSinFirma(t, sinFirma{orden: o.ID, p: p, contrato: &c.ID, cotizacion: &c.CotizacionID, operacion: operacion, motivo: motivoCodigo})
}

// sinFirma: los datos de orden.contrato_sin_firma.
type sinFirma struct {
	orden      int64
	p          comercialbd.ComercialProspecto
	contrato   *int64  // nil: la revisión se creó sin contrato (o no se pudo crear)
	cotizacion *int64  // la revisión; nil si no se pudo crear
	operacion  *string // cambiar_plan o cambiar_titular
	motivo     string
	clave      string // la del pendiente, cuando no hay revisión
	detalle    *string
}

// emitirContratoSinFirma: orden.contrato_sin_firma, aviso a supervisión.
func emitirContratoSinFirma(t *auditoria.Tx, s sinFirma) error {
	payload := map[string]any{"orden_id": s.orden, "prospecto_id": s.p.ID, "empresa_id": s.p.EmpresaID,
		"contrato_id": s.contrato, "cotizacion_id": s.cotizacion, "operacion": s.operacion, "motivo": s.motivo}
	if s.clave != "" {
		payload["clave"] = s.clave
	}
	if s.detalle != nil {
		payload["detalle"] = *s.detalle
	}
	return t.Emitir(EventoContratoSinFirma, payload)
}

// Motivos diagnósticos de orden.contrato_sin_firma: Argos aceptó el cambio pero comercial no pudo
// crear la revisión, así que no habrá contrato que firmar y la orden no cierra sin un supervisor.
const (
	MotivoSinCotizacionViva = "sin_cotizacion_viva" // el prospecto no tiene cotización aceptada ni revisión
	MotivoSinPlan           = "sin_plan"            // el cambio de plan no trae plan_id
	MotivoRevisionRechazada = "revision_rechazada"  // la revisión dio un error de negocio (detalle: el código)
	MotivoSinRegenerar      = "sin_regenerar"       // la orden se cerró antes de regenerar el contrato tras reabrirla
)

// operacionDeClave: cambiar_plan o cambiar_titular según la clave del pendiente.
func operacionDeClave(clave string) *string {
	idem := claveRevision(clave)
	return operacionDeRevision(&idem)
}

// EntradaSinRevision: un cambio hecho en Argos cuya revisión no se pudo crear (motivo diagnóstico).
type EntradaSinRevision struct {
	OrdenID int64
	Clave   string
	Motivo  string
	Detalle *string
}

func (m *Modulo) nuevoSinRevision() *auditoria.Caso[EntradaSinRevision, bool] {
	return auditoria.Nuevo(auditoria.Caso[EntradaSinRevision, bool]{
		Accion: "prospectos.revision_imposible", Acceso: auditoria.Autenticado,
		AuditarExito: func(r bool) bool { return r },
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaSinRevision) (bool, error) {
			if err := exigirSistema(ctx); err != nil {
				return false, err
			}
			q := comercialbd.New(t)
			id, err := q.ProspectoGanadoDeOrden(ctx, &in.OrdenID)
			if errors.Is(err, pgx.ErrNoRows) {
				return false, nil
			}
			if err != nil {
				return false, err
			}
			p, err := q.ProspectoPorID(ctx, id)
			if err != nil {
				return false, err
			}
			t.FijarEntidad("prospecto", p.ID)
			t.FijarEmpresa(deref(p.EmpresaID))
			return true, emitirContratoSinFirma(t, sinFirma{orden: in.OrdenID, p: p, operacion: operacionDeClave(in.Clave), motivo: in.Motivo,
				clave: in.Clave, detalle: in.Detalle})
		},
	})
}

// SinRevision avisa a supervisión que un cambio hecho en Argos quedó sin revisión (actor sistema).
func (m *Modulo) SinRevision(ctx context.Context, in EntradaSinRevision) error {
	_, err := m.sinRevision.Ejecutar(ctx, in)
	return err
}

// operacionDeRevision: cambiar_plan o cambiar_titular según la clave de la revisión (nil si no).
func operacionDeRevision(idem *string) *string {
	switch {
	case idem == nil:
		return nil
	case strings.HasPrefix(*idem, claveRevision("iris:plan:")):
		return ptr("cambiar_plan")
	case strings.HasPrefix(*idem, claveRevision("iris:titular:")):
		return ptr("cambiar_titular")
	}
	return nil
}

// ResultadoOrdenReabierta: el contrato regenerado de nuevo (nil si no hacía falta).
type ResultadoOrdenReabierta struct {
	ContratoID *int64 `json:"contrato_id"`
	// Renovada: la revisión a la que se le dio vigencia nueva (su PDF se vuelve a armar).
	Renovada *int64 `json:"renovada,omitempty"`
}

func (m *Modulo) nuevoOrdenReabierta() *auditoria.Caso[EntradaOrdenTerminada, ResultadoOrdenReabierta] {
	return auditoria.Nuevo(auditoria.Caso[EntradaOrdenTerminada, ResultadoOrdenReabierta]{
		Accion: "prospectos.revision_por_firmar", Acceso: auditoria.Autenticado,
		AuditarExito: func(r ResultadoOrdenReabierta) bool { return r.ContratoID != nil },
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaOrdenTerminada) (ResultadoOrdenReabierta, error) {
			r := ResultadoOrdenReabierta{}
			if err := exigirSistema(ctx); err != nil {
				return r, err
			}
			q := comercialbd.New(t)
			o, err := q.OrdenEstadoCompartido(ctx, in.OrdenID)
			if errors.Is(err, pgx.ErrNoRows) {
				return r, nil
			}
			if err != nil {
				return r, err
			}
			// Con el estado leído de la orden: sigue abierta (no cancelada, fallida ni cerrada). Si ya
			// se cerró (un supervisor, sin la firma) con una revisión de esta orden por firmar, avisa.
			if o.Estado == "cerrada" {
				return r, avisarCerradaSinRegenerar(ctx, t, q, in.OrdenID)
			}
			if sinFirmaEnSitio(o.Estado) {
				return r, nil
			}
			id, err := q.ProspectoGanadoDeOrden(ctx, &in.OrdenID)
			if errors.Is(err, pgx.ErrNoRows) {
				return r, nil
			}
			if err != nil {
				return r, err
			}
			p, err := tomar(ctx, t, q, id, nucleo.PermProspectosGanar, nil)
			if err != nil {
				return r, err
			}
			// Ya hay uno por firmar (el evento llegó dos veces): nada.
			if _, err := q.ContratoPorFirmarEnOrden(ctx, p.ID); err == nil {
				return r, nil
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return r, err
			}
			// La última revisión de la instalación vigente (sin aceptar), tenga contrato de esta orden
			// o no (la que Argos aceptó con la orden terminada no lo tiene).
			rev, err := q.RevisionPorFirmar(ctx, p.ID)
			if errors.Is(err, pgx.ErrNoRows) {
				return r, nil
			}
			if err != nil {
				return r, err
			}
			if !rev.VigenteHasta.After(m.ahora()) {
				antes, pdfAntes := rev.VigenteHasta, rev.PdfAdjuntoID
				pdfID, err := m.nuevoPDFID()
				if err != nil {
					return r, err
				}
				if rev, err = q.RenovarVigencia(ctx, comercialbd.RenovarVigenciaParams{
					VigenteHasta: m.ahora().Add(m.cfg.VigenciaCotizacion).UTC().Truncate(time.Microsecond), PdfAdjuntoID: pdfID, ID: rev.ID}); err != nil {
					return r, err
				}
				t.RegistrarCambios("cotizacion", rev.ID, map[string]any{"vigente_hasta": antes, "pdf_adjunto_id": pdfAntes},
					map[string]any{"vigente_hasta": rev.VigenteHasta, "pdf_adjunto_id": rev.PdfAdjuntoID})
				r.Renovada = &rev.ID
			}
			var cliente clientePDF
			if err := json.Unmarshal(rev.Cliente, &cliente); err != nil {
				return r, err
			}
			t.FijarEmpresa(deref(p.EmpresaID))
			ct, err := m.regenerarContrato(ctx, t, q, p, rev, cliente, in.OrdenID)
			if err != nil {
				return r, err
			}
			t.FijarEntidad("contrato", ct.ID)
			r.ContratoID = &ct.ID
			return r, historial(ctx, t, q, p.ID, "contrato_regenerado", nil, nil, nil, map[string]any{
				"contrato_id": ct.ID, "cotizacion_id": rev.ID, "orden_id": in.OrdenID})
		},
	})
}

// OrdenReabierta regenera el contrato por firmar de una orden reabierta (actor sistema) y arma
// su PDF en borrador.
func (m *Modulo) OrdenReabierta(ctx context.Context, in EntradaOrdenTerminada) (ResultadoOrdenReabierta, error) {
	r, err := m.ordenReabierta.Ejecutar(ctx, in)
	if err != nil || r.ContratoID == nil {
		return r, err
	}
	if r.Renovada != nil {
		if cot, err := m.VerCotizacion.Ejecutar(ctx, EntradaVerCotizacion{ID: *r.Renovada}); err == nil {
			m.conPDFCotizacion(ctx, cot)
		}
	}
	if ct, err := m.VerContrato.Ejecutar(ctx, EntradaVerContrato{ID: *r.ContratoID}); err == nil {
		m.conPDFBorrador(ctx, ct)
	}
	return r, nil
}

// OrdenTerminada anula el contrato regenerado de una orden cancelada, fallida o cerrada (actor
// sistema).
func (m *Modulo) OrdenTerminada(ctx context.Context, in EntradaOrdenTerminada) (ResultadoOrdenTerminada, error) {
	return m.ordenTerminada.Ejecutar(ctx, in)
}

// suscribirOrdenTerminada: orden.estado_cambiada a cancelada, fallida o cerrada (anula), o de
// cancelada o fallida a un estado abierto: reabierta o reprogramada (regenera). Lo suscribe
// SuscribirRevisionVenta (cmd/worker).
func (m *Modulo) suscribirOrdenTerminada(bus *integracion.Bus, bd nucleo.BD, log *slog.Logger) {
	bus.Suscribir("comercial.orden_terminada", "orden.estado_cambiada", func(ctx context.Context, e integracion.Evento) error {
		var in EntradaOrdenTerminada
		if err := json.Unmarshal(e.Payload, &in); err != nil {
			log.ErrorContext(ctx, "comercial: orden.estado_cambiada ilegible", "evento", e.ID, "err", err)
			return nil
		}
		switch {
		case in.OrdenID <= 0:
			return nil
		case sinFirmaEnSitio(in.Estado):
			_, err := m.OrdenTerminada(nucleo.ConBD(ctx, bd), in)
			return err
		case ordenTerminal(in.De) && !sinFirmaEnSitio(in.Estado):
			// Reabierta (→ pendiente) o reprogramada (fallida → programada).
			_, err := m.OrdenReabierta(nucleo.ConBD(ctx, bd), in)
			return err
		}
		return nil
	})
}

// avisarCerradaSinRegenerar: la orden reabierta se cerró antes de regenerar el contrato; si el
// prospecto tiene una revisión de esta orden por firmar, emite orden.contrato_sin_firma (la campana
// es una por revisión).
func avisarCerradaSinRegenerar(ctx context.Context, t *auditoria.Tx, q *comercialbd.Queries, orden int64) error {
	id, err := q.ProspectoGanadoDeOrden(ctx, &orden)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	p, err := q.ProspectoPorID(ctx, id)
	if err != nil {
		return err
	}
	rev, err := q.RevisionPorFirmar(ctx, p.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	ord := strconv.FormatInt(orden, 10)
	if rev.Idempotencia == nil || (!strings.HasPrefix(*rev.Idempotencia, claveRevision("iris:plan:"+ord+":")) &&
		!strings.HasPrefix(*rev.Idempotencia, claveRevision("iris:titular:"+ord+":"))) {
		return nil
	}
	t.FijarEmpresa(deref(p.EmpresaID))
	return emitirContratoSinFirma(t, sinFirma{orden: orden, p: p, cotizacion: &rev.ID, operacion: operacionDeRevision(rev.Idempotencia),
		motivo: MotivoSinRegenerar})
}
