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

	// ClaveIdempotencia es la Idempotency-Key de la petición (la pone el handler): si la
	// misma operación ya dejó su mensaje, se devuelve ese sin repetir la acción.
	ClaveIdempotencia string `json:"-"`
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
			t.FijarEntidad("ticket", in.ID)
			idem := conIdempotencia(ctx, in.ClaveIdempotencia)
			if previo, err := mensajeRepetido(ctx, q, in.ID, idem, m.ahora()); previo != nil || err != nil {
				if previo != nil {
					t.FijarEmpresa(deref(previo.Ticket.EmpresaID))
					t.Anotar(map[string]any{"repetida": true, "mensaje_id": previo.Mensaje.ID})
					return *previo, nil
				}
				return TicketConMensaje{}, err
			}
			f, err := tomar(ctx, t, q, in.ID, nucleo.PermTicketsNotaInterna, in.Version)
			if err != nil {
				return TicketConMensaje{}, err
			}
			msj, err := mensajeIdempotente(ctx, t, q, f, false, cuerpo, idem)
			if err != nil {
				return TicketConMensaje{}, err
			}
			t.Anotar(map[string]any{"mensaje_id": msj.ID})
			return TicketConMensaje{Ticket: aTicket(f, m.ahora()), Mensaje: aMensaje(msj)}, nil
		},
	})
}
