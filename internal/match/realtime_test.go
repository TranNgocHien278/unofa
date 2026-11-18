package match

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"

	"github.com/unofa/unofa/internal/clock"
	"github.com/unofa/unofa/internal/config"
	"github.com/unofa/unofa/internal/game"
	"github.com/unofa/unofa/internal/ledger"
	"github.com/unofa/unofa/internal/session"
	"github.com/unofa/unofa/internal/store"
)

const testOrigin = "https://game.test"

var userSeq atomic.Int64

type env struct {
	t    *testing.T
	ctx  context.Context
	hub  *Hub
	st   *store.Store
	sess *session.Manager
	srv  *httptest.Server
}

type player struct {
	uid    int64
	cookie *http.Cookie
}

// newEnv resets Postgres, flushes Redis DB 2, and serves hub.ServeWS over httptest.
func newEnv(t *testing.T, grace time.Duration) *env {
	return newEnvOpts(t, grace, clock.Real{}, 20*time.Second)
}

// newEnvOpts is newEnv with an injectable clock and turn timeout (W1 needs to put
// the clock exactly on the turn deadline without sleeping).
func newEnvOpts(t *testing.T, grace time.Duration, clk clock.Clock, turn time.Duration) *env {
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
	if _, err := st.Pool.Exec(ctx, `
		TRUNCATE admin_flags, balance_snapshot, transactions, loans, match_players, matches, room_members, rooms, users RESTART IDENTITY CASCADE;
		INSERT INTO users (user_id, username, password_hash, balance) VALUES (0, 'System', NULL, 0);`); err != nil {
		t.Fatalf("reset: %v", err)
	}

	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:56379", DB: 2})
	t.Cleanup(func() { rdb.Close() })
	if err := rdb.FlushDB(ctx).Err(); err != nil {
		t.Fatalf("redis flush: %v", err)
	}

	cfg := &config.Config{Game: config.GameCfg{
		MinPlayers:      2,
		MaxPlayers:      10,
		HandSize:        7,
		TurnTimeout:     turn,
		ReadyTimeout:    5 * time.Second,
		ReconnectGrace:  grace,
		MaxMessageBytes: 4096,
	}}
	sess := session.NewGame(rdb, 15*time.Minute)
	hub := New(cfg, st.Pool, st, clk, slog.Default())
	mux := http.NewServeMux()
	mux.HandleFunc("/api/ws", hub.ServeWS(sess, testOrigin))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &env{t: t, ctx: ctx, hub: hub, st: st, sess: sess, srv: srv}
}

func (e *env) newPlayer() player {
	e.t.Helper()
	n := userSeq.Add(1)
	u, err := e.st.CreateUser(e.ctx, fmt.Sprintf("ws%d_%d", time.Now().UnixNano(), n), "hash", 1, 1000)
	if err != nil {
		e.t.Fatalf("CreateUser: %v", err)
	}
	rec := httptest.NewRecorder()
	if _, err := e.sess.Create(e.ctx, rec, strconv.FormatInt(u.UserID, 10)); err != nil {
		e.t.Fatalf("session create: %v", err)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		e.t.Fatalf("want 1 cookie, got %d", len(cookies))
	}
	return player{uid: u.UserID, cookie: cookies[0]}
}

func (e *env) dial(p player, origin string) (*websocket.Conn, *http.Response, error) {
	d := websocket.Dialer{HandshakeTimeout: 5 * time.Second}
	h := http.Header{}
	h.Set("Origin", origin)
	h.Set("Cookie", p.cookie.Name+"="+p.cookie.Value)
	url := "ws" + strings.TrimPrefix(e.srv.URL, "http") + "/api/ws"
	return d.Dial(url, h)
}

func (e *env) mustDial(p player) *websocket.Conn {
	e.t.Helper()
	c, _, err := e.dial(p, testOrigin)
	if err != nil {
		e.t.Fatalf("dial: %v", err)
	}
	e.t.Cleanup(func() { c.Close() })
	return c
}

// startMatch builds a room, a running match with funded pot, and calls StartMatch.
func (e *env) startMatch(players ...player) (matchID string) {
	e.t.Helper()
	room, err := e.st.CreateRoom(e.ctx, players[0].uid, 10)
	if err != nil {
		e.t.Fatalf("CreateRoom: %v", err)
	}
	for _, p := range players[1:] {
		if _, err := e.st.AddMember(e.ctx, room.RoomID, p.uid); err != nil {
			e.t.Fatalf("AddMember: %v", err)
		}
	}
	m, err := e.st.CreateMatch(e.ctx, room.RoomID, 10)
	if err != nil {
		e.t.Fatalf("CreateMatch: %v", err)
	}
	uids := make([]int64, 0, len(players))
	seats := make([]int, 0, len(players))
	for i, p := range players {
		uids = append(uids, p.uid)
		seats = append(seats, i)
	}
	if err := e.st.AddMatchPlayers(e.ctx, m.MatchID, uids, seats); err != nil {
		e.t.Fatalf("AddMatchPlayers: %v", err)
	}
	for _, p := range players {
		if err := pgx.BeginFunc(e.ctx, e.st.Pool, func(tx pgx.Tx) error {
			return ledger.Transfer(e.ctx, tx, ledger.User(p.uid), ledger.Match(m.MatchID), 10,
				ledger.ReasonBet, fmt.Sprintf("bet:%s:%d", m.MatchID, p.uid))
		}); err != nil {
			e.t.Fatalf("bet: %v", err)
		}
	}
	if err := e.hub.StartMatch(e.ctx, m.MatchID, room.RoomID); err != nil {
		e.t.Fatalf("StartMatch: %v", err)
	}
	return m.MatchID
}

// handshake answers ready_check on every socket, then returns each socket's match_start frame.
func (e *env) handshake(matchID string, conns ...*websocket.Conn) []map[string]any {
	e.t.Helper()
	for _, c := range conns {
		readUntil(e.t, c, "ready_check")
		ack := map[string]any{"type": "ready_ack", "ref": "r", "match_id": matchID}
		if err := c.WriteJSON(ack); err != nil {
			e.t.Fatalf("ready_ack: %v", err)
		}
	}
	out := make([]map[string]any, 0, len(conns))
	for _, c := range conns {
		out = append(out, readUntil(e.t, c, "match_start"))
	}
	return out
}

// readUntil returns the first event frame named name, skipping other frames.
func readUntil(t *testing.T, c *websocket.Conn, name string) map[string]any {
	t.Helper()
	for {
		c.SetReadDeadline(time.Now().Add(5 * time.Second))
		_, b, err := c.ReadMessage()
		if err != nil {
			t.Fatalf("waiting for %s: %v", name, err)
		}
		var f map[string]any
		if err := json.Unmarshal(b, &f); err != nil {
			t.Fatalf("bad frame: %v", err)
		}
		if f["name"] == name {
			return f
		}
	}
}

// waitMatchStatus polls match_players.status until it equals want.
func (e *env) waitMatchStatus(matchID string, uid int64, want string) {
	e.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var got string
	for {
		if err := e.st.Pool.QueryRow(e.ctx,
			`SELECT status FROM match_players WHERE match_id = $1::uuid AND user_id = $2`,
			matchID, uid).Scan(&got); err != nil {
			e.t.Fatalf("query status: %v", err)
		}
		if got == want {
			return
		}
		if time.Now().After(deadline) {
			e.t.Fatalf("match_players.status = %q, want %q", got, want)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (e *env) liveView(matchID string, uid int64) game.View {
	e.t.Helper()
	v, ok := e.hub.State(matchID, uid)
	if !ok {
		e.t.Fatalf("hub.State: match %s not live", matchID)
	}
	return v.(game.View)
}

// TestW3_OversizeFrameCloses1009: a client frame over MaxMessageBytes (4 KB) closes with 1009.
func TestW3_OversizeFrameCloses1009(t *testing.T) {
	e := newEnv(t, time.Minute)
	p := e.newPlayer()
	c := e.mustDial(p)
	readUntil(t, c, "hello")

	if err := c.WriteMessage(websocket.TextMessage, bytes.Repeat([]byte("x"), 5*1024)); err != nil {
		t.Fatalf("write: %v", err)
	}
	c.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		_, _, err := c.ReadMessage()
		if err == nil {
			continue
		}
		ce, ok := err.(*websocket.CloseError)
		if !ok || ce.Code != websocket.CloseMessageTooBig {
			t.Fatalf("want close 1009, got %v", err)
		}
		return
	}
}

// TestW4_BadOriginRejected403: a wrong Origin never upgrades.
func TestW4_BadOriginRejected403(t *testing.T) {
	e := newEnv(t, time.Minute)
	p := e.newPlayer()
	c, resp, err := e.dial(p, "https://evil.test")
	if c != nil {
		c.Close()
		t.Fatal("socket opened with bad Origin")
	}
	if err == nil || resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("want 403 handshake failure, got resp=%v err=%v", resp, err)
	}
}

// TestW5_SecondSocketReplacesFirst: the first socket gets 1000 SESSION_REPLACED.
func TestW5_SecondSocketReplacesFirst(t *testing.T) {
	e := newEnv(t, time.Minute)
	p := e.newPlayer()
	first := e.mustDial(p)
	readUntil(t, first, "hello")

	second := e.mustDial(p)
	readUntil(t, second, "hello")

	first.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		_, _, err := first.ReadMessage()
		if err == nil {
			continue
		}
		ce, ok := err.(*websocket.CloseError)
		if !ok || ce.Code != websocket.CloseNormalClosure || ce.Text != "SESSION_REPLACED" {
			t.Fatalf("first socket: want 1000 SESSION_REPLACED, got %v", err)
		}
		return
	}
}

// TestSeq_MatchStartIsSeq1 checks the spec rule that seq starts at 1 on match_start.
func TestSeq_MatchStartIsSeq1(t *testing.T) {
	e := newEnv(t, time.Minute)
	a, b := e.newPlayer(), e.newPlayer()
	ca, cb := e.mustDial(a), e.mustDial(b)
	readUntil(t, ca, "hello")
	readUntil(t, cb, "hello")
	matchID := e.startMatch(a, b)
	frames := e.handshake(matchID, ca, cb)
	for i, f := range frames {
		if f["seq"] != float64(1) {
			t.Fatalf("socket %d: match_start seq = %v, want 1", i, f["seq"])
		}
	}
}

// TestW6_ReconnectWithinGraceKeepsSeat: drop, reconnect inside grace, state intact.
func TestW6_ReconnectWithinGraceKeepsSeat(t *testing.T) {
	e := newEnv(t, time.Minute)
	a, b := e.newPlayer(), e.newPlayer()
	ca, cb := e.mustDial(a), e.mustDial(b)
	readUntil(t, ca, "hello")
	readUntil(t, cb, "hello")
	matchID := e.startMatch(a, b)
	e.handshake(matchID, ca, cb)

	ca.Close()
	e.waitMatchStatus(matchID, a.uid, "disconnected")

	ca2 := e.mustDial(a)
	readUntil(t, ca2, "hello")
	readUntil(t, cb, "player_status")
	e.waitMatchStatus(matchID, a.uid, "connected")

	v := e.liveView(matchID, a.uid)
	if len(v.You.Hand) != 7 {
		t.Fatalf("hand size after reconnect = %d, want 7", len(v.You.Hand))
	}
	var seen bool
	for _, pl := range v.Public.Players {
		if pl.UserID == a.uid {
			seen = true
			if pl.Seat != 0 || pl.Status != "connected" {
				t.Fatalf("seat/status after reconnect = %d/%s, want 0/connected", pl.Seat, pl.Status)
			}
		}
	}
	if !seen {
		t.Fatal("player missing from public snapshot")
	}
}

// TestW7_GraceExpiryForfeits: grace expires, seat is forfeited, survivor gets match_end.
func TestW7_GraceExpiryForfeits(t *testing.T) {
	e := newEnv(t, 200*time.Millisecond)
	a, b := e.newPlayer(), e.newPlayer()
	ca, cb := e.mustDial(a), e.mustDial(b)
	readUntil(t, ca, "hello")
	readUntil(t, cb, "hello")
	matchID := e.startMatch(a, b)
	e.handshake(matchID, ca, cb)

	ca.Close()
	e.waitMatchStatus(matchID, a.uid, "forfeit")

	end := readUntil(t, cb, "match_end")
	data := end["data"].(map[string]any)
	if data["winner_id"] != float64(b.uid) || data["reason"] != "last_standing" || data["pot"] != float64(20) {
		t.Fatalf("match_end = %v, want winner %d, reason last_standing, pot 20", data, b.uid)
	}
}

// TestW8_HelloAfterReconnectCarriesMatchID: hello after reconnect has the live match_id.
func TestW8_HelloAfterReconnectCarriesMatchID(t *testing.T) {
	e := newEnv(t, time.Minute)
	a, b := e.newPlayer(), e.newPlayer()
	ca, cb := e.mustDial(a), e.mustDial(b)
	readUntil(t, ca, "hello")
	readUntil(t, cb, "hello")
	matchID := e.startMatch(a, b)
	e.handshake(matchID, ca, cb)

	ca.Close()
	e.waitMatchStatus(matchID, a.uid, "disconnected")

	ca2 := e.mustDial(a)
	hello := readUntil(t, ca2, "hello")
	if got := hello["data"].(map[string]any)["match_id"]; got != matchID {
		t.Fatalf("hello match_id = %v, want %s", got, matchID)
	}
}

// --- shared harness for W1/W2/W9/W10/W11 and the full-match seq check ---

// capture reads one socket in the background and keeps every frame as raw JSON.
// Tests start it after the handshake so it never competes with readUntil.
type capture struct {
	mu  sync.Mutex
	raw []string
	err error
}

func newCapture(c *websocket.Conn) *capture {
	cp := &capture{}
	go func() {
		for {
			c.SetReadDeadline(time.Now().Add(60 * time.Second))
			_, b, err := c.ReadMessage()
			if err != nil {
				cp.mu.Lock()
				cp.err = err
				cp.mu.Unlock()
				return
			}
			cp.mu.Lock()
			cp.raw = append(cp.raw, string(b))
			cp.mu.Unlock()
		}
	}()
	return cp
}

func (cp *capture) snapshot() ([]string, error) {
	cp.mu.Lock()
	defer cp.mu.Unlock()
	return append([]string(nil), cp.raw...), cp.err
}

// frames decodes the raw frames captured so far. Malformed frames fail the test.
func (cp *capture) frames(t *testing.T) []map[string]any {
	t.Helper()
	raw, _ := cp.snapshot()
	out := make([]map[string]any, 0, len(raw))
	for _, r := range raw {
		var f map[string]any
		if err := json.Unmarshal([]byte(r), &f); err != nil {
			t.Fatalf("bad frame %q: %v", r, err)
		}
		out = append(out, f)
	}
	return out
}

// waitFrame polls the capture until pred matches a frame or the deadline passes.
func (cp *capture) waitFrame(t *testing.T, what string, d time.Duration, pred func(map[string]any) bool) map[string]any {
	t.Helper()
	deadline := time.Now().Add(d)
	for {
		for _, f := range cp.frames(t) {
			if pred(f) {
				return f
			}
		}
		if time.Now().After(deadline) {
			raw, _ := cp.snapshot()
			t.Fatalf("timed out after %s waiting for %s (captured %d frames): %q", d, what, len(raw), raw)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func frameData(f map[string]any) map[string]any {
	d, _ := f["data"].(map[string]any)
	return d
}

// seqs lists the seq of every event frame in arrival order, ignoring frames
// without one (hello, errors).
func seqs(frames []map[string]any) []int {
	out := make([]int, 0, len(frames))
	for _, f := range frames {
		if v, ok := f["seq"].(float64); ok {
			out = append(out, int(v))
		}
	}
	return out
}

// sendAction writes one client action frame.
func sendAction(c *websocket.Conn, matchID, ref, action string, card *game.WireCard, color string) error {
	f := map[string]any{"type": "action", "ref": ref, "match_id": matchID, "action": action}
	if card != nil {
		f["card"] = card
	}
	if color != "" {
		f["chosen_color"] = color
	}
	c.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return c.WriteJSON(f)
}

// hasSocket reports whether uid still has a registered socket.
func (e *env) hasSocket(uid int64) bool {
	e.hub.mu.Lock()
	defer e.hub.mu.Unlock()
	for _, c := range e.hub.socks {
		if c.userID == uid {
			return true
		}
	}
	return false
}

func (e *env) matchLive(id string) bool {
	e.hub.mu.Lock()
	defer e.hub.mu.Unlock()
	_, ok := e.hub.matches[id]
	return ok
}

// viewOf asks the match goroutine for uid's snapshot, retrying while it is busy.
func (e *env) viewOf(matchID string, uid int64) (game.View, bool) {
	for i := 0; i < 20; i++ {
		if v, ok := e.hub.State(matchID, uid); ok {
			return v.(game.View), true
		}
		if !e.matchLive(matchID) {
			return game.View{}, false
		}
		time.Sleep(5 * time.Millisecond)
	}
	return game.View{}, false
}

func anyUID(conns map[int64]*websocket.Conn) int64 {
	for uid := range conns {
		return uid
	}
	return 0
}

// forcedPlay mirrors game.playable for the bot: wilds are always legal, otherwise
// the card must match the active color or the top card's rank.
func forcedPlay(v game.View) *game.WireCard {
	for i := range v.You.Hand {
		c := v.You.Hand[i]
		if c.Rank == "WILD" || c.Rank == "WILD4" || c.Color == v.Public.ActiveColor || c.Rank == v.Public.TopCard.Rank {
			return &c
		}
	}
	return nil
}

// step plays one turn of the match over the action channel and waits for the frame
// that proves it landed, so the next action never races the match goroutine. The
// player holding the turn plays the first legal card; otherwise draws and passes;
// a pending WILD4 challenger accepts. It returns false once the match is no longer
// live.
func (e *env) step(matchID string, conns map[int64]*websocket.Conn, caps map[int64]*capture) bool {
	v, ok := e.viewOf(matchID, anyUID(conns))
	if !ok {
		return false
	}
	if v.Public.Phase == "awaiting_challenge" && v.Public.PendingChallenger != nil {
		uid := *v.Public.PendingChallenger
		c := conns[uid]
		if c == nil {
			return false
		}
		before := caps[uid].count()
		if err := sendAction(c, matchID, "ch", "accept", nil, ""); err != nil {
			return false
		}
		return caps[uid].waitMore(e.t, before, 5*time.Second)
	}
	cur := v.Public.CurrentPlayer
	c := conns[cur]
	if c == nil {
		return false
	}
	cv, ok := e.viewOf(matchID, cur)
	if !ok {
		return false
	}
	before := caps[cur].count()
	ref := fmt.Sprintf("s%d", cv.Seq)
	if card := forcedPlay(cv); card != nil {
		if err := sendAction(c, matchID, ref, "play", card, "RED"); err != nil {
			return false
		}
		return caps[cur].waitMore(e.t, before, 5*time.Second)
	}
	if err := sendAction(c, matchID, ref, "draw", nil, ""); err != nil {
		return false
	}
	if !caps[cur].waitMore(e.t, before, 5*time.Second) {
		return false
	}
	before = caps[cur].count()
	if err := sendAction(c, matchID, ref+"p", "pass", nil, ""); err != nil {
		return false
	}
	return caps[cur].waitMore(e.t, before, 5*time.Second)
}

// count is how many frames have arrived so far.
func (cp *capture) count() int {
	cp.mu.Lock()
	defer cp.mu.Unlock()
	return len(cp.raw)
}

// waitMore waits for any frame after index `after`. It returns false on timeout
// instead of failing, because a match can end between the state read and the write.
func (cp *capture) waitMore(t *testing.T, after int, d time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(d)
	for {
		if cp.count() > after {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// TestW1_PlayAtDeadlineSingleOutcome: a play sent as the turn deadline arrives is
// either accepted or refused, never accepted and then also penalized. The timer
// channel is not injectable, so this spins to the deadline (no sleep) and tolerates
// both orderings; what it pins down is one outcome per turn.
func TestW1_PlayAtDeadlineSingleOutcome(t *testing.T) {
	e := newEnvOpts(t, time.Minute, clock.Real{}, 1500*time.Millisecond)
	a, b := e.newPlayer(), e.newPlayer()
	ca, cb := e.mustDial(a), e.mustDial(b)
	readUntil(t, ca, "hello")
	readUntil(t, cb, "hello")
	matchID := e.startMatch(a, b)
	e.handshake(matchID, ca, cb)

	conns := map[int64]*websocket.Conn{a.uid: ca, b.uid: cb}
	caps := map[int64]*capture{a.uid: newCapture(ca), b.uid: newCapture(cb)}

	var cur int64
	var cv game.View
	deadline := time.Now().Add(5 * time.Second)
	for cur == 0 && time.Now().Before(deadline) {
		for uid := range conns {
			if v, ok := e.viewOf(matchID, uid); ok && v.You.IsYourTurn {
				cur, cv = uid, v
			}
		}
		time.Sleep(2 * time.Millisecond)
	}
	if cur == 0 {
		t.Fatal("no player held the first turn")
	}

	// turn_deadline is second-precision; spin to that second, then fire.
	td, err := time.Parse(time.RFC3339, cv.Public.TurnDeadline)
	if err != nil {
		t.Fatalf("turn_deadline %q: %v", cv.Public.TurnDeadline, err)
	}
	for time.Now().Before(td) {
	}
	card := cv.You.Hand[0]
	if err := sendAction(conns[cur], matchID, "w1", "play", &card, "RED"); err != nil {
		t.Fatalf("play at deadline: %v", err)
	}

	own := caps[cur]
	if !own.waitFrameOK(t, 20*time.Second, func(f map[string]any) bool {
		d := frameData(f)
		if f["name"] != "action" || d["player_id"] != float64(cur) {
			return false
		}
		return d["action"] == "play" || d["action"] == "timeout"
	}) {
		t.Fatal("neither the play nor the timeout was applied; the turn is stuck")
	}

	var sawPlay, timeouts int
	for _, f := range own.frames(t) {
		d := frameData(f)
		if f["name"] != "action" || d["player_id"] != float64(cur) {
			continue
		}
		switch d["action"] {
		case "play":
			sawPlay++
		case "timeout":
			timeouts++
		}
	}
	if timeouts > 1 {
		t.Fatalf("%d timeout events for one turn, want at most 1", timeouts)
	}
	if sawPlay > 0 && timeouts > 0 {
		t.Fatalf("play accepted and the same turn was also penalized (play=%d timeout=%d)", sawPlay, timeouts)
	}
	if sawPlay == 0 {
		t.Logf("the tick won the race; the play was refused and the timeout applied once")
	}
}

// waitFrameOK is waitFrame without the fatal exit, for races where either outcome
// is legal.
func (cp *capture) waitFrameOK(t *testing.T, d time.Duration, pred func(map[string]any) bool) bool {
	t.Helper()
	deadline := time.Now().Add(d)
	for {
		for _, f := range cp.frames(t) {
			if pred(f) {
				return true
			}
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestW2_SimultaneousCatchUnoOneWinner: two players catch the same victim without
// reading in between. Both reach the match's single channel, so exactly one wins
// and the victim's hand grows by exactly two.
func TestW2_SimultaneousCatchUnoOneWinner(t *testing.T) {
	e := newEnv(t, time.Minute)
	ps := []player{e.newPlayer(), e.newPlayer(), e.newPlayer()}
	conns := map[int64]*websocket.Conn{}
	for _, pl := range ps {
		c := e.mustDial(pl)
		readUntil(t, c, "hello")
		conns[pl.uid] = c
	}
	matchID := e.startMatch(ps...)
	caps := handshakeCaptured(t, matchID, conns)

	window := func() (victim int64, ok bool) {
		for uid := range conns {
			if v, ok := e.viewOf(matchID, uid); ok && v.You.CanCatchUno != nil {
				return *v.You.CanCatchUno, true
			}
		}
		return 0, false
	}

	// Drive until someone drops to one card without calling UNO.
	var victim int64
	var found bool
	for i := 0; i < 300; i++ {
		if victim, found = window(); found {
			break
		}
		if !e.step(matchID, conns, caps) {
			break
		}
	}
	if !found {
		t.Fatal("no UNO window opened while driving the match")
	}

	var catchers []int64
	for uid := range conns {
		if uid != victim {
			catchers = append(catchers, uid)
		}
	}
	// Both catchers fire back to back, without reading between the two writes.
	for _, uid := range catchers {
		if err := sendAction(conns[uid], matchID, "catch", "catch_uno", nil, ""); err != nil {
			t.Fatalf("catch_uno: %v", err)
		}
	}
	// Poll until both decisions have landed: the broadcast success is shared, so
	// the loser's rejection can trail it by a round trip.
	// The success event is broadcast, so both catchers see it: count distinct seqs
	// (one per applied catch) and, separately, each socket's rejection.
	count := func() (success, closed int) {
		seqSeen := map[int]bool{}
		for _, uid := range catchers {
			for _, f := range caps[uid].frames(t) {
				d := frameData(f)
				if f["name"] == "action" && d["action"] == "catch_uno" && d["result"] == "success" {
					seq, _ := f["seq"].(float64)
					if !seqSeen[int(seq)] {
						seqSeen[int(seq)] = true
						success++
					}
				} else if f["type"] == "error" && f["code"] == "UNO_WINDOW_CLOSED" {
					closed++
				}
			}
		}
		return
	}
	deadline2 := time.Now().Add(5 * time.Second)
	var success, closed int
	for {
		success, closed = count()
		if success >= 1 && closed >= 1 {
			break
		}
		if time.Now().After(deadline2) {
			var dump []string
			for uid, c := range caps {
				raw, _ := c.snapshot()
				dump = append(dump, fmt.Sprintf("uid %d: %q", uid, raw))
			}
			t.Fatalf("success=%d UNO_WINDOW_CLOSED=%d, want one success and one rejection; %s",
				success, closed, strings.Join(dump, " | "))
		}
		time.Sleep(5 * time.Millisecond)
	}

	// The victim drew exactly two: one card plus the penalty.
	vv, ok := e.viewOf(matchID, victim)
	if !ok {
		t.Fatal("match not live after catch_uno")
	}
	if got := len(vv.You.Hand); got != 3 {
		t.Fatalf("victim hand = %d, want 3", got)
	}
}

// handshakeCaptured is handshake with a capture installed first, so match_start
// (seq 1) and everything after it is recorded.
func handshakeCaptured(t *testing.T, matchID string, conns map[int64]*websocket.Conn) map[int64]*capture {
	t.Helper()
	caps := map[int64]*capture{}
	for uid, c := range conns {
		caps[uid] = newCapture(c)
	}
	for uid, c := range conns {
		caps[uid].waitFrame(t, "ready_check", 5*time.Second, func(f map[string]any) bool {
			return f["name"] == "ready_check"
		})
		if err := c.WriteJSON(map[string]any{"type": "ready_ack", "ref": "r", "match_id": matchID}); err != nil {
			t.Fatalf("ready_ack: %v", err)
		}
	}
	for uid := range conns {
		caps[uid].waitFrame(t, "match_start", 5*time.Second, func(f map[string]any) bool {
			return f["name"] == "match_start"
		})
	}
	return caps
}

// dialSlow opens a socket whose kernel receive buffer is tiny, so a server write
// blocks after a few KB.
func (e *env) dialSlow(p player) *websocket.Conn {
	e.t.Helper()
	d := websocket.Dialer{
		HandshakeTimeout: 5 * time.Second,
		NetDialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			nd := &net.Dialer{Control: func(_, _ string, c syscall.RawConn) error {
				return c.Control(func(fd uintptr) {
					_ = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_RCVBUF, 2048)
				})
			}}
			return nd.DialContext(ctx, network, addr)
		},
	}
	h := http.Header{}
	h.Set("Origin", testOrigin)
	h.Set("Cookie", p.cookie.Name+"="+p.cookie.Value)
	url := "ws" + strings.TrimPrefix(e.srv.URL, "http") + "/api/ws"
	c, _, err := d.Dial(url, h)
	if err != nil {
		e.t.Fatalf("dial slow: %v", err)
	}
	e.t.Cleanup(func() { c.Close() })
	return c
}

// TestW9_SlowClientDoesNotStallMatch: one socket stops reading while the match
// floods events. The spec requires the server to close that socket and every other
// client to keep receiving every event with the right seq.
//
// The turn timeout is 10ms so the match emits far more bytes than a socket buffer
// holds; a write deadline of 5s (hub.go:80) is the only thing that can unblock the
// match goroutine, and deliver (hub.go:536) is the only thing that drops the socket.
func TestW9_SlowClientDoesNotStallMatch(t *testing.T) {
	e := newEnvOpts(t, 500*time.Millisecond, clock.Real{}, 10*time.Millisecond)
	a, b := e.newPlayer(), e.newPlayer()
	ca := e.mustDial(a)
	cb := e.dialSlow(b)
	readUntil(t, ca, "hello")
	readUntil(t, cb, "hello")
	matchID := e.startMatch(a, b)
	e.handshake(matchID, ca, cb)

	reader := newCapture(ca)

	// The reader must keep receiving with contiguous, monotonic seq while b stalls.
	hard := time.Now().Add(40 * time.Second)
	last := 0
	check := func() {
		s := seqs(reader.frames(t))
		if len(s) > 0 {
			last = s[len(s)-1]
		}
		seen := map[int]bool{1: true} // match_start was read by handshake
		for i, v := range s {
			if i > 0 && v < s[i-1] {
				t.Fatalf("seq went backwards on the reading socket: %d after %d", v, s[i-1])
			}
			seen[v] = true
		}
		for i := 1; i <= last; i++ {
			if !seen[i] {
				t.Fatalf("reading socket missed seq %d of %d events (last seq %d)", i, len(s), last)
			}
		}
	}
	for last < 20 {
		check()
		if time.Now().After(hard) {
			t.Fatalf("the reading socket stalled at seq %d: the stalled peer is holding "+
				"the match goroutine; hub.go has no per-connection drop policy", last)
		}
		time.Sleep(5 * time.Millisecond)
	}
	check()

	// The server must close the stalled socket. Reading it here would drain the
	// buffer and un-stall the write, so check the hub's registry instead, then
	// drain and require the connection to be already dead.
	gone := time.Now().Add(20 * time.Second)
	for e.hasSocket(b.uid) {
		check()
		if time.Now().After(gone) {
			t.Fatalf("stalled socket still registered after 20s (last seq %d): "+
				"project has a 5s write deadline at hub.go:80 and drop in deliver "+
				"(hub.go:536), but the socket was not shed", last)
		}
		time.Sleep(10 * time.Millisecond)
	}
	check()

	cb.SetReadDeadline(time.Now().Add(10 * time.Second))
	var drained int
	var rerr error
	for {
		if _, _, rerr = cb.ReadMessage(); rerr != nil {
			break
		}
		drained++
	}
	var netErr net.Error
	if errors.As(rerr, &netErr) && netErr.Timeout() {
		t.Fatalf("stalled socket: read timed out after %d frames; the server removed it "+
			"from the hub but never closed the connection", drained)
	}
	t.Logf("stalled socket shed after %d queued frames, client saw: %v", drained, rerr)
}

// TestW10_DrawPayloadHidesCard: the public draw event carries no card, and the
// drawn card reaches only the drawer's private frame.
//
// A draw emits two frames with the same seq: the private one, {"cards":[card]},
// and the public one. Other players get only the public one.
func TestW10_DrawPayloadHidesCard(t *testing.T) {
	e := newEnv(t, time.Minute)
	a, b := e.newPlayer(), e.newPlayer()
	ca, cb := e.mustDial(a), e.mustDial(b)
	readUntil(t, ca, "hello")
	readUntil(t, cb, "hello")
	matchID := e.startMatch(a, b)
	conns := map[int64]*websocket.Conn{a.uid: ca, b.uid: cb}
	caps := handshakeCaptured(t, matchID, conns)

	var cur int64
	for uid := range conns {
		if v, ok := e.viewOf(matchID, uid); ok && v.You.IsYourTurn {
			cur = uid
		}
	}
	if cur == 0 {
		t.Fatal("no player held the first turn")
	}
	if err := sendAction(conns[cur], matchID, "w10", "draw", nil, ""); err != nil {
		t.Fatalf("draw: %v", err)
	}

	isDraw := func(f map[string]any) bool {
		d := frameData(f)
		return f["name"] == "action" && d["action"] == "draw"
	}
	// The private frame is {"data":{"cards":[...]}} with no action field; the
	// public frame is the same seq with action=draw and no cards.
	private := caps[cur].waitFrame(t, "private draw payload", 5*time.Second, func(f map[string]any) bool {
		_, ok := frameData(f)["cards"]
		return f["name"] == "action" && ok
	})
	drawn, _ := frameData(private)["cards"].([]any)
	if len(drawn) != 1 {
		t.Fatalf("private draw payload = %v, want exactly one card", drawn)
	}
	card := drawn[0].(map[string]any)
	needle := fmt.Sprintf(`"color":%q,"rank":%q`, card["color"], card["rank"])

	for uid, c := range caps {
		pub := c.waitFrame(t, "public draw payload", 5*time.Second, func(f map[string]any) bool {
			_, hasCards := frameData(f)["cards"]
			return isDraw(f) && !hasCards
		})
		d := frameData(pub)
		if _, ok := d["card"]; ok {
			t.Fatalf("public draw payload carries a card: %v", d)
		}
		raw := mustJSON(t, pub)
		if strings.Contains(raw, needle) {
			t.Fatalf("public draw payload leaked the drawn card %v: %s", card, raw)
		}
		if uid != cur && strings.Contains(raw, `"cards"`) {
			t.Fatalf("non-drawer socket %d received a private cards payload: %s", uid, raw)
		}
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

// TestW11_NoForeignHandOrDeckInAnyFrame drives a whole match through the action
// channel and greps every frame every socket received.
//
// Patterns searched and why they are sufficient:
//   - `"deck"` / `"discard"`: the only wire keys a pile array or pile order could
//     use. Cards serialize as color+rank only, and the view exposes draw_pile_count
//     as a number, so a pile leak cannot reach a client without one of these keys.
//   - A card in two players' match_start hands: the dealer draws each card once, so
//     the same color+rank in two hands proves one of them was a foreign hand.
//   - `"cards"` (the private drawn-card payload) appearing on two sockets: draw
//     results are per-viewer, so the same payload on two sockets is a leak.
func TestW11_NoForeignHandOrDeckInAnyFrame(t *testing.T) {
	e := newEnv(t, time.Minute)
	ps := []player{e.newPlayer(), e.newPlayer(), e.newPlayer()}
	conns := map[int64]*websocket.Conn{}
	for _, p := range ps {
		c := e.mustDial(p)
		readUntil(t, c, "hello")
		conns[p.uid] = c
	}
	matchID := e.startMatch(ps...)
	caps := handshakeCaptured(t, matchID, conns)

	hard := time.Now().Add(90 * time.Second)
	steps := 0
	for {
		if !e.matchLive(matchID) {
			break
		}
		if time.Now().After(hard) || steps > 4000 {
			t.Fatalf("match did not end after %d actions", steps)
		}
		if !e.step(matchID, conns, caps) {
			break
		}
		steps++
	}
	caps[ps[0].uid].waitFrame(t, "match_end", 10*time.Second, func(f map[string]any) bool {
		return f["name"] == "match_end"
	})

	for uid, c := range caps {
		raw, _ := c.snapshot()
		for _, r := range raw {
			low := strings.ToLower(r)
			if strings.Contains(low, `"deck"`) || strings.Contains(low, `"discard"`) {
				t.Fatalf("socket %d received a pile payload: %s", uid, r)
			}
		}
		var starts int
		for _, f := range c.frames(t) {
			you, _ := frameData(f)["you"].(map[string]any)
			if f["name"] == "match_start" {
				starts++
				if len(you) == 0 {
					t.Fatalf("socket %d match_start has no you view: %v", uid, f)
				}
			}
			// A hand can only appear inside the per-viewer `you` object, and that
			// object must be the socket's own. Any other frame carrying `hand`
			// would be a hand payload on a public channel.
			if _, ok := frameData(f)["hand"]; ok {
				t.Fatalf("socket %d got a bare hand payload: %v", uid, f)
			}
			if len(you) > 0 {
				if got := you["user_id"]; got != float64(uid) {
					t.Fatalf("socket %d got another viewer's hand (you.user_id = %v)", uid, got)
				}
				if list, _ := you["hand"].([]any); f["name"] == "match_start" && len(list) != 7 {
					t.Fatalf("socket %d match_start hand = %d cards, want 7", uid, len(list))
				}
			}
		}
		if starts != 1 {
			t.Fatalf("socket %d received match_start %d times, want 1", uid, starts)
		}
	}

	// A drawn-cards payload is per-viewer. Each player has exactly one socket here,
	// and every private frame shares its seq with the public frame of the same
	// event, so one seq may carry cards on one socket only.
	owner := map[int]int64{}
	for uid, c := range caps {
		for _, f := range c.frames(t) {
			if _, ok := frameData(f)["cards"]; !ok {
				continue
			}
			seq, _ := f["seq"].(float64)
			if prev, dup := owner[int(seq)]; dup {
				t.Fatalf("drawn cards at seq %d reached sockets %d and %d", int(seq), prev, uid)
			}
			owner[int(seq)] = uid
		}
	}
}

// TestSeq_MonotonicAcrossFullMatch: across one match, seq starts at 1 on
// match_start and every later event increments it by exactly one, with no gaps. A
// socket may see one seq twice: the drawer gets the private frame, then the public
// frame of the same event.
func TestSeq_MonotonicAcrossFullMatch(t *testing.T) {
	e := newEnv(t, time.Minute)
	a, b := e.newPlayer(), e.newPlayer()
	ca, cb := e.mustDial(a), e.mustDial(b)
	readUntil(t, ca, "hello")
	readUntil(t, cb, "hello")
	matchID := e.startMatch(a, b)
	conns := map[int64]*websocket.Conn{a.uid: ca, b.uid: cb}
	caps := handshakeCaptured(t, matchID, conns)

	hard := time.Now().Add(90 * time.Second)
	steps := 0
	for {
		if !e.matchLive(matchID) {
			break
		}
		if time.Now().After(hard) || steps > 4000 {
			t.Fatalf("match did not end after %d actions", steps)
		}
		if !e.step(matchID, conns, caps) {
			break
		}
		steps++
	}
	caps[a.uid].waitFrame(t, "match_end", 10*time.Second, func(f map[string]any) bool {
		return f["name"] == "match_end"
	})

	for uid, c := range caps {
		s := seqs(c.frames(t))
		if len(s) == 0 || s[0] != 1 {
			t.Fatalf("socket %d: first seq = %v, want match_start at 1", uid, s)
		}
		highest := 0
		for _, v := range s {
			switch {
			case v == highest, v == highest+1:
				highest = v
			default:
				t.Fatalf("socket %d: seq %d after %d (want %d or %d)", uid, v, highest, highest, highest+1)
			}
		}
		frames := c.frames(t)
		lastFrame := frames[len(frames)-1]
		if lastFrame["name"] != "match_end" {
			t.Fatalf("socket %d: last frame = %v, want match_end", uid, lastFrame["name"])
		}
		if lastFrame["seq"] == nil {
			t.Fatalf("socket %d: match_end carries no seq", uid)
		}
	}
}

// TestStatePushAfterAction: every applied batch is followed by a per-viewer
// `state` frame whose data.seq is the batch's last seq and whose `you` is the
// socket's own view. After the match settles the room is open again with every
// lobby_ready flag cleared.
func TestStatePushAfterAction(t *testing.T) {
	e := newEnv(t, time.Minute)
	a, b := e.newPlayer(), e.newPlayer()
	ca, cb := e.mustDial(a), e.mustDial(b)
	readUntil(t, ca, "hello")
	readUntil(t, cb, "hello")
	matchID := e.startMatch(a, b)
	conns := map[int64]*websocket.Conn{a.uid: ca, b.uid: cb}
	caps := handshakeCaptured(t, matchID, conns)

	var cur int64
	for uid := range conns {
		if v, ok := e.viewOf(matchID, uid); ok && v.You.IsYourTurn {
			cur = uid
		}
	}
	if err := sendAction(conns[cur], matchID, "s1", "draw", nil, ""); err != nil {
		t.Fatalf("draw: %v", err)
	}
	for uid, c := range caps {
		st := c.waitFrame(t, "state push", 5*time.Second, func(f map[string]any) bool {
			return f["name"] == "state"
		})
		d := frameData(st)
		if d["seq"] != float64(2) {
			t.Fatalf("socket %d: state seq = %v, want 2", uid, d["seq"])
		}
		if _, ok := st["seq"]; ok {
			t.Fatalf("socket %d: state frame carries a top-level seq", uid)
		}
		you, _ := d["you"].(map[string]any)
		if you["user_id"] != float64(uid) {
			t.Fatalf("socket %d: state you.user_id = %v", uid, you["user_id"])
		}
		hand, _ := you["hand"].([]any)
		if want := 7; uid == cur {
			want = 8
			if len(hand) != want {
				t.Fatalf("drawer hand = %d, want %d", len(hand), want)
			}
		}
	}

	room, err := e.st.MembershipsForUser(e.ctx, a.uid)
	if err != nil || len(room) == 0 {
		t.Fatalf("membership: %v", err)
	}
	if err := e.st.SetReady(e.ctx, room[0].RoomID, a.uid, true); err != nil {
		t.Fatalf("SetReady: %v", err)
	}
	if err := e.hub.Forfeit(e.ctx, matchID, a.uid); err != nil {
		t.Fatalf("Forfeit: %v", err)
	}
	caps[b.uid].waitFrame(t, "match_end", 5*time.Second, func(f map[string]any) bool {
		return f["name"] == "match_end"
	})
	deadline := time.Now().Add(5 * time.Second)
	for {
		r, err := e.st.GetRoomByID(e.ctx, room[0].RoomID)
		ms, _ := e.st.ListMembers(e.ctx, room[0].RoomID)
		ready := false
		for _, m := range ms {
			ready = ready || m.LobbyReady
		}
		if err == nil && r.Status == "open" && !ready {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("room after settle: status=%v ready=%v err=%v, want open and no ready", r.Status, ready, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
