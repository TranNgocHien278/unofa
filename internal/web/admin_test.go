package web_test

import (
	"net/http"
	"testing"

	"github.com/unofa/unofa/internal/session"
)

// TestAdminLogin_SuccessAndWrongPassword covers POST /api/admin/login.
func TestAdminLogin_SuccessAndWrongPassword(t *testing.T) {
	e := newEnv(t)
	ok := e.adminAPI(http.MethodPost, "/api/admin/login", "", map[string]any{
		"username": testAdminUser, "password": testAdminPass,
	})
	if ok.status != http.StatusNoContent || ok.cookie(session.AdminCookieName) == "" {
		t.Fatalf("admin login: status %d cookie=%q", ok.status, ok.cookie(session.AdminCookieName))
	}
	bad := e.adminAPI(http.MethodPost, "/api/admin/login", "", map[string]any{
		"username": testAdminUser, "password": "nope",
	})
	if bad.status != http.StatusUnauthorized || bad.errCode(t) != "INVALID_CREDENTIALS" {
		t.Fatalf("wrong admin pw: status %d code %q", bad.status, bad.errCode(t))
	}
}

// TestAdminAccounts_RequireAdminSession covers the admin auth gate on GET /api/admin/accounts.
func TestAdminAccounts_RequireAdminSession(t *testing.T) {
	e := newEnv(t)
	r := e.adminAPI(http.MethodGet, "/api/admin/accounts", "", nil)
	if r.status != http.StatusUnauthorized || r.errCode(t) != "UNAUTHENTICATED" {
		t.Fatalf("status %d code %q want 401 UNAUTHENTICATED", r.status, r.errCode(t))
	}
}

// TestAdminAccounts_ListsUsers covers GET /api/admin/accounts with cursor shape.
func TestAdminAccounts_ListsUsers(t *testing.T) {
	e := newEnv(t)
	e.register("alice")
	e.register("bob")
	ack := e.adminCookie()
	r := e.adminAPI(http.MethodGet, "/api/admin/accounts?limit=10", ack, nil)
	if r.status != http.StatusOK {
		t.Fatalf("status %d body %s", r.status, r.body)
	}
	m := r.json(t)
	items, _ := m["items"].([]any)
	// The System row (user 0) is listed too; the spec does not say to hide it.
	if len(items) != 3 {
		t.Errorf("accounts = %d want 3 (alice, bob, System)", len(items))
	}
	if _, ok := m["next_cursor"]; !ok {
		t.Error("next_cursor missing")
	}
}

// TestAdminAdjustBalance_AuditAndOneSnapshot covers POST /api/admin/accounts/{id}/balance.
// Must write an admin_audit row and exactly one non-zero balance_snapshot for the target.
func TestAdminAdjustBalance_AuditAndOneSnapshot(t *testing.T) {
	e := newEnv(t)
	_, uid := e.register("alice")
	ack := e.adminCookie()

	before := e.scalar(`SELECT count(*) FROM balance_snapshot WHERE user_id = $1`, uid)
	auditBefore := e.scalar(`SELECT count(*) FROM admin_audit WHERE target_user_id = $1`, uid)

	r := e.adminAPI(http.MethodPost, "/api/admin/accounts/"+fmtInt(uid)+"/balance", ack, map[string]any{"delta": 100})
	if r.status != http.StatusOK {
		t.Fatalf("adjust status %d body %s", r.status, r.body)
	}
	if num(t, r.json(t), "balance") != 300 {
		t.Errorf("balance = %v want 300", r.json(t)["balance"])
	}
	if auditAfter := e.scalar(`SELECT count(*) FROM admin_audit WHERE target_user_id = $1`, uid); auditAfter-auditBefore != 1 {
		t.Errorf("admin_audit rows added = %d want 1", auditAfter-auditBefore)
	}
	if delta := e.scalar(`SELECT count(*) FROM balance_snapshot WHERE user_id = $1`, uid) - before; delta != 1 {
		t.Errorf("balance_snapshot rows added by one adjust = %d want exactly 1", delta)
	}
	if latest := e.scalar(`SELECT balance FROM balance_snapshot WHERE user_id = $1 ORDER BY id DESC LIMIT 1`, uid); latest != 300 {
		t.Errorf("latest snapshot = %d want 300", latest)
	}
}

// TestAdminDeleteAccount covers DELETE /api/admin/accounts/{id} 204 and the scrub.
func TestAdminDeleteAccount(t *testing.T) {
	e := newEnv(t)
	_, uid := e.register("alice")
	ack := e.adminCookie()
	r := e.adminAPI(http.MethodDelete, "/api/admin/accounts/"+fmtInt(uid), ack, nil)
	if r.status != http.StatusNoContent {
		t.Fatalf("delete status %d body %s", r.status, r.body)
	}
	if n := e.scalar(`SELECT count(*) FROM users WHERE user_id = $1 AND deleted_at IS NOT NULL`, uid); n != 1 {
		t.Errorf("user not soft-deleted: %d", n)
	}
}

// TestAdminFlags_ListAndClear covers GET /api/admin/flags and clearing one.
func TestAdminFlags_ListAndClear(t *testing.T) {
	e := newEnv(t)
	ack := e.adminCookie()
	e.exec(`INSERT INTO admin_flags (kind, detail) VALUES ('TEST_FLAG', '{"n":1}')`)
	id := e.scalar(`SELECT id FROM admin_flags WHERE kind = 'TEST_FLAG'`)

	r := e.adminAPI(http.MethodGet, "/api/admin/flags", ack, nil)
	if r.status != http.StatusOK {
		t.Fatalf("flags status %d body %s", r.status, r.body)
	}
	items, _ := r.json(t)["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("open flags = %d want 1", len(items))
	}

	c := e.adminAPI(http.MethodPost, "/api/admin/flags/"+fmtInt(id)+"/clear", ack, nil)
	if c.status != http.StatusNoContent {
		t.Fatalf("clear status %d body %s", c.status, c.body)
	}
}

// TestAdminAudit_ListsEntries covers GET /api/admin/audit after a balance adjust.
func TestAdminAudit_ListsEntries(t *testing.T) {
	e := newEnv(t)
	_, uid := e.register("alice")
	ack := e.adminCookie()
	e.adminAPI(http.MethodPost, "/api/admin/accounts/"+fmtInt(uid)+"/balance", ack, map[string]any{"delta": 10})

	r := e.adminAPI(http.MethodGet, "/api/admin/audit?limit=20", ack, nil)
	if r.status != http.StatusOK {
		t.Fatalf("audit status %d body %s", r.status, r.body)
	}
	items, _ := r.json(t)["items"].([]any)
	if len(items) == 0 {
		t.Fatal("audit empty after an adjust")
	}
	first := items[0].(map[string]any)
	if first["action"] != "SYSTEM_ADD" || first["admin_username"] != testAdminUser {
		t.Errorf("latest audit row = %v", first)
	}
}
