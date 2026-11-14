package web_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/unofa/unofa/internal/session"
)

// tokenOf strips the cookie name and "=" from a cookie pair, giving the Redis key suffix.
func tokenOf(cookie string) string {
	return strings.TrimPrefix(cookie, session.GameCookieName+"=")
}

// TestRegister_SuccessShapeAndCookie covers POST /api/register 201 (04 §Auth).
func TestRegister_SuccessShapeAndCookie(t *testing.T) {
	e := newEnv(t)
	r := e.api(http.MethodPost, "/api/register", "", map[string]any{
		"username": "alice", "password": testPassword, "terms_accepted": true,
	})
	if r.status != http.StatusCreated {
		t.Fatalf("status %d body %s", r.status, r.body)
	}
	m := r.json(t)
	if m["username"] != "alice" {
		t.Errorf("username = %v want alice", m["username"])
	}
	if got := num(t, m, "balance"); got != 200 {
		t.Errorf("balance = %d want 200 (signup bonus)", got)
	}
	if _, ok := m["user_id"]; !ok {
		t.Error("user_id missing from body")
	}
	if _, ok := m["server_now"]; !ok {
		t.Error("server_now missing from body")
	}
	if _, ok := m["password"]; ok {
		t.Error("password must never be echoed")
	}
	found := false
	for _, c := range r.cookies {
		if c.Name != session.GameCookieName {
			continue
		}
		found = true
		if !c.Secure || !c.HttpOnly || c.Path != "/" || c.Value == "" {
			t.Errorf("cookie attrs wrong: secure=%v httponly=%v path=%q", c.Secure, c.HttpOnly, c.Path)
		}
	}
	if !found {
		t.Fatalf("no %s cookie in response", session.GameCookieName)
	}
}

// TestRegister_DuplicateUsernameIsUsernameTaken covers 409 USERNAME_TAKEN.
func TestRegister_DuplicateUsernameIsUsernameTaken(t *testing.T) {
	e := newEnv(t)
	e.register("alice")
	r := e.api(http.MethodPost, "/api/register", "", map[string]any{
		"username": "alice", "password": testPassword, "terms_accepted": true,
	})
	if r.status != http.StatusConflict || r.errCode(t) != "USERNAME_TAKEN" {
		t.Fatalf("status %d code %q want 409 USERNAME_TAKEN", r.status, r.errCode(t))
	}
}

// TestRegister_ReservedUsernamesRejected covers USERNAME_INVALID for System and deleted_ prefix.
func TestRegister_ReservedUsernamesRejected(t *testing.T) {
	e := newEnv(t)
	for _, name := range []string{"system", "System", "deleted_7"} {
		r := e.api(http.MethodPost, "/api/register", "", map[string]any{
			"username": name, "password": testPassword, "terms_accepted": true,
		})
		if r.status != http.StatusBadRequest || r.errCode(t) != "USERNAME_INVALID" {
			t.Errorf("%s: status %d code %q want 400 USERNAME_INVALID", name, r.status, r.errCode(t))
		}
	}
}

// TestRegister_BodyErrors covers terms, password length and unknown-field body errors.
func TestRegister_BodyErrors(t *testing.T) {
	e := newEnv(t)
	cases := []struct {
		name string
		body map[string]any
		code string
	}{
		{"terms not accepted", map[string]any{"username": "bob", "password": testPassword, "terms_accepted": false}, "TERMS_NOT_ACCEPTED"},
		{"password too short", map[string]any{"username": "bob", "password": "short", "terms_accepted": true}, "PASSWORD_TOO_SHORT"},
		{"unknown field", map[string]any{"username": "bob", "password": testPassword, "terms_accepted": true, "extra": 1}, ""},
	}
	for _, c := range cases {
		r := e.api(http.MethodPost, "/api/register", "", c.body)
		if r.status != http.StatusBadRequest {
			t.Errorf("%s: status %d want 400", c.name, r.status)
		}
		if c.code != "" && r.errCode(t) != c.code {
			t.Errorf("%s: code %q want %s", c.name, r.errCode(t), c.code)
		}
	}
}

// TestLogin_SuccessReturnsShapeAndCookie covers POST /api/login 200.
func TestLogin_SuccessReturnsShapeAndCookie(t *testing.T) {
	e := newEnv(t)
	_, uid := e.register("alice")
	r := e.api(http.MethodPost, "/api/login", "", map[string]any{
		"username": "alice", "password": testPassword,
	})
	if r.status != http.StatusOK {
		t.Fatalf("status %d body %s", r.status, r.body)
	}
	m := r.json(t)
	if num(t, m, "user_id") != uid {
		t.Errorf("user_id = %v want %d", m["user_id"], uid)
	}
	if m["username"] != "alice" || m["locale"] != "en" {
		t.Errorf("username/locale = %v/%v", m["username"], m["locale"])
	}
	if num(t, m, "terms_version") != 1 {
		t.Errorf("terms_version = %v want 1", m["terms_version"])
	}
	if r.cookie(session.GameCookieName) == "" {
		t.Fatal("no session cookie on login")
	}
}

// TestLogin_WrongPasswordIsInvalidCredentials covers 401 INVALID_CREDENTIALS.
func TestLogin_WrongPasswordIsInvalidCredentials(t *testing.T) {
	e := newEnv(t)
	e.register("alice")
	r := e.api(http.MethodPost, "/api/login", "", map[string]any{
		"username": "alice", "password": "wrong-password",
	})
	if r.status != http.StatusUnauthorized || r.errCode(t) != "INVALID_CREDENTIALS" {
		t.Fatalf("status %d code %q want 401 INVALID_CREDENTIALS", r.status, r.errCode(t))
	}
	if r.cookie(session.GameCookieName) != "" {
		t.Error("failed login must not set a cookie")
	}
}

// TestLogin_SystemAccountCannotLogin is the HTTP half of T14: user 0 has no password.
func TestLogin_SystemAccountCannotLogin(t *testing.T) {
	e := newEnv(t)
	for _, name := range []string{"System", "system"} {
		for _, pw := range []string{"", testPassword} {
			r := e.api(http.MethodPost, "/api/login", "", map[string]any{
				"username": name, "password": pw,
			})
			if r.status != http.StatusUnauthorized || r.errCode(t) != "INVALID_CREDENTIALS" {
				t.Errorf("login %q/%q: status %d code %q want 401 INVALID_CREDENTIALS",
					name, pw, r.status, r.errCode(t))
			}
			if r.cookie(session.GameCookieName) != "" {
				t.Errorf("login %q set a session cookie", name)
			}
		}
	}
}

// TestAuthGate_ProtectedEndpointsRequireCookie covers 401 UNAUTHENTICATED with no or bad cookie.
func TestAuthGate_ProtectedEndpointsRequireCookie(t *testing.T) {
	e := newEnv(t)
	endpoints := []struct{ method, path string }{
		{http.MethodGet, "/api/me"},
		{http.MethodPatch, "/api/me"},
		{http.MethodPost, "/api/transfers"},
		{http.MethodGet, "/api/transactions"},
		{http.MethodGet, "/api/me/balance-chart"},
		{http.MethodGet, "/api/me/matches"},
		{http.MethodGet, "/api/loans/products"},
		{http.MethodPost, "/api/loans"},
		{http.MethodGet, "/api/loans/active"},
		{http.MethodPost, "/api/loans/1/repay"},
		{http.MethodPost, "/api/rooms"},
		{http.MethodPost, "/api/rooms/ABCDEF/join"},
		{http.MethodPost, "/api/rooms/ABCDEF/leave"},
		{http.MethodPost, "/api/rooms/ABCDEF/ready"},
		{http.MethodPost, "/api/rooms/ABCDEF/stake"},
		{http.MethodPost, "/api/rooms/ABCDEF/kick"},
		{http.MethodPost, "/api/rooms/ABCDEF/play"},
		{http.MethodGet, "/api/rooms/ABCDEF"},
		{http.MethodGet, "/api/matches/m1/state"},
	}
	for _, cookie := range []string{"", session.GameCookieName + "=bogus-token"} {
		for _, ep := range endpoints {
			r := e.api(ep.method, ep.path, cookie, nil)
			if r.status != http.StatusUnauthorized || r.errCode(t) != "UNAUTHENTICATED" {
				t.Errorf("%s %s cookie=%q: status %d code %q want 401 UNAUTHENTICATED",
					ep.method, ep.path, cookie, r.status, r.errCode(t))
			}
		}
	}
}

// TestHeartbeat_RenewsSessionTTL covers POST /api/session/heartbeat 204 and TTL renewal.
func TestHeartbeat_RenewsSessionTTL(t *testing.T) {
	e := newEnv(t)
	cookie, _ := e.register("alice")
	key := "session:" + tokenOf(cookie)
	if err := e.rdb.Expire(t.Context(), key, 5*time.Second).Err(); err != nil {
		t.Fatalf("shorten ttl: %v", err)
	}
	r := e.api(http.MethodPost, "/api/session/heartbeat", cookie, nil)
	if r.status != http.StatusNoContent {
		t.Fatalf("heartbeat status %d body %s", r.status, r.body)
	}
	ttl, err := e.rdb.TTL(t.Context(), key).Result()
	if err != nil {
		t.Fatalf("ttl: %v", err)
	}
	if ttl < 5*time.Minute {
		t.Errorf("ttl after heartbeat = %v, want renewed to about 15m", ttl)
	}
}

// TestHeartbeat_WithoutSessionIsSessionExpired covers 401 SESSION_EXPIRED.
func TestHeartbeat_WithoutSessionIsSessionExpired(t *testing.T) {
	e := newEnv(t)
	r := e.api(http.MethodPost, "/api/session/heartbeat", "", nil)
	if r.status != http.StatusUnauthorized || r.errCode(t) != "SESSION_EXPIRED" {
		t.Fatalf("status %d code %q want 401 SESSION_EXPIRED", r.status, r.errCode(t))
	}
}

// TestLogout_InvalidatesCookie covers POST /api/logout 204 then 401 on the next call.
func TestLogout_InvalidatesCookie(t *testing.T) {
	e := newEnv(t)
	cookie, _ := e.register("alice")
	r := e.api(http.MethodPost, "/api/logout", cookie, nil)
	if r.status != http.StatusNoContent {
		t.Fatalf("logout status %d body %s", r.status, r.body)
	}
	if r.cookie(session.GameCookieName) != "" {
		t.Error("logout must clear the cookie")
	}
	after := e.api(http.MethodGet, "/api/me", cookie, nil)
	if after.status != http.StatusUnauthorized || after.errCode(t) != "UNAUTHENTICATED" {
		t.Fatalf("after logout: status %d code %q want 401 UNAUTHENTICATED", after.status, after.errCode(t))
	}
}
