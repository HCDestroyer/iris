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

// Avisos a supervisión de la instalación (B20, verificación de #280): la campana de los
// supervisores con alcance sobre la orden (ordenes.validar_identidad: global, su zona o su
// cuadrilla) cuando
//
//   - paso_titular: un paso del titular quedó hecho en Argos con la orden ya cancelada o fallida
//     (orden.paso_titular_tras_cancelar). Clave paso_titular:<pendiente_id>. Con la orden cerrada,
//     un cambio de plan o de titular no llega por aquí: lo avisa contrato_sin_firma;
//   - contrato_sin_firma: un cambio de plan o de titular quedó sin la firma del contrato nuevo
//     (orden.contrato_sin_firma: cierre del supervisor, cambio tras el cierre, orden cerrada antes de
//     regenerar, o revisión imposible con su motivo). Clave contrato_sin_firma:<contrato_id>, si no
//     contrato_sin_firma:cotizacion:<cotizacion_id> y, sin revisión, contrato_sin_firma:pendiente:<clave>.
//
// Destinatarios (DestinatariosDeOrden): con la orden sin empresa (empresa_id null, sin conector),
// todos los que tienen el permiso con alcance sobre la orden, sin filtro de empresa.
//
// Solo campana (informativo; lo hecho en Argos no se deshace solo). Uno por usuario y clave.

// Tipos de aviso de supervisión.
const (
	TipoAvisoPasoTitular      = "paso_titular"
	TipoAvisoContratoSinFirma = "contrato_sin_firma"
)

// EntradaAvisarSupervision: el payload de los dos eventos (Tipo lo pone el suscriptor).
type EntradaAvisarSupervision struct {
	EventoID    uuid.UUID `json:"-"`
	Tipo        string    `json:"-"`
	OrdenID     int64     `json:"orden_id"`
	PendienteID int64     `json:"pendiente_id"`
	ContratoID  int64     `json:"contrato_id"`
	// CotizacionID: la revisión (contrato_sin_firma sin contrato: la clave va por ella).
	CotizacionID int64   `json:"cotizacion_id"`
	ProspectoID  *int64  `json:"prospecto_id"`
	Operacion    *string `json:"operacion"`
	EstadoOrden  string  `json:"estado_orden"`
	Motivo       string  `json:"motivo"`
	// Clave del pendiente y detalle (motivos diagnósticos: la revisión no se pudo crear).
	Clave   string  `json:"clave"`
	Detalle *string `json:"detalle"`
}

// cambioDe: «el plan», «el titular» o «el paso del titular».
func cambioDe(op *string) string {
	switch {
	case op == nil:
		return "el contrato"
	case *op == "cambiar_plan":
		return "el plan"
	case *op == "cambiar_titular":
		return "el titular"
	}
	return "el paso del titular"
}

func (m *Modulo) nuevoAvisarSupervision() *auditoria.Caso[EntradaAvisarSupervision, ResultadoAvisarTope] {
	return auditoria.Nuevo(auditoria.Caso[EntradaAvisarSupervision, ResultadoAvisarTope]{
		Accion: "avisos.supervision_instalacion", Acceso: auditoria.Autenticado, SinEmpresa: true,
		AuditarExito: func(r ResultadoAvisarTope) bool { return r.Avisos > 0 },
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaAvisarSupervision) (ResultadoAvisarTope, error) {
			r := ResultadoAvisarTope{}
			if err := soloSistema(ctx); err != nil {
				return r, err
			}
			if in.OrdenID <= 0 || in.EventoID == uuid.Nil {
				return r, nil
			}
			var clave, titulo, nivel string
			switch in.Tipo {
			case TipoAvisoPasoTitular:
				if in.PendienteID <= 0 {
					return r, nil
				}
				clave = fmt.Sprintf("%s:%d", TipoAvisoPasoTitular, in.PendienteID)
				nivel = "alerta"
			case TipoAvisoContratoSinFirma:
				switch {
				case in.ContratoID > 0:
					clave = fmt.Sprintf("%s:%d", TipoAvisoContratoSinFirma, in.ContratoID)
				case in.CotizacionID > 0:
					// Revisión creada sin contrato (Argos aceptó con la orden ya cerrada).
					clave = fmt.Sprintf("%s:cotizacion:%d", TipoAvisoContratoSinFirma, in.CotizacionID)
				case in.Clave != "":
					// La revisión no se pudo crear (motivo diagnóstico): una por pendiente.
					clave = fmt.Sprintf("%s:pendiente:%s", TipoAvisoContratoSinFirma, in.Clave)
				default:
					return r, nil
				}
				nivel = "alerta"
			default:
				return r, nil
			}
			q := notificacionesbd.New(t)
			o, err := q.OrdenParaAviso(ctx, in.OrdenID)
			if esSinFilas(err) {
				return r, nil
			}
			if err != nil {
				return r, err
			}
			if o.EmpresaID != nil {
				t.FijarEmpresa(*o.EmpresaID)
			} else {
				t.FijarEmpresa(0)
			}
			t.FijarEntidad("orden", o.ID)
			switch in.Tipo {
			case TipoAvisoPasoTitular:
				titulo = fmt.Sprintf("Argos aplicó %s de la orden %s, que ya estaba %s", cambioDe(in.Operacion), o.Numero, in.EstadoOrden)
			case TipoAvisoContratoSinFirma:
				titulo = tituloSinFirma(o.Numero, in)
			}
			datos, err := json.Marshal(map[string]any{"orden_id": o.ID, "pendiente_id": nulo(in.PendienteID), "contrato_id": nulo(in.ContratoID),
				"cotizacion_id": nulo(in.CotizacionID),
				"prospecto_id":  in.ProspectoID, "operacion": in.Operacion, "estado_orden": in.EstadoOrden, "motivo": in.Motivo,
				"clave": in.Clave, "detalle": in.Detalle})
			if err != nil {
				return r, err
			}
			us, err := q.DestinatariosDeOrden(ctx, notificacionesbd.DestinatariosDeOrdenParams{
				Permiso: string(nucleo.PermOrdenesValidarIdentidad), OrdenID: o.ID})
			if err != nil {
				return r, err
			}
			enlace := "/ordenes/" + strconv.FormatInt(o.ID, 10)
			usuarios := []int64{}
			for _, u := range us {
				_, err := q.InsertarAviso(ctx, notificacionesbd.InsertarAvisoParams{UsuarioID: u.ID, EmpresaID: o.EmpresaID, Tipo: in.Tipo,
					Clave: clave, Nivel: nivel, Titulo: titulo, Enlace: &enlace, Datos: datos, EventoID: pgtype.UUID{Bytes: in.EventoID, Valid: true}})
				if esSinFilas(err) {
					continue // ya avisado
				}
				if err != nil {
					return r, err
				}
				usuarios = append(usuarios, u.ID)
			}
			r.Avisos = len(usuarios)
			if r.Avisos == 0 {
				return r, nil
			}
			return r, t.Emitir("notificaciones.avisos_creados", map[string]any{"tipo": in.Tipo, "clave": clave,
				"empresa_id": o.EmpresaID, "usuarios": usuarios, "correo": "omitido"})
		},
	})
}

// nulo: nil en vez de 0 (el id no vino).
func nulo(id int64) any {
	if id <= 0 {
		return nil
	}
	return id
}

func (m *Modulo) suscribirSupervision(bus *integracion.Bus, bd nucleo.BD) {
	for evento, tipo := range map[string]string{
		"orden.paso_titular_tras_cancelar": TipoAvisoPasoTitular,
		"orden.contrato_sin_firma":         TipoAvisoContratoSinFirma,
	} {
		bus.Suscribir("notificaciones.avisos", evento, func(ctx context.Context, e integracion.Evento) error {
			var in EntradaAvisarSupervision
			if err := json.Unmarshal(e.Payload, &in); err != nil {
				return fmt.Errorf("notificaciones: payload de %s: %w", e.Evento, err)
			}
			in.EventoID, in.Tipo = e.ID, tipo
			_, err := m.AvisarSupervision.Ejecutar(nucleo.ConBD(ctx, bd), in)
			return err
		})
	}
}

// tituloSinFirma: el texto de contrato_sin_firma según el motivo (los diagnósticos dicen por qué
// la orden no cierra sin un supervisor).
func tituloSinFirma(numero string, in EntradaAvisarSupervision) string {
	cambio := cambioDe(in.Operacion)
	switch in.Motivo {
	case "sin_cotizacion_viva":
		return fmt.Sprintf("Argos aplicó %s de la orden %s, pero la venta no tiene cotización: no habrá contrato nuevo que firmar", cambio, numero)
	case "sin_plan":
		return fmt.Sprintf("Argos aplicó el cambio de plan de la orden %s sin el plan nuevo: no habrá contrato nuevo que firmar", numero)
	case "revision_rechazada":
		detalle := ""
		if in.Detalle != nil {
			detalle = " (" + *in.Detalle + ")"
		}
		return fmt.Sprintf("Argos aplicó %s de la orden %s, pero no se pudo crear el contrato nuevo%s", cambio, numero, detalle)
	case "sin_regenerar":
		return fmt.Sprintf("La orden %s se cerró antes de regenerar el contrato nuevo (%s)", numero, cambio)
	}
	return fmt.Sprintf("La orden %s se cerró sin la firma del contrato nuevo (%s)", numero, cambio)
}
