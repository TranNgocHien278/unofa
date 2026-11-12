package ledger

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
)

// Invariant returns SUM(users.balance) + SUM(matches.pot) from the spec's
// exact SQL (03 §Invariant lệch sổ). Result is 0 when the books balance.
// It must run inside the caller's transaction to see a consistent snapshot.
func Invariant(ctx context.Context, tx pgx.Tx) (int64, error) {
	const q = `SELECT (SELECT COALESCE(SUM(balance), 0) FROM users) +
	                 (SELECT COALESCE(SUM(pot), 0) FROM matches)`
	var sum int64
	if err := tx.QueryRow(ctx, q).Scan(&sum); err != nil {
		return 0, err
	}
	return sum, nil
}

// CheckInvariant runs Invariant outside any caller transaction (single-shot
// helper used by the hourly job and integration tests).  When the sum is
// non-zero it writes an admin_flags row of kind MONEY_INVARIANT.
//
// shortcut: opens its own pgx.Conn from pool via Begin; upgrade to pooled tx
// helper once the jobs package is wired.
func CheckInvariant(ctx context.Context, pool interface {
	Begin(context.Context) (pgx.Tx, error)
}, now time.Time) (int64, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	sum, err := Invariant(ctx, tx)
	if err != nil {
		return 0, err
	}
	if sum != 0 {
		detail, _ := json.Marshal(map[string]any{"sum": sum, "checked_at": now.UTC()})
		if _, err := tx.Exec(ctx,
			`INSERT INTO admin_flags (kind, detail) VALUES ('MONEY_INVARIANT', $1)`, detail); err != nil {
			return sum, err
		}
	}
	return sum, tx.Commit(ctx)
}
