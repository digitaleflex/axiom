package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/digitaleflex/axiom/services/agent/internal/protocol"
)

// Client presents the agent credential on outgoing Engine requests and rotates
// it. Every signed request carries Authorization, X-Agent-ID, X-Timestamp and
// X-Nonce; the Engine rejects replayed nonces and skewed timestamps (#77).
type Client struct {
	BaseURL string
	HTTP    *http.Client
	Store   *Store
	// Now and NewNonce are injectable for deterministic tests.
	Now      func() time.Time
	NewNonce func() string

	guard *ReplayGuard
}

// NewClient returns a Client for the Engine at baseURL.
func NewClient(baseURL string, store *Store) *Client {
	return &Client{
		BaseURL:  strings.TrimRight(baseURL, "/"),
		HTTP:     &http.Client{Timeout: 30 * time.Second},
		Store:    store,
		Now:      func() time.Time { return time.Now().UTC() },
		NewNonce: randomNonce,
		guard:    NewReplayGuard(protocol.MaxClockSkew + 5*time.Minute),
	}
}

func (c *Client) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now().UTC()
}

// Sign adds authentication and replay-protection headers to req.
func (c *Client) Sign(req *http.Request) error {
	cred, ok := c.Store.Current()
	if !ok {
		return ErrNoCredential
	}
	now := c.now()
	nonce := c.NewNonce()
	if c.guard != nil {
		nonce = c.guard.Next(now, c.NewNonce)
	}
	req.Header.Set("Authorization", "Bearer "+cred.Token)
	req.Header.Set("X-Agent-ID", cred.AgentID)
	req.Header.Set("X-Timestamp", now.Format(time.RFC3339))
	req.Header.Set("X-Nonce", nonce)
	return nil
}

// Do signs and performs req.
func (c *Client) Do(req *http.Request) (*http.Response, error) {
	if err := c.Sign(req); err != nil {
		return nil, err
	}
	return c.HTTP.Do(req)
}

// rotateResponse mirrors the Engine's POST /api/v1/agent/rotate payload.
type rotateResponse struct {
	AgentID             string    `json:"agentId"`
	ServerID            string    `json:"serverId"`
	Credential          string    `json:"credential"`
	CredentialVersion   int       `json:"credentialVersion"`
	CredentialExpiresAt time.Time `json:"credentialExpiresAt"`
}

// Rotate asks the Engine for a new credential and stores it atomically,
// keeping the old one valid in memory for the grace window. It never requires a
// reinstall.
func (c *Client) Rotate(ctx context.Context) (Credential, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/v1/agent/rotate", nil)
	if err != nil {
		return Credential{}, err
	}
	if err := c.Sign(req); err != nil {
		return Credential{}, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return Credential{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Credential{}, fmt.Errorf("auth: rotate: engine returned %d", resp.StatusCode)
	}
	var out rotateResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&out); err != nil {
		return Credential{}, fmt.Errorf("auth: rotate: decode: %w", err)
	}
	next := Credential{
		Token:     out.Credential,
		Version:   out.CredentialVersion,
		ExpiresAt: out.CredentialExpiresAt,
		AgentID:   out.AgentID,
		ServerID:  out.ServerID,
	}
	if _, err := c.Store.Rotate(next, c.now().Add(RotationGrace)); err != nil {
		return Credential{}, err
	}
	return next, nil
}

func randomNonce() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("auth: entropy source unavailable: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// ReplayGuard rejects replayed nonces inside a window, using protocol.Dedupe
// semantics. The Engine enforces the same rule with its own cache (#77).
type ReplayGuard struct{ dedupe *protocol.Dedupe }

// NewReplayGuard returns a guard with the given memory window.
func NewReplayGuard(ttl time.Duration) *ReplayGuard {
	return &ReplayGuard{dedupe: protocol.NewDedupe(ttl)}
}

// Accept records nonce and reports whether it is fresh (false means replay).
func (g *ReplayGuard) Accept(nonce string, now time.Time) bool {
	if g == nil || g.dedupe == nil || nonce == "" {
		return false
	}
	return !g.dedupe.Check(nonce, now)
}

// Next returns a nonce the guard has not seen before.
func (g *ReplayGuard) Next(now time.Time, gen func() string) string {
	for i := 0; i < 8; i++ {
		n := gen()
		if g.Accept(n, now) {
			return n
		}
	}
	// Astronomically unlikely; fall back to the last value.
	return gen()
}
