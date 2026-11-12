package web

import (
	"net/http"
	"time"

	"github.com/unofa/unofa/internal/clock"
	"github.com/unofa/unofa/internal/config"
	"github.com/unofa/unofa/internal/ratelimit"
	"github.com/unofa/unofa/internal/session"
	"github.com/unofa/unofa/internal/store"
)

// NewGameRouter builds the uno.example.com mux (04 §HTTP API).
// It owns no state beyond the shared Deps; the match hub is injected via Deps.Match.
func NewGameRouter(d Deps) http.Handler {
	mux := http.NewServeMux()

	// --- public + session ---
	mux.HandleFunc("POST /api/register", H(d, d.register))
	mux.HandleFunc("POST /api/login", H(d, d.login))
	mux.HandleFunc("POST /api/logout", H(d, d.logout))
	mux.HandleFunc("POST /api/session/heartbeat", H(d, d.heartbeat))
	mux.HandleFunc("GET /api/me", H(d, d.me))
	mux.HandleFunc("PATCH /api/me", H(d, d.patchMe))

	// --- money ---
	mux.HandleFunc("POST /api/transfers", H(d, d.transfer))
	mux.HandleFunc("GET /api/transactions", H(d, d.transactions))
	mux.HandleFunc("GET /api/me/balance", H(d, d.balanceSeries))
	mux.HandleFunc("GET /api/me/matches", H(d, d.myMatches))

	// --- loans ---
	mux.HandleFunc("GET /api/loans/products", H(d, d.loanProducts))
	mux.HandleFunc("POST /api/loans", H(d, d.borrow))
	mux.HandleFunc("GET /api/loans/active", H(d, d.activeLoan))
	mux.HandleFunc("POST /api/loans/{id}/repay", H(d, d.repay))

	// --- rooms ---
	mux.HandleFunc("POST /api/rooms", H(d, d.createRoom))
	mux.HandleFunc("POST /api/rooms/{code}/join", H(d, d.joinRoom))
	mux.HandleFunc("POST /api/rooms/{code}/leave", H(d, d.leaveRoom))
	mux.HandleFunc("POST /api/rooms/{code}/ready", H(d, d.setReady))
	mux.HandleFunc("POST /api/rooms/{code}/stake", H(d, d.setStake))
	mux.HandleFunc("POST /api/rooms/{code}/kick", H(d, d.kick))
	mux.HandleFunc("POST /api/rooms/{code}/play", H(d, d.play))
	mux.HandleFunc("GET /api/rooms/{code}", H(d, d.getRoom))

	// --- matches ---
	mux.HandleFunc("GET /api/matches/{id}/state", H(d, d.matchState))

	// --- ws (handled by the match hub; web only declares the route) ---
	if d.Match != nil {
		mux.HandleFunc("GET /api/ws", d.handleWS)
	}

	// --- healthz, no auth, no logging ---
	mux.HandleFunc("GET /healthz", d.healthz)

	return mux
}

// NewAdminRouter builds the admin.example.com mux (04 §Admin, 07 §Admin).
func NewAdminRouter(d Deps) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /api/admin/login", H(d, d.adminLogin))
	mux.HandleFunc("POST /api/admin/logout", H(d, d.adminLogout))
	mux.HandleFunc("POST /api/admin/session/heartbeat", H(d, d.adminHeartbeat))
	mux.HandleFunc("GET /api/admin/metrics/requests", H(d, d.adminMetricRequests))
	mux.HandleFunc("GET /api/admin/metrics/status-codes", H(d, d.adminMetricCodes))
	mux.HandleFunc("GET /api/admin/accounts", H(d, d.adminListAccounts))
	mux.HandleFunc("GET /api/admin/accounts/{id}", H(d, d.adminAccountDetail))
	mux.HandleFunc("POST /api/admin/accounts/{id}/balance", H(d, d.adminAdjustBalance))
	mux.HandleFunc("DELETE /api/admin/accounts/{id}", H(d, d.adminDeleteAccount))
	mux.HandleFunc("GET /api/admin/flags", H(d, d.adminListFlags))
	mux.HandleFunc("POST /api/admin/flags/{id}/clear", H(d, d.adminClearFlag))
	mux.HandleFunc("GET /api/admin/audit", H(d, d.adminAudit))

	mux.HandleFunc("GET /healthz", d.healthz)
	return mux
}

// healthz reports DB and Redis readiness (07 §/healthz).
func (d Deps) healthz(w http.ResponseWriter, r *http.Request) {
	status := "ok"
	db := "ok"
	redis := "ok"
	if err := d.Store.Ping(r.Context()); err != nil {
		status = "degraded"
		db = "down"
	}
	if err := d.RDB.Ping(r.Context()).Err(); err != nil {
		status = "degraded"
		redis = "down"
	}
	code := http.StatusOK
	if status != "ok" {
		code = http.StatusServiceUnavailable
	}
	writeRawJSON(w, code, map[string]any{
		"status":   status,
		"db":       db,
		"redis":    redis,
		"uptime_s": uptimeSeconds(),
	})
}

var startAt = time.Now().UTC()

func uptimeSeconds() int64 { return int64(time.Since(startAt).Seconds()) }

// BuildDeps wires the runtime dependencies the handlers share. cmd/unofa calls
// it once per origin so the two routers reuse the same store and Redis handle.
func BuildDeps(cfg *config.Config, st *store.Store, rdb interface{}, clk clock.Clock, match MatchService) Deps {
	// rdb is typed loosely to keep web from importing go-redis at the boundary;
	// the session manager and limiter take the concrete *redis.Client.
	d := Deps{
		Cfg:    cfg,
		Store:  st,
		Clock:  clk,
		Match:  match,
		Now:    func() time.Time { return clk.Now().UTC() },
		GameSess:  session.NewGame(nil, cfg.Redis.SessionTTL),
		AdminSess: session.NewAdmin(nil, cfg.Redis.SessionTTL),
		Limiter:   ratelimit.New(nil),
	}
	return d
}
