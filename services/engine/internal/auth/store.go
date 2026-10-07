package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// PGStore is the PostgreSQL implementation of Store (tables created by
// migrations 001/003 for users and 011_sessions.sql for sessions).
type PGStore struct{ db *sql.DB }

// NewPGStore returns a Store backed by db.
func NewPGStore(db *sql.DB) *PGStore { return &PGStore{db: db} }

// CreateUser inserts a new account. A duplicate email (unique lower(email)
// index) maps to ErrEmailTaken.
func (s *PGStore) CreateUser(ctx context.Context, u User, passwordHash string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO users (id, email, display_name, password_hash, password_updated_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		u.ID, u.Email, u.DisplayName, passwordHash, u.CreatedAt, u.CreatedAt)
	if isUniqueViolation(err) {
		return ErrEmailTaken
	}
	return wrap("create user", err)
}

// UserByEmail returns the user and password hash for an email.
func (s *PGStore) UserByEmail(ctx context.Context, email string) (User, string, error) {
	var (
		u       User
		hash    sql.NullString
		created time.Time
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT id, email, display_name, created_at, password_hash
		FROM users WHERE lower(email) = lower($1)`, email).
		Scan(&u.ID, &u.Email, &u.DisplayName, &created, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, "", ErrUserNotFound
	}
	if err != nil {
		return User{}, "", wrap("get user by email", err)
	}
	u.CreatedAt = created
	return u, hash.String, nil
}

// UserByID returns the user for id.
func (s *PGStore) UserByID(ctx context.Context, id string) (User, error) {
	var (
		u       User
		email   sql.NullString
		created time.Time
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT id, email, display_name, created_at FROM users WHERE id = $1`, id).
		Scan(&u.ID, &email, &u.DisplayName, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrUserNotFound
	}
	if err != nil {
		return User{}, wrap("get user by id", err)
	}
	u.Email = email.String
	u.CreatedAt = created
	return u, nil
}

// CreateSession persists a session with its token hash.
func (s *PGStore) CreateSession(ctx context.Context, sess Session, tokenHash string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO sessions
			(session_id, user_id, token_hash, csrf_token, user_agent, ip, created_at, last_seen_at, expires_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		sess.ID, sess.UserID, tokenHash, sess.CSRFToken, sess.UserAgent, sess.IP,
		sess.CreatedAt, sess.LastSeenAt, sess.ExpiresAt)
	return wrap("create session", err)
}

const sessionColumns = `session_id, user_id, csrf_token, user_agent, ip, created_at, last_seen_at, expires_at, revoked_at`

func scanSession(row interface{ Scan(...any) error }) (Session, error) {
	var (
		sess      Session
		revokedAt sql.NullTime
	)
	if err := row.Scan(&sess.ID, &sess.UserID, &sess.CSRFToken, &sess.UserAgent, &sess.IP,
		&sess.CreatedAt, &sess.LastSeenAt, &sess.ExpiresAt, &revokedAt); err != nil {
		return Session{}, err
	}
	if revokedAt.Valid {
		t := revokedAt.Time
		sess.RevokedAt = &t
	}
	return sess, nil
}

// SessionByTokenHash returns the session for a token hash.
func (s *PGStore) SessionByTokenHash(ctx context.Context, tokenHash string) (Session, error) {
	sess, err := scanSession(s.db.QueryRowContext(ctx, `SELECT `+sessionColumns+` FROM sessions WHERE token_hash = $1`, tokenHash))
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrSessionNotFound
	}
	if err != nil {
		return Session{}, wrap("get session by token", err)
	}
	return sess, nil
}

// TouchSession records activity for a session.
func (s *PGStore) TouchSession(ctx context.Context, sessionID string, at time.Time) error {
	res, err := s.db.ExecContext(ctx, `UPDATE sessions SET last_seen_at = $2 WHERE session_id = $1`, sessionID, at)
	if err != nil {
		return wrap("touch session", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrSessionNotFound
	}
	return nil
}

// RevokeSessionByToken revokes the session for a token hash.
func (s *PGStore) RevokeSessionByToken(ctx context.Context, tokenHash string, at time.Time) error {
	res, err := s.db.ExecContext(ctx, `UPDATE sessions SET revoked_at = $2 WHERE token_hash = $1 AND revoked_at IS NULL`, tokenHash, at)
	if err != nil {
		return wrap("revoke session by token", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrSessionNotFound
	}
	return nil
}

// RevokeSession revokes one session owned by userID.
func (s *PGStore) RevokeSession(ctx context.Context, userID, sessionID string, at time.Time) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET revoked_at = $3 WHERE session_id = $1 AND user_id = $2 AND revoked_at IS NULL`,
		sessionID, userID, at)
	if err != nil {
		return wrap("revoke session", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrSessionNotFound
	}
	return nil
}

// RevokeOtherSessions revokes all of the user's sessions except keepSessionID.
func (s *PGStore) RevokeOtherSessions(ctx context.Context, userID, keepSessionID string, at time.Time) (int, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET revoked_at = $3 WHERE user_id = $1 AND session_id <> $2 AND revoked_at IS NULL`,
		userID, keepSessionID, at)
	if err != nil {
		return 0, wrap("revoke other sessions", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// ListSessions returns the user's non-revoked sessions, newest first.
func (s *PGStore) ListSessions(ctx context.Context, userID string) ([]Session, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+sessionColumns+` FROM sessions WHERE user_id = $1 AND revoked_at IS NULL ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, wrap("list sessions", err)
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		sess, err := scanSession(rows)
		if err != nil {
			return nil, wrap("scan session", err)
		}
		out = append(out, sess)
	}
	if err := rows.Err(); err != nil {
		return nil, wrap("list sessions", err)
	}
	return out, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func wrap(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("auth: %s: %w", operation, err)
}
