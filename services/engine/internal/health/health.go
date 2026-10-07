// Package health defines the probe report and policy types used to verify
// that a deployed application is actually alive before it can go LIVE
// (issue #65, API contract §16).
package health

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// MaxBodyBytes caps the persisted probe body so events stay small and
// free of secrets (API contract §14).
const MaxBodyBytes = 4096

// Probe types (planner HealthPlan.Type).
const (
	TypeHTTP = "http"
	TypeTCP  = "tcp"
)

// Outcome statuses recorded in health event data (API contract §16).
const (
	StatusHealthy   = "HEALTHY"
	StatusUnhealthy = "UNHEALTHY"
)

// Deployment event types for persisted health results (API contract §14,
// §16). Health results are stored as deployment events, not a new table.
const (
	EventPassed = "health.passed"
	EventFailed = "health.failed"
)

// ProbeReport is the outcome of one probe attempt. Body is truncated to
// MaxBodyBytes so it can be persisted without bloating the event log.
type ProbeReport struct {
	StatusCode int       `json:"statusCode"`
	LatencyMs  int64     `json:"latencyMs"`
	Body       string    `json:"body,omitempty"`
	CheckedAt  time.Time `json:"checkedAt"`
	Attempt    int       `json:"attempt"`
}

// Policy describes how a probe is evaluated. It mirrors the planner's
// HealthPlan with executor-friendly types.
type Policy struct {
	Type           string        // "http" | "tcp"
	Path           string        // HTTP path probed (empty for tcp)
	ExpectedStatus string        // "200" or a range like "200-399"
	Timeout        time.Duration // per-probe deadline (enforced by the agent)
	Retries        int           // executor-level attempts
	Interval       time.Duration // delay between attempts
}

// DefaultPolicy builds a Policy from plan-like parameters, filling zero
// values with production defaults.
func DefaultPolicy(typ, path, expectedStatus string, timeout time.Duration, retries int, interval time.Duration) Policy {
	if typ == "" {
		typ = TypeHTTP
	}
	if expectedStatus == "" {
		expectedStatus = "200-399"
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	if retries <= 0 {
		retries = 3
	}
	if interval <= 0 {
		interval = 2 * time.Second
	}
	return Policy{Type: typ, Path: path, ExpectedStatus: expectedStatus, Timeout: timeout, Retries: retries, Interval: interval}
}

// NewProbeReport builds a report, truncating Body to MaxBodyBytes and
// stamping CheckedAt.
func NewProbeReport(statusCode int, latencyMs int64, body string, attempt int) ProbeReport {
	return ProbeReport{
		StatusCode: statusCode,
		LatencyMs:  latencyMs,
		Body:       truncate(body),
		CheckedAt:  time.Now().UTC(),
		Attempt:    attempt,
	}
}

func truncate(s string) string {
	if len(s) <= MaxBodyBytes {
		return s
	}
	return s[:MaxBodyBytes]
}

// Evaluate returns StatusHealthy or StatusUnhealthy and an explicit reason
// for the latter. HTTP passes when the status code is in the expected range
// ("200-399" or a single code such as "200"); tcp passes when the probe
// connected (StatusCode 0 — the agent only reports a connected probe and
// returns an error otherwise).
func (p Policy) Evaluate(r ProbeReport) (status, reason string) {
	if p.Type == TypeTCP {
		if r.StatusCode == 0 {
			return StatusHealthy, ""
		}
		return StatusUnhealthy, fmt.Sprintf("tcp connect failed (status %d)", r.StatusCode)
	}
	if r.StatusCode == 0 {
		return StatusUnhealthy, "probe returned no HTTP status"
	}
	if !p.expects(r.StatusCode) {
		return StatusUnhealthy, fmt.Sprintf("status %d not in expected range %q", r.StatusCode, p.ExpectedStatus)
	}
	return StatusHealthy, ""
}

func (p Policy) expects(code int) bool {
	exp := strings.TrimSpace(p.ExpectedStatus)
	if exp == "" {
		return true
	}
	if i := strings.Index(exp, "-"); i > 0 {
		lo, err1 := strconv.Atoi(strings.TrimSpace(exp[:i]))
		hi, err2 := strconv.Atoi(strings.TrimSpace(exp[i+1:]))
		if err1 != nil || err2 != nil {
			return false
		}
		return code >= lo && code <= hi
	}
	want, err := strconv.Atoi(exp)
	if err != nil {
		return false
	}
	return code == want
}
