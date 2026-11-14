// Command unofa is the single-binary server for UNOFA.
// It serves uno.example.com on game_addr and admin.example.com on admin_addr
// (docs/spec/07-ops-admin.md §Kiến trúc).
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/unofa/unofa/internal/clock"
	"github.com/unofa/unofa/internal/config"
	"github.com/unofa/unofa/internal/jobs"
	"github.com/unofa/unofa/internal/match"
	"github.com/unofa/unofa/internal/middleware"
	"github.com/unofa/unofa/internal/store"
	"github.com/unofa/unofa/internal/web"
)

func main() {
	cfgPath := flag.String("config", "config.yaml", "path to config.yaml")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		logger.Error("config load failed", slog.String("err", err.Error()))
		os.Exit(1)
	}

	// Set log level from config.
	if cfg.Log.Level == "debug" {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := run(ctx, cfg, logger); err != nil {
		logger.Error("server exited with error", slog.String("err", err.Error()))
		os.Exit(1)
	}
}

// run performs the ordered startup sequence from docs/spec/07-ops-admin.md §Startup:
// config is validated by config.Load before run is called; then Postgres, Redis,
// one startup transaction, background jobs, and both listeners.
func run(ctx context.Context, cfg *config.Config, logger *slog.Logger) error {
	clk := clock.Real{}

	// store.New pings Postgres before returning.
	st, err := store.New(ctx, cfg.DB.DSN, cfg.DB.MaxConns, cfg.DB.MinConns, cfg.DB.ConnTimeout)
	if err != nil {
		return fmt.Errorf("connect postgres: %w", err)
	}
	defer st.Close()
	logger.Info("step 2: postgres connected")

	rdb := redis.NewClient(&redis.Options{Addr: cfg.Redis.Addr, DB: cfg.Redis.DB})
	defer rdb.Close()
	if err := rdb.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("connect redis: %w", err)
	}
	logger.Info("step 3: redis connected")

	aborted, rooms, err := startupTx(ctx, st, clk.Now())
	if err != nil {
		return fmt.Errorf("startup transaction: %w", err)
	}
	logger.Info("step 4: startup transaction committed",
		slog.Int64("aborted_matches", aborted),
		slog.Int64("rooms_deleted", rooms),
	)

	hub := match.New(cfg, st.Pool, st, clk, logger)
	deps := web.BuildDeps(cfg, st, rdb, clk, hub)
	hub.SetLimiter(deps.Limiter)

	runner := jobs.New(cfg, st, clk, logger)
	jobCtx, stopJobs := context.WithCancel(ctx)
	defer runner.Wait()
	defer stopJobs()
	runner.Start(jobCtx)
	logger.Info("step 5: background jobs started")

	writer := st.NewAsyncWriter()
	logMW := middleware.LoggingMiddleware(httpLogAdapter{writer}, logger, []string{"/healthz"})
	gameSrv := &http.Server{
		Addr:              cfg.Server.GameAddr,
		Handler:           middleware.RequestIDMiddleware(logMW(web.NewGameRouter(deps))),
		ReadHeaderTimeout: 10 * time.Second,
	}
	adminSrv := &http.Server{
		Addr:              cfg.Server.AdminAddr,
		Handler:           middleware.RequestIDMiddleware(logMW(web.NewAdminRouter(deps))),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 2)
	for _, srv := range []*http.Server{gameSrv, adminSrv} {
		go func(srv *http.Server) {
			if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				errCh <- err
			}
		}(srv)
	}
	logger.Info("step 6: listeners open",
		slog.String("game_addr", cfg.Server.GameAddr),
		slog.String("admin_addr", cfg.Server.AdminAddr),
	)

	var runErr error
	select {
	case <-ctx.Done():
	case runErr = <-errCh:
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutdownCancel()
	for _, srv := range []*http.Server{gameSrv, adminSrv} {
		if err := srv.Shutdown(shutdownCtx); err != nil {
			logger.Error("listener shutdown failed", slog.String("err", err.Error()))
		}
	}
	writer.Close(shutdownCtx)

	logger.Info("server shutdown complete")
	return runErr
}

// httpLogAdapter converts middleware log entries (string IP) to store rows (netip IP).
type httpLogAdapter struct{ w *store.AsyncWriter }

func (a httpLogAdapter) Write(e middleware.LogEntry) {
	row := store.LogEntry{
		RequestID:  e.RequestID,
		Endpoint:   e.Endpoint,
		StatusCode: e.StatusCode,
		UserID:     e.UserID,
		At:         e.At,
	}
	if ip, err := netip.ParseAddr(e.IP); err == nil {
		row.IP = &ip
	}
	a.w.Write(row)
}

// startupTx runs the spec step 4 transaction: abort running matches, wipe rooms,
// and record SERVER_RESTARTED. All three commit together or not at all.
func startupTx(ctx context.Context, st *store.Store, now time.Time) (aborted, rooms int64, err error) {
	tx, err := st.Pool.Begin(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback(ctx)

	stx := st.InTx(tx)
	if aborted, err = stx.AbortAllRunning(ctx, now); err != nil {
		return 0, 0, err
	}
	if rooms, err = stx.DeleteAllRooms(ctx); err != nil {
		return 0, 0, err
	}
	detail, err := json.Marshal(map[string]int64{"count_of_aborted_matches": aborted})
	if err != nil {
		return 0, 0, err
	}
	if _, err = stx.InsertFlag(ctx, "SERVER_RESTARTED", detail); err != nil {
		return 0, 0, err
	}
	return aborted, rooms, tx.Commit(ctx)
}
