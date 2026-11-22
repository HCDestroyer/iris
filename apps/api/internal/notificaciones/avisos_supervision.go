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
//   - paso_titular: un paso del titular quedó hecho en Argos con la orden ya cancelada, fallida o
//     (un cambio de plan o de titular) cerrada (orden.paso_titular_tras_cancelar). Clave
//     paso_titular:<pendiente_id>;
//   - contrato_sin_firma: la orden quedó cerrada con el contrato regenerado sin firmar
//     (orden.contrato_sin_firma). Clave contrato_sin_firma:<contrato_id>.
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
	ProspectoID *int64    `json:"prospecto_id"`
	Operacion   *string   `json:"operacion"`
	EstadoOrden string    `json:"estado_orden"`
	Motivo      string    `json:"motivo"`
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
				if in.ContratoID <= 0 {
					return r, nil
				}
				clave = fmt.Sprintf("%s:%d", TipoAvisoContratoSinFirma, in.ContratoID)
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
				titulo = fmt.Sprintf("La orden %s se cerró sin la firma del contrato nuevo (%s)", o.Numero, cambioDe(in.Operacion))
			}
			datos, err := json.Marshal(map[string]any{"orden_id": o.ID, "pendiente_id": nulo(in.PendienteID), "contrato_id": nulo(in.ContratoID),
				"prospecto_id": in.ProspectoID, "operacion": in.Operacion, "estado_orden": in.EstadoOrden, "motivo": in.Motivo})
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
