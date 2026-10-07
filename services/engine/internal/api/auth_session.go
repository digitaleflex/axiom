package api

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/digitaleflex/axiom/services/engine/internal/auth"
)

// csrfHeader is the request header carrying the double-submit CSRF token for
// cookie-authenticated mutating requests. Bearer API clients are exempt.
const csrfHeader = "X-CSRF-Token"

// SessionAuthenticator resolves the actor from an opaque session token, either
// as the HttpOnly `axiom_session` cookie (browser) or as
// `Authorization: Bearer <session-token>` (API clients). It also accepts an
// optional static machine token (AXIOM_API_TOKEN) as a documented escape hatch.
// It fails closed.
type SessionAuthenticator struct {
	svc        *auth.Service
	static     *Principal
	staticHash [32]byte
}

// NewSessionAuthenticator builds a session authenticator. When staticToken is
// non-empty, that token authenticates as staticPrincipal for machine access.
func NewSessionAuthenticator(svc *auth.Service, staticToken string, staticPrincipal Principal) *SessionAuthenticator {
	a := &SessionAuthenticator{svc: svc}
	if staticToken != "" {
		p := staticPrincipal
		a.static = &p
		a.staticHash = sha256.Sum256([]byte(staticToken))
	}
	return a
}

// Authenticate implements Authenticator.
func (s *SessionAuthenticator) Authenticate(r *http.Request) (Principal, error) {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		token := strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
		if token == "" {
			return Principal{}, ErrUnauthenticated
		}
		if s.static != nil {
			got := sha256.Sum256([]byte(token))
			if subtle.ConstantTimeCompare(got[:], s.staticHash[:]) == 1 {
				return *s.static, nil
			}
		}
		if s.svc == nil {
			return Principal{}, ErrUnauthenticated
		}
		user, session, err := s.svc.ValidateSession(r.Context(), token)
		if err != nil {
			return Principal{}, ErrUnauthenticated
		}
		return principalFromSession(user, session, false), nil
	}

	if s.svc == nil {
		return Principal{}, ErrUnauthenticated
	}
	cookie, err := r.Cookie(auth.SessionCookieName)
	if err != nil || cookie.Value == "" {
		return Principal{}, ErrUnauthenticated
	}
	user, session, err := s.svc.ValidateSession(r.Context(), cookie.Value)
	if err != nil {
		return Principal{}, ErrUnauthenticated
	}
	return principalFromSession(user, session, true), nil
}

func principalFromSession(user auth.User, session auth.Session, cookieAuth bool) Principal {
	return Principal{
		UserID:     user.ID,
		Name:       user.DisplayName,
		SessionID:  session.ID,
		CSRFToken:  session.CSRFToken,
		CookieAuth: cookieAuth,
	}
}

// --- handlers -----------------------------------------------------------------

// register creates an account and starts a session (auto sign-in).
// POST /api/v1/auth/register
func (a *API) register(w http.ResponseWriter, r *http.Request) error {
	if a.authSvc == nil {
		return errUnavailable
	}
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Name     string `json:"name"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	user, err := a.authSvc.Register(r.Context(), in.Email, in.Password, in.Name)
	switch {
	case errors.Is(err, auth.ErrInvalidEmail):
		return errValidation("invalid account", map[string]any{"fields": map[string]any{"email": "must be a valid email address"}})
	case errors.Is(err, auth.ErrWeakPassword):
		return errValidation("invalid account", map[string]any{"fields": map[string]any{
			"password": fmt.Sprintf("must be between %d and %d characters", auth.MinPasswordLength, auth.MaxPasswordLength),
		}})
	case errors.Is(err, auth.ErrEmailTaken):
		return newError(http.StatusConflict, CodeConflict, "an account with this email already exists", nil)
	case err != nil:
		return err
	}
	return a.startSession(w, r, user, http.StatusCreated)
}

// login verifies credentials and starts a session. POST /api/v1/auth/login
func (a *API) login(w http.ResponseWriter, r *http.Request) error {
	if a.authSvc == nil {
		return errUnavailable
	}
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	user, err := a.authSvc.Login(r.Context(), in.Email, in.Password)
	if errors.Is(err, auth.ErrInvalidCredentials) {
		return newError(http.StatusUnauthorized, CodeUnauthorized, "email or password is incorrect", nil)
	}
	if err != nil {
		return err
	}
	return a.startSession(w, r, user, http.StatusOK)
}

// startSession creates the session, sets the cookie and writes the auth payload.
func (a *API) startSession(w http.ResponseWriter, r *http.Request, user auth.User, status int) error {
	created, err := a.authSvc.CreateSession(r.Context(), user.ID, r.UserAgent(), clientIP(r))
	if err != nil {
		return err
	}
	a.setSessionCookie(w, created.Token, created.Session.ExpiresAt)
	writeJSON(w, status, authResponse{
		User:      userDTO{ID: user.ID, Name: user.DisplayName, Email: user.Email},
		CSRFToken: created.CSRFToken,
		ExpiresAt: created.Session.ExpiresAt.UTC().Format(time.RFC3339),
	})
	return nil
}

// me returns the current actor plus the CSRF token so a reloaded SPA can
// re-acquire it. GET /api/v1/auth/me
func (a *API) me(w http.ResponseWriter, r *http.Request) error {
	p := principal(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{"id": p.UserID, "name": p.Name, "csrfToken": p.CSRFToken})
	return nil
}

// logout revokes the current session (cookie or bearer) and clears the cookie.
// POST /api/v1/auth/logout
func (a *API) logout(w http.ResponseWriter, r *http.Request) error {
	if a.authSvc != nil {
		if token := requestSessionToken(r); token != "" {
			if err := a.authSvc.Logout(r.Context(), token); err != nil {
				return err
			}
		}
	}
	a.clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// listSessions returns the caller's active sessions without secrets.
// GET /api/v1/auth/sessions
func (a *API) listSessions(w http.ResponseWriter, r *http.Request) error {
	if a.authSvc == nil {
		return errUnavailable
	}
	p := principal(r.Context())
	sessions, err := a.authSvc.ListSessions(r.Context(), p.UserID)
	if err != nil {
		return err
	}
	items := make([]sessionDTO, 0, len(sessions))
	for _, session := range sessions {
		items = append(items, toSessionDTO(session, session.ID == p.SessionID))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
	return nil
}

// revokeSession revokes one of the caller's sessions.
// DELETE /api/v1/auth/sessions/{sessionID}
func (a *API) revokeSession(w http.ResponseWriter, r *http.Request) error {
	if a.authSvc == nil {
		return errUnavailable
	}
	p := principal(r.Context())
	id := r.PathValue("sessionID")
	err := a.authSvc.RevokeSession(r.Context(), p.UserID, id)
	if errors.Is(err, auth.ErrSessionNotFound) {
		return errNotFound("session", id)
	}
	if err != nil {
		return err
	}
	if id == p.SessionID {
		a.clearSessionCookie(w)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// revokeOtherSessions revokes every session except the caller's current one.
// DELETE /api/v1/auth/sessions
func (a *API) revokeOtherSessions(w http.ResponseWriter, r *http.Request) error {
	if a.authSvc == nil {
		return errUnavailable
	}
	p := principal(r.Context())
	n, err := a.authSvc.RevokeOthers(r.Context(), p.UserID, p.SessionID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"revoked": n})
	return nil
}

// --- DTOs & cookie/request helpers -------------------------------------------

type userDTO struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email,omitempty"`
}

type authResponse struct {
	User      userDTO `json:"user"`
	CSRFToken string  `json:"csrfToken"`
	ExpiresAt string  `json:"expiresAt"`
}

// sessionDTO deliberately omits token hashes and CSRF tokens.
type sessionDTO struct {
	ID         string `json:"id"`
	Current    bool   `json:"current"`
	UserAgent  string `json:"userAgent,omitempty"`
	IP         string `json:"ip,omitempty"`
	CreatedAt  string `json:"createdAt"`
	LastSeenAt string `json:"lastSeenAt"`
	ExpiresAt  string `json:"expiresAt"`
}

func toSessionDTO(s auth.Session, current bool) sessionDTO {
	return sessionDTO{
		ID:         s.ID,
		Current:    current,
		UserAgent:  s.UserAgent,
		IP:         s.IP,
		CreatedAt:  s.CreatedAt.UTC().Format(time.RFC3339),
		LastSeenAt: s.LastSeenAt.UTC().Format(time.RFC3339),
		ExpiresAt:  s.ExpiresAt.UTC().Format(time.RFC3339),
	}
}

// setSessionCookie writes the HttpOnly session cookie. SameSite=Lax mitigates
// CSRF on top of the double-submit token; Secure is set in production.
func (a *API) setSessionCookie(w http.ResponseWriter, token string, expires time.Time) {
	ttl := auth.DefaultSessionTTL
	if a.authSvc != nil {
		ttl = a.authSvc.SessionTTL()
	}
	http.SetCookie(w, &http.Cookie{
		Name:     auth.SessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   a.secure,
		SameSite: http.SameSiteLaxMode,
		Expires:  expires,
		MaxAge:   int(ttl.Seconds()),
	})
}

func (a *API) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     auth.SessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   a.secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// requestSessionToken extracts the opaque session token from the cookie or the
// Authorization header, cookie first.
func requestSessionToken(r *http.Request) string {
	if c, err := r.Cookie(auth.SessionCookieName); err == nil && c.Value != "" {
		return c.Value
	}
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	}
	return ""
}

// clientIP returns the peer address. It intentionally does not trust
// X-Forwarded-For without a configured trusted proxy.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
