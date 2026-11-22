package campo

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	campobd "github.com/HCDestroyer/iris/apps/api/internal/campo/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// orden.firmar_contrato (contrato argosisp.md 0.10.0 §3.4.8, ajuste e de §10.5; ALTA 1 de la
// revisión de #280): cuando Argos acepta un cambio de plan o de titular, comercial regenera el
// contrato a nombre del titular final, en borrador y ligado a la orden. El titular lo firma en sitio,
// con el técnico asignado y la orden en sitio, como la firma de la orden (también sin señal: la
// operación va en el lote). La firma en sitio cubre la subida de precio (B6): solo entonces el
// contrato anterior se anula y la revisión de la cotización queda aceptada. El PDF firmado lo arma
// y guarda comercial antes de la transacción; la operación se reclama y el contrato se marca
// firmado en la misma transacción.

// OpFirmarContrato es el tipo de la operación del lote.
const OpFirmarContrato = "orden.firmar_contrato"

// EntradaFirmarContrato son los datos de orden.firmar_contrato.
type EntradaFirmarContrato struct {
	ID                int64    `json:"-"`
	ContratoID        int64    `json:"contratoId" minimum:"1" doc:"El contrato regenerado de la orden (GET /ordenes/{id}/titular, contratoPorFirmar)"`
	FirmanteNombre    string   `json:"firmanteNombre" minLength:"1" maxLength:"200" doc:"El titular (o el representante de la empresa titular)"`
	FirmanteDocumento *string  `json:"firmanteDocumento,omitempty" maxLength:"40" auditoria:"sensible" doc:"Opcional; nunca va en eventos ni en la auditoría"`
	SVG               string   `json:"svg" minLength:"10" maxLength:"200000" auditoria:"sensible" doc:"Firma en SVG: <svg width=\"…\" height=\"…\"> con <path d=\"…\"> (comandos M, L, C, Q, H, V, Z)"`
	Lat               *float64 `json:"lat,omitempty" minimum:"-90" maximum:"90"`
	Lon               *float64 `json:"lon,omitempty" minimum:"-180" maximum:"180"`
	Sync              *OpSync  `json:"-"`
	preparada         *FirmaContratoPreparada
}

func (e EntradaFirmarContrato) ordenID() int64 { return e.ID }

// EntradaFirmaContratoSync es lo que comercial necesita para armar el PDF firmado.
type EntradaFirmaContratoSync struct {
	OrdenID           int64    `json:"orden_id"`
	ContratoID        int64    `json:"contrato_id"`
	FirmanteNombre    string   `json:"firmante_nombre"`
	FirmanteDocumento *string  `json:"firmante_documento" auditoria:"sensible"`
	SVG               string   `json:"svg" auditoria:"sensible"`
	Lat               *float64 `json:"lat"`
	Lon               *float64 `json:"lon"`
}

// FirmaContratoPreparada: el PDF firmado ya guardado (su adjunto y SHA-256) y la firma normalizada.
type FirmaContratoPreparada struct {
	Entrada   EntradaFirmaContratoSync
	AdjuntoID uuid.UUID
	SHA256    string
	En        time.Time
}

// ContratosEnOrden firma el contrato regenerado de la orden (comercial.Modulo).
type ContratosEnOrden interface {
	PrepararFirmaEnOrden(ctx context.Context, in EntradaFirmaContratoSync) (FirmaContratoPreparada, error)
	FirmarEnOrdenEnTx(ctx context.Context, t *auditoria.Tx, l FirmaContratoPreparada) (int64, error)
}

// ConectarContratos enchufa comercial (que se crea después de campo: comercial usa campo).
func (m *Modulo) ConectarContratos(c ContratosEnOrden) { m.cfg.Contratos = c }

func sinContratos() *nucleo.Problema {
	return nucleo.NuevoProblema(http.StatusServiceUnavailable, "no_disponible", "No disponible",
		"La firma del contrato en la orden no está conectada en este servidor")
}

func (m *Modulo) nuevoFirmarContrato() *auditoria.Caso[EntradaFirmarContrato, Orden] {
	return accionOrden("ordenes.firmar_contrato", nucleo.PermOrdenesCambiarTitular, func(e EntradaFirmarContrato) *OpSync { return e.Sync },
		func(ctx context.Context, t *auditoria.Tx, q *campobd.Queries, in EntradaFirmarContrato) (campobd.CampoOrden, string, error) {
			f, err := tomar(ctx, t, q, in.ID, nucleo.PermOrdenesCambiarTitular, nil, in.Sync)
			if err != nil {
				return f, "", err
			}
			// El técnico asignado, en sitio y en una instalación (§3.4, «Quién»).
			if !propia(ctx, f) {
				return f, "", fueraDeAlcance("El contrato regenerado lo firma el titular en sitio, con el técnico asignado")
			}
			if err := noPausada(f, "firmar el contrato de"); err != nil {
				return f, "", err
			}
			if err := exigirEstado(f, "firmar el contrato de", EstadoEnSitio); err != nil {
				return f, "", err
			}
			if err := exigirInstalacion(ctx, q, f); err != nil {
				return f, "", err
			}
			if m.cfg.Contratos == nil || in.preparada == nil {
				return f, "", sinContratos()
			}
			contrato, err := m.cfg.Contratos.FirmarEnOrdenEnTx(ctx, t, *in.preparada)
			if err != nil {
				return f, "", err
			}
			t.RegistrarCambios("orden_contrato", f.ID, nil, map[string]any{"contrato_id": contrato, "firma": auditoria.Mascara,
				"pdf_firmado_id": in.preparada.AdjuntoID, "pdf_firmado_sha256": in.preparada.SHA256})
			f, err = tocar(ctx, t, q, f)
			return f, "", err
		})
}

// FirmarContrato: la firma del contrato regenerado en la orden (operación del lote). Arma el PDF
// firmado (comercial, fuera de la transacción) y corre la acción.
func (m *Modulo) FirmarContrato(ctx context.Context, in EntradaFirmarContrato) (Orden, error) {
	if m.cfg.Contratos == nil {
		return Orden{}, sinContratos()
	}
	l, err := m.cfg.Contratos.PrepararFirmaEnOrden(ctx, EntradaFirmaContratoSync{OrdenID: in.ID, ContratoID: in.ContratoID,
		FirmanteNombre: in.FirmanteNombre, FirmanteDocumento: in.FirmanteDocumento, SVG: in.SVG, Lat: in.Lat, Lon: in.Lon})
	if err != nil {
		return Orden{}, err
	}
	in.preparada = &l
	return m.Titular.FirmarContrato.Ejecutar(ctx, in)
}
