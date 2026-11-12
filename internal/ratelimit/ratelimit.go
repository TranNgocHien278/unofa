// Package ratelimit implements the app-layer rate limits defined in
// docs/spec/04-http-api.md §Rate limit.
//
// Fixed-window counter via Redis MULTI/INCR/EXPIRE NX (one round-trip).
// nginx handles the outer HTTP and WS limits; this package only handles the
// app-level ones listed in §Lớp app.
package ratelimit

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/unofa/unofa/internal/errs"
)

// Limiter checks a named bucket against a fixed window.
type Limiter struct {
	rdb *redis.Client
}

func New(rdb *redis.Client) *Limiter { return &Limiter{rdb: rdb} }

// Allow returns nil if the action is within limits, or *errs.Error RATE_LIMITED
// with RetryAfter set to the window duration in seconds.
//
// group: short name used as the bucket prefix, e.g. "login_ip".
// key: the per-entity discriminator, e.g. the IP or username.
// limit: how many requests are allowed in the window.
// window: the fixed-window duration.
func (l *Limiter) Allow(ctx context.Context, group, key string, limit int, window time.Duration) error {
	ts := time.Now().UTC().Truncate(window).Unix()
	rkey := fmt.Sprintf("rl:%s:%s:%d", group, key, ts)
	seconds := int(window.Seconds())

	pipe := l.rdb.Pipeline()
	incrCmd := pipe.Incr(ctx, rkey)
	pipe.ExpireNX(ctx, rkey, window)
	if _, err := pipe.Exec(ctx); err != nil {
		// Redis down: fail open (spec 07 §Redis chết — do not refuse requests when
		// only rate limit is unavailable; the invariant and session layers will catch abuse).
		return nil
	}

	count := incrCmd.Val()
	if count > int64(limit) {
		return errs.NewRetry(errs.RateLimited, seconds)
	}
	return nil
}

// Count returns the current count for a bucket without incrementing it.
// Returns 0 when the key does not exist.
func (l *Limiter) Count(ctx context.Context, group, key string, window time.Duration) (int64, error) {
	ts := time.Now().UTC().Truncate(window).Unix()
	rkey := fmt.Sprintf("rl:%s:%s:%d", group, key, ts)
	val, err := l.rdb.Get(ctx, rkey).Result()
	if err != nil {
		return 0, nil
	}
	n, _ := strconv.ParseInt(val, 10, 64)
	return n, nil
}

// RetryAfterSeconds returns the number of seconds until the current window expires.
func RetryAfterSeconds(window time.Duration) int {
	now := time.Now().UTC()
	next := now.Truncate(window).Add(window)
	return int(next.Sub(now).Seconds()) + 1
}

// WriteRateLimited writes a 429 response with Retry-After header.
func WriteRateLimited(w http.ResponseWriter, seconds int) {
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusTooManyRequests)
	fmt.Fprintf(w, `{"error":"RATE_LIMITED","retry_after":%d}`, seconds)
}
