// Package store holds the pgx pool and every non-money SQL statement in UNOFA:
// users, rooms, matches, http_log, admin audit/flags and balance snapshots.
//
// Money never moves here; that is internal/ledger's job and it takes a pgx.Tx
// from its caller (docs/CONTRACTS.md §Store).
package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DB is what both *pgxpool.Pool and pgx.Tx provide; every query runs on it.
type DB interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Begin(ctx context.Context) (pgx.Tx, error)
	CopyFrom(ctx context.Context, table pgx.Identifier, cols []string, src pgx.CopyFromSource) (int64, error)
}

// Store is the persistence handle. Pool is exported so ledger/jobs can begin a
// transaction on the same pool.
type Store struct {
	Pool *pgxpool.Pool

	// db is Pool, or a caller transaction after InTx.
	db DB
	// timeout bounds every query issued through this package.
	timeout time.Duration
}

// New opens a pool and verifies it can reach Postgres. maxConns/minConns are
// clamped to sane defaults when zero; timeout bounds both dialling and each query.
func New(ctx context.Context, dsn string, maxConns, minConns int32, timeout time.Duration) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	if maxConns > 0 {
		cfg.MaxConns = maxConns
	}
	if minConns > 0 && minConns <= cfg.MaxConns {
		cfg.MinConns = minConns
	}
	if timeout > 0 {
		cfg.ConnConfig.ConnectTimeout = timeout
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	s := &Store{Pool: pool, db: pool, timeout: timeout}
	if err := s.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return s, nil
}

// Close releases every pooled connection.
func (s *Store) Close() { s.Pool.Close() }

// Ping checks the pool can serve a connection.
func (s *Store) Ping(ctx context.Context) error { return s.Pool.Ping(ctx) }

// InTx returns a Store whose queries run inside tx, so callers can combine
// store writes with ledger calls or other store writes atomically (e.g. the
// startup wipe: DeleteAllRooms + AbortAllRunning in one transaction, 03 §Phòng).
func (s *Store) InTx(tx pgx.Tx) *Store {
	return &Store{Pool: s.Pool, db: tx, timeout: s.timeout}
}

// tx runs fn in a transaction (a savepoint when s is already InTx).
func (s *Store) tx(ctx context.Context, fn func(pgx.Tx) error) error {
	return pgx.BeginFunc(ctx, s.db, fn)
}

// timeoutCtx applies the store query deadline when one is configured.
func (s *Store) timeoutCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	if s.timeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, s.timeout)
}
