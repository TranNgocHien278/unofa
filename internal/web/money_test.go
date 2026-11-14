package web_test

import (
	"net/http"
	"testing"

	"github.com/unofa/unofa/internal/config"
)

// TestTransfer_HappyPathMovesMoneyAndMatchesDB covers POST /api/transfers 201.
func TestTransfer_HappyPathMovesMoneyAndMatchesDB(t *testing.T) {
	e := newEnv(t)
	aliceCk, alice := e.register("alice")
	_, bob := e.register("bob")

	r := e.api(http.MethodPost, "/api/transfers", aliceCk, map[string]any{
		"to_username": "bob", "amount": 50, "client_uuid": "t-happy-1",
	})
	if r.status != http.StatusCreated {
		t.Fatalf("status %d body %s", r.status, r.body)
	}
	m := r.json(t)
	if num(t, m, "amount") != 50 || m["to_username"] != "bob" {
		t.Errorf("body amount/to = %v/%v", m["amount"], m["to_username"])
	}
	after := num(t, m, "balance_after")
	if after != 150 {
		t.Errorf("balance_after = %d want 150", after)
	}
	if db := e.balance(alice); db != after {
		t.Errorf("DB alice balance %d != balance_after %d", db, after)
	}
	if db := e.balance(bob); db != 250 {
		t.Errorf("DB bob balance %d want 250", db)
	}
}

// TestTransfer_InsufficientBalance covers 400 INSUFFICIENT_BALANCE.
func TestTransfer_InsufficientBalance(t *testing.T) {
	e := newEnv(t)
	aliceCk, alice := e.register("alice")
	e.register("bob")
	r := e.api(http.MethodPost, "/api/transfers", aliceCk, map[string]any{
		"to_username": "bob", "amount": 201, "client_uuid": "t-over-1",
	})
	if r.status != http.StatusBadRequest || r.errCode(t) != "INSUFFICIENT_BALANCE" {
		t.Fatalf("status %d code %q want 400 INSUFFICIENT_BALANCE", r.status, r.errCode(t))
	}
	if db := e.balance(alice); db != 200 {
		t.Errorf("failed transfer moved money: alice=%d", db)
	}
}

// TestTransfer_SelfTransferRejected covers 400 SELF_TRANSFER.
func TestTransfer_SelfTransferRejected(t *testing.T) {
	e := newEnv(t)
	aliceCk, _ := e.register("alice")
	r := e.api(http.MethodPost, "/api/transfers", aliceCk, map[string]any{
		"to_username": "alice", "amount": 10, "client_uuid": "t-self-1",
	})
	if r.status != http.StatusBadRequest || r.errCode(t) != "SELF_TRANSFER" {
		t.Fatalf("status %d code %q want 400 SELF_TRANSFER", r.status, r.errCode(t))
	}
}

// TestTransfer_MissingClientUUIDRejectedAsClientError checks a missing idempotency key.
// Spec has no dedicated code; the handler must answer as a client error, not 500.
func TestTransfer_MissingClientUUIDRejectedAsClientError(t *testing.T) {
	e := newEnv(t)
	aliceCk, _ := e.register("alice")
	e.register("bob")
	r := e.api(http.MethodPost, "/api/transfers", aliceCk, map[string]any{
		"to_username": "bob", "amount": 10,
	})
	if r.status >= 400 && r.status < 500 {
		return
	}
	t.Fatalf("missing client_uuid: status %d code %q want a 4xx client error", r.status, r.errCode(t))
}

// TestTransfer_WritesExactlyOneNonZeroSnapshot is the duplicate-snapshot regression:
// one transfer must add one balance_snapshot row for the sender, carrying the new balance.
func TestTransfer_WritesExactlyOneNonZeroSnapshot(t *testing.T) {
	e := newEnv(t)
	aliceCk, alice := e.register("alice")
	e.register("bob")

	before := e.scalar(`SELECT count(*) FROM balance_snapshot WHERE user_id = $1`, alice)
	r := e.api(http.MethodPost, "/api/transfers", aliceCk, map[string]any{
		"to_username": "bob", "amount": 50, "client_uuid": "t-snap-1",
	})
	if r.status != http.StatusCreated {
		t.Fatalf("status %d body %s", r.status, r.body)
	}
	after := e.scalar(`SELECT count(*) FROM balance_snapshot WHERE user_id = $1`, alice)
	if delta := after - before; delta != 1 {
		t.Errorf("sender balance_snapshot rows added by one transfer = %d, want exactly 1", delta)
	}
	latest := e.scalar(`SELECT balance FROM balance_snapshot WHERE user_id = $1 ORDER BY id DESC LIMIT 1`, alice)
	if latest == 0 {
		t.Errorf("latest snapshot balance is 0, want non-zero (balance_after %d)", num(t, r.json(t), "balance_after"))
	}
	if latest != 150 {
		t.Errorf("latest snapshot balance = %d want 150", latest)
	}
}

// TestTransactions_CursorPagingShape covers GET /api/transactions {items, next_cursor}.
func TestTransactions_CursorPagingShape(t *testing.T) {
	e := newEnv(t)
	aliceCk, alice := e.register("alice")
	e.register("bob")
	for i, uuid := range []string{"p-1", "p-2"} {
		_ = i
		r := e.api(http.MethodPost, "/api/transfers", aliceCk, map[string]any{
			"to_username": "bob", "amount": 10, "client_uuid": uuid,
		})
		if r.status != http.StatusCreated {
			t.Fatalf("transfer %s: status %d", uuid, r.status)
		}
	}
	total := e.scalar(`SELECT count(*) FROM transactions WHERE from_user_id = $1 OR to_user_id = $1`, alice)

	seen := map[float64]bool{}
	cursor := ""
	pages := 0
	for {
		path := "/api/transactions?limit=2"
		if cursor != "" {
			path += "&before=" + cursor
		}
		r := e.api(http.MethodGet, path, aliceCk, nil)
		if r.status != http.StatusOK {
			t.Fatalf("page %d: status %d body %s", pages, r.status, r.body)
		}
		m := r.json(t)
		items, ok := m["items"].([]any)
		if !ok {
			t.Fatalf("items missing or not an array: %v", m["items"])
		}
		if pages == 0 && len(items) != 2 {
			t.Errorf("first page has %d items, want 2", len(items))
		}
		for _, it := range items {
			id := it.(map[string]any)["transaction_id"].(float64)
			if seen[id] {
				t.Errorf("duplicate transaction_id %v across pages", id)
			}
			seen[id] = true
		}
		pages++
		next, _ := m["next_cursor"].(float64)
		if m["next_cursor"] == nil || pages > 10 {
			break
		}
		cursor = fmtInt(int64(next))
	}
	if int64(len(seen)) != total {
		t.Errorf("paged %d distinct transactions, DB has %d", len(seen), total)
	}
}

// TestBalanceChart_NoReplayedZeroRow covers GET /api/me/balance-chart after a transfer.
func TestBalanceChart_NoReplayedZeroRow(t *testing.T) {
	e := newEnv(t)
	aliceCk, _ := e.register("alice")
	e.register("bob")
	r := e.api(http.MethodPost, "/api/transfers", aliceCk, map[string]any{
		"to_username": "bob", "amount": 50, "client_uuid": "chart-1",
	})
	if r.status != http.StatusCreated {
		t.Fatalf("transfer status %d", r.status)
	}
	chart := e.api(http.MethodGet, "/api/me/balance-chart?range=today", aliceCk, nil)
	if chart.status != http.StatusOK {
		t.Fatalf("balance-chart status %d body %s", chart.status, chart.body)
	}
	points, _ := chart.json(t)["points"].([]any)
	if len(points) == 0 {
		t.Fatal("chart has no points after a transfer")
	}
	for i, p := range points {
		if b := num(t, p.(map[string]any), "balance"); b == 0 {
			t.Errorf("point %d has balance 0: replayed zero row", i)
		}
	}
}

// TestTransfer_RateLimitReturns429WithRetryAfter covers 429 RATE_LIMITED with Retry-After.
func TestTransfer_RateLimitReturns429WithRetryAfter(t *testing.T) {
	e := newEnv(t, func(c *config.Config) {
		c.Rate.MoneyPerUser = config.Window{Limit: 1, Period: 60_000_000_000} // 1 per minute
	})
	aliceCk, _ := e.register("alice")
	e.register("bob")
	first := e.api(http.MethodPost, "/api/transfers", aliceCk, map[string]any{
		"to_username": "bob", "amount": 1, "client_uuid": "rl-1",
	})
	if first.status != http.StatusCreated {
		t.Fatalf("first transfer status %d body %s", first.status, first.body)
	}
	second := e.api(http.MethodPost, "/api/transfers", aliceCk, map[string]any{
		"to_username": "bob", "amount": 1, "client_uuid": "rl-2",
	})
	if second.status != http.StatusTooManyRequests || second.errCode(t) != "RATE_LIMITED" {
		t.Fatalf("second status %d code %q want 429 RATE_LIMITED", second.status, second.errCode(t))
	}
	if ra := second.header.Get("Retry-After"); ra == "" {
		t.Error("429 without Retry-After header")
	}
}
