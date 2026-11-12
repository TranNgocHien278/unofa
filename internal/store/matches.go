package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/unofa/unofa/internal/errs"
)

type Match struct {
	MatchID   string
	RoomID    string
	Stake     int64
	Pot       int64
	Status    string
	WinnerID  *int64
	StartedAt time.Time
	EndedAt   *time.Time
}

type MatchPlayer struct {
	MatchID        string
	UserID         int64
	Username       string
	Seat           int
	Status         string
	DisconnectedAt *time.Time
	ForfeitedAt    *time.Time
}

const matchCols = `match_id::text, room_id::text, stake, pot, status, winner_id, started_at, ended_at`

func scanMatch(row pgx.Row) (Match, error) {
	var m Match
	err := row.Scan(&m.MatchID, &m.RoomID, &m.Stake, &m.Pot, &m.Status, &m.WinnerID, &m.StartedAt, &m.EndedAt)
	if errors.Is(err, pgx.ErrNoRows) || isBadUUID(err) {
		return m, errs.New(errs.NotFound)
	}
	return m, err
}

func collectMatches(rows pgx.Rows, err error) ([]Match, error) {
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Match, error) { return scanMatch(r) })
}

// CreateMatch inserts a running match with pot 0; bets move money in via ledger.
func (s *Store) CreateMatch(ctx context.Context, roomID string, stake int64) (*Match, error) {
	ctx, cancel := s.timeoutCtx(ctx)
	defer cancel()
	m, err := scanMatch(s.db.QueryRow(ctx, `INSERT INTO matches (room_id, stake, status)
		VALUES ($1, $2, 'running') RETURNING `+matchCols, roomID, stake))
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// AddMatchPlayers inserts players; seats[i] belongs to userIDs[i].
func (s *Store) AddMatchPlayers(ctx context.Context, matchID string, userIDs []int64, seats []int) error {
	if len(userIDs) != len(seats) {
		return errs.New(errs.Internal)
	}
	ctx, cancel := s.timeoutCtx(ctx)
	defer cancel()
	_, err := s.db.Exec(ctx, `INSERT INTO match_players (match_id, user_id, seat)
		SELECT $1, u, s FROM unnest($2::bigint[], $3::int[]) AS t(u, s)`, matchID, userIDs, seats)
	return err
}

// GetMatch returns NOT_FOUND for unknown or malformed ids.
func (s *Store) GetMatch(ctx context.Context, matchID string) (*Match, error) {
	ctx, cancel := s.timeoutCtx(ctx)
	defer cancel()
	m, err := scanMatch(s.db.QueryRow(ctx, `SELECT `+matchCols+` FROM matches WHERE match_id = $1::uuid`, matchID))
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// GetMatchPlayers returns players by seat.
func (s *Store) GetMatchPlayers(ctx context.Context, matchID string) ([]MatchPlayer, error) {
	ctx, cancel := s.timeoutCtx(ctx)
	defer cancel()
	rows, err := s.db.Query(ctx, `SELECT p.match_id::text, p.user_id, u.username, p.seat, p.status,
		p.disconnected_at, p.forfeited_at
		FROM match_players p JOIN users u USING (user_id)
		WHERE p.match_id = $1::uuid ORDER BY p.seat`, matchID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (MatchPlayer, error) {
		var p MatchPlayer
		err := r.Scan(&p.MatchID, &p.UserID, &p.Username, &p.Seat, &p.Status, &p.DisconnectedAt, &p.ForfeitedAt)
		return p, err
	})
}

// MatchForUser returns the running match the user plays in, or nil.
// A forfeited player no longer counts as in the match.
func (s *Store) MatchForUser(ctx context.Context, userID int64) (*Match, error) {
	ctx, cancel := s.timeoutCtx(ctx)
	defer cancel()
	m, err := scanMatch(s.db.QueryRow(ctx, `SELECT m.`+matchColsPrefixed+` FROM matches m
		JOIN match_players p USING (match_id)
		WHERE p.user_id = $1 AND m.status = 'running' AND p.status <> 'forfeit'
		ORDER BY m.started_at DESC LIMIT 1`, userID))
	if errs.As(err).Code == errs.NotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

const matchColsPrefixed = `match_id::text, m.room_id::text, m.stake, m.pot, m.status, m.winner_id, m.started_at, m.ended_at`

// HasRunningMatch backs USER_IN_MATCH for admin delete (03 §Xoá tài khoản).
func (s *Store) HasRunningMatch(ctx context.Context, userID int64) (bool, error) {
	m, err := s.MatchForUser(ctx, userID)
	return m != nil, err
}

// SetMatchStatus changes matches.status (running, ended, aborted).
func (s *Store) SetMatchStatus(ctx context.Context, matchID, status string) error {
	return s.execMatch(ctx, `UPDATE matches SET status = $2 WHERE match_id = $1::uuid`, matchID, status)
}

// SetPlayerStatus updates a player's connection state and its timestamp column.
func (s *Store) SetPlayerStatus(ctx context.Context, matchID string, userID int64, status string, at time.Time) error {
	return s.execMatch(ctx, `UPDATE match_players SET status = $3,
		disconnected_at = CASE WHEN $3 = 'disconnected' THEN $4 ELSE disconnected_at END,
		forfeited_at    = CASE WHEN $3 = 'forfeit' THEN $4 ELSE forfeited_at END
		WHERE match_id = $1::uuid AND user_id = $2`, matchID, userID, status, at)
}

// EndMatch marks a running match ended with its winner. Ending twice is
// MATCH_ENDED so a duplicate payout path notices.
func (s *Store) EndMatch(ctx context.Context, matchID string, winnerID int64, at time.Time) error {
	ctx, cancel := s.timeoutCtx(ctx)
	defer cancel()
	tag, err := s.db.Exec(ctx, `UPDATE matches SET status = 'ended', winner_id = $2, ended_at = $3
		WHERE match_id = $1::uuid AND status = 'running'`, matchID, winnerID, at)
	if isBadUUID(err) {
		return errs.New(errs.NotFound)
	}
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errs.New(errs.MatchEnded)
	}
	return nil
}

// AbortAllRunning is the startup step: every running match becomes aborted
// with its pot kept (D14). Returns how many rows changed.
func (s *Store) AbortAllRunning(ctx context.Context, at time.Time) (int64, error) {
	ctx, cancel := s.timeoutCtx(ctx)
	defer cancel()
	tag, err := s.db.Exec(ctx, `UPDATE matches SET status = 'aborted', ended_at = $1 WHERE status = 'running'`, at)
	return tag.RowsAffected(), err
}

// ListMatchesForUser pages a user's finished/aborted history, newest first.
// before is the started_at of the last row seen (zero = first page); match
// ids are UUIDs so they cannot serve as an ordered cursor.
func (s *Store) ListMatchesForUser(ctx context.Context, userID int64, limit int, before time.Time) ([]Match, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	ctx, cancel := s.timeoutCtx(ctx)
	defer cancel()
	return collectMatches(s.db.Query(ctx, `SELECT m.`+matchColsPrefixed+` FROM matches m
		JOIN match_players p USING (match_id)
		WHERE p.user_id = $1 AND ($2::timestamptz IS NULL OR m.started_at < $2)
		ORDER BY m.started_at DESC LIMIT $3`, userID, nullTime(before), limit))
}

func (s *Store) execMatch(ctx context.Context, sql string, args ...any) error {
	ctx, cancel := s.timeoutCtx(ctx)
	defer cancel()
	tag, err := s.db.Exec(ctx, sql, args...)
	if err != nil && !isBadUUID(err) {
		return err
	}
	if err != nil || tag.RowsAffected() == 0 {
		return errs.New(errs.NotFound)
	}
	return nil
}

// isBadUUID reports invalid_text_representation, which is what a malformed id
// from a URL produces on a ::uuid cast.
func isBadUUID(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "22P02"
}

func nullTime(t time.Time) *time.Time { return zeroToNull(t) }
