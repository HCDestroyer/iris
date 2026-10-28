package soporte

import (
	"context"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	soportebd "github.com/HCDestroyer/iris/apps/api/internal/soporte/bd"
)

// EntradaCerrarTicket es la entrada de tickets.cerrar. Desde resuelto, el motivo es opcional;
// desde nuevo, abierto o en_espera (cerrar sin resolver: spam, duplicado, desistió) es
// obligatorio. Cerrado es final.
type EntradaCerrarTicket struct {
	ID      int64   `json:"-"`
	Version int32   `json:"version" minimum:"1" doc:"Versión leída; si cambió, 409"`
	Motivo  *string `json:"motivo,omitempty" maxLength:"500" doc:"Obligatorio si el ticket no está resuelto"`
}

func (m *Modulo) nuevoCerrarTicket() *auditoria.Caso[EntradaCerrarTicket, Ticket] {
	return auditoria.Nuevo(auditoria.Caso[EntradaCerrarTicket, Ticket]{
		Accion:  "tickets.cerrar",
		Permiso: nucleo.PermTicketsCerrar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaCerrarTicket) (Ticket, error) {
			q := soportebd.New(t)
			ahora := m.ahora()
			motivo, err := textoOpcional("motivo", in.Motivo, 500)
			if err != nil {
				return Ticket{}, err
			}
			antes, err := tomar(ctx, t, q, in.ID, nucleo.PermTicketsCerrar, &in.Version)
			if err != nil {
				return Ticket{}, err
			}
			if err := exigirEstado(antes, "cerrar", EstadoNuevo, EstadoAbierto, EstadoEnEspera, EstadoResuelto); err != nil {
				return Ticket{}, err
			}
			if antes.Estado != EstadoResuelto && motivo == nil {
				return Ticket{}, campoObligatorio("motivo")
			}
			cal, err := cargarCalendario(ctx, q, antes.CalendarioID)
			if err != nil {
				return Ticket{}, err
			}
			f, err := guardar(ctx, t, q, antes, cerrar(antes, cal, ahora), cal)
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

// cerrar devuelve el ticket cerrado (cierra la pausa en curso si estaba en espera).
func cerrar(f soportebd.SoporteTicket, cal *Calendario, ahora time.Time) soportebd.SoporteTicket {
	salirDeEspera(&f, cal, ahora)
	f.Estado, f.CerradoEn = EstadoCerrado, &ahora
	return f
}
