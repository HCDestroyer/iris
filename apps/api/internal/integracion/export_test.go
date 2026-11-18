package integracion

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
)

// AlVaciar fija el gancho que corre tras el último barrido vacío del relay, con el candado tomado.
func (r *Relay) AlVaciar(f func()) { r.alVaciar = f }

// EscucharOutboxConListo es EscucharOutbox con el aviso de «escuchando y barrido encolado».
func EscucharOutboxConListo(ctx context.Context, pool *pgxpool.Pool, cliente *river.Client[pgx.Tx], log *slog.Logger, listo func(pid uint32)) {
	escucharOutbox(ctx, pool, cliente, log, listo)
}
