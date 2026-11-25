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

// Posible falla general (contrato Iris ↔ ArgosISP 0.13.0 §3.11, B29): cuando una empresa pasa del
// umbral de tickets de caídas en 10 min, soporte deja de abrirlos, los agrupa en un ticket y avisa a
// supervisión: la campana de quienes pueden cambiar esos umbrales (tickets.configurar_caidas) en la
// empresa. Uno por ticket (clave con su id).

// TipoAvisoPosibleFalla: Iris agrupó caídas en una posible falla general.
const TipoAvisoPosibleFalla = "posible_falla_general"

// EntradaAvisarPosibleFalla es el payload de soporte.posible_falla_general.
type EntradaAvisarPosibleFalla struct {
	EventoID  uuid.UUID `json:"-"`
	TicketID  int64     `json:"ticket_id"`
	EmpresaID int64     `json:"empresa_id"`
	Tickets   int       `json:"tickets"`
}

func (m *Modulo) nuevoAvisarPosibleFalla() *auditoria.Caso[EntradaAvisarPosibleFalla, ResultadoAvisarTope] {
	return auditoria.Nuevo(auditoria.Caso[EntradaAvisarPosibleFalla, ResultadoAvisarTope]{
		Accion: "avisos.posible_falla_general", Acceso: auditoria.Autenticado, SinEmpresa: true,
		AuditarExito: func(r ResultadoAvisarTope) bool { return r.Avisos > 0 },
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaAvisarPosibleFalla) (ResultadoAvisarTope, error) {
			r := ResultadoAvisarTope{}
			if err := soloSistema(ctx); err != nil {
				return r, err
			}
			if in.TicketID <= 0 || in.EmpresaID <= 0 {
				return r, nil
			}
			t.FijarEmpresa(in.EmpresaID)
			t.FijarEntidad("ticket", in.TicketID)
			q := notificacionesbd.New(t)
			us, err := q.DestinatariosConPermiso(ctx, notificacionesbd.DestinatariosConPermisoParams{
				Permiso: string(nucleo.PermTicketsConfigurarCaidas), Empresa: &in.EmpresaID})
			if err != nil {
				return r, err
			}
			clave := fmt.Sprintf("%s:%d", TipoAvisoPosibleFalla, in.TicketID)
			datos, err := json.Marshal(map[string]any{"ticket_id": in.TicketID, "tickets": in.Tickets})
			if err != nil {
				return r, err
			}
			enlace := "/tickets/" + strconv.FormatInt(in.TicketID, 10)
			usuarios := []int64{}
			for _, u := range us {
				_, err := q.InsertarAviso(ctx, notificacionesbd.InsertarAvisoParams{UsuarioID: u.ID, EmpresaID: &in.EmpresaID, Tipo: TipoAvisoPosibleFalla,
					Clave: clave, Nivel: "peligro", Titulo: "Posible falla general: muchas caídas en pocos minutos", Enlace: &enlace, Datos: datos,
					EventoID: pgtype.UUID{Bytes: in.EventoID, Valid: in.EventoID != uuid.Nil}})
				if esSinFilas(err) {
					continue
				}
				if err != nil {
					return r, err
				}
				r.Avisos++
				usuarios = append(usuarios, u.ID)
			}
			if r.Avisos == 0 {
				return r, nil
			}
			t.Anotar(map[string]any{"clave": clave, "avisos": r.Avisos})
			return r, t.Emitir("notificaciones.avisos_creados", map[string]any{"tipo": TipoAvisoPosibleFalla, "clave": clave,
				"empresa_id": in.EmpresaID, "usuarios": usuarios, "correo": "omitido"})
		},
	})
}

func (m *Modulo) suscribirPosibleFalla(bus *integracion.Bus, bd nucleo.BD) {
	bus.Suscribir("notificaciones.avisos", "soporte.posible_falla_general", func(ctx context.Context, e integracion.Evento) error {
		var in EntradaAvisarPosibleFalla
		if err := json.Unmarshal(e.Payload, &in); err != nil {
			return fmt.Errorf("notificaciones: payload de %s: %w", e.Evento, err)
		}
		in.EventoID = e.ID
		_, err := m.AvisarPosibleFalla.Ejecutar(nucleo.ConBD(ctx, bd), in)
		return err
	})
	bus.Suscribir("notificaciones.avisos", "soporte.verificacion_vencida", func(ctx context.Context, e integracion.Evento) error {
		var in EntradaAvisarVerificacion
		if err := json.Unmarshal(e.Payload, &in); err != nil {
			return fmt.Errorf("notificaciones: payload de %s: %w", e.Evento, err)
		}
		in.EventoID = e.ID
		_, err := m.AvisarVerificacion.Ejecutar(nucleo.ConBD(ctx, bd), in)
		return err
	})
}

// Verificación vencida (§3.11, B30): el cliente no respondió a la verificación tras cerrarse una falla
// general y el ticket volvió a abierto; se avisa a quien lo tiene asignado (sin asignado, a nadie).

// TipoAvisoVerificacionVencida: un ticket volvió a abierto sin respuesta del cliente.
const TipoAvisoVerificacionVencida = "verificacion_vencida"

// EntradaAvisarVerificacion es el payload de soporte.verificacion_vencida.
type EntradaAvisarVerificacion struct {
	EventoID  uuid.UUID `json:"-"`
	TicketID  int64     `json:"ticket_id"`
	Numero    string    `json:"numero"`
	EmpresaID *int64    `json:"empresa_id"`
	AsignadoA *int64    `json:"asignado_a"`
}

func (m *Modulo) nuevoAvisarVerificacion() *auditoria.Caso[EntradaAvisarVerificacion, ResultadoAvisarTope] {
	return auditoria.Nuevo(auditoria.Caso[EntradaAvisarVerificacion, ResultadoAvisarTope]{
		Accion: "avisos.verificacion_vencida", Acceso: auditoria.Autenticado, SinEmpresa: true,
		AuditarExito: func(r ResultadoAvisarTope) bool { return r.Avisos > 0 },
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaAvisarVerificacion) (ResultadoAvisarTope, error) {
			r := ResultadoAvisarTope{}
			if err := soloSistema(ctx); err != nil {
				return r, err
			}
			if in.TicketID <= 0 || in.AsignadoA == nil {
				return r, nil
			}
			if in.EmpresaID != nil {
				t.FijarEmpresa(*in.EmpresaID)
			} else {
				t.FijarEmpresa(0)
			}
			t.FijarEntidad("ticket", in.TicketID)
			q := notificacionesbd.New(t)
			clave := fmt.Sprintf("%s:%d:%s", TipoAvisoVerificacionVencida, in.TicketID, in.EventoID)
			datos, err := json.Marshal(map[string]any{"ticket_id": in.TicketID, "numero": in.Numero})
			if err != nil {
				return r, err
			}
			enlace := "/tickets/" + strconv.FormatInt(in.TicketID, 10)
			_, err = q.InsertarAviso(ctx, notificacionesbd.InsertarAvisoParams{UsuarioID: *in.AsignadoA, EmpresaID: in.EmpresaID, Tipo: TipoAvisoVerificacionVencida,
				Clave: clave, Nivel: "alerta", Titulo: "El cliente no confirmó tras la falla general: " + in.Numero, Enlace: &enlace, Datos: datos,
				EventoID: pgtype.UUID{Bytes: in.EventoID, Valid: in.EventoID != uuid.Nil}})
			if esSinFilas(err) {
				return r, nil
			}
			if err != nil {
				return r, err
			}
			r.Avisos = 1
			return r, t.Emitir("notificaciones.avisos_creados", map[string]any{"tipo": TipoAvisoVerificacionVencida, "clave": clave,
				"empresa_id": in.EmpresaID, "usuarios": []int64{*in.AsignadoA}, "correo": "omitido"})
		},
	})
}
