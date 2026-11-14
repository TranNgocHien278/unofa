// Package match owns the per-match goroutine, the WebSocket hub, and the
// reconnect grace clock. One goroutine per match receives all actions, timer
// ticks, and socket events over a single channel (05 §Thứ tự xử lý).
package match

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/unofa/unofa/internal/clock"
	"github.com/unofa/unofa/internal/config"
	"github.com/unofa/unofa/internal/errs"
	"github.com/unofa/unofa/internal/game"
	"github.com/unofa/unofa/internal/ledger"
	"github.com/unofa/unofa/internal/ratelimit"
	"github.com/unofa/unofa/internal/store"
)

// Hub is the top-level registry of live matches and open WebSocket connections.
// It satisfies internal/web.MatchService.
type Hub struct {
	cfg     *config.Config
	pool    *pgxpool.Pool
	st      *store.Store
	clk     clock.Clock
	log     *slog.Logger
	limiter *ratelimit.Limiter

	mu      sync.Mutex
	matches map[string]*matchRun // matchID → goroutine
	socks   map[string]*wsConn   // sessionToken → open socket
}

// matchRun is one live match: the game state and the channel everything for
// this match funnels through (05 §Thứ tự xử lý).
type matchRun struct {
	matchID string
	roomID  string
	inits   []game.PlayerInit
	pot     int64
	state   *game.State // nil until the handshake finishes
	grace   map[int64]*graceTimer
	ch      chan any
	done    chan struct{}
}

type graceTimer struct{ t *time.Timer }

// wsConn is a registered WebSocket. matchID is guarded by Hub.mu. Writes go
// through write or writeLocked, which hold wmu because gorilla forbids concurrent
// writers. shut uses WriteControl, which gorilla allows concurrently.
type wsConn struct {
	token   string
	userID  int64
	matchID string
	conn    *websocket.Conn
	wmu     sync.Mutex
}

func (c *wsConn) write(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	c.wmu.Lock()
	defer c.wmu.Unlock()
	return c.writeLocked(b)
}

// writeLocked sends already-marshaled bytes. The caller holds wmu.
func (c *wsConn) writeLocked(b []byte) error {
	c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return c.conn.WriteMessage(websocket.TextMessage, b)
}

func (c *wsConn) shut(code int, reason string) {
	c.conn.WriteControl(websocket.CloseMessage,
		websocket.FormatCloseMessage(code, reason), time.Now().Add(time.Second))
	c.conn.Close()
}

// New creates a Hub.
func New(cfg *config.Config, pool *pgxpool.Pool, st *store.Store, clk clock.Clock, log *slog.Logger) *Hub {
	return &Hub{
		cfg:     cfg,
		pool:    pool,
		st:      st,
		clk:     clk,
		log:     log,
		matches: map[string]*matchRun{},
		socks:   map[string]*wsConn{},
	}
}

// --- web.MatchService ---

// StartMatch registers the match, sends ready_check, and launches its goroutine.
// The game state is built after the handshake so the first turn clock starts at match_start.
func (h *Hub) StartMatch(ctx context.Context, matchID, roomID string) error {
	players, err := h.st.GetMatchPlayers(ctx, matchID)
	if err != nil {
		return err
	}
	m, err := h.st.GetMatch(ctx, matchID)
	if err != nil {
		return err
	}

	inits := make([]game.PlayerInit, 0, len(players))
	for _, p := range players {
		inits = append(inits, game.PlayerInit{UserID: p.UserID, Seat: p.Seat})
	}
	run := &matchRun{
		matchID: matchID,
		roomID:  roomID,
		inits:   inits,
		pot:     int64(len(inits)) * m.Stake,
		grace:   map[int64]*graceTimer{},
		ch:      make(chan any, 512),
		done:    make(chan struct{}),
	}

	h.mu.Lock()
	h.matches[matchID] = run
	var targets []*wsConn
	for _, c := range h.socks {
		if hasUser(inits, c.userID) {
			c.matchID = matchID
			targets = append(targets, c)
		}
	}
	h.mu.Unlock()

	check := map[string]any{"match_id": matchID}
	for _, c := range targets {
		h.deliver(c, h.frame(game.Event{Name: "ready_check"}, check))
	}

	go h.runMatch(run)
	return nil
}

func hasUser(inits []game.PlayerInit, uid int64) bool {
	for _, in := range inits {
		if in.UserID == uid {
			return true
		}
	}
	return false
}

// Forfeit sends a forfeit message to the match goroutine.
func (h *Hub) Forfeit(_ context.Context, matchID string, userID int64) error {
	h.mu.Lock()
	run, ok := h.matches[matchID]
	h.mu.Unlock()
	if !ok {
		return nil
	}
	select {
	case run.ch <- forfeitMsg{userID: userID, now: h.clk.Now()}:
	default:
	}
	return nil
}

// State returns the per-viewer snapshot for GET /api/matches/{id}/state.
func (h *Hub) State(matchID string, userID int64) (any, bool) {
	h.mu.Lock()
	run, ok := h.matches[matchID]
	h.mu.Unlock()
	if !ok {
		return nil, false
	}
	resp := make(chan any, 1)
	select {
	case run.ch <- snapshotReq{userID: userID, resp: resp}:
	default:
		return nil, false
	}
	select {
	case v := <-resp:
		return v, v != nil
	case <-time.After(2 * time.Second):
		return nil, false
	}
}

// Snapshot is State with an error for a match that is not live.
func (h *Hub) Snapshot(matchID string, userID int64) (any, error) {
	v, ok := h.State(matchID, userID)
	if !ok {
		return nil, errs.New(errs.NotInMatch)
	}
	return v, nil
}

// CloseSession closes the WebSocket bound to a session key.
func (h *Hub) CloseSession(token, reason string) {
	h.mu.Lock()
	conn, ok := h.socks[token]
	h.mu.Unlock()
	if ok {
		conn.shut(1000, reason)
	}
}

// --- match goroutine messages ---

type (
	actionMsg struct {
		userID int64
		action game.Action
		now    time.Time
	}
	forfeitMsg struct {
		userID int64
		now    time.Time
	}
	disconnectMsg struct {
		userID int64
		now    time.Time
	}
	reconnectMsg struct {
		userID int64
		now    time.Time
	}
	graceMsg struct {
		userID int64
		gt     *graceTimer
	}
	readyAckMsg struct{ userID int64 }
	snapshotReq struct {
		userID int64
		resp   chan<- any
	}
)

func (h *Hub) runMatch(run *matchRun) {
	defer close(run.done)
	defer func() {
		if r := recover(); r != nil {
			h.log.Error("match panic",
				slog.String("match_id", run.matchID),
				slog.Any("err", r))
		}
		for _, g := range run.grace {
			g.t.Stop()
		}
		h.mu.Lock()
		delete(h.matches, run.matchID)
		for _, c := range h.socks {
			if c.matchID == run.matchID {
				c.matchID = ""
			}
		}
		h.mu.Unlock()
	}()

	if !h.handshake(run) {
		return
	}

	timer := time.NewTimer(run.state.TurnDeadline().Sub(h.clk.Now()))
	defer timer.Stop()

	for {
		select {
		case <-timer.C:
			evts := run.state.Tick(h.clk.Now())
			h.broadcast(run, evts)
			h.resetTimer(run, timer)
			if run.state.Phase() == game.Ended {
				h.settleMatch(run)
				return
			}

		case msg, ok := <-run.ch:
			if !ok {
				return
			}
			switch m := msg.(type) {
			case actionMsg:
				evts, err := run.state.Apply(m.action, m.userID, m.now)
				if err != nil {
					h.sendMatchError(run, m.userID, m.action.Ref, err)
					continue
				}
				h.broadcast(run, evts)
				h.resetTimer(run, timer)
				if run.state.Phase() == game.Ended {
					h.settleMatch(run)
					return
				}

			case forfeitMsg:
				h.stopGrace(run, m.userID)
				if h.forfeit(run, m.userID, m.now) {
					h.settleMatch(run)
					return
				}

			case graceMsg:
				if run.grace[m.userID] != m.gt {
					continue
				}
				delete(run.grace, m.userID)
				if h.forfeit(run, m.userID, h.clk.Now()) {
					h.settleMatch(run)
					return
				}

			case disconnectMsg:
				evts := run.state.Disconnect(m.userID, m.now)
				if len(evts) == 0 {
					continue
				}
				h.broadcast(run, evts)
				h.updatePlayerStatus(run, m.userID, "disconnected", m.now)
				h.armGrace(run, m.userID)

			case reconnectMsg:
				h.stopGrace(run, m.userID)
				evts := run.state.Reconnect(m.userID, m.now)
				if len(evts) == 0 {
					continue
				}
				h.broadcast(run, evts)
				h.updatePlayerStatus(run, m.userID, "connected", m.now)

			case snapshotReq:
				m.resp <- run.state.View(m.userID, h.clk.Now())
			}
		}
	}
}

// handshake waits for every player's ready_ack or ReadyTimeout (ready_check was
// sent in StartMatch), then deals and sends each player their match_start view.
// It returns false when the match cannot start.
func (h *Hub) handshake(run *matchRun) bool {
	pending := map[int64]bool{}
	for _, in := range run.inits {
		pending[in.UserID] = true
	}
	timeout := time.NewTimer(h.cfg.Game.ReadyTimeout)
	defer timeout.Stop()

	for len(pending) > 0 {
		select {
		case <-timeout.C:
			pending = nil
		case msg := <-run.ch:
			switch m := msg.(type) {
			case readyAckMsg:
				delete(pending, m.userID)
			case snapshotReq:
				m.resp <- nil
			}
		}
	}

	now := h.clk.Now()
	st, err := game.NewMatch(run.matchID, run.inits, game.Config{
		HandSize:    h.cfg.Game.HandSize,
		TurnTimeout: h.cfg.Game.TurnTimeout,
		MinPlayers:  h.cfg.Game.MinPlayers,
		MaxPlayers:  h.cfg.Game.MaxPlayers,
	}, rand.Reader, now)
	if err != nil {
		h.log.Error("new match failed", slog.String("match_id", run.matchID), slog.Any("err", err))
		return false
	}
	st.SetPot(run.pot)
	run.state = st
	for _, in := range run.inits {
		h.sendPrivate(run, in.UserID, game.Event{Name: "match_start", Seq: st.Seq()},
			st.MatchStartView(in.UserID, now))
	}
	return true
}

// forfeit removes a player and reports whether the match ended.
func (h *Hub) forfeit(run *matchRun, uid int64, now time.Time) bool {
	h.broadcast(run, run.state.Forfeit(uid, now))
	h.updatePlayerStatus(run, uid, "forfeit", now)
	return run.state.Phase() == game.Ended
}

func (h *Hub) stopGrace(run *matchRun, uid int64) {
	if g, ok := run.grace[uid]; ok {
		g.t.Stop()
		delete(run.grace, uid)
	}
}

// armGrace starts the reconnect grace clock. Expiry sends a graceMsg tied to this
// timer, so a stale expiry after a reconnect is ignored.
func (h *Hub) armGrace(run *matchRun, uid int64) {
	g := &graceTimer{}
	g.t = time.AfterFunc(h.cfg.Game.ReconnectGrace, func() {
		h.enqueue(run, graceMsg{userID: uid, gt: g})
	})
	if old, ok := run.grace[uid]; ok {
		old.t.Stop()
	}
	run.grace[uid] = g
}

func (h *Hub) enqueue(run *matchRun, msg any) {
	select {
	case run.ch <- msg:
	case <-run.done:
	}
}

func (h *Hub) resetTimer(run *matchRun, t *time.Timer) {
	if !t.Stop() {
		select {
		case <-t.C:
		default:
		}
	}
	d := run.state.TurnDeadline().Sub(h.clk.Now())
	if d < time.Millisecond {
		d = time.Millisecond
	}
	t.Reset(d)
}

func (h *Hub) settleMatch(run *matchRun) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	winnerID, hasWinner := run.state.Winner()
	now := h.clk.Now()

	if hasWinner && winnerID != 0 && run.state.Pot() > 0 {
		if err := pgx.BeginFunc(ctx, h.pool, func(tx pgx.Tx) error {
			return ledger.Transfer(ctx, tx, ledger.Match(run.matchID), ledger.User(winnerID),
				run.state.Pot(), ledger.ReasonPayout,
				"match:"+run.matchID+":payout")
		}); err != nil {
			h.log.Error("payout failed",
				slog.String("match_id", run.matchID), slog.Any("err", err))
		}
		if err := h.st.EndMatch(ctx, run.matchID, winnerID, now); err != nil {
			h.log.Error("end match failed",
				slog.String("match_id", run.matchID), slog.Any("err", err))
		}
	} else if run.state.Pot() > 0 {
		// Burn when there is no winner or pot comes from an aborted game.
		if err := pgx.BeginFunc(ctx, h.pool, func(tx pgx.Tx) error {
			return ledger.Transfer(ctx, tx, ledger.Match(run.matchID), ledger.System,
				run.state.Pot(), ledger.ReasonBurn,
				"match:"+run.matchID+":burn")
		}); err != nil {
			h.log.Error("burn failed",
				slog.String("match_id", run.matchID), slog.Any("err", err))
		}
	}
	_ = h.st.SetStatus(ctx, run.roomID, "in_match")
}

// --- fan-out ---

// conns returns the registered sockets matching keep. Hub.mu is released before
// the caller writes, so a slow socket never holds the lock.
func (h *Hub) conns(keep func(*wsConn) bool) []*wsConn {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []*wsConn
	for _, c := range h.socks {
		if keep(c) {
			out = append(out, c)
		}
	}
	return out
}

func (h *Hub) frame(e game.Event, data any) map[string]any {
	f := map[string]any{
		"type":       "event",
		"name":       e.Name,
		"server_now": h.clk.Now().UTC().Format(time.RFC3339),
		"data":       data,
	}
	if e.Seq > 0 {
		f["seq"] = e.Seq
	}
	return f
}

func (h *Hub) broadcast(run *matchRun, evts []game.Event) {
	for _, e := range evts {
		for uid, payload := range e.Private {
			h.sendPrivate(run, uid, e, payload)
		}
		h.broadcastPublic(run, e)
	}
}

// sendMatchError delivers a rule rejection to the one socket that sent the action.
func (h *Hub) sendMatchError(run *matchRun, uid int64, ref string, err error) {
	f := map[string]any{"type": "error", "code": string(errs.As(err).Code), "request_id": ref}
	for _, c := range h.conns(func(c *wsConn) bool { return c.matchID == run.matchID && c.userID == uid }) {
		h.deliver(c, f)
	}
}

// broadcastPublic sends the public part of an event to every socket in the match.
func (h *Hub) broadcastPublic(run *matchRun, e game.Event) {
	f := h.frame(e, e.Data)
	for _, c := range h.conns(func(c *wsConn) bool { return c.matchID == run.matchID }) {
		h.deliver(c, f)
	}
}

// sendPrivate sends a per-viewer payload to one user's sockets in the match.
func (h *Hub) sendPrivate(run *matchRun, uid int64, e game.Event, payload any) {
	f := h.frame(e, payload)
	for _, c := range h.conns(func(c *wsConn) bool { return c.matchID == run.matchID && c.userID == uid }) {
		h.deliver(c, f)
	}
}

// deliver writes one frame. A failed write drops that socket; the match continues.
func (h *Hub) deliver(c *wsConn, v any) {
	if err := c.write(v); err != nil {
		h.log.Warn("ws write failed, dropping socket",
			slog.Int64("user_id", c.userID), slog.Any("err", err))
		c.shut(1011, "write failed")
		h.dropConn(c)
	}
}

// detach unregisters c. It reports whether c was the session's current socket
// and the match it was attached to.
func (h *Hub) detach(c *wsConn) (bool, string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.socks[c.token] != c {
		return false, ""
	}
	delete(h.socks, c.token)
	return true, c.matchID
}

// dropConn unregisters c, forwards a disconnect to a running match, and closes the
// socket. A replaced socket is not forwarded: its session already has a successor.
func (h *Hub) dropConn(c *wsConn) {
	if current, matchID := h.detach(c); current && matchID != "" {
		h.mu.Lock()
		run, ok := h.matches[matchID]
		h.mu.Unlock()
		if ok {
			go h.enqueue(run, disconnectMsg{userID: c.userID, now: h.clk.Now()})
		}
	}
	c.conn.Close()
}

func (h *Hub) updatePlayerStatus(run *matchRun, userID int64, status string, now time.Time) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := h.st.SetPlayerStatus(ctx, run.matchID, userID, status, now); err != nil {
		h.log.Error("set player status failed",
			slog.String("match_id", run.matchID), slog.Any("err", err))
	}
}
