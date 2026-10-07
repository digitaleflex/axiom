package agentauth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// PGStore is the PostgreSQL implementation of Store (tables created by
// migration 009_agent_credentials.sql).
type PGStore struct{ db *sql.DB }

// NewPGStore returns a Store backed by db.
func NewPGStore(db *sql.DB) *PGStore { return &PGStore{db: db} }

// SaveBootstrapToken persists a token hash bound to serverID.
func (s *PGStore) SaveBootstrapToken(ctx context.Context, tokenHash, serverID string, expiresAt time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO agent_bootstrap_tokens (token_hash, server_id, expires_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (token_hash) DO NOTHING`, tokenHash, serverID, expiresAt)
	return wrap("save bootstrap token", err)
}

// ConsumeBootstrapToken atomically marks a token used and returns it. Missing,
// already-used and expired tokens are distinguished.
func (s *PGStore) ConsumeBootstrapToken(ctx context.Context, tokenHash string, now time.Time) (BootstrapToken, error) {
	var bt BootstrapToken
	err := s.db.QueryRowContext(ctx, `
		UPDATE agent_bootstrap_tokens
		SET used_at = $2
		WHERE token_hash = $1 AND used_at IS NULL AND expires_at > $2
		RETURNING server_id, expires_at`, tokenHash, now).Scan(&bt.ServerID, &bt.ExpiresAt)
	if err == nil {
		used := now
		bt.UsedAt = &used
		return bt, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return bt, wrap("consume bootstrap token", err)
	}
	// Diagnose why the atomic update matched nothing.
	var (
		serverID string
		expires  time.Time
		usedAt   sql.NullTime
	)
	derr := s.db.QueryRowContext(ctx, `
		SELECT server_id, expires_at, used_at FROM agent_bootstrap_tokens WHERE token_hash = $1`,
		tokenHash).Scan(&serverID, &expires, &usedAt)
	switch {
	case errors.Is(derr, sql.ErrNoRows):
		return BootstrapToken{}, ErrTokenInvalid
	case derr != nil:
		return BootstrapToken{}, wrap("lookup bootstrap token", derr)
	case usedAt.Valid:
		return BootstrapToken{}, ErrTokenUsed
	default:
		return BootstrapToken{}, ErrTokenExpired
	}
}

const identityColumns = `agent_id, server_id, agent_version, status, created_at, revoked_at,
	credential_version, credential_expires_at, credential_hash, prev_credential_hash, prev_expires_at`

func scanIdentity(row interface{ Scan(...any) error }) (Identity, error) {
	var (
		id         Identity
		revokedAt  sql.NullTime
		prevHash   sql.NullString
		prevExpiry sql.NullTime
	)
	if err := row.Scan(
		&id.AgentID, &id.ServerID, &id.AgentVersion, &id.Status, &id.CreatedAt, &revokedAt,
		&id.CredentialVersion, &id.CredentialExpiresAt, &id.credentialHash, &prevHash, &prevExpiry,
	); err != nil {
		return Identity{}, err
	}
	if revokedAt.Valid {
		t := revokedAt.Time
		id.RevokedAt = &t
	}
	if prevHash.Valid {
		id.prevCredentialHash = prevHash.String
	}
	if prevExpiry.Valid {
		t := prevExpiry.Time
		id.prevExpiresAt = &t
	}
	return id, nil
}

// IdentityByServer returns the identity bound to serverID.
func (s *PGStore) IdentityByServer(ctx context.Context, serverID string) (Identity, error) {
	id, err := scanIdentity(s.db.QueryRowContext(ctx, `SELECT `+identityColumns+` FROM agent_identities WHERE server_id = $1`, serverID))
	if errors.Is(err, sql.ErrNoRows) {
		return Identity{}, ErrIdentityNotFound
	}
	if err != nil {
		return Identity{}, wrap("get identity by server", err)
	}
	return id, nil
}

// IdentityByAgent returns the identity for agentID.
func (s *PGStore) IdentityByAgent(ctx context.Context, agentID string) (Identity, error) {
	id, err := scanIdentity(s.db.QueryRowContext(ctx, `SELECT `+identityColumns+` FROM agent_identities WHERE agent_id = $1`, agentID))
	if errors.Is(err, sql.ErrNoRows) {
		return Identity{}, ErrIdentityNotFound
	}
	if err != nil {
		return Identity{}, wrap("get identity by agent", err)
	}
	return id, nil
}

// CreateIdentity inserts a new agent identity.
func (s *PGStore) CreateIdentity(ctx context.Context, id Identity) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO agent_identities
			(agent_id, server_id, agent_version, status, created_at, revoked_at,
			 credential_version, credential_expires_at, credential_hash, prev_credential_hash, prev_expires_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		id.AgentID, id.ServerID, id.AgentVersion, id.Status, id.CreatedAt, id.RevokedAt,
		id.CredentialVersion, id.CredentialExpiresAt, id.credentialHash, nullable(id.prevCredentialHash), id.prevExpiresAt)
	return wrap("create identity", err)
}

// UpdateIdentity updates mutable identity/credential fields.
func (s *PGStore) UpdateIdentity(ctx context.Context, id Identity) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE agent_identities
		SET agent_version = $2, status = $3, revoked_at = $4,
		    credential_version = $5, credential_expires_at = $6,
		    credential_hash = $7, prev_credential_hash = $8, prev_expires_at = $9
		WHERE agent_id = $1`,
		id.AgentID, id.AgentVersion, id.Status, id.RevokedAt,
		id.CredentialVersion, id.CredentialExpiresAt, id.credentialHash, nullable(id.prevCredentialHash), id.prevExpiresAt)
	if err != nil {
		return wrap("update identity", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrIdentityNotFound
	}
	return nil
}

// RevokeIdentity marks an identity revoked.
func (s *PGStore) RevokeIdentity(ctx context.Context, agentID string, now time.Time) error {
	res, err := s.db.ExecContext(ctx, `UPDATE agent_identities SET status = 'revoked', revoked_at = $2 WHERE agent_id = $1`, agentID, now)
	if err != nil {
		return wrap("revoke identity", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrIdentityNotFound
	}
	return nil
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func wrap(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("agentauth: %s: %w", operation, err)
}
