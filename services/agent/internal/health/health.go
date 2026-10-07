// Package health implements runtime health and readiness verification at the
// Agent/runtime boundary (#85). It probes an application over HTTP (default) or
// TCP until it passes, the retry budget is exhausted, or the context is done,
// and returns a structured report the Engine persists as health.passed /
// health.failed.
//
// Bounds (issue #85 acceptance criteria):
//
//   - No hang: every attempt is bounded by Spec.Timeout, the total poll loop is
//     bounded by the context the dispatcher supplies, and every wait is
//     interruptible by that context.
//   - Actionable failures: a non-matching HTTP status yields *UnhealthyError
//     (StatusCode/Reason/Attempt); a transport failure or timeout yields an
//     error that matches ErrNoResponse.
//   - Readiness vs liveness: Spec.Type selects the probe; a startup grace
//     period tolerates failures before the application is expected to be up
//     (those attempts are still reported through Report.Attempt).
//   - Policy from the plan: Spec carries the validated timeout/retry/grace
//     policy the dispatcher extracts from the DeploymentPlan.
//
// Report.ToProtocol converts to protocol.HealthReport so VERIFY results flow
// back over the agent protocol without this package importing any transport.
package health

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/digitaleflex/axiom/services/agent/internal/protocol"
)

// Probe types.
const (
	TypeHTTP = "http"
	TypeTCP  = "tcp"
)

// Defaults applied when a Spec leaves a field zero.
const (
	DefaultTimeout        = 5 * time.Second
	DefaultInterval       = 500 * time.Millisecond
	DefaultExpectedStatus = "200-399"
	maxBodyBytes          = 4096
)

// Sentinel errors. ErrUnhealthy is wrapped by *UnhealthyError; ErrNoResponse
// marks a probe that never produced an HTTP response (connection error, reset,
// or timeout) and is wrapped by *UnhealthyError.cause.
var (
	ErrUnhealthy   = errors.New("health: check failed")
	ErrNoResponse  = errors.New("health: no response")
	ErrInvalidSpec = errors.New("health: invalid spec")
)

// UnhealthyError is the structured failure for a health verification. It
// matches ErrUnhealthy via errors.Is, and ErrNoResponse when no response was
// received.
type UnhealthyError struct {
	StatusCode int    // last observed HTTP status (0 for TCP / no response)
	Reason     string // human detail without secrets
	Attempt    int    // 1-based attempt that produced this failure
	cause      error  // ErrNoResponse when no response was received
}

func (e *UnhealthyError) Error() string {
	if e.StatusCode != 0 {
		return fmt.Sprintf("health: unhealthy after attempt %d: status %d: %s", e.Attempt, e.StatusCode, e.Reason)
	}
	return fmt.Sprintf("health: unhealthy after attempt %d: %s", e.Attempt, e.Reason)
}

func (e *UnhealthyError) Unwrap() error {
	if e.cause != nil {
		return e.cause
	}
	return ErrUnhealthy
}

// Spec is one health-verification policy. Type selects HTTP or TCP. For HTTP,
// either URL is used verbatim or the target is built from Scheme (default
// https), Domain and Path. For TCP, Host and Port are dialled.
type Spec struct {
	Type string

	// URL is a complete target (takes precedence over Domain).
	URL string
	// Domain is a hostname; Scheme defaults to https and Path to "/".
	Domain string
	Scheme string
	Path   string

	// Host/Port select the TCP target when Type == TypeTCP.
	Host string
	Port int

	Timeout        time.Duration
	Retries        int
	Interval       time.Duration
	GracePeriod    time.Duration
	ExpectedStatus string // "200-399" or a single "200"
}

// Report is the outcome of a successful probe, or the last observed state on
// failure.
type Report struct {
	StatusCode int       `json:"statusCode"`
	LatencyMs  int64     `json:"latencyMs"`
	Attempt    int       `json:"attempt"`
	CheckedAt  time.Time `json:"checkedAt"`
	Body       string    `json:"body,omitempty"`
}

// ToProtocol converts the report to the wire HealthReport carried by a VERIFY
// Result.
func (r Report) ToProtocol() protocol.HealthReport {
	return protocol.HealthReport{
		StatusCode: r.StatusCode,
		LatencyMs:  r.LatencyMs,
		Attempt:    r.Attempt,
	}
}

// Checker runs health verification. All fields are injectable for tests; the
// zero value is usable (NewChecker just documents the production defaults).
type Checker struct {
	// HTTPClient is used for HTTP probes. nil = a client with no built-in
	// timeout (each attempt still gets a context deadline from Spec.Timeout).
	HTTPClient *http.Client
	// Clock is the time source for latency and the grace window. nil = time.Now.
	Clock func() time.Time
	// Sleep waits between attempts. nil = a context-aware timer. Tests inject a
	// fake that also advances Clock, so no real time passes.
	Sleep func(ctx context.Context, d time.Duration) error
}

// NewChecker returns a Checker with production defaults.
func NewChecker() *Checker {
	return &Checker{HTTPClient: &http.Client{}}
}

func (c *Checker) now() time.Time {
	if c.Clock != nil {
		return c.Clock()
	}
	return time.Now()
}

func (c *Checker) client() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{}
}

func (c *Checker) sleep(ctx context.Context, d time.Duration) error {
	if c.Sleep != nil {
		return c.Sleep(ctx, d)
	}
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Check polls the target until it passes, the retry budget is exhausted, or
// ctx is done. Failures inside the grace window do not count against the retry
// budget (they are "skipped"), but every probe still increments Report.Attempt.
//
// The number of *counted* failures tolerated is 1+Retries (so Retries is the
// number of retries after the first attempt). A non-zero GracePeriod therefore
// requires the caller to supply a deadline on ctx to stay bounded, which the
// dispatcher does.
func (c *Checker) Check(ctx context.Context, spec Spec) (Report, error) {
	if err := spec.normalize(); err != nil {
		return Report{}, err
	}
	low, high, err := parseStatusRange(spec.ExpectedStatus)
	if err != nil {
		return Report{}, err
	}

	start := c.now()
	allowed := 1 + maxInt(spec.Retries, 0)
	counted := 0
	attempt := 0
	var last *UnhealthyError

	for {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return c.failureReport(attempt, last), interrupted(ctxErr)
		}
		attempt++
		rep, err := c.probe(ctx, spec, low, high, attempt)
		if err == nil {
			rep.CheckedAt = c.now()
			return rep, nil
		}
		var ue *UnhealthyError
		if errors.As(err, &ue) {
			last = ue
		} else {
			last = &UnhealthyError{Reason: err.Error(), Attempt: attempt, cause: ErrNoResponse}
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return c.failureReport(attempt, last), interrupted(ctxErr)
		}

		inGrace := spec.GracePeriod > 0 && c.now().Sub(start) < spec.GracePeriod
		if !inGrace {
			counted++
			if counted >= allowed {
				return c.failureReport(attempt, last), last
			}
		}
		if err := c.sleep(ctx, spec.Interval); err != nil {
			return c.failureReport(attempt, last), interrupted(err)
		}
	}
}

func (c *Checker) failureReport(attempt int, last *UnhealthyError) Report {
	rep := Report{Attempt: attempt, CheckedAt: c.now()}
	if last != nil {
		rep.StatusCode = last.StatusCode
	}
	return rep
}

func (c *Checker) probe(ctx context.Context, spec Spec, low, high, attempt int) (Report, error) {
	if spec.Type == TypeTCP {
		return c.probeTCP(ctx, spec, attempt)
	}
	return c.probeHTTP(ctx, spec, low, high, attempt)
}

func (c *Checker) probeHTTP(ctx context.Context, spec Spec, low, high, attempt int) (Report, error) {
	target := spec.targetURL()
	actx, cancel := context.WithTimeout(ctx, spec.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(actx, http.MethodGet, target, nil)
	if err != nil {
		return Report{}, &UnhealthyError{Reason: "build request: " + err.Error(), Attempt: attempt, cause: ErrNoResponse}
	}
	start := c.now()
	resp, err := c.client().Do(req)
	latency := c.now().Sub(start).Milliseconds()
	if err != nil {
		return Report{LatencyMs: latency, Attempt: attempt},
			&UnhealthyError{Reason: err.Error(), Attempt: attempt, cause: ErrNoResponse}
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	rep := Report{
		StatusCode: resp.StatusCode,
		LatencyMs:  latency,
		Attempt:    attempt,
		Body:       strings.TrimSpace(string(body)),
	}
	if resp.StatusCode < low || resp.StatusCode > high {
		return rep, &UnhealthyError{
			StatusCode: resp.StatusCode,
			Reason:     fmt.Sprintf("status not in %s", spec.ExpectedStatus),
			Attempt:    attempt,
		}
	}
	return rep, nil
}

func (c *Checker) probeTCP(ctx context.Context, spec Spec, attempt int) (Report, error) {
	addr := net.JoinHostPort(spec.Host, strconv.Itoa(spec.Port))
	actx, cancel := context.WithTimeout(ctx, spec.Timeout)
	defer cancel()

	start := c.now()
	conn, err := (&net.Dialer{}).DialContext(actx, "tcp", addr)
	latency := c.now().Sub(start).Milliseconds()
	if err != nil {
		return Report{LatencyMs: latency, Attempt: attempt},
			&UnhealthyError{Reason: err.Error(), Attempt: attempt, cause: ErrNoResponse}
	}
	_ = conn.Close()
	return Report{LatencyMs: latency, Attempt: attempt}, nil
}

// normalize fills defaults and rejects unusable specs.
func (s *Spec) normalize() error {
	if s.Type == "" {
		s.Type = TypeHTTP
	}
	switch s.Type {
	case TypeHTTP, TypeTCP:
	default:
		return fmt.Errorf("%w: type %q", ErrInvalidSpec, s.Type)
	}
	if s.Timeout <= 0 {
		s.Timeout = DefaultTimeout
	}
	if s.Interval <= 0 {
		s.Interval = DefaultInterval
	}
	if s.ExpectedStatus == "" {
		s.ExpectedStatus = DefaultExpectedStatus
	}
	switch s.Type {
	case TypeHTTP:
		if s.URL == "" && s.Domain == "" {
			return fmt.Errorf("%w: url or domain required", ErrInvalidSpec)
		}
	case TypeTCP:
		if s.Host == "" || s.Port < 1 || s.Port > 65535 {
			return fmt.Errorf("%w: host and port required for tcp", ErrInvalidSpec)
		}
	}
	return nil
}

func (s Spec) targetURL() string {
	if s.URL != "" {
		return s.URL
	}
	scheme := s.Scheme
	if scheme == "" {
		scheme = "https"
	}
	path := s.Path
	if path == "" {
		path = "/"
	} else if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return scheme + "://" + s.Domain + path
}

// parseStatusRange parses "200-399" or a single "200".
func parseStatusRange(s string) (int, int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		s = DefaultExpectedStatus
	}
	if i := strings.IndexByte(s, '-'); i >= 0 {
		lo, errLo := strconv.Atoi(strings.TrimSpace(s[:i]))
		hi, errHi := strconv.Atoi(strings.TrimSpace(s[i+1:]))
		if errLo != nil || errHi != nil || lo < 100 || hi > 599 || lo > hi {
			return 0, 0, fmt.Errorf("%w: expected status %q", ErrInvalidSpec, s)
		}
		return lo, hi, nil
	}
	v, err := strconv.Atoi(s)
	if err != nil || v < 100 || v > 599 {
		return 0, 0, fmt.Errorf("%w: expected status %q", ErrInvalidSpec, s)
	}
	return v, v, nil
}

// interrupted wraps a context/loop interruption so callers can match
// context.Canceled / context.DeadlineExceeded with errors.Is.
func interrupted(err error) error {
	return fmt.Errorf("health: check interrupted: %w", err)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
