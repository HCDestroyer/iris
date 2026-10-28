package pendientes

import (
	"context"
	"fmt"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	pendientesbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/pendientes/bd"
)

type eventoBloqueo struct {
	eventoPendiente
	DependeDe int64  `json:"depende_de"`
	Motivo    string `json:"motivo,omitempty"`
}

// bloquearDependientes: el pendiente id quedó estado (fallido_negocio, agotado): sus dependientes
// en espera pasan a bloqueado con motivo depende_de_<id>_fallido, y los de estos (en cadena) con
// depende_de_<id>_bloqueado. Emite integracion.pendiente_bloqueado por cada uno.
func bloquearDependientes(ctx context.Context, t *auditoria.Tx, id int64, estado string) error {
	q := pendientesbd.New(t)
	cola := []struct {
		id    int64
		causa string
	}{{id, "fallido"}}
	if estado == EstadoBloqueado {
		cola[0].causa = "bloqueado"
	}
	for len(cola) > 0 {
		dep := cola[0]
		cola = cola[1:]
		motivo := fmt.Sprintf("depende_de_%d_%s", dep.id, dep.causa)
		filas, err := q.BloquearDependientes(ctx, pendientesbd.BloquearDependientesParams{Motivo: &motivo, ID: dep.id})
		if err != nil {
			return err
		}
		for _, f := range filas {
			t.RegistrarCambios("pendiente", f.ID, map[string]any{"estado": EstadoPendiente}, map[string]any{"estado": f.Estado})
			if err := t.Emitir(EventoBloqueado, eventoBloqueo{eventoPendiente: eventoDe(f), DependeDe: dep.id, Motivo: motivo}); err != nil {
				return err
			}
			cola = append(cola, struct {
				id    int64
				causa string
			}{f.ID, "bloqueado"})
		}
	}
	return nil
}

// desbloquearDependientes: el pendiente id quedó hecho; vuelven a la cola los bloqueados cuyas
// dependencias están todas hechas. Emite integracion.pendiente_desbloqueado por cada uno.
func (m *Modulo) desbloquearDependientes(ctx context.Context, t *auditoria.Tx, id int64, ahora time.Time) error {
	filas, err := pendientesbd.New(t).DesbloquearDependientes(ctx, pendientesbd.DesbloquearDependientesParams{Ahora: ahora, ID: id})
	if err != nil {
		return err
	}
	for _, f := range filas {
		t.RegistrarCambios("pendiente", f.ID, map[string]any{"estado": EstadoBloqueado}, map[string]any{"estado": f.Estado})
		if err := t.Emitir(EventoDesbloqueado, eventoBloqueo{eventoPendiente: eventoDe(f), DependeDe: id}); err != nil {
			return err
		}
	}
	return nil
}
