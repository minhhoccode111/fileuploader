package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// errBoom is the failure the fakes report.
var errBoom = errors.New("boom")

// fakePool is a pool that hands out a preset transaction.
type fakePool struct {
	tx     pgx.Tx
	begun  int
	closed bool
}

func (f *fakePool) Exec(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}

func (f *fakePool) Query(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
	return nil, nil
}

func (f *fakePool) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row {
	return nil
}

// BeginTx -.
func (f *fakePool) BeginTx(_ context.Context, _ pgx.TxOptions) (pgx.Tx, error) { //nolint:gocritic // the signature has to match pgxpool.Pool.BeginTx.
	f.begun++

	return f.tx, nil
}

func (f *fakePool) Close() {
	f.closed = true
}

// Stat returns nil: a fake has no pool to inspect.
func (f *fakePool) Stat() *pgxpool.Stat {
	return nil
}

// fakeLogger records what the package reports through the injected logger.
type fakeLogger struct {
	errs []any
}

func (f *fakeLogger) Info(_ string, _ ...any) {}

func (f *fakeLogger) Error(message any, _ ...any) {
	f.errs = append(f.errs, message)
}

// fakeTx records how the transaction ended.
type fakeTx struct {
	pgx.Tx

	committed   bool
	rolledBack  bool
	rollbackErr error
}

func (f *fakeTx) Commit(_ context.Context) error {
	f.committed = true

	return nil
}

func (f *fakeTx) Rollback(_ context.Context) error {
	f.rolledBack = true

	return f.rollbackErr
}

func TestDoCommitsWhenTheCallbackSucceeds(t *testing.T) {
	t.Parallel()

	tx := &fakeTx{}
	pg := &Postgres{pool: &fakePool{tx: tx}}

	err := pg.Do(context.Background(), func(_ context.Context) error {
		return nil
	})
	if err != nil {
		t.Fatalf("Do() error = %v, want nil", err)
	}

	if !tx.committed {
		t.Error("Do() did not commit the transaction")
	}

	if tx.rolledBack {
		t.Error("Do() rolled back a transaction it committed")
	}
}

func TestDoRollsBackWhenTheCallbackFails(t *testing.T) {
	t.Parallel()

	tx := &fakeTx{}
	pg := &Postgres{pool: &fakePool{tx: tx}}

	err := pg.Do(context.Background(), func(_ context.Context) error {
		return errBoom
	})
	if !errors.Is(err, errBoom) {
		t.Fatalf("Do() error = %v, want %v", err, errBoom)
	}

	if !tx.rolledBack {
		t.Error("Do() did not roll back the failed transaction")
	}

	if tx.committed {
		t.Error("Do() committed a failed transaction")
	}
}

func TestDoRollsBackWhenTheCallbackPanics(t *testing.T) {
	t.Parallel()

	tx := &fakeTx{}
	pg := &Postgres{pool: &fakePool{tx: tx}}

	var err error

	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Error("Do() swallowed the panic")
			}
		}()

		err = pg.Do(context.Background(), func(_ context.Context) error {
			panic("boom")
		})
	}()

	if err != nil {
		t.Errorf("Do() error = %v, want nil", err)
	}

	if !tx.rolledBack {
		t.Error("Do() did not roll back the panicked transaction")
	}
}

func TestDoHandsTheTransactionToTheCallback(t *testing.T) {
	t.Parallel()

	tx := &fakeTx{}
	pg := &Postgres{pool: &fakePool{tx: tx}}

	var got Querier

	err := pg.Do(context.Background(), func(ctx context.Context) error {
		got = pg.Querier(ctx)

		return nil
	})
	if err != nil {
		t.Fatalf("Do() error = %v, want nil", err)
	}

	if got != Querier(tx) {
		t.Errorf("Querier() = %v, want the transaction", got)
	}
}

func TestDoReusesTheRunningTransaction(t *testing.T) {
	t.Parallel()

	tx := &fakeTx{}
	pool := &fakePool{tx: tx}
	pg := &Postgres{pool: pool}

	var got Querier

	err := pg.Do(context.Background(), func(ctx context.Context) error {
		return pg.Do(ctx, func(innerCtx context.Context) error {
			got = pg.Querier(innerCtx)

			return nil
		})
	})
	if err != nil {
		t.Fatalf("Do() error = %v, want nil", err)
	}

	if pool.begun != 1 {
		t.Errorf("BeginTx() called %d times, want 1", pool.begun)
	}

	if got != Querier(tx) {
		t.Errorf("Querier() = %v, want the running transaction", got)
	}
}

func TestDoLogsARollbackFailureThroughTheInjectedLogger(t *testing.T) {
	t.Parallel()

	tx := &fakeTx{rollbackErr: errBoom}
	l := &fakeLogger{}
	pg := &Postgres{pool: &fakePool{tx: tx}}
	WithLogger(l)(pg)

	err := pg.Do(context.Background(), func(_ context.Context) error {
		return errBoom
	})
	if !errors.Is(err, errBoom) {
		t.Fatalf("Do() error = %v, want %v", err, errBoom)
	}

	if len(l.errs) == 0 {
		t.Error("Do() did not log the rollback failure")
	}
}

func TestQuerierReturnsThePoolOutsideATransaction(t *testing.T) {
	t.Parallel()

	pool := &fakePool{}
	pg := &Postgres{pool: pool}

	if got := pg.Querier(context.Background()); got != Querier(pool) {
		t.Errorf("Querier() = %v, want the pool", got)
	}
}
