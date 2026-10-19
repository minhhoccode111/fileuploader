package postgres

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type (
	// Querier is the query surface shared by a connection pool and a transaction.
	Querier interface {
		Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
		Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
		QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	}

	// Pool is the connection pool the package talks to. *pgxpool.Pool satisfies it.
	Pool interface {
		Querier
		BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error)
		Close()
	}
)

// txKey is the context key the running transaction is stored under.
type txKey struct{}

// Querier returns the transaction bound to ctx, or the pool when the context
// carries no transaction.
func (p *Postgres) Querier(ctx context.Context) Querier {
	q, ok := ctx.Value(txKey{}).(Querier)
	if !ok {
		return p.pool
	}

	return q
}

// Do runs fn inside a single database transaction. The transaction is committed
// when fn returns nil and rolled back otherwise. The context handed to fn
// carries the transaction, so repositories reach it through Querier. A Do
// called on a context that already carries a transaction reuses that one
// instead of opening a second.
func (p *Postgres) Do(ctx context.Context, fn func(context.Context) error) error {
	if _, ok := ctx.Value(txKey{}).(Querier); ok {
		return fn(ctx)
	}

	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("postgres - Do - p.pool.BeginTx: %w", err)
	}

	committed := false

	// The rollback is deferred so a panicking callback cannot leak the
	// connection it holds.
	defer func() {
		if !committed {
			rollback(ctx, tx)
		}
	}()

	err = fn(context.WithValue(ctx, txKey{}, Querier(tx)))
	if err != nil {
		return err
	}

	err = tx.Commit(ctx)
	if err != nil {
		return fmt.Errorf("postgres - Do - tx.Commit: %w", err)
	}

	committed = true

	return nil
}

// rollback undoes a transaction. The context is stripped of its cancellation so
// a failed request still releases the connection.
func rollback(ctx context.Context, tx pgx.Tx) {
	err := tx.Rollback(context.WithoutCancel(ctx))
	if err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		log.Printf("postgres - rollback - tx.Rollback: %v", err)
	}
}
