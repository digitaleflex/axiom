// Package auth is the GitHub connection boundary (issue #91): authorization
// flow with single-use state bound to user and browser, PKCE, encrypted token
// persistence, refresh, disconnect/revocation and provider error normalization.
// Tokens never leave this package except through AccessToken, and are never logged.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/digitaleflex/axiom/services/engine/internal/security/secrets"
)

// Normalized errors.
var (
	ErrStateInvalid   = errors.New("github: authorization state is invalid, expired or already used")
	ErrDenied         = errors.New("github: authorization was denied by the user")
	ErrCodeRejected   = errors.New("github: authorization code was rejected")
	ErrProvider       = errors.New("github: provider error")
	ErrTokenInvalid   = errors.New("github: token is invalid or revoked")
	ErrNotFound       = errors.New("github: connection not found")
	ErrDisconnected   = errors.New("github: connection is disconnected")
	ErrNeedsAttention = errors.New("github: connection needs to be reconnected")
)

// Connection is the non-secret view of a GitHub connection.
type Connection struct {
	ID           string    `json:"id"`
	AccountLogin string    `json:"accountLogin"`
	AccountType  string    `json:"accountType"`
	Status       string    `json:"status"` // active | needs_attention | disconnected
	Scopes       string    `json:"scopes"`
	ConnectedAt  time.Time `json:"connectedAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
	UserID       string    `json:"-"`
}

// OAuthState is a pending authorization (stored hashed).
type OAuthState struct {
	StateHash, UserID, BrowserHash string
	VerifierSealed                 []byte
	ExpiresAt                      time.Time
}

// StoredTokens are sealed tokens with their expiry.
type StoredTokens struct {
	AccessSealed, RefreshSealed []byte
	ExpiresAt                   time.Time
}

// Store persists states and connections.
type Store interface {
	SaveState(ctx context.Context, s OAuthState) error
	// ConsumeState atomically deletes and returns the state (single use).
	ConsumeState(ctx context.Context, stateHash string, now time.Time) (OAuthState, error)
	// UpsertConnection creates or reconnects the (user, GitHub account) connection.
	// sealTokens is called with the final connection ID to bind ciphertexts to it.
	UpsertConnection(ctx context.Context, userID string, acct Account, scopes string, sealTokens func(connectionID string) (StoredTokens, error)) (Connection, error)
	ListConnections(ctx context.Context, userID string) ([]Connection, error)
	GetConnection(ctx context.Context, userID, id string) (Connection, error)
	Tokens(ctx context.Context, id string) (StoredTokens, error)
	UpdateTokens(ctx context.Context, id string, t StoredTokens) error
	SetStatus(ctx context.Context, id, status string) error
	// Disconnect wipes tokens and marks the connection disconnected.
	Disconnect(ctx context.Context, id string) error
}

// Service implements the connection lifecycle.
type Service struct {
	Store    Store
	Provider Provider
	Box      *secrets.Box
	Log      *slog.Logger
	StateTTL time.Duration
	Now      func() time.Time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now().UTC()
}

func (s *Service) log() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

// Start begins an authorization. browserSecret must be set as an HttpOnly
// cookie on the caller's browser and presented back at the callback.
func (s *Service) Start(ctx context.Context, userID string) (authorizeURL, browserSecret string, err error) {
	state, err := randomToken(32)
	if err != nil {
		return "", "", err
	}
	browserSecret, err = randomToken(32)
	if err != nil {
		return "", "", err
	}
	verifier, err := randomToken(48) // 64 chars, within PKCE 43..128
	if err != nil {
		return "", "", err
	}
	sealed, err := s.Box.Seal([]byte(verifier), []byte("github-oauth-verifier:"+hashToken(state)))
	if err != nil {
		return "", "", err
	}
	ttl := s.StateTTL
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	if err := s.Store.SaveState(ctx, OAuthState{
		StateHash: hashToken(state), UserID: userID, BrowserHash: hashToken(browserSecret),
		VerifierSealed: sealed, ExpiresAt: s.now().Add(ttl),
	}); err != nil {
		return "", "", fmt.Errorf("save oauth state: %w", err)
	}
	challenge := sha256.Sum256([]byte(verifier))
	return s.Provider.AuthorizeURL(state, base64.RawURLEncoding.EncodeToString(challenge[:])), browserSecret, nil
}

// Callback completes an authorization. providerError is GitHub's `error`
// query parameter (e.g. access_denied).
func (s *Service) Callback(ctx context.Context, state, code, providerError, browserSecret string) (Connection, error) {
	if state == "" {
		return Connection{}, ErrStateInvalid
	}
	st, err := s.Store.ConsumeState(ctx, hashToken(state), s.now())
	if err != nil {
		return Connection{}, ErrStateInvalid
	}
	// The flow must finish in the browser that started it (login CSRF).
	if subtle.ConstantTimeCompare([]byte(st.BrowserHash), []byte(hashToken(browserSecret))) != 1 {
		return Connection{}, ErrStateInvalid
	}
	if providerError != "" {
		if providerError == "access_denied" {
			return Connection{}, ErrDenied
		}
		return Connection{}, fmt.Errorf("%w: %s", ErrProvider, providerError)
	}
	if code == "" {
		return Connection{}, ErrCodeRejected
	}
	verifier, err := s.Box.Open(st.VerifierSealed, []byte("github-oauth-verifier:"+st.StateHash))
	if err != nil {
		return Connection{}, ErrStateInvalid
	}
	tokens, err := s.Provider.Exchange(ctx, code, string(verifier))
	if err != nil {
		return Connection{}, err
	}
	acct, err := s.Provider.Account(ctx, tokens.Access)
	if err != nil {
		return Connection{}, err
	}
	conn, err := s.Store.UpsertConnection(ctx, st.UserID, acct, tokens.Scopes, func(id string) (StoredTokens, error) {
		return s.seal(id, tokens)
	})
	if err != nil {
		return Connection{}, fmt.Errorf("persist connection: %w", err)
	}
	s.log().Info("github connection established", "connectionId", conn.ID, "account", acct.Login, "userId", st.UserID)
	return conn, nil
}

func (s *Service) seal(id string, t Tokens) (StoredTokens, error) {
	access, err := s.Box.Seal([]byte(t.Access), []byte("github-access:"+id))
	if err != nil {
		return StoredTokens{}, err
	}
	out := StoredTokens{AccessSealed: access, ExpiresAt: t.ExpiresAt}
	if t.Refresh != "" {
		if out.RefreshSealed, err = s.Box.Seal([]byte(t.Refresh), []byte("github-refresh:"+id)); err != nil {
			return StoredTokens{}, err
		}
	}
	return out, nil
}

// List returns the user's connections (never tokens).
func (s *Service) List(ctx context.Context, userID string) ([]Connection, error) {
	return s.Store.ListConnections(ctx, userID)
}

// Get returns one connection owned by userID.
func (s *Service) Get(ctx context.Context, userID, id string) (Connection, error) {
	return s.Store.GetConnection(ctx, userID, id)
}

// Disconnect revokes the grant (best effort) and wipes stored tokens.
// After this, AccessToken fails and repository access is impossible.
func (s *Service) Disconnect(ctx context.Context, userID, id string) error {
	conn, err := s.Store.GetConnection(ctx, userID, id)
	if err != nil {
		return err
	}
	if conn.Status != "disconnected" {
		if token, err := s.openAccess(ctx, id); err == nil {
			if err := s.Provider.Revoke(ctx, token); err != nil {
				s.log().Warn("github grant revocation failed; tokens are wiped locally", "connectionId", id, "error", err)
			}
		}
	}
	return s.Store.Disconnect(ctx, id)
}

// AccessToken returns a valid access token for repository access, refreshing
// expiring tokens. It is the only way tokens leave this package.
func (s *Service) AccessToken(ctx context.Context, userID, id string) (string, error) {
	conn, err := s.Store.GetConnection(ctx, userID, id)
	if err != nil {
		return "", err
	}
	switch conn.Status {
	case "disconnected":
		return "", ErrDisconnected
	case "needs_attention":
		return "", ErrNeedsAttention
	}
	st, err := s.Store.Tokens(ctx, id)
	if err != nil {
		return "", err
	}
	if !st.ExpiresAt.IsZero() && s.now().Add(time.Minute).After(st.ExpiresAt) {
		return s.refresh(ctx, id, st)
	}
	access, err := s.Box.Open(st.AccessSealed, []byte("github-access:"+id))
	if err != nil {
		return "", fmt.Errorf("decrypt token: %w", err)
	}
	return string(access), nil
}

func (s *Service) refresh(ctx context.Context, id string, st StoredTokens) (string, error) {
	if len(st.RefreshSealed) == 0 {
		_ = s.Store.SetStatus(ctx, id, "needs_attention")
		return "", ErrNeedsAttention
	}
	refresh, err := s.Box.Open(st.RefreshSealed, []byte("github-refresh:"+id))
	if err != nil {
		return "", fmt.Errorf("decrypt refresh token: %w", err)
	}
	t, err := s.Provider.Refresh(ctx, string(refresh))
	if err != nil {
		if errors.Is(err, ErrCodeRejected) {
			_ = s.Store.SetStatus(ctx, id, "needs_attention")
			return "", ErrNeedsAttention
		}
		return "", err
	}
	sealed, err := s.seal(id, t)
	if err != nil {
		return "", err
	}
	if err := s.Store.UpdateTokens(ctx, id, sealed); err != nil {
		return "", err
	}
	return t.Access, nil
}

// MarkNeedsAttention flags a connection whose token GitHub rejected.
func (s *Service) MarkNeedsAttention(ctx context.Context, id string) error {
	return s.Store.SetStatus(ctx, id, "needs_attention")
}

func (s *Service) openAccess(ctx context.Context, id string) (string, error) {
	st, err := s.Store.Tokens(ctx, id)
	if err != nil {
		return "", err
	}
	b, err := s.Box.Open(st.AccessSealed, []byte("github-access:"+id))
	return string(b), err
}

func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func hashToken(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
