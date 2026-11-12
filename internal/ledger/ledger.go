// Package ledger owns every movement of U: the two money functions Transfer and
// Seize, the loan interest formula, and the books-balance invariant
// (docs/spec/03-data-model.md §Tiền, §Loan, §Invariant lệch sổ).
//
// Every function here runs inside a caller-provided pgx.Tx and never opens its own;
// the caller's transaction is what makes a bet/payout/repay atomic.
package ledger

import (
	"context"
	"errors"
	"sort"

	"github.com/jackc/pgx/v5"

	"github.com/unofa/unofa/internal/errs"
)

// Reasons accepted by transactions.reason (03 §Reason). The DB CHECK is the
// second line of defense; these constants keep callers from inventing strings.
const (
	ReasonSignupBonus   = "SIGNUP_BONUS"
	ReasonBet           = "BET"
	ReasonPayout        = "PAYOUT"
	ReasonBurn          = "BURN"
	ReasonTransfer      = "TRANSFER"
	ReasonLoanDisburse  = "LOAN_DISBURSE"
	ReasonLoanRepay     = "LOAN_REPAY"
	ReasonSeize         = "SEIZE"
	ReasonSystemAdd     = "SYSTEM_ADD"
	ReasonSystemGet     = "SYSTEM_GET"
	ReasonAccountDelete = "ACCOUNT_DELETE"
)

var reasons = map[string]bool{
	ReasonSignupBonus: true, ReasonBet: true, ReasonPayout: true, ReasonBurn: true,
	ReasonTransfer: true, ReasonLoanDisburse: true, ReasonLoanRepay: true,
	ReasonSeize: true, ReasonSystemAdd: true, ReasonSystemGet: true,
	ReasonAccountDelete: true,
}

// Acct is one side of a movement: a users row or a matches.pot (03 §Hai hàm chuyển tiền).
type Acct struct {
	UserID  *int64
	MatchID *string
}

// User returns the account of a users row. User(0) is System.
func User(id int64) Acct { return Acct{UserID: &id} }

// Match returns the pot account of a match.
func Match(id string) Acct { return Acct{MatchID: &id} }

// System is the only account allowed to go negative: its depth equals the U in circulation.
var System = User(0)

func (a Acct) valid() error {
	if (a.UserID == nil) == (a.MatchID == nil) {
		return errs.New(errs.Internal)
	}
	if a.MatchID != nil && *a.MatchID == "" {
		return errs.New(errs.Internal)
	}
	return nil
}

func (a Acct) equal(b Acct) bool {
	if a.UserID != nil && b.UserID != nil {
		return *a.UserID == *b.UserID
	}
	if a.MatchID != nil && b.MatchID != nil {
		return *a.MatchID == *b.MatchID
	}
	return false
}

// checkArgs rejects programming errors before any row is locked. The closed code
// set has no "bad amount" code, so a request the HTTP layer should already have
// refused surfaces as INTERNAL rather than silently moving 0 or negative U.
func checkArgs(from, to Acct, amount int64, reason, key string) error {
	if key == "" || !reasons[reason] || amount < 1 {
		return errs.New(errs.Internal)
	}
	if err := from.valid(); err != nil {
		return err
	}
	if err := to.valid(); err != nil {
		return err
	}
	if from.equal(to) {
		if reason == ReasonTransfer {
			return errs.New(errs.SelfTransfer)
		}
		return errs.New(errs.Internal)
	}
	return nil
}

// Transfer moves amount from one account to another and fails the whole
// transaction when the sender cannot cover it. It is the only strict mover.
//
// Player-to-player transfers (reason TRANSFER) additionally enforce the sliding
// 24h limit and refuse while the sender has an overdue loan, both inside the same
// transaction and after the sender row is locked (03 §Chuyển khoản giữa người chơi).
func Transfer(ctx context.Context, tx pgx.Tx, from, to Acct, amount int64, reason, key string) error {
	if err := checkArgs(from, to, amount, reason, key); err != nil {
		return err
	}
	fromBal, _, err := lockAccts(ctx, tx, from, to)
	if err != nil {
		return err
	}
	if from.UserID == nil || *from.UserID != 0 {
		if amount > fromBal {
			return errs.New(errs.InsufficientBalance)
		}
	}
	if reason == ReasonTransfer {
		if err := checkTransferRules(ctx, tx, *from.UserID, amount); err != nil {
			return err
		}
	}
	replayed, err := claimKey(ctx, tx, from, to, amount, reason, key)
	if err != nil || replayed {
		return err
	}
	if _, err := moveAcct(ctx, tx, from, -amount); err != nil {
		return err
	}
	if _, err := moveAcct(ctx, tx, to, amount); err != nil {
		return err
	}
	return nil
}

// Seize takes min(owed, balance) from one account and returns how much was taken
// plus how much was forgiven. The floor is 0U: a balance of 0 writes no
// transactions row at all (amount >= 1), and the caller still closes the loan.
//
// Replaying the key returns the previous seizure instead of taking money twice.
func Seize(ctx context.Context, tx pgx.Tx, from, to Acct, owed int64, reason, key string) (seized, forgiven int64, err error) {
	if owed < 1 {
		return 0, 0, nil
	}
	if err := checkArgs(from, to, owed, reason, key); err != nil {
		return 0, 0, err
	}
	fromBal, _, err := lockAccts(ctx, tx, from, to)
	if err != nil {
		return 0, 0, err
	}
	seized = owed
	if from.UserID == nil || *from.UserID != 0 {
		if fromBal < seized {
			seized = fromBal
		}
	}
	if seized < 1 {
		return 0, owed, nil
	}
	replayed, err := claimKey(ctx, tx, from, to, seized, reason, key)
	if err != nil {
		return 0, 0, err
	}
	if replayed {
		return seized, owed - seized, nil
	}
	if _, err := moveAcct(ctx, tx, from, -seized); err != nil {
		return 0, 0, err
	}
	if _, err := moveAcct(ctx, tx, to, seized); err != nil {
		return 0, 0, err
	}
	return seized, owed - seized, nil
}

// lockAccts locks both accounts in the fixed order of 03 §Hai hàm chuyển tiền:
// users ascending by user_id, then matches. Every money path obeys this order, which
// is what keeps two opposite transfers from deadlocking (T13).
//
// The locks are taken one statement at a time, in order, so Postgres never gets to
// reorder them the way a multi-row FOR UPDATE with a plan could.
func lockAccts(ctx context.Context, tx pgx.Tx, from, to Acct) (fromBal, toBal int64, err error) {
	ids := make([]int64, 0, 2)
	if from.UserID != nil {
		ids = append(ids, *from.UserID)
	}
	if to.UserID != nil {
		ids = append(ids, *to.UserID)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	bal := make(map[int64]int64, len(ids))
	for _, id := range ids {
		if _, seen := bal[id]; seen {
			continue
		}
		var b int64
		if err := tx.QueryRow(ctx, `SELECT balance FROM users WHERE user_id = $1 FOR UPDATE`, id).Scan(&b); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return 0, 0, errs.New(errs.UserNotFound)
			}
			return 0, 0, err
		}
		bal[id] = b
	}
	if from.UserID != nil {
		fromBal = bal[*from.UserID]
	} else {
		fromBal, err = lockMatch(ctx, tx, *from.MatchID)
		if err != nil {
			return 0, 0, err
		}
	}
	if to.UserID != nil {
		toBal = bal[*to.UserID]
	} else {
		toBal, err = lockMatch(ctx, tx, *to.MatchID)
		if err != nil {
			return 0, 0, err
		}
	}
	return fromBal, toBal, nil
}

func lockMatch(ctx context.Context, tx pgx.Tx, id string) (int64, error) {
	var pot int64
	if err := tx.QueryRow(ctx, `SELECT pot FROM matches WHERE match_id = $1 FOR UPDATE`, id).Scan(&pot); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, errs.New(errs.NotFound)
		}
		return 0, err
	}
	return pot, nil
}

// moveAcct applies a signed delta and snapshots the new balance of every users side
// (including System), so the balance chart never has to replay the transactions table.
func moveAcct(ctx context.Context, tx pgx.Tx, a Acct, delta int64) (int64, error) {
	if a.UserID != nil {
		var bal int64
		if err := tx.QueryRow(ctx,
			`UPDATE users SET balance = balance + $2 WHERE user_id = $1 RETURNING balance`,
			*a.UserID, delta).Scan(&bal); err != nil {
			return 0, err
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO balance_snapshot (user_id, balance) VALUES ($1, $2)`, *a.UserID, bal); err != nil {
			return 0, err
		}
		return bal, nil
	}
	var pot int64
	if err := tx.QueryRow(ctx,
		`UPDATE matches SET pot = pot + $2 WHERE match_id = $1 RETURNING pot`,
		*a.MatchID, delta).Scan(&pot); err != nil {
		return 0, err
	}
	return pot, nil
}

const insertTxSQL = `INSERT INTO transactions
    (from_user_id, from_match_id, to_user_id, to_match_id, amount, reason, idempotency_key)
    VALUES ($1, $2, $3, $4, $5, $6, $7)
    ON CONFLICT (idempotency_key) DO NOTHING`

// claimKey writes the ledger row for this movement before any balance changes, so
// UNIQUE(idempotency_key) is the single point that decides "first time or replay".
// A replay is a no-op that reports success with the previous result.
func claimKey(ctx context.Context, tx pgx.Tx, from, to Acct, amount int64, reason, key string) (bool, error) {
	tag, err := tx.Exec(ctx, insertTxSQL, from.UserID, from.MatchID, to.UserID, to.MatchID, amount, reason, key)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 1 {
		return false, nil
	}
	return true, assertSameRow(ctx, tx, key, from, to, amount, reason)
}

// assertSameRow refuses a key reused for a different movement: that is a caller bug,
// and replaying it as if it were the first request would hide lost money.
func assertSameRow(ctx context.Context, tx pgx.Tx, key string, from, to Acct, amount int64, reason string) error {
	var (
		fu, tu *int64
		fm, tm *string
		amt    int64
		rsn    string
	)
	err := tx.QueryRow(ctx, `SELECT from_user_id, from_match_id, to_user_id, to_match_id, amount, reason
		FROM transactions WHERE idempotency_key = $1`, key).Scan(&fu, &fm, &tu, &tm, &amt, &rsn)
	if err != nil {
		return err
	}
	if amt != amount || rsn != reason || !samePtr(fu, from.UserID) || !samePtr(fm, from.MatchID) ||
		!samePtr(tu, to.UserID) || !samePtr(tm, to.MatchID) {
		return errs.New(errs.Internal)
	}
	return nil
}

func samePtr[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// checkTransferRules enforces the two player-transfer gates of 03 §Chuyển khoản giữa
// người chơi, both measured inside the caller's transaction after the sender is locked.
func checkTransferRules(ctx context.Context, tx pgx.Tx, sender int64, amount int64) error {
	var overdue bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM loans WHERE user_id = $1 AND status = 'active' AND now() > due_at)`,
		sender).Scan(&overdue); err != nil {
		return err
	}
	if overdue {
		return errs.New(errs.LoanOverdue)
	}
	var sent int64
	if err := tx.QueryRow(ctx,
		`SELECT COALESCE(SUM(amount), 0) FROM transactions
		 WHERE reason = 'TRANSFER' AND from_user_id = $1
		   AND created_at > now() - interval '24 hours'`, sender).Scan(&sent); err != nil {
		return err
	}
	if sent+amount > transferDailyLimit {
		return errs.New(errs.TransferLimit)
	}
	return nil
}

// transferDailyLimit is the per-sender sliding window budget. The spec puts the
// number in config; Transfer's signature (docs/CONTRACTS.md) has no config argument,
// so cmd/unofa hands it over once at startup with SetTransferDailyLimit.
var transferDailyLimit int64 = 1000

// SetTransferDailyLimit overrides the spec default of 1000U/24h. Call once at startup,
// before serving traffic; it is not safe to change while requests are in flight.
func SetTransferDailyLimit(limit int64) {
	if limit > 0 {
		transferDailyLimit = limit
	}
}
