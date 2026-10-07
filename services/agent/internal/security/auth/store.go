package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Store keeps the agent credential in memory and in a 0600 file written
// atomically. Rotation swaps the file but keeps the previous credential in
// memory until its grace window ends, so no reinstall is required and an
// in-flight request signed with the old credential still succeeds.
type Store struct {
	path string

	mu        sync.Mutex
	cur       Credential
	has       bool
	prev      Credential
	prevUntil time.Time
}

// NewStore returns a Store rooted at path (the credential file).
func NewStore(path string) *Store { return &Store{path: path} }

// Path returns the credential file path.
func (s *Store) Path() string { return s.path }

// Load reads the credential file into memory. A missing file is ErrNoCredential.
func (s *Store) Load() (Credential, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Credential{}, ErrNoCredential
		}
		return Credential{}, fmt.Errorf("auth: read credential: %w", err)
	}
	var c Credential
	if err := json.Unmarshal(raw, &c); err != nil {
		return Credential{}, fmt.Errorf("auth: parse credential: %w", err)
	}
	if err := c.validate(); err != nil {
		return Credential{}, err
	}
	s.cur, s.has = c, true
	return c, nil
}

// Current returns the in-memory credential.
func (s *Store) Current() (Credential, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cur, s.has
}

// Previous returns the credential still inside its rotation grace window.
func (s *Store) Previous(now time.Time) (Credential, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.has || s.prev.Token == "" || !now.Before(s.prevUntil) {
		return Credential{}, false
	}
	return s.prev, true
}

// Save persists c as the current credential (atomic, 0600).
func (s *Store) Save(c Credential) error {
	if err := c.validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := writeFileAtomic(s.path, c); err != nil {
		return err
	}
	s.cur, s.has = c, true
	return nil
}

// Rotate persists next atomically and keeps the previous credential valid in
// memory until graceEnd. It returns the replaced credential.
func (s *Store) Rotate(next Credential, graceEnd time.Time) (Credential, error) {
	if err := next.validate(); err != nil {
		return Credential{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := writeFileAtomic(s.path, next); err != nil {
		return Credential{}, err
	}
	old := s.cur
	s.prev, s.prevUntil = old, graceEnd
	s.cur, s.has = next, true
	return old, nil
}

// writeFileAtomic writes the credential JSON via a same-directory temp file,
// fsyncs file and directory, then renames. The final file is 0600.
func writeFileAtomic(path string, c Credential) error {
	raw, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("auth: encode credential: %w", err)
	}
	raw = append(raw, '\n')

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("auth: create dir %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".credential-*")
	if err != nil {
		return fmt.Errorf("auth: temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("auth: chmod: %w", err)
	}
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("auth: write: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("auth: fsync: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("auth: close: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("auth: rename: %w", err)
	}
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}
