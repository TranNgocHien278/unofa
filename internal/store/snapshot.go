package store

import (
	"context"
	"strings"
	"time"
)

// SnapshotPoint is one bucket of the balance chart (04 §GET /api/me/balance-chart).
// At is the bucket start; Balance is the last balance recorded in that bucket,
// or carried forward when the bucket has no write of its own.
type SnapshotPoint struct {
	At      time.Time
	Balance int64
}

// InsertSnapshot records one balance_snapshot row. The caller writes it in the
// same transaction as the balance change (03 §Số dư và snapshot).
func (s *Store) InsertSnapshot(ctx context.Context, userID, balance int64, at time.Time) error {
	ctx, cancel := s.timeoutCtx(ctx)
	defer cancel()
	_, err := s.db.Exec(ctx, `INSERT INTO balance_snapshot (user_id, balance, at) VALUES ($1, $2, $3)`,
		userID, balance, at)
	return err
}

// Range buckets: today/24h by hour, everything longer by day (UTC).
type bucketSpec struct {
	count int
	step  time.Duration
	today bool
}

var rangeBuckets = map[string]bucketSpec{
	"today": {0, time.Hour, true},
	"24h":   {24, time.Hour, false},
	"3d":    {3, 24 * time.Hour, false},
	"7d":    {7, 24 * time.Hour, false},
	"30d":   {30, 24 * time.Hour, false},
	"60d":   {60, 24 * time.Hour, false},
}

// rangeSpec matches case-insensitively: the API lists the range as "Today".
func rangeSpec(rng string) bucketSpec {
	if spec, ok := rangeBuckets[strings.ToLower(rng)]; ok {
		return spec
	}
	return rangeBuckets["30d"]
}

// SeriesForUser returns the balance chart for one range string. An unknown
// range falls back to "30d", as the API does.
func (s *Store) SeriesForUser(ctx context.Context, userID int64, rng string) ([]SnapshotPoint, error) {
	return s.seriesForUserAt(ctx, userID, rng, time.Now().UTC())
}

// bucketStarts returns the UTC bucket starts ending at the bucket containing
// now. "today" runs from UTC midnight; the rest are fixed counts of full steps.
func bucketStarts(rng string, now time.Time) []time.Time {
	spec := rangeSpec(rng)
	current := now.UTC().Truncate(spec.step)
	if spec.today {
		var starts []time.Time
		for t := now.UTC().Truncate(24 * time.Hour); !t.After(current); t = t.Add(spec.step) {
			starts = append(starts, t)
		}
		return starts
	}
	starts := make([]time.Time, spec.count)
	for i := range starts {
		starts[i] = current.Add(-time.Duration(spec.count-1-i) * spec.step)
	}
	return starts
}

func (s *Store) seriesForUserAt(ctx context.Context, userID int64, rng string, now time.Time) ([]SnapshotPoint, error) {
	spec := rangeSpec(rng)
	starts := bucketStarts(rng, now)
	ends := make([]time.Time, len(starts))
	for i := range starts {
		ends[i] = starts[i].Add(spec.step)
	}
	ctx, cancel := s.timeoutCtx(ctx)
	defer cancel()
	// Each bucket takes the last snapshot before its end, so empty buckets carry
	// the previous balance forward.
	rows, err := s.db.Query(ctx, `SELECT b.start, (
			SELECT s.balance FROM balance_snapshot s
			WHERE s.user_id = $1 AND s.at < b.stop
			ORDER BY s.at DESC LIMIT 1)
		FROM unnest($2::timestamptz[], $3::timestamptz[]) WITH ORDINALITY AS b(start, stop, ord)
		ORDER BY b.ord`, userID, starts, ends)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SnapshotPoint
	for rows.Next() {
		var p SnapshotPoint
		var bal *int64
		if err := rows.Scan(&p.At, &bal); err != nil {
			return nil, err
		}
		if bal == nil {
			continue // before the user's first snapshot: nothing to plot
		}
		p.Balance = *bal
		p.At = p.At.UTC()
		out = append(out, p)
	}
	return out, rows.Err()
}
