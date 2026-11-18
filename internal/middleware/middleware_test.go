package middleware

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestWriteJSONNull(t *testing.T) {
	w := httptest.NewRecorder()
	WriteJSON(w, 200, time.Unix(0, 0), nil)
	if got := w.Body.String(); got != "null" {
		t.Fatalf("body = %q, want null", got)
	}
}

func TestWriteJSONAddsServerNow(t *testing.T) {
	w := httptest.NewRecorder()
	WriteJSON(w, 200, time.Unix(0, 0), map[string]any{"a": 1})
	if got := w.Body.String(); got != `{"a":1,"server_now":"1970-01-01T00:00:00Z"}` {
		t.Fatalf("body = %q", got)
	}
}

type captureLog struct{ got []LogEntry }

func (c *captureLog) Write(e LogEntry) { c.got = append(c.got, e) }

func TestLoggingRecordsRoutePattern(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/matches/{id}/state", func(http.ResponseWriter, *http.Request) {})
	lw := &captureLog{}
	h := LoggingMiddleware(lw, slog.New(slog.DiscardHandler), nil)(mux)
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/matches/abc-123/state", nil))
	if len(lw.got) != 1 || lw.got[0].Endpoint != "GET /api/matches/{id}/state" {
		t.Fatalf("logged %+v", lw.got)
	}
}
