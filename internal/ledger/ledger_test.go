package ledger_test

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/unofa/unofa/internal/ledger"
)

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

// resetUsers truncates money tables and re-inserts the System row so each test
// starts from the same baseline. Balance of System starts at 0 (invariant: 0).
func resetUsers(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	_, err := pool.Exec(ctx, `
		TRUNCATE admin_flags, balance_snapshot, transactions, loans, match_players, matches, room_members, rooms, users RESTART IDENTITY CASCADE;
		INSERT INTO users (user_id, username, password_hash, balance) VALUES (0, 'System', NULL, 0);
	`)
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
}

func addUser(t *testing.T, pool *pgxpool.Pool, id int64, bal int64) {
	t.Helper()
	ctx := context.Background()
	// Insert with balance=0 first, then fund via System→Transfer so invariant holds.
	_, err := pool.Exec(ctx,
		`INSERT INTO users (user_id, username, balance) VALUES ($1, $2, 0)`,
		id, "user"+string(rune('a'+id-1)))
	if err != nil {
		t.Fatalf("addUser insert %d: %v", id, err)
	}
	if bal > 0 {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatalf("addUser begin %d: %v", id, err)
		}
		key := "setup:user:" + string(rune('a'+id-1))
		if err := ledger.Transfer(ctx, tx, ledger.System, ledger.User(id), bal, ledger.ReasonSignupBonus, key); err != nil {
			tx.Rollback(ctx)
			t.Fatalf("addUser fund %d: %v", id, err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("addUser commit %d: %v", id, err)
		}
	}
}

func invariantMustZero(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	tx, _ := pool.Begin(context.Background())
	defer tx.Rollback(context.Background())
	sum, err := ledger.Invariant(context.Background(), tx)
	if err != nil {
		t.Fatalf("Invariant: %v", err)
	}
	if sum != 0 {
		t.Errorf("T1: invariant = %d, want 0", sum)
	}
}

// T1: SUM(users.balance) + SUM(matches.pot) == 0 after every transfer.
func TestT1_InvariantZeroAfterTransfer(t *testing.T) {
	pool := testPool(t)
	resetUsers(t, pool)
	addUser(t, pool, 1, 200)

	ctx := context.Background()
	tx, _ := pool.Begin(ctx)
	err := ledger.Transfer(ctx, tx, ledger.System, ledger.User(1), 200, ledger.ReasonSignupBonus, "signup:1")
	if err != nil {
		t.Fatalf("Transfer: %v", err)
	}
	tx.Commit(ctx)
	invariantMustZero(t, pool)
}

// T2: Overdraft → fail, balance stays non-negative.
func TestT2_OverdraftFails(t *testing.T) {
	pool := testPool(t)
	resetUsers(t, pool)
	addUser(t, pool, 1, 100)

	ctx := context.Background()
	tx, _ := pool.Begin(ctx)
	err := ledger.Transfer(ctx, tx, ledger.User(1), ledger.System, 101, ledger.ReasonTransfer, "transfer:over")
	tx.Rollback(ctx)
	if err == nil {
		t.Fatal("T2: expected error on overdraft, got nil")
	}
	var bal int64
	pool.QueryRow(ctx, `SELECT balance FROM users WHERE user_id = 1`).Scan(&bal)
	if bal < 0 {
		t.Errorf("T2: balance went negative: %d", bal)
	}
}

// T3: Same idempotency key twice → only one transactions row.
func TestT3_IdempotencyTransferDedup(t *testing.T) {
	pool := testPool(t)
	resetUsers(t, pool)
	addUser(t, pool, 1, 500)
	addUser(t, pool, 2, 0)

	ctx := context.Background()
	for i := 0; i < 2; i++ {
		tx, _ := pool.Begin(ctx)
		err := ledger.Transfer(ctx, tx, ledger.User(1), ledger.User(2), 50, ledger.ReasonTransfer, "transfer:idem")
		if err != nil {
			tx.Rollback(ctx)
			t.Fatalf("T3 iter %d: %v", i, err)
		}
		tx.Commit(ctx)
	}
	var cnt int
	pool.QueryRow(ctx, `SELECT COUNT(*) FROM transactions WHERE idempotency_key = 'transfer:idem'`).Scan(&cnt)
	if cnt != 1 {
		t.Errorf("T3: expected 1 transaction row, got %d", cnt)
	}
	var bal int64
	pool.QueryRow(ctx, `SELECT balance FROM users WHERE user_id = 1`).Scan(&bal)
	if bal != 450 {
		t.Errorf("T3: user1 balance = %d, want 450", bal)
	}
}

// T4: Seize when balance == 0 → no transactions row, forgiven == owed.
func TestT4_SeizeZeroBalance(t *testing.T) {
	pool := testPool(t)
	resetUsers(t, pool)
	addUser(t, pool, 1, 0)

	ctx := context.Background()
	tx, _ := pool.Begin(ctx)
	seized, forgiven, err := ledger.Seize(ctx, tx, ledger.User(1), ledger.System, 500, ledger.ReasonSeize, "seize:t4")
	tx.Commit(ctx)
	if err != nil {
		t.Fatalf("T4: Seize: %v", err)
	}
	if seized != 0 {
		t.Errorf("T4: seized = %d, want 0", seized)
	}
	if forgiven != 500 {
		t.Errorf("T4: forgiven = %d, want 500", forgiven)
	}
	var cnt int
	pool.QueryRow(ctx, `SELECT COUNT(*) FROM transactions WHERE idempotency_key = 'seize:t4'`).Scan(&cnt)
	if cnt != 0 {
		t.Errorf("T4: expected 0 transaction rows, got %d", cnt)
	}
	invariantMustZero(t, pool)
}

// T5: Seize when balance < owed → seize all, forgiven == owed - balance.
func TestT5_SeizePartial(t *testing.T) {
	pool := testPool(t)
	resetUsers(t, pool)
	addUser(t, pool, 1, 300)

	ctx := context.Background()
	tx, _ := pool.Begin(ctx)
	seized, forgiven, err := ledger.Seize(ctx, tx, ledger.User(1), ledger.System, 500, ledger.ReasonSeize, "seize:t5")
	tx.Commit(ctx)
	if err != nil {
		t.Fatalf("T5: %v", err)
	}
	if seized != 300 {
		t.Errorf("T5: seized = %d, want 300", seized)
	}
	if forgiven != 200 {
		t.Errorf("T5: forgiven = %d, want 200", forgiven)
	}
	var bal int64
	pool.QueryRow(ctx, `SELECT balance FROM users WHERE user_id = 1`).Scan(&bal)
	if bal != 0 {
		t.Errorf("T5: balance = %d, want 0", bal)
	}
	invariantMustZero(t, pool)
}

// T7: Transfer limit 1000U/24h sliding window.
func TestT7_TransferLimitExceeded(t *testing.T) {
	ledger.SetTransferDailyLimit(1000)
	pool := testPool(t)
	ctx := context.Background()
	// Hard-delete any leftover data for these test-only user IDs to prevent cross-run contamination.
	pool.Exec(ctx, `DELETE FROM transactions WHERE from_user_id IN (71,72) OR to_user_id IN (71,72)`)
	pool.Exec(ctx, `DELETE FROM balance_snapshot WHERE user_id IN (71,72)`)
	pool.Exec(ctx, `DELETE FROM users WHERE user_id IN (71,72)`)
	addUser(t, pool, 71, 2000)
	addUser(t, pool, 72, 0)
	t.Cleanup(func() {
		pool.Exec(ctx, `DELETE FROM transactions WHERE from_user_id IN (71,72) OR to_user_id IN (71,72)`)
		pool.Exec(ctx, `DELETE FROM balance_snapshot WHERE user_id IN (71,72)`)
		pool.Exec(ctx, `DELETE FROM users WHERE user_id IN (71,72)`)
	})

	for i := 0; i < 10; i++ {
		var sent int64
		pool.QueryRow(ctx, `SELECT COALESCE(SUM(amount),0) FROM transactions WHERE reason='TRANSFER' AND from_user_id=71 AND created_at > now() - interval '24 hours'`).Scan(&sent)
		tx, _ := pool.Begin(ctx)
		err := ledger.Transfer(ctx, tx, ledger.User(71), ledger.User(72), 100, ledger.ReasonTransfer, "t7:limit:"+string([]byte{byte('a' + i)}))
		tx.Commit(ctx)
		if err != nil {
			t.Fatalf("T7: transfer %d (sent before=%d): %v", i, sent, err)
		}
	}
	tx, _ := pool.Begin(ctx)
	err := ledger.Transfer(ctx, tx, ledger.User(71), ledger.User(72), 1, ledger.ReasonTransfer, "t7:limit:overflow")
	tx.Rollback(ctx)
	if err == nil {
		t.Fatal("T7: expected TRANSFER_LIMIT error, got nil")
	}
}

// T8: Transfer blocked when sender has overdue loan.
func TestT8_TransferBlockedOnOverdueLoan(t *testing.T) {
	pool := testPool(t)
	resetUsers(t, pool)
	addUser(t, pool, 1, 1000)
	addUser(t, pool, 2, 0)

	ctx := context.Background()
	past := time.Now().UTC().Add(-48 * time.Hour)
	_, err := pool.Exec(ctx,
		`INSERT INTO loans (user_id, principal, term, rate_bps, due_at, status)
		 VALUES (1, 500, '24h', 100, $1, 'active')`, past)
	if err != nil {
		t.Fatalf("T8: insert loan: %v", err)
	}

	tx, _ := pool.Begin(ctx)
	err = ledger.Transfer(ctx, tx, ledger.User(1), ledger.User(2), 50, ledger.ReasonTransfer, "transfer:t8")
	tx.Rollback(ctx)
	if err == nil {
		t.Fatal("T8: expected LOAN_OVERDUE, got nil")
	}
}

// T13: Deadlock test — two opposite transfers in separate goroutines, both must succeed.
func TestT13_DeadlockOppositeTransfers(t *testing.T) {
	pool := testPool(t)
	resetUsers(t, pool)
	addUser(t, pool, 1, 500)
	addUser(t, pool, 2, 500)

	ctx := context.Background()
	var wg sync.WaitGroup
	errs := make([]error, 2)

	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			from, to := int64(1), int64(2)
			key := "transfer:dead:1to2"
			if idx == 1 {
				from, to = 2, 1
				key = "transfer:dead:2to1"
			}
			tx, err := pool.Begin(ctx)
			if err != nil {
				errs[idx] = err
				return
			}
			err = ledger.Transfer(ctx, tx,
				ledger.User(from), ledger.User(to),
				50, ledger.ReasonTransfer, key)
			if err != nil {
				tx.Rollback(ctx)
				errs[idx] = err
				return
			}
			errs[idx] = tx.Commit(ctx)
		}(i)
	}
	wg.Wait()

	for i, e := range errs {
		if e != nil {
			t.Errorf("T13: goroutine %d: %v", i, e)
		}
	}
	invariantMustZero(t, pool)
}
