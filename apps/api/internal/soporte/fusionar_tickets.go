package soporte

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	soportebd "github.com/HCDestroyer/iris/apps/api/internal/soporte/bd"
)

// EntradaFusionarTickets es la entrada de tickets.fusionar: el ticket {id} (duplicado) se
// cierra fusionado en destinoId, que sigue su curso. Los dos quedan con una nota interna.
type EntradaFusionarTickets struct {
	ID        int64   `json:"-"`
	Version   int32   `json:"version" minimum:"1" doc:"Versión leída del ticket duplicado; si cambió, 409"`
	DestinoID int64   `json:"destinoId" minimum:"1" doc:"Ticket que absorbe al duplicado"`
	Motivo    *string `json:"motivo,omitempty" maxLength:"500"`
}

// ResultadoFusion son los dos tickets después de fusionar.
type ResultadoFusion struct {
	Origen  Ticket `json:"origen" doc:"El duplicado, ya cerrado"`
	Destino Ticket `json:"destino"`
}

func fusionInvalida(detalle string) *nucleo.Problema {
	return nucleo.ReglaNegocio("fusion_invalida", detalle)
}

func (m *Modulo) nuevoFusionarTickets() *auditoria.Caso[EntradaFusionarTickets, ResultadoFusion] {
	return auditoria.Nuevo(auditoria.Caso[EntradaFusionarTickets, ResultadoFusion]{
		Accion:  "tickets.fusionar",
		Permiso: nucleo.PermTicketsFusionar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaFusionarTickets) (ResultadoFusion, error) {
			q := soportebd.New(t)
			ahora := m.ahora()
			motivo, err := textoOpcional("motivo", in.Motivo, 500)
			if err != nil {
				return ResultadoFusion{}, err
			}
			if in.DestinoID == in.ID {
				return ResultadoFusion{}, fusionInvalida("Un ticket no se fusiona consigo mismo")
			}
			// Acceso antes de bloquear: sin permiso sobre alguno de los dos, no se toma ningún
			// candado (lectura sin candado; se verifica de nuevo con la fila bloqueada).
			t.FijarEntidad("ticket", in.ID)
			if err := accesoFusion(ctx, q, in.ID, in.DestinoID); err != nil {
				return ResultadoFusion{}, err
			}
			// Candados en orden de id: dos fusiones cruzadas no se bloquean entre sí.
			for _, id := range []int64{min(in.ID, in.DestinoID), max(in.ID, in.DestinoID)} {
				if _, err := q.TicketPorIDParaActualizar(ctx, id); err != nil && !errors.Is(err, pgx.ErrNoRows) {
					return ResultadoFusion{}, err
				}
			}
			antes, err := tomar(ctx, t, q, in.ID, nucleo.PermTicketsFusionar, &in.Version)
			if err != nil {
				return ResultadoFusion{}, err
			}
			if err := exigirEstado(antes, "fusionar", EstadoNuevo, EstadoAbierto, EstadoEnEspera, EstadoResuelto); err != nil {
				return ResultadoFusion{}, err
			}
			destino, err := q.TicketPorIDParaActualizar(ctx, in.DestinoID)
			if errors.Is(err, pgx.ErrNoRows) {
				return ResultadoFusion{}, fusionInvalida("El ticket destino no existe")
			}
			if err != nil {
				return ResultadoFusion{}, err
			}
			if err := accesoDestino(ctx, destino); err != nil {
				return ResultadoFusion{}, err
			}
			if destino.Estado == EstadoCerrado {
				return ResultadoFusion{}, fusionInvalida("El ticket destino está cerrado")
			}
			if deref(destino.EmpresaID) != deref(antes.EmpresaID) {
				return ResultadoFusion{}, fusionInvalida("Los tickets son de empresas distintas")
			}

			cal, err := cargarCalendario(ctx, q, antes.CalendarioID)
			if err != nil {
				return ResultadoFusion{}, err
			}
			despues := cerrar(antes, cal, ahora)
			despues.FusionadoEnID = &destino.ID
			f, err := guardar(ctx, t, q, antes, despues, cal)
			if err != nil {
				return ResultadoFusion{}, err
			}
			if err := m.anularEncuestaTicket(ctx, t, q, f.ID, "fusionado"); err != nil {
				return ResultadoFusion{}, err
			}
			nota := fmt.Sprintf("Fusionado en %s.", destino.Numero)
			if motivo != nil {
				nota += " " + *motivo
			}
			if _, err := mensaje(ctx, t, q, f, false, nota); err != nil {
				return ResultadoFusion{}, err
			}
			if _, err := mensaje(ctx, t, q, destino, false, fmt.Sprintf("Se fusionó %s: %s", f.Numero, f.Asunto)); err != nil {
				return ResultadoFusion{}, err
			}
			if err := historial(ctx, t, q, f, "fusionado", &f.Numero, &destino.Numero, motivo, map[string]any{"destino_id": destino.ID}); err != nil {
				return ResultadoFusion{}, err
			}
			if err := historial(ctx, t, q, destino, "fusionado", &f.Numero, &destino.Numero, motivo, map[string]any{"origen_id": f.ID}); err != nil {
				return ResultadoFusion{}, err
			}
			if err := cambioDeEstado(ctx, t, q, antes, f, ptr("fusionado en "+destino.Numero)); err != nil {
				return ResultadoFusion{}, err
			}
			if err := t.Emitir("ticket.fusionado", map[string]any{"ticket_id": f.ID, "destino_id": destino.ID, "motivo": motivo}); err != nil {
				return ResultadoFusion{}, err
			}
			// El destino recibió mensaje e historial: sube su versión (quien lo tenga abierto
			// recarga) y se devuelve actualizado.
			if destino, err = q.TocarTicket(ctx, destino.ID); err != nil {
				return ResultadoFusion{}, err
			}
			return ResultadoFusion{Origen: aTicket(f, ahora), Destino: aTicket(destino, ahora)}, nil
		},
	})
}

// accesoFusion verifica, sin candados, que el actor puede fusionar el origen y el destino.
func accesoFusion(ctx context.Context, q *soportebd.Queries, origenID, destinoID int64) error {
	origen, err := q.TicketPorID(ctx, origenID)
	if errors.Is(err, pgx.ErrNoRows) {
		return noEncontrado()
	}
	if err != nil {
		return err
	}
	if err := verificarAcceso(ctx, nucleo.PermTicketsFusionar, origen); err != nil {
		return err
	}
	destino, err := q.TicketPorID(ctx, destinoID)
	if errors.Is(err, pgx.ErrNoRows) {
		return fusionInvalida("El ticket destino no existe")
	}
	if err != nil {
		return err
	}
	return accesoDestino(ctx, destino)
}

// accesoDestino: un destino que el actor no puede ver se reporta como inexistente (422).
func accesoDestino(ctx context.Context, destino soportebd.SoporteTicket) error {
	if err := verificarAcceso(ctx, nucleo.PermTicketsFusionar, destino); err != nil {
		var p *nucleo.Problema
		if errors.As(err, &p) && p.Status == 404 {
			return fusionInvalida("El ticket destino no existe")
		}
		return err
	}
	return nil
}
