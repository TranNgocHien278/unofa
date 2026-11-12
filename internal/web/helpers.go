package web

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/unofa/unofa/internal/errs"
	"github.com/unofa/unofa/internal/middleware"
)

const rfc3339 = time.RFC3339

// itoa keeps the hot paths free of fmt.
func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// parseID parses a positive integer path segment.
func parseID(raw string) (int64, error) {
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n <= 0 {
		return 0, errs.New(errs.NotFound)
	}
	return n, nil
}

// parseCursorTime parses the opaque RFC3339 cursor used by list endpoints.
func parseCursorTime(raw string) time.Time {
	if raw == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}

// writeRawJSON writes a value that already carries its own server_now field
// (the match snapshot, which the match goroutine owns).
func writeRawJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// middlewareRealIP is a thin alias so auth.go does not import middleware twice
// under two names.
func middlewareRealIP(r *http.Request) string {
	ip := middleware.RealIP(r)
	if ip == nil {
		return ""
	}
	return ip.String()
}

// mustJSON marshals a flag detail payload, falling back to an empty object.
func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return b
}
