package web_test

import (
	"net/http"
	"testing"
)

// createRoom makes a room as the given host cookie and returns its code.
func (e *env) createRoom(hostCk string, stake int64) (code string, roomID string) {
	e.t.Helper()
	r := e.api(http.MethodPost, "/api/rooms", hostCk, map[string]any{"stake": stake})
	if r.status != http.StatusCreated {
		e.t.Fatalf("create room: status %d body %s", r.status, r.body)
	}
	m := r.json(e.t)
	c, _ := m["code"].(string)
	id, _ := m["room_id"].(string)
	return c, id
}

// TestRooms_CreateReturnsSixCharCode covers POST /api/rooms 201.
func TestRooms_CreateReturnsSixCharCode(t *testing.T) {
	e := newEnv(t)
	hostCk, host := e.register("host")
	r := e.api(http.MethodPost, "/api/rooms", hostCk, map[string]any{"stake": 50})
	if r.status != http.StatusCreated {
		t.Fatalf("status %d body %s", r.status, r.body)
	}
	m := r.json(t)
	code, _ := m["code"].(string)
	if len(code) != 6 {
		t.Errorf("code %q length %d want 6", code, len(code))
	}
	if num(t, m, "stake") != 50 || num(t, m, "host_user_id") != host {
		t.Errorf("stake/host = %v/%v", m["stake"], m["host_user_id"])
	}
}

// TestRooms_CreateInvalidStake covers 400 INVALID_STAKE outside {10,25,50,100,500}.
func TestRooms_CreateInvalidStake(t *testing.T) {
	e := newEnv(t)
	hostCk, _ := e.register("host")
	r := e.api(http.MethodPost, "/api/rooms", hostCk, map[string]any{"stake": 33})
	if r.status != http.StatusBadRequest || r.errCode(t) != "INVALID_STAKE" {
		t.Fatalf("status %d code %q want 400 INVALID_STAKE", r.status, r.errCode(t))
	}
}

// TestRooms_JoinReadyGetLeave covers join 200, ready 204, GET room 200, leave 204.
func TestRooms_JoinReadyGetLeave(t *testing.T) {
	e := newEnv(t)
	hostCk, _ := e.register("host")
	guestCk, _ := e.register("guest")
	code, _ := e.createRoom(hostCk, 10)

	j := e.api(http.MethodPost, "/api/rooms/"+code+"/join", guestCk, nil)
	if j.status != http.StatusOK {
		t.Fatalf("join status %d body %s", j.status, j.body)
	}
	if n := len(j.json(t)["players"].([]any)); n != 2 {
		t.Errorf("join players = %d want 2", n)
	}

	rd := e.api(http.MethodPost, "/api/rooms/"+code+"/ready", guestCk, map[string]any{"ready": true})
	if rd.status != http.StatusNoContent {
		t.Fatalf("ready status %d body %s", rd.status, rd.body)
	}

	g := e.api(http.MethodGet, "/api/rooms/"+code, hostCk, nil)
	if g.status != http.StatusOK {
		t.Fatalf("get room status %d body %s", g.status, g.body)
	}
	if g.json(t)["status"] != "open" {
		t.Errorf("room status = %v want open", g.json(t)["status"])
	}

	lv := e.api(http.MethodPost, "/api/rooms/"+code+"/leave", guestCk, nil)
	if lv.status != http.StatusNoContent {
		t.Fatalf("leave status %d body %s", lv.status, lv.body)
	}
}

// TestRooms_StakeHostOnly covers 403 NOT_HOST for a non-host, and 204 for the host.
func TestRooms_StakeHostOnly(t *testing.T) {
	e := newEnv(t)
	hostCk, _ := e.register("host")
	guestCk, _ := e.register("guest")
	code, _ := e.createRoom(hostCk, 10)
	e.api(http.MethodPost, "/api/rooms/"+code+"/join", guestCk, nil)

	bad := e.api(http.MethodPost, "/api/rooms/"+code+"/stake", guestCk, map[string]any{"stake": 25})
	if bad.status != http.StatusForbidden || bad.errCode(t) != "NOT_HOST" {
		t.Fatalf("guest stake: status %d code %q want 403 NOT_HOST", bad.status, bad.errCode(t))
	}
	ok := e.api(http.MethodPost, "/api/rooms/"+code+"/stake", hostCk, map[string]any{"stake": 25})
	if ok.status != http.StatusNoContent {
		t.Fatalf("host stake: status %d body %s", ok.status, ok.body)
	}
}

// TestRooms_KickHostOnly covers kick 204 by host and 403 NOT_HOST from a guest.
func TestRooms_KickHostOnly(t *testing.T) {
	e := newEnv(t)
	hostCk, _ := e.register("host")
	guestCk, guest := e.register("guest")
	code, _ := e.createRoom(hostCk, 10)
	e.api(http.MethodPost, "/api/rooms/"+code+"/join", guestCk, nil)

	bad := e.api(http.MethodPost, "/api/rooms/"+code+"/kick", guestCk, map[string]any{"user_id": guest})
	if bad.status != http.StatusForbidden || bad.errCode(t) != "NOT_HOST" {
		t.Fatalf("guest kick: status %d code %q want 403 NOT_HOST", bad.status, bad.errCode(t))
	}
	ok := e.api(http.MethodPost, "/api/rooms/"+code+"/kick", hostCk, map[string]any{"user_id": guest})
	if ok.status != http.StatusNoContent {
		t.Fatalf("host kick: status %d body %s", ok.status, ok.body)
	}
	if n := e.scalar(`SELECT count(*) FROM room_members rm JOIN rooms r USING (room_id) WHERE r.code = $1`, code); n != 1 {
		t.Errorf("members after kick = %d want 1 (host only)", n)
	}
}

// TestRooms_PlayRegression_OnlyPayingReadyPlayersSeated is the BUG 2 regression.
//
// Lobby: host (ready, funded), B (ready, funded), C (joined, NOT ready), D (ready,
// but balance 0 so the 50U bet fails). Expected: play seats only host and B.
// Checked two ways: the matchStub's recorded StartMatch, and match_players in the DB.
func TestRooms_PlayRegression_OnlyPayingReadyPlayersSeated(t *testing.T) {
	e := newEnv(t)
	hostCk, host := e.register("host")
	bCk, b := e.register("bplayer")
	cCk, c := e.register("cnotready")
	dCk, d := e.register("dbroke")
	code, _ := e.createRoom(hostCk, 50)

	for _, ck := range []string{bCk, cCk, dCk} {
		if r := e.api(http.MethodPost, "/api/rooms/"+code+"/join", ck, nil); r.status != http.StatusOK {
			t.Fatalf("join: status %d body %s", r.status, r.body)
		}
	}
	e.api(http.MethodPost, "/api/rooms/"+code+"/ready", hostCk, map[string]any{"ready": true})
	e.api(http.MethodPost, "/api/rooms/"+code+"/ready", bCk, map[string]any{"ready": true})
	e.api(http.MethodPost, "/api/rooms/"+code+"/ready", dCk, map[string]any{"ready": true})
	// C stays not-ready on purpose.

	// Drain D below the 50U stake so its bet transfer fails.
	e.exec(`UPDATE users SET balance = 0 WHERE user_id = $1`, d)

	r := e.api(http.MethodPost, "/api/rooms/"+code+"/play", hostCk, nil)
	if r.status != http.StatusOK {
		t.Fatalf("play status %d body %s", r.status, r.body)
	}
	matchID, _ := r.json(t)["match_id"].(string)

	// 1. What the match goroutine was asked to start.
	calls := e.stub.startCalls()
	if len(calls) != 1 || calls[0].matchID != matchID {
		t.Fatalf("StartMatch calls = %+v, want exactly one for match %s", calls, matchID)
	}

	// 2. Who is actually seated in match_players.
	rows, err := e.st.Pool.Query(t.Context(),
		`SELECT user_id FROM match_players WHERE match_id = $1::uuid ORDER BY user_id`, matchID)
	if err != nil {
		t.Fatalf("query match_players: %v", err)
	}
	var seated []int64
	for rows.Next() {
		var uid int64
		if err := rows.Scan(&uid); err != nil {
			t.Fatal(err)
		}
		seated = append(seated, uid)
	}
	rows.Close()

	want := map[int64]bool{host: true, b: true}
	if len(seated) != len(want) {
		t.Fatalf("seated = %v, want exactly host(%d) and b(%d)", seated, host, b)
	}
	for _, uid := range seated {
		if !want[uid] {
			t.Errorf("user %d seated but must not be: not ready or bet failed (c=%d d=%d)", uid, c, d)
		}
	}

	// 3. Only the two paying, ready players have a bet in the ledger.
	bets := e.scalar(`SELECT count(*) FROM transactions WHERE reason = 'BET' AND to_match_id = $1::uuid`, matchID)
	if bets != 2 {
		t.Errorf("BET transactions into match = %d want 2", bets)
	}
}

// TestRooms_PlayNotEnoughPlayers covers 400 NOT_ENOUGH_PLAYERS when only the host is ready.
func TestRooms_PlayNotEnoughPlayers(t *testing.T) {
	e := newEnv(t)
	hostCk, _ := e.register("host")
	code, _ := e.createRoom(hostCk, 10)
	e.api(http.MethodPost, "/api/rooms/"+code+"/ready", hostCk, map[string]any{"ready": true})
	r := e.api(http.MethodPost, "/api/rooms/"+code+"/play", hostCk, nil)
	if r.status != http.StatusBadRequest || r.errCode(t) != "NOT_ENOUGH_PLAYERS" {
		t.Fatalf("status %d code %q want 400 NOT_ENOUGH_PLAYERS", r.status, r.errCode(t))
	}
	if len(e.stub.startCalls()) != 0 {
		t.Error("StartMatch called despite NOT_ENOUGH_PLAYERS")
	}
}

// TestMatchState_NotLiveIsNotInMatch covers 403 NOT_IN_MATCH when the match is not live.
func TestMatchState_NotLiveIsNotInMatch(t *testing.T) {
	e := newEnv(t)
	ck, _ := e.register("alice")
	r := e.api(http.MethodGet, "/api/matches/00000000-0000-0000-0000-000000000001/state", ck, nil)
	if r.status != http.StatusForbidden || r.errCode(t) != "NOT_IN_MATCH" {
		t.Fatalf("status %d code %q want 403 NOT_IN_MATCH", r.status, r.errCode(t))
	}
}

// TestMatchState_LivePassesThroughView covers 200 with the match view unchanged.
func TestMatchState_LivePassesThroughView(t *testing.T) {
	e := newEnv(t)
	ck, uid := e.register("alice")
	e.stub.stateOK = true
	e.stub.stateView = map[string]any{"match_id": "m1", "seq": 42, "marker": "from-stub"}
	r := e.api(http.MethodGet, "/api/matches/m1/state", ck, nil)
	if r.status != http.StatusOK {
		t.Fatalf("status %d body %s", r.status, r.body)
	}
	m := r.json(t)
	if m["marker"] != "from-stub" || num(t, m, "seq") != 42 {
		t.Errorf("view not passed through: %v", m)
	}
	e.stub.mu.Lock()
	gotUID := e.stub.stateUID
	e.stub.mu.Unlock()
	if gotUID != uid {
		t.Errorf("State called with user %d want %d", gotUID, uid)
	}
}
