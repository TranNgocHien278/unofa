package store

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/unofa/unofa/internal/errs"
	"github.com/unofa/unofa/internal/ledger"
	"github.com/unofa/unofa/internal/loan"
)

func fundUser(t *testing.T, s *Store, userID, amount int64) {
	t.Helper()
	ctx := context.Background()
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		return ledger.Transfer(ctx, tx, ledger.System, ledger.User(userID), amount,
			ledger.ReasonSignupBonus, fmt.Sprintf("test:fund:%d", userID))
	})
	if err != nil {
		t.Fatalf("fund %d: %v", userID, err)
	}
}

// T11: delete keeps the users row, renames it deleted_{id}, invariant stays 0.
func TestT11_DeleteAccountKeepsRowAndRenames(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	u := newUser(t, s)
	fundUser(t, s, u.UserID, 100)

	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	now := time.Now()
	if _, err := loan.DeleteAccount(ctx, tx, u.UserID, now, loan.LoanCfg{OverdueBps: 500, CapMultiplier: 2}); err != nil {
		t.Fatalf("DeleteAccount: %v", err)
	}
	// Step 4 as the HTTP handler runs it (internal/web/admin.go).
	if err := s.InTx(tx).DeletePersonalData(ctx, u.UserID); err != nil {
		t.Fatalf("DeletePersonalData: %v", err)
	}
	sum, err := ledger.Invariant(ctx, tx)
	if err != nil {
		t.Fatalf("Invariant: %v", err)
	}
	if sum != 0 {
		t.Errorf("invariant = %d, want 0", sum)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetByID(ctx, u.UserID)
	if err != nil {
		t.Fatalf("row gone: %v", err)
	}
	if want := fmt.Sprintf("deleted_%d", u.UserID); got.Username != want {
		t.Errorf("username = %q, want %q", got.Username, want)
	}
	if got.Balance != 0 {
		t.Errorf("balance = %d, want 0", got.Balance)
	}
}

// T12: deleting an account while it is in a running match is refused.
func TestT12_DeleteAccountInRunningMatchRefused(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	u := newUser(t, s)
	room, err := s.CreateRoom(ctx, u.UserID, 50)
	if err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	m, err := s.CreateMatch(ctx, room.RoomID, 50)
	if err != nil {
		t.Fatalf("CreateMatch: %v", err)
	}
	if err := s.AddMatchPlayers(ctx, m.MatchID, []int64{u.UserID}, []int{0}); err != nil {
		t.Fatalf("AddMatchPlayers: %v", err)
	}

	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	_, err = loan.DeleteAccount(ctx, tx, u.UserID, time.Now(), loan.LoanCfg{OverdueBps: 500, CapMultiplier: 2})
	wantCode(t, err, errs.UserInMatch)
}

// T14: the System row can never be used to log in. The bcrypt/hash check lives
// in internal/web (auth.go), which cannot be imported here without an import
// cycle, so this asserts the reachable half: no usable credential row exists.
func TestT14_SystemCannotAuthenticate(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	u, err := s.GetByUsername(ctx, "System")
	if err == nil {
		if u.UserID == 0 && u.PasswordHash != nil {
			t.Fatalf("System row yields usable credential: %+v", u)
		}
	} else {
		wantCode(t, err, errs.UserNotFound)
	}

	sys, err := s.GetByID(ctx, 0)
	if err != nil {
		t.Fatalf("GetByID(0): %v", err)
	}
	if sys.PasswordHash != nil {
		t.Fatalf("System has password_hash %q", *sys.PasswordHash)
	}

	if _, err := NormalizeUsername("System"); err == nil {
		t.Fatal("login-side NormalizeUsername accepted System")
	} else {
		wantCode(t, err, errs.UsernameInvalid)
	}
}

// T15: reserved usernames are rejected at registration.
func TestT15_ReservedUsernamesRejected(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	for _, name := range []string{"System", "SYSTEM", "deleted_1"} {
		if _, err := s.CreateUser(ctx, name, "h", 1, 0); err == nil {
			t.Errorf("%q accepted", name)
		} else {
			wantCode(t, err, errs.UsernameInvalid)
		}
	}
}
