// Command unofa is the single-binary server for UNOFA.
// It serves uno.example.com on game_addr and admin.example.com on admin_addr
// (docs/spec/07-ops-admin.md §Kiến trúc).
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/unofa/unofa/internal/config"
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
// 1. Config already validated before run() is called.
// 2. Connect Postgres (ping).
// 3. Connect Redis (ping); exit if unavailable.
// 4. Startup transaction: abort running matches, delete rooms, insert SERVER_RESTARTED flag.
// 5. Start background jobs.
// 6. Open both listeners.
// This stub compiles and exits cleanly; the real deps are wired after wave-2 packages exist.
func run(ctx context.Context, cfg *config.Config, logger *slog.Logger) error {
	logger.Info("unofa starting",
		slog.String("game_addr", cfg.Server.GameAddr),
		slog.String("admin_addr", cfg.Server.AdminAddr),
	)

	// Placeholder until store/ledger/web packages are ready.
	// Each item below corresponds to a spec startup step.
	logger.Info("step 2: connect postgres (pending store package)")
	logger.Info("step 3: connect redis (pending)")
	logger.Info("step 4: startup transaction (pending)")
	logger.Info("step 5: start background jobs (pending jobs package)")
	logger.Info("step 6: open listeners (pending web package)")

	// Block until shutdown signal.
	logger.Info("server ready (stub mode)")
	<-ctx.Done()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutdownCancel()
	_ = shutdownCtx

	logger.Info("server shutdown complete")
	return nil
}

// healthzResponse is the /healthz response shape (07 §/healthz).
type healthzResponse struct {
	Status   string `json:"status"`
	DB       string `json:"db"`
	Redis    string `json:"redis"`
	UptimeS  int64  `json:"uptime_s"`
}

// startedAt is set at process startup for uptime tracking.
var startedAt = func() time.Time { return time.Now().UTC() }()

func uptime() int64 { return int64(time.Since(startedAt).Seconds()) }

// banner prints a compact startup banner for ops visibility.
func banner(cfg *config.Config) string {
	return fmt.Sprintf("UNOFA game=%s admin=%s", cfg.Server.GameAddr, cfg.Server.AdminAddr)
}
