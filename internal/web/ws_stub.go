package web

import (
	"net/http"

	"golang.org/x/crypto/bcrypt"
)

// handleWS is a stub that the match hub replaces at startup. The router only
// registers the path; the real WebSocket upgrade lives in internal/match.
// Until the hub is wired in, this returns 503 so the healthz check can still
// report the game server as alive.
func (d Deps) handleWS(w http.ResponseWriter, r *http.Request) {
	if d.Match == nil {
		http.Error(w, `{"error":"SERVICE_UNAVAILABLE"}`, http.StatusServiceUnavailable)
		return
	}
	// Real upgrade is handled via the MatchService interface; each deployment
	// wraps the game router's mux with the WebSocket upgrader from internal/match.
	http.Error(w, `{"error":"SERVICE_UNAVAILABLE"}`, http.StatusServiceUnavailable)
}

// _ prevents the import from being dropped before internal/match provides the
// real upgrade handler.
var _ = bcrypt.DefaultCost
