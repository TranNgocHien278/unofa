package store

import (
	"context"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
)

// LogEntry is one http_log row. IP nil means no address was recorded.
type LogEntry struct {
	RequestID  string
	Endpoint   string
	StatusCode int
	IP         *netip.Addr
	UserID     *int64
	At         time.Time
}

// AsyncWriter batches http_log inserts (07 §http_log): 500 rows or 1s,
// whichever comes first, and never blocks the request goroutine. A full
// channel drops the row and bumps a counter instead.
type AsyncWriter struct {
	s        *Store
	entries  chan LogEntry
	flushReq chan chan struct{}
	dropped  atomic.Int64
	quit     chan struct{}
	done     chan struct{}

	closeOnce sync.Once
}

// Batch and flush boundaries; vars so tests can shrink them.
var (
	httpLogBatch = 500
	httpLogEvery = time.Second
	httpLogQueue = 4096
)

// NewAsyncWriter starts the background flusher. Close stops it.
func (s *Store) NewAsyncWriter() *AsyncWriter {
	w := &AsyncWriter{
		s:        s,
		entries:  make(chan LogEntry, httpLogQueue),
		flushReq: make(chan chan struct{}),
		quit:     make(chan struct{}),
		done:     make(chan struct{}),
	}
	go w.run()
	return w
}

// Write queues one row. It never blocks: when the buffer is full the entry is
// dropped and DropCount grows.
func (w *AsyncWriter) Write(e LogEntry) {
	if e.At.IsZero() {
		e.At = time.Now().UTC()
	}
	select {
	case w.entries <- e:
	default:
		w.dropped.Add(1)
	}
}

// DropCount reports rows discarded because the buffer was full.
func (w *AsyncWriter) DropCount() int64 { return w.dropped.Load() }

func (w *AsyncWriter) run() {
	defer close(w.done)
	batch := make([]LogEntry, 0, httpLogBatch)
	timer := time.NewTimer(httpLogEvery)
	defer timer.Stop()
	flush := func() {
		if len(batch) == 0 {
			return
		}
		// A failed insert is dropped: http_log must never take the app down.
		_ = w.s.insertLogs(context.Background(), batch)
		batch = batch[:0]
	}
	// drainAll writes every queued entry in batches of at most httpLogBatch.
	drainAll := func() {
		for {
			select {
			case e := <-w.entries:
				batch = append(batch, e)
				if len(batch) >= httpLogBatch {
					flush()
				}
			default:
				flush()
				return
			}
		}
	}
	for {
		select {
		case e := <-w.entries:
			batch = append(batch, e)
			if len(batch) >= httpLogBatch {
				flush()
				if !timer.Stop() {
					<-timer.C
				}
				timer.Reset(httpLogEvery)
			}
		case <-timer.C:
			flush()
			timer.Reset(httpLogEvery)
		case ack := <-w.flushReq:
			drainAll()
			close(ack)
		case <-w.quit:
			drainAll()
			return
		}
	}
}

// Flush writes everything queued so far and waits for the rows to reach
// Postgres. Safe to call concurrently with Write.
func (w *AsyncWriter) Flush(ctx context.Context) {
	ack := make(chan struct{})
	select {
	case w.flushReq <- ack:
	case <-ctx.Done():
		return
	case <-w.done:
		return
	}
	select {
	case <-ack:
	case <-ctx.Done():
	case <-w.done:
	}
}

// Close stops the flusher after writing the queue. Final rows are written on
// the background context so a cancelled caller ctx cannot lose them.
func (w *AsyncWriter) Close(ctx context.Context) {
	w.closeOnce.Do(func() { close(w.quit) })
	select {
	case <-w.done:
	case <-ctx.Done():
	}
}

func (s *Store) insertLogs(ctx context.Context, entries []LogEntry) error {
	if len(entries) == 0 {
		return nil
	}
	ctx, cancel := s.timeoutCtx(ctx)
	defer cancel()
	_, err := s.db.CopyFrom(ctx, pgx.Identifier{"http_log"},
		[]string{"request_id", "endpoint", "status_code", "ip", "user_id", "at"},
		pgx.CopyFromSlice(len(entries), func(i int) ([]any, error) {
			e := entries[i]
			var ip any
			if e.IP != nil {
				ip = *e.IP
			}
			return []any{e.RequestID, e.Endpoint, e.StatusCode, ip, e.UserID, e.At}, nil
		}))
	return err
}
