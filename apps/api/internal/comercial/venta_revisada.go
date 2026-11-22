package comercial

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	comercialbd "github.com/HCDestroyer/iris/apps/api/internal/comercial/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/pendientes"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Cotización y contrato al cambiar en la instalación (contrato argosisp.md 0.10.0 §3.4.8, pedido
// B20; decisión 2 del PM y ajuste e de §10.5). Cuando Argos acepta el cambio de plan (A40) o de
// titular (A39) de la orden de instalación de una venta de Iris:
//
//   - se crea una revisión nueva de la cotización, **vigente** (no aceptada), con el plan nuevo (y
//     su precio de Argos), a nombre del titular confirmado en la orden y con vigencia nueva;
//   - se regenera el contrato en borrador con esa revisión, ligado a la orden (contenido.ordenId),
//     a nombre del titular nuevo. El contrato firmado **sigue vivo**: el cambio de contrato ocurre al
//     firmar el regenerado en la orden (FirmarContratoEnOrden, o /contratos/{id}/firmar con la
//     firma del cliente), que anula el anterior y acepta la revisión. La firma en sitio cubre la
//     subida de precio (B6). Un cambio hecho desde el panel no se da por aceptado sin esa firma;
//   - una revisión anterior aún sin firmar queda reemplazada (y su contrato regenerado, anulado);
//   - el vendedor recibe un aviso (prospecto.venta_revisada → campana).
//
// Se dispara con integracion.pendiente_hecho de las operaciones cambiar_plan y cambiar_titular
// (nunca antes de que Argos acepte). Idempotente por la clave del pendiente.

// Motivos de la revisión.
const (
	RevisionPorPlan    = "plan"
	RevisionPorTitular = "titular"
)

// EventoVentaRevisada avisa al vendedor.
const EventoVentaRevisada = "prospecto.venta_revisada"

// EntradaRevisarVenta: un paso del titular hecho en Argos.
type EntradaRevisarVenta struct {
	OrdenID int64  `json:"orden_id"`
	Motivo  string `json:"motivo"`
	PlanID  *int64 `json:"plan_id,omitempty"`
	// Clave del pendiente (idempotencia de la revisión).
	Clave string `json:"clave"`
	// plan: el plan nuevo leído de Argos (fuera de la transacción).
	plan *PlanComercial
}

// ResultadoRevisarVenta: lo creado (nada si la orden no es de una venta de Iris).
type ResultadoRevisarVenta struct {
	ProspectoID  *int64 `json:"prospecto_id"`
	CotizacionID *int64 `json:"cotizacion_id"`
	ContratoID   *int64 `json:"contrato_id"`
}

// claveRevision: idempotencia de la cotización nueva.
func claveRevision(clave string) string { return "iris:venta:" + clave }

func (m *Modulo) nuevoRevisarVenta() *auditoria.Caso[EntradaRevisarVenta, ResultadoRevisarVenta] {
	return auditoria.Nuevo(auditoria.Caso[EntradaRevisarVenta, ResultadoRevisarVenta]{
		Accion: "prospectos.venta_revisada", Acceso: auditoria.Autenticado,
		AuditarExito: func(r ResultadoRevisarVenta) bool { return r.CotizacionID != nil },
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaRevisarVenta) (ResultadoRevisarVenta, error) {
			r := ResultadoRevisarVenta{}
			if err := exigirSistema(ctx); err != nil {
				return r, err
			}
			q := comercialbd.New(t)
			id, err := q.ProspectoGanadoDeOrden(ctx, &in.OrdenID)
			if errors.Is(err, pgx.ErrNoRows) {
				return r, nil // la orden no viene de una venta de Iris
			}
			if err != nil {
				return r, err
			}
			// Con la orden cancelada o fallida no hay firma en sitio: la revisión se crea igual (Argos
			// ya tiene el cambio), pero sin contrato; al reabrirla o reprogramarla, OrdenReabierta lo
			// regenera con la última revisión. Candado compartido de la orden.
			o, err := q.OrdenEstadoCompartido(ctx, in.OrdenID)
			if err != nil {
				return r, err
			}
			terminada := ordenTerminal(o.Estado)
			idem := claveRevision(in.Clave)
			if _, err := q.CotizacionPorIdempotencia(ctx, &idem); err == nil {
				return r, nil // ya revisada (el evento llegó dos veces)
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return r, err
			}
			p, err := tomar(ctx, t, q, id, nucleo.PermProspectosGanar, nil)
			if err != nil {
				return r, err
			}
			// La base es la última revisión: la que espera firma en la orden, si hay, o la aceptada.
			pendiente, err := q.RevisionPorFirmar(ctx, p.ID)
			hayPendiente := err == nil
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return r, err
			}
			base := pendiente
			if !hayPendiente {
				if base, err = q.CotizacionViva(ctx, p.ID); errors.Is(err, pgx.ErrNoRows) {
					return r, nil
				} else if err != nil {
					return r, err
				}
			}
			lineas, err := q.LineasDeCotizacion(ctx, base.ID)
			if err != nil {
				return r, err
			}
			mensual, unico := centavos(base.TotalMensual), centavos(base.TotalUnico)
			planID := base.PlanID
			if in.Motivo == RevisionPorPlan && in.plan != nil {
				planID = &in.plan.ID
				for i := range lineas {
					l := &lineas[i]
					if l.Tipo != "plan" {
						continue
					}
					l.PlanID, l.Descripcion = &in.plan.ID, in.plan.Nombre
					if in.plan.Precio != nil {
						precio, err := leerMonto("precio", *in.plan.Precio, maxCentavos)
						if err != nil {
							return r, err
						}
						total, cabe := multiplicar(centavos(l.Cantidad), precio)
						if !cabe {
							return r, nucleo.ReglaNegocio("monto_invalido", "El precio del plan nuevo no cabe en la cotización")
						}
						mensual += total - centavos(l.Total)
						l.PrecioUnitario, l.Total = aNumeric(precio), aNumeric(total)
					}
				}
			}
			cliente := clienteDe(p)
			if nombre, err := q.NombreTitularDeOrden(ctx, in.OrdenID); err == nil && nombre != nil && *nombre != "" {
				if *nombre != cliente.Nombre {
					// Titular nuevo: su documento vive solo en Argos (A41), nunca en el contrato de Iris.
					cliente.Nombre, cliente.Documento = *nombre, ""
				}
			} else if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return r, err
			}
			numeroOrden, err := q.NumeroDeOrden(ctx, in.OrdenID)
			if err != nil {
				return r, err
			}
			// La revisión anterior sin firmar queda reemplazada y su contrato regenerado, anulado. La
			// aceptada y el contrato firmado siguen vivos hasta la firma del regenerado.
			var reemplaza *int64
			if hayPendiente {
				if err := q.MarcarReemplazada(ctx, pendiente.ID); err != nil {
					return r, err
				}
				t.RegistrarCambios("cotizacion", pendiente.ID, map[string]any{"estado": pendiente.Estado}, map[string]any{"estado": "reemplazada"})
				reemplaza = &pendiente.ID
			}
			if err := anularPorFirmarEnOrden(ctx, t, q, p.ID, "Reemplazado por otra revisión en la instalación "+numeroOrden); err != nil {
				return r, err
			}
			revision, err := q.SiguienteRevision(ctx, p.ID)
			if err != nil {
				return r, err
			}
			crudoCliente, err := json.Marshal(cliente)
			if err != nil {
				return r, err
			}
			notas := fmt.Sprintf("Revisión por cambio de %s en la instalación %s: se acepta al firmar el contrato regenerado en la orden", in.Motivo, numeroOrden)
			if terminada {
				notas += " (la orden está " + o.Estado + ": el contrato se regenera al reabrirla)"
			}
			// Vigencia nueva: la de la venta puede haber vencido (el prospecto está ganado).
			vence := m.ahora().Add(m.cfg.VigenciaCotizacion).UTC().Truncate(time.Microsecond)
			pdfID, err := m.nuevoPDFID()
			if err != nil {
				return r, err
			}
			cot, err := q.CrearCotizacion(ctx, comercialbd.CrearCotizacionParams{
				EmpresaID: p.EmpresaID, ProspectoID: p.ID, Revision: revision, Moneda: base.Moneda,
				TotalMensual: aNumeric(mensual), TotalUnico: aNumeric(unico), VigenteHasta: vence, PlanID: planID,
				Notas: notas, Cliente: crudoCliente, Idempotencia: &idem, PdfAdjuntoID: pdfID,
			})
			if err != nil {
				return r, err
			}
			for _, l := range lineas {
				if _, err := q.CrearLineaCotizacion(ctx, comercialbd.CrearLineaCotizacionParams{
					CotizacionID: cot.ID, Posicion: l.Posicion, Tipo: l.Tipo, PlanID: l.PlanID, ProductoID: l.ProductoID,
					Descripcion: l.Descripcion, Cantidad: l.Cantidad, PrecioUnitario: l.PrecioUnitario, Recurrente: l.Recurrente, Total: l.Total,
				}); err != nil {
					return r, err
				}
			}
			if hayPendiente {
				if err := q.EnlazarReemplazo(ctx, comercialbd.EnlazarReemplazoParams{ReemplazadaPor: &cot.ID, ID: pendiente.ID}); err != nil {
					return r, err
				}
			}
			t.RegistrarCambios("cotizacion", cot.ID, nil, cot)
			if err := t.Emitir("cotizacion.creada", map[string]any{
				"cotizacion_id": cot.ID, "prospecto_id": p.ID, "numero": cot.Numero, "revision": cot.Revision, "plan_id": cot.PlanID,
				"total_mensual": aTexto(mensual), "total_unico": aTexto(unico), "moneda": cot.Moneda, "reemplaza_id": reemplaza,
			}); err != nil {
				return r, err
			}
			// Contrato regenerado a nombre del titular, para firmar en la orden (con la orden
			// terminada, al reabrirla).
			var contrato *int64
			if !terminada {
				ct, err := m.regenerarContrato(ctx, t, q, p, cot, cliente, in.OrdenID)
				if err != nil {
					return r, err
				}
				contrato = &ct.ID
			}
			if err := historial(ctx, t, q, p.ID, "venta_revisada", nil, nil, nil, map[string]any{
				"orden_id": in.OrdenID, "motivo": in.Motivo, "cotizacion_id": cot.ID, "base_id": base.ID, "reemplaza_id": reemplaza, "contrato_id": contrato,
			}); err != nil {
				return r, err
			}
			if err := t.Emitir(EventoVentaRevisada, map[string]any{
				"prospecto_id": p.ID, "empresa_id": p.EmpresaID, "vendedor_id": p.VendedorID, "orden_id": in.OrdenID,
				"motivo": in.Motivo, "cotizacion_id": cot.ID, "contrato_id": contrato,
			}); err != nil {
				return r, err
			}
			r.ProspectoID, r.CotizacionID, r.ContratoID = &p.ID, &cot.ID, contrato
			return r, nil
		},
	})
}

// nuevoPDFID: el id del adjunto del PDF de una revisión (nil sin almacenamiento de documentos); el
// PDF se arma fuera de la transacción (conPDFCotizacion).
func (m *Modulo) nuevoPDFID() (*uuid.UUID, error) {
	if m.cfg.Documentos == nil {
		return nil, nil
	}
	id, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	return &id, nil
}

// regenerarContrato crea el contrato en borrador con la cotización nueva (a firmar en la orden).
func (m *Modulo) regenerarContrato(ctx context.Context, t *auditoria.Tx, q *comercialbd.Queries, p comercialbd.ComercialProspecto,
	cot comercialbd.ComercialCotizacion, cliente clientePDF, orden int64) (comercialbd.ComercialContrato, error) {
	c, err := m.aCotizacion(ctx, q, cot)
	if err != nil {
		return comercialbd.ComercialContrato{}, err
	}
	requiereObra := false
	if fa, err := q.UltimaFactibilidad(ctx, p.ID); err == nil {
		requiereObra = fa.Resultado == FactibleRequiereObra
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return comercialbd.ComercialContrato{}, err
	}
	contenido := ContenidoContrato{
		Plantilla: PlantillaEstandar, Fecha: m.ahora(), Cliente: cliente, Cotizacion: cot.Numero, Moneda: cot.Moneda,
		TotalMensual: c.TotalMensual, TotalUnico: c.TotalUnico, Lineas: c.Lineas, Clausulas: clausulasEstandar(10),
		RequiereObra: requiereObra, OrdenID: &orden,
	}
	for _, l := range c.Lineas {
		if l.Tipo == "plan" {
			contenido.Plan = l.Descripcion
		}
	}
	var pdfID *uuid.UUID
	if m.cfg.Documentos != nil {
		adjunto, err := uuid.NewV7()
		if err != nil {
			return comercialbd.ComercialContrato{}, err
		}
		pdfID = &adjunto
	}
	crudo, err := json.Marshal(contenido)
	if err != nil {
		return comercialbd.ComercialContrato{}, err
	}
	f, err := q.CrearContrato(ctx, comercialbd.CrearContratoParams{EmpresaID: p.EmpresaID, ProspectoID: p.ID,
		CotizacionID: cot.ID, Plantilla: PlantillaEstandar, Contenido: crudo, PdfAdjuntoID: pdfID})
	if err != nil {
		return f, err
	}
	contenido.Numero = f.Numero
	if crudo, err = json.Marshal(contenido); err != nil {
		return f, err
	}
	if _, err := t.Exec(ctx, "UPDATE comercial.contratos SET contenido = $1 WHERE id = $2", crudo, f.ID); err != nil {
		return f, err
	}
	t.RegistrarCambios("contrato", f.ID, nil, map[string]any{"numero": f.Numero, "cotizacion_id": f.CotizacionID, "plantilla": f.Plantilla,
		"estado": f.Estado, "orden_id": orden})
	return f, t.Emitir("contrato.creado", map[string]any{"contrato_id": f.ID, "prospecto_id": p.ID, "cotizacion_id": cot.ID, "numero": f.Numero})
}

// RevisarVenta lee el plan nuevo de Argos (fuera de la transacción) y revisa la venta. Sin plan
// legible (Argos caído) devuelve el error: el relay lo reintenta.
func (m *Modulo) RevisarVenta(ctx context.Context, in EntradaRevisarVenta) (ResultadoRevisarVenta, error) {
	if in.Motivo == RevisionPorPlan && in.PlanID != nil && m.cfg.Planes != nil {
		ps, err := m.cfg.Planes.Planes(ctx, nil)
		if err != nil {
			return ResultadoRevisarVenta{}, err
		}
		for i := range ps {
			if ps[i].ID == *in.PlanID {
				in.plan = &ps[i]
			}
		}
		if in.plan == nil {
			// Argos ya no lo lista (lo desactivó): solo cambia el id; el nombre lo pone una persona.
			in.plan = &PlanComercial{ID: *in.PlanID, Nombre: "Plan " + strconv.FormatInt(*in.PlanID, 10)}
		}
	}
	r, err := m.revisarVenta.Ejecutar(ctx, in)
	if err != nil || r.CotizacionID == nil {
		return r, err
	}
	if cot, err := m.VerCotizacion.Ejecutar(ctx, EntradaVerCotizacion{ID: *r.CotizacionID}); err == nil {
		m.conPDFCotizacion(ctx, cot)
	}
	if r.ContratoID == nil {
		return r, nil
	}
	if ct, err := m.VerContrato.Ejecutar(ctx, EntradaVerContrato{ID: *r.ContratoID}); err == nil {
		m.conPDFBorrador(ctx, ct)
	}
	return r, nil
}

// SuscribirRevisionVenta: el cambio de plan o de titular hecho en Argos revisa la venta (cmd/worker).
func (m *Modulo) SuscribirRevisionVenta(bus *integracion.Bus, bd nucleo.BD, log *slog.Logger) {
	// La orden cancelada o fallida anula el contrato regenerado que esperaba la firma en sitio.
	m.suscribirOrdenTerminada(bus, bd, log)
	bus.Suscribir("comercial.venta_revisada", pendientes.EventoHecho, func(ctx context.Context, e integracion.Evento) error {
		var p eventoPendienteOrden
		if err := json.Unmarshal(e.Payload, &p); err != nil || p.Entidad != "orden" {
			return nil
		}
		var motivo string
		switch p.Operacion {
		case pendientes.OpCambiarPlan:
			motivo = RevisionPorPlan
		case pendientes.OpCambiarTitular:
			motivo = RevisionPorTitular
		default:
			return nil
		}
		orden, err := strconv.ParseInt(p.EntidadID, 10, 64)
		if err != nil || orden <= 0 || p.Clave == "" {
			return nil
		}
		ctx = nucleo.ConBD(ctx, bd)
		in := EntradaRevisarVenta{OrdenID: orden, Motivo: motivo, Clave: p.Clave}
		if motivo == RevisionPorPlan {
			reg, err := pendientes.PorClave(ctx, bd, p.Clave)
			if err != nil {
				return err
			}
			var cuerpo struct {
				PlanID *int64 `json:"plan_id"`
			}
			if err := json.Unmarshal(reg.Payload, &cuerpo); err != nil || cuerpo.PlanID == nil {
				log.WarnContext(ctx, "comercial: cambio de plan sin plan_id", "pendiente", reg.ID)
				return nil
			}
			in.PlanID = cuerpo.PlanID
		}
		_, err = m.RevisarVenta(ctx, in)
		var pr *nucleo.Problema
		if errors.As(err, &pr) && pr.Status < 500 {
			log.WarnContext(ctx, "comercial: la venta no se revisó", "orden", orden, "codigo", pr.Codigo())
			return nil
		}
		return err
	})
}
