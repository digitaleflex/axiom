// Package dispatcher implements the Agent-side operation dispatcher (#80):
// bounded, idempotent execution of Engine-dispatched operations.
//
// Every dispatch follows the same pipeline (docs/architecture/agent-protocol.md
// §6):
//
//  1. Validate envelope, operation type and server-identity binding
//     (protocol.Operation.Validate).
//  2. Idempotency: a replayed operation ID returns the cached result from the
//     in-memory results cache — it is never re-executed.
//  3. Acknowledge (accepted) before execution.
//  4. Execute through the runtime Adapter with a per-operation timeout
//     (CREATE/NETWORK 3m, START/STOP/REMOVE 2m, VERIFY from the payload's
//     timeoutSeconds; all injectable).
//  5. Report a deterministic Result envelope with a stable error code.
//
// The operation set is closed: PREPARE is deliberately NOT part of the
// protocol V0.1 closed set (the Engine executor dropped it), so it is rejected
// here as UNKNOWN_OPERATION like any other unknown type — there is no PREPARE
// handler and no code path that could execute one.
//
// The dispatcher is transport-agnostic: the caller (transport adapter, #75
// follow-up) delivers protocol.Operation values and relays the returned
// Acknowledgement/Result messages.
package dispatcher

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/digitaleflex/axiom/services/agent/internal/protocol"
)

// Default per-operation execution bounds (injectable via Timeouts).
const (
	DefaultCreateTimeout  = 3 * time.Minute
	DefaultNetworkTimeout = 3 * time.Minute
	DefaultStartTimeout   = 2 * time.Minute
	DefaultStopTimeout    = 2 * time.Minute
	DefaultRemoveTimeout  = 2 * time.Minute

	// DefaultResultCacheSize bounds the in-memory results cache (last N
	// operation results kept for redelivery).
	DefaultResultCacheSize = 100
	// DefaultDedupeTTL is the replay window for operation IDs.
	DefaultDedupeTTL = time.Hour
)

// Result error codes. These mirror the protocol error envelope
// (docs/architecture/agent-protocol.md §9); the two Go modules cannot share
// the protocol package, so the mapping is repeated here by design.
const (
	CodeUnknownOperation = protocol.CodeUnknownOperation
	CodeInvalidMessage   = protocol.CodeInvalidMessage
	CodeForbidden        = protocol.CodeForbidden
	CodeReplayed         = protocol.CodeReplayed
	CodeVersionMismatch  = protocol.CodeVersionMismatch
	CodeStaleMessage     = protocol.CodeStaleMessage
	CodeInternal         = protocol.CodeInternal
	// CodeIncompleteScope is the stable code for an operation that omits the
	// resource scope required to execute it (#145: a missing ApplicationID).
	CodeIncompleteScope = protocol.CodeIncompleteScope
	// CodeInterrupted reports an aborted execution (context cancellation or
	// per-operation timeout). The protocol envelope has no INTERRUPTED code;
	// the dispatcher defines it locally.
	CodeInterrupted = "OPERATION_INTERRUPTED"
)

// Param structs mirror the protocol.Payload fields per operation. The
// dispatcher never sees raw protocol.Payload on its adapter boundary: each
// operation maps to exactly one typed parameter struct.
type (
	// CreateParams mirrors protocol.Payload for CREATE_RUNTIME.
	CreateParams struct {
		ImageRef  string `json:"imageRef"`
		Container string `json:"container"`
		Port      int    `json:"port"`
	}
	// NetworkParams mirrors protocol.Payload for NETWORK.
	NetworkParams struct {
		Container string `json:"container"`
		Proxy     string `json:"proxy"`
		Domain    string `json:"domain"`
		TLS       bool   `json:"tls"`
		Port      int    `json:"port"`
	}
	// StartParams mirrors protocol.Payload for START.
	StartParams struct {
		Container string `json:"container"`
	}
	// VerifyParams mirrors protocol.Payload for VERIFY.
	VerifyParams struct {
		Domain         string `json:"domain"`
		Path           string `json:"path"`
		TimeoutSeconds int    `json:"timeoutSeconds"`
	}
	// StopParams mirrors protocol.Payload for STOP.
	StopParams struct {
		Container string `json:"container"`
	}
	// RemoveParams mirrors protocol.Payload for REMOVE.
	RemoveParams struct {
		Container string `json:"container"`
	}
)

// HealthReport mirrors protocol.HealthReport so VERIFY results flow back with
// status code, latency and attempt (the modules cannot share the package).
type HealthReport struct {
	StatusCode int   `json:"statusCode"`
	LatencyMs  int64 `json:"latencyMs"`
	Attempt    int   `json:"attempt"`
}

// Adapter executes one bounded runtime operation against the host. The Docker
// adapter (#83) is the production implementation; tests fake it. Implementations
// must be safe for concurrent use and must honor ctx cancellation.
type Adapter interface {
	CreateRuntime(ctx context.Context, p CreateParams) error
	ConfigureNetwork(ctx context.Context, p NetworkParams) error
	StartRuntime(ctx context.Context, p StartParams) error
	HealthCheck(ctx context.Context, p VerifyParams) (HealthReport, error)
	StopRuntime(ctx context.Context, p StopParams) error
	RemoveRuntime(ctx context.Context, p RemoveParams) error
}

// ErrorCoder is implemented by adapter errors that carry a stable
// machine-readable code (e.g. the Docker adapter's *docker.Error). The
// dispatcher surfaces that code verbatim in Result.ErrorCode.
type ErrorCoder interface{ ErrorCode() string }

// ScopedAdapter is an Adapter that needs the operation's resource scope
// (application, deployment and server identity) to act on the runtime — the
// Docker adapter stamps canonical ownership labels from it, and the Traefik
// adapter names the per-deployment dynamic file from it. The closed operation
// payloads carry none of them, so Dispatch resolves a per-operation view
// through this optional interface before executing.
//
// It is additive: an Adapter that does not implement it is used as-is, exactly
// as before.
type ScopedAdapter interface {
	Adapter
	// WithScope returns the adapter bound to one operation's scope. The
	// returned value must be safe for concurrent use and must not mutate the
	// receiver. applicationID and deploymentID are distinct identities
	// (#145); neither is ever derived from the other.
	WithScope(applicationID, deploymentID, serverID string) Adapter
}

// scopedAdapter resolves the per-operation adapter when the configured one
// needs a resource scope.
func scopedAdapter(a Adapter, op protocol.Operation) Adapter {
	if a == nil {
		return nil
	}
	if s, ok := a.(ScopedAdapter); ok {
		return s.WithScope(op.ApplicationID, op.DeploymentID, op.ServerID)
	}
	return a
}

// Timeouts bounds each operation's execution. Zero values fall back to the
// defaults; VERIFY always uses the payload's timeoutSeconds (validated 1..600
// by the protocol).
type Timeouts struct {
	Create  time.Duration
	Network time.Duration
	Start   time.Duration
	Stop    time.Duration
	Remove  time.Duration
}

// For returns the execution bound for one operation.
func (t Timeouts) For(op protocol.Operation) time.Duration {
	switch op.Type {
	case protocol.OpCreateRuntime:
		return orDefault(t.Create, DefaultCreateTimeout)
	case protocol.OpConfigureNetwork:
		return orDefault(t.Network, DefaultNetworkTimeout)
	case protocol.OpStartRuntime:
		return orDefault(t.Start, DefaultStartTimeout)
	case protocol.OpStopRuntime:
		return orDefault(t.Stop, DefaultStopTimeout)
	case protocol.OpRemoveRuntime:
		return orDefault(t.Remove, DefaultRemoveTimeout)
	case protocol.OpVerifyHealth:
		// VERIFY's bound comes from the payload; validation guarantees >= 1s.
		if op.Payload.TimeoutSeconds > 0 {
			return time.Duration(op.Payload.TimeoutSeconds) * time.Second
		}
		return DefaultStartTimeout
	}
	return DefaultStartTimeout
}

func orDefault(v, def time.Duration) time.Duration {
	if v > 0 {
		return v
	}
	return def
}

// Dispatcher validates, dedupes, acknowledges and executes bounded
// operations. It is safe for concurrent use.
type Dispatcher struct {
	// Adapters are the runtime backends, in preference order; the first
	// adapter executes every operation. A production agent wires exactly one
	// (the Docker adapter, #83).
	Adapters []Adapter
	// Dedupe is the operation-ID replay guard. Optional; defaults to a
	// protocol.Dedupe with DefaultDedupeTTL.
	Dedupe *protocol.Dedupe
	// Timeouts bounds per-operation execution. Optional; zero values use the
	// defaults above.
	Timeouts Timeouts
	// Log receives structured dispatch events. Optional.
	Log *slog.Logger

	// Now and NewID are injectable for deterministic tests.
	Now   func() time.Time
	NewID func() string
	// OnAck is invoked with the operation after validation and dedupe, before
	// execution. It is the seam the transport uses to send the
	// Acknowledgement while execution runs.
	OnAck func(op protocol.Operation)

	results *resultCache
}

// NewDispatcher returns a Dispatcher over the given adapters.
func NewDispatcher(adapters []Adapter, log *slog.Logger, opts ...Option) *Dispatcher {
	if log == nil {
		log = slog.Default()
	}
	d := &Dispatcher{
		Adapters: adapters,
		Dedupe:   protocol.NewDedupe(DefaultDedupeTTL),
		Log:      log,
		Now:      func() time.Time { return time.Now().UTC() },
		NewID:    newMessageID,
		results:  newResultCache(DefaultResultCacheSize),
	}
	for _, o := range opts {
		o(d)
	}
	return d
}

// Option configures a Dispatcher.
type Option func(*Dispatcher)

// WithDedupe injects a custom replay guard.
func WithDedupe(d *protocol.Dedupe) Option {
	return func(x *Dispatcher) {
		if d != nil {
			x.Dedupe = d
		}
	}
}

// WithTimeouts injects custom per-operation bounds.
func WithTimeouts(t Timeouts) Option {
	return func(x *Dispatcher) { x.Timeouts = t }
}

// WithClock injects a deterministic clock.
func WithClock(now func() time.Time) Option {
	return func(x *Dispatcher) {
		if now != nil {
			x.Now = now
		}
	}
}

// WithIDGen injects a message-ID generator (tests).
func WithIDGen(f func() string) Option {
	return func(x *Dispatcher) {
		if f != nil {
			x.NewID = f
		}
	}
}

// WithOnAck injects the acknowledgement hook.
func WithOnAck(f func(protocol.Operation)) Option {
	return func(x *Dispatcher) { x.OnAck = f }
}

// WithResultCacheSize overrides the results cache bound (tests).
func WithResultCacheSize(n int) Option {
	return func(x *Dispatcher) { x.results = newResultCache(n) }
}

// Dispatch validates op, enforces idempotency, acknowledges and executes the
// operation with a per-operation timeout. It returns the Acknowledgement
// (accepted/rejected) and the Result. Replayed operation IDs return the
// cached result without re-executing; unknown types are rejected with
// UNKNOWN_OPERATION and never reach an adapter.
func (d *Dispatcher) Dispatch(ctx context.Context, op protocol.Operation, agentID protocol.AgentIdentity) (protocol.Acknowledgement, protocol.Result) {
	now := d.now()
	ack := protocol.Acknowledgement{
		Envelope:     envelope(protocol.Version, d.newID(), now),
		OperationID:  op.OperationID,
		DeploymentID: op.DeploymentID,
	}
	result := protocol.Result{
		Envelope:     envelope(protocol.Version, d.newID(), now),
		OperationID:  op.OperationID,
		DeploymentID: op.DeploymentID,
		FinishedAt:   now,
	}

	// 1. Envelope + type + binding validation.
	if err := op.Validate(now, agentID); err != nil {
		reason := codeForErr(err)
		ack.Accepted = false
		ack.Reason = reason
		result.Success = false
		result.ErrorCode = reason
		result.Message = err.Error()
		d.Log.Warn("operation rejected",
			"operationId", op.OperationID, "type", op.Type, "reason", reason)
		return ack, result
	}

	// 2. Idempotency: a replayed attempt returns the cached result.
	if d.Dedupe.Check(op.OperationID, now) {
		if cached, ok := d.results.Get(op.OperationID); ok {
			d.Log.Info("operation replayed; returning cached result", "operationId", op.OperationID)
			ack.Accepted = true
			return ack, cached
		}
		// Seen, but the result was evicted from the cache: report the replay
		// instead of re-executing blindly.
		ack.Accepted = false
		ack.Reason = CodeReplayed
		result.Success = false
		result.ErrorCode = CodeReplayed
		result.Message = "operation was already accepted; its result is no longer cached"
		return ack, result
	}

	// 3. Acknowledge before execution.
	ack.Accepted = true
	if d.OnAck != nil {
		d.OnAck(op)
	}

	// 4. Execute with the per-operation timeout.
	execCtx, cancel := context.WithTimeout(ctx, d.Timeouts.For(op))
	defer cancel()
	result = d.execute(execCtx, op)

	// 5. Cache the result so redelivery can answer without re-executing.
	d.results.Put(result)
	return ack, result
}

// execute runs the operation through the adapter and normalizes the outcome
// into a Result.
func (d *Dispatcher) execute(ctx context.Context, op protocol.Operation) protocol.Result {
	now := d.now()
	res := protocol.Result{
		Envelope:     envelope(protocol.Version, d.newID(), now),
		OperationID:  op.OperationID,
		DeploymentID: op.DeploymentID,
		FinishedAt:   now,
	}
	adapter := scopedAdapter(d.adapter(), op)
	if adapter == nil {
		res.Success = false
		res.ErrorCode = CodeInternal
		res.Message = "no runtime adapter configured"
		return res
	}

	var err error
	switch op.Type {
	case protocol.OpCreateRuntime:
		err = adapter.CreateRuntime(ctx, CreateParams{
			ImageRef: op.Payload.ImageRef, Container: op.Payload.Container, Port: op.Payload.Port,
		})
	case protocol.OpConfigureNetwork:
		err = adapter.ConfigureNetwork(ctx, NetworkParams{
			Container: op.Payload.Container, Proxy: op.Payload.Proxy,
			Domain: op.Payload.Domain, TLS: op.Payload.TLS, Port: op.Payload.Port,
		})
	case protocol.OpStartRuntime:
		err = adapter.StartRuntime(ctx, StartParams{Container: op.Payload.Container})
	case protocol.OpVerifyHealth:
		var report HealthReport
		report, err = adapter.HealthCheck(ctx, VerifyParams{
			Domain: op.Payload.Domain, Path: op.Payload.Path, TimeoutSeconds: op.Payload.TimeoutSeconds,
		})
		if err == nil {
			res.Health = &protocol.HealthReport{
				StatusCode: report.StatusCode, LatencyMs: report.LatencyMs, Attempt: report.Attempt,
			}
		}
	case protocol.OpStopRuntime:
		err = adapter.StopRuntime(ctx, StopParams{Container: op.Payload.Container})
	case protocol.OpRemoveRuntime:
		err = adapter.RemoveRuntime(ctx, RemoveParams{Container: op.Payload.Container})
	default:
		// Unreachable: validation rejects unknown types before execution.
		res.Success = false
		res.ErrorCode = CodeUnknownOperation
		res.Message = "unknown operation type: " + op.Type
		return res
	}

	if err != nil {
		res.Success = false
		res.ErrorCode, res.Message = classifyError(err)
		return res
	}
	res.Success = true
	return res
}

// adapter returns the first configured adapter, or nil.
func (d *Dispatcher) adapter() Adapter {
	if len(d.Adapters) == 0 {
		return nil
	}
	return d.Adapters[0]
}

func (d *Dispatcher) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now().UTC()
}

func (d *Dispatcher) newID() string {
	if d.NewID != nil {
		return d.NewID()
	}
	return newMessageID()
}

// codeForErr maps a validation failure to a protocol error code.
func codeForErr(err error) string {
	switch {
	case errors.Is(err, protocol.ErrVersion):
		return CodeVersionMismatch
	case errors.Is(err, protocol.ErrStale):
		return CodeStaleMessage
	case errors.Is(err, protocol.ErrType):
		return CodeUnknownOperation
	case errors.Is(err, protocol.ErrBinding):
		return CodeForbidden
	case errors.Is(err, protocol.ErrReplay):
		return CodeReplayed
	case errors.Is(err, protocol.ErrFormat):
		return CodeInvalidMessage
	case errors.Is(err, protocol.ErrIncompleteScope):
		return CodeIncompleteScope
	default:
		return CodeInternal
	}
}

// classifyError maps an execution failure to a stable result code. Context
// cancellation and per-operation timeouts both report CodeInterrupted;
// adapter errors carrying a stable code surface it verbatim.
func classifyError(err error) (code, message string) {
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return CodeInterrupted, "operation was interrupted"
	}
	var coder ErrorCoder
	if errors.As(err, &coder) && coder.ErrorCode() != "" {
		return coder.ErrorCode(), err.Error()
	}
	return CodeInternal, "operation failed"
}

// resultCache is a bounded in-memory cache of operation results keyed by
// operation ID. Redelivery of a known ID answers from the cache instead of
// re-executing (#80).
type resultCache struct {
	mu      sync.Mutex
	max     int
	order   []string // operation IDs, oldest first
	results map[string]protocol.Result
}

func newResultCache(max int) *resultCache {
	if max <= 0 {
		max = DefaultResultCacheSize
	}
	return &resultCache{max: max, results: map[string]protocol.Result{}}
}

// Get returns the cached result for id.
func (c *resultCache) Get(id string) (protocol.Result, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	r, ok := c.results[id]
	return r, ok
}

// Put records r, evicting the oldest entry when the cache is full.
func (c *resultCache) Put(r protocol.Result) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.results[r.OperationID]; !exists {
		c.order = append(c.order, r.OperationID)
		if len(c.order) > c.max {
			oldest := c.order[0]
			c.order = c.order[1:]
			delete(c.results, oldest)
		}
	}
	c.results[r.OperationID] = r
}

// Len returns the number of cached results.
func (c *resultCache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.results)
}

func envelope(version int, id string, sentAt time.Time) protocol.Envelope {
	return protocol.Envelope{Protocol: version, MessageID: id, SentAt: sentAt}
}

// newMessageID returns a unique message ID (msg_<24 hex>).
func newMessageID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		panic("dispatcher: entropy source unavailable: " + err.Error())
	}
	return "msg_" + hex.EncodeToString(b)
}
