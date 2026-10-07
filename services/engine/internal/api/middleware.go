package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net/http"
	"regexp"
	"runtime/debug"
	"strings"
	"time"
)

type ctxKey int

const (
	requestIDKey ctxKey = iota
	principalKey
)

var validRequestID = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// requestIDMiddleware accepts a well-formed client X-Request-ID or generates one,
// echoes it in the response and stores it in the context (ADR-0006).
func requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if !validRequestID.MatchString(id) {
			b := make([]byte, 8)
			_, _ = rand.Read(b)
			id = "req_" + hex.EncodeToString(b)
		}
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey, id)))
	})
}

func requestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

// statusRecorder captures the status code for access logs while keeping
// http.Flusher available for SSE.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) { s.status = code; s.ResponseWriter.WriteHeader(code) }
func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// accessLog logs method, path, status and duration. It never logs headers,
// query strings or bodies (they may contain secrets).
func (a *API) accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		a.log.Info("http request", "requestId", requestID(r.Context()), "method", r.Method, "path", r.URL.Path,
			"status", rec.status, "durationMs", time.Since(start).Milliseconds())
	})
}

// recoverer converts panics into a 500 envelope without leaking details.
func (a *API) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				a.log.Error("panic", "requestId", requestID(r.Context()), "panic", v, "stack", string(debug.Stack()))
				a.writeError(w, r, errors.New("panic"))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// Principal is the authenticated actor.
type Principal struct {
	UserID string `json:"id"`
	Name   string `json:"name"`
}

func principal(ctx context.Context) Principal {
	p, _ := ctx.Value(principalKey).(Principal)
	return p
}

// Authenticator resolves the actor of a request. It is the boundary that the
// user authentication work (#125) replaces; it must fail closed.
type Authenticator interface {
	Authenticate(r *http.Request) (Principal, error)
}

// ErrUnauthenticated means credentials are missing or invalid.
var ErrUnauthenticated = errors.New("unauthenticated")

// TokenAuthenticator accepts a single static bearer token (interim, #125).
type TokenAuthenticator struct {
	hash      [32]byte
	principal Principal
}

func NewTokenAuthenticator(token string, p Principal) *TokenAuthenticator {
	return &TokenAuthenticator{hash: sha256.Sum256([]byte(token)), principal: p}
}

func (t *TokenAuthenticator) Authenticate(r *http.Request) (Principal, error) {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return Principal{}, ErrUnauthenticated
	}
	got := sha256.Sum256([]byte(strings.TrimPrefix(h, "Bearer ")))
	if subtle.ConstantTimeCompare(got[:], t.hash[:]) != 1 {
		return Principal{}, ErrUnauthenticated
	}
	return t.principal, nil
}

// DevAuthenticator authenticates every request as a local developer.
// Only wired in the development environment without a configured token.
type DevAuthenticator struct{ Principal Principal }

func (d DevAuthenticator) Authenticate(*http.Request) (Principal, error) { return d.Principal, nil }

// denyAll is used when no authenticator is configured: fail closed.
type denyAll struct{}

func (denyAll) Authenticate(*http.Request) (Principal, error) { return Principal{}, ErrUnauthenticated }

func (a *API) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, err := a.auth.Authenticate(r)
		if err != nil {
			w.Header().Set("WWW-Authenticate", `Bearer realm="axiom"`)
			a.writeError(w, r, newError(http.StatusUnauthorized, CodeUnauthorized, "authentication is required", nil))
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalKey, p)))
	})
}
