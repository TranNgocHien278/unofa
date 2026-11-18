package store

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Transaction is one row of transactions as the money screens show it.
// Direction is "in" when the user received the amount, "out" when they paid it.
type Transaction struct {
	ID           int64
	Reason       string
	Amount       int64
	Direction    string
	Counterparty string // username on the other side, or "System" / "pot"
	At           time.Time
}

// TransferFilter narrows GET /api/transactions (04 §Tiền).
type TransferFilter struct {
	Reasons   []string // empty means all
	Direction string   // "", "in" or "out"
	Limit     int
	Before    int64 // transaction id cursor; 0 starts at the newest
}

// ListTransactions pages one user's money history newest first, optionally
// filtered by reason and direction. The counterparty name comes from a left
// join so a deleted account shows its deleted_ name.
func (s *Store) ListTransactions(ctx context.Context, userID int64, f TransferFilter) ([]Transaction, error) {
	if f.Limit <= 0 || f.Limit > 100 {
		f.Limit = 50
	}
	reasons := []string{}
	for _, r := range f.Reasons {
		r = strings.ToUpper(strings.TrimSpace(r))
		if r != "" {
			reasons = append(reasons, r)
		}
	}

	ctx, cancel := s.timeoutCtx(ctx)
	defer cancel()

	rows, err := s.db.Query(ctx, `
		WITH mine AS (
			SELECT t.id, t.reason, t.amount, t.created_at,
			       'in'::text AS direction,
			       t.from_user_id, t.from_match_id
			FROM transactions t WHERE t.to_user_id = $1
			UNION ALL
			SELECT t.id, t.reason, t.amount, t.created_at,
			       'out'::text AS direction,
			       t.to_user_id, t.to_match_id
			FROM transactions t WHERE t.from_user_id = $1
		)
		SELECT m.id, m.reason, m.amount, m.direction, m.created_at,
		       COALESCE(u.username, CASE WHEN m.from_match_id IS NOT NULL OR m.from_user_id = 0 THEN 'System' ELSE '' END)
		FROM mine m
		LEFT JOIN users u ON u.user_id = m.from_user_id
		WHERE ($2::bigint = 0 OR m.id < $2)
		  AND (cardinality($3::text[]) = 0 OR m.reason = ANY($3::text[]))
		  AND ($4::text = '' OR m.direction = $4::text)
		ORDER BY m.id DESC
		LIMIT $5`,
		userID, f.Before, reasons, f.Direction, f.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Transaction
	for rows.Next() {
		var t Transaction
		var counterparty string
		if err := rows.Scan(&t.ID, &t.Reason, &t.Amount, &t.Direction, &t.At, &counterparty); err != nil {
			return nil, err
		}
		t.Counterparty = counterparty
		out = append(out, t)
	}
	return out, rows.Err()
}

// LastTransactionID is the highest id this user is involved in; used as the
// balance_after value for a transfer response.
func (s *Store) BalanceOf(ctx context.Context, userID int64) (int64, error) {
	ctx, cancel := s.timeoutCtx(ctx)
	defer cancel()
	var bal int64
	err := s.db.QueryRow(ctx, `SELECT balance FROM users WHERE user_id = $1`, userID).Scan(&bal)
	if err == pgx.ErrNoRows {
		return 0, nil
	}
	return bal, err
}

// AdminAccountDetail bundles what GET /api/admin/accounts/{id} shows.
type AdminAccountDetail struct {
	User         User
	Balance      int64
	Transactions []Transaction
	Loan         map[string]any // nil when there is no active loan
}

// MetricRequests returns bucketed request counts by endpoint, for the admin chart.
func (s *Store) MetricRequests(ctx context.Context, rng string) ([]map[string]any, error) {
	ctx, cancel := s.timeoutCtx(ctx)
	defer cancel()
	rows, err := s.db.Query(ctx, `
		SELECT b.bucket, COALESCE(l.endpoint, ''), count(l.id)
		FROM `+bucketSeries+`
		LEFT JOIN http_log l ON l.at >= now() - $2::interval AND date_trunc($1, l.at) = b.bucket
		GROUP BY 1, 2 ORDER BY 1`, bucketTrunc(rng), bucketInterval(rng))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byEp := map[string]map[string]int64{}
	var order []string
	for rows.Next() {
		var at time.Time
		var endpoint string
		var n int64
		if err := rows.Scan(&at, &endpoint, &n); err != nil {
			return nil, err
		}
		bucket := at.UTC().Format(time.RFC3339)
		if byEp[bucket] == nil {
			order = append(order, bucket)
			byEp[bucket] = map[string]int64{}
		}
		if endpoint != "" {
			byEp[bucket][endpoint] = n
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(order))
	for _, b := range order {
		out = append(out, map[string]any{"at": b, "by_endpoint": byEp[b]})
	}
	return out, nil
}

// bucketSeries lists every bucket in the range, so a quiet hour reads as zero
// instead of vanishing from the time axis.
const bucketSeries = `generate_series(date_trunc($1, now() - $2::interval), date_trunc($1, now()), ('1 ' || $1)::interval) AS b(bucket)`

// MetricStatusCodes returns bucketed HTTP status code counts.
func (s *Store) MetricStatusCodes(ctx context.Context, rng string) ([]map[string]any, error) {
	ctx, cancel := s.timeoutCtx(ctx)
	defer cancel()
	rows, err := s.db.Query(ctx, `
		SELECT b.bucket, COALESCE(l.status_code::text, ''), count(l.id)
		FROM `+bucketSeries+`
		LEFT JOIN http_log l ON l.at >= now() - $2::interval AND date_trunc($1, l.at) = b.bucket
		GROUP BY 1, 2 ORDER BY 1`, bucketTrunc(rng), bucketInterval(rng))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byCodes := map[string]map[string]int64{}
	var order []string
	seen := map[string]bool{}
	for rows.Next() {
		var at time.Time
		var code string
		var n int64
		if err := rows.Scan(&at, &code, &n); err != nil {
			return nil, err
		}
		bucket := at.UTC().Format(time.RFC3339)
		if !seen[bucket] {
			order = append(order, bucket)
			seen[bucket] = true
		}
		if byCodes[bucket] == nil {
			byCodes[bucket] = map[string]int64{}
		}
		if code != "" {
			byCodes[bucket][code] = n
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(order))
	for _, b := range order {
		out = append(out, map[string]any{"at": b, "by_code": byCodes[b]})
	}
	return out, nil
}

func bucketTrunc(rng string) string {
	switch rng {
	case "Today", "today", "24h":
		return "hour"
	default:
		return "day"
	}
}

func bucketInterval(rng string) string {
	switch rng {
	case "Today", "today":
		return "1 day"
	case "24h":
		return "24 hours"
	case "3d":
		return "3 days"
	case "7d":
		return "7 days"
	case "30d":
		return "30 days"
	case "60d":
		return "60 days"
	default:
		return "30 days"
	}
}
