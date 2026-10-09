package dispatcher

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitaleflex/axiom/services/agent/internal/protocol"
)

// fakeAdapter records every call and replays scripted outcomes.
type fakeAdapter struct {
	mu    sync.Mutex
	calls []string // "CreateRuntime", "HealthCheck", ...

	createErr        error
	networkErr       error
	startErr         error
	verifyReport     HealthReport
	verifyErr        error
	stopErr          error
	removeErr        error
	blockUntilCancel bool // HealthCheck/StartRuntime block until ctx is done
}

func (f *fakeAdapter) record(name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, name)
}

func (f *fakeAdapter) CreateRuntime(_ context.Context, _ CreateParams) error {
	f.record("CreateRuntime")
	return f.createErr
}

func (f *fakeAdapter) ConfigureNetwork(_ context.Context, _ NetworkParams) error {
	f.record("ConfigureNetwork")
	return f.networkErr
}

func (f *fakeAdapter) StartRuntime(ctx context.Context, _ StartParams) error {
	f.record("StartRuntime")
	if f.blockUntilCancel {
		<-ctx.Done()
		return ctx.Err()
	}
	return f.startErr
}

func (f *fakeAdapter) HealthCheck(ctx context.Context, _ VerifyParams) (HealthReport, error) {
	f.record("HealthCheck")
	if f.blockUntilCancel {
		<-ctx.Done()
		return HealthReport{}, ctx.Err()
	}
	return f.verifyReport, f.verifyErr
}

func (f *fakeAdapter) StopRuntime(_ context.Context, _ StopParams) error {
	f.record("StopRuntime")
	return f.stopErr
}

func (f *fakeAdapter) RemoveRuntime(_ context.Context, _ RemoveParams) error {
	f.record("RemoveRuntime")
	return f.removeErr
}

func (f *fakeAdapter) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// codedError carries a stable machine code, like the Docker adapter's errors.
type codedError struct{ code string }

func (e codedError) Error() string     { return "adapter failure: " + e.code }
func (e codedError) ErrorCode() string { return e.code }

var testIdentity = protocol.AgentIdentity{
	AgentID:  "agent_0123456789abcdef01234567",
	ServerID: "srv_0123456789abcdef012345",
}

// newOperation builds a valid operation of the given type.
func newOperation(opID, opType string, payload protocol.Payload) protocol.Operation {
	return protocol.Operation{
		Envelope: protocol.Envelope{
			Protocol:  protocol.Version,
			MessageID: "msg_0123456789abcdef0123456789abcdef",
			SentAt:    time.Now().UTC(),
		},
		OperationID:   opID,
		Type:          opType,
		DeploymentID:  "dep_0123456789abcdef01234567",
		ApplicationID: "app_0123456789abcdef01234567",
		ServerID:      testIdentity.ServerID,
		Payload:       payload,
	}
}

func newDispatcher(adapter *fakeAdapter, opts ...Option) *Dispatcher {
	opts = append([]Option{WithClock(func() time.Time { return time.Now().UTC() })}, opts...)
	return NewDispatcher([]Adapter{adapter}, slog.Default(), opts...)
}

// TestDispatchPerOperation is the per-operation table: every supported type
// maps to exactly one bounded adapter call, success and failure included.
func TestDispatchPerOperation(t *testing.T) {
	cases := []struct {
		name       string
		opType     string
		payload    protocol.Payload
		wantCall   string
		adapter    func() *fakeAdapter
		wantCode   string
		wantHealth bool
	}{
		{
			name:     "CREATE_RUNTIME success",
			opType:   protocol.OpCreateRuntime,
			payload:  protocol.Payload{ImageRef: "sha256:" + strings.Repeat("a", 64), Container: "axiom-app-1", Port: 3000},
			wantCall: "CreateRuntime",
			adapter:  func() *fakeAdapter { return &fakeAdapter{} },
		},
		{
			name:     "CREATE_RUNTIME failure surfaces adapter code",
			opType:   protocol.OpCreateRuntime,
			payload:  protocol.Payload{ImageRef: "sha256:" + strings.Repeat("a", 64), Container: "axiom-app-1", Port: 3000},
			wantCall: "CreateRuntime",
			adapter: func() *fakeAdapter {
				return &fakeAdapter{createErr: codedError{code: "RUNTIME_IMAGE_MISSING"}}
			},
			wantCode: "RUNTIME_IMAGE_MISSING",
		},
		{
			name:     "NETWORK success",
			opType:   protocol.OpConfigureNetwork,
			payload:  protocol.Payload{Container: "axiom-app-1", Proxy: "traefik", Domain: "app.example.com", TLS: true, Port: 443},
			wantCall: "ConfigureNetwork",
			adapter:  func() *fakeAdapter { return &fakeAdapter{} },
		},
		{
			name:     "NETWORK failure",
			opType:   protocol.OpConfigureNetwork,
			payload:  protocol.Payload{Container: "axiom-app-1", Proxy: "traefik", Domain: "app.example.com", TLS: true, Port: 443},
			wantCall: "ConfigureNetwork",
			adapter:  func() *fakeAdapter { return &fakeAdapter{networkErr: errors.New("boom")} },
			wantCode: CodeInternal,
		},
		{
			name:     "START success",
			opType:   protocol.OpStartRuntime,
			payload:  protocol.Payload{Container: "axiom-app-1"},
			wantCall: "StartRuntime",
			adapter:  func() *fakeAdapter { return &fakeAdapter{} },
		},
		{
			name:     "START failure",
			opType:   protocol.OpStartRuntime,
			payload:  protocol.Payload{Container: "axiom-app-1"},
			wantCall: "StartRuntime",
			adapter:  func() *fakeAdapter { return &fakeAdapter{startErr: codedError{code: "RUNTIME_CONTAINER_NOT_FOUND"}} },
			wantCode: "RUNTIME_CONTAINER_NOT_FOUND",
		},
		{
			name:     "VERIFY success carries the health report",
			opType:   protocol.OpVerifyHealth,
			payload:  protocol.Payload{Domain: "app.example.com", Path: "/health", TimeoutSeconds: 30},
			wantCall: "HealthCheck",
			adapter: func() *fakeAdapter {
				return &fakeAdapter{verifyReport: HealthReport{StatusCode: 200, LatencyMs: 84, Attempt: 1}}
			},
			wantHealth: true,
		},
		{
			name:     "VERIFY failure",
			opType:   protocol.OpVerifyHealth,
			payload:  protocol.Payload{Domain: "app.example.com", Path: "/health", TimeoutSeconds: 30},
			wantCall: "HealthCheck",
			adapter:  func() *fakeAdapter { return &fakeAdapter{verifyErr: errors.New("probe failed")} },
			wantCode: CodeInternal,
		},
		{
			name:     "STOP success",
			opType:   protocol.OpStopRuntime,
			payload:  protocol.Payload{Container: "axiom-app-1"},
			wantCall: "StopRuntime",
			adapter:  func() *fakeAdapter { return &fakeAdapter{} },
		},
		{
			name:     "STOP failure",
			opType:   protocol.OpStopRuntime,
			payload:  protocol.Payload{Container: "axiom-app-1"},
			wantCall: "StopRuntime",
			adapter:  func() *fakeAdapter { return &fakeAdapter{stopErr: errors.New("boom")} },
			wantCode: CodeInternal,
		},
		{
			name:     "REMOVE success",
			opType:   protocol.OpRemoveRuntime,
			payload:  protocol.Payload{Container: "axiom-app-1"},
			wantCall: "RemoveRuntime",
			adapter:  func() *fakeAdapter { return &fakeAdapter{} },
		},
		{
			name:     "REMOVE failure",
			opType:   protocol.OpRemoveRuntime,
			payload:  protocol.Payload{Container: "axiom-app-1"},
			wantCall: "RemoveRuntime",
			adapter:  func() *fakeAdapter { return &fakeAdapter{removeErr: errors.New("boom")} },
			wantCode: CodeInternal,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			adapter := tc.adapter()
			d := newDispatcher(adapter)
			op := newOperation("op_dep_0123456789abcdef01234567_CREATE_1", tc.opType, tc.payload)

			ack, result := d.Dispatch(context.Background(), op, testIdentity)

			if !ack.Accepted {
				t.Fatalf("ack.Accepted = false, want true (reason %q)", ack.Reason)
			}
			if adapter.count() != 1 {
				t.Fatalf("adapter calls = %v, want exactly 1 (%s)", adapter.calls, tc.wantCall)
			}
			if adapter.calls[0] != tc.wantCall {
				t.Fatalf("adapter call = %q, want %q", adapter.calls[0], tc.wantCall)
			}
			if tc.wantCode == "" {
				if !result.Success {
					t.Fatalf("result = %+v, want success", result)
				}
			} else {
				if result.Success {
					t.Fatalf("result = %+v, want failure with code %q", result, tc.wantCode)
				}
				if result.ErrorCode != tc.wantCode {
					t.Fatalf("errorCode = %q, want %q", result.ErrorCode, tc.wantCode)
				}
			}
			if tc.wantHealth {
				if result.Health == nil {
					t.Fatal("result.Health is nil, want the probe report")
				}
				if result.Health.StatusCode != 200 || result.Health.LatencyMs != 84 || result.Health.Attempt != 1 {
					t.Fatalf("health = %+v, want 200/84/1", result.Health)
				}
			}
			if result.OperationID != op.OperationID || result.DeploymentID != op.DeploymentID {
				t.Fatalf("result envelope = %s/%s, want %s/%s",
					result.OperationID, result.DeploymentID, op.OperationID, op.DeploymentID)
			}
			if result.FinishedAt.IsZero() {
				t.Error("result.FinishedAt is zero")
			}
		})
	}
}

// TestDispatchUnknownTypeRejectedWithoutExecution proves PREPARE (and any
// other type outside the closed set) is rejected as UNKNOWN_OPERATION and
// never reaches an adapter.
func TestDispatchUnknownTypeRejectedWithoutExecution(t *testing.T) {
	for _, opType := range []string{"PREPARE", "SHELL", "DEPLOY", ""} {
		t.Run(opType, func(t *testing.T) {
			adapter := &fakeAdapter{}
			d := newDispatcher(adapter)
			op := newOperation("op_dep_0123456789abcdef01234567_PREPARE_1", opType, protocol.Payload{})

			ack, result := d.Dispatch(context.Background(), op, testIdentity)

			if ack.Accepted {
				t.Fatal("ack.Accepted = true, want false for unknown type")
			}
			if ack.Reason != CodeUnknownOperation {
				t.Fatalf("ack.Reason = %q, want %q", ack.Reason, CodeUnknownOperation)
			}
			if result.Success || result.ErrorCode != CodeUnknownOperation {
				t.Fatalf("result = %+v, want UNKNOWN_OPERATION failure", result)
			}
			if adapter.count() != 0 {
				t.Fatalf("adapter was called %d times, want 0 (no execution)", adapter.count())
			}
		})
	}
}

// TestDispatchBindingMismatchRejected proves server-identity binding is
// enforced before execution.
func TestDispatchBindingMismatchRejected(t *testing.T) {
	adapter := &fakeAdapter{}
	d := newDispatcher(adapter)
	op := newOperation("op_dep_0123456789abcdef01234567_CREATE_1", protocol.OpCreateRuntime,
		protocol.Payload{ImageRef: "sha256:" + strings.Repeat("a", 64), Container: "axiom-app-1", Port: 3000})
	op.ServerID = "srv_ffffffffffffffffffffffff" // not this agent's server

	ack, result := d.Dispatch(context.Background(), op, testIdentity)

	if ack.Accepted || result.Success {
		t.Fatalf("dispatch succeeded, want rejection: ack=%+v result=%+v", ack, result)
	}
	if result.ErrorCode != CodeForbidden {
		t.Fatalf("errorCode = %q, want %q", result.ErrorCode, CodeForbidden)
	}
	if adapter.count() != 0 {
		t.Fatal("adapter executed an operation bound to another server")
	}
}

// TestDispatchDuplicateDeliveryReturnsCachedResult proves operation-ID
// idempotency: the same operation ID redelivered does not re-execute.
func TestDispatchDuplicateDeliveryReturnsCachedResult(t *testing.T) {
	adapter := &fakeAdapter{}
	d := newDispatcher(adapter)
	op := newOperation("op_dep_0123456789abcdef01234567_CREATE_1", protocol.OpCreateRuntime,
		protocol.Payload{ImageRef: "sha256:" + strings.Repeat("a", 64), Container: "axiom-app-1", Port: 3000})

	ack1, result1 := d.Dispatch(context.Background(), op, testIdentity)
	ack2, result2 := d.Dispatch(context.Background(), op, testIdentity)

	if !ack1.Accepted || !ack2.Accepted {
		t.Fatalf("acks = %+v / %+v, want both accepted", ack1, ack2)
	}
	if !result1.Success || !result2.Success {
		t.Fatalf("results = %+v / %+v, want both success", result1, result2)
	}
	if adapter.count() != 1 {
		t.Fatalf("adapter calls = %d, want exactly 1 (no re-execution)", adapter.count())
	}
	if result2 != result1 {
		t.Fatalf("redelivered result = %+v, want the cached %+v", result2, result1)
	}
}

// TestDispatchCacheHitAfterFailure proves failed results are cached too: a
// redelivery of a failed operation returns the original failure without
// re-executing.
func TestDispatchCacheHitAfterFailure(t *testing.T) {
	adapter := &fakeAdapter{createErr: codedError{code: "RUNTIME_IMAGE_MISSING"}}
	d := newDispatcher(adapter)
	op := newOperation("op_dep_0123456789abcdef01234567_CREATE_1", protocol.OpCreateRuntime,
		protocol.Payload{ImageRef: "sha256:" + strings.Repeat("a", 64), Container: "axiom-app-1", Port: 3000})

	_, result1 := d.Dispatch(context.Background(), op, testIdentity)
	_, result2 := d.Dispatch(context.Background(), op, testIdentity)

	if result1.Success || result2.Success {
		t.Fatal("want failure")
	}
	if result1.ErrorCode != "RUNTIME_IMAGE_MISSING" || result2.ErrorCode != "RUNTIME_IMAGE_MISSING" {
		t.Fatalf("codes = %q / %q, want RUNTIME_IMAGE_MISSING", result1.ErrorCode, result2.ErrorCode)
	}
	if adapter.count() != 1 {
		t.Fatalf("adapter calls = %d, want exactly 1", adapter.count())
	}
}

// TestDispatchAckPrecedesExecution proves the acknowledgement is produced
// before the adapter runs: the OnAck hook and the adapter append to a shared
// sequence log, and "ack" must always precede the adapter call.
func TestDispatchAckPrecedesExecution(t *testing.T) {
	adapter := &fakeAdapter{}
	d := newDispatcher(adapter)

	var mu sync.Mutex
	var sequence []string
	d.OnAck = func(protocol.Operation) {
		mu.Lock()
		defer mu.Unlock()
		sequence = append(sequence, "ack")
	}
	// Wrap the adapter so StartRuntime also logs "execute".
	seqAdapter := &sequenceAdapter{fake: adapter, log: &sequence, mu: &mu}
	d.Adapters = []Adapter{seqAdapter}

	op := newOperation("op_dep_0123456789abcdef01234567_START_1", protocol.OpStartRuntime,
		protocol.Payload{Container: "axiom-app-1"})
	ack, result := d.Dispatch(context.Background(), op, testIdentity)

	if !ack.Accepted || !result.Success {
		t.Fatalf("ack = %+v, result = %+v, want accepted/success", ack, result)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(sequence) != 2 || sequence[0] != "ack" || sequence[1] != "execute" {
		t.Fatalf("sequence = %v, want [ack execute]", sequence)
	}
}

// sequenceAdapter wraps a fakeAdapter and logs "execute" when StartRuntime runs.
type sequenceAdapter struct {
	fake *fakeAdapter
	log  *[]string
	mu   *sync.Mutex
}

func (s *sequenceAdapter) CreateRuntime(ctx context.Context, p CreateParams) error {
	s.mu.Lock()
	*s.log = append(*s.log, "execute")
	s.mu.Unlock()
	return s.fake.CreateRuntime(ctx, p)
}

func (s *sequenceAdapter) ConfigureNetwork(ctx context.Context, p NetworkParams) error {
	s.mu.Lock()
	*s.log = append(*s.log, "execute")
	s.mu.Unlock()
	return s.fake.ConfigureNetwork(ctx, p)
}

func (s *sequenceAdapter) StartRuntime(ctx context.Context, p StartParams) error {
	s.mu.Lock()
	*s.log = append(*s.log, "execute")
	s.mu.Unlock()
	return s.fake.StartRuntime(ctx, p)
}

func (s *sequenceAdapter) HealthCheck(ctx context.Context, p VerifyParams) (HealthReport, error) {
	s.mu.Lock()
	*s.log = append(*s.log, "execute")
	s.mu.Unlock()
	return s.fake.HealthCheck(ctx, p)
}

func (s *sequenceAdapter) StopRuntime(ctx context.Context, p StopParams) error {
	s.mu.Lock()
	*s.log = append(*s.log, "execute")
	s.mu.Unlock()
	return s.fake.StopRuntime(ctx, p)
}

func (s *sequenceAdapter) RemoveRuntime(ctx context.Context, p RemoveParams) error {
	s.mu.Lock()
	*s.log = append(*s.log, "execute")
	s.mu.Unlock()
	return s.fake.RemoveRuntime(ctx, p)
}

// TestDispatchTimeoutInterrupted proves the per-operation bound aborts a
// stuck adapter and reports CodeInterrupted.
func TestDispatchTimeoutInterrupted(t *testing.T) {
	adapter := &fakeAdapter{blockUntilCancel: true}
	d := newDispatcher(adapter, WithTimeouts(Timeouts{Start: 10 * time.Millisecond}))
	op := newOperation("op_dep_0123456789abcdef01234567_START_1", protocol.OpStartRuntime,
		protocol.Payload{Container: "axiom-app-1"})

	ack, result := d.Dispatch(context.Background(), op, testIdentity)

	if !ack.Accepted {
		t.Fatalf("ack.Accepted = false, want true (accepted, then timed out): %q", ack.Reason)
	}
	if result.Success || result.ErrorCode != CodeInterrupted {
		t.Fatalf("result = %+v, want %q failure", result, CodeInterrupted)
	}
}

// TestDispatchContextCancelInterrupted proves caller cancellation aborts
// execution and reports CodeInterrupted.
func TestDispatchContextCancelInterrupted(t *testing.T) {
	adapter := &fakeAdapter{blockUntilCancel: true}
	d := newDispatcher(adapter)
	op := newOperation("op_dep_0123456789abcdef01234567_START_1", protocol.OpStartRuntime,
		protocol.Payload{Container: "axiom-app-1"})

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()
	_, result := d.Dispatch(ctx, op, testIdentity)

	if result.Success || result.ErrorCode != CodeInterrupted {
		t.Fatalf("result = %+v, want %q failure", result, CodeInterrupted)
	}
}

// TestDispatchVerifyUsesPayloadTimeout proves VERIFY's bound comes from the
// payload's timeoutSeconds, not the defaults.
func TestDispatchVerifyUsesPayloadTimeout(t *testing.T) {
	adapter := &fakeAdapter{blockUntilCancel: true}
	d := newDispatcher(adapter, WithTimeouts(Timeouts{Start: time.Hour, Create: time.Hour}))
	op := newOperation("op_dep_0123456789abcdef01234567_VERIFY_1", protocol.OpVerifyHealth,
		protocol.Payload{Domain: "app.example.com", Path: "/health", TimeoutSeconds: 1})

	start := time.Now()
	_, result := d.Dispatch(context.Background(), op, testIdentity)
	elapsed := time.Since(start)

	if result.Success || result.ErrorCode != CodeInterrupted {
		t.Fatalf("result = %+v, want %q failure", result, CodeInterrupted)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("VERIFY ran %v, want it bounded by the 1s payload timeout", elapsed)
	}
}

// TestDispatchResultCacheIsBounded proves the results cache evicts the oldest
// entries beyond its bound.
func TestDispatchResultCacheIsBounded(t *testing.T) {
	adapter := &fakeAdapter{}
	d := newDispatcher(adapter, WithResultCacheSize(5))

	for i := 0; i < 7; i++ {
		op := newOperation(
			fmt.Sprintf("op_dep_0123456789abcdef01234567_CREATE_%d", i+1),
			protocol.OpCreateRuntime,
			protocol.Payload{ImageRef: "sha256:" + strings.Repeat("a", 64), Container: "axiom-app-1", Port: 3000})
		if _, result := d.Dispatch(context.Background(), op, testIdentity); !result.Success {
			t.Fatalf("dispatch %d failed: %+v", i, result)
		}
	}
	if got := d.results.Len(); got != 5 {
		t.Fatalf("results cache size = %d, want 5 (bounded)", got)
	}
	// The two oldest results were evicted; redelivering them re-executes
	// (and reports REPLAYED only if still in the dedupe window without a
	// cached result).
	op := newOperation("op_dep_0123456789abcdef01234567_CREATE_1", protocol.OpCreateRuntime,
		protocol.Payload{ImageRef: "sha256:" + strings.Repeat("a", 64), Container: "axiom-app-1", Port: 3000})
	_, result := d.Dispatch(context.Background(), op, testIdentity)
	if result.Success {
		t.Fatal("redelivery of an evicted op re-executed and succeeded; want REPLAYED")
	}
	if result.ErrorCode != CodeReplayed {
		t.Fatalf("errorCode = %q, want %q", result.ErrorCode, CodeReplayed)
	}
}

// TestDispatchNoAdapter proves validation and dedupe still work without an
// adapter; execution reports INTERNAL.
func TestDispatchNoAdapter(t *testing.T) {
	d := NewDispatcher(nil, slog.Default())
	op := newOperation("op_dep_0123456789abcdef01234567_CREATE_1", protocol.OpCreateRuntime,
		protocol.Payload{ImageRef: "sha256:" + strings.Repeat("a", 64), Container: "axiom-app-1", Port: 3000})

	ack, result := d.Dispatch(context.Background(), op, testIdentity)
	if !ack.Accepted {
		t.Fatalf("ack = %+v, want accepted", ack)
	}
	if result.Success || result.ErrorCode != CodeInternal {
		t.Fatalf("result = %+v, want INTERNAL failure", result)
	}
}

// TestTimeoutsFor documents the default per-operation bounds.
func TestTimeoutsFor(t *testing.T) {
	tm := Timeouts{}
	create := newOperation("op_x", protocol.OpCreateRuntime, protocol.Payload{})
	if got := tm.For(create); got != DefaultCreateTimeout {
		t.Fatalf("Create = %v, want %v", got, DefaultCreateTimeout)
	}
	verify := newOperation("op_x", protocol.OpVerifyHealth, protocol.Payload{TimeoutSeconds: 42})
	if got := tm.For(verify); got != 42*time.Second {
		t.Fatalf("Verify = %v, want 42s from payload", got)
	}
}

// TestDispatchRefusesOperationWithoutApplicationScope is the #145 boundary:
// an operation that does not name its application is refused with the stable
// INCOMPLETE_SCOPE code and never reaches an adapter.
func TestDispatchRefusesOperationWithoutApplicationScope(t *testing.T) {
	for _, applicationID := range []string{"", "app_short", "dep_0123456789abcdef01234567"} {
		adapter := &fakeAdapter{}
		d := newDispatcher(adapter)
		op := newOperation("op_dep_0123456789abcdef01234567_CREATE_RUNTIME_1",
			protocol.OpCreateRuntime, protocol.Payload{ImageRef: "sha256:" + strings.Repeat("c", 64), Container: "axiom-app-1", Port: 3000})
		op.ApplicationID = applicationID

		ack, result := d.Dispatch(context.Background(), op, testIdentity)
		if ack.Accepted {
			t.Fatalf("applicationId %q: operation must be refused", applicationID)
		}
		if ack.Reason != CodeIncompleteScope || result.ErrorCode != CodeIncompleteScope {
			t.Fatalf("applicationId %q: codes = %q/%q, want %q", applicationID, ack.Reason, result.ErrorCode, CodeIncompleteScope)
		}
		if len(adapter.calls) != 0 {
			t.Fatalf("applicationId %q: adapter was called %v, want no call", applicationID, adapter.calls)
		}
	}
}

// scopedFake records the scope Dispatch binds it to, proving the application
// travels from the operation to the adapter distinctly from the deployment.
type scopedFake struct {
	fakeAdapter
	applicationID string
	deploymentID  string
	serverID      string
}

func (s *scopedFake) WithScope(applicationID, deploymentID, serverID string) Adapter {
	s.applicationID, s.deploymentID, s.serverID = applicationID, deploymentID, serverID
	return s
}

// TestDispatchPassesFullScope proves WithScope receives the application, the
// deployment and the server, and that the application is never derived from
// the deployment.
func TestDispatchPassesFullScope(t *testing.T) {
	scoped := &scopedFake{}
	d := NewDispatcher([]Adapter{scoped}, slog.Default(),
		WithClock(func() time.Time { return time.Now().UTC() }))
	op := newOperation("op_dep_0123456789abcdef01234567_CREATE_RUNTIME_1",
		protocol.OpCreateRuntime, protocol.Payload{ImageRef: "sha256:" + strings.Repeat("c", 64), Container: "axiom-app-1", Port: 3000})
	op.ApplicationID = "app_ffffffffffffffffffffffff"

	if _, result := d.Dispatch(context.Background(), op, testIdentity); !result.Success {
		t.Fatalf("result = %+v, want success", result)
	}
	if scoped.applicationID != op.ApplicationID {
		t.Fatalf("applicationID = %q, want %q", scoped.applicationID, op.ApplicationID)
	}
	if scoped.deploymentID != op.DeploymentID || scoped.serverID != op.ServerID {
		t.Fatalf("scope = (%q, %q), want (%q, %q)", scoped.deploymentID, scoped.serverID, op.DeploymentID, op.ServerID)
	}
}
