// Package session manages player and admin sessions in Redis.
// Sessions are keyed by a random token stored in an HttpOnly cookie.
// Design: docs/spec/04-http-api.md §Auth, docs/spec/07-ops-admin.md §Admin.
package session

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	GameCookieName  = "__Host-unofa_sid"
	AdminCookieName = "__Host-unofa_admin_sid"
	gameKeyPrefix   = "session:"
	adminKeyPrefix  = "admin:session:"
)

// ErrNotFound means the session key does not exist in Redis or is expired.
var ErrNotFound = errors.New("session not found")

// Manager creates and validates sessions in Redis.
type Manager struct {
	rdb        *redis.Client
	ttl        time.Duration
	cookieName string
	keyPrefix  string
}

// NewGame returns a session manager for player sessions.
func NewGame(rdb *redis.Client, ttl time.Duration) *Manager {
	return &Manager{rdb: rdb, ttl: ttl, cookieName: GameCookieName, keyPrefix: gameKeyPrefix}
}

// NewAdmin returns a session manager for admin sessions.
func NewAdmin(rdb *redis.Client, ttl time.Duration) *Manager {
	return &Manager{rdb: rdb, ttl: ttl, cookieName: AdminCookieName, keyPrefix: adminKeyPrefix}
}

// Create mints a session token for userID (player) or username (admin), sets the
// cookie on w, and stores the value in Redis with TTL.
func (m *Manager) Create(ctx context.Context, w http.ResponseWriter, value string) (string, error) {
	token, err := randomToken()
	if err != nil {
		return "", err
	}
	key := m.keyPrefix + token
	if err := m.rdb.Set(ctx, key, value, m.ttl).Err(); err != nil {
		return "", fmt.Errorf("session create: %w", err)
	}
	http.SetCookie(w, m.cookie(token))
	return token, nil
}

// CreateForUser stores user_id as the session value.
func (m *Manager) CreateForUser(ctx context.Context, w http.ResponseWriter, userID int64) error {
	_, err := m.Create(ctx, w, strconv.FormatInt(userID, 10))
	return err
}

// Get reads the session token from the request cookie and returns the stored value.
// Returns ErrNotFound when the cookie is absent or the Redis key is gone/expired.
func (m *Manager) Get(ctx context.Context, r *http.Request) (string, error) {
	c, err := r.Cookie(m.cookieName)
	if err != nil {
		return "", ErrNotFound
	}
	key := m.keyPrefix + c.Value
	val, err := m.rdb.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return "", ErrNotFound
	}
	return val, err
}

// GetUserID returns the authenticated player's id from the session.
func (m *Manager) GetUserID(ctx context.Context, r *http.Request) (int64, error) {
	val, err := m.Get(ctx, r)
	if err != nil {
		return 0, err
	}
	id, err := strconv.ParseInt(val, 10, 64)
	if err != nil || id <= 0 {
		return 0, ErrNotFound
	}
	return id, nil
}

// Renew extends the TTL of an existing session (called by the heartbeat endpoint).
func (m *Manager) Renew(ctx context.Context, r *http.Request) error {
	c, err := r.Cookie(m.cookieName)
	if err != nil {
		return ErrNotFound
	}
	key := m.keyPrefix + c.Value
	ok, err := m.rdb.Expire(ctx, key, m.ttl).Result()
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotFound
	}
	return nil
}

// Delete removes the session from Redis and clears the cookie.
func (m *Manager) Delete(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(m.cookieName); err == nil {
		m.rdb.Del(ctx, m.keyPrefix+c.Value)
	}
	expired := m.cookie("")
	expired.MaxAge = -1
	http.SetCookie(w, expired)
}

// DeleteByToken removes a specific token (called when password changes or account deleted).
func (m *Manager) DeleteByToken(ctx context.Context, token string) error {
	return m.rdb.Del(ctx, m.keyPrefix+token).Err()
}

// DeleteAllForUser is a no-op at the Redis layer: since there is at most one session
// per user (D3), we look up by scanning for sessions matching the user's id.
// In the MVP this is called only on password change; a full scan is acceptable because
// it only touches the session keyspace. When scale requires it, store a user→token
// reverse index instead.
func (m *Manager) DeleteAllForUser(ctx context.Context, userID int64) error {
	val := strconv.FormatInt(userID, 10)
	var cursor uint64
	for {
		var keys []string
		var err error
		keys, cursor, err = m.rdb.Scan(ctx, cursor, m.keyPrefix+"*", 100).Result()
		if err != nil {
			return err
		}
		for _, k := range keys {
			got, err := m.rdb.Get(ctx, k).Result()
			if err != nil {
				continue
			}
			if got == val {
				m.rdb.Del(ctx, k)
			}
		}
		if cursor == 0 {
			break
		}
	}
	return nil
}

func (m *Manager) cookie(token string) *http.Cookie {
	return &http.Cookie{
		Name:     m.cookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(m.ttl.Seconds()),
	}
}

func randomToken() (string, error) {
	b := make([]byte, 24) // 192 bits → 32 base64url chars
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}
