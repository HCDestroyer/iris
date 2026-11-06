package notificaciones

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/riverqueue/river"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion"
	notificacionesbd "github.com/HCDestroyer/iris/apps/api/internal/notificaciones/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// EventoPush es un evento del outbox que puede terminar en push.
type EventoPush = integracion.Evento

// EventosPush son los eventos que escucha el emisor (contratos/push.md §3).
var EventosPush = []string{
	"chat.mensaje_enviado", "chat.mencion", "conversacion.mencion", "conversacion.aviso_sin_atender",
	"orden.asignada", "orden.programada",
}

// Suscribir registra el emisor en el bus del outbox (cmd/worker). Sin FCM no hace nada: no se crean
// envíos que nadie mandaría.
func (m *Modulo) Suscribir(bus *integracion.Bus, bd nucleo.BD) {
	for _, ev := range EventosPush {
		bus.Suscribir("notificaciones.push", ev, func(ctx context.Context, e integracion.Evento) error {
			if !m.Activo() {
				return nil
			}
			_, err := m.PrepararEnvios.Ejecutar(nucleo.ConBD(ctx, bd), e)
			return err
		})
	}
}

// destino: a quién avisar de un evento y con qué datos (solo referencias).
type destino struct {
	tipo     string
	usuarios []int64
	clave    *string
	datos    map[string]any
}

// destinoDe lee el payload del evento. nil = el evento no avisa a nadie.
func destinoDe(e integracion.Evento) (*destino, error) {
	var p struct {
		ConversacionID    int64     `json:"conversacion_id"`
		MensajeID         uuid.UUID `json:"mensaje_id"`
		AutorID           int64     `json:"autor_id"`
		UsuarioID         int64     `json:"usuario_id"`
		Destinatarios     []int64   `json:"destinatarios"`
		Usuarios          []int64   `json:"usuarios"`
		BotContestaEn     *string   `json:"bot_contesta_en"`
		OrdenID           int64     `json:"orden_id"`
		TecnicoID         *int64    `json:"tecnico_id"`
		TecnicoAnteriorID *int64    `json:"tecnico_anterior_id"`
		Reprogramada      bool      `json:"reprogramada"`
		Origen            string    `json:"origen"`
	}
	if err := json.Unmarshal(e.Payload, &p); err != nil {
		return nil, fmt.Errorf("notificaciones: payload de %s: %w", e.Evento, err)
	}
	sinAutor := func(us []int64, autor int64) []int64 {
		out := []int64{}
		for _, u := range us {
			if u > 0 && u != autor && !slices.Contains(out, u) {
				out = append(out, u)
			}
		}
		return out
	}
	claveMsj := func() *string { c := "msj:" + p.MensajeID.String(); return &c }
	claveOrden := func() *string {
		if e.RequestID == "" {
			return nil
		}
		c := fmt.Sprintf("orden:%d:%s", p.OrdenID, e.RequestID)
		return &c
	}
	switch e.Evento {
	case "chat.mensaje_enviado":
		return &destino{tipo: TipoChat, usuarios: sinAutor(p.Destinatarios, p.AutorID), clave: claveMsj(),
			datos: map[string]any{"mensaje_id": p.MensajeID, "conversacion_id": p.ConversacionID}}, nil
	case "chat.mencion":
		// Mismo mensaje que su chat.mensaje_enviado: un solo push (la mención gana).
		return &destino{tipo: TipoMencion, usuarios: sinAutor([]int64{p.UsuarioID}, p.AutorID), clave: claveMsj(),
			datos: map[string]any{"mensaje_id": p.MensajeID, "conversacion_id": p.ConversacionID, "origen": "chat", "mencion": true}}, nil
	case "conversacion.mencion":
		return &destino{tipo: TipoMencion, usuarios: sinAutor([]int64{p.UsuarioID}, p.AutorID),
			datos: map[string]any{"mensaje_id": p.MensajeID, "conversacion_id": p.ConversacionID, "origen": "bandeja"}}, nil
	case "conversacion.aviso_sin_atender":
		return &destino{tipo: TipoBandeja, usuarios: sinAutor(p.Usuarios, 0),
			datos: map[string]any{"conversacion_id": p.ConversacionID, "bot_contesta_en": p.BotContestaEn}}, nil
	case "orden.asignada":
		if p.TecnicoID == nil || (p.TecnicoAnteriorID != nil && *p.TecnicoAnteriorID == *p.TecnicoID) {
			return nil, nil
		}
		return &destino{tipo: TipoOrden, usuarios: []int64{*p.TecnicoID}, clave: claveOrden(),
			datos: map[string]any{"orden_id": p.OrdenID, "asignada": true}}, nil
	case "orden.programada":
		// La de origen asignar ya la avisa orden.asignada; la primera programación no se avisa.
		if !p.Reprogramada || p.Origen == "asignar" || p.TecnicoID == nil {
			return nil, nil
		}
		return &destino{tipo: TipoOrden, usuarios: []int64{*p.TecnicoID}, clave: claveOrden(),
			datos: map[string]any{"orden_id": p.OrdenID, "reprogramada": true}}, nil
	}
	return nil, nil
}

// ArgsEnviar es el job que manda un envío. Único por envío: un evento repetido no lo encola dos
// veces (y el envío ya es único por evento y dispositivo).
type ArgsEnviar struct {
	EnvioID int64 `json:"envio_id"`
}

// Kind implementa river.JobArgs.
func (ArgsEnviar) Kind() string { return "notificaciones.enviar" }

// InsertOpts: hasta 8 intentos, único por argumentos.
func (ArgsEnviar) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: MaxIntentos, UniqueOpts: river.UniqueOpts{ByArgs: true}}
}

// MaxIntentos de un envío (429 y 5xx).
const MaxIntentos = 8

// nuevoPrepararEnvios: un envío por dispositivo de cada destinatario (en la empresa del evento) y su
// job. Corre como sistema; no deja fila de auditoría (son muchos y no cambian nada de negocio).
func (m *Modulo) nuevoPrepararEnvios() *auditoria.Caso[EventoPush, int] {
	return auditoria.Nuevo(auditoria.Caso[EventoPush, int]{
		Accion:       "notificaciones.preparar",
		Acceso:       auditoria.Autenticado, // el actor sistema del relay; no se expone por HTTP
		AuditarExito: func(int) bool { return false },
		Logica: func(ctx context.Context, t *auditoria.Tx, e EventoPush) (int, error) {
			d, err := destinoDe(e)
			if err != nil || d == nil || len(d.usuarios) == 0 {
				return 0, err
			}
			datos, err := json.Marshal(d.datos)
			if err != nil {
				return 0, err
			}
			q := notificacionesbd.New(t)
			ds, err := q.DispositivosDeUsuarios(ctx, notificacionesbd.DispositivosDeUsuariosParams{Usuarios: d.usuarios, Empresa: e.EmpresaID})
			if err != nil {
				return 0, err
			}
			n := 0
			for _, disp := range ds {
				id, err := q.InsertarEnvio(ctx, notificacionesbd.InsertarEnvioParams{EventoID: e.ID, Evento: e.Evento, DispositivoID: disp.ID,
					UsuarioID: disp.UsuarioID, EmpresaID: disp.EmpresaID, Tipo: d.tipo, Clave: d.clave, Datos: datos})
				if esSinFilas(err) {
					// Evento repetido (nada que hacer) u otro evento de la misma acción: suma sus datos.
					if d.clave != nil {
						if err := q.FusionarEnvio(ctx, notificacionesbd.FusionarEnvioParams{Clave: d.clave, DispositivoID: disp.ID, Datos: datos, Tipo: d.tipo}); err != nil {
							return n, err
						}
					}
					continue
				}
				if err != nil {
					return n, err
				}
				if m.cfg.Encolar != nil {
					if err := m.cfg.Encolar(ctx, t.Tx, ArgsEnviar{EnvioID: id}); err != nil {
						return n, err
					}
				}
				n++
			}
			return n, nil
		},
	})
}
