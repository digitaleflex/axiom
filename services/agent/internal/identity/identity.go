// Package identity owns the Runtime Agent's persistent identity (#76): the
// agent ID generated on first run and the server binding issued at
// registration. The identity lives in a 0600 file written atomically with
// fsync, so a crash never leaves a torn or world-readable identity file.
//
// The agent ID is generated locally before registration (so the agent has a
// stable local identity to report); the Engine issues the authoritative ID at
// registration and the agent persists it here. Credentials live in
// internal/security/auth, never in this file.
package identity

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

// IDPattern is the canonical agent ID format (protocol.AgentIdentity).
var IDPattern = regexp.MustCompile(`^agent_[0-9a-f]{24}$`)

// ErrInvalidIdentity means the identity is malformed and must not be persisted.
var ErrInvalidIdentity = errors.New("identity: invalid agent identity")

// Identity is the persisted agent identity. AgentID is generated on first run;
// ServerID and Registered are set once the Engine acknowledges registration.
type Identity struct {
	AgentID    string `json:"agentId"`
	ServerID   string `json:"serverId,omitempty"`
	Registered bool   `json:"registered"`
}

// GenerateID returns a new canonical agent ID (agent_<24 hex characters>).
func GenerateID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		// The entropy source failing is unrecoverable for identity.
		panic("identity: entropy source unavailable: " + err.Error())
	}
	return "agent_" + hex.EncodeToString(b)
}

// ValidID reports whether id is a canonical agent ID.
func ValidID(id string) bool { return IDPattern.MatchString(id) }

func (i Identity) validate() error {
	if !ValidID(i.AgentID) {
		return fmt.Errorf("%w: agentId", ErrInvalidIdentity)
	}
	if i.Registered && i.ServerID == "" {
		return fmt.Errorf("%w: registered identity requires serverId", ErrInvalidIdentity)
	}
	return nil
}

// Store persists an Identity at a fixed path.
type Store struct{ path string }

// NewStore returns a Store rooted at path (the identity file).
func NewStore(path string) *Store { return &Store{path: path} }

// Path returns the identity file path.
func (s *Store) Path() string { return s.path }

// Load returns the persisted identity, generating and persisting a fresh,
// unregistered identity on first run. A corrupt file is an error: the agent
// must not silently rotate its identity (that would orphan the server binding).
func (s *Store) Load() (Identity, error) {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			id := Identity{AgentID: GenerateID()}
			if err := s.Save(id); err != nil {
				return Identity{}, err
			}
			return id, nil
		}
		return Identity{}, fmt.Errorf("identity: read %s: %w", s.path, err)
	}
	var id Identity
	if err := json.Unmarshal(raw, &id); err != nil {
		return Identity{}, fmt.Errorf("identity: parse %s: %w", s.path, err)
	}
	if err := id.validate(); err != nil {
		return Identity{}, err
	}
	return id, nil
}

// Save atomically replaces the identity file (0600) with fsync.
func (s *Store) Save(id Identity) error {
	if err := id.validate(); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(id, "", "  ")
	if err != nil {
		return fmt.Errorf("identity: encode: %w", err)
	}
	raw = append(raw, '\n')
	return WriteFileAtomic(s.path, raw)
}

// WriteFileAtomic writes data to path via a same-directory temp file, fsyncs
// the file and the parent directory, then renames. The final file is 0600.
func WriteFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("identity: create dir %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".identity-*")
	if err != nil {
		return fmt.Errorf("identity: temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once renamed

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("identity: chmod: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("identity: write: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("identity: fsync: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("identity: close: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("identity: rename: %w", err)
	}
	// fsync the directory so the rename itself is durable.
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}
