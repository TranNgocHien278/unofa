// Package e2e boots the real cmd/unofa binary and drives its HTTP and WebSocket
// surface over the network. It proves config, store, redis, sessions, rate limit,
// match hub and jobs are wired together.
//
// Runs only when UNOFA_TEST_DSN is set. The DSN must point at a database that is
// dedicated to this test and already migrated.
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5"
)

const (
	gameBase   = "http://127.0.0.1:18090"
	gameWS     = "ws://127.0.0.1:18090/api/ws"
	gameOrigin = "https://uno.example.com"
	cookieName = "__Host-unofa_sid"
	password   = "correct-horse-battery"
)

var (
	httpClient = &http.Client{Timeout: 10 * time.Second}
	codeRe     = regexp.MustCompile(`^[A-Z2-9]{6}$`)
)

// resp is one HTTP exchange. sid is the session token from a Set-Cookie header, if any.
type resp struct {
	status int
	body   []byte
	sid    string
}

// call sends one request with the session cookie set by hand. No cookie jar is used.
func call(t *testing.T, method, path, sid string, body any) resp {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, gameBase+path, rd)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if sid != "" {
		req.Header.Set("Cookie", cookieName+"="+sid)
	}
	r, err := httpClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer r.Body.Close()
	b, _ := io.ReadAll(r.Body)
	out := resp{status: r.StatusCode, body: b}
	for _, c := range r.Cookies() {
		if c.Name == cookieName {
			out.sid = c.Value
		}
	}
	return out
}

func decode(t *testing.T, r resp) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(r.body, &m); err != nil {
		t.Fatalf("decode %q: %v", r.body, err)
	}
	return m
}

// expect reports a mismatch and lets the flow continue.
func expect(t *testing.T, label string, r resp, want int) {
	t.Helper()
	if r.status != want {
		t.Errorf("%s: status %d want %d body=%s", label, r.status, want, r.body)
	}
}

// must stops the test: later steps need this response.
func must(t *testing.T, label string, r resp, want int) {
	t.Helper()
	if r.status != want {
		t.Fatalf("%s: status %d want %d body=%s", label, r.status, want, r.body)
	}
}

func num(m map[string]any, key string) float64 {
	f, _ := m[key].(float64)
	return f
}

func tailFile(path string, n int) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return err.Error()
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

func configYAML(dsn string) string {
	return fmt.Sprintf(`server:
  game_addr: "127.0.0.1:18090"
  admin_addr: "127.0.0.1:18091"
  game_origin: "https://uno.example.com"
  admin_origin: "https://admin.example.com"
  trusted_proxies: ["172.16.0.0/12"]
db:
  dsn: %q
  max_conns: 20
  min_conns: 5
  conn_timeout: 5s
redis:
  addr: "127.0.0.1:56379"
  db: 3
  session_ttl: 15m
  heartbeat_interval: 1m
money:
  signup_bonus: 200
  transfer_daily_limit: 1000
  transfer_min: 1
loan:
  packages: [500, 1000, 2000]
  terms:
    - { name: "24h", duration: 24h, rate_bps: 100 }
    - { name: "3d",  duration: 72h, rate_bps: 300 }
    - { name: "30d", duration: 720h, rate_bps: 1000 }
  overdue_bps: 500
  cap_multiplier: 2
game:
  min_players: 2
  max_players: 10
  hand_size: 7
  turn_timeout: 20s
  ready_timeout: 5s
  reconnect_grace: 90s
  max_message_bytes: 4096
  allowed_stakes: [10, 25, 50, 100, 500]
jobs:
  invariant_interval: 1h
  loan_seize_interval: 1h
  http_log_retention: 1440h
  http_log_cleanup_interval: 24h
  start_delay: 30s
log:
  level: "info"
  format: "json"
rate:
  login_per_ip:          { limit: 5,  period: 1m }
  login_per_username:    { limit: 10, period: 1h }
  login_lock: 15m
  register_per_ip:       { limit: 10, period: 1h }
  heartbeat_per_session: { limit: 3,  period: 1m }
  ws_actions_per_user:   { limit: 5,  period: 1s }
  ws_uno_per_user:       { limit: 2,  period: 1s }
  match_state_per_user:  { limit: 60, period: 1m }
  money_per_user:        { limit: 20, period: 1m }
  credential_per_user:   { limit: 10, period: 1m }
  read_per_user:         { limit: 60, period: 1m }
  room_create_per_user:  { limit: 10, period: 1m }
  room_join_per_user:    { limit: 20, period: 1m }
  admin_login_per_ip:    { limit: 5,  period: 1m }
  admin_per_admin:       { limit: 60, period: 1m }
  ws_messages_per_conn:  { limit: 10, period: 1s }
`, dsn)
}

func TestE2EFullFlow(t *testing.T) {
	dsn := os.Getenv("UNOFA_TEST_DSN")
	if dsn == "" {
		t.Skip("UNOFA_TEST_DSN unset")
	}

	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "unofa")
	build := exec.Command("go", "build", "-o", bin, "./cmd/unofa")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}

	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(configYAML(dsn)), 0o600); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "server.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(bin, "-config", cfgPath)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		t.Fatalf("start server: %v", err)
	}
	var waitErr error
	exited := make(chan struct{})
	go func() {
		waitErr = cmd.Wait()
		close(exited)
	}()

	t.Cleanup(func() {
		if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
			t.Errorf("SIGTERM: %v (server may have exited early)", err)
		}
		select {
		case <-exited:
			if waitErr != nil {
				t.Errorf("server did not exit cleanly: %v", waitErr)
			}
		case <-time.After(15 * time.Second):
			_ = cmd.Process.Kill()
			t.Errorf("server did not exit within 15s of SIGTERM")
		}
		logFile.Close()
		logs := tailFile(logPath, 200)
		if !strings.Contains(logs, "server shutdown complete") {
			t.Errorf("missing 'server shutdown complete' log line")
		}
		if t.Failed() {
			t.Logf("server log tail:\n%s", tailFile(logPath, 60))
		}
	})

	// Readiness: poll /healthz until 200, bounded by a 10s deadline.
	healthy := func() bool {
		r, err := httpClient.Get(gameBase + "/healthz")
		if err != nil {
			return false
		}
		r.Body.Close()
		return r.StatusCode == http.StatusOK
	}
	deadline := time.Now().Add(10 * time.Second)
	for !healthy() {
		select {
		case <-exited:
			t.Fatalf("server exited during startup: %v\n%s", waitErr, tailFile(logPath, 60))
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("/healthz not 200 within 10s\n%s", tailFile(logPath, 60))
		}
		time.Sleep(100 * time.Millisecond)
	}

	db, err := pgx.Connect(context.Background(), dsn)
	if err != nil {
		t.Fatalf("pgx connect: %v", err)
	}
	defer db.Close(context.Background())

	suffix := strconv.FormatInt(time.Now().UnixNano()%1000000000, 10)
	nameA := "e2e_a_" + suffix
	nameB := "e2e_b_" + suffix

	// a. register two players
	ra := call(t, "POST", "/api/register", "", map[string]any{"username": nameA, "password": password, "terms_accepted": true})
	must(t, "a: register A", ra, http.StatusCreated)
	if ra.sid == "" {
		t.Fatal("a: register A set no session cookie")
	}
	rb := call(t, "POST", "/api/register", "", map[string]any{"username": nameB, "password": password, "terms_accepted": true})
	must(t, "a: register B", rb, http.StatusCreated)
	if rb.sid == "" {
		t.Fatal("a: register B set no session cookie")
	}
	idA := int64(num(decode(t, ra), "user_id"))
	idB := int64(num(decode(t, rb), "user_id"))
	t.Logf("a: register A=201 id=%d; B=201 id=%d", idA, idB)

	// b. login both; the login cookie replaces the register cookie
	la := call(t, "POST", "/api/login", "", map[string]any{"username": nameA, "password": password, "terms_version": 1})
	must(t, "b: login A", la, http.StatusOK)
	lb := call(t, "POST", "/api/login", "", map[string]any{"username": nameB, "password": password, "terms_version": 1})
	must(t, "b: login B", lb, http.StatusOK)
	sidA, sidB := la.sid, lb.sid
	if sidA == "" || sidB == "" {
		t.Fatal("b: login set no session cookie")
	}
	t.Logf("b: login A=200 B=200")

	// c. heartbeat
	hb := call(t, "POST", "/api/session/heartbeat", sidA, nil)
	expect(t, "c: heartbeat A", hb, http.StatusNoContent)

	// d. A transfers 100U to B
	balA := func() float64 {
		r := call(t, "GET", "/api/me", sidA, nil)
		expect(t, "d: GET /api/me A", r, http.StatusOK)
		return num(decode(t, r), "balance")
	}
	beforeA := balA()
	tr := call(t, "POST", "/api/transfers", sidA, map[string]any{
		"to_username": nameB, "amount": 100, "client_uuid": "e2e-transfer-" + suffix,
	})
	expect(t, "d: transfer A->B", tr, http.StatusCreated)
	afterA := balA()
	if beforeA-afterA != 100 {
		t.Errorf("d: A balance dropped by %v, want exactly 100 (before=%v after=%v)", beforeA-afterA, beforeA, afterA)
	}
	t.Logf("d: transfer 201, A balance %v -> %v", beforeA, afterA)

	// e. transactions for both sides, plus paging shape
	for _, side := range []struct {
		label, sid, dir, cp string
	}{
		{"A", sidA, "out", nameB},
		{"B", sidB, "in", nameA},
	} {
		r := call(t, "GET", "/api/transactions", side.sid, nil)
		expect(t, "e: transactions "+side.label, r, http.StatusOK)
		m := decode(t, r)
		items, ok := m["items"].([]any)
		if !ok {
			t.Errorf("e: %s: items is not an array: %s", side.label, r.body)
		}
		if _, ok := m["next_cursor"]; !ok {
			t.Errorf("e: %s: next_cursor key missing: %s", side.label, r.body)
		}
		found := false
		for _, it := range items {
			row, _ := it.(map[string]any)
			if row["reason"] == "TRANSFER" && num(row, "amount") == 100 &&
				row["direction"] == side.dir && row["counterparty"] == side.cp {
				found = true
			}
		}
		if !found {
			t.Errorf("e: %s: TRANSFER %s 100 vs %s not in items: %s", side.label, side.dir, side.cp, r.body)
		}
	}

	// f. balance chart: spec path is /api/me/balance-chart; fall back to the router's path
	chartPath := "/api/me/balance-chart?range=30d"
	rc := call(t, "GET", chartPath, sidA, nil)
	if rc.status == http.StatusNotFound {
		t.Errorf("f: GET /api/me/balance-chart returned 404; spec 04 path not registered, router has GET /api/me/balance")
		chartPath = "/api/me/balance?range=30d"
		rc = call(t, "GET", chartPath, sidA, nil)
	}
	expect(t, "f: chart "+chartPath, rc, http.StatusOK)
	points, _ := decode(t, rc)["points"].([]any)
	for _, p := range points {
		if row, _ := p.(map[string]any); num(row, "balance") == 0 {
			t.Errorf("f: zero-valued chart point injected: %v", row)
		}
	}
	t.Logf("f: chart %s 200 points=%d", chartPath, len(points))

	// g. room at stake 10, B joins, both ready
	rr := call(t, "POST", "/api/rooms", sidA, map[string]any{"stake": 10})
	must(t, "g: create room", rr, http.StatusCreated)
	code, _ := decode(t, rr)["code"].(string)
	if !codeRe.MatchString(code) {
		t.Fatalf("g: room code %q is not 6 chars of [A-Z2-9]", code)
	}
	expect(t, "g: B joins", call(t, "POST", "/api/rooms/"+code+"/join", sidB, nil), http.StatusOK)
	expect(t, "g: A ready", call(t, "POST", "/api/rooms/"+code+"/ready", sidA, map[string]any{"ready": true}), http.StatusNoContent)
	expect(t, "g: B ready", call(t, "POST", "/api/rooms/"+code+"/ready", sidB, map[string]any{"ready": true}), http.StatusNoContent)
	t.Logf("g: room create 201 code=%s, join 200, ready 204 x2", code)

	// h. host plays, then per-viewer state
	rp := call(t, "POST", "/api/rooms/"+code+"/play", sidA, nil)
	must(t, "h: play", rp, http.StatusOK)
	matchID, _ := decode(t, rp)["match_id"].(string)
	if matchID == "" {
		t.Fatalf("h: play returned no match_id: %s", rp.body)
	}
	t.Logf("h: play 200 match_id=%s", matchID)

	// Handshake waits up to ready_timeout for ready_ack, which only sockets send. Poll
	// until the match goroutine answers. A 403 NOT_IN_MATCH during handshake is expected.
	var rs resp
	for deadline := time.Now().Add(15 * time.Second); ; {
		rs = call(t, "GET", "/api/matches/"+matchID+"/state", sidA, nil)
		if rs.status == http.StatusOK || time.Now().After(deadline) {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	expect(t, "h: A match state", rs, http.StatusOK)
	vA := decode(t, rs)
	you, _ := vA["you"].(map[string]any)
	public, _ := vA["public"].(map[string]any)
	handA, _ := you["hand"].([]any)
	if len(handA) != 7 {
		t.Errorf("h: A hand size %d, want 7", len(handA))
	}
	if num(you, "user_id") != float64(idA) {
		t.Errorf("h: A view you.user_id=%v want %d", you["user_id"], idA)
	}
	if _, leak := public["hand"]; leak {
		t.Errorf("h: LEAK public has a hand key")
	}
	if n := bytes.Count(rs.body, []byte(`"hand"`)); n != 1 {
		t.Errorf("h: A view contains %d \"hand\" keys, want exactly 1 (under you)", n)
	}
	players, _ := public["players"].([]any)
	for _, p := range players {
		row, _ := p.(map[string]any)
		if _, leak := row["hand"]; leak {
			t.Errorf("h: LEAK public player row has hand: %v", row)
		}
		if num(row, "user_id") == float64(idB) && num(row, "hand_count") != 7 {
			t.Errorf("h: B hand_count=%v want 7", row["hand_count"])
		}
	}
	if num(public, "pot") != 20 {
		t.Errorf("h: pot=%v want 20 (2 players x stake 10)", public["pot"])
	}

	rsB := call(t, "GET", "/api/matches/"+matchID+"/state", sidB, nil)
	expect(t, "h: B match state", rsB, http.StatusOK)
	handB, _ := decode(t, rsB)["you"].(map[string]any)["hand"].([]any)
	if len(handB) != 7 {
		t.Errorf("h: B hand size %d, want 7", len(handB))
	}
	if reflect.DeepEqual(handA, handB) {
		t.Errorf("h: A and B views carry identical hands")
	}

	// i. WebSocket hello for A with the configured Origin
	hdr := http.Header{}
	hdr.Set("Cookie", cookieName+"="+sidA)
	hdr.Set("Origin", gameOrigin)
	ws, wsResp, err := websocket.DefaultDialer.Dial(gameWS, hdr)
	if err != nil {
		status := 0
		var body []byte
		if wsResp != nil {
			status = wsResp.StatusCode
			body, _ = io.ReadAll(io.LimitReader(wsResp.Body, 512))
			wsResp.Body.Close()
		}
		t.Errorf("i: ws dial: %v (http status %d body=%q)", err, status, body)
	} else {
		defer ws.Close()
		_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
		_, msg, err := ws.ReadMessage()
		if err != nil {
			t.Errorf("i: ws read hello: %v", err)
		} else {
			var frame map[string]any
			_ = json.Unmarshal(msg, &frame)
			data, _ := frame["data"].(map[string]any)
			if frame["name"] != "hello" || num(data, "user_id") != float64(idA) || data["match_id"] != matchID {
				t.Errorf("i: hello frame wrong: %s", msg)
			} else {
				t.Logf("i: ws hello user_id=%d match_id=%s", idA, matchID)
			}
		}
	}

	// j. http_log rows for API calls, none for /healthz
	countLog := func(where string) int {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		var n int
		if err := db.QueryRow(ctx, "SELECT count(*) FROM http_log WHERE "+where).Scan(&n); err != nil {
			t.Fatalf("http_log count: %v", err)
		}
		return n
	}
	logDeadline := time.Now().Add(5 * time.Second)
	for countLog("endpoint = '/api/transfers'") == 0 && time.Now().Before(logDeadline) {
		time.Sleep(200 * time.Millisecond)
	}
	if n := countLog("endpoint = '/api/transfers'"); n == 0 {
		t.Errorf("j: no http_log row for /api/transfers")
	}
	if n := countLog("endpoint LIKE '/api/%'"); n == 0 {
		t.Errorf("j: no http_log rows for any /api/ call")
	}
	if n := countLog("endpoint LIKE '/healthz%'"); n != 0 {
		t.Errorf("j: %d http_log rows for /healthz, want 0", n)
	}

	// 4. money invariant: SUM(users.balance) + SUM(matches.pot) must be 0
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var invariant int64
	if err := db.QueryRow(ctx, `SELECT (SELECT COALESCE(SUM(balance),0) FROM users) +
		(SELECT COALESCE(SUM(pot),0) FROM matches)`).Scan(&invariant); err != nil {
		t.Fatalf("invariant query: %v", err)
	}
	if invariant != 0 {
		t.Errorf("4: money invariant = %d, want 0", invariant)
	}
	t.Logf("4: money invariant SUM(users.balance)+SUM(matches.pot) = %d", invariant)
}
