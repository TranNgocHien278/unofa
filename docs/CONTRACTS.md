# CONTRACTS — internal boundaries between parallel work streams

Read `docs/spec/*.md` as the source of truth. This file only fixes **where code
lives and how packages call each other**, so parallel streams do not collide.
If this file and the spec disagree, the spec wins — and this file is wrong.

Everything is Go 1.25, module `github.com/unofa/unofa`.

## Package map — one owner per directory

| Package | Owns | Never imports |
|---|---|---|
| `internal/errs` | coded errors (done) | anything in this repo |
| `internal/clock` | injectable clock (done) | — |
| `internal/config` | config load/validate (done) | — |
| `internal/game` | pure rules engine, cards, per-match state machine. No I/O, no DB, no net. | ledger, store, web, match |
| `internal/ledger` | `Transfer`, `Seize`, loan math, invariant SQL, money error mapping. Takes a `pgx.Tx` from the caller. | web, match |
| `internal/store` | pgx pool + all non-money SQL (users, sessions-adjacent reads, rooms, http_log, admin, flags, matches). | web, match |
| `internal/web` | HTTP: routers, middleware, sessions, rate limit, handlers, http_log writer. | internal/match (except via interfaces it declares) |
| `internal/match` | per-match goroutine, hub/broadcast, WS connection lifecycle, timers. Imports `internal/game` and `internal/ledger`/`internal/store`. | internal/web |
| `internal/jobs` | background jobs (invariant, seize, http_log cleanup). | web |
| `cmd/unofa` | main: config, deps, startup transaction, listeners, wiring. | — |
| `web/` | player SPA (React 18 + TS + Vite). | — |
| `web/admin/` | admin SPA, separate entry, reuses `web/src/api/client.ts`. | — |

`internal/web` must not import `internal/match`; the reverse is allowed only through
small interfaces that `internal/web` declares and `cmd/unofa` satisfies. This keeps
the dependency arrow one-way.

## Money rules (internal/ledger)

```go
type Acct struct {
    UserID  *int64   // set for a users row
    MatchID *string  // set for matches.pot
}

func Transfer(ctx context.Context, tx pgx.Tx, from, to Acct, amount int64, reason, key string) error
func Seize(ctx context.Context, tx pgx.Tx, from, to Acct, owed int64, reason, key string) (seized, forgiven int64, err error)
func Owed(principal int64, rateBps int, dueAt time.Time, now time.Time, overdueBps int, capMultiplier int64) (owed int64, periods int)
func Invariant(ctx context.Context, tx pgx.Tx) (sum int64, err error)
```

- Errors come back as `*errs.Error` with the spec code (`INSUFFICIENT_BALANCE`, …).
- Idempotency: replaying a key returns the previous result, never double-charges.
- Lock order: `users` by `user_id` ascending, then `matches`. Every path obeys this.

## Store (internal/store)

```go
type Store struct{ Pool *pgxpool.Pool }
func New(ctx context.Context, dsn string, maxConns, minConns int32, timeout time.Duration) (*Store, error)
func (s *Store) Close()
```

Queries return plain structs; money movement is never done here.

## Web layer (internal/web)

```go
type Deps struct {
    Cfg    *config.Config
    Store  *store.Store
    RDB    *redis.Client
    Clock  clock.Clock
    Log    *slog.Logger
    Matches MatchService   // interface declared here, implemented by internal/match
}

type MatchService interface {
    // Called after Play locks the room and has created the match row.
    StartMatch(ctx context.Context, matchID, roomID string, players []MatchPlayer) error
    // Called by clients on leave/forfeit and by admin delete.
    Forfeit(ctx context.Context, matchID string, userID int64) error
    CloseSession(sessionID string, reason string) // closes the WS bound to a session
}
```

Router constructors: `NewGameRouter(d Deps) http.Handler`, `NewAdminRouter(d Deps) http.Handler`.

## Wire conventions

- Error body: `{"error": "<CODE>", "request_id": "<id>"}`. `errs.Status(code)` gives the HTTP status. `Retry-After` on 429.
- Every response that carries time includes `server_now` (RFC3339, UTC).
- Lists: `{items, next_cursor}`, `limit` default 50 max 100, cursor = last row id.
- Session cookie `__Host-unofa_sid` / admin `__Host-unofa_admin_sid`; Redis keys `session:<key>` / `admin:session:<key>`; value = user_id or admin username.
- Rate limit keys `rl:<group>:<key>:<window>` via `MULTI INCR + EXPIRE NX`.

## Error-code discipline

`internal/errs` holds the closed set. Adding a code = edit `docs/spec/04-http-api.md` **and** `web/src/i18n/locales/{en,vi}.json` in the same change.

## Tests are part of the deliverable

Every spec test id (`R#`, `T#`, `W#`, `F#`) needs a test whose name contains the id,
e.g. `TestR12_ChallengeCorrect_DrawerDrawsFour`. Integration tests skip when
`UNOFA_TEST_DSN` is unset. `go test -race ./...` must pass.

## Not in scope

No load balancer, no Kafka, no match persistence, no friends, no OAuth, no scoring to 500.
Do not add libraries beyond the ones listed in 06 §Libraries plus `pgx`, `go-redis`, `gorilla/websocket`, `bcrypt`, `yaml`.
