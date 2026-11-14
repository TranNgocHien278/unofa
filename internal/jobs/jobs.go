// Package jobs runs the in-process background jobs from 07 §Job nền:
// the money invariant check, the hourly loan seize and the http_log cleanup.
// One goroutine per job, each on its own ticker, all started after StartDelay.
package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/unofa/unofa/internal/clock"
	"github.com/unofa/unofa/internal/config"
	"github.com/unofa/unofa/internal/ledger"
	"github.com/unofa/unofa/internal/loan"
	"github.com/unofa/unofa/internal/store"
)

// Job names as they appear in the JSON log line.
const (
	JobInvariant  = "money_invariant"
	JobLoanSeize  = "loan_seize"
	JobLogCleanup = "http_log_cleanup"
)

// jobFunc is one job body: it returns how many rows it touched.
type jobFunc func(context.Context) (int64, error)

type job struct {
	name  string
	every time.Duration
	fn    jobFunc
}

// Runner owns the job goroutines. Start spawns them and returns; Wait blocks
// until ctx is cancelled and every goroutine has exited.
type Runner struct {
	st    *store.Store
	clk   clock.Clock
	log   *slog.Logger
	delay time.Duration
	jobs  []job

	wg sync.WaitGroup
}

// New builds the runner from cfg. Job intervals fall back to the spec defaults
// (1h / 1h / 24h) when the config leaves them zero.
func New(cfg *config.Config, st *store.Store, clk clock.Clock, log *slog.Logger) *Runner {
	if clk == nil {
		clk = clock.Real{}
	}
	if log == nil {
		log = slog.Default()
	}
	r := &Runner{st: st, clk: clk, log: log, delay: cfg.Jobs.StartDelay}
	lcfg := loanCfg(cfg)

	r.jobs = []job{
		{JobInvariant, orDefault(cfg.Jobs.InvariantInterval, time.Hour), func(ctx context.Context) (int64, error) {
			return runInvariant(ctx, st, clk)
		}},
		{JobLoanSeize, orDefault(cfg.Jobs.LoanSeizeInterval, time.Hour), func(ctx context.Context) (int64, error) {
			return runSeize(ctx, st, clk, lcfg)
		}},
		{JobLogCleanup, orDefault(cfg.Jobs.HTTPLogCleanupInterval, 24*time.Hour), func(ctx context.Context) (int64, error) {
			return runCleanup(ctx, st, clk, orDefault(cfg.Jobs.HTTPLogRetention, 60*24*time.Hour))
		}},
	}
	return r
}

// Start spawns one goroutine per job and returns immediately. Every job waits
// StartDelay before its first tick.
func (r *Runner) Start(ctx context.Context) {
	for _, j := range r.jobs {
		r.wg.Add(1)
		go func(j job) {
			defer r.wg.Done()
			r.loop(ctx, j)
		}(j)
	}
}

// Wait blocks until every job goroutine has exited.
func (r *Runner) Wait() { r.wg.Wait() }

func (r *Runner) loop(ctx context.Context, j job) {
	if r.delay > 0 {
		t := time.NewTimer(r.delay)
		defer t.Stop()
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
	tick := time.NewTicker(j.every)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			r.run(ctx, j)
		}
	}
}

// run executes one job iteration. A panic is recovered and logged; the loop
// keeps ticking.
func (r *Runner) run(ctx context.Context, j job) {
	start := time.Now()
	var (
		affected int64
		err      error
	)
	func() {
		defer func() {
			if p := recover(); p != nil {
				err = fmt.Errorf("panic: %v", p)
			}
		}()
		affected, err = j.fn(ctx)
	}()

	var errStr string
	if err != nil {
		errStr = err.Error()
	}
	r.log.Info("job",
		"job", j.name,
		"duration_ms", time.Since(start).Milliseconds(),
		"affected", affected,
		"error", errStr,
	)
}

// runInvariant runs the spec's SUM = 0 query in a transaction. A non-zero sum
// writes one MONEY_INVARIANT admin flag; money is never moved.
func runInvariant(ctx context.Context, st *store.Store, clk clock.Clock) (int64, error) {
	sum, err := ledger.CheckInvariant(ctx, st.Pool, clk.Now())
	if err != nil {
		return 0, err
	}
	var affected int64
	if sum != 0 {
		affected = 1
	}
	return affected, nil
}

// runSeize seizes every loan at the cap, one transaction per loan so a failing
// seize rolls back only its own loan. Each transaction re-checks the loan is
// still active and at the cap; the key loan:{id}:seize makes a re-run a no-op.
func runSeize(ctx context.Context, st *store.Store, clk clock.Clock, lcfg loan.LoanCfg) (int64, error) {
	now := clk.Now()
	rows, err := st.Pool.Query(ctx,
		`SELECT loan_id, user_id, principal, rate_bps, due_at FROM loans
		 WHERE status = 'active'`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	type candidate struct {
		loanID    int64
		userID    int64
		principal int64
		rateBps   int
		dueAt     time.Time
	}
	var candidates []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.loanID, &c.userID, &c.principal, &c.rateBps, &c.dueAt); err != nil {
			return 0, err
		}
		owed, _ := ledger.Owed(c.principal, c.rateBps, c.dueAt, now, lcfg.OverdueBps, lcfg.CapMultiplier)
		if owed >= c.principal*lcfg.CapMultiplier {
			candidates = append(candidates, c)
		}
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	var (
		affected int64
		failures []error
	)
	for _, c := range candidates {
		seized := false
		err := pgx.BeginFunc(ctx, st.Pool, func(tx pgx.Tx) error {
			var serr error
			seized, serr = seizeLoan(ctx, tx, c.loanID, now, lcfg)
			return serr
		})
		if err != nil {
			failures = append(failures, fmt.Errorf("loan %d: %w", c.loanID, err))
			continue
		}
		if seized {
			affected++
		}
	}
	if len(failures) > 0 {
		return affected, errors.Join(failures...)
	}
	return affected, nil
}

// seizeLoan seizes loanID in the caller's transaction, writing the LOAN_SEIZED
// admin flag. It returns false when the loan no longer qualifies.
func seizeLoan(ctx context.Context, tx pgx.Tx, loanID int64, now time.Time, cfg loan.LoanCfg) (bool, error) {
	var (
		userID    int64
		principal int64
		rateBps   int
		dueAt     time.Time
	)
	err := tx.QueryRow(ctx,
		`SELECT user_id, principal, rate_bps, due_at FROM loans
		 WHERE loan_id = $1 AND status = 'active'`, loanID).Scan(&userID, &principal, &rateBps, &dueAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	owed, _ := ledger.Owed(principal, rateBps, dueAt, now, cfg.OverdueBps, cfg.CapMultiplier)
	if owed < principal*cfg.CapMultiplier {
		return false, nil
	}

	key := fmt.Sprintf("loan:%d:seize", loanID)
	seized, forgiven, err := ledger.Seize(ctx, tx,
		ledger.User(userID), ledger.System,
		owed, ledger.ReasonSeize, key)
	if err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE loans SET status = 'seized', forgiven_at_seize = $2, settled_at = $3
		 WHERE loan_id = $1`, loanID, forgiven, now); err != nil {
		return false, err
	}

	detail, _ := json.Marshal(map[string]any{
		"loan_id": loanID, "user_id": userID, "seized": seized, "forgiven": forgiven,
	})
	if _, err := tx.Exec(ctx,
		`INSERT INTO admin_flags (kind, detail) VALUES ('LOAN_SEIZED', $1)`, detail); err != nil {
		return false, err
	}
	return true, nil
}

// runCleanup deletes http_log rows older than the retention window.
func runCleanup(ctx context.Context, st *store.Store, clk clock.Clock, retention time.Duration) (int64, error) {
	return st.DeleteLogsBefore(ctx, clk.Now().Add(-retention))
}

// loanCfg mirrors web's Deps.loanCfg so the job prices loans identically.
func loanCfg(cfg *config.Config) loan.LoanCfg {
	terms := make([]loan.TermCfg, 0, len(cfg.Loan.Terms))
	for _, t := range cfg.Loan.Terms {
		terms = append(terms, loan.TermCfg{Name: t.Name, Duration: t.Duration, RateBps: t.RateBps})
	}
	return loan.LoanCfg{
		Packages:      cfg.Loan.Packages,
		Terms:         terms,
		OverdueBps:    cfg.Loan.OverdueBps,
		CapMultiplier: cfg.Loan.CapMultiplier,
	}
}

func orDefault(d, def time.Duration) time.Duration {
	if d <= 0 {
		return def
	}
	return d
}
