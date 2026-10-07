package secrets

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// ErrNotFound is returned when no value is stored for a scope/name pair.
var ErrNotFound = errors.New("secrets: no value stored")

// EncryptedStore persists secrets at rest, sealed with Box (AES-256-GCM).
// The associated data binds every ciphertext to scope+"/"+name, so a stored
// value cannot be moved to another scope or renamed without detection.
//
// SECURITY: Get returns plaintext and is for internal use only — callers
// must be components authorized to consume the value (the GitHub API
// client, the build/runtime injection boundary). ListNames returns names
// only. No method returns, logs or wraps plaintext in errors.
type EncryptedStore struct {
	DB  *sql.DB
	Box *Box
}

// SecretMeta is the write-only metadata view of a stored secret: the name
// and its last update time. Values are never included.
type SecretMeta struct {
	Name      string    `json:"name"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// aad binds a ciphertext to its scope and name.
func aad(scope, name string) []byte { return []byte(scope + "/" + name) }

// Put seals plaintext and upserts it under (scope, name). The plaintext is
// never written to the database, logs or errors.
func (s EncryptedStore) Put(ctx context.Context, scope, name, plaintext string) error {
	if s.Box == nil {
		return errors.New("secrets: box is not configured")
	}
	sealed, err := s.Box.Seal([]byte(plaintext), aad(scope, name))
	if err != nil {
		return fmt.Errorf("secrets: seal %s/%s: %w", scope, name, err)
	}
	_, err = s.DB.ExecContext(ctx, `INSERT INTO secrets (secret_id, scope, name, value_sealed)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (scope, name) DO UPDATE
		SET value_sealed = EXCLUDED.value_sealed, updated_at = now()`, newSecretID(), scope, name, sealed)
	if err != nil {
		return fmt.Errorf("secrets: store %s/%s: %w", scope, name, err)
	}
	return nil
}

// Get decrypts the value stored under (scope, name).
//
// SECURITY: plaintext is returned to the caller. Restrict to components
// authorized to consume the value; never log it, never return it through
// metadata APIs, never include it in errors.
func (s EncryptedStore) Get(ctx context.Context, scope, name string) (string, error) {
	var sealed []byte
	err := s.DB.QueryRowContext(ctx, `SELECT value_sealed FROM secrets WHERE scope = $1 AND name = $2`, scope, name).
		Scan(&sealed)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("secrets: load %s/%s: %w", scope, name, err)
	}
	if s.Box == nil {
		return "", errors.New("secrets: box is not configured")
	}
	pt, err := s.Box.Open(sealed, aad(scope, name))
	if err != nil {
		return "", fmt.Errorf("secrets: open %s/%s: %w", scope, name, err)
	}
	return string(pt), nil
}

// Exists reports whether a value is stored under (scope, name).
func (s EncryptedStore) Exists(ctx context.Context, scope, name string) (bool, error) {
	var exists bool
	err := s.DB.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM secrets WHERE scope = $1 AND name = $2)`, scope, name).
		Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("secrets: exists %s/%s: %w", scope, name, err)
	}
	return exists, nil
}

// Delete removes the value stored under (scope, name).
func (s EncryptedStore) Delete(ctx context.Context, scope, name string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM secrets WHERE scope = $1 AND name = $2`, scope, name)
	if err != nil {
		return fmt.Errorf("secrets: delete %s/%s: %w", scope, name, err)
	}
	return nil
}

// ListNames returns metadata (name, updatedAt) for every value stored under
// scope, ordered by name. Values are never returned.
func (s EncryptedStore) ListNames(ctx context.Context, scope string) ([]SecretMeta, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT name, updated_at FROM secrets WHERE scope = $1 ORDER BY name`, scope)
	if err != nil {
		return nil, fmt.Errorf("secrets: list %s: %w", scope, err)
	}
	defer rows.Close()
	out := []SecretMeta{}
	for rows.Next() {
		var m SecretMeta
		if err := rows.Scan(&m.Name, &m.UpdatedAt); err != nil {
			return nil, err
		}
		m.UpdatedAt = m.UpdatedAt.UTC()
		out = append(out, m)
	}
	return out, rows.Err()
}

func newSecretID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return "sec_" + hex.EncodeToString(b)
}
