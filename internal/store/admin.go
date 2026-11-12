package store

import (
	"context"
	"encoding/json"
	"net/netip"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/unofa/unofa/internal/errs"
)

// AuditEntry is one admin_audit row (written by the admin handlers).
type AuditEntry struct {
	ID            int64
	AdminUsername string
	Action        string
	TargetUserID  *int64
	Amount        *int64
	At            time.Time
	IP            *netip.Addr
	RequestID     string
}

// InsertAudit records an admin action. Every admin write path calls this.
func (s *Store) InsertAudit(ctx context.Context, e AuditEntry) (int64, error) {
	ctx, cancel := s.timeoutCtx(ctx)
	defer cancel()
	var id int64
	err := s.db.QueryRow(ctx, `INSERT INTO admin_audit
		(admin_username, action, target_user_id, amount, at, ip, request_id)
		VALUES ($1, $2, $3, $4, COALESCE($5, now()), $6, $7) RETURNING id`,
		e.AdminUsername, e.Action, e.TargetUserID, e.Amount, zeroToNull(e.At), e.IP, e.RequestID).Scan(&id)
	return id, err
}

// ListAudit pages the audit trail newest first; before = 0 starts at the top.
func (s *Store) ListAudit(ctx context.Context, limit int, before int64) ([]AuditEntry, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	ctx, cancel := s.timeoutCtx(ctx)
	defer cancel()
	rows, err := s.db.Query(ctx, `SELECT id, admin_username, action, target_user_id, amount, at, ip, request_id
		FROM admin_audit WHERE ($1 = 0 OR id < $1) ORDER BY id DESC LIMIT $2`, before, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (AuditEntry, error) {
		var e AuditEntry
		err := r.Scan(&e.ID, &e.AdminUsername, &e.Action, &e.TargetUserID, &e.Amount, &e.At, &e.IP, &e.RequestID)
		return e, err
	})
}

// Flag is one admin_flags row.
type Flag struct {
	ID        int64
	Kind      string
	Detail    json.RawMessage
	At        time.Time
	ClearedAt *time.Time
}

// InsertFlag records a system flag (MONEY_INVARIANT, seize failures, ...).
// A nil detail becomes '{}'.
func (s *Store) InsertFlag(ctx context.Context, kind string, detail json.RawMessage) (int64, error) {
	if len(detail) == 0 {
		detail = json.RawMessage(`{}`)
	}
	ctx, cancel := s.timeoutCtx(ctx)
	defer cancel()
	var id int64
	err := s.db.QueryRow(ctx, `INSERT INTO admin_flags (kind, detail) VALUES ($1, $2) RETURNING id`,
		kind, detail).Scan(&id)
	return id, err
}

// ListOpenFlags returns uncleared flags, newest first (the open index order).
func (s *Store) ListOpenFlags(ctx context.Context, limit int) ([]Flag, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	ctx, cancel := s.timeoutCtx(ctx)
	defer cancel()
	rows, err := s.db.Query(ctx, `SELECT id, kind, detail, at, cleared_at FROM admin_flags
		WHERE cleared_at IS NULL ORDER BY at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Flag, error) {
		var f Flag
		err := r.Scan(&f.ID, &f.Kind, &f.Detail, &f.At, &f.ClearedAt)
		return f, err
	})
}

// ClearFlag stamps cleared_at; clearing twice keeps the first timestamp.
func (s *Store) ClearFlag(ctx context.Context, id int64, now time.Time) error {
	ctx, cancel := s.timeoutCtx(ctx)
	defer cancel()
	tag, err := s.db.Exec(ctx, `UPDATE admin_flags SET cleared_at = COALESCE(cleared_at, $2) WHERE id = $1`, id, now)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errs.New(errs.NotFound)
	}
	return nil
}

func zeroToNull(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}
