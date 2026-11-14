package web_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/unofa/unofa/internal/middleware"
	"github.com/unofa/unofa/internal/store"
	"github.com/unofa/unofa/internal/web"
)

// TestHealthz_OKAndNotLogged covers GET /healthz: 200 with db/redis ok, no http_log row.
// The router is wrapped the way cmd/unofa wraps it, so the skip list is actually exercised.
func TestHealthz_OKAndNotLogged(t *testing.T) {
	e := newEnv(t)
	w := e.st.NewAsyncWriter()
	logged := middleware.RequestIDMiddleware(
		middleware.LoggingMiddleware(dbLog{w}, quietLogger(), []string{"/healthz"})(web.NewGameRouter(e.deps)))
	srv := httptest.NewServer(logged)
	t.Cleanup(srv.Close)

	h := send(t, http.MethodGet, srv.URL+"/healthz", "", nil)
	if h.status != http.StatusOK {
		t.Fatalf("healthz status %d body %s", h.status, h.body)
	}
	m := h.json(t)
	if m["status"] != "ok" || m["db"] != "ok" || m["redis"] != "ok" {
		t.Errorf("healthz body = %v want status/db/redis ok", m)
	}

	// Control request: a logged endpoint, so we know the writer really flushes.
	send(t, http.MethodPost, srv.URL+"/api/login", "", map[string]any{"username": "nobody", "password": "x"})
	w.Close(context.Background())

	if n := e.scalar(`SELECT count(*) FROM http_log WHERE endpoint = '/healthz'`); n != 0 {
		t.Errorf("http_log rows for /healthz = %d want 0", n)
	}
	if n := e.scalar(`SELECT count(*) FROM http_log WHERE endpoint = 'POST /api/login' OR endpoint = '/api/login'`); n != 1 {
		t.Errorf("control /api/login rows = %d want 1 (writer did not flush?)", n)
	}
	_ = store.LogEntry{}
}
