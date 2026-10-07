// Package heartbeat implements the Agent → Engine liveness loop (#78): a
// ticker that sends signed protocol.Heartbeat messages on the negotiated
// interval, reports agent-observed status (READY, or DEGRADED when the
// capability probe fails), ships capability and resource samples from the
// capabilities package, and reconnects with backoff after transport
// failures. A 401 from the Engine (revoked/expired credential) stops the
// loop with ErrRevoked — retrying cannot succeed without re-registration.
//
// Offline-after-timeout is NOT enforced here: the Engine derives staleness
// from arrival time at read time (server.EffectiveStatusAt, StaleAfter 5m).
package heartbeat

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/digitaleflex/axiom/services/agent/internal/capabilities"
	"github.com/digitaleflex/axiom/services/agent/internal/protocol"
	"github.com/digitaleflex/axiom/services/agent/internal/security/auth"
)

// DefaultInterval is used when the registration response did not negotiate
// a heartbeat interval (or negotiated a non-positive one).
const DefaultInterval = 30 * time.Second

// StatusFunc reports the agent-observed status: READY or DEGRADED. It is
// consulted on every tick; a capability-probe failure forces DEGRADED
// regardless of what StatusFunc returns.
type StatusFunc func() string

// Transport delivers one heartbeat to the Engine.
type Transport interface {
	Send(ctx context.Context, hb protocol.Heartbeat) (HeartbeatResponse, error)
}

// HeartbeatResponse is the Engine's answer to one heartbeat.
type HeartbeatResponse struct {
	// OK is true when the Engine accepted and persisted the heartbeat.
	OK bool `json:"ok"`
	// ServerStatus is the Engine's effective server status after persisting
	// (READY, DEGRADED, OFFLINE, …).
	ServerStatus string `json:"serverStatus"`
}

// ErrRevoked reports that the Engine rejected the agent credential with
// HTTP 401 (revoked, expired or otherwise invalid). The loop must stop:
// no amount of retrying can succeed without re-registration (#77).
var ErrRevoked = errors.New("heartbeat: engine rejected the agent credential (401)")

// HTTPTransport posts signed heartbeats to POST /api/v1/agent/heartbeat.
// Requests are signed by auth.Client (Authorization, X-Agent-ID,
// X-Timestamp, X-Nonce), the same credential path as registration and
// rotation.
type HTTPTransport struct {
	BaseURL string
	Auth    *auth.Client
	HTTP    *http.Client
	// Now is injectable for deterministic tests.
	Now func() time.Time
}

// NewHTTPTransport returns an HTTPTransport for the Engine at baseURL.
func NewHTTPTransport(baseURL string, client *auth.Client) *HTTPTransport {
	return &HTTPTransport{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Auth:    client,
		HTTP:    &http.Client{Timeout: 30 * time.Second},
	}
}

// Send implements Transport.
func (t *HTTPTransport) Send(ctx context.Context, hb protocol.Heartbeat) (HeartbeatResponse, error) {
	raw, err := json.Marshal(hb)
	if err != nil {
		return HeartbeatResponse{}, fmt.Errorf("heartbeat: encode: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.BaseURL+"/api/v1/agent/heartbeat", bytes.NewReader(raw))
	if err != nil {
		return HeartbeatResponse{}, fmt.Errorf("heartbeat: request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := t.Auth.Do(req) // signs the request
	if err != nil {
		return HeartbeatResponse{}, fmt.Errorf("heartbeat: send: %w", err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusOK:
		var out HeartbeatResponse
		if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&out); err != nil {
			return HeartbeatResponse{}, fmt.Errorf("heartbeat: decode response: %w", err)
		}
		return out, nil
	case resp.StatusCode == http.StatusUnauthorized:
		// Revoked, expired or invalid credential: fatal for the loop.
		return HeartbeatResponse{}, fmt.Errorf("%w: engine returned 401", ErrRevoked)
	default:
		return HeartbeatResponse{}, fmt.Errorf("heartbeat: engine returned %d", resp.StatusCode)
	}
}

// Backoff computes the delay before the next retry after a failure.
type Backoff interface {
	// Next returns the delay before the next retry.
	Next() time.Duration
	// Reset clears the failure streak (after a successful send).
	Reset()
}

// ExponentialBackoff doubles the delay after each failure, capped at Max.
type ExponentialBackoff struct {
	Initial time.Duration
	Max     time.Duration
	Factor  float64

	current time.Duration
}

// Next implements Backoff.
func (b *ExponentialBackoff) Next() time.Duration {
	if b.current <= 0 {
		b.current = b.Initial
	} else {
		b.current = time.Duration(float64(b.current) * b.Factor)
		if b.current > b.Max {
			b.current = b.Max
		}
	}
	return b.current
}

// Reset implements Backoff.
func (b *ExponentialBackoff) Reset() { b.current = 0 }

// Loop sends heartbeats on the negotiated interval until ctx is cancelled
// or the Engine rejects the credential.
type Loop struct {
	// Identity is the registered agent identity (agentId + serverId).
	Identity protocol.AgentIdentity
	// Interval is the heartbeat period; non-positive falls back to
	// DefaultInterval.
	Interval time.Duration
	// Status reports the agent-observed status (READY normally). Optional;
	// defaults to READY.
	Status StatusFunc
	// Resources collects the capability/resource sample for each heartbeat.
	// Optional; when nil the heartbeat carries no capability data.
	Resources func(ctx context.Context) (capabilities.Report, error)
	// Transport delivers the heartbeat. Required.
	Transport Transport
	// Backoff paces retries after transport failures. Optional; defaults to
	// ExponentialBackoff{1s, 30s, ×2}.
	Backoff Backoff
	// Log receives structured lifecycle events. Optional.
	Log *slog.Logger

	// Now and NewID are injectable for deterministic tests.
	Now   func() time.Time
	NewID func() string
}

// NewLoop returns a Loop sending signed heartbeats over transport.
func NewLoop(identity protocol.AgentIdentity, interval time.Duration, transport Transport, log *slog.Logger) *Loop {
	if log == nil {
		log = slog.Default()
	}
	return &Loop{
		Identity:  identity,
		Interval:  interval,
		Transport: transport,
		Log:       log,
	}
}

// Run sends heartbeats until ctx is cancelled or the credential is rejected.
// Transport failures are retried with backoff (without waiting for the next
// tick); ErrRevoked stops the loop immediately.
func (l *Loop) Run(ctx context.Context) error {
	if l.Transport == nil {
		return errors.New("heartbeat: transport is required")
	}
	if l.Interval <= 0 {
		l.Interval = DefaultInterval
	}
	if l.Now == nil {
		l.Now = func() time.Time { return time.Now().UTC() }
	}
	if l.NewID == nil {
		l.NewID = newMessageID
	}
	if l.Status == nil {
		l.Status = func() string { return "READY" }
	}
	if l.Log == nil {
		l.Log = slog.Default()
	}
	if l.Backoff == nil {
		l.Backoff = &ExponentialBackoff{Initial: time.Second, Max: 30 * time.Second, Factor: 2}
	}

	ticker := time.NewTicker(l.Interval)
	defer ticker.Stop()
	for {
		err := l.sendOnce(ctx)
		if err != nil {
			if errors.Is(err, ErrRevoked) {
				l.Log.Error("heartbeat stopped: engine rejected the agent credential", "error", err)
				return err
			}
			l.Log.Warn("heartbeat send failed; retrying with backoff", "error", err)
			delay := l.Backoff.Next()
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
			// Retry immediately; do not wait for the next tick.
			continue
		}
		l.Backoff.Reset()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// sendOnce builds and sends one heartbeat. A capability-probe failure forces
// status DEGRADED: the agent is alive enough to report, but its view of the
// host is incomplete.
func (l *Loop) sendOnce(ctx context.Context) error {
	status := l.Status()
	var (
		caps     []string
		cpu, mem int
		disk     int
	)
	if l.Resources != nil {
		report, err := l.Resources(ctx)
		if err != nil {
			l.Log.Warn("capability probe failed; reporting DEGRADED", "error", err)
			status = "DEGRADED"
		} else {
			caps = report.Capabilities()
			cpu, mem, disk = report.CPUCount, report.MemoryMB, report.DiskFreeMB
		}
	}
	now := l.Now()
	hb := protocol.Heartbeat{
		Envelope: protocol.Envelope{
			Protocol:  protocol.Version,
			MessageID: l.NewID(),
			SentAt:    now,
		},
		AgentIdentity: l.Identity,
		Status:        status,
		Capabilities:  caps,
		CPUCount:      cpu,
		MemoryMB:      mem,
		DiskFreeMB:    disk,
	}
	if err := hb.Validate(now); err != nil {
		return fmt.Errorf("heartbeat: build: %w", err)
	}
	resp, err := l.Transport.Send(ctx, hb)
	if err != nil {
		return err
	}
	l.Log.Debug("heartbeat accepted", "serverStatus", resp.ServerStatus)
	return nil
}

// newMessageID returns a unique heartbeat message ID (hb_<24 hex>).
func newMessageID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		panic("heartbeat: entropy source unavailable: " + err.Error())
	}
	return "hb_" + hex.EncodeToString(b)
}
