// Package auth owns the Runtime Agent's credential lifecycle (#77): the
// credential file, the HTTP client that presents it, and the verification
// helper that mirrors the Engine's checks. Credentials are opaque bearer
// tokens; the Engine stores only their SHA-256 hash. The plaintext token lives
// only in the agent's 0600 credential file and must never be logged.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

const (
	// MaxClockSkew bounds timestamp drift between agent and Engine.
	MaxClockSkew = 5 * time.Minute
	// DefaultTTL is the credential lifetime granted at registration/rotation.
	DefaultTTL = 90 * 24 * time.Hour
	// RotationGrace keeps the previous credential valid after rotation, so an
	// in-flight request is not broken by a concurrent rotation.
	RotationGrace = 5 * time.Minute
)

var (
	// ErrNoCredential means the agent has not registered yet.
	ErrNoCredential = errors.New("auth: no credential stored")
	// ErrCredentialExpired means the credential is past its expiry.
	ErrCredentialExpired = errors.New("auth: credential expired")
	// ErrCredentialRevoked means the Engine revoked the credential.
	ErrCredentialRevoked = errors.New("auth: credential revoked")
	// ErrCredentialInvalid means the presented credential does not match.
	ErrCredentialInvalid = errors.New("auth: credential invalid")
	// ErrIncompleteCredential means a credential is missing required fields.
	ErrIncompleteCredential = errors.New("auth: incomplete credential")
	// ErrReplay means a nonce was already used inside the replay window.
	ErrReplay = errors.New("auth: replayed nonce")
)

// Credential is the agent's own credential material. Token is plaintext and is
// persisted only in the 0600 credential file; it is never logged by this
// package.
type Credential struct {
	Token     string    `json:"token"`
	Version   int       `json:"version"`
	ExpiresAt time.Time `json:"expiresAt"`
	AgentID   string    `json:"agentId"`
	ServerID  string    `json:"serverId"`
}

func (c Credential) validate() error {
	if c.Token == "" || c.AgentID == "" || c.ServerID == "" || c.Version <= 0 {
		return ErrIncompleteCredential
	}
	return nil
}

// HashCredential returns the Engine-side stored form of a credential (SHA-256).
func HashCredential(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// VerifyCredential constant-time checks a presented credential against the
// stored hash, expiry and revoked flag. It mirrors the Engine's agentauth
// verification so both sides agree on the contract (#77).
func VerifyCredential(presented string, storedHash []byte, expiresAt time.Time, revoked bool, now time.Time) error {
	if revoked {
		return ErrCredentialRevoked
	}
	if subtle.ConstantTimeCompare(HashCredential(presented), storedHash) != 1 {
		return ErrCredentialInvalid
	}
	if !now.Before(expiresAt) {
		return ErrCredentialExpired
	}
	return nil
}

// NewToken returns a fresh opaque credential token.
func NewToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("auth: entropy: %w", err)
	}
	return "ac_" + hex.EncodeToString(b), nil
}
