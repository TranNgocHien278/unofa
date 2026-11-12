package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/unofa/unofa/internal/errs"
)

// User is one row of users. Nullable columns are pointers.
type User struct {
	UserID           int64
	Username         string
	PasswordHash     *string
	Locale           *string
	Balance          int64
	TermsVersion     int32
	TermsAcceptedAt  *time.Time
	LoginLockedUntil *time.Time
	DeletedAt        *time.Time
	CreatedAt        time.Time
}

const userCols = `user_id, username, password_hash, locale, balance, terms_version,
	terms_accepted_at, login_locked_until, deleted_at, created_at`

func scanUser(row pgx.Row) (*User, error) {
	var u User
	err := row.Scan(&u.UserID, &u.Username, &u.PasswordHash, &u.Locale, &u.Balance,
		&u.TermsVersion, &u.TermsAcceptedAt, &u.LoginLockedUntil, &u.DeletedAt, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errs.New(errs.UserNotFound)
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// NormalizeUsername lowercases and validates a name the schema will accept.
// Reserved names are rejected here because the DB only enforces uniqueness
// (03 §Xoá tài khoản: block 'system' and the 'deleted_' prefix).
func NormalizeUsername(name string) (string, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" || name == "system" || strings.HasPrefix(name, "deleted_") {
		return "", errs.New(errs.UsernameInvalid)
	}
	return name, nil
}

// CreateUser allocates the next users_seq id and inserts the row. The caller
// owns the opening balance transaction entry (SIGNUP_BONUS, internal/ledger).
func (s *Store) CreateUser(ctx context.Context, username, passwordHash string, termsVersion int32, balance int64) (*User, error) {
	name, err := NormalizeUsername(username)
	if err != nil {
		return nil, err
	}
	ctx, cancel := s.timeoutCtx(ctx)
	defer cancel()

	var id int64
	if err := s.db.QueryRow(ctx, `SELECT nextval('users_seq')`).Scan(&id); err != nil {
		return nil, err
	}
	if id == 0 {
		return nil, errs.New(errs.Internal)
	}
	row := s.db.QueryRow(ctx, `INSERT INTO users
		(user_id, username, password_hash, balance, terms_version, terms_accepted_at)
		VALUES ($1, $2, $3, $4, $5, now())
		RETURNING `+userCols, id, name, passwordHash, balance, termsVersion)
	u, err := scanUser(row)
	if isUniqueViolation(err) {
		return nil, errs.New(errs.UsernameTaken)
	}
	return u, err
}

// GetByID returns a user or USER_NOT_FOUND.
func (s *Store) GetByID(ctx context.Context, userID int64) (*User, error) {
	ctx, cancel := s.timeoutCtx(ctx)
	defer cancel()
	return scanUser(s.db.QueryRow(ctx, `SELECT `+userCols+` FROM users WHERE user_id = $1`, userID))
}

// GetByUsername matches case-insensitively (03 §Người dùng).
func (s *Store) GetByUsername(ctx context.Context, username string) (*User, error) {
	ctx, cancel := s.timeoutCtx(ctx)
	defer cancel()
	return scanUser(s.db.QueryRow(ctx,
		`SELECT `+userCols+` FROM users WHERE username = lower($1)`, username))
}

// UpdateLocale sets users.locale; nil clears it.
func (s *Store) UpdateLocale(ctx context.Context, userID int64, locale *string) error {
	ctx, cancel := s.timeoutCtx(ctx)
	defer cancel()
	tag, err := s.db.Exec(ctx, `UPDATE users SET locale = $2 WHERE user_id = $1`, userID, locale)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errs.New(errs.UserNotFound)
	}
	return nil
}

// UpdateUsername renames a user, mapping a conflict to USERNAME_TAKEN.
func (s *Store) UpdateUsername(ctx context.Context, userID int64, username string) error {
	name, err := NormalizeUsername(username)
	if err != nil {
		return err
	}
	ctx, cancel := s.timeoutCtx(ctx)
	defer cancel()
	tag, err := s.db.Exec(ctx, `UPDATE users SET username = $2 WHERE user_id = $1`, userID, name)
	if isUniqueViolation(err) {
		return errs.New(errs.UsernameTaken)
	}
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errs.New(errs.UserNotFound)
	}
	return nil
}

// UpdatePasswordHash replaces the stored hash; caller hashes and verifies.
func (s *Store) UpdatePasswordHash(ctx context.Context, userID int64, hash string) error {
	ctx, cancel := s.timeoutCtx(ctx)
	defer cancel()
	tag, err := s.db.Exec(ctx, `UPDATE users SET password_hash = $2 WHERE user_id = $1`, userID, hash)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errs.New(errs.UserNotFound)
	}
	return nil
}

// SetLoginLockedUntil sets (or clears with nil) users.login_locked_until.
func (s *Store) SetLoginLockedUntil(ctx context.Context, userID int64, until *time.Time) error {
	ctx, cancel := s.timeoutCtx(ctx)
	defer cancel()
	tag, err := s.db.Exec(ctx,
		`UPDATE users SET login_locked_until = $2 WHERE user_id = $1`, userID, until)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errs.New(errs.UserNotFound)
	}
	return nil
}

// DeletePersonalData applies step 4 of 03 §Xoá tài khoản. Balance, loan and
// session work stays with the caller's transaction; this only scrubs the row.
func (s *Store) DeletePersonalData(ctx context.Context, userID int64) error {
	ctx, cancel := s.timeoutCtx(ctx)
	defer cancel()
	tag, err := s.db.Exec(ctx, `UPDATE users
		SET username = 'deleted_' || user_id, password_hash = NULL, locale = NULL, deleted_at = now()
		WHERE user_id = $1`, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errs.New(errs.UserNotFound)
	}
	return nil
}

// ListAccounts pages the admin account list by user_id descending, filtering
// on a username prefix (compared literally, so '_' and '%' are not wildcards). before = 0 starts at the newest row.
func (s *Store) ListAccounts(ctx context.Context, search string, limit int, before int64) ([]User, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	ctx, cancel := s.timeoutCtx(ctx)
	defer cancel()
	rows, err := s.db.Query(ctx, `SELECT `+userCols+` FROM users
		WHERE left(username, length($1)) = $1
		  AND ($2 = 0 OR user_id < $2)
		ORDER BY user_id DESC
		LIMIT $3`, strings.ToLower(search), before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *u)
	}
	return out, rows.Err()
}

// SumBalances returns SUM(users.balance) for the invariant job.
func (s *Store) SumBalances(ctx context.Context) (int64, error) {
	ctx, cancel := s.timeoutCtx(ctx)
	defer cancel()
	var sum int64
	err := s.db.QueryRow(ctx, `SELECT COALESCE(SUM(balance), 0) FROM users`).Scan(&sum)
	return sum, err
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
