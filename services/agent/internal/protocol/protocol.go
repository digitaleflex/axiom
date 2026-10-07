// Package protocol is the canonical Agent ↔ Engine domain contract
// (issue #75, docs/architecture/agent-protocol.md). It defines every message
// with validation, independent of transport (ADR-0008): the same structs are
// used whether the transport becomes long-poll HTTPS, WebSocket or gRPC.
//
// The agent initiates all communication. The Engine authorizes every
// operation; the agent re-validates type, identity binding, idempotency and
// freshness before executing anything.
package protocol

import (
	"errors"
	"fmt"
	"regexp"
	"time"
)

// Versions. The agent and Engine each support a range; they negotiate the
// highest common version at registration.
const (
	Version    = 1
	MinVersion = 1
)

// Compatible reports whether the peer's version range overlaps ours.
func Compatible(peerMin, peerMax int) bool {
	return peerMax >= MinVersion && peerMin <= Version
}

// Operation types. This set is closed: the agent rejects anything else,
// and the Engine never sends anything else. No shell payload exists.
const (
	OpCreateRuntime    = "CREATE_RUNTIME"
	OpConfigureNetwork = "NETWORK"
	OpStartRuntime     = "START"
	OpVerifyHealth     = "VERIFY"
	OpStopRuntime      = "STOP"
	OpRemoveRuntime    = "REMOVE"
)

// ValidOperation reports whether t is a known operation type.
func ValidOperation(t string) bool {
	switch t {
	case OpCreateRuntime, OpConfigureNetwork, OpStartRuntime, OpVerifyHealth, OpStopRuntime, OpRemoveRuntime:
		return true
	}
	return false
}

// Limits and formats.
const (
	MaxClockSkew     = 5 * time.Minute
	IdempotencyKeyV1 = "op_<deploymentID>_<STEP>_<attempt>"
)

var (
	idRe          = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)
	deploymentRe  = regexp.MustCompile(`^dep_[0-9a-f]{24}$`)
	operationIDRe = regexp.MustCompile(`^op_dep_[0-9a-f]{24}_[A-Z_]+_[0-9]+$`)
	correlationRe = regexp.MustCompile(`^req_[0-9a-f]{16}$`)
	agentIDRe     = regexp.MustCompile(`^agent_[0-9a-f]{24}$`)
	sha256Re      = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

// Envelope is carried by every message.
type Envelope struct {
	// Protocol is the message's protocol version.
	Protocol int `json:"protocol"`
	// MessageID is a unique ID for this transmission (dedupe, tracing).
	MessageID string `json:"messageId"`
	// SentAt is the sender timestamp (RFC3339). Receivers reject messages
	// outside MaxClockSkew (replay resistance) unless otherwise noted.
	SentAt time.Time `json:"sentAt"`
	// CorrelationID traces the message to its API request (req_…).
	CorrelationID string `json:"correlationId,omitempty"`
}

func (e Envelope) validate(now time.Time) error {
	if e.Protocol < MinVersion || e.Protocol > Version {
		return fmt.Errorf("%w: protocol %d", ErrVersion, e.Protocol)
	}
	if !idRe.MatchString(e.MessageID) {
		return fmt.Errorf("%w: messageId", ErrFormat)
	}
	if e.SentAt.IsZero() || e.SentAt.After(now.Add(MaxClockSkew)) || now.Sub(e.SentAt) > 24*time.Hour {
		return fmt.Errorf("%w: sentAt", ErrStale)
	}
	if e.CorrelationID != "" && !correlationRe.MatchString(e.CorrelationID) {
		return fmt.Errorf("%w: correlationId", ErrFormat)
	}
	return nil
}

// Errors.
var (
	ErrVersion = errors.New("protocol: unsupported version")
	ErrFormat  = errors.New("protocol: malformed field")
	ErrStale   = errors.New("protocol: message outside freshness window")
	ErrType    = errors.New("protocol: unknown or unsupported operation type")
	ErrBinding = errors.New("protocol: server identity mismatch")
	ErrReplay  = errors.New("protocol: replayed message")
)

// AgentIdentity binds an agent to its server. The agent ID is issued at
// registration and stored by both sides; every later message carries both.
type AgentIdentity struct {
	AgentID  string `json:"agentId"`
	ServerID string `json:"serverId"`
}

func (a AgentIdentity) validate() error {
	if !agentIDRe.MatchString(a.AgentID) {
		return fmt.Errorf("%w: agentId", ErrFormat)
	}
	if !idRe.MatchString(a.ServerID) {
		return fmt.Errorf("%w: serverId", ErrFormat)
	}
	return nil
}

// RegistrationRequest is sent once with the bootstrap credential (transport
// carries the credential, never this body). The Engine answers with the
// agent ID and the negotiated protocol version.
type RegistrationRequest struct {
	Envelope
	AgentIdentity
	AgentVersion string   `json:"agentVersion"`
	Capabilities []string `json:"capabilities"`
}

func (r RegistrationRequest) Validate(now time.Time) error {
	if err := r.Envelope.validate(now); err != nil {
		return err
	}
	if r.ServerID == "" {
		return fmt.Errorf("%w: serverId required (agentId is issued by the Engine)", ErrFormat)
	}
	if r.AgentVersion == "" || len(r.Capabilities) == 0 {
		return fmt.Errorf("%w: agentVersion and capabilities required", ErrFormat)
	}
	return nil
}

// RegistrationResponse issues the identity.
type RegistrationResponse struct {
	Envelope
	AgentIdentity
	// Negotiated is the protocol version both sides must use.
	Negotiated int `json:"negotiated"`
	// HeartbeatInterval tells the agent how often to report.
	HeartbeatIntervalSeconds int `json:"heartbeatIntervalSeconds"`
}

// Heartbeat replaces the local heartbeat log with Engine-visible liveness (#78).
type Heartbeat struct {
	Envelope
	AgentIdentity
	Status       string   `json:"status"` // READY | DEGRADED | OFFLINE (agent-observed)
	Capabilities []string `json:"capabilities"`
	CPUCount     int      `json:"cpuCount"`
	MemoryMB     int      `json:"memoryMb"`
	DiskFreeMB   int      `json:"diskFreeMb"`
}

func (h Heartbeat) Validate(now time.Time) error {
	if err := h.Envelope.validate(now); err != nil {
		return err
	}
	if err := h.AgentIdentity.validate(); err != nil {
		return err
	}
	switch h.Status {
	case "READY", "DEGRADED", "OFFLINE":
	default:
		return fmt.Errorf("%w: status", ErrFormat)
	}
	return nil
}

// Operation is one authorized unit of work. Payloads are typed per operation;
// there is no command string, no shell, no script.
type Operation struct {
	Envelope
	// OperationID is the idempotency key: op_<deploymentID>_<STEP>_<attempt>.
	// Retried attempts reuse attempt numbers deterministically, so redelivery
	// executes at most once per attempt.
	OperationID  string  `json:"operationId"`
	Type         string  `json:"type"`
	DeploymentID string  `json:"deploymentId"`
	ServerID     string  `json:"serverId"`
	Payload      Payload `json:"payload"`
}

func (o Operation) Validate(now time.Time, agent AgentIdentity) error {
	if err := o.Envelope.validate(now); err != nil {
		return err
	}
	if !ValidOperation(o.Type) {
		return fmt.Errorf("%w: %q", ErrType, o.Type)
	}
	if !operationIDRe.MatchString(o.OperationID) {
		return fmt.Errorf("%w: operationId", ErrFormat)
	}
	if !deploymentRe.MatchString(o.DeploymentID) {
		return fmt.Errorf("%w: deploymentId", ErrFormat)
	}
	// Server identity binding: the operation must target this agent's server.
	if o.ServerID != agent.ServerID {
		return fmt.Errorf("%w: operation targets %q, agent serves %q", ErrBinding, o.ServerID, agent.ServerID)
	}
	return o.Payload.validate(o.Type)
}

// Payload is the typed parameters of an operation.
type Payload struct {
	// CREATE_RUNTIME / image builds reference.
	ImageRef string `json:"imageRef,omitempty"`
	// Runtime container name (Axiom-assigned, DNS-safe).
	Container string `json:"container,omitempty"`
	// Application port inside the runtime.
	Port int `json:"port,omitempty"`
	// NETWORK.
	Proxy  string `json:"proxy,omitempty"`
	Domain string `json:"domain,omitempty"`
	TLS    bool   `json:"tls,omitempty"`
	// VERIFY.
	Path           string `json:"path,omitempty"`
	TimeoutSeconds int    `json:"timeoutSeconds,omitempty"`
}

func (p Payload) validate(t string) error {
	switch t {
	case OpCreateRuntime:
		if !sha256Re.MatchString(p.ImageRef) && !validImageRef(p.ImageRef) {
			return fmt.Errorf("%w: imageRef", ErrFormat)
		}
		if !idRe.MatchString(p.Container) || p.Port < 1 || p.Port > 65535 {
			return fmt.Errorf("%w: container/port", ErrFormat)
		}
	case OpConfigureNetwork:
		if !idRe.MatchString(p.Container) || p.Proxy == "" || p.Domain == "" || p.Port < 1 || p.Port > 65535 {
			return fmt.Errorf("%w: network parameters", ErrFormat)
		}
	case OpStartRuntime, OpStopRuntime, OpRemoveRuntime:
		if !idRe.MatchString(p.Container) {
			return fmt.Errorf("%w: container", ErrFormat)
		}
	case OpVerifyHealth:
		if p.Domain == "" || p.TimeoutSeconds < 1 || p.TimeoutSeconds > 600 {
			return fmt.Errorf("%w: verify parameters", ErrFormat)
		}
	}
	return nil
}

// validImageRef accepts registry/name:tag references built by the Engine
// (imageTag) as well as digests.
func validImageRef(s string) bool {
	if len(s) == 0 || len(s) > 512 || len(s) != len(trimSpace(s)) {
		return false
	}
	for _, r := range s {
		if r < 0x20 || r > 0x7e {
			return false
		}
	}
	return true
}

func trimSpace(s string) string {
	i, j := 0, len(s)
	for i < j && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n') {
		i++
	}
	for i < j && (s[j-1] == ' ' || s[j-1] == '\t' || s[j-1] == '\n') {
		j--
	}
	return s[i:j]
}

// Acknowledgement confirms receipt and validation of an operation, before
// execution. The Engine uses it to distinguish "rejected" from "running".
type Acknowledgement struct {
	Envelope
	OperationID  string `json:"operationId"`
	DeploymentID string `json:"deploymentId"`
	Accepted     bool   `json:"accepted"`
	Reason       string `json:"reason,omitempty"`
}

// Result reports the outcome of an executed operation.
type Result struct {
	Envelope
	OperationID  string `json:"operationId"`
	DeploymentID string `json:"deploymentId"`
	Success      bool   `json:"success"`
	// ErrorCode is a stable machine code (e.g. RUNTIME_IMAGE_MISSING,
	// HEALTH_CHECK_FAILED); Message is human detail without secrets.
	ErrorCode string `json:"errorCode,omitempty"`
	Message   string `json:"message,omitempty"`
	// Health carries probe details for VERIFY results.
	Health     *HealthReport `json:"health,omitempty"`
	FinishedAt time.Time     `json:"finishedAt"`
}

// HealthReport mirrors the Engine's probe report (internal/health) so VERIFY
// results flow back with status code and latency.
type HealthReport struct {
	StatusCode int   `json:"statusCode"`
	LatencyMs  int64 `json:"latencyMs"`
	Attempt    int   `json:"attempt"`
}

// Error is the protocol error envelope.
type Error struct {
	Envelope
	Code      string `json:"code"`
	Message   string `json:"message"`
	RelatedID string `json:"relatedId,omitempty"`
	Retryable bool   `json:"retryable"`
}

// Error codes.
const (
	CodeUnknownOperation = "UNKNOWN_OPERATION"
	CodeUnauthorized     = "UNAUTHORIZED"
	CodeForbidden        = "FORBIDDEN"
	CodeInvalidMessage   = "INVALID_MESSAGE"
	CodeStaleMessage     = "STALE_MESSAGE"
	CodeReplayed         = "REPLAYED"
	CodeVersionMismatch  = "VERSION_MISMATCH"
	CodeInternal         = "INTERNAL"
)

// Dedupe tracks recent message/operation IDs to reject replays within the
// freshness window. The Engine persists idempotency durably (#116); this is
// the agent-side guard for the transport window.
type Dedupe struct {
	seen map[string]time.Time
	ttl  time.Duration
}

// NewDedupe returns a replay guard with the given memory window.
func NewDedupe(ttl time.Duration) *Dedupe {
	return &Dedupe{seen: map[string]time.Time{}, ttl: ttl}
}

// Check records id and reports whether it was already seen (and not expired).
func (d *Dedupe) Check(id string, now time.Time) bool {
	if t, ok := d.seen[id]; ok && now.Sub(t) < d.ttl {
		return true
	}
	for k, t := range d.seen {
		if now.Sub(t) >= d.ttl {
			delete(d.seen, k)
		}
	}
	d.seen[id] = now
	return false
}
