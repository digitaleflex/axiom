package protocol

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func now() time.Time { return time.Now().UTC() }

func envelope() Envelope {
	return Envelope{Protocol: Version, MessageID: "msg_abc123", SentAt: now(), CorrelationID: "req_0123456789abcdef"}
}

func TestEnvelopeValidation(t *testing.T) {
	if err := envelope().validate(now()); err != nil {
		t.Fatalf("valid envelope: %v", err)
	}
	bad := envelope()
	bad.Protocol = 99
	if err := bad.validate(now()); err == nil {
		t.Fatal("future protocol must be rejected")
	}
	bad = envelope()
	bad.SentAt = now().Add(time.Hour)
	if err := bad.validate(now()); err == nil {
		t.Fatal("future timestamp beyond skew must be rejected")
	}
	bad = envelope()
	bad.SentAt = now().Add(-48 * time.Hour)
	if err := bad.validate(now()); err == nil {
		t.Fatal("ancient timestamp must be rejected")
	}
	bad = envelope()
	bad.CorrelationID = "not-a-correlation-id"
	if err := bad.validate(now()); err == nil {
		t.Fatal("malformed correlation ID must be rejected")
	}
}

func TestOperationValidation(t *testing.T) {
	agent := AgentIdentity{AgentID: "agent_" + strings.Repeat("a", 24), ServerID: "srv_1"}
	op := Operation{Envelope: envelope(), OperationID: "op_dep_" + strings.Repeat("b", 24) + "_CREATE_RUNTIME_1",
		Type: OpCreateRuntime, DeploymentID: "dep_" + strings.Repeat("b", 24),
		ApplicationID: "app_" + strings.Repeat("b", 24), ServerID: "srv_1",
		Payload: Payload{ImageRef: "sha256:" + strings.Repeat("c", 64), Container: "axiom-app-1", Port: 3000}}
	if err := op.Validate(now(), agent); err != nil {
		t.Fatalf("valid operation: %v", err)
	}
	op.Type = "RUN_SHELL"
	if err := op.Validate(now(), agent); err == nil {
		t.Fatal("arbitrary operation type must be rejected")
	} else if !strings.Contains(err.Error(), "unknown or unsupported") {
		t.Fatalf("wrong error: %v", err)
	}
	op.Type = OpCreateRuntime
	op.ServerID = "srv_2"
	if err := op.Validate(now(), agent); err == nil {
		t.Fatal("operation for another server must be rejected (identity binding)")
	}
	op.ServerID = "srv_1"
	op.OperationID = "op_reused-without-attempt"
	if err := op.Validate(now(), agent); err == nil {
		t.Fatal("idempotency key without attempt counter must be rejected")
	}
	op.OperationID = "op_dep_" + strings.Repeat("b", 24) + "_CREATE_RUNTIME_1"
	op.Payload.Port = 99999
	if err := op.Validate(now(), agent); err == nil {
		t.Fatal("invalid port must be rejected")
	}
}

// TestOperationRequiresApplicationScope is the #145 contract: applicationId is
// mandatory and is never inferred from deploymentId. An operation that omits
// it is refused with ErrIncompleteScope, whatever the envelope version.
func TestOperationRequiresApplicationScope(t *testing.T) {
	agent := AgentIdentity{AgentID: "agent_" + strings.Repeat("a", 24), ServerID: "srv_1"}
	base := func() Operation {
		return Operation{Envelope: envelope(), OperationID: "op_dep_" + strings.Repeat("b", 24) + "_CREATE_RUNTIME_1",
			Type: OpCreateRuntime, DeploymentID: "dep_" + strings.Repeat("b", 24),
			ApplicationID: "app_" + strings.Repeat("b", 24), ServerID: "srv_1",
			Payload: Payload{ImageRef: "sha256:" + strings.Repeat("c", 64), Container: "axiom-app-1", Port: 3000}}
	}
	for _, applicationID := range []string{"", "app_1", "not-an-app", "dep_" + strings.Repeat("b", 24)} {
		op := base()
		op.ApplicationID = applicationID
		err := op.Validate(now(), agent)
		if !errors.Is(err, ErrIncompleteScope) {
			t.Fatalf("applicationId %q: err = %v, want ErrIncompleteScope", applicationID, err)
		}
		if errors.Is(err, ErrFormat) {
			t.Fatalf("applicationId %q: refusal must be a scope refusal, not a format error", applicationID)
		}
	}
	// #145: an operation on the current version that omits applicationId is
	// still refused. The scope requirement is enforced by the check, never by
	// a version gate.
	if op := base(); op.Protocol != 2 {
		t.Fatalf("envelope version = %d, want 2", op.Protocol)
	}
	scoped := base()
	scoped.Protocol = Version
	scoped.ApplicationID = ""
	if err := scoped.Validate(now(), agent); !errors.Is(err, ErrIncompleteScope) {
		t.Fatalf("current-version operation without applicationId: err = %v, want ErrIncompleteScope", err)
	}
}

func TestPayloadPerType(t *testing.T) {
	cases := []struct {
		typ     string
		payload Payload
		ok      bool
	}{
		{OpConfigureNetwork, Payload{Container: "c", Proxy: "traefik", Domain: "app.example.com", Port: 3000}, true},
		{OpConfigureNetwork, Payload{Container: "c", Proxy: "traefik", Port: 3000}, false}, // no domain
		{OpStartRuntime, Payload{Container: "c"}, true},
		{OpVerifyHealth, Payload{Domain: "app.example.com", TimeoutSeconds: 30}, true},
		{OpVerifyHealth, Payload{Domain: "app.example.com", TimeoutSeconds: 0}, false},
		{OpStopRuntime, Payload{}, false}, // no container
	}
	for _, c := range cases {
		if err := c.payload.validate(c.typ); (err == nil) != c.ok {
			t.Errorf("%s %+v: want ok=%v, got %v", c.typ, c.payload, c.ok, err)
		}
	}
}

func TestVersionCompatibility(t *testing.T) {
	if !Compatible(1, 1) || !Compatible(1, 3) || !Compatible(MinVersion, Version) {
		t.Fatal("overlapping ranges must be compatible")
	}
	if Compatible(Version+1, Version+3) {
		t.Fatal("disjoint future range must be incompatible")
	}
	// #145: the application scope is required from the V2 envelope upwards —
	// the scope check enforces it at every accepted version (see
	// TestOperationRequiresApplicationScope), never the envelope version.
	// MinVersion = 1 keeps every existing V1 peer compatible.
	if Version != 2 || MinVersion != 1 {
		t.Fatalf("Version/MinVersion = %d/%d, want 2/1", Version, MinVersion)
	}
}

func TestDedupeRejectsReplay(t *testing.T) {
	d := NewDedupe(time.Hour)
	n := now()
	if d.Check("msg_1", n) {
		t.Fatal("first sight must be new")
	}
	if !d.Check("msg_1", n.Add(time.Minute)) {
		t.Fatal("second sight within window must be a replay")
	}
	if d.Check("msg_1", n.Add(2*time.Hour)) {
		t.Fatal("expired entries must be forgotten")
	}
}

func TestAllMessagesRoundTrip(t *testing.T) {
	agent := AgentIdentity{AgentID: "agent_" + strings.Repeat("a", 24), ServerID: "srv_1"}
	msgs := []any{
		RegistrationRequest{Envelope: envelope(), AgentVersion: "0.1.0", Capabilities: []string{"docker"}},
		RegistrationResponse{Envelope: envelope(), AgentIdentity: agent, Negotiated: 1, HeartbeatIntervalSeconds: 30},
		Heartbeat{Envelope: envelope(), AgentIdentity: agent, Status: "READY", CPUCount: 4},
		Operation{Envelope: envelope(), OperationID: "op_x", Type: OpStartRuntime, DeploymentID: "dep_x", ApplicationID: "app_x", ServerID: "srv_1", Payload: Payload{Container: "c"}},
		Acknowledgement{Envelope: envelope(), OperationID: "op_x", DeploymentID: "dep_x", Accepted: true},
		Result{Envelope: envelope(), OperationID: "op_x", DeploymentID: "dep_x", Success: true, Health: &HealthReport{StatusCode: 200, LatencyMs: 84, Attempt: 1}},
		Error{Envelope: envelope(), Code: CodeInvalidMessage, Retryable: false},
	}
	for _, m := range msgs {
		raw, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		var back map[string]any
		if err := json.Unmarshal(raw, &back); err != nil {
			t.Fatal(err)
		}
		if back["protocol"] != float64(Version) {
			t.Fatalf("protocol version lost: %v", back)
		}
	}
}

func TestRegistrationValidation(t *testing.T) {
	r := RegistrationRequest{Envelope: envelope(), AgentVersion: "0.1.0", Capabilities: []string{"docker"}}
	r.ServerID = "srv_1"
	if err := r.Validate(now()); err != nil {
		t.Fatalf("valid registration: %v", err)
	}
	r.Capabilities = nil
	if err := r.Validate(now()); err == nil {
		t.Fatal("capability report is required")
	}
}
