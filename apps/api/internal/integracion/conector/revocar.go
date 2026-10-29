package conector

import (
	"context"
	"log/slog"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Revocación del iris_pat_ entregado en un canje (canje fallido, conector desactivado o
// reemplazado). Si la revocación inmediata falla, se encola un reintento: el token no puede
// quedar vivo porque la base o identidad tuvieron un problema pasajero.

// ArgsRevocarToken reintenta revocar un token entregado. Único por token.
type ArgsRevocarToken struct {
	TokenID int64 `json:"token_id"`
}

// Kind implementa river.JobArgs.
func (ArgsRevocarToken) Kind() string { return "integracion.revocar_token_entregado" }

// InsertOpts: único por token, con muchos intentos (la espera exponencial de River).
func (ArgsRevocarToken) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: integracion.ColaIntegracion, MaxAttempts: 25, UniqueOpts: river.UniqueOpts{ByArgs: true}}
}

// revocarEntregado revoca el token o, si falla, encola el reintento (en tx si la hay; si no, en
// una transacción propia). Nunca devuelve error: lo peor es que el reintento quede en el log.
func (n *Nucleo) revocarEntregado(ctx context.Context, tx pgx.Tx, id int64, motivo string) {
	tokens := n.tokensFijados()
	var err error
	if tokens == nil {
		err = errSinTokens
	} else if err = tokens.RevocarTokenIntegracion(ctx, id, motivo); err == nil {
		return
	}
	slog.WarnContext(ctx, "conector: no se pudo revocar el token entregado; se reintenta", "token_id", id, "motivo", motivo, "err", err)
	if n.cfg.Encolar == nil {
		slog.ErrorContext(ctx, "conector: sin cola para reintentar la revocación", "token_id", id)
		return
	}
	encolar := func(ctx context.Context, tx pgx.Tx) error {
		return n.cfg.Encolar(ctx, tx, ArgsRevocarToken{TokenID: id})
	}
	if tx != nil {
		err = encolar(ctx, tx)
	} else {
		err = nucleo.EnTx(context.WithoutCancel(ctx), encolar)
	}
	if err != nil {
		slog.ErrorContext(ctx, "conector: no se pudo encolar la revocación del token entregado", "token_id", id, "err", err)
	}
}

var errSinTokens = nucleo.ReglaNegocio("emisor_token_no_configurado", "Iris no tiene configurado el emisor de tokens de integración")

type trabajadorRevocar struct {
	river.WorkerDefaults[ArgsRevocarToken]
	bd     nucleo.BD
	tokens Tokens
}

func (w *trabajadorRevocar) Work(ctx context.Context, j *river.Job[ArgsRevocarToken]) error {
	ctx = nucleo.ConBD(contexto.Sistema(ctx, contexto.CanalWorker), w.bd)
	err := w.tokens.RevocarTokenIntegracion(ctx, j.Args.TokenID, "revocacion_pendiente")
	if p, _ := nucleo.AProblema(err); err != nil && p != nil && p.Codigo() == "no_encontrado" {
		slog.WarnContext(ctx, "conector: el token a revocar ya no existe", "token_id", strconv.FormatInt(j.Args.TokenID, 10))
		return nil
	}
	return err
}

// RegistrarRevocador agrega el reintento de revocación al worker (cmd/worker).
func RegistrarRevocador(workers *river.Workers, bd nucleo.BD, tokens Tokens) {
	river.AddWorker(workers, &trabajadorRevocar{bd: bd, tokens: tokens})
}
