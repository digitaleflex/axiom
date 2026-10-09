// Package agentauth owns Engine-side agent registration and credential
// lifecycle (#76/#77): single-use bootstrap tokens, agent identities bound to
// servers, bearer-credential verification, rotation with grace, replay
// protection and revocation.
//
// Credentials and bootstrap tokens are stored only as SHA-256 hashes; plaintext
// is returned exactly once at issue/redeem time and never logged.
package agentauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
)

const (
	// BootstrapTTL is how long an issued bootstrap token remains valid.
	BootstrapTTL = 15 * time.Minute
	// CredentialTTL is the agent credential lifetime.
	CredentialTTL = 90 * 24 * time.Hour
	// RotationGrace keeps the previous credential valid after rotation.
	RotationGrace = 5 * time.Minute
	// MaxClockSkew bounds the timestamp drift accepted on signed requests.
	MaxClockSkew = 5 * time.Minute
	// NonceTTL is the replay window for request nonces.
	NonceTTL = 10 * time.Minute

	// ProtocolVersion is the negotiated Agent ↔ Engine protocol version
	// (services/agent/internal/protocol.Version; the modules cannot share code).
	// V2 requires the application scope on every operation; MinVersion = 1, so
	// no existing V1 peer is broken.
	ProtocolVersion = 2
	// HeartbeatIntervalSeconds advertised to the agent at registration (#78).
	HeartbeatIntervalSeconds = 30
)

// Status values returned by Status, used to distinguish registered, revoked and
// unknown agents (#76).
const (
	StatusRegistered = "registered"
	StatusRevoked    = "revoked"
	StatusUnknown    = "unknown"
)

var (
	// ErrServerNotFound means no such server exists.
	ErrServerNotFound = errors.New("agentauth: server not found")
	// ErrServerNotPending means a bootstrap token may only be issued for a
	// pending server.
	ErrServerNotPending = errors.New("agentauth: server is not pending")
	// ErrServerRevoked means the server (and thus its agent) is revoked.
	ErrServerRevoked = errors.New("agentauth: server is revoked")
	// ErrTokenInvalid means the bootstrap token does not exist.
	ErrTokenInvalid = errors.New("agentauth: bootstrap token invalid")
	// ErrTokenUsed means the bootstrap token was already redeemed.
	ErrTokenUsed = errors.New("agentauth: bootstrap token already used")
	// ErrTokenExpired means the bootstrap token is past its TTL.
	ErrTokenExpired = errors.New("agentauth: bootstrap token expired")
	// ErrServerMismatch means the token is bound to a different server.
	ErrServerMismatch = errors.New("agentauth: bootstrap token bound to a different server")
	// ErrIdentityNotFound means no agent identity exists for the lookup.
	ErrIdentityNotFound = errors.New("agentauth: agent identity not found")
	// ErrCredentialInvalid means the presented credential does not match.
	ErrCredentialInvalid = errors.New("agentauth: credential invalid")
	// ErrCredentialExpired means the credential is past its expiry.
	ErrCredentialExpired = errors.New("agentauth: credential expired")
	// ErrRevoked means the agent credential is revoked.
	ErrRevoked = errors.New("agentauth: credential revoked")
	// ErrClockSkew means the request timestamp is outside MaxClockSkew.
	ErrClockSkew = errors.New("agentauth: timestamp outside allowed skew")
	// ErrReplay means the request nonce was already seen in the replay window.
	ErrReplay = errors.New("agentauth: replayed request")
)

// ServerLookup resolves a server's current status. It is kept narrow so
// agentauth does not depend on the server domain package.
type ServerLookup interface {
	ServerStatus(ctx context.Context, serverID string) (string, error)
}

// ServerLookupFunc adapts a function to ServerLookup.
type ServerLookupFunc func(ctx context.Context, serverID string) (string, error)

// ServerStatus implements ServerLookup.
func (f ServerLookupFunc) ServerStatus(ctx context.Context, serverID string) (string, error) {
	return f(ctx, serverID)
}

// BootstrapToken is a persisted single-use registration token.
type BootstrapToken struct {
	ServerID  string
	ExpiresAt time.Time
	UsedAt    *time.Time
}

// Identity is an agent identity bound to exactly one server. Credential hashes
// are never serialized to clients.
type Identity struct {
	AgentID             string
	ServerID            string
	AgentVersion        string
	Status              string
	CreatedAt           time.Time
	RevokedAt           *time.Time
	CredentialVersion   int
	CredentialExpiresAt time.Time

	credentialHash     string
	prevCredentialHash string
	prevExpiresAt      *time.Time
}

// Registration is the result of a successful redeem or rotation. Credential is
// plaintext and returned exactly once.
type Registration struct {
	AgentID                  string
	ServerID                 string
	AgentVersion             string
	Credential               string
	CredentialVersion        int
	CredentialExpiresAt      time.Time
	Negotiated               int
	HeartbeatIntervalSeconds int
}

// StatusResult distinguishes registered, revoked and unknown agents.
type StatusResult struct {
	Status            string
	AgentID           string
	ServerID          string
	AgentVersion      string
	CredentialVersion int
}

// Store is the persistence boundary for tokens and identities.
type Store interface {
	SaveBootstrapToken(ctx context.Context, tokenHash, serverID string, expiresAt time.Time) error
	ConsumeBootstrapToken(ctx context.Context, tokenHash string, now time.Time) (BootstrapToken, error)
	IdentityByServer(ctx context.Context, serverID string) (Identity, error)
	IdentityByAgent(ctx context.Context, agentID string) (Identity, error)
	CreateIdentity(ctx context.Context, id Identity) error
	UpdateIdentity(ctx context.Context, id Identity) error
	RevokeIdentity(ctx context.Context, agentID string, now time.Time) error
}

// Service coordinates bootstrap tokens, registration, credentials and replay
// protection.
type Service struct {
	store   Store
	servers ServerLookup
	now     func() time.Time
	nonces  *NonceCache
}

// Option configures a Service.
type Option func(*Service)

// WithClock injects a deterministic clock (tests).
func WithClock(now func() time.Time) Option { return func(s *Service) { s.now = now } }

// WithServerLookup injects the server status resolver.
func WithServerLookup(l ServerLookup) Option { return func(s *Service) { s.servers = l } }

// WithNonceTTL overrides the replay window (tests).
func WithNonceTTL(ttl time.Duration) Option {
	return func(s *Service) { s.nonces = NewNonceCache(ttl) }
}

// NewService builds a Service.
func NewService(store Store, opts ...Option) *Service {
	s := &Service{
		store:  store,
		now:    func() time.Time { return time.Now().UTC() },
		nonces: NewNonceCache(NonceTTL),
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

// Issue creates a single-use bootstrap token bound to serverID. The server must
// exist and be pending.
func (s *Service) Issue(ctx context.Context, serverID string) (string, time.Time, error) {
	if s == nil || s.store == nil {
		return "", time.Time{}, errors.New("agentauth: store is required")
	}
	if serverID == "" {
		return "", time.Time{}, ErrServerNotFound
	}
	status, err := s.serverStatus(ctx, serverID)
	if err != nil {
		return "", time.Time{}, err
	}
	if status != "pending" {
		return "", time.Time{}, ErrServerNotPending
	}
	token, err := newToken("bt_")
	if err != nil {
		return "", time.Time{}, err
	}
	expires := s.now().Add(BootstrapTTL)
	if err := s.store.SaveBootstrapToken(ctx, hashToken(token), serverID, expires); err != nil {
		return "", time.Time{}, err
	}
	return token, expires, nil
}

// Redeem validates and consumes a bootstrap token and creates or re-binds the
// agent identity. Re-registration of the same server returns the SAME agent ID
// (idempotent) with a new credential version.
func (s *Service) Redeem(ctx context.Context, token, serverID, agentVersion string, capabilities []string) (Registration, error) {
	if s == nil || s.store == nil {
		return Registration{}, errors.New("agentauth: store is required")
	}
	if token == "" {
		return Registration{}, ErrTokenInvalid
	}
	if agentVersion == "" || len(capabilities) == 0 {
		return Registration{}, fmt.Errorf("agentauth: %w: agentVersion and capabilities are required", ErrTokenInvalid)
	}
	now := s.now()
	bt, err := s.store.ConsumeBootstrapToken(ctx, hashToken(token), now)
	if err != nil {
		return Registration{}, err
	}
	if serverID != "" && serverID != bt.ServerID {
		return Registration{}, ErrServerMismatch
	}
	status, err := s.serverStatus(ctx, bt.ServerID)
	if err != nil {
		return Registration{}, err
	}
	if status == "revoked" {
		return Registration{}, ErrServerRevoked
	}

	id, err := s.store.IdentityByServer(ctx, bt.ServerID)
	existing := true
	switch {
	case err == nil:
		// idempotent re-registration: reuse the agent ID, new credential version
	case errors.Is(err, ErrIdentityNotFound):
		existing = false
		id = Identity{
			AgentID:      "agent_" + randomHex(12),
			ServerID:     bt.ServerID,
			Status:       "active",
			CreatedAt:    now,
			AgentVersion: agentVersion,
		}
	default:
		return Registration{}, err
	}
	id.AgentVersion = agentVersion
	id.Status = "active"
	id.RevokedAt = nil
	id.CredentialVersion++
	id.prevCredentialHash = ""
	id.prevExpiresAt = nil

	credential, err := newToken("ac_")
	if err != nil {
		return Registration{}, err
	}
	id.credentialHash = hashToken(credential)
	id.CredentialExpiresAt = now.Add(CredentialTTL)

	if existing {
		err = s.store.UpdateIdentity(ctx, id)
	} else {
		err = s.store.CreateIdentity(ctx, id)
	}
	if err != nil {
		return Registration{}, err
	}
	return Registration{
		AgentID:                  id.AgentID,
		ServerID:                 id.ServerID,
		AgentVersion:             id.AgentVersion,
		Credential:               credential,
		CredentialVersion:        id.CredentialVersion,
		CredentialExpiresAt:      id.CredentialExpiresAt,
		Negotiated:               ProtocolVersion,
		HeartbeatIntervalSeconds: HeartbeatIntervalSeconds,
	}, nil
}

// Rotate verifies the current credential, enforces replay freshness and issues
// the next credential. The previous credential remains valid for RotationGrace.
func (s *Service) Rotate(ctx context.Context, agentID, credential, nonce, timestamp string) (Registration, error) {
	if s == nil || s.store == nil {
		return Registration{}, errors.New("agentauth: store is required")
	}
	now := s.now()
	if err := s.VerifyFreshness(nonce, timestamp, now); err != nil {
		return Registration{}, err
	}
	id, err := s.store.IdentityByAgent(ctx, agentID)
	if err != nil {
		return Registration{}, err
	}
	if err := s.checkActive(ctx, id); err != nil {
		return Registration{}, err
	}
	if subtle.ConstantTimeCompare([]byte(hashToken(credential)), []byte(id.credentialHash)) != 1 {
		return Registration{}, ErrCredentialInvalid
	}
	if !now.Before(id.CredentialExpiresAt) {
		return Registration{}, ErrCredentialExpired
	}

	next, err := newToken("ac_")
	if err != nil {
		return Registration{}, err
	}
	graceEnd := now.Add(RotationGrace)
	id.prevCredentialHash = id.credentialHash
	id.prevExpiresAt = &graceEnd
	id.credentialHash = hashToken(next)
	id.CredentialVersion++
	id.CredentialExpiresAt = now.Add(CredentialTTL)
	if err := s.store.UpdateIdentity(ctx, id); err != nil {
		return Registration{}, err
	}
	return Registration{
		AgentID:                  id.AgentID,
		ServerID:                 id.ServerID,
		AgentVersion:             id.AgentVersion,
		Credential:               next,
		CredentialVersion:        id.CredentialVersion,
		CredentialExpiresAt:      id.CredentialExpiresAt,
		Negotiated:               ProtocolVersion,
		HeartbeatIntervalSeconds: HeartbeatIntervalSeconds,
	}, nil
}

// Authenticate enforces replay freshness then verifies the credential. It is
// the guard for protected agent requests (#77).
func (s *Service) Authenticate(ctx context.Context, agentID, credential, nonce, timestamp string) (Identity, error) {
	if s == nil || s.store == nil {
		return Identity{}, errors.New("agentauth: store is required")
	}
	now := s.now()
	if err := s.VerifyFreshness(nonce, timestamp, now); err != nil {
		return Identity{}, err
	}
	return s.Verify(ctx, agentID, credential, now)
}

// Verify checks the credential (current, or previous inside grace), the
// identity status and the server's revocation state.
func (s *Service) Verify(ctx context.Context, agentID, credential string, now time.Time) (Identity, error) {
	if s == nil || s.store == nil {
		return Identity{}, errors.New("agentauth: store is required")
	}
	id, err := s.store.IdentityByAgent(ctx, agentID)
	if err != nil {
		return Identity{}, err
	}
	if err := s.checkActive(ctx, id); err != nil {
		return Identity{}, err
	}
	presented := hashToken(credential)
	if subtle.ConstantTimeCompare([]byte(presented), []byte(id.credentialHash)) == 1 {
		if !now.Before(id.CredentialExpiresAt) {
			return Identity{}, ErrCredentialExpired
		}
		return id, nil
	}
	if id.prevCredentialHash != "" && id.prevExpiresAt != nil &&
		subtle.ConstantTimeCompare([]byte(presented), []byte(id.prevCredentialHash)) == 1 {
		if now.Before(*id.prevExpiresAt) {
			return id, nil
		}
		return Identity{}, ErrCredentialExpired
	}
	return Identity{}, ErrCredentialInvalid
}

// Revoke marks an identity revoked (server revocation path).
func (s *Service) Revoke(ctx context.Context, agentID string, now time.Time) error {
	if s == nil || s.store == nil {
		return errors.New("agentauth: store is required")
	}
	return s.store.RevokeIdentity(ctx, agentID, now)
}

// Status distinguishes registered, revoked and unknown agents (#76).
func (s *Service) Status(ctx context.Context, agentID, serverID string) (StatusResult, error) {
	if s == nil || s.store == nil {
		return StatusResult{}, errors.New("agentauth: store is required")
	}
	if agentID == "" && serverID == "" {
		return StatusResult{Status: StatusUnknown}, nil
	}
	var (
		id  Identity
		err error
	)
	if agentID != "" {
		id, err = s.store.IdentityByAgent(ctx, agentID)
	} else {
		id, err = s.store.IdentityByServer(ctx, serverID)
	}
	if errors.Is(err, ErrIdentityNotFound) {
		return StatusResult{Status: StatusUnknown}, nil
	}
	if err != nil {
		return StatusResult{}, err
	}
	if serverID != "" && id.ServerID != serverID {
		return StatusResult{Status: StatusUnknown}, nil
	}
	res := StatusResult{AgentID: id.AgentID, ServerID: id.ServerID, AgentVersion: id.AgentVersion, CredentialVersion: id.CredentialVersion}
	if id.Status == "revoked" || id.RevokedAt != nil {
		res.Status = StatusRevoked
		return res, nil
	}
	if status, err := s.serverStatus(ctx, id.ServerID); err == nil && status == "revoked" {
		res.Status = StatusRevoked
		return res, nil
	}
	res.Status = StatusRegistered
	return res, nil
}

// VerifyFreshness enforces the timestamp skew bound and single-use nonce.
func (s *Service) VerifyFreshness(nonce, timestamp string, now time.Time) error {
	if nonce == "" {
		return ErrReplay
	}
	ts, err := time.Parse(time.RFC3339, timestamp)
	if err != nil {
		return ErrClockSkew
	}
	if d := now.Sub(ts); d > MaxClockSkew || d < -MaxClockSkew {
		return ErrClockSkew
	}
	if s.nonces.replay(nonce, now) {
		return ErrReplay
	}
	return nil
}

// checkActive rejects revoked identities and identities on revoked servers.
func (s *Service) checkActive(ctx context.Context, id Identity) error {
	if id.Status == "revoked" || id.RevokedAt != nil {
		return ErrRevoked
	}
	status, err := s.serverStatus(ctx, id.ServerID)
	if err != nil {
		return err
	}
	if status == "revoked" {
		return ErrRevoked
	}
	return nil
}

func (s *Service) serverStatus(ctx context.Context, serverID string) (string, error) {
	if s.servers == nil {
		return "", errors.New("agentauth: server lookup is required")
	}
	return s.servers.ServerStatus(ctx, serverID)
}

// NonceCache is a bounded replay window for request nonces.
type NonceCache struct {
	mu   sync.Mutex
	seen map[string]time.Time
	ttl  time.Duration
}

// NewNonceCache returns a cache with the given window.
func NewNonceCache(ttl time.Duration) *NonceCache {
	return &NonceCache{seen: map[string]time.Time{}, ttl: ttl}
}

// replay records nonce and reports whether it was already seen in-window.
func (c *NonceCache) replay(nonce string, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if t, ok := c.seen[nonce]; ok && now.Sub(t) < c.ttl {
		return true
	}
	for k, t := range c.seen {
		if now.Sub(t) >= c.ttl {
			delete(c.seen, k)
		}
	}
	c.seen[nonce] = now
	return false
}

func newToken(prefix string) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("agentauth: entropy: %w", err)
	}
	return prefix + hex.EncodeToString(b), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("agentauth: entropy source unavailable: " + err.Error())
	}
	return hex.EncodeToString(b)
}
