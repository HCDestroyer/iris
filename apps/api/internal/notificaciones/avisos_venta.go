package notificaciones

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion"
	notificacionesbd "github.com/HCDestroyer/iris/apps/api/internal/notificaciones/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Aviso al vendedor cuando la instalación cambia su venta (contrato argosisp.md 0.10.0 §3.4.8,
// pedido B20; decisión 2 del PM: «ventas recibe aviso»): prospecto.venta_revisada pone en la
// campana del vendedor que el plan o el titular cambiaron, con enlace al prospecto. Solo campana
// (informativo: no bloquea nada). Uno por cotización nueva.

// TipoAvisoVentaRevisada: la instalación cambió el plan o el titular de una venta.
const TipoAvisoVentaRevisada = "venta_revisada"

// EntradaAvisarVentaRevisada es el payload de prospecto.venta_revisada.
type EntradaAvisarVentaRevisada struct {
	EventoID     uuid.UUID `json:"-"`
	ProspectoID  int64     `json:"prospecto_id"`
	EmpresaID    *int64    `json:"empresa_id"`
	VendedorID   *int64    `json:"vendedor_id"`
	OrdenID      int64     `json:"orden_id"`
	Motivo       string    `json:"motivo"`
	CotizacionID int64     `json:"cotizacion_id"`
}

func (m *Modulo) nuevoAvisarVentaRevisada() *auditoria.Caso[EntradaAvisarVentaRevisada, ResultadoAvisarTope] {
	return auditoria.Nuevo(auditoria.Caso[EntradaAvisarVentaRevisada, ResultadoAvisarTope]{
		Accion: "avisos.venta_revisada", Acceso: auditoria.Autenticado, SinEmpresa: true,
		AuditarExito: func(r ResultadoAvisarTope) bool { return r.Avisos > 0 },
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaAvisarVentaRevisada) (ResultadoAvisarTope, error) {
			r := ResultadoAvisarTope{}
			if err := soloSistema(ctx); err != nil {
				return r, err
			}
			if in.VendedorID == nil || in.ProspectoID <= 0 || in.CotizacionID <= 0 || in.EventoID == uuid.Nil {
				return r, nil
			}
			if in.EmpresaID != nil {
				t.FijarEmpresa(*in.EmpresaID)
			}
			t.FijarEntidad("prospecto", in.ProspectoID)
			titulo := "La instalación cambió el plan de tu venta"
			if in.Motivo == "titular" {
				titulo = "La instalación cambió el titular de tu venta"
			}
			clave := fmt.Sprintf("%s:%d", TipoAvisoVentaRevisada, in.CotizacionID)
			datos, err := json.Marshal(map[string]any{"prospecto_id": in.ProspectoID, "orden_id": in.OrdenID, "motivo": in.Motivo,
				"cotizacion_id": in.CotizacionID})
			if err != nil {
				return r, err
			}
			enlace := "/comercial/" + strconv.FormatInt(in.ProspectoID, 10)
			_, err = notificacionesbd.New(t).InsertarAviso(ctx, notificacionesbd.InsertarAvisoParams{UsuarioID: *in.VendedorID,
				EmpresaID: in.EmpresaID, Tipo: TipoAvisoVentaRevisada, Clave: clave, Nivel: "info", Titulo: titulo, Enlace: &enlace,
				Datos: datos, EventoID: pgtype.UUID{Bytes: in.EventoID, Valid: true}})
			if esSinFilas(err) {
				return r, nil // ya avisado
			}
			if err != nil {
				return r, err
			}
			r.Avisos = 1
			return r, t.Emitir("notificaciones.avisos_creados", map[string]any{"tipo": TipoAvisoVentaRevisada, "clave": clave,
				"empresa_id": in.EmpresaID, "usuarios": []int64{*in.VendedorID}, "correo": "omitido"})
		},
	})
}

func (m *Modulo) suscribirVentas(bus *integracion.Bus, bd nucleo.BD) {
	bus.Suscribir("notificaciones.avisos", "prospecto.venta_revisada", func(ctx context.Context, e integracion.Evento) error {
		var in EntradaAvisarVentaRevisada
		if err := json.Unmarshal(e.Payload, &in); err != nil {
			return fmt.Errorf("notificaciones: payload de %s: %w", e.Evento, err)
		}
		in.EventoID = e.ID
		_, err := m.AvisarVentaRevisada.Ejecutar(nucleo.ConBD(ctx, bd), in)
		return err
	})
}
