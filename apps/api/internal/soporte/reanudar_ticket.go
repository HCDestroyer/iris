package soporte

import (
	"context"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	soportebd "github.com/HCDestroyer/iris/apps/api/internal/soporte/bd"
)

// EntradaReanudarTicket es la entrada de tickets.reanudar (en_espera → abierto). El tiempo
// laboral en espera se suma a la pausa y el vencimiento de resolución se corre.
type EntradaReanudarTicket struct {
	ID      int64   `json:"-"`
	Version int32   `json:"version" minimum:"1" doc:"Versión leída; si cambió, 409"`
	Motivo  *string `json:"motivo,omitempty" maxLength:"500"`
}

func (m *Modulo) nuevoReanudarTicket() *auditoria.Caso[EntradaReanudarTicket, Ticket] {
	return auditoria.Nuevo(auditoria.Caso[EntradaReanudarTicket, Ticket]{
		Accion:  "tickets.reanudar",
		Permiso: nucleo.PermTicketsReanudar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaReanudarTicket) (Ticket, error) {
			q := soportebd.New(t)
			ahora := m.ahora()
			motivo, err := textoOpcional("motivo", in.Motivo, 500)
			if err != nil {
				return Ticket{}, err
			}
			antes, err := tomar(ctx, t, q, in.ID, nucleo.PermTicketsReanudar, &in.Version)
			if err != nil {
				return Ticket{}, err
			}
			if err := exigirEstado(antes, "reanudar", EstadoEnEspera); err != nil {
				return Ticket{}, err
			}
			cal, err := cargarCalendario(ctx, q, antes.CalendarioID)
			if err != nil {
				return Ticket{}, err
			}
			despues := antes
			salirDeEspera(&despues, cal, ahora)
			despues.Estado = EstadoAbierto
			f, err := guardar(ctx, t, q, antes, despues, cal)
			if err != nil {
				return Ticket{}, err
			}
			if err := cambioDeEstado(ctx, t, q, antes, f, motivo); err != nil {
				return Ticket{}, err
			}
			return aTicket(f, ahora), nil
		},
	})
}
