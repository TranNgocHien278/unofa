package loan_test

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/unofa/unofa/internal/errs"
	"github.com/unofa/unofa/internal/ledger"
	"github.com/unofa/unofa/internal/loan"
)

// testCfg mirrors loan: in config.example.yaml (03 §Gói và kỳ hạn).
func testCfg() loan.LoanCfg {
	return loan.LoanCfg{
		Packages:      []int64{500, 1000, 2000},
		OverdueBps:    500,
		CapMultiplier: 2,
		Terms: []loan.TermCfg{
			{Name: loan.Term24h, Duration: 24 * time.Hour, RateBps: 100},
			{Name: loan.Term3d, Duration: 72 * time.Hour, RateBps: 300},
			{Name: loan.Term30d, Duration: 720 * time.Hour, RateBps: 1000},
		},
	}
}

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("UNOFA_TEST_DSN")
	if dsn == "" {
		t.Skip("UNOFA_TEST_DSN not set")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func resetUsers(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		TRUNCATE admin_flags, balance_snapshot, transactions, loans, match_players, matches, room_members, rooms, users RESTART IDENTITY CASCADE;
		INSERT INTO users (user_id, username, password_hash, balance) VALUES (0, 'System', NULL, 0);
	`)
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
}

func addUser(t *testing.T, pool *pgxpool.Pool, id int64) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO users (user_id, username, balance) VALUES ($1, $2, 0)`,
		id, "loanuser"+string(rune('a'+id-1)))
	if err != nil {
		t.Fatalf("addUser %d: %v", id, err)
	}
}

// T9: golden loan series for 500U / 24h. 505, 530, 557, ... and cap at n = 14.
func TestT9_GoldenLoanSeries(t *testing.T) {
	cfg := testCfg()
	dueAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	want := []int64{505, 530, 557, 585, 614, 645, 677, 711, 747, 784, 823, 864, 907, 952, 1000}
	for n, w := range want {
		now := dueAt.Add(time.Duration(n) * 24 * time.Hour)
		owed, periods := ledger.Owed(500, 100, dueAt, now, cfg.OverdueBps, cfg.CapMultiplier)
		if owed != w {
			t.Errorf("n=%d: owed = %d, want %d", n, owed, w)
		}
		if periods != n {
			t.Errorf("n=%d: periods = %d, want %d", n, periods, n)
		}
	}
}

// T9: seize fires exactly at n = 14, not 15.
func TestT9_SeizeAtFourteenPeriods(t *testing.T) {
	cfg := testCfg()
	dueAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	got := ledger.SeizeAt(500, 100, dueAt, cfg.OverdueBps, cfg.CapMultiplier)
	want := dueAt.Add(14 * 24 * time.Hour)
	if !got.Equal(want) {
		t.Errorf("SeizeAt = %s, want %s", got, want)
	}
	if got.Equal(dueAt.Add(15 * 24 * time.Hour)) {
		t.Error("SeizeAt: landed on n=15, the float formula's answer")
	}
}

// T9: Active derives the same series from a row written by Disburse.
func TestT9_ActiveReportsLiveOwed(t *testing.T) {
	pool := testPool(t)
	resetUsers(t, pool)
	addUser(t, pool, 1)

	ctx := context.Background()
	cfg := testCfg()
	dueAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	_, err = loan.Disburse(ctx, tx, 1, 500, loan.Term24h, dueAt.Add(-24*time.Hour), cfg)
	if err != nil {
		tx.Rollback(ctx)
		t.Fatalf("Disburse: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	for _, tc := range []struct {
		name  string
		now   time.Time
		owed  int64
		over  bool
		seize time.Time
	}{
		{"due", dueAt, 505, false, dueAt.Add(14 * 24 * time.Hour)},
		{"n=1", dueAt.Add(24 * time.Hour), 530, true, dueAt.Add(14 * 24 * time.Hour)},
		{"n=14", dueAt.Add(14 * 24 * time.Hour), 1000, true, dueAt.Add(14 * 24 * time.Hour)},
	} {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatalf("%s begin: %v", tc.name, err)
		}
		a, ok, err := loan.Active(ctx, tx, 1, tc.now, cfg)
		tx.Rollback(ctx)
		if err != nil || !ok {
			t.Fatalf("%s: Active ok=%v err=%v", tc.name, ok, err)
		}
		if a.OwendNow != tc.owed {
			t.Errorf("%s: owed = %d, want %d", tc.name, a.OwendNow, tc.owed)
		}
		if a.Overdue != tc.over {
			t.Errorf("%s: overdue = %v, want %v", tc.name, a.Overdue, tc.over)
		}
		if !a.SeizeAt.Equal(tc.seize) {
			t.Errorf("%s: seize_at = %s, want %s", tc.name, a.SeizeAt, tc.seize)
		}
	}
}

// T10: owed never passes 2 x principal. Exact at the boundary, clamped one step past.
func TestT10_CapAtBoundary(t *testing.T) {
	cfg := testCfg()
	dueAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	capAmount := int64(1000) // 2 x 500

	atCap := dueAt.Add(14 * 24 * time.Hour)
	if owed, _ := ledger.Owed(500, 100, dueAt, atCap, cfg.OverdueBps, cfg.CapMultiplier); owed != capAmount {
		t.Errorf("n=14: owed = %d, want cap %d", owed, capAmount)
	}

	pastCap := dueAt.Add(15 * 24 * time.Hour)
	if owed, _ := ledger.Owed(500, 100, dueAt, pastCap, cfg.OverdueBps, cfg.CapMultiplier); owed != capAmount {
		t.Errorf("n=15: owed = %d, want cap %d", owed, capAmount)
	}

	far := dueAt.Add(365 * 24 * time.Hour)
	if owed, _ := ledger.Owed(500, 100, dueAt, far, cfg.OverdueBps, cfg.CapMultiplier); owed != capAmount {
		t.Errorf("n=365: owed = %d, want cap %d", owed, capAmount)
	}

	// The cap scales with the package: 2000U caps at 4000U, not at some fixed number.
	if owed, _ := ledger.Owed(2000, 100, dueAt, far, cfg.OverdueBps, cfg.CapMultiplier); owed != 4000 {
		t.Errorf("2000U cap: owed = %d, want 4000", owed)
	}
}

// T8: a user with an overdue loan cannot transfer, checked end to end through
// the loan package (Disburse writes the row, ledger.Transfer refuses it).
func TestT8_OverdueLoanBlocksTransfer(t *testing.T) {
	pool := testPool(t)
	resetUsers(t, pool)
	addUser(t, pool, 1)
	addUser(t, pool, 2)

	ctx := context.Background()
	cfg := testCfg()
	now := time.Now().UTC()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := loan.Disburse(ctx, tx, 1, 500, loan.Term24h, now.Add(-48*time.Hour), cfg); err != nil {
		tx.Rollback(ctx)
		t.Fatalf("Disburse: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin transfer: %v", err)
	}
	err = ledger.Transfer(ctx, tx, ledger.User(1), ledger.User(2), 50, ledger.ReasonTransfer, "transfer:t8:loan")
	tx.Rollback(ctx)

	var e *errs.Error
	if !errors.As(err, &e) || e.Code != errs.LoanOverdue {
		t.Fatalf("T8: error = %v, want %s", err, errs.LoanOverdue)
	}
}

// T6: two simultaneous borrows for the same user. Exactly one loan exists afterwards.
func TestT6_ConcurrentBorrowOneWins(t *testing.T) {
	pool := testPool(t)
	resetUsers(t, pool)
	addUser(t, pool, 1)

	ctx := context.Background()
	cfg := testCfg()
	now := time.Now().UTC()

	var wg sync.WaitGroup
	errsSeen := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			tx, err := pool.Begin(ctx)
			if err != nil {
				errsSeen[idx] = err
				return
			}
			if _, err := loan.Disburse(ctx, tx, 1, 500, loan.Term24h, now, cfg); err != nil {
				tx.Rollback(ctx)
				errsSeen[idx] = err
				return
			}
			errsSeen[idx] = tx.Commit(ctx)
		}(i)
	}
	wg.Wait()

	var ok, conflict int
	for i, err := range errsSeen {
		if err == nil {
			ok++
			continue
		}
		conflict++
		var e *errs.Error
		if !errors.As(err, &e) || e.Code != errs.LoanActiveExists {
			t.Errorf("goroutine %d: error = %v, want %s", i, err, errs.LoanActiveExists)
		}
	}
	if ok != 1 || conflict != 1 {
		t.Fatalf("T6: %d succeeded, %d failed; want exactly 1 and 1", ok, conflict)
	}

	var active int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM loans WHERE user_id = 1 AND status = 'active'`).Scan(&active); err != nil {
		t.Fatalf("count: %v", err)
	}
	if active != 1 {
		t.Errorf("active loans = %d, want 1", active)
	}
}
