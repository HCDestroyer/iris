package soporte

import (
	"context"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	soportebd "github.com/HCDestroyer/iris/apps/api/internal/soporte/bd"
)

// EntradaResolverTicket es la entrada de tickets.resolver (nuevo, abierto o en_espera →
// resuelto). La solución queda como mensaje público y cuenta como respuesta si no la había.
type EntradaResolverTicket struct {
	ID       int64  `json:"-"`
	Version  int32  `json:"version" minimum:"1" doc:"Versión leída; si cambió, 409"`
	Solucion string `json:"solucion" minLength:"3" maxLength:"20000" doc:"Qué se hizo; la ve el cliente"`
}

func (m *Modulo) nuevoResolverTicket() *auditoria.Caso[EntradaResolverTicket, Ticket] {
	return auditoria.Nuevo(auditoria.Caso[EntradaResolverTicket, Ticket]{
		Accion:  "tickets.resolver",
		Permiso: nucleo.PermTicketsResolver,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaResolverTicket) (Ticket, error) {
			q := soportebd.New(t)
			ahora := m.ahora()
			solucion, err := texto("solucion", in.Solucion, 3, 20000)
			if err != nil {
				return Ticket{}, err
			}
			antes, err := tomar(ctx, t, q, in.ID, nucleo.PermTicketsResolver, &in.Version)
			if err != nil {
				return Ticket{}, err
			}
			if err := exigirEstado(antes, "resolver", EstadoNuevo, EstadoAbierto, EstadoEnEspera); err != nil {
				return Ticket{}, err
			}
			cal, err := cargarCalendario(ctx, q, antes.CalendarioID)
			if err != nil {
				return Ticket{}, err
			}
			despues := antes
			salirDeEspera(&despues, cal, ahora)
			despues.Estado, despues.ResueltoEn = EstadoResuelto, &ahora
			marcarRespuesta(&despues, ahora)
			f, err := guardar(ctx, t, q, antes, despues, cal)
			if err != nil {
				return Ticket{}, err
			}
			if _, err := mensaje(ctx, t, q, f, true, solucion); err != nil {
				return Ticket{}, err
			}
			if err := cambioDeEstado(ctx, t, q, antes, f, ptr("resuelto")); err != nil {
				return Ticket{}, err
			}
			if f.Maestro != nil {
				if err := notificarHijos(ctx, t, q, f, solucion); err != nil {
					return Ticket{}, err
				}
			}
			return aTicket(f, ahora), nil
		},
	})
}

// notificarHijos avisa a cada hijo no cerrado de un incidente maestro que se resolvió: mensaje
// público de Iris y ticket.respondido (así sale por el portal o, si el hijo vino por correo, por
// correo al contacto). No cambia el estado de los hijos ni cuenta como su primera respuesta: un
// agente los resuelve (el cliente puede seguir con problemas propios).
func notificarHijos(ctx context.Context, t *auditoria.Tx, q *soportebd.Queries, maestro soportebd.SoporteTicket, solucion string) error {
	hijos, err := q.HijosNoCerrados(ctx, maestro.ID)
	if err != nil {
		return err
	}
	for _, h := range hijos {
		cuerpo := "El incidente de red que afectaba tu servicio se resolvió: " + solucion +
			"\n\nSi todavía tienes problemas, responde a este mensaje."
		msj, err := mensajeSistema(ctx, q, h, true, cuerpo)
		if err != nil {
			return err
		}
		if err := t.Emitir("ticket.respondido", map[string]any{"ticket_id": h.ID, "mensaje_id": msj.ID}); err != nil {
			return err
		}
	}
	t.Anotar(map[string]any{"hijos_notificados": len(hijos)})
	return nil
}
