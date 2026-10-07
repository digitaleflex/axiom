// Package transport hardens the Agent's outbound HTTP to the Engine (#88):
// request/response size limits, an allow-list of Engine endpoints, a
// token-bucket rate limiter and a timeout policy. Every validation failure is
// a typed error and the caller must not proceed: the helpers here fail closed.
//
// The agent is the client; the Engine is the only peer it talks to. This
// package is the boundary that keeps a misconfigured or compromised Engine
// endpoint from abusing the agent's network surface.
package transport

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Size limits.
const (
	// MaxRequestBytes bounds an outbound request body.
	MaxRequestBytes = 1 << 16 // 64 KiB
	// MaxResponseBytes bounds an inbound response body.
	MaxResponseBytes = 1 << 16 // 64 KiB
)

// Timeout policy.
const (
	// DefaultTimeout is the default outbound request timeout.
	DefaultTimeout = 30 * time.Second
	// MaxRedirects bounds the redirects the agent will follow.
	MaxRedirects = 3
)

// Errors (fail closed). Callers must treat any non-nil error as "do not
// proceed".
var (
	// ErrBodyTooLarge means a request or response body exceeds the limit.
	ErrBodyTooLarge = errors.New("transport: body exceeds maximum size")
	// ErrEndpointNotAllowed means the Engine endpoint is not allowlisted.
	ErrEndpointNotAllowed = errors.New("transport: engine endpoint is not allowlisted")
	// ErrInsecureEndpoint means the endpoint scheme is not permitted.
	ErrInsecureEndpoint = errors.New("transport: engine endpoint scheme is not permitted")
	// ErrRateLimited means the rate limit was exceeded.
	ErrRateLimited = errors.New("transport: rate limit exceeded")
	// ErrTimeout means the request timed out.
	ErrTimeout = errors.New("transport: request timed out")
)

// ReadAll reads from r, returning ErrBodyTooLarge if the body exceeds max.
// It wraps io.LimitReader to bound the read: it reads max+1 bytes so an
// oversized body is detected rather than silently truncated.
func ReadAll(r io.Reader, max int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, ErrBodyTooLarge
	}
	return data, nil
}

// RequireEngineEndpoint validates that rawURL is an allowed Engine endpoint.
// In production, https is required; http is only permitted for loopback
// (development). The host must be in allowlist. The production flag is
// explicit: the caller states the environment rather than inferring it.
func RequireEngineEndpoint(rawURL string, allowlist []string, production bool) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrEndpointNotAllowed, err)
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return fmt.Errorf("%w: scheme must be http or https, got %q", ErrEndpointNotAllowed, u.Scheme)
	}
	if u.Scheme == "http" {
		if production {
			return fmt.Errorf("%w: http is not permitted in production", ErrInsecureEndpoint)
		}
		if !isLoopback(u.Hostname()) {
			return fmt.Errorf("%w: http is only permitted for loopback in development", ErrInsecureEndpoint)
		}
	}
	if !hostAllowed(u, allowlist) {
		return fmt.Errorf("%w: %s", ErrEndpointNotAllowed, u.Host)
	}
	return nil
}

// isLoopback reports whether host is a loopback address or "localhost".
func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// hostAllowed reports whether the URL's host matches an allowlist entry.
// Entries may be "host" or "host:port"; both the full host and the bare
// hostname are matched.
func hostAllowed(u *url.URL, allowlist []string) bool {
	host := strings.ToLower(u.Host)
	hostname := strings.ToLower(u.Hostname())
	for _, a := range allowlist {
		a = strings.ToLower(a)
		if a == host || a == hostname {
			return true
		}
	}
	return false
}

// RateLimiter is a token-bucket rate limiter with an injectable clock.
type RateLimiter struct {
	mu     sync.Mutex
	rate   float64 // tokens per second
	burst  int
	tokens float64
	last   time.Time
	now    func() time.Time
}

// NewRateLimiter returns a RateLimiter with the given rate (tokens per
// second) and burst size. If now is nil, time.Now is used.
func NewRateLimiter(rate float64, burst int, now func() time.Time) *RateLimiter {
	if now == nil {
		now = func() time.Time { return time.Now() }
	}
	return &RateLimiter{
		rate:   rate,
		burst:  burst,
		tokens: float64(burst),
		now:    now,
	}
}

// Allow reports whether a request may proceed, consuming one token.
func (l *RateLimiter) Allow() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if !l.last.IsZero() {
		elapsed := now.Sub(l.last).Seconds()
		l.tokens += elapsed * l.rate
		if l.tokens > float64(l.burst) {
			l.tokens = float64(l.burst)
		}
	}
	l.last = now
	if l.tokens >= 1 {
		l.tokens--
		return true
	}
	return false
}

// Check accumulates validation errors. The caller must not proceed if Err()
// is non-nil. It makes the fail-closed contract explicit at call sites.
type Check struct {
	errs []error
}

// Add records err (if non-nil).
func (c *Check) Add(err error) {
	if err != nil {
		c.errs = append(c.errs, err)
	}
}

// Err returns the joined validation errors, or nil if all checks passed.
func (c *Check) Err() error {
	return errors.Join(c.errs...)
}
