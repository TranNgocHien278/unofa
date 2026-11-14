package web

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/unofa/unofa/internal/errs"
	"github.com/unofa/unofa/internal/ledger"
	"github.com/unofa/unofa/internal/loan"
	"github.com/unofa/unofa/internal/store"
)

// CurrentTermsVersion is bumped to force everyone to accept again on next login
// (01 §Nội dung pháp lý §Lưu trữ).
const CurrentTermsVersion = 1

var usernameRe = regexp.MustCompile(`^[a-z0-9_]{3,20}$`)

// samePasswordCompare is used so a missing user costs the same time as a wrong
// password: an attacker must not learn which usernames exist from latency.
var dummyHash = func() []byte {
	h, _ := bcrypt.GenerateFromPassword([]byte("not-a-real-password"), bcrypt.MinCost)
	return h
}()

// register handles POST /api/register (04 §Auth).
func (d Deps) register(w http.ResponseWriter, r *http.Request) error {
	ip := clientIP(r)
	if err := d.Limiter.Allow(r.Context(), "reg_ip", ip,
		d.Cfg.Rate.RegisterPerIP.Limit, d.Cfg.Rate.RegisterPerIP.Period); err != nil {
		return err
	}

	var body struct {
		Username      string `json:"username"`
		Password      string `json:"password"`
		TermsAccepted bool   `json:"terms_accepted"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		return err
	}
	if !body.TermsAccepted {
		return errs.New(errs.TermsNotAccepted)
	}
	if err := validatePassword(body.Password); err != nil {
		return err
	}
	name, err := store.NormalizeUsername(body.Username)
	if err != nil {
		return err
	}
	if !usernameRe.MatchString(name) {
		return errs.New(errs.UsernameInvalid)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(body.Password), 12)
	if err != nil {
		return errs.New(errs.Internal)
	}

	now := d.now()
	var user *store.User
	txErr := pgx.BeginFunc(r.Context(), d.Store.Pool, func(tx pgx.Tx) error {
		created, err := d.Store.InTx(tx).CreateUser(r.Context(), name, string(hash), CurrentTermsVersion, 0)
		if err != nil {
			return err
		}
		// Every U comes from System, so the books stay balanced (03 §Reason).
		if err := ledger.Transfer(r.Context(), tx, ledger.System, ledger.User(created.UserID),
			d.Cfg.Money.SignupBonus, ledger.ReasonSignupBonus,
			"signup:"+itoa(created.UserID)); err != nil {
			return err
		}
		user = created
		user.Balance = d.Cfg.Money.SignupBonus
		return nil
	})
	if txErr != nil {
		return txErr
	}

	if err := d.GameSess.CreateForUser(r.Context(), w, user.UserID); err != nil {
		return errs.New(errs.ServiceUnavailable)
	}
	writeJSON(w, http.StatusCreated, now, map[string]any{
		"user_id":  user.UserID,
		"username": user.Username,
		"balance":  user.Balance,
	})
	return nil
}

// login handles POST /api/login.
func (d Deps) login(w http.ResponseWriter, r *http.Request) error {
	ip := clientIP(r)
	if err := d.Limiter.Allow(r.Context(), "login_ip", ip,
		d.Cfg.Rate.LoginPerIP.Limit, d.Cfg.Rate.LoginPerIP.Period); err != nil {
		return err
	}

	var body struct {
		Username     string `json:"username"`
		Password     string `json:"password"`
		TermsVersion *int   `json:"terms_version"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		return err
	}
	name := strings.ToLower(strings.TrimSpace(body.Username))

	if err := d.Limiter.Allow(r.Context(), "login_user", name,
		d.Cfg.Rate.LoginPerUsername.Limit, d.Cfg.Rate.LoginPerUsername.Period); err != nil {
		// Too many attempts for this username: lock the account and flag it.
		until := d.now().Add(d.Cfg.Rate.LoginLockDuration)
		if u, ferr := d.Store.GetByUsername(r.Context(), name); ferr == nil {
			_ = d.Store.SetLoginLockedUntil(r.Context(), u.UserID, &until)
			_, _ = d.Store.InsertFlag(r.Context(), "LOGIN_LOCKED", mustJSON(map[string]any{
				"user_id": u.UserID, "username": name, "until": until,
			}))
		}
		return errs.NewRetry(errs.RateLimited, retrySeconds(d.Cfg.Rate.LoginPerUsername.Period))
	}

	user, err := d.Store.GetByUsername(r.Context(), name)
	if err != nil {
		// Same timing and same code as a wrong password.
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(body.Password))
		return errs.New(errs.InvalidCredential)
	}
	if user.PasswordHash == nil || user.DeletedAt != nil {
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(body.Password))
		return errs.New(errs.InvalidCredential)
	}
	if user.LoginLockedUntil != nil && d.now().Before(*user.LoginLockedUntil) {
		return errs.NewRetry(errs.LoginLocked, int(user.LoginLockedUntil.Sub(d.now()).Seconds())+1)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(*user.PasswordHash), []byte(body.Password)); err != nil {
		return errs.New(errs.InvalidCredential)
	}

	// A client that accepted an older terms version must tick again
	// (01 §Nội dung pháp lý §Lưu trữ).
	if body.TermsVersion != nil && *body.TermsVersion < CurrentTermsVersion {
		return errs.New(errs.TermsOutdated)
	}

	if err := d.GameSess.CreateForUser(r.Context(), w, user.UserID); err != nil {
		return errs.New(errs.ServiceUnavailable)
	}
	writeJSON(w, http.StatusOK, d.now(), map[string]any{
		"user_id":       user.UserID,
		"username":      user.Username,
		"balance":       user.Balance,
		"locale":        derefString(user.Locale, "en"),
		"terms_version": user.TermsVersion,
	})
	return nil
}

// logout handles POST /api/logout: drop the session and close its socket.
func (d Deps) logout(w http.ResponseWriter, r *http.Request) error {
	uid, err := d.userID(r)
	if err != nil {
		return err
	}
	if token, serr := d.GameSess.Get(r.Context(), r); serr == nil && d.Match != nil {
		d.Match.CloseSession(token, "LOGOUT")
	}
	d.GameSess.Delete(r.Context(), w, r)
	_ = uid
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// me handles GET /api/me.
func (d Deps) me(w http.ResponseWriter, r *http.Request) error {
	uid, err := d.userID(r)
	if err != nil {
		return err
	}
	user, err := d.Store.GetByID(r.Context(), uid)
	if err != nil {
		return err
	}

	var activeLoan any
	if al, ok, lerr := loanActive(r.Context(), d, uid); lerr == nil && ok {
		activeLoan = al
	}

	writeJSON(w, http.StatusOK, d.now(), map[string]any{
		"user_id":       user.UserID,
		"username":      user.Username,
		"balance":       user.Balance,
		"locale":        derefString(user.Locale, "en"),
		"terms_version": user.TermsVersion,
		"active_loan":   activeLoan,
	})
	return nil
}

// patchMe handles PATCH /api/me: locale, username and password changes (D5).
func (d Deps) patchMe(w http.ResponseWriter, r *http.Request) error {
	uid, err := d.userID(r)
	if err != nil {
		return err
	}

	var body struct {
		Locale          *string `json:"locale"`
		Username        *string `json:"username"`
		NewPassword     *string `json:"new_password"`
		CurrentPassword string  `json:"current_password"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		return err
	}

	user, err := d.Store.GetByID(r.Context(), uid)
	if err != nil {
		return err
	}

	// Locale is the only field that does not need the current password.
	if body.Locale != nil && body.Username == nil && body.NewPassword == nil {
		locale := *body.Locale
		if locale != "en" && locale != "vi" {
			return errs.New(errs.Internal)
		}
		if err := d.Store.UpdateLocale(r.Context(), uid, &locale); err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, d.now(), map[string]any{"locale": locale})
		return nil
	}

	// Every credential change re-proves the password (D5).
	if body.CurrentPassword == "" || user.PasswordHash == nil {
		return errs.New(errs.InvalidCredential)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(*user.PasswordHash), []byte(body.CurrentPassword)); err != nil {
		return errs.New(errs.InvalidCredential)
	}

	if body.Username != nil {
		name, err := store.NormalizeUsername(*body.Username)
		if err != nil {
			return err
		}
		if !usernameRe.MatchString(name) {
			return errs.New(errs.UsernameInvalid)
		}
		if err := d.Store.UpdateUsername(r.Context(), uid, name); err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, d.now(), map[string]any{"username": name})
		return nil
	}

	if body.NewPassword != nil {
		if err := validatePassword(*body.NewPassword); err != nil {
			return err
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(*body.NewPassword), 12)
		if err != nil {
			return errs.New(errs.Internal)
		}
		if err := d.Store.UpdatePasswordHash(r.Context(), uid, string(hash)); err != nil {
			return err
		}
		// Every session dies and a fresh one is issued; the old socket is closed.
		if token, serr := d.GameSess.Get(r.Context(), r); serr == nil {
			d.GameSess.DeleteByToken(r.Context(), token)
			if d.Match != nil {
				d.Match.CloseSession(token, "PASSWORD_CHANGED")
			}
		}
		if err := d.GameSess.CreateForUser(r.Context(), w, uid); err != nil {
			return errs.New(errs.ServiceUnavailable)
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	}
	return errs.New(errs.Internal)
}

// heartbeat handles POST /api/session/heartbeat. It extends the session TTL and
// keeps the WebSocket's session alive (D3: WS traffic never extends a session).
func (d Deps) heartbeat(w http.ResponseWriter, r *http.Request) error {
	token, err := d.GameSess.Get(r.Context(), r)
	if err != nil {
		return errs.New(errs.SessionExpired)
	}
	if err := d.Limiter.Allow(r.Context(), "hb", token,
		d.Cfg.Rate.HeartbeatPerSession.Limit, d.Cfg.Rate.HeartbeatPerSession.Period); err != nil {
		return err
	}
	if err := d.GameSess.Renew(r.Context(), r); err != nil {
		return errs.New(errs.SessionExpired)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// --- helpers ---

func validatePassword(pw string) error {
	// 8..72 bytes: bcrypt silently truncates past 72, so a longer password would
	// mean the tail never mattered (01 §Đăng ký step 2).
	n := len(pw)
	if n < 8 {
		return errs.New(errs.PasswordTooShort)
	}
	if n > 72 {
		return errs.New(errs.PasswordTooLong)
	}
	return nil
}

func clientIP(r *http.Request) string {
	return middlewareRealIP(r)
}

func derefString(p *string, def string) string {
	if p == nil || *p == "" {
		return def
	}
	return *p
}

func retrySeconds(window time.Duration) int {
	now := time.Now().UTC()
	return int(now.Truncate(window).Add(window).Sub(now).Seconds()) + 1
}

func (d Deps) now() time.Time {
	if d.Now != nil {
		return d.Now().UTC()
	}
	if d.Clock != nil {
		return d.Clock.Now().UTC()
	}
	return time.Now().UTC()
}

// loanActive is a small wrapper so /api/me and /api/loans/active share it.
func loanActive(ctx context.Context, d Deps, uid int64) (*loan.ActiveLoan, bool, error) {
	var out *loan.ActiveLoan
	var found bool
	err := pgx.BeginFunc(ctx, d.Store.Pool, func(tx pgx.Tx) error {
		al, ok, err := loan.Active(ctx, tx, uid, d.now(), d.loanCfg())
		if err != nil {
			return err
		}
		if ok {
			out = &al
			found = true
		}
		return nil
	})
	return out, found, err
}
