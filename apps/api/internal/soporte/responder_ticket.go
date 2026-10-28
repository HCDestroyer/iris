package soporte

import (
	"context"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	soportebd "github.com/HCDestroyer/iris/apps/api/internal/soporte/bd"
)

// EntradaResponderTicket es la entrada de tickets.responder: un mensaje público al cliente.
type EntradaResponderTicket struct {
	ID      int64  `json:"-"`
	Version *int32 `json:"version,omitempty" minimum:"1" doc:"Versión leída (opcional); si se envía y cambió, 409"`
	Cuerpo  string `json:"cuerpo" minLength:"1" maxLength:"20000"`
}

// TicketConMensaje es el ticket tras agregarle un mensaje.
type TicketConMensaje struct {
	Ticket  Ticket  `json:"ticket"`
	Mensaje Mensaje `json:"mensaje"`
}

func (m *Modulo) nuevoResponderTicket() *auditoria.Caso[EntradaResponderTicket, TicketConMensaje] {
	return auditoria.Nuevo(auditoria.Caso[EntradaResponderTicket, TicketConMensaje]{
		Accion:  "tickets.responder",
		Permiso: nucleo.PermTicketsResponder,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaResponderTicket) (TicketConMensaje, error) {
			q := soportebd.New(t)
			ahora := m.ahora()
			cuerpo, err := texto("cuerpo", in.Cuerpo, 1, 20000)
			if err != nil {
				return TicketConMensaje{}, err
			}
			antes, err := tomar(ctx, t, q, in.ID, nucleo.PermTicketsResponder, in.Version)
			if err != nil {
				return TicketConMensaje{}, err
			}
			// Responder a un resuelto no lo reabre (para eso, reabrir); a un cerrado, no.
			if err := exigirEstado(antes, "responder", EstadoNuevo, EstadoAbierto, EstadoEnEspera, EstadoResuelto); err != nil {
				return TicketConMensaje{}, err
			}
			cal, err := cargarCalendario(ctx, q, antes.CalendarioID)
			if err != nil {
				return TicketConMensaje{}, err
			}
			despues := antes
			abrirSiNuevo(&despues)
			marcarRespuesta(&despues, ahora)
			f, err := guardar(ctx, t, q, antes, despues, cal)
			if err != nil {
				return TicketConMensaje{}, err
			}
			msj, err := mensaje(ctx, t, q, f, true, cuerpo)
			if err != nil {
				return TicketConMensaje{}, err
			}
			if err := cambioDeEstado(ctx, t, q, antes, f, ptr("respondido")); err != nil {
				return TicketConMensaje{}, err
			}
			if err := t.Emitir("ticket.respondido", map[string]any{"ticket_id": f.ID, "mensaje_id": msj.ID}); err != nil {
				return TicketConMensaje{}, err
			}
			return TicketConMensaje{Ticket: aTicket(f, ahora), Mensaje: aMensaje(msj)}, nil
		},
	})
}
