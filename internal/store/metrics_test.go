package store

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// Buckets come back as timestamptz; with rows present they must scan and format as RFC3339.
func TestMetricsWithRows(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	ep := fmt.Sprintf("/api/test/%d", time.Now().UnixNano())
	if _, err := s.db.Exec(ctx, `INSERT INTO http_log (request_id, endpoint, status_code) VALUES ('r1', $1, 200), ('r2', $1, 429)`, ep); err != nil {
		t.Fatalf("insert: %v", err)
	}
	t.Cleanup(func() { _, _ = s.db.Exec(context.Background(), `DELETE FROM http_log WHERE endpoint = $1`, ep) })

	req, err := s.MetricRequests(ctx, "Today")
	if err != nil {
		t.Fatalf("MetricRequests: %v", err)
	}
	var found int64
	for _, b := range req {
		if _, err := time.Parse(time.RFC3339, b["at"].(string)); err != nil {
			t.Fatalf("bucket at %v: %v", b["at"], err)
		}
		found += b["by_endpoint"].(map[string]int64)[ep]
	}
	if found != 2 {
		t.Fatalf("endpoint count = %d, want 2", found)
	}

	codes, err := s.MetricStatusCodes(ctx, "Today")
	if err != nil {
		t.Fatalf("MetricStatusCodes: %v", err)
	}
	// Today is hourly over 1 day: every hour is present, quiet ones as empty maps.
	if len(req) < 24 || len(codes) != len(req) {
		t.Fatalf("buckets: requests %d, codes %d, want >= 24 and equal", len(req), len(codes))
	}
}
