// Package web is the HTTP surface for both origins
// (docs/spec/04-http-api.md, docs/spec/07-ops-admin.md).
//
// Two routers: NewGameRouter serves uno.example.com, NewAdminRouter serves
// admin.example.com. They share no cookie, no session and no rate-limit bucket.
package web

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/unofa/unofa/internal/clock"
	"github.com/unofa/unofa/internal/config"
	"github.com/unofa/unofa/internal/errs"
	"github.com/unofa/unofa/internal/loan"
	"github.com/unofa/unofa/internal/middleware"
	"github.com/unofa/unofa/internal/ratelimit"
	"github.com/unofa/unofa/internal/session"
	"github.com/unofa/unofa/internal/store"
)

// MatchService is the narrow seam between HTTP and the live match machinery.
// internal/web declares it; the match hub satisfies it. Keeping the interface here
// is what stops web from importing match.
type MatchService interface {
	// ServeWS upgrades a request to the player WebSocket bound to the game session.
	ServeWS(gameSess *session.Manager, allowedOrigin string) http.HandlerFunc
	// StartMatch is called after POST /api/rooms/{code}/play has locked the room and
	// created the matches row. It must return once the match goroutine is registered.
	StartMatch(ctx context.Context, matchID, roomID string) error
	// Forfeit removes a player for good (leave mid-match, admin delete).
	Forfeit(ctx context.Context, matchID string, userID int64) error
	// Snapshot returns the per-viewer state for reconnect / F5.
	Snapshot(matchID string, userID int64) (any, error)
	// State returns the raw view struct (internal/game.View) for GET /api/matches/{id}/state.
	State(matchID string, userID int64) (any, bool)
	// CloseSession closes the WebSocket bound to a session (logout, password change).
	CloseSession(sessionToken, reason string)
	// CloseUser closes all WebSockets for a given userID (account delete).
	CloseUser(userID int64, reason string)
}

// Deps is everything the handlers need.
type Deps struct {
	Cfg       *config.Config
	Store     *store.Store
	RDB       *redis.Client
	Clock     clock.Clock
	Match     MatchService
	GameSess  *session.Manager
	AdminSess *session.Manager
	Limiter   *ratelimit.Limiter
	Now       func() time.Time
}

// handler is a method that can fail with a coded error.
type handler func(w http.ResponseWriter, r *http.Request) error

// H adapts a coded handler into an http.HandlerFunc.
func H(d Deps, fn handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := fn(w, r); err != nil {
			middleware.WriteError(w, r, errs.As(err))
		}
	}
}

// --- request helpers ---

// decodeJSON reads a JSON body of at most 16 KB (07 §nginx client_max_body_size).
// An unknown field or a malformed body is a client error, not a 500.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return errs.New(errs.InvalidMove)
	}
	return nil
}

// writeJSON writes v with server_now attached (04 §Thời gian).
func writeJSON(w http.ResponseWriter, status int, now time.Time, v any) {
	middleware.WriteJSON(w, status, now, v)
}

// pathValue returns the named path segment.
func pathValue(r *http.Request, name string) string { return r.PathValue(name) }

// queryInt reads an integer query parameter with a default and a ceiling.
func queryInt(r *http.Request, name string, def, max int) int {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return def
	}
	if max > 0 && n > max {
		return max
	}
	return n
}

// queryInt64 reads a 64-bit integer query parameter; malformed means zero.
func queryInt64(r *http.Request, name string) int64 {
	n, err := strconv.ParseInt(r.URL.Query().Get(name), 10, 64)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// csv splits a comma list, dropping empties.
func csv(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// userID returns the authenticated player id or UNAUTHENTICATED.
func (d Deps) userID(r *http.Request) (int64, error) {
	id, err := d.GameSess.GetUserID(r.Context(), r)
	if err != nil {
		return 0, errs.New(errs.Unauthenticated)
	}
	return id, nil
}

// adminName returns the authenticated admin username or UNAUTHENTICATED.
func (d Deps) adminName(r *http.Request) (string, error) {
	name, err := d.AdminSess.Get(r.Context(), r)
	if err != nil || name == "" {
		return "", errs.New(errs.Unauthenticated)
	}
	return name, nil
}

// loanCfg converts the config into the ledger/loan parameter block.
func (d Deps) loanCfg() loan.LoanCfg {
	terms := make([]loan.TermCfg, 0, len(d.Cfg.Loan.Terms))
	for _, t := range d.Cfg.Loan.Terms {
		terms = append(terms, loan.TermCfg{Name: t.Name, Duration: t.Duration, RateBps: t.RateBps})
	}
	return loan.LoanCfg{
		Packages:      d.Cfg.Loan.Packages,
		Terms:         terms,
		OverdueBps:    d.Cfg.Loan.OverdueBps,
		CapMultiplier: d.Cfg.Loan.CapMultiplier,
	}
}
