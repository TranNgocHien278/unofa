package web_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"

	"github.com/unofa/unofa/internal/clock"
	"github.com/unofa/unofa/internal/config"
	"github.com/unofa/unofa/internal/middleware"
	"github.com/unofa/unofa/internal/session"
	"github.com/unofa/unofa/internal/store"
	"github.com/unofa/unofa/internal/web"
)

const (
	testRedisAddr = "127.0.0.1:56379"
	testRedisDB   = 1
	testPassword  = "hunter2hunter2"
	testAdminUser = "root"
	testAdminPass = "adminpass"
)

// matchStub implements web.MatchService with recorded calls. No match goroutine runs.
type matchStub struct {
	mu        sync.Mutex
	starts    []startCall
	closes    []string
	stateOK   bool
	stateView map[string]any
	stateUID  int64
}

type startCall struct{ matchID, roomID string }

func (m *matchStub) ServeWS(*session.Manager, string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotImplemented) }
}

func (m *matchStub) StartMatch(_ context.Context, matchID, roomID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.starts = append(m.starts, startCall{matchID: matchID, roomID: roomID})
	return nil
}

func (m *matchStub) Forfeit(context.Context, string, int64) error { return nil }

func (m *matchStub) Snapshot(string, int64) (any, error) { return nil, nil }

func (m *matchStub) State(_ string, userID int64) (any, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stateUID = userID
	if !m.stateOK {
		return nil, false
	}
	return m.stateView, true
}

func (m *matchStub) CloseSession(_, reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closes = append(m.closes, reason)
}

func (m *matchStub) CloseUser(_ int64, reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closes = append(m.closes, reason)
}

func (m *matchStub) closeReasons() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.closes...)
}

func (m *matchStub) startCalls() []startCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]startCall(nil), m.starts...)
}

// env is one test's world: real store, real Redis DB 1, both routers over httptest.
type env struct {
	t        *testing.T
	st       *store.Store
	rdb      *redis.Client
	stub     *matchStub
	cfg      *config.Config
	deps     web.Deps
	clk      *clock.Fake
	gameSrv  *httptest.Server
	adminSrv *httptest.Server
}

// newEnv skips when UNOFA_TEST_DSN is unset (same pattern as internal/store).
// mutate lets a test change the config before the routers are built.
func newEnv(t *testing.T, mutate ...func(*config.Config)) *env {
	t.Helper()
	dsn := os.Getenv("UNOFA_TEST_DSN")
	if dsn == "" {
		t.Skip("UNOFA_TEST_DSN unset")
	}
	ctx := context.Background()
	st, err := store.New(ctx, dsn, 4, 1, 5*time.Second)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(st.Close)

	rdb := redis.NewClient(&redis.Options{Addr: testRedisAddr, DB: testRedisDB})
	t.Cleanup(func() { _ = rdb.Close() })
	if err := rdb.FlushDB(ctx).Err(); err != nil {
		t.Fatalf("redis FlushDB: %v", err)
	}
	resetDB(t, st)

	hash, err := bcrypt.GenerateFromPassword([]byte(testAdminPass), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("bcrypt: %v", err)
	}
	cfg := baseConfig(string(hash))
	for _, m := range mutate {
		m(cfg)
	}

	clk := &clock.Fake{T: time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC)}
	stub := &matchStub{}
	deps := web.BuildDeps(cfg, st, rdb, clk, stub)

	e := &env{t: t, st: st, rdb: rdb, stub: stub, cfg: cfg, deps: deps, clk: clk}
	e.gameSrv = httptest.NewServer(web.NewGameRouter(deps))
	e.adminSrv = httptest.NewServer(web.NewAdminRouter(deps))
	t.Cleanup(e.gameSrv.Close)
	t.Cleanup(e.adminSrv.Close)
	return e
}

// resetDB truncates every table this suite touches and re-seeds the System row.
func resetDB(t *testing.T, st *store.Store) {
	t.Helper()
	ctx := context.Background()
	if _, err := st.Pool.Exec(ctx, `TRUNCATE admin_flags, admin_audit, balance_snapshot, transactions, loans, match_players, matches, room_members, rooms, users, http_log RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	if _, err := st.Pool.Exec(ctx, `INSERT INTO users (user_id, username, password_hash, balance) VALUES (0, 'System', NULL, 0)`); err != nil {
		t.Fatalf("seed System: %v", err)
	}
}

func baseConfig(adminHash string) *config.Config {
	w := config.Window{Limit: 100000, Period: time.Hour}
	return &config.Config{
		Redis: config.Redis{Addr: testRedisAddr, DB: testRedisDB, SessionTTL: 15 * time.Minute, HeartbeatInterval: time.Minute},
		Admin: []config.Admin{{Username: testAdminUser, BcryptHash: adminHash}},
		Money: config.Money{SignupBonus: 200, TransferDailyLimit: 100000, TransferMin: 1},
		Loan: config.Loan{
			Packages: []int64{500, 1000, 2000},
			Terms: []config.LoanTerm{
				{Name: "24h", Duration: 24 * time.Hour, RateBps: 100},
				{Name: "3d", Duration: 72 * time.Hour, RateBps: 300},
				{Name: "30d", Duration: 720 * time.Hour, RateBps: 1000},
			},
			OverdueBps:    500,
			CapMultiplier: 3,
		},
		Game: config.GameCfg{
			MinPlayers:    2,
			MaxPlayers:    10,
			HandSize:      7,
			AllowedStakes: []int64{10, 25, 50, 100, 500},
		},
		Rate: config.Rate{
			LoginPerIP:          w,
			LoginPerUsername:    w,
			LoginLockDuration:   time.Minute,
			RegisterPerIP:       w,
			HeartbeatPerSession: w,
			WSActionsPerUser:    w,
			WSUnoPerUser:        w,
			MatchStatePerUser:   w,
			MoneyPerUser:        w,
			CredentialPerUser:   w,
			ReadPerUser:         w,
			RoomCreatePerUser:   w,
			RoomJoinPerUser:     w,
			AdminLoginPerIP:     w,
			AdminPerAdmin:       w,
			WSMessagesPerConn:   w,
		},
	}
}

// res is one HTTP response, read fully.
type res struct {
	status  int
	body    []byte
	header  http.Header
	cookies []*http.Cookie
}

func (r res) json(t *testing.T) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(r.body, &m); err != nil {
		t.Fatalf("decode body %q: %v", r.body, err)
	}
	return m
}

func (r res) errCode(t *testing.T) string {
	t.Helper()
	s, _ := r.json(t)["error"].(string)
	return s
}

// cookie returns "name=value" for the named Set-Cookie, or "" when absent or cleared.
func (r res) cookie(name string) string {
	for _, c := range r.cookies {
		if c.Name == name && c.Value != "" {
			return name + "=" + c.Value
		}
	}
	return ""
}

// num reads a JSON number field as int64.
func num(t *testing.T, m map[string]any, key string) int64 {
	t.Helper()
	f, ok := m[key].(float64)
	if !ok {
		t.Fatalf("field %q missing or not a number: %v", key, m[key])
	}
	return int64(f)
}

// send issues one request with no cookie jar: the Cookie header is set by hand
// because __Host- cookies are Secure and the httptest server is plain HTTP.
func send(t *testing.T, method, url, cookie string, body any) res {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, url, rdr)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return res{status: resp.StatusCode, body: raw, header: resp.Header, cookies: resp.Cookies()}
}

// api calls the game origin; adminAPI calls the admin origin.
func (e *env) api(method, path, cookie string, body any) res {
	return send(e.t, method, e.gameSrv.URL+path, cookie, body)
}

func (e *env) adminAPI(method, path, cookie string, body any) res {
	return send(e.t, method, e.adminSrv.URL+path, cookie, body)
}

// register creates a user through the real endpoint and returns its session cookie and id.
func (e *env) register(name string) (string, int64) {
	e.t.Helper()
	r := e.api(http.MethodPost, "/api/register", "", map[string]any{
		"username": name, "password": testPassword, "terms_accepted": true,
	})
	if r.status != http.StatusCreated {
		e.t.Fatalf("register %s: status %d body %s", name, r.status, r.body)
	}
	return r.cookie(session.GameCookieName), num(e.t, r.json(e.t), "user_id")
}

func (e *env) login(name string) string {
	e.t.Helper()
	r := e.api(http.MethodPost, "/api/login", "", map[string]any{
		"username": name, "password": testPassword,
	})
	if r.status != http.StatusOK {
		e.t.Fatalf("login %s: status %d body %s", name, r.status, r.body)
	}
	return r.cookie(session.GameCookieName)
}

var _ = (*env).login

func (e *env) adminCookie() string {
	e.t.Helper()
	r := e.adminAPI(http.MethodPost, "/api/admin/login", "", map[string]any{
		"username": testAdminUser, "password": testAdminPass,
	})
	if r.status != http.StatusNoContent {
		e.t.Fatalf("admin login: status %d body %s", r.status, r.body)
	}
	return r.cookie(session.AdminCookieName)
}

func (e *env) balance(uid int64) int64 {
	e.t.Helper()
	b, err := e.st.BalanceOf(context.Background(), uid)
	if err != nil {
		e.t.Fatalf("BalanceOf(%d): %v", uid, err)
	}
	return b
}

func (e *env) scalar(sql string, args ...any) int64 {
	e.t.Helper()
	var n int64
	if err := e.st.Pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		e.t.Fatalf("scalar %q: %v", sql, err)
	}
	return n
}

func (e *env) exec(sql string, args ...any) {
	e.t.Helper()
	if _, err := e.st.Pool.Exec(context.Background(), sql, args...); err != nil {
		e.t.Fatalf("exec %q: %v", sql, err)
	}
}

// dbLog adapts middleware.LogWriter onto the real async http_log writer, as cmd/unofa does.
type dbLog struct{ w *store.AsyncWriter }

func (d dbLog) Write(e middleware.LogEntry) {
	d.w.Write(store.LogEntry{
		RequestID:  e.RequestID,
		Endpoint:   e.Endpoint,
		StatusCode: e.StatusCode,
		UserID:     e.UserID,
		At:         e.At,
	})
}

// quietLogger keeps slog output out of test logs.
func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// fmtInt formats an int64 for a query string.
func fmtInt(n int64) string { return strconv.FormatInt(n, 10) }
