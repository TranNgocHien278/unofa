package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/unofa/unofa/internal/errs"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("UNOFA_TEST_DSN")
	if dsn == "" {
		t.Skip("UNOFA_TEST_DSN unset")
	}
	s, err := New(context.Background(), dsn, 4, 1, 5*time.Second)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(s.Close)
	return s
}

var userSeq atomic.Int64

func newUser(t *testing.T, s *Store) *User {
	t.Helper()
	name := fmt.Sprintf("t%d_%d", time.Now().UnixNano(), userSeq.Add(1))
	u, err := s.CreateUser(context.Background(), name, "hash", 1, 0)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	return u
}

func wantCode(t *testing.T, err error, code errs.Code) {
	t.Helper()
	var e *errs.Error
	if !errors.As(err, &e) || e.Code != code {
		t.Fatalf("want %s, got %v", code, err)
	}
}

func TestRoomCodeShape(t *testing.T) {
	re := regexp.MustCompile(`^[A-HJ-NP-Z2-9]{6}$`)
	seen := map[byte]bool{}
	for range 5000 {
		c := randomCode()
		if !re.MatchString(c) {
			t.Fatalf("bad code %q", c)
		}
		for i := range len(c) {
			seen[c[i]] = true
		}
	}
	if len(seen) != len(codeAlphabet) {
		t.Fatalf("alphabet coverage %d/%d", len(seen), len(codeAlphabet))
	}
}

func TestReservedUsernames(t *testing.T) {
	for _, n := range []string{"system", "SyStEm", " System ", "deleted_7", "DELETED_x", ""} {
		if _, err := NormalizeUsername(n); err == nil {
			t.Fatalf("%q accepted", n)
		}
	}
	if n, err := NormalizeUsername("Alice"); err != nil || n != "alice" {
		t.Fatalf("Alice -> %q %v", n, err)
	}
}

func TestUsers(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	u := newUser(t, s)
	if u.UserID == 0 {
		t.Fatal("got id 0")
	}
	_, err := s.CreateUser(ctx, strings.ToUpper(u.Username), "h", 1, 0)
	wantCode(t, err, errs.UsernameTaken)
	_, err = s.CreateUser(ctx, "System", "h", 1, 0)
	wantCode(t, err, errs.UsernameInvalid)

	got, err := s.GetByUsername(ctx, strings.ToUpper(u.Username))
	if err != nil || got.UserID != u.UserID {
		t.Fatalf("GetByUsername: %v %v", got, err)
	}
	if err := s.DeletePersonalData(ctx, u.UserID); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetByID(ctx, u.UserID)
	if got.Username != fmt.Sprintf("deleted_%d", u.UserID) || got.PasswordHash != nil || got.DeletedAt == nil {
		t.Fatalf("not scrubbed: %+v", got)
	}
	_, err = s.GetByID(ctx, -1)
	wantCode(t, err, errs.UserNotFound)
}

func TestRoomCodeCollisionRetry(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	first, err := s.CreateRoom(ctx, newUser(t, s).UserID, 10)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	genCode = func() string {
		calls++
		if calls <= 2 {
			return first.Code // collide twice
		}
		return randomCode()
	}
	t.Cleanup(func() { genCode = randomCode })

	second, err := s.CreateRoom(ctx, newUser(t, s).UserID, 10)
	if err != nil {
		t.Fatalf("CreateRoom after collision: %v", err)
	}
	if calls != 3 || second.Code == first.Code {
		t.Fatalf("calls=%d code=%s first=%s", calls, second.Code, first.Code)
	}
	genCode = func() string { return first.Code }
	_, err = s.CreateRoom(ctx, newUser(t, s).UserID, 10)
	wantCode(t, err, errs.ServiceUnavailable)
}

func TestRoomSeatLimitAndAlreadyInRoom(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	host := newUser(t, s)
	room, err := s.CreateRoom(ctx, host.UserID, 25)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateRoom(ctx, host.UserID, 25); err == nil {
		t.Fatal("host created second room")
	} else {
		wantCode(t, err, errs.AlreadyInRoom)
	}

	for i := 1; i < MaxRoomMembers; i++ {
		seat, err := s.AddMember(ctx, room.RoomID, newUser(t, s).UserID)
		if err != nil || seat != i {
			t.Fatalf("join %d: seat=%d err=%v", i, seat, err)
		}
	}
	_, err = s.AddMember(ctx, room.RoomID, newUser(t, s).UserID)
	wantCode(t, err, errs.RoomFull)

	// Rejoin is idempotent and keeps the seat.
	if seat, err := s.AddMember(ctx, room.RoomID, host.UserID); err != nil || seat != 0 {
		t.Fatalf("rejoin: %d %v", seat, err)
	}

	other, err := s.CreateRoom(ctx, newUser(t, s).UserID, 25)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.AddMember(ctx, other.RoomID, host.UserID)
	wantCode(t, err, errs.AlreadyInRoom)

	// Freed seat is reused.
	members, _ := s.ListMembers(ctx, room.RoomID)
	if len(members) != MaxRoomMembers {
		t.Fatalf("members=%d", len(members))
	}
	if left, err := s.RemoveMember(ctx, room.RoomID, members[3].UserID); err != nil || left != 9 {
		t.Fatalf("remove: %d %v", left, err)
	}
	newcomer := newUser(t, s)
	if seat, err := s.AddMember(ctx, room.RoomID, newcomer.UserID); err != nil || seat != 3 {
		t.Fatalf("reuse seat: %d %v", seat, err)
	}
	if ms, _ := s.MembershipsForUser(ctx, newcomer.UserID); len(ms) != 1 || ms[0].RoomID != room.RoomID {
		t.Fatalf("memberships: %+v", ms)
	}

	if err := s.SetStatus(ctx, room.RoomID, "locked"); err != nil {
		t.Fatal(err)
	}
	s.RemoveMember(ctx, room.RoomID, newcomer.UserID)
	_, err = s.AddMember(ctx, room.RoomID, newUser(t, s).UserID)
	wantCode(t, err, errs.RoomLocked)

	_, err = s.GetByCode(ctx, "ZZZZZZ")
	wantCode(t, err, errs.RoomNotFound)
	_, err = s.AddMember(ctx, "not-a-uuid", host.UserID)
	wantCode(t, err, errs.RoomNotFound)
}

func TestStartupWipe(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	host := newUser(t, s)
	room, err := s.CreateRoom(ctx, host.UserID, 50)
	if err != nil {
		t.Fatal(err)
	}
	idle, err := s.CreateRoom(ctx, newUser(t, s).UserID, 50)
	if err != nil {
		t.Fatal(err)
	}
	m, err := s.CreateMatch(ctx, room.RoomID, 50)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddMatchPlayers(ctx, m.MatchID, []int64{host.UserID}, []int{0}); err != nil {
		t.Fatal(err)
	}
	if in, _ := s.HasRunningMatch(ctx, host.UserID); !in {
		t.Fatal("HasRunningMatch false")
	}

	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ts := s.InTx(tx)
	if _, err := ts.DeleteAllRooms(ctx); err != nil {
		t.Fatal(err)
	}
	n, err := ts.AbortAllRunning(ctx, time.Now())
	if err != nil || n < 1 {
		t.Fatalf("abort: %d %v", n, err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	var members int
	s.Pool.QueryRow(ctx, `SELECT count(*) FROM room_members`).Scan(&members)
	if members != 0 {
		t.Fatalf("room_members left: %d", members)
	}
	_, err = s.GetByCode(ctx, idle.Code)
	wantCode(t, err, errs.RoomNotFound)
	got, _ := s.GetMatch(ctx, m.MatchID)
	if got.Status != "aborted" || got.EndedAt == nil {
		t.Fatalf("match not aborted: %+v", got)
	}
	if in, _ := s.HasRunningMatch(ctx, host.UserID); in {
		t.Fatal("still running after abort")
	}
	_, err = s.AddMember(ctx, room.RoomID, newUser(t, s).UserID)
	wantCode(t, err, errs.RoomLocked)
	_, err = s.GetMatch(ctx, "garbage")
	wantCode(t, err, errs.NotFound)
}

func TestMatchLifecycle(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	a, b := newUser(t, s), newUser(t, s)
	room, err := s.CreateRoom(ctx, a.UserID, 10)
	if err != nil {
		t.Fatal(err)
	}
	m, _ := s.CreateMatch(ctx, room.RoomID, 10)
	if err := s.AddMatchPlayers(ctx, m.MatchID, []int64{a.UserID, b.UserID}, []int{0, 1}); err != nil {
		t.Fatal(err)
	}
	if cur, _ := s.MatchForUser(ctx, b.UserID); cur == nil || cur.MatchID != m.MatchID {
		t.Fatalf("MatchForUser: %+v", cur)
	}
	if err := s.EndMatch(ctx, m.MatchID, a.UserID, time.Now()); err != nil {
		t.Fatal(err)
	}
	wantCode(t, s.EndMatch(ctx, m.MatchID, a.UserID, time.Now()), errs.MatchEnded)
	ps, _ := s.GetMatchPlayers(ctx, m.MatchID)
	if len(ps) != 2 || ps[1].UserID != b.UserID {
		t.Fatalf("players: %+v", ps)
	}
	hist, err := s.ListMatchesForUser(ctx, b.UserID, 10, time.Time{})
	if err != nil || len(hist) != 1 || *hist[0].WinnerID != a.UserID {
		t.Fatalf("history: %+v %v", hist, err)
	}
	if more, _ := s.ListMatchesForUser(ctx, b.UserID, 10, hist[0].StartedAt); len(more) != 0 {
		t.Fatalf("cursor page: %+v", more)
	}
}

func countLogs(t *testing.T, s *Store, prefix string) int {
	t.Helper()
	var n int
	if err := s.Pool.QueryRow(context.Background(),
		`SELECT count(*) FROM http_log WHERE request_id LIKE $1 || '%'`, prefix).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func waitFor(t *testing.T, d time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return cond()
}

func TestHTTPLogBatchAt500(t *testing.T) {
	s := testStore(t)
	httpLogEvery = time.Hour // only the size trigger can flush
	t.Cleanup(func() { httpLogEvery = time.Second })
	w := s.NewAsyncWriter()
	prefix := fmt.Sprintf("b500-%d-", time.Now().UnixNano())

	for i := range 499 {
		w.Write(LogEntry{RequestID: fmt.Sprint(prefix, i), Endpoint: "/api/me", StatusCode: 200})
	}
	time.Sleep(200 * time.Millisecond)
	if n := countLogs(t, s, prefix); n != 0 {
		t.Fatalf("flushed early: %d rows", n)
	}
	w.Write(LogEntry{RequestID: prefix + "last", Endpoint: "/api/me", StatusCode: 200})
	if !waitFor(t, 3*time.Second, func() bool { return countLogs(t, s, prefix) == 500 }) {
		t.Fatalf("500-row batch not written: %d", countLogs(t, s, prefix))
	}
	w.Close(context.Background())
}

func TestHTTPLogFlushAfterInterval(t *testing.T) {
	s := testStore(t)
	w := s.NewAsyncWriter() // default 1s interval
	prefix := fmt.Sprintf("t1s-%d-", time.Now().UnixNano())
	uid := int64(0)
	for i := range 3 {
		w.Write(LogEntry{RequestID: fmt.Sprint(prefix, i), Endpoint: "/api/login", StatusCode: 429, UserID: &uid})
	}
	if n := countLogs(t, s, prefix); n != 0 {
		t.Fatalf("written synchronously: %d", n)
	}
	if !waitFor(t, 2500*time.Millisecond, func() bool { return countLogs(t, s, prefix) == 3 }) {
		t.Fatalf("1s flush missing: %d", countLogs(t, s, prefix))
	}

	// Flush and Close write whatever is queued without waiting for the timer.
	w.Write(LogEntry{RequestID: prefix + "flush", Endpoint: "/x", StatusCode: 200})
	w.Flush(context.Background())
	if n := countLogs(t, s, prefix); n != 4 {
		t.Fatalf("after Flush: %d", n)
	}
	w.Write(LogEntry{RequestID: prefix + "close", Endpoint: "/x", StatusCode: 200})
	w.Close(context.Background())
	if n := countLogs(t, s, prefix); n != 5 {
		t.Fatalf("after Close: %d", n)
	}
}

func TestHTTPLogDropCounter(t *testing.T) {
	// No flusher goroutine: the queue fills and Write must drop, not block.
	w := &AsyncWriter{entries: make(chan LogEntry, 2)}
	done := make(chan struct{})
	go func() {
		for range 5 {
			w.Write(LogEntry{RequestID: "x"})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Write blocked")
	}
	if got := w.DropCount(); got != 3 {
		t.Fatalf("DropCount=%d want 3", got)
	}
}

func TestBucketStarts(t *testing.T) {
	now := time.Date(2026, 11, 12, 9, 30, 0, 0, time.UTC)
	cases := []struct {
		rng   string
		n     int
		step  time.Duration
		first time.Time
	}{
		{"today", 10, time.Hour, time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC)},
		{"Today", 10, time.Hour, time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC)},
		{"24h", 24, time.Hour, time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC)},
		{"3d", 3, 24 * time.Hour, time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)},
		{"7d", 7, 24 * time.Hour, time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)},
		{"30d", 30, 24 * time.Hour, time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)},
		{"60d", 60, 24 * time.Hour, time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)},
		{"bogus", 30, 24 * time.Hour, time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)},
	}
	for _, c := range cases {
		got := bucketStarts(c.rng, now)
		if len(got) != c.n || !got[0].Equal(c.first) {
			t.Fatalf("%s: n=%d first=%v", c.rng, len(got), got[0])
		}
		for i := 1; i < len(got); i++ {
			if got[i].Sub(got[i-1]) != c.step {
				t.Fatalf("%s: step %v at %d", c.rng, got[i].Sub(got[i-1]), i)
			}
		}
	}
}

func TestSnapshotSeriesBuckets(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	u := newUser(t, s)
	now := time.Date(2026, 11, 12, 9, 30, 0, 0, time.UTC)
	at := func(h, m int) time.Time { return time.Date(2026, 11, 12, h, m, 0, 0, time.UTC) }
	for _, snap := range []struct {
		t time.Time
		b int64
	}{
		{at(2, 10), 100}, {at(2, 50), 120}, // two writes in one hour: last wins
		{at(5, 0), 80},
		{now.Add(-5 * 24 * time.Hour), 40}, // 2026-11-07, only visible in daily ranges
	} {
		if err := s.InsertSnapshot(ctx, u.UserID, snap.b, snap.t); err != nil {
			t.Fatal(err)
		}
	}

	today, err := s.seriesForUserAt(ctx, u.UserID, "today", now)
	if err != nil {
		t.Fatal(err)
	}
	// 00:00..09:00 = 10 hourly buckets; 11-07 snapshot carries into 00:00 and 01:00.
	want := []int64{40, 40, 120, 120, 120, 80, 80, 80, 80, 80}
	if len(today) != len(want) {
		t.Fatalf("today: %d points", len(today))
	}
	for i, p := range today {
		if p.Balance != want[i] || p.At.Hour() != i {
			t.Fatalf("today[%d]=%+v want %d", i, p, want[i])
		}
	}

	week, err := s.seriesForUserAt(ctx, u.UserID, "7d", now)
	if err != nil {
		t.Fatal(err)
	}
	// 11-06 has no snapshot yet and is omitted; 11-07..11-11 carry 40; 11-12 ends at 80.
	if len(week) != 6 || week[0].Balance != 40 || week[0].At.Day() != 7 || week[5].Balance != 80 {
		t.Fatalf("7d: %+v", week)
	}

	day, err := s.seriesForUserAt(ctx, u.UserID, "24h", now)
	if err != nil {
		t.Fatal(err)
	}
	if len(day) != 24 || day[0].Balance != 40 || day[23].Balance != 80 {
		t.Fatalf("24h: len=%d %+v", len(day), day)
	}

	empty, err := s.seriesForUserAt(ctx, newUser(t, s).UserID, "30d", now)
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty user: %+v %v", empty, err)
	}
}

func TestAdminAuditAndFlags(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	rid := fmt.Sprintf("audit-%d", time.Now().UnixNano())
	id1, err := s.InsertAudit(ctx, AuditEntry{AdminUsername: "root", Action: "BALANCE", RequestID: rid})
	if err != nil {
		t.Fatal(err)
	}
	id2, _ := s.InsertAudit(ctx, AuditEntry{AdminUsername: "root", Action: "DELETE", RequestID: rid})
	page, err := s.ListAudit(ctx, 1, id2+1)
	if err != nil || len(page) != 1 || page[0].ID != id2 {
		t.Fatalf("audit page 1: %+v %v", page, err)
	}
	if page, _ = s.ListAudit(ctx, 1, id2); len(page) != 1 || page[0].ID != id1 {
		t.Fatalf("audit page 2: %+v", page)
	}

	fid, err := s.InsertFlag(ctx, "TEST_FLAG", []byte(`{"n":1}`))
	if err != nil {
		t.Fatal(err)
	}
	open, _ := s.ListOpenFlags(ctx, 100)
	found := false
	for _, f := range open {
		found = found || f.ID == fid
	}
	if !found {
		t.Fatal("flag not open")
	}
	if err := s.ClearFlag(ctx, fid, time.Now()); err != nil {
		t.Fatal(err)
	}
	open, _ = s.ListOpenFlags(ctx, 100)
	for _, f := range open {
		if f.ID == fid {
			t.Fatal("flag still open")
		}
	}
	wantCode(t, s.ClearFlag(ctx, -1, time.Now()), errs.NotFound)
}
