package store

import (
	"context"
	"crypto/rand"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/unofa/unofa/internal/errs"
)

// MaxRoomMembers counts waiting seats too (03 §Phòng và host).
const MaxRoomMembers = 10

const codeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // A-Z + 2-9 minus O, I, 0, 1

// codeAttempts bounds collision retries; 32^6 codes make even 2 a near-certainty.
const codeAttempts = 8

// genCode is a var so tests can force collisions.
var genCode = randomCode

func randomCode() string {
	b := make([]byte, 6)
	rand.Read(b) // crypto/rand.Read never returns an error since Go 1.24
	for i := range b {
		b[i] = codeAlphabet[int(b[i])%len(codeAlphabet)] // 256 % 32 == 0, so no bias
	}
	return string(b)
}

type Room struct {
	RoomID     string
	Code       string
	HostUserID int64
	Stake      int64
	Status     string
	CreatedAt  time.Time
}

// Member is a room_members row joined with the fields GET /api/rooms/{code} shows.
type Member struct {
	RoomID     string
	UserID     int64
	Username   string
	Seat       int
	LobbyReady bool
	Balance    int64
	JoinedAt   time.Time
}

const roomCols = `room_id::text, code, host_user_id, stake, status, created_at`

func scanRoom(row pgx.Row) (*Room, error) {
	var r Room
	err := row.Scan(&r.RoomID, &r.Code, &r.HostUserID, &r.Stake, &r.Status, &r.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) || isBadUUID(err) {
		return nil, errs.New(errs.RoomNotFound)
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// CreateRoom inserts a room with a fresh code and seats the host at seat 0,
// in one transaction. Fails ALREADY_IN_ROOM if the host already sits elsewhere.
func (s *Store) CreateRoom(ctx context.Context, hostUserID, stake int64) (*Room, error) {
	ctx, cancel := s.timeoutCtx(ctx)
	defer cancel()
	var room *Room
	err := s.tx(ctx, func(tx pgx.Tx) error {
		if err := lockUserNotInRoom(ctx, tx, hostUserID, ""); err != nil {
			return err
		}
		for range codeAttempts {
			code := genCode()
			// Savepoint per attempt: a unique violation aborts the enclosing tx otherwise.
			err := pgx.BeginFunc(ctx, tx, func(sp pgx.Tx) error {
				var err error
				room, err = scanRoom(sp.QueryRow(ctx, `INSERT INTO rooms (code, host_user_id, stake)
					VALUES ($1, $2, $3) RETURNING `+roomCols, code, hostUserID, stake))
				return err
			})
			if isUniqueViolation(err) {
				continue
			}
			if err != nil {
				return err
			}
			_, err = tx.Exec(ctx, `INSERT INTO room_members (room_id, user_id, seat) VALUES ($1, $2, 0)`,
				room.RoomID, hostUserID)
			return err
		}
		return errs.New(errs.ServiceUnavailable)
	})
	if err != nil {
		return nil, err
	}
	return room, nil
}

// lockUserNotInRoom locks the users row (serialising concurrent joins by the
// same user; ledger lock order is users first) and checks the user sits in no
// room other than exceptRoom.
func lockUserNotInRoom(ctx context.Context, tx pgx.Tx, userID int64, exceptRoom string) error {
	var other bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (
			SELECT 1 FROM room_members WHERE user_id = u.user_id AND room_id::text <> $2)
		FROM users u WHERE u.user_id = $1 FOR UPDATE OF u`,
		userID, exceptRoom).Scan(&other)
	if errors.Is(err, pgx.ErrNoRows) {
		return errs.New(errs.UserNotFound)
	}
	if err != nil {
		return err
	}
	if other {
		return errs.New(errs.AlreadyInRoom)
	}
	return nil
}

// GetByCode returns ROOM_NOT_FOUND for unknown codes.
func (s *Store) GetByCode(ctx context.Context, code string) (*Room, error) {
	ctx, cancel := s.timeoutCtx(ctx)
	defer cancel()
	return scanRoom(s.db.QueryRow(ctx, `SELECT `+roomCols+` FROM rooms WHERE code = $1`, code))
}

func (s *Store) GetRoomByID(ctx context.Context, roomID string) (*Room, error) {
	ctx, cancel := s.timeoutCtx(ctx)
	defer cancel()
	return scanRoom(s.db.QueryRow(ctx, `SELECT `+roomCols+` FROM rooms WHERE room_id = $1::uuid`, roomID))
}

// SetStatus moves a room between open, locked and in_match.
func (s *Store) SetStatus(ctx context.Context, roomID, status string) error {
	return s.execRoom(ctx, `UPDATE rooms SET status = $2 WHERE room_id = $1`, roomID, status)
}

func (s *Store) SetHost(ctx context.Context, roomID string, hostUserID int64) error {
	return s.execRoom(ctx, `UPDATE rooms SET host_user_id = $2 WHERE room_id = $1`, roomID, hostUserID)
}

// SetStake changes the stake; the stake_preset CHECK rejects non-presets.
func (s *Store) SetStake(ctx context.Context, roomID string, stake int64) error {
	return s.execRoom(ctx, `UPDATE rooms SET stake = $2 WHERE room_id = $1`, roomID, stake)
}

// SetReady flips room_members.lobby_ready for one member.
func (s *Store) SetReady(ctx context.Context, roomID string, userID int64, ready bool) error {
	return s.execRoom(ctx, `UPDATE room_members SET lobby_ready = $3 WHERE room_id = $1 AND user_id = $2`,
		roomID, userID, ready)
}

// DeleteRoom removes an empty room; members cascade.
func (s *Store) DeleteRoom(ctx context.Context, roomID string) error {
	return s.execRoom(ctx, `DELETE FROM rooms WHERE room_id = $1`, roomID)
}

func (s *Store) execRoom(ctx context.Context, sql string, args ...any) error {
	ctx, cancel := s.timeoutCtx(ctx)
	defer cancel()
	tag, err := s.db.Exec(ctx, sql, args...)
	if err != nil && !isBadUUID(err) {
		return err
	}
	if err != nil || tag.RowsAffected() == 0 {
		return errs.New(errs.RoomNotFound)
	}
	return nil
}

// AddMember seats userID at the lowest free seat. Rejoining the same room
// returns the existing seat. Errors: ROOM_NOT_FOUND, ROOM_LOCKED (status
// 'locked'; 'in_match' admits waiting seats), ROOM_FULL, ALREADY_IN_ROOM.
func (s *Store) AddMember(ctx context.Context, roomID string, userID int64) (seat int, err error) {
	ctx, cancel := s.timeoutCtx(ctx)
	defer cancel()
	err = s.tx(ctx, func(tx pgx.Tx) error {
		// Lock the user first (lock order), but report a missing room before ALREADY_IN_ROOM.
		elsewhere := lockUserNotInRoom(ctx, tx, userID, roomID)
		if elsewhere != nil && errs.As(elsewhere).Code != errs.AlreadyInRoom {
			return elsewhere
		}
		var status string
		err := tx.QueryRow(ctx, `SELECT status FROM rooms WHERE room_id = $1::uuid FOR UPDATE`, roomID).Scan(&status)
		if errors.Is(err, pgx.ErrNoRows) || isBadUUID(err) {
			return errs.New(errs.RoomNotFound)
		}
		if err != nil {
			return err
		}
		if elsewhere != nil {
			return elsewhere
		}
		err = tx.QueryRow(ctx, `SELECT seat FROM room_members WHERE room_id = $1 AND user_id = $2`,
			roomID, userID).Scan(&seat)
		if err == nil {
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if status == "locked" {
			return errs.New(errs.RoomLocked)
		}
		err = tx.QueryRow(ctx, `INSERT INTO room_members (room_id, user_id, seat)
			SELECT $1, $2, s FROM generate_series(0, $3 - 1) s
			WHERE s NOT IN (SELECT seat FROM room_members WHERE room_id = $1)
			ORDER BY s LIMIT 1
			RETURNING seat`, roomID, userID, MaxRoomMembers).Scan(&seat)
		if errors.Is(err, pgx.ErrNoRows) {
			return errs.New(errs.RoomFull)
		}
		return err
	})
	return seat, err
}

// RemoveMember deletes the membership and returns how many members remain, so
// the caller can pick a new host or DeleteRoom when it reaches 0.
func (s *Store) RemoveMember(ctx context.Context, roomID string, userID int64) (remaining int, err error) {
	ctx, cancel := s.timeoutCtx(ctx)
	defer cancel()
	err = s.db.QueryRow(ctx, `WITH d AS (
			DELETE FROM room_members WHERE room_id = $1 AND user_id = $2 RETURNING user_id)
		SELECT count(*) FROM room_members WHERE room_id = $1 AND user_id NOT IN (SELECT user_id FROM d)`,
		roomID, userID).Scan(&remaining)
	return remaining, err
}

// ListMembers returns members by seat.
func (s *Store) ListMembers(ctx context.Context, roomID string) ([]Member, error) {
	return s.queryMembers(ctx, `WHERE m.room_id = $1 ORDER BY m.seat`, roomID)
}

// MembershipsForUser lists every room the user sits in (0 or 1 in practice).
func (s *Store) MembershipsForUser(ctx context.Context, userID int64) ([]Member, error) {
	return s.queryMembers(ctx, `WHERE m.user_id = $1 ORDER BY m.joined_at`, userID)
}

func (s *Store) queryMembers(ctx context.Context, where string, arg any) ([]Member, error) {
	ctx, cancel := s.timeoutCtx(ctx)
	defer cancel()
	rows, err := s.db.Query(ctx, `SELECT m.room_id::text, m.user_id, u.username, m.seat, m.lobby_ready,
		u.balance, m.joined_at FROM room_members m JOIN users u USING (user_id) `+where, arg)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Member, error) {
		var m Member
		err := r.Scan(&m.RoomID, &m.UserID, &m.Username, &m.Seat, &m.LobbyReady, &m.Balance, &m.JoinedAt)
		return m, err
	})
}

// DeleteAllRooms is the startup wipe (D1). Run it with AbortAllRunning in one
// transaction via InTx.
//
// Rooms still referenced by matches.room_id cannot be deleted (FK without
// ON DELETE), so those rows are kept but emptied and given an unreachable
// state: status 'locked' with no members, which AddMember rejects.
// shortcut: stale rooms keep their codes; drop when a migration makes
// matches.room_id ON DELETE SET NULL.
func (s *Store) DeleteAllRooms(ctx context.Context) (deleted int64, err error) {
	ctx, cancel := s.timeoutCtx(ctx)
	defer cancel()
	err = s.tx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM room_members`); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `DELETE FROM rooms r
			WHERE NOT EXISTS (SELECT 1 FROM matches m WHERE m.room_id = r.room_id)`)
		if err != nil {
			return err
		}
		deleted = tag.RowsAffected()
		_, err = tx.Exec(ctx, `UPDATE rooms SET status = 'locked'`)
		return err
	})
	return deleted, err
}

// LockRoomForPlay flips a room from open to locked in the same transaction that
// creates the match, and returns the seats that were in the lobby (03 §Phòng và host).
// Concurrent plays on one room serialise on this row lock; the second sees
// status != 'open' and gets ROOM_LOCKED.
func (s *Store) LockRoomForPlay(ctx context.Context, roomID string) ([]Member, error) {
	var members []Member
	err := s.tx(ctx, func(tx pgx.Tx) error {
		var status string
		err := tx.QueryRow(ctx, `SELECT status FROM rooms WHERE room_id = $1::uuid FOR UPDATE`, roomID).Scan(&status)
		if errors.Is(err, pgx.ErrNoRows) || isBadUUID(err) {
			return errs.New(errs.RoomNotFound)
		}
		if err != nil {
			return err
		}
		if status != "open" {
			return errs.New(errs.RoomLocked)
		}
		if _, err := tx.Exec(ctx, `UPDATE rooms SET status = 'locked' WHERE room_id = $1::uuid`, roomID); err != nil {
			return err
		}
		members, err = s.InTx(tx).queryMembers(ctx, `WHERE m.room_id = $1 ORDER BY m.seat`, roomID)
		return err
	})
	return members, err
}
