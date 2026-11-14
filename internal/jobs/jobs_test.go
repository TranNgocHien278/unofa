package jobs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/unofa/unofa/internal/clock"
	"github.com/unofa/unofa/internal/ledger"
	"github.com/unofa/unofa/internal/loan"
	"github.com/unofa/unofa/internal/store"
)

func testLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(buf, nil))
}

// A panicking job must be recovered, logged with its error, and the runner
// must keep executing later runs.
func TestPanicInJobDoesNotKillRunner(t *testing.T) {
	var buf bytes.Buffer
	r := &Runner{log: testLogger(&buf)}
	boom := job{name: "boom", fn: func(context.Context) (int64, error) { panic("kaboom") }}
	ok := job{name: "ok", fn: func(context.Context) (int64, error) { return 3, nil }}

	r.run(context.Background(), boom)
	r.run(context.Background(), ok)

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 log lines, got %d: %q", len(lines), buf.String())
	}
	var first map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatalf("log line not JSON: %v", err)
	}
	if first["job"] != "boom" || !strings.Contains(first["error"].(string), "kaboom") {
		t.Fatalf("panic not logged with error: %v", first)
	}
	var second map[string]any
	if err := json.Unmarshal([]byte(lines[1]), &second); err != nil {
		t.Fatalf("log line not JSON: %v", err)
	}
	if second["job"] != "ok" || second["affected"].(float64) != 3 || second["error"] != "" {
		t.Fatalf("ok run not logged correctly: %v", second)
	}
}

func TestJobErrorIsLogged(t *testing.T) {
	var buf bytes.Buffer
	r := &Runner{log: testLogger(&buf)}
	r.run(context.Background(), job{name: "bad", fn: func(context.Context) (int64, error) {
		return 0, errors.New("db down")
	}})
	if !strings.Contains(buf.String(), "db down") {
		t.Fatalf("error not logged: %q", buf.String())
	}
}

// Skewing user 0 by +7 must make the invariant job write one MONEY_INVARIANT
// flag with sum = baseline + 7. The skew is reverted on cleanup.
func TestInvariantJobFlagsSkewedBooks(t *testing.T) {
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

	baseline, err := ledger.CheckInvariant(ctx, st.Pool, time.Now())
	if err != nil {
		t.Fatalf("baseline invariant: %v", err)
	}

	before := maxFlagID(t, st)
	if _, err := st.Pool.Exec(ctx, `UPDATE users SET balance = balance + 7 WHERE user_id = 0`); err != nil {
		t.Fatalf("skew: %v", err)
	}
	t.Cleanup(func() {
		_, _ = st.Pool.Exec(ctx, `UPDATE users SET balance = balance - 7 WHERE user_id = 0`)
		_, _ = st.Pool.Exec(ctx, `DELETE FROM admin_flags WHERE id > $1`, before)
	})

	fake := &clock.Fake{T: time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)}

	affected, err := runInvariant(ctx, st, fake)
	if err != nil {
		t.Fatalf("runInvariant: %v", err)
	}
	if affected != 1 {
		t.Fatalf("affected = %d, want 1", affected)
	}

	var kind string
	var detail []byte
	if err := st.Pool.QueryRow(ctx,
		`SELECT kind, detail FROM admin_flags WHERE id > $1 ORDER BY id DESC LIMIT 1`, before,
	).Scan(&kind, &detail); err != nil {
		t.Fatalf("read flag: %v", err)
	}
	if kind != "MONEY_INVARIANT" {
		t.Fatalf("kind = %q, want MONEY_INVARIANT", kind)
	}
	var d struct {
		Sum int64 `json:"sum"`
	}
	if err := json.Unmarshal(detail, &d); err != nil {
		t.Fatalf("detail not JSON: %v", err)
	}
	if d.Sum != baseline+7 {
		t.Fatalf("flag sum = %d, want %d", d.Sum, baseline+7)
	}
}

func maxFlagID(t *testing.T, st *store.Store) int64 {
	t.Helper()
	var id int64
	if err := st.Pool.QueryRow(context.Background(),
		`SELECT COALESCE(MAX(id), 0) FROM admin_flags`).Scan(&id); err != nil {
		t.Fatalf("max flag id: %v", err)
	}
	return id
}

func openStore(t *testing.T) *store.Store {
	t.Helper()
	dsn := os.Getenv("UNOFA_TEST_DSN")
	if dsn == "" {
		t.Skip("UNOFA_TEST_DSN unset")
	}
	st, err := store.New(context.Background(), dsn, 4, 1, 5*time.Second)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(st.Close)
	return st
}

func resetJobTables(t *testing.T, st *store.Store) {
	t.Helper()
	_, err := st.Pool.Exec(context.Background(), `
		TRUNCATE admin_flags, balance_snapshot, transactions, loans, match_players, matches, room_members, rooms, users, http_log RESTART IDENTITY CASCADE;
		INSERT INTO users (user_id, username, password_hash, balance) VALUES (0, 'System', NULL, 0);
	`)
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
}

func addJobUser(t *testing.T, st *store.Store, id int64) {
	t.Helper()
	_, err := st.Pool.Exec(context.Background(),
		`INSERT INTO users (user_id, username, balance) VALUES ($1, $2, 0)`,
		id, fmt.Sprintf("jobuser%d", id))
	if err != nil {
		t.Fatalf("addJobUser %d: %v", id, err)
	}
}

func jobsLoanCfg() loan.LoanCfg {
	return loan.LoanCfg{
		Packages:      []int64{500, 1000, 2000},
		OverdueBps:    500,
		CapMultiplier: 2,
		Terms: []loan.TermCfg{
			{Name: loan.Term24h, Duration: 24 * time.Hour, RateBps: 100},
		},
	}
}

// disburseAt opens a 500U / 24h loan for userID at clock now. Due is now + 24h.
func disburseAt(t *testing.T, st *store.Store, userID int64, now time.Time) loan.Loan {
	t.Helper()
	ctx := context.Background()
	tx, err := st.Pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	l, err := loan.Disburse(ctx, tx, userID, 500, loan.Term24h, now, jobsLoanCfg())
	if err != nil {
		tx.Rollback(ctx)
		t.Fatalf("Disburse: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
	return l
}

func userBalance(t *testing.T, st *store.Store, userID int64) int64 {
	t.Helper()
	var bal int64
	if err := st.Pool.QueryRow(context.Background(),
		`SELECT balance FROM users WHERE user_id = $1`, userID).Scan(&bal); err != nil {
		t.Fatalf("balance %d: %v", userID, err)
	}
	return bal
}

func seizedFlagCount(t *testing.T, st *store.Store) int {
	t.Helper()
	var n int
	if err := st.Pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM admin_flags WHERE kind = 'LOAN_SEIZED'`).Scan(&n); err != nil {
		t.Fatalf("count LOAN_SEIZED: %v", err)
	}
	return n
}

// The loan reaches the cap at 14 overdue periods (ledger.SeizeAt). One tick
// seizes it, debits the 500U balance to 0, and writes one flag. A second tick
// finds nothing active, so it writes no second flag and debits nothing.
func TestSeizeJobSeizesLoanAtCapOnce(t *testing.T) {
	st := openStore(t)
	resetJobTables(t, st)
	ctx := context.Background()
	cfg := jobsLoanCfg()
	addJobUser(t, st, 1)
	l := disburseAt(t, st, 1, time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC))

	clk := &clock.Fake{T: ledger.SeizeAt(500, 100, l.DueAt, cfg.OverdueBps, cfg.CapMultiplier)}

	affected, err := runSeize(ctx, st, clk, cfg)
	if err != nil {
		t.Fatalf("runSeize: %v", err)
	}
	if affected != 1 {
		t.Fatalf("affected = %d, want 1", affected)
	}

	var status string
	if err := st.Pool.QueryRow(ctx, `SELECT status FROM loans WHERE loan_id = $1`, l.LoanID).Scan(&status); err != nil {
		t.Fatalf("read loan status: %v", err)
	}
	if status != "seized" {
		t.Fatalf("loan status = %q, want seized", status)
	}
	if bal := userBalance(t, st, 1); bal != 0 {
		t.Fatalf("user balance = %d, want 0 (floor)", bal)
	}

	var detail []byte
	if err := st.Pool.QueryRow(ctx,
		`SELECT detail FROM admin_flags WHERE kind = 'LOAN_SEIZED'`).Scan(&detail); err != nil {
		t.Fatalf("read LOAN_SEIZED flag: %v", err)
	}
	var d struct {
		LoanID   int64 `json:"loan_id"`
		UserID   int64 `json:"user_id"`
		Seized   int64 `json:"seized"`
		Forgiven int64 `json:"forgiven"`
	}
	if err := json.Unmarshal(detail, &d); err != nil {
		t.Fatalf("detail not JSON: %v", err)
	}
	if d.LoanID != l.LoanID || d.UserID != 1 || d.Seized != 500 || d.Forgiven != 500 {
		t.Fatalf("flag detail = %+v, want loan %d user 1 seized 500 forgiven 500", d, l.LoanID)
	}

	clk.Advance(24 * time.Hour)
	affected, err = runSeize(ctx, st, clk, cfg)
	if err != nil {
		t.Fatalf("second runSeize: %v", err)
	}
	if affected != 0 {
		t.Fatalf("second tick affected = %d, want 0", affected)
	}
	if n := seizedFlagCount(t, st); n != 1 {
		t.Fatalf("LOAN_SEIZED flags = %d after second tick, want 1", n)
	}
	if bal := userBalance(t, st, 1); bal != 0 {
		t.Fatalf("user balance = %d after second tick, want 0", bal)
	}
}

// SeizeDue runs every candidate on the one tx that runSeize opens. If loan 2's
// seize fails, runSeize returns the error and its deferred Rollback undoes the
// loan 1 seizure too. This test asserts the isolation SeizeDue's doc comment
// promises, so it fails on the current code. The failure is injected with a
// trigger that rejects the LOAN_SEIZED flag for user 2.
func TestSeizeJobIsolatesFailingLoan(t *testing.T) {
	st := openStore(t)
	resetJobTables(t, st)
	ctx := context.Background()
	cfg := jobsLoanCfg()
	addJobUser(t, st, 1)
	addJobUser(t, st, 2)
	now := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	l1 := disburseAt(t, st, 1, now)
	disburseAt(t, st, 2, now)

	if _, err := st.Pool.Exec(ctx, `
		CREATE FUNCTION jobs_test_fail_seize() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			IF NEW.detail->>'user_id' = '2' THEN RAISE EXCEPTION 'injected seize failure'; END IF;
			RETURN NEW;
		END $$;
		CREATE TRIGGER jobs_test_fail_seize BEFORE INSERT ON admin_flags
			FOR EACH ROW EXECUTE FUNCTION jobs_test_fail_seize();
	`); err != nil {
		t.Fatalf("install trigger: %v", err)
	}
	t.Cleanup(func() {
		_, _ = st.Pool.Exec(context.Background(),
			`DROP TRIGGER IF EXISTS jobs_test_fail_seize ON admin_flags; DROP FUNCTION IF EXISTS jobs_test_fail_seize();`)
	})

	clk := &clock.Fake{T: ledger.SeizeAt(500, 100, l1.DueAt, cfg.OverdueBps, cfg.CapMultiplier)}
	if _, err := runSeize(ctx, st, clk, cfg); err == nil {
		t.Fatal("runSeize: want injected error, got nil")
	}

	var status string
	if err := st.Pool.QueryRow(ctx, `SELECT status FROM loans WHERE loan_id = $1`, l1.LoanID).Scan(&status); err != nil {
		t.Fatalf("read loan 1 status: %v", err)
	}
	if status != "seized" {
		t.Fatalf("loan 1 status = %q after loan 2 failed, want seized: one failing seize rolled back the whole tick", status)
	}
}

// The http_log job deletes rows older than retention (60 days) and keeps the rest.
func TestCleanupJobDeletesOnlyExpiredLogs(t *testing.T) {
	st := openStore(t)
	resetJobTables(t, st)
	ctx := context.Background()
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	retention := 60 * 24 * time.Hour
	if _, err := st.Pool.Exec(ctx, `
		INSERT INTO http_log (request_id, endpoint, status_code, at) VALUES
			('old', '/x', 200, $1),
			('recent', '/x', 200, $2)`,
		now.Add(-retention-24*time.Hour), now.Add(-time.Hour)); err != nil {
		t.Fatalf("seed http_log: %v", err)
	}

	affected, err := runCleanup(ctx, st, &clock.Fake{T: now}, retention)
	if err != nil {
		t.Fatalf("runCleanup: %v", err)
	}
	if affected != 1 {
		t.Fatalf("affected = %d, want 1", affected)
	}

	rows, err := st.Pool.Query(ctx, `SELECT request_id FROM http_log ORDER BY id`)
	if err != nil {
		t.Fatalf("read http_log: %v", err)
	}
	defer rows.Close()
	var left []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan: %v", err)
		}
		left = append(left, id)
	}
	if len(left) != 1 || left[0] != "recent" {
		t.Fatalf("remaining http_log request_ids = %v, want [recent]", left)
	}
}

// No job body may run before StartDelay. The probe job records its first call
// time and the test checks it is at least delay after Start.
func TestStartDelayHoldsFirstTick(t *testing.T) {
	const delay = 200 * time.Millisecond
	calls := make(chan time.Time, 16)
	r := &Runner{
		delay: delay,
		log:   slog.New(slog.NewJSONHandler(io.Discard, nil)),
		jobs: []job{{name: "probe", every: 10 * time.Millisecond, fn: func(context.Context) (int64, error) {
			select {
			case calls <- time.Now():
			default:
			}
			return 0, nil
		}}},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	t0 := time.Now()
	r.Start(ctx)
	select {
	case first := <-calls:
		if got := first.Sub(t0); got < delay {
			t.Fatalf("first run after %v, want >= StartDelay %v", got, delay)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("job never ran")
	}
	cancel()
	r.Wait()
}
