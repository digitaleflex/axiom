package agentauth

import (
	"context"
	"sync"
	"time"
)

// MemoryStore is an in-memory Store used by tests and the API test harness.
type MemoryStore struct {
	mu       sync.Mutex
	tokens   map[string]memoryToken
	byServer map[string]Identity
	byAgent  map[string]Identity
}

type memoryToken struct {
	serverID  string
	expiresAt time.Time
	usedAt    *time.Time
}

// NewMemoryStore returns an empty MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		tokens:   map[string]memoryToken{},
		byServer: map[string]Identity{},
		byAgent:  map[string]Identity{},
	}
}

// SaveBootstrapToken implements Store.
func (m *MemoryStore) SaveBootstrapToken(_ context.Context, tokenHash, serverID string, expiresAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tokens[tokenHash] = memoryToken{serverID: serverID, expiresAt: expiresAt}
	return nil
}

// ConsumeBootstrapToken implements Store.
func (m *MemoryStore) ConsumeBootstrapToken(_ context.Context, tokenHash string, now time.Time) (BootstrapToken, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tokens[tokenHash]
	if !ok {
		return BootstrapToken{}, ErrTokenInvalid
	}
	if t.usedAt != nil {
		return BootstrapToken{}, ErrTokenUsed
	}
	if !now.Before(t.expiresAt) {
		return BootstrapToken{}, ErrTokenExpired
	}
	used := now
	t.usedAt = &used
	m.tokens[tokenHash] = t
	return BootstrapToken{ServerID: t.serverID, ExpiresAt: t.expiresAt, UsedAt: &used}, nil
}

// IdentityByServer implements Store.
func (m *MemoryStore) IdentityByServer(_ context.Context, serverID string) (Identity, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id, ok := m.byServer[serverID]
	if !ok {
		return Identity{}, ErrIdentityNotFound
	}
	return cloneIdentity(id), nil
}

// IdentityByAgent implements Store.
func (m *MemoryStore) IdentityByAgent(_ context.Context, agentID string) (Identity, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id, ok := m.byAgent[agentID]
	if !ok {
		return Identity{}, ErrIdentityNotFound
	}
	return cloneIdentity(id), nil
}

// CreateIdentity implements Store.
func (m *MemoryStore) CreateIdentity(_ context.Context, id Identity) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.byServer[id.ServerID]; exists {
		return ErrIdentityNotFound
	}
	m.store(id)
	return nil
}

// UpdateIdentity implements Store.
func (m *MemoryStore) UpdateIdentity(_ context.Context, id Identity) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.byAgent[id.AgentID]; !exists {
		return ErrIdentityNotFound
	}
	m.store(id)
	return nil
}

// RevokeIdentity implements Store.
func (m *MemoryStore) RevokeIdentity(_ context.Context, agentID string, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	id, ok := m.byAgent[agentID]
	if !ok {
		return ErrIdentityNotFound
	}
	id.Status = "revoked"
	t := now
	id.RevokedAt = &t
	m.store(id)
	return nil
}

func (m *MemoryStore) store(id Identity) {
	m.byAgent[id.AgentID] = id
	m.byServer[id.ServerID] = id
}

func cloneIdentity(id Identity) Identity {
	if id.RevokedAt != nil {
		t := *id.RevokedAt
		id.RevokedAt = &t
	}
	if id.prevExpiresAt != nil {
		t := *id.prevExpiresAt
		id.prevExpiresAt = &t
	}
	return id
}

// StaticServers is a ServerLookup over a fixed status map (tests).
type StaticServers map[string]string

// ServerStatus implements ServerLookup.
func (m StaticServers) ServerStatus(_ context.Context, serverID string) (string, error) {
	status, ok := m[serverID]
	if !ok {
		return "", ErrServerNotFound
	}
	return status, nil
}
