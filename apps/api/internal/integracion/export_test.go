package integracion

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
)

// AlVaciar fija el gancho que corre tras el último barrido vacío del relay, con el candado tomado.
func (r *Relay) AlVaciar(f func()) { r.alVaciar = f }

// AlTerminar fija el gancho que corre al terminar Ejecutar (candado suelto, segunda mirada hecha).
func (r *Relay) AlTerminar(f func()) { r.alTerminar = f }

// GanchosEscucha expone los ganchos del listener del outbox.
type GanchosEscucha struct {
	Listo             func(pid uint32)
	AntesDeReconectar func(espera time.Duration)
}

// EscucharOutboxConGanchos es EscucharOutbox con ganchos de prueba.
func EscucharOutboxConGanchos(ctx context.Context, pool *pgxpool.Pool, cliente *river.Client[pgx.Tx], log *slog.Logger, g GanchosEscucha) {
	escucharOutbox(ctx, pool, cliente, log, ganchosEscucha{listo: g.Listo, antesDeReconectar: g.AntesDeReconectar})
}
