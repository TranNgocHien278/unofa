package match

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/unofa/unofa/internal/errs"
	"github.com/unofa/unofa/internal/game"
	"github.com/unofa/unofa/internal/ratelimit"
	"github.com/unofa/unofa/internal/session"
)

// SetLimiter is called by cmd/unofa. It lives here so ServeWS can rate-limit
// per-user WS actions without importing web.
func (h *Hub) SetLimiter(l *ratelimit.Limiter) { h.limiter = l }

// clientFrame is the only client → server shape (05 §Khung message).
type clientFrame struct {
	Type        string    `json:"type"`
	Ref         string    `json:"ref"`
	MatchID     string    `json:"match_id"`
	Action      string    `json:"action"`
	Card        *wireCard `json:"card"`
	ChosenColor string    `json:"chosen_color"`
}

type wireCard struct {
	Color string `json:"color"`
	Rank  string `json:"rank"`
}

// ServeWS upgrades the request to a WebSocket and registers it with the hub. The
// Origin check runs in the upgrader (403 on mismatch). One socket per session: a
// new connection replaces the old one with close 1000 SESSION_REPLACED.
func (h *Hub) ServeWS(sess *session.Manager, allowedOrigin string) http.HandlerFunc {
	up := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return strings.EqualFold(r.Header.Get("Origin"), allowedOrigin)
		},
		ReadBufferSize:  512,
		WriteBufferSize: 4096,
	}
	return func(w http.ResponseWriter, r *http.Request) {
		uid, err := sess.GetUserID(r.Context(), r)
		if err != nil {
			http.Error(w, `{"error":"UNAUTHENTICATED"}`, http.StatusUnauthorized)
			return
		}
		token, _ := sess.Get(r.Context(), r)

		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		conn.SetReadLimit(int64(h.cfg.Game.MaxMessageBytes))
		c := &wsConn{token: token, userID: uid, conn: conn}

		var matchID string
		if m, _ := h.st.MatchForUser(r.Context(), uid); m != nil {
			matchID = m.MatchID
		}
		memberships, _ := h.st.MembershipsForUser(r.Context(), uid)
		var roomInfo any
		if len(memberships) > 0 {
			roomInfo = map[string]any{"room_id": memberships[0].RoomID, "seat": memberships[0].Seat}
		}

		// Hold wmu across registration and hello so hello is the first frame on this socket.
		c.wmu.Lock()
		h.mu.Lock()
		old := h.socks[token]
		h.socks[token] = c
		run, live := h.matches[matchID]
		if live {
			c.matchID = matchID
		} else {
			matchID = ""
		}
		h.mu.Unlock()
		if old != nil {
			old.shut(1000, "SESSION_REPLACED")
		}
		hello := map[string]any{
			"type":       "event",
			"name":       "hello",
			"server_now": h.clk.Now().UTC().Format(time.RFC3339),
			"data": map[string]any{
				"user_id":  uid,
				"room":     roomInfo,
				"match_id": matchIDOrNil(matchID),
			},
		}
		b, _ := json.Marshal(hello)
		err = c.writeLocked(b)
		c.wmu.Unlock()
		if err != nil {
			h.dropConn(c)
			return
		}
		defer h.dropConn(c)

		if live {
			h.enqueue(run, reconnectMsg{userID: uid, now: h.clk.Now()})
		}

		h.readLoop(r, c)
	}
}

// readLoop handles inbound frames until the socket closes. A read error only
// ends the loop; the deferred dropConn unregisters and forwards the disconnect.
func (h *Hub) readLoop(r *http.Request, c *wsConn) {
	for {
		mt, msg, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
		if mt != websocket.TextMessage {
			continue
		}

		var f clientFrame
		if err := json.Unmarshal(msg, &f); err != nil {
			h.sendError(c, "", errs.New(errs.InvalidMove))
			continue
		}

		switch f.Type {
		case "action":
			if !h.handleAction(r, c, f) {
				return
			}
		case "ready_ack":
			h.handleReadyAck(c, f)
		}
	}
}

// handleAction forwards a player action to the match goroutine. It returns false
// when the socket was closed for repeated rate-limit violations.
func (h *Hub) handleAction(r *http.Request, c *wsConn, f clientFrame) bool {
	key := strconv.FormatInt(c.userID, 10)
	if h.limiter != nil {
		win := h.cfg.Rate.WSActionsPerUser
		if err := h.limiter.Allow(r.Context(), "ws_action", key, win.Limit, win.Period); err != nil {
			h.sendError(c, f.Ref, errs.New(errs.RateLimited))
			return h.countViolation(r, c, key)
		}
	}

	h.mu.Lock()
	run, ok := h.matches[f.MatchID]
	h.mu.Unlock()
	if !ok {
		h.sendError(c, f.Ref, errs.New(errs.NotInMatch))
		return true
	}

	var wc *game.WireCard
	if f.Card != nil {
		wc = &game.WireCard{Color: f.Card.Color, Rank: f.Card.Rank}
	}
	select {
	case run.ch <- actionMsg{
		userID: c.userID,
		now:    h.clk.Now(),
		action: game.Action{Ref: f.Ref, MatchID: f.MatchID, Action: f.Action, Card: wc, ChosenColor: f.ChosenColor},
	}:
	default:
		h.sendError(c, f.Ref, errs.New(errs.ServiceUnavailable))
	}
	return true
}

// countViolation closes the socket with 1008 when the per-connection violation
// budget is spent (05 §Đóng kết nối).
func (h *Hub) countViolation(r *http.Request, c *wsConn, key string) bool {
	if h.limiter == nil {
		return true
	}
	win := h.cfg.Rate.WSMessagesPerConn
	if err := h.limiter.Allow(r.Context(), "ws_violation", key, win.Limit, win.Period); err != nil {
		c.shut(1008, "rate limit")
		return false
	}
	return true
}

func (h *Hub) handleReadyAck(c *wsConn, f clientFrame) {
	h.mu.Lock()
	run, ok := h.matches[f.MatchID]
	h.mu.Unlock()
	if !ok {
		h.sendError(c, f.Ref, errs.New(errs.NotInMatch))
		return
	}
	select {
	case run.ch <- readyAckMsg{userID: c.userID}:
	default:
		h.sendError(c, f.Ref, errs.New(errs.ServiceUnavailable))
	}
}

// sendError writes an error frame to one socket.
func (h *Hub) sendError(c *wsConn, ref string, e *errs.Error) {
	h.deliver(c, map[string]any{"type": "error", "code": string(e.Code), "request_id": ref})
}

func matchIDOrNil(s string) any {
	if s == "" {
		return nil
	}
	return s
}
