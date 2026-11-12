package web

import (
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/unofa/unofa/internal/errs"
	"github.com/unofa/unofa/internal/ledger"
	"github.com/unofa/unofa/internal/loan"
	"github.com/unofa/unofa/internal/store"
	"golang.org/x/crypto/bcrypt"
)

// adminLogin handles POST /api/admin/login (07 §Admin §Session admin).
func (d Deps) adminLogin(w http.ResponseWriter, r *http.Request) error {
	ip := clientIP(r)
	if err := d.Limiter.Allow(r.Context(), "admin_login_ip", ip,
		d.Cfg.Rate.AdminLoginPerIP.Limit, d.Cfg.Rate.AdminLoginPerIP.Period); err != nil {
		return err
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		return err
	}
	for _, admin := range d.Cfg.Admin {
		if admin.Username != body.Username {
			continue
		}
		if err := bcrypt.CompareHashAndPassword([]byte(admin.BcryptHash), []byte(body.Password)); err != nil {
			return errs.New(errs.InvalidCredential)
		}
		if _, err := d.AdminSess.Create(r.Context(), w, body.Username); err != nil {
			return errs.New(errs.ServiceUnavailable)
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	}
	_ = bcrypt.CompareHashAndPassword([]byte("$2a$12$placeholder"), []byte(body.Password))
	return errs.New(errs.InvalidCredential)
}

// adminLogout handles POST /api/admin/logout.
func (d Deps) adminLogout(w http.ResponseWriter, r *http.Request) error {
	d.AdminSess.Delete(r.Context(), w, r)
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// adminHeartbeat handles POST /api/admin/session/heartbeat.
func (d Deps) adminHeartbeat(w http.ResponseWriter, r *http.Request) error {
	if err := d.AdminSess.Renew(r.Context(), r); err != nil {
		return errs.New(errs.SessionExpired)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// adminMetricRequests handles GET /api/admin/metrics/requests.
func (d Deps) adminMetricRequests(w http.ResponseWriter, r *http.Request) error {
	if _, err := d.adminName(r); err != nil {
		return err
	}
	rng := r.URL.Query().Get("range")
	buckets, err := d.Store.MetricRequests(r.Context(), rng)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, d.now(), map[string]any{"buckets": buckets})
	return nil
}

// adminMetricCodes handles GET /api/admin/metrics/status-codes.
func (d Deps) adminMetricCodes(w http.ResponseWriter, r *http.Request) error {
	if _, err := d.adminName(r); err != nil {
		return err
	}
	rng := r.URL.Query().Get("range")
	buckets, err := d.Store.MetricStatusCodes(r.Context(), rng)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, d.now(), map[string]any{"buckets": buckets})
	return nil
}

// adminListAccounts handles GET /api/admin/accounts.
func (d Deps) adminListAccounts(w http.ResponseWriter, r *http.Request) error {
	if _, err := d.adminName(r); err != nil {
		return err
	}
	users, err := d.Store.ListAccounts(r.Context(),
		r.URL.Query().Get("q"),
		queryInt(r, "limit", 50, 100),
		queryInt64(r, "before"),
	)
	if err != nil {
		return err
	}
	out := make([]map[string]any, 0, len(users))
	for _, u := range users {
		out = append(out, userRow(u))
	}
	var next any
	if len(users) > 0 {
		next = users[len(users)-1].UserID
	}
	writeJSON(w, http.StatusOK, d.now(), map[string]any{"items": out, "next_cursor": next})
	return nil
}

// adminAccountDetail handles GET /api/admin/accounts/{id}.
func (d Deps) adminAccountDetail(w http.ResponseWriter, r *http.Request) error {
	if _, err := d.adminName(r); err != nil {
		return err
	}
	uid, err := parseID(pathValue(r, "id"))
	if err != nil {
		return errs.New(errs.UserNotFound)
	}
	user, err := d.Store.GetByID(r.Context(), uid)
	if err != nil {
		return err
	}
	txs, err := d.Store.ListTransactions(r.Context(), uid, store.TransferFilter{Limit: 50})
	if err != nil {
		return err
	}
	txRows := make([]map[string]any, 0, len(txs))
	for _, t := range txs {
		txRows = append(txRows, map[string]any{
			"transaction_id": t.ID,
			"reason":         t.Reason,
			"amount":         t.Amount,
			"direction":      t.Direction,
			"at":             t.At.UTC().Format(rfc3339),
		})
	}
	var activeLoanRow any
	if al, ok, lerr := loanActive(r.Context(), d, uid); lerr == nil && ok {
		activeLoanRow = map[string]any{
			"loan_id":   al.LoanID,
			"principal": al.Principal,
			"due_at":    al.DueAt.UTC().Format(rfc3339),
			"owed_now":  al.OwendNow,
			"overdue":   al.Overdue,
		}
	}
	writeJSON(w, http.StatusOK, d.now(), map[string]any{
		"user":         userRow(*user),
		"active_loan":  activeLoanRow,
		"transactions": txRows,
	})
	return nil
}

// adminAdjustBalance handles POST /api/admin/accounts/{id}/balance.
func (d Deps) adminAdjustBalance(w http.ResponseWriter, r *http.Request) error {
	adminUser, err := d.adminName(r)
	if err != nil {
		return err
	}
	uid, err := parseID(pathValue(r, "id"))
	if err != nil {
		return errs.New(errs.UserNotFound)
	}
	var body struct {
		Delta int64 `json:"delta"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		return err
	}
	if body.Delta == 0 {
		return errs.New(errs.Internal)
	}
	now := d.now()
	rid := requestID(r)
	var newBalance int64
	txErr := pgx.BeginFunc(r.Context(), d.Store.Pool, func(tx pgx.Tx) error {
		var reason string
		if body.Delta > 0 {
			reason = ledger.ReasonSystemAdd
			amt := body.Delta
			if err := ledger.Transfer(r.Context(), tx, ledger.System, ledger.User(uid), amt, reason, "admin:"+rid); err != nil {
				return err
			}
		} else {
			reason = ledger.ReasonSystemGet
			amt := -body.Delta
			if err := ledger.Transfer(r.Context(), tx, ledger.User(uid), ledger.System, amt, reason, "admin:"+rid); err != nil {
				return err
			}
		}
		st := d.Store.InTx(tx)
		var err error
		newBalance, err = st.BalanceOf(r.Context(), uid)
		if err != nil {
			return err
		}
		if err := st.InsertSnapshot(r.Context(), uid, newBalance, now); err != nil {
			return err
		}
		action := "SYSTEM_ADD"
		if body.Delta < 0 {
			action = "SYSTEM_GET"
		}
		amt := body.Delta
		if amt < 0 {
			amt = -amt
		}
		_, err = st.InsertAudit(r.Context(), store.AuditEntry{
			AdminUsername: adminUser,
			Action:        action,
			TargetUserID:  &uid,
			Amount:        &amt,
			At:            now,
			RequestID:     rid,
		})
		return err
	})
	if txErr != nil {
		return txErr
	}
	writeJSON(w, http.StatusOK, now, map[string]any{"balance": newBalance})
	return nil
}

// adminDeleteAccount handles DELETE /api/admin/accounts/{id}.
func (d Deps) adminDeleteAccount(w http.ResponseWriter, r *http.Request) error {
	adminUser, err := d.adminName(r)
	if err != nil {
		return err
	}
	uid, err := parseID(pathValue(r, "id"))
	if err != nil {
		return errs.New(errs.UserNotFound)
	}
	now := d.now()
	rid := requestID(r)
	var result loan.DeleteAccountResult
	txErr := pgx.BeginFunc(r.Context(), d.Store.Pool, func(tx pgx.Tx) error {
		res, err := loan.DeleteAccount(r.Context(), tx, uid, now, d.loanCfg())
		if err != nil {
			return err
		}
		result = res
		st := d.Store.InTx(tx)
		if err := st.DeletePersonalData(r.Context(), uid); err != nil {
			return err
		}
		_, err = st.InsertAudit(r.Context(), store.AuditEntry{
			AdminUsername: adminUser,
			Action:        "DELETE_USER",
			TargetUserID:  &uid,
			At:            now,
			RequestID:     rid,
		})
		return err
	})
	if txErr != nil {
		return txErr
	}
	if result.SessionKey != "" {
		d.GameSess.DeleteByToken(r.Context(), result.SessionKey)
	}
	if result.WSUserID != 0 && d.Match != nil {
		_ = d.Match.Forfeit(r.Context(), "", result.WSUserID)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// adminListFlags handles GET /api/admin/flags.
func (d Deps) adminListFlags(w http.ResponseWriter, r *http.Request) error {
	if _, err := d.adminName(r); err != nil {
		return err
	}
	flags, err := d.Store.ListOpenFlags(r.Context(), queryInt(r, "limit", 50, 100))
	if err != nil {
		return err
	}
	out := make([]map[string]any, 0, len(flags))
	for _, f := range flags {
		out = append(out, map[string]any{
			"id":     f.ID,
			"kind":   f.Kind,
			"detail": f.Detail,
			"at":     f.At.UTC().Format(rfc3339),
		})
	}
	writeJSON(w, http.StatusOK, d.now(), map[string]any{"items": out})
	return nil
}

// adminClearFlag handles POST /api/admin/flags/{id}/clear.
func (d Deps) adminClearFlag(w http.ResponseWriter, r *http.Request) error {
	adminUser, err := d.adminName(r)
	if err != nil {
		return err
	}
	fid, err := parseID(pathValue(r, "id"))
	if err != nil {
		return errs.New(errs.NotFound)
	}
	now := d.now()
	rid := requestID(r)
	txErr := pgx.BeginFunc(r.Context(), d.Store.Pool, func(tx pgx.Tx) error {
		if err := d.Store.InTx(tx).ClearFlag(r.Context(), fid, now); err != nil {
			return err
		}
		_, err = d.Store.InTx(tx).InsertAudit(r.Context(), store.AuditEntry{
			AdminUsername: adminUser,
			Action:        "CLEAR_FLAG",
			At:            now,
			RequestID:     rid,
		})
		return err
	})
	if txErr != nil {
		return txErr
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// adminAudit handles GET /api/admin/audit.
func (d Deps) adminAudit(w http.ResponseWriter, r *http.Request) error {
	if _, err := d.adminName(r); err != nil {
		return err
	}
	rows, err := d.Store.ListAudit(r.Context(),
		queryInt(r, "limit", 50, 100),
		queryInt64(r, "before"),
	)
	if err != nil {
		return err
	}
	out := make([]map[string]any, 0, len(rows))
	for _, a := range rows {
		row := map[string]any{
			"id":             a.ID,
			"admin_username": a.AdminUsername,
			"action":         a.Action,
			"at":             a.At.UTC().Format(rfc3339),
			"request_id":     a.RequestID,
		}
		if a.TargetUserID != nil {
			row["target_user_id"] = *a.TargetUserID
		}
		if a.Amount != nil {
			row["amount"] = *a.Amount
		}
		out = append(out, row)
	}
	var next any
	if len(rows) > 0 {
		next = rows[len(rows)-1].ID
	}
	writeJSON(w, http.StatusOK, d.now(), map[string]any{"items": out, "next_cursor": next})
	return nil
}

// --- helpers ---

func userRow(u store.User) map[string]any {
	row := map[string]any{
		"user_id":        u.UserID,
		"username":       u.Username,
		"balance":        u.Balance,
		"locale":         derefString(u.Locale, "en"),
		"terms_version":  u.TermsVersion,
		"created_at":     u.CreatedAt.UTC().Format(rfc3339),
	}
	if u.LoginLockedUntil != nil {
		row["login_locked_until"] = u.LoginLockedUntil.UTC().Format(rfc3339)
	}
	if u.DeletedAt != nil {
		row["deleted_at"] = u.DeletedAt.UTC().Format(rfc3339)
	}
	return row
}

func requestID(r *http.Request) string {
	if rid := r.Header.Get("X-Request-ID"); rid != "" {
		return rid
	}
	return strings.ReplaceAll(time.Now().UTC().Format("20060102150405.999999999"), ".", "")
}

var _ = time.Now
