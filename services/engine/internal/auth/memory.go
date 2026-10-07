package auth

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"
)

// MemoryStore is an in-memory Store for unit tests and local development. It is
// safe for concurrent use.
type MemoryStore struct {
	mu       sync.Mutex
	users    map[string]User
	hashes   map[string]string
	emailIdx map[string]string // lower(email) -> user id
	sessions map[string]Session
	tokenIdx map[string]string // token hash -> session id
}

// NewMemoryStore returns an empty in-memory store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		users:    map[string]User{},
		hashes:   map[string]string{},
		emailIdx: map[string]string{},
		sessions: map[string]Session{},
		tokenIdx: map[string]string{},
	}
}

// CreateUser implements Store.
func (m *MemoryStore) CreateUser(_ context.Context, u User, passwordHash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := strings.ToLower(u.Email)
	if _, ok := m.emailIdx[key]; ok {
		return ErrEmailTaken
	}
	m.users[u.ID] = u
	m.hashes[u.ID] = passwordHash
	m.emailIdx[key] = u.ID
	return nil
}

// UserByEmail implements Store.
func (m *MemoryStore) UserByEmail(_ context.Context, email string) (User, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id, ok := m.emailIdx[strings.ToLower(strings.TrimSpace(email))]
	if !ok {
		return User{}, "", ErrUserNotFound
	}
	return m.users[id], m.hashes[id], nil
}

// UserByID implements Store.
func (m *MemoryStore) UserByID(_ context.Context, id string) (User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[id]
	if !ok {
		return User{}, ErrUserNotFound
	}
	return u, nil
}

// CreateSession implements Store.
func (m *MemoryStore) CreateSession(_ context.Context, s Session, tokenHash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[s.ID] = s
	m.tokenIdx[tokenHash] = s.ID
	return nil
}

// SessionByTokenHash implements Store.
func (m *MemoryStore) SessionByTokenHash(_ context.Context, tokenHash string) (Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id, ok := m.tokenIdx[tokenHash]
	if !ok {
		return Session{}, ErrSessionNotFound
	}
	return m.sessions[id], nil
}

// TouchSession implements Store.
func (m *MemoryStore) TouchSession(_ context.Context, sessionID string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[sessionID]
	if !ok {
		return ErrSessionNotFound
	}
	s.LastSeenAt = at
	m.sessions[sessionID] = s
	return nil
}

// RevokeSessionByToken implements Store.
func (m *MemoryStore) RevokeSessionByToken(_ context.Context, tokenHash string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	id, ok := m.tokenIdx[tokenHash]
	if !ok {
		return ErrSessionNotFound
	}
	s := m.sessions[id]
	if s.RevokedAt == nil {
		t := at
		s.RevokedAt = &t
		m.sessions[id] = s
	}
	return nil
}

// RevokeSession implements Store.
func (m *MemoryStore) RevokeSession(_ context.Context, userID, sessionID string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[sessionID]
	if !ok || s.UserID != userID {
		return ErrSessionNotFound
	}
	if s.RevokedAt == nil {
		t := at
		s.RevokedAt = &t
		m.sessions[sessionID] = s
	}
	return nil
}

// RevokeOtherSessions implements Store.
func (m *MemoryStore) RevokeOtherSessions(_ context.Context, userID, keepSessionID string, at time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for id, s := range m.sessions {
		if s.UserID != userID || id == keepSessionID || s.RevokedAt != nil {
			continue
		}
		t := at
		s.RevokedAt = &t
		m.sessions[id] = s
		n++
	}
	return n, nil
}

// ListSessions implements Store.
func (m *MemoryStore) ListSessions(_ context.Context, userID string) ([]Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Session
	for _, s := range m.sessions {
		if s.UserID == userID && s.RevokedAt == nil {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}
