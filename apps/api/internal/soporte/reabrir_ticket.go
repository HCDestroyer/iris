package soporte

import (
	"context"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	soportebd "github.com/HCDestroyer/iris/apps/api/internal/soporte/bd"
)

// EntradaReabrirTicket es la entrada de tickets.reabrir (resuelto → abierto). El reloj de
// resolución sigue donde quedó: el tiempo laboral en resuelto cuenta como pausa. Un ticket
// cerrado no se reabre (se abre otro).
type EntradaReabrirTicket struct {
	ID      int64  `json:"-"`
	Version int32  `json:"version" minimum:"1" doc:"Versión leída; si cambió, 409"`
	Motivo  string `json:"motivo" minLength:"3" maxLength:"500"`
}

func (m *Modulo) nuevoReabrirTicket() *auditoria.Caso[EntradaReabrirTicket, Ticket] {
	return auditoria.Nuevo(auditoria.Caso[EntradaReabrirTicket, Ticket]{
		Accion:  "tickets.reabrir",
		Permiso: nucleo.PermTicketsReabrir,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaReabrirTicket) (Ticket, error) {
			q := soportebd.New(t)
			ahora := m.ahora()
			motivo, err := texto("motivo", in.Motivo, 3, 500)
			if err != nil {
				return Ticket{}, err
			}
			antes, err := tomar(ctx, t, q, in.ID, nucleo.PermTicketsReabrir, &in.Version)
			if err != nil {
				return Ticket{}, err
			}
			if err := exigirEstado(antes, "reabrir", EstadoResuelto); err != nil {
				return Ticket{}, err
			}
			cal, err := cargarCalendario(ctx, q, antes.CalendarioID)
			if err != nil {
				return Ticket{}, err
			}
			despues := antes
			acumularPausa(&despues, cal, antes.ResueltoEn, ahora)
			despues.Estado, despues.ResueltoEn = EstadoAbierto, nil
			f, err := guardar(ctx, t, q, antes, despues, cal)
			if err != nil {
				return Ticket{}, err
			}
			if err := m.anularEncuestaTicket(ctx, t, q, f.ID, "reabierto"); err != nil {
				return Ticket{}, err
			}
			if err := cambioDeEstado(ctx, t, q, antes, f, &motivo); err != nil {
				return Ticket{}, err
			}
			return aTicket(f, ahora), nil
		},
	})
}
