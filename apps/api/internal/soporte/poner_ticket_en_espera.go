package soporte

import (
	"context"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	soportebd "github.com/HCDestroyer/iris/apps/api/internal/soporte/bd"
)

// EntradaPonerEnEspera es la entrada de tickets.poner_en_espera (abierto → en_espera). Pausa
// el reloj de resolución hasta reanudar, resolver o cerrar; el de respuesta sigue corriendo.
type EntradaPonerEnEspera struct {
	ID      int64  `json:"-"`
	Version int32  `json:"version" minimum:"1" doc:"Versión leída; si cambió, 409"`
	Motivo  string `json:"motivo" minLength:"3" maxLength:"500" doc:"Qué se espera (del cliente, de un proveedor…)"`
}

func (m *Modulo) nuevoPonerTicketEnEspera() *auditoria.Caso[EntradaPonerEnEspera, Ticket] {
	return auditoria.Nuevo(auditoria.Caso[EntradaPonerEnEspera, Ticket]{
		Accion:  "tickets.poner_en_espera",
		Permiso: nucleo.PermTicketsPonerEnEspera,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaPonerEnEspera) (Ticket, error) {
			q := soportebd.New(t)
			ahora := m.ahora()
			motivo, err := texto("motivo", in.Motivo, 3, 500)
			if err != nil {
				return Ticket{}, err
			}
			antes, err := tomar(ctx, t, q, in.ID, nucleo.PermTicketsPonerEnEspera, &in.Version)
			if err != nil {
				return Ticket{}, err
			}
			if err := exigirEstado(antes, "poner en espera", EstadoAbierto); err != nil {
				return Ticket{}, err
			}
			cal, err := cargarCalendario(ctx, q, antes.CalendarioID)
			if err != nil {
				return Ticket{}, err
			}
			despues := antes
			despues.Estado, despues.EnEsperaDesde = EstadoEnEspera, &ahora
			f, err := guardar(ctx, t, q, antes, despues, cal)
			if err != nil {
				return Ticket{}, err
			}
			if err := cambioDeEstado(ctx, t, q, antes, f, &motivo); err != nil {
				return Ticket{}, err
			}
			return aTicket(f, ahora), nil
		},
	})
}
