// Package auth implements the Engine's user authentication and session
// boundary (#125): account registration, credential verification, opaque
// session tokens (stored hashed), CSRF tokens, expiry, sliding activity and
// revocation.
//
// Session tokens and CSRF tokens are random 32-byte values. Only the SHA-256
// hash of a session token is persisted; plaintext tokens are returned exactly
// once (login/register) and never logged. Passwords are hashed with an interim
// stdlib-only PBKDF2-HMAC-SHA256 KDF (see password.go).
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"time"
)

const (
	// SessionCookieName is the HttpOnly cookie carrying the opaque session token.
	SessionCookieName = "axiom_session"
	// DefaultSessionTTL is the default session lifetime (30 days).
	DefaultSessionTTL = 720 * time.Hour
	// DefaultTouchInterval bounds how often last_seen_at is persisted.
	DefaultTouchInterval = time.Minute
	// MinPasswordLength and MaxPasswordLength bound accepted passwords.
	MinPasswordLength = 12
	MaxPasswordLength = 1024

	sessionTokenBytes = 32
	csrfTokenBytes    = 32
	idBytes           = 12
	maxUserAgentLen   = 512
	maxIPLen          = 64
)

var (
	// ErrInvalidEmail means the email failed validation.
	ErrInvalidEmail = errors.New("auth: invalid email address")
	// ErrWeakPassword means the password failed validation.
	ErrWeakPassword = errors.New("auth: password does not meet requirements")
	// ErrEmailTaken means an account already exists for the email.
	ErrEmailTaken = errors.New("auth: email already registered")
	// ErrInvalidCredentials is the generic login failure (never discloses
	// whether the email or the password was wrong).
	ErrInvalidCredentials = errors.New("auth: invalid credentials")
	// ErrUserNotFound means no user matches the lookup.
	ErrUserNotFound = errors.New("auth: user not found")
	// ErrSessionNotFound means no session matches the lookup (or it is not
	// owned by the user).
	ErrSessionNotFound = errors.New("auth: session not found")
	// ErrSessionInvalid means the presented session token is expired, revoked
	// or unknown.
	ErrSessionInvalid = errors.New("auth: session invalid or expired")
)

var emailPattern = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

// User is an authenticated account. It never carries credential material.
type User struct {
	ID          string
	Email       string
	DisplayName string
	CreatedAt   time.Time
}

// Session is a persisted session. TokenHash is never exposed to clients.
type Session struct {
	ID         string
	UserID     string
	UserAgent  string
	IP         string
	CreatedAt  time.Time
	LastSeenAt time.Time
	ExpiresAt  time.Time
	RevokedAt  *time.Time
	// CSRFToken backs double-submit CSRF protection for cookie-authenticated
	// browser requests. It is returned to the client at session creation (and
	// on GET /auth/me) but omitted from session listings.
	CSRFToken string
}

// CreatedSession is the result of CreateSession. Token is the opaque session
// token and is returned exactly once.
type CreatedSession struct {
	Session   Session
	Token     string
	CSRFToken string
}

// Store is the persistence boundary for users and sessions.
type Store interface {
	CreateUser(ctx context.Context, u User, passwordHash string) error
	UserByEmail(ctx context.Context, email string) (User, string, error)
	UserByID(ctx context.Context, id string) (User, error)

	CreateSession(ctx context.Context, s Session, tokenHash string) error
	SessionByTokenHash(ctx context.Context, tokenHash string) (Session, error)
	TouchSession(ctx context.Context, sessionID string, at time.Time) error
	RevokeSessionByToken(ctx context.Context, tokenHash string, at time.Time) error
	RevokeSession(ctx context.Context, userID, sessionID string, at time.Time) error
	RevokeOtherSessions(ctx context.Context, userID, keepSessionID string, at time.Time) (int, error)
	ListSessions(ctx context.Context, userID string) ([]Session, error)
}

// Service coordinates registration, login and the session lifecycle.
type Service struct {
	store      Store
	now        func() time.Time
	ttl        time.Duration
	iterations int
	touch      time.Duration
	newID      func(prefix string) string
}

// Option configures a Service.
type Option func(*Service)

// WithClock injects a deterministic clock (tests).
func WithClock(now func() time.Time) Option { return func(s *Service) { s.now = now } }

// WithSessionTTL overrides the session lifetime.
func WithSessionTTL(ttl time.Duration) Option { return func(s *Service) { s.ttl = ttl } }

// WithPasswordIterations overrides the PBKDF2 iteration count (tests only).
func WithPasswordIterations(n int) Option { return func(s *Service) { s.iterations = n } }

// WithTouchInterval overrides how often last_seen_at is rewritten.
func WithTouchInterval(d time.Duration) Option { return func(s *Service) { s.touch = d } }

// NewService builds a Service.
func NewService(store Store, opts ...Option) *Service {
	s := &Service{
		store:      store,
		now:        func() time.Time { return time.Now().UTC() },
		ttl:        DefaultSessionTTL,
		iterations: pbkdf2Iterations,
		touch:      DefaultTouchInterval,
		newID: func(prefix string) string {
			return prefix + randomHex(idBytes)
		},
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

// SessionTTL returns the configured session lifetime.
func (s *Service) SessionTTL() time.Duration { return s.ttl }

// Register validates and creates an account. It returns ErrEmailTaken when the
// email is already registered.
func (s *Service) Register(ctx context.Context, email, password, displayName string) (User, error) {
	if s == nil || s.store == nil {
		return User{}, errors.New("auth: store is required")
	}
	email = normalizeEmail(email)
	if !validEmail(email) {
		return User{}, ErrInvalidEmail
	}
	if err := validatePassword(password); err != nil {
		return User{}, err
	}
	if _, _, err := s.store.UserByEmail(ctx, email); err == nil {
		return User{}, ErrEmailTaken
	} else if !errors.Is(err, ErrUserNotFound) {
		return User{}, err
	}
	hash, err := hashPassword(password, s.iterations)
	if err != nil {
		return User{}, err
	}
	user := User{
		ID:          s.newID("usr_"),
		Email:       email,
		DisplayName: displayNameOrDefault(displayName, email),
		CreatedAt:   s.now(),
	}
	if err := s.store.CreateUser(ctx, user, hash); err != nil {
		return User{}, err
	}
	return user, nil
}

// Login verifies credentials. It always returns the same generic error on
// failure and performs equivalent work for unknown emails to limit timing
// side channels.
func (s *Service) Login(ctx context.Context, email, password string) (User, error) {
	if s == nil || s.store == nil {
		return User{}, errors.New("auth: store is required")
	}
	email = normalizeEmail(email)
	user, hash, err := s.store.UserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			// Burn a KDF computation so a missing account is not observably
			// faster than a wrong password.
			_, _ = hashPassword(password, s.iterations)
			return User{}, ErrInvalidCredentials
		}
		return User{}, err
	}
	if !verifyPassword(hash, password) {
		return User{}, ErrInvalidCredentials
	}
	return user, nil
}

// CreateSession issues an opaque session token for userID. The plaintext token
// and CSRF token are returned exactly once.
func (s *Service) CreateSession(ctx context.Context, userID, userAgent, ip string) (CreatedSession, error) {
	if s == nil || s.store == nil {
		return CreatedSession{}, errors.New("auth: store is required")
	}
	if userID == "" {
		return CreatedSession{}, ErrUserNotFound
	}
	token, err := randomToken(sessionTokenBytes)
	if err != nil {
		return CreatedSession{}, err
	}
	csrf, err := randomToken(csrfTokenBytes)
	if err != nil {
		return CreatedSession{}, err
	}
	now := s.now()
	session := Session{
		ID:         s.newID("ses_"),
		UserID:     userID,
		UserAgent:  truncate(userAgent, maxUserAgentLen),
		IP:         truncate(ip, maxIPLen),
		CreatedAt:  now,
		LastSeenAt: now,
		ExpiresAt:  now.Add(s.ttl),
		CSRFToken:  csrf,
	}
	if err := s.store.CreateSession(ctx, session, hashToken(token)); err != nil {
		return CreatedSession{}, err
	}
	return CreatedSession{Session: session, Token: token, CSRFToken: csrf}, nil
}

// ValidateSession resolves a session token to its user and session. Expired or
// revoked sessions are rejected with ErrSessionInvalid. Activity is recorded
// (sliding last_seen_at) at most once per touch interval.
func (s *Service) ValidateSession(ctx context.Context, token string) (User, Session, error) {
	if s == nil || s.store == nil {
		return User{}, Session{}, ErrSessionInvalid
	}
	if token == "" {
		return User{}, Session{}, ErrSessionInvalid
	}
	session, err := s.store.SessionByTokenHash(ctx, hashToken(token))
	if err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			return User{}, Session{}, ErrSessionInvalid
		}
		return User{}, Session{}, err
	}
	now := s.now()
	if session.RevokedAt != nil || !now.Before(session.ExpiresAt) {
		return User{}, Session{}, ErrSessionInvalid
	}
	if now.Sub(session.LastSeenAt) >= s.touch {
		if err := s.store.TouchSession(ctx, session.ID, now); err != nil {
			return User{}, Session{}, err
		}
		session.LastSeenAt = now
	}
	user, err := s.store.UserByID(ctx, session.UserID)
	if err != nil {
		return User{}, Session{}, err
	}
	return user, session, nil
}

// Logout revokes the session identified by token. Unknown tokens are ignored so
// logout is idempotent.
func (s *Service) Logout(ctx context.Context, token string) error {
	if s == nil || s.store == nil || token == "" {
		return nil
	}
	err := s.store.RevokeSessionByToken(ctx, hashToken(token), s.now())
	if errors.Is(err, ErrSessionNotFound) {
		return nil
	}
	return err
}

// ListSessions returns the user's active (non-revoked, non-expired) sessions.
func (s *Service) ListSessions(ctx context.Context, userID string) ([]Session, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("auth: store is required")
	}
	all, err := s.store.ListSessions(ctx, userID)
	if err != nil {
		return nil, err
	}
	now := s.now()
	out := make([]Session, 0, len(all))
	for _, session := range all {
		if session.RevokedAt == nil && now.Before(session.ExpiresAt) {
			out = append(out, session)
		}
	}
	return out, nil
}

// RevokeSession revokes one session owned by userID.
func (s *Service) RevokeSession(ctx context.Context, userID, sessionID string) error {
	if s == nil || s.store == nil {
		return errors.New("auth: store is required")
	}
	return s.store.RevokeSession(ctx, userID, sessionID, s.now())
}

// RevokeOthers revokes every session of userID except keepSessionID and returns
// how many were revoked.
func (s *Service) RevokeOthers(ctx context.Context, userID, keepSessionID string) (int, error) {
	if s == nil || s.store == nil {
		return 0, errors.New("auth: store is required")
	}
	return s.store.RevokeOtherSessions(ctx, userID, keepSessionID, s.now())
}

// --- helpers ------------------------------------------------------------------

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func validEmail(email string) bool {
	if len(email) < 3 || len(email) > 254 || !emailPattern.MatchString(email) {
		return false
	}
	_, err := mail.ParseAddress(email)
	return err == nil
}

func validatePassword(password string) error {
	if len(password) < MinPasswordLength || len(password) > MaxPasswordLength {
		return ErrWeakPassword
	}
	return nil
}

func displayNameOrDefault(name, email string) string {
	name = strings.TrimSpace(name)
	if name != "" {
		return name
	}
	if at := strings.IndexByte(email, '@'); at > 0 {
		return email[:at]
	}
	return email
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}

func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("auth: entropy: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("auth: entropy source unavailable: " + err.Error())
	}
	return hex.EncodeToString(b)
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
