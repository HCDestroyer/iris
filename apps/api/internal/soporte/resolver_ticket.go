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
			// Encuesta de satisfacción (una por ticket; no al maestro de un incidente).
			if err := m.generarEncuestaTicket(ctx, t, q, f); err != nil {
				return Ticket{}, err
			}
			if f.Maestro != nil {
				// El aviso a los hijos va en un job (tandas), no en esta transacción.
				if err := m.encolarAviso(ctx, t, f, AvisoResuelto, solucion); err != nil {
					return Ticket{}, err
				}
			}
			return aTicket(f, ahora), nil
		},
	})
}
