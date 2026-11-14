// Package middleware provides HTTP middleware for the UNOFA game server.
// Handler construction takes a Deps struct so tests can inject a fake clock,
// a no-op log writer, and a stub Redis client without starting real services.
package middleware

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/unofa/unofa/internal/errs"
)

// requestIDKey is the context key for the request-id string.
type requestIDKey struct{}

// RequestID returns the request-id attached to ctx by RequestIDMiddleware.
func RequestID(ctx context.Context) string {
	v, _ := ctx.Value(requestIDKey{}).(string)
	return v
}

// newRequestID generates a short random ID for http_log.request_id.
// Format: 8 lowercase hex chars, cheap, unguessable enough for correlation.
func newRequestID() string {
	return fmt.Sprintf("%08x%08x", rand.Uint32(), rand.Uint32())
}

// RequestIDMiddleware mints a request-id and injects it into the context.
func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rid := newRequestID()
		ctx := context.WithValue(r.Context(), requestIDKey{}, rid)
		w.Header().Set("X-Request-ID", rid)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RealIP extracts the real client IP from X-Forwarded-For when behind a trusted
// proxy. This must only be used when nginx is the front-end (docs/spec/07 §nginx);
// on a direct connection it is spoofable.
func RealIP(r *http.Request) net.IP {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Take the first address: nginx appends the connecting IP; the first entry
		// is set by the original client and already validated upstream by nginx.
		host, _, err := net.SplitHostPort(xff)
		if err != nil {
			host = xff
		}
		ip := net.ParseIP(host)
		if ip != nil {
			return ip
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return net.ParseIP(r.RemoteAddr)
	}
	return net.ParseIP(host)
}

// ErrorResponse is the unified error envelope (04 §Response lỗi).
// Never exposes technical details; the client translates the code.
type ErrorResponse struct {
	Error     string `json:"error"`
	RequestID string `json:"request_id"`
}

// WriteError writes the standard error JSON body.
// If e has RetryAfter > 0 the Retry-After header is also set.
func WriteError(w http.ResponseWriter, r *http.Request, e *errs.Error) {
	rid := RequestID(r.Context())
	status := errs.Status(e.Code)
	if e.RetryAfter > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(e.RetryAfter))
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	body, _ := json.Marshal(ErrorResponse{
		Error:     string(e.Code),
		RequestID: rid,
	})
	_, _ = w.Write(body)
}

// WriteJSON writes a 200/201 JSON body with server_now attached.
func WriteJSON(w http.ResponseWriter, status int, now time.Time, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	// Wrap in an envelope that adds server_now. We marshal v first, then
	// re-open it as a map so we can inject the extra field.
	raw, err := json.Marshal(v)
	if err != nil {
		return
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) == nil {
		m["server_now"] = now.UTC().Format(time.RFC3339)
		if out, err := json.Marshal(m); err == nil {
			_, _ = w.Write(out)
			return
		}
	}
	_, _ = w.Write(raw)
}

// LogEntry is one structured line written to the async http_log writer.
type LogEntry struct {
	RequestID  string    `json:"request_id"`
	Endpoint   string    `json:"endpoint"` // route pattern, not the actual URL
	StatusCode int       `json:"status_code"`
	IP         string    `json:"ip,omitempty"`
	UserID     *int64    `json:"user_id,omitempty"`
	At         time.Time `json:"at"`
	DurMs      int64     `json:"dur_ms"`
}

// LogWriter is accepted by LoggingMiddleware.
type LogWriter interface {
	// Write enqueues one entry. Must never block; drop if full.
	Write(e LogEntry)
}

// UserIDFromContext returns the authenticated user-id placed by SessionMiddleware.
func UserIDFromContext(ctx context.Context) (int64, bool) {
	v, ok := ctx.Value(userIDKey{}).(int64)
	return v, ok
}

type userIDKey struct{}

// WithUserID attaches a user-id to a context (called by SessionMiddleware after
// looking up the session in Redis).
func WithUserID(ctx context.Context, uid int64) context.Context {
	return context.WithValue(ctx, userIDKey{}, uid)
}

// RoutePatternKey is used by the router to stash the matched route pattern so the
// log middleware records /api/matches/{id}/state instead of the real URL.
type routePatternKey struct{}

func WithRoutePattern(ctx context.Context, pattern string) context.Context {
	return context.WithValue(ctx, routePatternKey{}, pattern)
}

func RoutePattern(ctx context.Context) string {
	v, _ := ctx.Value(routePatternKey{}).(string)
	return v
}

// LoggingMiddleware records one async LogEntry per request (04 §rate limit,
// 07 §Admin §Metric). Skips /healthz (07 §/healthz) and static assets.
func LoggingMiddleware(lw LogWriter, logger *slog.Logger, skipPrefixes []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for _, p := range skipPrefixes {
				if len(r.URL.Path) >= len(p) && r.URL.Path[:len(p)] == p {
					next.ServeHTTP(w, r)
					return
				}
			}

			start := time.Now()
			rw := &statusWriter{ResponseWriter: w, code: http.StatusOK}
			next.ServeHTTP(rw, r)
			dur := time.Since(start)

			rid := RequestID(r.Context())
			pattern := RoutePattern(r.Context())
			if pattern == "" {
				pattern = r.URL.Path
			}

			ip := RealIP(r)
			ipStr := ""
			if ip != nil {
				ipStr = ip.String()
			}

			uid, hasUID := UserIDFromContext(r.Context())
			entry := LogEntry{
				RequestID:  rid,
				Endpoint:   pattern,
				StatusCode: rw.code,
				IP:         ipStr,
				At:         time.Now().UTC(),
				DurMs:      dur.Milliseconds(),
			}
			if hasUID {
				entry.UserID = &uid
			}

			lw.Write(entry)

			// Also emit a slog line for real-time ops visibility.
			logger.Info("request",
				slog.String("request_id", rid),
				slog.String("method", r.Method),
				slog.String("route", pattern),
				slog.Int("status", rw.code),
				slog.Int64("dur_ms", dur.Milliseconds()),
				slog.String("ip", ipStr),
			)
		})
	}
}

// statusWriter captures the status code written by the handler.
type statusWriter struct {
	http.ResponseWriter
	code    int
	written bool
}

func (rw *statusWriter) WriteHeader(code int) {
	if !rw.written {
		rw.code = code
		rw.written = true
	}
	rw.ResponseWriter.WriteHeader(code)
}

// Hijack lets gorilla/websocket's Upgrader take over the connection; without
// it the upgrade fails with 500. A hijacked response never goes through
// WriteHeader, so record 101 here for http_log.
func (rw *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := rw.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("middleware: underlying ResponseWriter does not implement http.Hijacker")
	}
	if !rw.written {
		rw.code = http.StatusSwitchingProtocols
		rw.written = true
	}
	return h.Hijack()
}

var _ http.Hijacker = (*statusWriter)(nil)
