package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// PGStore implements Store on PostgreSQL (migration 004).
type PGStore struct{ DB *sql.DB }

func (p PGStore) SaveState(ctx context.Context, s OAuthState) error {
	// Opportunistic cleanup of expired states.
	_, _ = p.DB.ExecContext(ctx, `DELETE FROM github_oauth_states WHERE expires_at < now() - interval '1 hour'`)
	_, err := p.DB.ExecContext(ctx, `INSERT INTO github_oauth_states (state_hash, user_id, browser_hash, verifier_sealed, expires_at)
		VALUES ($1, $2, $3, $4, $5)`, s.StateHash, s.UserID, s.BrowserHash, s.VerifierSealed, s.ExpiresAt)
	return err
}

func (p PGStore) ConsumeState(ctx context.Context, hash string, now time.Time) (OAuthState, error) {
	var s OAuthState
	err := p.DB.QueryRowContext(ctx, `DELETE FROM github_oauth_states WHERE state_hash = $1
		RETURNING state_hash, user_id, browser_hash, verifier_sealed, expires_at`, hash).
		Scan(&s.StateHash, &s.UserID, &s.BrowserHash, &s.VerifierSealed, &s.ExpiresAt)
	if err != nil {
		return OAuthState{}, ErrStateInvalid
	}
	if now.After(s.ExpiresAt) {
		return OAuthState{}, ErrStateInvalid
	}
	return s, nil
}

const connColumns = `id, user_id, account_login, account_type, status, scopes, created_at, updated_at`

func scanConn(s interface{ Scan(...any) error }) (Connection, error) {
	var c Connection
	if err := s.Scan(&c.ID, &c.UserID, &c.AccountLogin, &c.AccountType, &c.Status, &c.Scopes, &c.ConnectedAt, &c.UpdatedAt); err != nil {
		return Connection{}, err
	}
	c.ConnectedAt, c.UpdatedAt = c.ConnectedAt.UTC(), c.UpdatedAt.UTC()
	return c, nil
}

func (p PGStore) UpsertConnection(ctx context.Context, userID string, acct Account, scopes string, seal func(string) (StoredTokens, error)) (c Connection, err error) {
	tx, err := p.DB.BeginTx(ctx, nil)
	if err != nil {
		return Connection{}, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	var id string
	err = tx.QueryRowContext(ctx, `SELECT id FROM github_connections WHERE user_id = $1 AND provider_user_id = $2 FOR UPDATE`, userID, acct.ID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		id = newConnectionID()
		if _, err = tx.ExecContext(ctx, `INSERT INTO github_connections (id, user_id, provider_user_id, account_login, account_type, status, scopes)
			VALUES ($1, $2, $3, $4, $5, 'disconnected', $6)`, id, userID, acct.ID, acct.Login, acct.Type, scopes); err != nil {
			return Connection{}, fmt.Errorf("insert connection: %w", err)
		}
	} else if err != nil {
		return Connection{}, err
	}
	t, err := seal(id)
	if err != nil {
		return Connection{}, err
	}
	c, err = scanConn(tx.QueryRowContext(ctx, `UPDATE github_connections SET account_login = $2, account_type = $3, status = 'active', scopes = $4,
		token_ciphertext = $5, refresh_ciphertext = $6, token_expires_at = $7, disconnected_at = NULL, updated_at = now()
		WHERE id = $1 RETURNING `+connColumns, id, acct.Login, acct.Type, scopes, t.AccessSealed, nullBytes(t.RefreshSealed), nullTime(t.ExpiresAt)))
	if err != nil {
		return Connection{}, fmt.Errorf("update connection: %w", err)
	}
	return c, tx.Commit()
}

func (p PGStore) ListConnections(ctx context.Context, userID string) ([]Connection, error) {
	rows, err := p.DB.QueryContext(ctx, `SELECT `+connColumns+` FROM github_connections WHERE user_id = $1 AND provider_user_id IS NOT NULL ORDER BY created_at, id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Connection{}
	for rows.Next() {
		c, err := scanConn(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (p PGStore) GetConnection(ctx context.Context, userID, id string) (Connection, error) {
	c, err := scanConn(p.DB.QueryRowContext(ctx, `SELECT `+connColumns+` FROM github_connections WHERE id = $1 AND user_id = $2`, id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return Connection{}, ErrNotFound
	}
	return c, err
}

func (p PGStore) Tokens(ctx context.Context, id string) (StoredTokens, error) {
	var t StoredTokens
	var exp sql.NullTime
	err := p.DB.QueryRowContext(ctx, `SELECT token_ciphertext, refresh_ciphertext, token_expires_at FROM github_connections WHERE id = $1 AND token_ciphertext IS NOT NULL`, id).
		Scan(&t.AccessSealed, &t.RefreshSealed, &exp)
	if errors.Is(err, sql.ErrNoRows) {
		return StoredTokens{}, ErrDisconnected
	}
	if exp.Valid {
		t.ExpiresAt = exp.Time.UTC()
	}
	return t, err
}

func (p PGStore) UpdateTokens(ctx context.Context, id string, t StoredTokens) error {
	_, err := p.DB.ExecContext(ctx, `UPDATE github_connections SET token_ciphertext = $2, refresh_ciphertext = COALESCE($3, refresh_ciphertext),
		token_expires_at = $4, updated_at = now() WHERE id = $1 AND status <> 'disconnected'`, id, t.AccessSealed, nullBytes(t.RefreshSealed), nullTime(t.ExpiresAt))
	return err
}

func (p PGStore) SetStatus(ctx context.Context, id, status string) error {
	_, err := p.DB.ExecContext(ctx, `UPDATE github_connections SET status = $2, updated_at = now() WHERE id = $1 AND status <> 'disconnected'`, id, status)
	return err
}

func (p PGStore) Disconnect(ctx context.Context, id string) error {
	_, err := p.DB.ExecContext(ctx, `UPDATE github_connections SET status = 'disconnected', token_ciphertext = NULL, refresh_ciphertext = NULL,
		token_expires_at = NULL, disconnected_at = now(), updated_at = now() WHERE id = $1`, id)
	return err
}

func newConnectionID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return "ghc_" + hex.EncodeToString(b)
}

func nullBytes(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return b
}

func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}
