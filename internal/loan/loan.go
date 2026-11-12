// Package loan implements the loan service on top of internal/ledger.
// Every function that writes to the DB accepts a caller-provided pgx.Tx.
// DeleteAccount is the only function that is multi-step within a single
// caller-provided transaction; it documents which side effects must be
// performed by the caller afterwards.
package loan

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/unofa/unofa/internal/errs"
	"github.com/unofa/unofa/internal/ledger"
)

// Term names matching docs/spec/03-data-model.md §Loan.
const (
	Term24h  = "24h"
	Term3d   = "3d"
	Term30d  = "30d"
)

// TermCfg is one entry in LoanCfg.Terms.
type TermCfg struct {
	Name     string        `yaml:"name"`
	Duration time.Duration `yaml:"duration"`
	RateBps  int           `yaml:"rate_bps"`
}

// LoanCfg carries the knobs the HTTP layer and job pass in (from config.Loan).
type LoanCfg struct {
	Packages      []int64   `yaml:"packages"`
	Terms         []TermCfg `yaml:"terms"`
	OverdueBps    int       `yaml:"overdue_bps"`
	CapMultiplier int64     `yaml:"cap_multiplier"`
}

// Loan is the row returned by Disburse.
type Loan struct {
	LoanID    int64
	UserID    int64
	Principal int64
	Term      string
	RateBps   int
	DueAt     time.Time
	Status    string
}

// ActiveLoan is the shape returned by Active (04 §GET /api/loans/active).
type ActiveLoan struct {
	LoanID        int64
	Principal     int64
	DueAt         time.Time
	OwendNow      int64
	Overdue       bool
	PeriodsOverdue int
	SeizeAt       time.Time
}

// SeizedLoan is returned by SeizeDue for each loan seized in the hourly job.
type SeizedLoan struct {
	LoanID   int64
	UserID   int64
	Seized   int64
	Forgiven int64
}

// DeleteAccountResult carries the side effects the HTTP handler must execute
// after the SQL parts of DeleteAccount succeed (session teardown, WS close, etc.).
type DeleteAccountResult struct {
	SessionKey  string
	WSUserID    int64
	OldUsername string
}

// findTerm returns the TermCfg matching name, or an error.
func findTerm(cfg LoanCfg, name string) (TermCfg, error) {
	for _, t := range cfg.Terms {
		if t.Name == name {
			return t, nil
		}
	}
	return TermCfg{}, errs.New(errs.InvalidLoanProduct)
}

// validPackage checks principal is in the allowed list.
func validPackage(cfg LoanCfg, principal int64) bool {
	for _, p := range cfg.Packages {
		if p == principal {
			return true
		}
	}
	return false
}

// Disburse creates a new active loan and credits the user. The caller must have
// no active loan already; the unique index one_active_loan enforces it at DB level
// but we also check in Go to return a typed error (T6).
func Disburse(ctx context.Context, tx pgx.Tx, userID int64, principal int64, term string, now time.Time, cfg LoanCfg) (Loan, error) {
	if !validPackage(cfg, principal) {
		return Loan{}, errs.New(errs.InvalidLoanProduct)
	}
	tc, err := findTerm(cfg, term)
	if err != nil {
		return Loan{}, err
	}

	var exists bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM loans WHERE user_id = $1 AND status = 'active')`, userID).Scan(&exists); err != nil {
		return Loan{}, err
	}
	if exists {
		return Loan{}, errs.New(errs.LoanActiveExists)
	}

	dueAt := now.Add(tc.Duration)
	var loanID int64
	if err := tx.QueryRow(ctx,
		`INSERT INTO loans (user_id, principal, term, rate_bps, due_at)
		 VALUES ($1, $2, $3, $4, $5) RETURNING loan_id`,
		userID, principal, tc.Name, tc.RateBps, dueAt).Scan(&loanID); err != nil {
		if isUniqueViolation(err) {
			return Loan{}, errs.New(errs.LoanActiveExists)
		}
		return Loan{}, err
	}

	key := fmt.Sprintf("loan:%d:disburse", loanID)
	if err := ledger.Transfer(ctx, tx,
		ledger.System, ledger.User(userID),
		principal, ledger.ReasonLoanDisburse, key); err != nil {
		return Loan{}, err
	}

	return Loan{
		LoanID: loanID, UserID: userID, Principal: principal,
		Term: tc.Name, RateBps: tc.RateBps, DueAt: dueAt, Status: "active",
	}, nil
}

// Active returns the caller's current active loan with live interest, or false.
func Active(ctx context.Context, tx pgx.Tx, userID int64, now time.Time, cfg LoanCfg) (ActiveLoan, bool, error) {
	var (
		l   Loan
		row struct {
			loanID    int64
			principal int64
			rateBps   int
			dueAt     time.Time
		}
	)
	err := tx.QueryRow(ctx,
		`SELECT loan_id, principal, rate_bps, due_at FROM loans
		 WHERE user_id = $1 AND status = 'active'`, userID).Scan(
		&row.loanID, &row.principal, &row.rateBps, &row.dueAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ActiveLoan{}, false, nil
	}
	if err != nil {
		return ActiveLoan{}, false, err
	}
	l.LoanID = row.loanID
	l.Principal = row.principal
	l.RateBps = row.rateBps
	l.DueAt = row.dueAt

	owed, periods := ledger.Owed(l.Principal, l.RateBps, l.DueAt, now, cfg.OverdueBps, cfg.CapMultiplier)
	seizeAt := ledger.SeizeAt(l.Principal, l.RateBps, l.DueAt, cfg.OverdueBps, cfg.CapMultiplier)

	return ActiveLoan{
		LoanID:         l.LoanID,
		Principal:      l.Principal,
		DueAt:          l.DueAt,
		OwendNow:       owed,
		Overdue:        now.After(l.DueAt),
		PeriodsOverdue: periods,
		SeizeAt:        seizeAt,
	}, true, nil
}

// Repay pays off the active loan and returns the amount paid.
func Repay(ctx context.Context, tx pgx.Tx, userID, loanID int64, now time.Time, cfg LoanCfg) (paid int64, err error) {
	var l Loan
	err = tx.QueryRow(ctx,
		`SELECT loan_id, principal, rate_bps, due_at, status FROM loans
		 WHERE loan_id = $1 AND user_id = $2`, loanID, userID).Scan(
		&l.LoanID, &l.Principal, &l.RateBps, &l.DueAt, &l.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, errs.New(errs.LoanNotFound)
	}
	if err != nil {
		return 0, err
	}
	if l.Status != "active" {
		return 0, errs.New(errs.LoanNotActive)
	}

	owed, _ := ledger.Owed(l.Principal, l.RateBps, l.DueAt, now, cfg.OverdueBps, cfg.CapMultiplier)
	key := fmt.Sprintf("loan:%d:repay", loanID)
	if err := ledger.Transfer(ctx, tx,
		ledger.User(userID), ledger.System,
		owed, ledger.ReasonLoanRepay, key); err != nil {
		return 0, err
	}

	if _, err := tx.Exec(ctx,
		`UPDATE loans SET status = 'repaid', owed_at_settle = $2, settled_at = $3
		 WHERE loan_id = $1`, loanID, owed, now); err != nil {
		return 0, err
	}
	return owed, nil
}

// SeizeDue finds every active loan at or past the cap (owed >= 2 * principal)
// and seizes each one. Used by the hourly job (03 §Siết nợ).
//
// The function opens its own series of single-loan transactions to avoid one
// failed seize blocking others; each seize is idempotent via its key.
func SeizeDue(ctx context.Context, tx pgx.Tx, now time.Time, cfg LoanCfg) ([]SeizedLoan, error) {
	rows, err := tx.Query(ctx,
		`SELECT loan_id, user_id, principal, rate_bps, due_at FROM loans
		 WHERE status = 'active'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type candidate struct {
		loanID    int64
		userID    int64
		principal int64
		rateBps   int
		dueAt     time.Time
	}
	var candidates []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.loanID, &c.userID, &c.principal, &c.rateBps, &c.dueAt); err != nil {
			return nil, err
		}
		owed, _ := ledger.Owed(c.principal, c.rateBps, c.dueAt, now, cfg.OverdueBps, cfg.CapMultiplier)
		if owed >= c.principal*cfg.CapMultiplier {
			candidates = append(candidates, c)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var seized []SeizedLoan
	for _, c := range candidates {
		owed, _ := ledger.Owed(c.principal, c.rateBps, c.dueAt, now, cfg.OverdueBps, cfg.CapMultiplier)
		key := fmt.Sprintf("loan:%d:seize", c.loanID)
		s, f, err := ledger.Seize(ctx, tx,
			ledger.User(c.userID), ledger.System,
			owed, ledger.ReasonSeize, key)
		if err != nil {
			return nil, err
		}

		if _, err := tx.Exec(ctx,
			`UPDATE loans SET status = 'seized', forgiven_at_seize = $2, settled_at = $3
			 WHERE loan_id = $1`, c.loanID, f, now); err != nil {
			return nil, err
		}

		detail, _ := json.Marshal(map[string]any{
			"loan_id": c.loanID, "user_id": c.userID, "seized": s, "forgiven": f,
		})
		if _, err := tx.Exec(ctx,
			`INSERT INTO admin_flags (kind, detail) VALUES ('LOAN_SEIZED', $1)`, detail); err != nil {
			return nil, err
		}

		seized = append(seized, SeizedLoan{LoanID: c.loanID, UserID: c.userID, Seized: s, Forgiven: f})
	}
	return seized, nil
}

// DeleteAccount carries out steps 1-6 of 03 §Xoá tài khoản inside the caller's
// transaction. It returns a DeleteAccountResult whose fields the caller (admin
// handler) must act on after committing.
//
// Steps implemented here:
//  1. Reject if user has a running match.
//  2. Seize any active loan (like the hourly job).
//  3. Transfer remaining balance to System with reason ACCOUNT_DELETE.
//  4. Anonymise the users row; do NOT delete it (FK safety).
//  5. Delete balance_snapshot rows.
//  6. Null out http_log.user_id.
//
// The caller must then:
//   - Delete the session key from Redis (result.SessionKey).
//   - Close any open WS connection for this user (result.WSUserID).
func DeleteAccount(ctx context.Context, tx pgx.Tx, userID int64, now time.Time, cfg LoanCfg) (DeleteAccountResult, error) {
	var inMatch bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT 1 FROM match_players mp
			JOIN matches m ON m.match_id = mp.match_id
			WHERE mp.user_id = $1 AND m.status = 'running'
		)`, userID).Scan(&inMatch); err != nil {
		return DeleteAccountResult{}, err
	}
	if inMatch {
		return DeleteAccountResult{}, errs.New(errs.UserInMatch)
	}

	var loanID int64
	var principal int64
	var rateBps int
	var dueAt time.Time
	err := tx.QueryRow(ctx,
		`SELECT loan_id, principal, rate_bps, due_at FROM loans
		 WHERE user_id = $1 AND status = 'active'`, userID).Scan(&loanID, &principal, &rateBps, &dueAt)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return DeleteAccountResult{}, err
	}
	if err == nil {
		owed, _ := ledger.Owed(principal, rateBps, dueAt, now, cfg.OverdueBps, cfg.CapMultiplier)
		key := fmt.Sprintf("loan:%d:seize", loanID)
		seized, forgiven, serr := ledger.Seize(ctx, tx,
			ledger.User(userID), ledger.System,
			owed, ledger.ReasonSeize, key)
		if serr != nil {
			return DeleteAccountResult{}, serr
		}
		if _, serr = tx.Exec(ctx,
			`UPDATE loans SET status = 'seized', forgiven_at_seize = $2, settled_at = $3
			 WHERE loan_id = $1`, loanID, forgiven, now); serr != nil {
			return DeleteAccountResult{}, serr
		}
		_ = seized
	}

	var balance int64
	if err := tx.QueryRow(ctx,
		`SELECT balance FROM users WHERE user_id = $1`, userID).Scan(&balance); err != nil {
		return DeleteAccountResult{}, err
	}
	if balance > 0 {
		key := fmt.Sprintf("delete:%d", userID)
		if terr := ledger.Transfer(ctx, tx,
			ledger.User(userID), ledger.System,
			balance, ledger.ReasonAccountDelete, key); terr != nil {
			return DeleteAccountResult{}, terr
		}
	}

	var oldUsername string
	if err := tx.QueryRow(ctx,
		`UPDATE users SET username = $2, password_hash = NULL, locale = NULL, deleted_at = $3
		 WHERE user_id = $1 RETURNING username`,
		userID, fmt.Sprintf("deleted_%d", userID), now).Scan(&oldUsername); err != nil {
		return DeleteAccountResult{}, err
	}

	if _, err := tx.Exec(ctx,
		`DELETE FROM balance_snapshot WHERE user_id = $1`, userID); err != nil {
		return DeleteAccountResult{}, err
	}

	if _, err := tx.Exec(ctx,
		`UPDATE http_log SET user_id = NULL WHERE user_id = $1`, userID); err != nil {
		return DeleteAccountResult{}, err
	}

	return DeleteAccountResult{
		OldUsername: oldUsername,
		WSUserID:    userID,
	}, nil
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	return containsCode(err, "23505")
}

func containsCode(err error, code string) bool {
	type coder interface{ Code() string }
	var c coder
	if errors.As(err, &c) {
		return c.Code() == code
	}
	return false
}
