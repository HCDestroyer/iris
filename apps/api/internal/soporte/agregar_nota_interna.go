package soporte

import (
	"context"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	soportebd "github.com/HCDestroyer/iris/apps/api/internal/soporte/bd"
)

// EntradaNotaInterna es la entrada de tickets.nota_interna: un mensaje que el cliente no ve.
// No cambia el estado ni cuenta como respuesta; se permite en cualquier estado.
type EntradaNotaInterna struct {
	ID      int64  `json:"-"`
	Version *int32 `json:"version,omitempty" minimum:"1" doc:"Versión leída (opcional); si se envía y cambió, 409"`
	Cuerpo  string `json:"cuerpo" minLength:"1" maxLength:"20000"`
}

func (m *Modulo) nuevoAgregarNotaInterna() *auditoria.Caso[EntradaNotaInterna, TicketConMensaje] {
	return auditoria.Nuevo(auditoria.Caso[EntradaNotaInterna, TicketConMensaje]{
		Accion:  "tickets.nota_interna",
		Permiso: nucleo.PermTicketsNotaInterna,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaNotaInterna) (TicketConMensaje, error) {
			q := soportebd.New(t)
			cuerpo, err := texto("cuerpo", in.Cuerpo, 1, 20000)
			if err != nil {
				return TicketConMensaje{}, err
			}
			f, err := tomar(ctx, t, q, in.ID, nucleo.PermTicketsNotaInterna, in.Version)
			if err != nil {
				return TicketConMensaje{}, err
			}
			msj, err := mensaje(ctx, t, q, f, false, cuerpo)
			if err != nil {
				return TicketConMensaje{}, err
			}
			t.Anotar(map[string]any{"mensaje_id": msj.ID})
			return TicketConMensaje{Ticket: aTicket(f, m.ahora()), Mensaje: aMensaje(msj)}, nil
		},
	})
}
