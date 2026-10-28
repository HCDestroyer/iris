package nucleo

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// BD es lo mínimo de un pool de pgx que necesita EnTx (*pgxpool.Pool lo cumple).
type BD interface {
	Begin(ctx context.Context) (pgx.Tx, error)
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

var _ BD = (*pgxpool.Pool)(nil)

type claveBD int

const (
	clavePool claveBD = iota
	claveTx
)

// ConBD deja el pool en el contexto. Lo hacen el servidor HTTP (middleware), el worker y la
// consola al arrancar; EnTx lo toma de ahí.
func ConBD(ctx context.Context, bd BD) context.Context {
	return context.WithValue(ctx, clavePool, bd)
}

// BDDe devuelve el pool del contexto (nil si no hay).
func BDDe(ctx context.Context) BD {
	bd, _ := ctx.Value(clavePool).(BD)
	return bd
}

// TxDe devuelve la transacción en curso del contexto (nil si no hay).
func TxDe(ctx context.Context) pgx.Tx {
	tx, _ := ctx.Value(claveTx).(pgx.Tx)
	return tx
}

// ErrSinBD: el contexto no trae pool (falta ConBD al arrancar).
var ErrSinBD = errors.New("nucleo: el contexto no tiene base de datos (falta nucleo.ConBD)")

// EnTx corre fn en una transacción. Si el contexto ya trae una (un caso de uso que llama a
// otro), abre un savepoint dentro de ella: así el caso anidado se revierte solo, y todo se
// confirma o se revierte con la transacción externa. El ctx que recibe fn lleva la transacción.
//
// Si fn devuelve error (o entra en pánico) se hace ROLLBACK; si no, COMMIT.
func EnTx(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) (err error) {
	var tx pgx.Tx
	if externa := TxDe(ctx); externa != nil {
		tx, err = externa.Begin(ctx) // savepoint
	} else {
		bd := BDDe(ctx)
		if bd == nil {
			return ErrSinBD
		}
		tx, err = bd.Begin(ctx)
	}
	if err != nil {
		return fmt.Errorf("abrir transacción: %w", err)
	}

	defer func() {
		if r := recover(); r != nil {
			_ = tx.Rollback(context.WithoutCancel(ctx))
			panic(r)
		}
		if err != nil {
			_ = tx.Rollback(context.WithoutCancel(ctx))
		}
	}()

	if err = fn(context.WithValue(ctx, claveTx, tx), tx); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("confirmar transacción: %w", err)
	}
	return nil
}
