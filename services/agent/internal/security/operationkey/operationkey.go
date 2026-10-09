// Package operationkey owns the Engine→Agent operation signing key (ADR-0008,
// C6/C8): the HMAC key the Engine hands this agent exactly once, in clear, at
// registration (the "operationSigningKey" field, lowercase hex), and the agent
// keeps in a 0600 file written atomically with fsync, mirroring identity.Store.
//
// The key signs every inbound operation: the Engine proves possession of it
// with an HMAC-SHA256 over a canonical form of the request (see Canonical) and
// the agent verifies the signature before the operation reaches the
// dispatcher. The key never travels again after registration/rotation, is
// never logged, and an agent without a key refuses every operation (see
// bootstrap.refuseInbound).
//
// The wire contract is fixed and byte-exact on both sides:
//
//	canonical = strings.Join([]string{
//		"AXIOM-HMAC-V1",   // domain separator
//		"<METHOD>",        // POST
//		"<PATH>",          // request path, e.g. /api/v1/agent/operations
//		"<AgentID>",       // the agent's ID, known independently of the body
//		"<Version>",       // numeric protocol version of the operation (2)
//		"<Timestamp>",     // exact X-Timestamp value
//		"<Nonce>",         // exact X-Nonce value
//		"<sha256(body)>",  // lowercase hex over the raw body bytes
//	}, "\n")             // no trailing newline
//
// signature = hex-lowercase HMAC-SHA256(canonical, key), sent as
// "X-Axiom-Signature: v1=<hex-lowercase>" and compared with hmac.Equal.
package operationkey

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// Wire headers and canonical-form constants. These are part of the wire
// contract: the Engine signs with exactly these names and values.
const (
	// SignatureHeader carries the operation signature.
	SignatureHeader = "X-Axiom-Signature"
	// SignaturePrefix versions the signature scheme; the value that follows
	// is the lowercase hex HMAC-SHA256.
	SignaturePrefix = "v1="
	// TimestampHeader carries the signing timestamp (RFC3339).
	TimestampHeader = "X-Timestamp"
	// NonceHeader carries a per-request nonce (anti-replay).
	NonceHeader = "X-Nonce"
	// CanonicalDomain is the first canonical line: a domain separator that
	// keeps operation signatures from ever colliding with another HMAC.
	CanonicalDomain = "AXIOM-HMAC-V1"
	// KeySize is the size in bytes of a key on the wire (64 hex characters).
	KeySize = 32
)

// Errors.
var (
	// ErrNoKey means no operation signing key is stored: the agent has not
	// registered yet (or the Engine issued no key) and must refuse every
	// inbound operation.
	ErrNoKey = errors.New("operationkey: no operation signing key")
	// ErrInvalidKey means the key material is malformed and must never be
	// persisted or used.
	ErrInvalidKey = errors.New("operationkey: invalid operation signing key")
)

// Key is a raw HMAC-SHA256 operation signing key. Production keys are exactly
// KeySize bytes (the wire field is lowercase hex); the type itself carries the
// raw bytes so the signing primitives are usable with any key length.
type Key []byte

// ParseHex decodes the "operationSigningKey" field of a registration or rotate
// response: lowercase hex encoding of exactly KeySize bytes. Anything else is
// refused: a short, oversized or non-canonical key is a malformed Engine
// response, not a value to normalize.
func ParseHex(s string) (Key, error) {
	if s == "" {
		return nil, fmt.Errorf("%w: empty", ErrInvalidKey)
	}
	if strings.ToLower(s) != s {
		return nil, fmt.Errorf("%w: not lowercase hex", ErrInvalidKey)
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidKey, err)
	}
	if len(b) != KeySize {
		return nil, fmt.Errorf("%w: %d bytes, want %d", ErrInvalidKey, len(b), KeySize)
	}
	return Key(b), nil
}

// Hex returns the lowercase hex encoding of the key (the wire form).
func (k Key) Hex() string { return hex.EncodeToString(k) }

// BodyDigest returns the lowercase hex SHA-256 of body: the eighth canonical
// line, computed over the raw bytes as received.
func BodyDigest(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// Canonical builds the exact signed form (see the package doc). The lines are
// joined with "\n" and there is NO trailing newline; timestamp and nonce are
// the exact header values, never a normalized copy.
func Canonical(method, path, agentID string, version int, timestamp, nonce string, body []byte) string {
	return strings.Join([]string{
		CanonicalDomain,
		method,
		path,
		agentID,
		strconv.Itoa(version),
		timestamp,
		nonce,
		BodyDigest(body),
	}, "\n")
}

// Sign returns the lowercase hex HMAC-SHA256 of canonical under k. It is the
// value that follows "v1=" in the signature header.
func (k Key) Sign(canonical string) string {
	mac := hmac.New(sha256.New, []byte(k))
	mac.Write([]byte(canonical))
	return hex.EncodeToString(mac.Sum(nil))
}

// Verify reports whether sig — the signature header value without the
// SignaturePrefix — matches the HMAC of canonical under k. The comparison is
// constant time (hmac.Equal) and fails closed on empty material.
func (k Key) Verify(canonical, sig string) bool {
	if len(k) == 0 || sig == "" {
		return false
	}
	return hmac.Equal([]byte(k.Sign(canonical)), []byte(sig))
}

// persisted is the on-disk shape: the key in its lowercase hex wire form.
type persisted struct {
	OperationSigningKey string `json:"operationSigningKey"`
}

// Store persists the operation signing key at a fixed path. The file is 0600
// and replaced atomically (same-directory temp file, fsync, rename), exactly
// like identity.Store and auth.Store: a crash never leaves a torn or
// world-readable key file.
type Store struct {
	path string

	mu  sync.RWMutex
	cur Key
	has bool
}

// NewStore returns a Store rooted at path (the operation key file).
func NewStore(path string) *Store { return &Store{path: path} }

// Path returns the operation key file path.
func (s *Store) Path() string { return s.path }

// Load reads the key file into memory. A missing file is ErrNoKey: an agent
// that has never registered simply has no key and refuses every operation. A
// corrupt file is an error: the agent must not silently forget the key its
// operations are signed with.
func (s *Store) Load() (Key, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNoKey
		}
		return nil, fmt.Errorf("operationkey: read %s: %w", s.path, err)
	}
	var rec persisted
	if err := json.Unmarshal(raw, &rec); err != nil {
		return nil, fmt.Errorf("operationkey: parse %s: %w", s.path, err)
	}
	key, err := decodeKey(rec.OperationSigningKey)
	if err != nil {
		return nil, err
	}
	s.cur, s.has = append(Key(nil), key...), true
	return key, nil
}

// Save persists key atomically (0600) and keeps it in memory. The key must be
// non-empty; the 32-byte wire rule is enforced at the Engine boundary by
// ParseHex.
func (s *Store) Save(key Key) error {
	if len(key) == 0 {
		return fmt.Errorf("%w: empty", ErrInvalidKey)
	}
	raw, err := json.MarshalIndent(persisted{OperationSigningKey: key.Hex()}, "", "  ")
	if err != nil {
		return fmt.Errorf("operationkey: encode: %w", err)
	}
	raw = append(raw, '\n')
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := writeKeyFileAtomic(s.path, raw); err != nil {
		return err
	}
	s.cur, s.has = append(Key(nil), key...), true
	return nil
}

// Current returns the in-memory key. ok is false when no key is stored: the
// caller must fail closed (refuse the operation).
func (s *Store) Current() (Key, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.has {
		return nil, false
	}
	return append(Key(nil), s.cur...), true
}

// decodeKey accepts any non-empty lowercase hex key. The strict 32-byte wire
// rule belongs to ParseHex; the store round-trips whatever it saved.
func decodeKey(s string) (Key, error) {
	if s == "" {
		return nil, fmt.Errorf("%w: empty", ErrInvalidKey)
	}
	if strings.ToLower(s) != s {
		return nil, fmt.Errorf("%w: not lowercase hex", ErrInvalidKey)
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidKey, err)
	}
	return Key(b), nil
}

// writeKeyFileAtomic writes data to path via a same-directory temp file, fsyncs
// the file and the parent directory, then renames. The final file is 0600
// (mirrors identity.WriteFileAtomic).
func writeKeyFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("operationkey: create dir %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".operationkey-*")
	if err != nil {
		return fmt.Errorf("operationkey: temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once renamed

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("operationkey: chmod: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("operationkey: write: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("operationkey: fsync: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("operationkey: close: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("operationkey: rename: %w", err)
	}
	// fsync the directory so the rename itself is durable.
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}
