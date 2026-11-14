package web

import (
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/unofa/unofa/internal/errs"
	"github.com/unofa/unofa/internal/ledger"
	"github.com/unofa/unofa/internal/loan"
	"github.com/unofa/unofa/internal/store"
)

// transfer handles POST /api/transfers (04 §Tiền).
func (d Deps) transfer(w http.ResponseWriter, r *http.Request) error {
	uid, err := d.userID(r)
	if err != nil {
		return err
	}
	if err := d.Limiter.Allow(r.Context(), "money", itoa(uid),
		d.Cfg.Rate.MoneyPerUser.Limit, d.Cfg.Rate.MoneyPerUser.Period); err != nil {
		return err
	}

	var body struct {
		ToUsername string `json:"to_username"`
		Amount     int64  `json:"amount"`
		ClientUUID string `json:"client_uuid"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		return err
	}
	if body.ClientUUID == "" {
		return errs.New(errs.InvalidRequest)
	}
	if body.Amount < d.Cfg.Money.TransferMin {
		return errs.New(errs.InsufficientBalance)
	}

	now := d.now()
	var balanceAfter int64
	txErr := pgx.BeginFunc(r.Context(), d.Store.Pool, func(tx pgx.Tx) error {
		to, err := d.Store.InTx(tx).GetByUsername(r.Context(), body.ToUsername)
		if err != nil {
			return errs.New(errs.UserNotFound)
		}
		if to.UserID == uid {
			return errs.New(errs.SelfTransfer)
		}
		if err := ledger.Transfer(r.Context(), tx, ledger.User(uid), ledger.User(to.UserID),
			body.Amount, ledger.ReasonTransfer, "transfer:"+body.ClientUUID); err != nil {
			return err
		}
		balanceAfter, err = d.Store.InTx(tx).BalanceOf(r.Context(), uid)
		return err
	})
	if txErr != nil {
		return txErr
	}

	writeJSON(w, http.StatusCreated, now, map[string]any{
		"amount":        body.Amount,
		"to_username":   body.ToUsername,
		"balance_after": balanceAfter,
	})
	return nil
}

// transactions handles GET /api/transactions.
func (d Deps) transactions(w http.ResponseWriter, r *http.Request) error {
	uid, err := d.userID(r)
	if err != nil {
		return err
	}
	f := store.TransferFilter{
		Reasons:   csv(r.URL.Query().Get("reason")),
		Direction: r.URL.Query().Get("direction"),
		Limit:     queryInt(r, "limit", 50, 100),
		Before:    queryInt64(r, "before"),
	}
	if f.Direction != "" && f.Direction != "in" && f.Direction != "out" {
		f.Direction = ""
	}
	items, err := d.Store.ListTransactions(r.Context(), uid, f)
	if err != nil {
		return err
	}
	out := make([]map[string]any, 0, len(items))
	for _, t := range items {
		out = append(out, map[string]any{
			"transaction_id": t.ID,
			"reason":         t.Reason,
			"amount":         t.Amount,
			"direction":      t.Direction,
			"counterparty":   t.Counterparty,
			"at":             t.At.UTC().Format(rfc3339),
		})
	}
	var next any
	if len(items) == f.Limit && len(items) > 0 {
		next = items[len(items)-1].ID
	}
	writeJSON(w, http.StatusOK, d.now(), map[string]any{"items": out, "next_cursor": next})
	return nil
}

// balanceSeries handles GET /api/me/balance-chart (chart data, 06 §Chart).
func (d Deps) balanceSeries(w http.ResponseWriter, r *http.Request) error {
	uid, err := d.userID(r)
	if err != nil {
		return err
	}
	points, err := d.Store.SeriesForUser(r.Context(), uid, r.URL.Query().Get("range"))
	if err != nil {
		return err
	}
	out := make([]map[string]any, 0, len(points))
	for _, p := range points {
		out = append(out, map[string]any{
			"at":      p.At.UTC().Format(rfc3339),
			"balance": p.Balance,
		})
	}
	writeJSON(w, http.StatusOK, d.now(), map[string]any{"points": out})
	return nil
}

// myMatches handles GET /api/me/matches (D15).
func (d Deps) myMatches(w http.ResponseWriter, r *http.Request) error {
	uid, err := d.userID(r)
	if err != nil {
		return err
	}
	limit := queryInt(r, "limit", 50, 100)
	before := parseCursorTime(r.URL.Query().Get("before"))
	items, err := d.Store.ListMatchesForUser(r.Context(), uid, limit, before)
	if err != nil {
		return err
	}
	out := make([]map[string]any, 0, len(items))
	for _, m := range items {
		row := map[string]any{
			"match_id":   m.MatchID,
			"stake":      m.Stake,
			"pot":        m.Pot,
			"status":     m.Status,
			"started_at": m.StartedAt.UTC().Format(rfc3339),
		}
		if m.WinnerID != nil {
			row["winner_id"] = *m.WinnerID
		}
		if m.EndedAt != nil {
			row["ended_at"] = m.EndedAt.UTC().Format(rfc3339)
		}
		out = append(out, row)
	}
	var next any
	if len(items) == limit && len(items) > 0 {
		next = items[len(items)-1].StartedAt.UTC().Format(rfc3339)
	}
	writeJSON(w, http.StatusOK, d.now(), map[string]any{"items": out, "next_cursor": next})
	return nil
}

// loanProducts handles GET /api/loans/products (D6: rates come from config).
func (d Deps) loanProducts(w http.ResponseWriter, r *http.Request) error {
	if _, err := d.userID(r); err != nil {
		return err
	}
	terms := make([]map[string]any, 0, len(d.Cfg.Loan.Terms))
	for _, t := range d.Cfg.Loan.Terms {
		terms = append(terms, map[string]any{"term": t.Name, "rate_bps": t.RateBps})
	}
	writeJSON(w, http.StatusOK, d.now(), map[string]any{
		"packages":       d.Cfg.Loan.Packages,
		"terms":          terms,
		"overdue_bps":    d.Cfg.Loan.OverdueBps,
		"cap_multiplier": d.Cfg.Loan.CapMultiplier,
	})
	return nil
}

// borrow handles POST /api/loans.
func (d Deps) borrow(w http.ResponseWriter, r *http.Request) error {
	uid, err := d.userID(r)
	if err != nil {
		return err
	}
	if err := d.Limiter.Allow(r.Context(), "money", itoa(uid),
		d.Cfg.Rate.MoneyPerUser.Limit, d.Cfg.Rate.MoneyPerUser.Period); err != nil {
		return err
	}
	var body struct {
		Principal int64  `json:"principal"`
		Term      string `json:"term"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		return err
	}
	if !d.Cfg.PackageAllowed(body.Principal) {
		return errs.New(errs.InvalidLoanProduct)
	}
	if _, ok := d.Cfg.LoanTermByName(body.Term); !ok {
		return errs.New(errs.InvalidLoanProduct)
	}

	now := d.now()
	cfg := d.loanCfg()
	var taken loan.Loan
	var owedNow int64
	var balanceAfter int64
	txErr := pgx.BeginFunc(r.Context(), d.Store.Pool, func(tx pgx.Tx) error {
		l, err := loan.Disburse(r.Context(), tx, uid, body.Principal, body.Term, now, cfg)
		if err != nil {
			return err
		}
		taken = l
		balanceAfter, err = d.Store.InTx(tx).BalanceOf(r.Context(), uid)
		if err != nil {
			return err
		}
		owedNow, _ = ledger.Owed(l.Principal, l.RateBps, l.DueAt, now, cfg.OverdueBps, cfg.CapMultiplier)
		return nil
	})
	if txErr != nil {
		return txErr
	}

	writeJSON(w, http.StatusCreated, now, map[string]any{
		"loan_id":       taken.LoanID,
		"principal":     taken.Principal,
		"term":          taken.Term,
		"due_at":        taken.DueAt.UTC().Format(rfc3339),
		"owed_now":      owedNow,
		"balance_after": balanceAfter,
	})
	return nil
}

// activeLoan handles GET /api/loans/active.
func (d Deps) activeLoan(w http.ResponseWriter, r *http.Request) error {
	uid, err := d.userID(r)
	if err != nil {
		return err
	}
	al, ok, err := loanActive(r.Context(), d, uid)
	if err != nil {
		return err
	}
	if !ok {
		writeJSON(w, http.StatusOK, d.now(), nil)
		return nil
	}
	writeJSON(w, http.StatusOK, d.now(), map[string]any{
		"loan_id":         al.LoanID,
		"principal":       al.Principal,
		"due_at":          al.DueAt.UTC().Format(rfc3339),
		"owed_now":        al.OwendNow,
		"overdue":         al.Overdue,
		"periods_overdue": al.PeriodsOverdue,
		"seize_at":        al.SeizeAt.UTC().Format(rfc3339),
	})
	return nil
}

// repay handles POST /api/loans/{id}/repay.
func (d Deps) repay(w http.ResponseWriter, r *http.Request) error {
	uid, err := d.userID(r)
	if err != nil {
		return err
	}
	loanID, perr := parseID(pathValue(r, "id"))
	if perr != nil {
		return errs.New(errs.LoanNotFound)
	}
	now := d.now()
	var paid, balanceAfter int64
	txErr := pgx.BeginFunc(r.Context(), d.Store.Pool, func(tx pgx.Tx) error {
		p, err := loan.Repay(r.Context(), tx, uid, loanID, now, d.loanCfg())
		if err != nil {
			return err
		}
		paid = p
		balanceAfter, err = d.Store.InTx(tx).BalanceOf(r.Context(), uid)
		return err
	})
	if txErr != nil {
		return txErr
	}
	writeJSON(w, http.StatusOK, now, map[string]any{
		"owed_paid":     paid,
		"balance_after": balanceAfter,
	})
	return nil
}

var _ = fmt.Sprintf
