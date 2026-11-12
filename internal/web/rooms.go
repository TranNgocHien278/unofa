package web

import (
	"context"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/unofa/unofa/internal/errs"
	"github.com/unofa/unofa/internal/ledger"
	"github.com/unofa/unofa/internal/store"
)

// createRoom handles POST /api/rooms.
func (d Deps) createRoom(w http.ResponseWriter, r *http.Request) error {
	uid, err := d.userID(r)
	if err != nil {
		return err
	}
	if err := d.Limiter.Allow(r.Context(), "room_create", itoa(uid),
		d.Cfg.Rate.RoomCreatePerUser.Limit, d.Cfg.Rate.RoomCreatePerUser.Period); err != nil {
		return err
	}

	var body struct {
		Stake int64 `json:"stake"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		return err
	}
	if !d.Cfg.StakeAllowed(body.Stake) {
		return errs.New(errs.InvalidStake)
	}

	room, err := d.Store.CreateRoom(r.Context(), uid, body.Stake)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, d.now(), map[string]any{
		"room_id":      room.RoomID,
		"code":         room.Code,
		"stake":        room.Stake,
		"host_user_id": room.HostUserID,
	})
	return nil
}

// joinRoom handles POST /api/rooms/{code}/join.
func (d Deps) joinRoom(w http.ResponseWriter, r *http.Request) error {
	uid, err := d.userID(r)
	if err != nil {
		return err
	}
	if err := d.Limiter.Allow(r.Context(), "room_join", itoa(uid),
		d.Cfg.Rate.RoomJoinPerUser.Limit, d.Cfg.Rate.RoomJoinPerUser.Period); err != nil {
		return err
	}
	code := normalizeCode(pathValue(r, "code"))
	room, err := d.Store.GetByCode(r.Context(), code)
	if err != nil {
		return err
	}
	if _, err := d.Store.AddMember(r.Context(), room.RoomID, uid); err != nil {
		return err
	}
	members, err := d.Store.ListMembers(r.Context(), room.RoomID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, d.now(), map[string]any{
		"room_id": room.RoomID,
		"code":    room.Code,
		"stake":   room.Stake,
		"players": memberRows(members),
	})
	return nil
}

// leaveRoom handles POST /api/rooms/{code}/leave. Leaving mid-match forfeits
// immediately (D4), then the room membership is dropped.
func (d Deps) leaveRoom(w http.ResponseWriter, r *http.Request) error {
	uid, err := d.userID(r)
	if err != nil {
		return err
	}
	code := normalizeCode(pathValue(r, "code"))
	room, err := d.Store.GetByCode(r.Context(), code)
	if err != nil {
		return err
	}

	// A running match means the seat is forfeited for good, not just left.
	if match, merr := d.Store.MatchForUser(r.Context(), uid); merr == nil && match != nil &&
		match.RoomID == room.RoomID && match.Status == "running" && d.Match != nil {
		if ferr := d.Match.Forfeit(r.Context(), match.MatchID, uid); ferr != nil {
			return ferr
		}
	}

	remaining, err := d.Store.RemoveMember(r.Context(), room.RoomID, uid)
	if err != nil {
		return err
	}
	if remaining == 0 {
		_ = d.Store.DeleteRoom(r.Context(), room.RoomID)
		w.WriteHeader(http.StatusNoContent)
		return nil
	}
	if room.HostUserID == uid {
		members, lerr := d.Store.ListMembers(r.Context(), room.RoomID)
		if lerr == nil && len(members) > 0 {
			_ = d.Store.SetHost(r.Context(), room.RoomID, members[0].UserID)
		}
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// setReady handles POST /api/rooms/{code}/ready.
func (d Deps) setReady(w http.ResponseWriter, r *http.Request) error {
	uid, err := d.userID(r)
	if err != nil {
		return err
	}
	var body struct {
		Ready bool `json:"ready"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		return err
	}
	room, err := d.requireOpenRoom(r, pathValue(r, "code"))
	if err != nil {
		return err
	}
	if err := d.Store.SetReady(r.Context(), room.RoomID, uid, body.Ready); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// setStake handles POST /api/rooms/{code}/stake (host only).
func (d Deps) setStake(w http.ResponseWriter, r *http.Request) error {
	uid, err := d.userID(r)
	if err != nil {
		return err
	}
	var body struct {
		Stake int64 `json:"stake"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		return err
	}
	room, err := d.requireOpenRoom(r, pathValue(r, "code"))
	if err != nil {
		return err
	}
	if room.HostUserID != uid {
		return errs.New(errs.NotHost)
	}
	if !d.Cfg.StakeAllowed(body.Stake) {
		return errs.New(errs.InvalidStake)
	}
	if err := d.Store.SetStake(r.Context(), room.RoomID, body.Stake); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// kick handles POST /api/rooms/{code}/kick (host only, open room only, D11).
func (d Deps) kick(w http.ResponseWriter, r *http.Request) error {
	uid, err := d.userID(r)
	if err != nil {
		return err
	}
	var body struct {
		UserID int64 `json:"user_id"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		return err
	}
	room, err := d.requireOpenRoom(r, pathValue(r, "code"))
	if err != nil {
		return err
	}
	if room.HostUserID != uid {
		return errs.New(errs.NotHost)
	}
	// The host cannot kick themselves; leaving is the endpoint for that.
	if body.UserID == uid {
		return errs.New(errs.NotHost)
	}
	remaining, err := d.Store.RemoveMember(r.Context(), room.RoomID, body.UserID)
	if err != nil {
		return err
	}
	if remaining == 0 {
		_ = d.Store.DeleteRoom(r.Context(), room.RoomID)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// getRoom handles GET /api/rooms/{code}.
func (d Deps) getRoom(w http.ResponseWriter, r *http.Request) error {
	if _, err := d.userID(r); err != nil {
		return err
	}
	room, err := d.Store.GetByCode(r.Context(), normalizeCode(pathValue(r, "code")))
	if err != nil {
		return err
	}
	members, err := d.Store.ListMembers(r.Context(), room.RoomID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, d.now(), map[string]any{
		"room_id":      room.RoomID,
		"code":         room.Code,
		"stake":        room.Stake,
		"status":       room.Status,
		"host_user_id": room.HostUserID,
		"players":      memberRows(members),
	})
	return nil
}

// play handles POST /api/rooms/{code}/play: lock the room, bet every seat, create
// the match row and hand it to the match goroutine.
func (d Deps) play(w http.ResponseWriter, r *http.Request) error {
	uid, err := d.userID(r)
	if err != nil {
		return err
	}
	if err := d.Limiter.Allow(r.Context(), "room_play", itoa(uid),
		d.Cfg.Rate.RoomCreatePerUser.Limit, d.Cfg.Rate.RoomCreatePerUser.Period); err != nil {
		return err
	}
	room, err := d.requireOpenRoom(r, pathValue(r, "code"))
	if err != nil {
		return err
	}
	if room.HostUserID != uid {
		return errs.New(errs.NotHost)
	}

	now := d.now()
	var matchID string
	txErr := pgx.BeginFunc(r.Context(), d.Store.Pool, func(tx pgx.Tx) error {
		st := d.Store.InTx(tx)

		// Lock the room for the handshake so a late joiner cannot slip in.
		locked, err := st.LockRoomForPlay(r.Context(), room.RoomID)
		if err != nil {
			return err
		}
		ready := 0
		for _, m := range locked {
			if m.LobbyReady {
				ready++
			}
		}
		if ready < d.Cfg.Game.MinPlayers {
			return errs.New(errs.NotEnoughPlayers)
		}

		match, err := st.CreateMatch(r.Context(), room.RoomID, room.Stake)
		if err != nil {
			return err
		}
		matchID = match.MatchID

		seats := make([]int, 0, len(locked))
		userIDs := make([]int64, 0, len(locked))
		for _, m := range locked {
			userIDs = append(userIDs, m.UserID)
			seats = append(seats, m.Seat)
			// The host's own balance is checked here; anyone short is dropped in
			// handshake step 5, not turned into a host error (04 §play).
			if err := ledger.Transfer(r.Context(), tx, ledger.User(m.UserID), ledger.Match(match.MatchID),
				room.Stake, ledger.ReasonBet, "match:"+match.MatchID+":bet:"+itoa(m.UserID)); err != nil {
				if m.UserID == room.HostUserID {
					return errs.New(errs.InsufficientBalance)
				}
				continue
			}
			if err := st.InsertSnapshot(r.Context(), m.UserID, 0, now); err != nil {
				return err
			}
		}
		return st.AddMatchPlayers(r.Context(), match.MatchID, userIDs, seats)
	})
	if txErr != nil {
		// Unlock the room if the transaction rolled back after locking it.
		_ = d.Store.SetStatus(r.Context(), room.RoomID, "open")
		return txErr
	}

	if d.Match != nil {
		if err := d.Match.StartMatch(r.Context(), matchID, room.RoomID); err != nil {
			return errs.New(errs.Internal)
		}
	}

	writeJSON(w, http.StatusOK, now, map[string]any{"match_id": matchID})
	return nil
}

// matchState handles GET /api/matches/{id}/state (snapshot for reconnect / F5).
func (d Deps) matchState(w http.ResponseWriter, r *http.Request) error {
	uid, err := d.userID(r)
	if err != nil {
		return err
	}
	if err := d.Limiter.Allow(r.Context(), "match_state", itoa(uid),
		d.Cfg.Rate.MatchStatePerUser.Limit, d.Cfg.Rate.MatchStatePerUser.Period); err != nil {
		return err
	}
	matchID := pathValue(r, "id")
	if d.Match == nil {
		return errs.New(errs.ServiceUnavailable)
	}
	view, ok := d.Match.State(matchID, uid)
	if !ok {
		return errs.New(errs.NotInMatch)
	}
	writeRawJSON(w, http.StatusOK, view)
	return nil
}

// --- helpers ---

// requireOpenRoom loads a room and rejects any status other than open.
func (d Deps) requireOpenRoom(r *http.Request, code string) (*store.Room, error) {
	room, err := d.Store.GetByCode(r.Context(), normalizeCode(code))
	if err != nil {
		return nil, err
	}
	if room.Status != "open" {
		return nil, errs.New(errs.RoomLocked)
	}
	return room, nil
}

func memberRows(members []store.Member) []map[string]any {
	out := make([]map[string]any, 0, len(members))
	for _, m := range members {
		out = append(out, map[string]any{
			"user_id":     m.UserID,
			"username":    m.Username,
			"seat":        m.Seat,
			"lobby_ready": m.LobbyReady,
			"balance":     m.Balance,
		})
	}
	return out
}

// normalizeCode upper-cases a room code so a mobile keyboard's autocorrect does
// not turn a valid invite into ROOM_NOT_FOUND.
func normalizeCode(code string) string {
	out := make([]rune, 0, len(code))
	for _, c := range code {
		if c >= 'a' && c <= 'z' {
			c -= 'a' - 'A'
		}
		out = append(out, c)
	}
	return string(out)
}

var _ = context.Background
var _ = time.Now
