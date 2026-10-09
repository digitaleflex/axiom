package agentclient

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitaleflex/axiom/services/engine/internal/deployment"
	"github.com/digitaleflex/axiom/services/engine/internal/executor"
	"github.com/digitaleflex/axiom/services/engine/internal/health"
	"github.com/digitaleflex/axiom/services/engine/internal/server"
)

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// fakeAgent records the operations posted to it and answers with a scripted
// response.
type fakeAgent struct {
	mu    sync.Mutex
	ops   []map[string]any
	heads []http.Header
	// status and body are the answer; empty body means a success result.
	status  int
	body    string
	handler func(w http.ResponseWriter, r *http.Request)
	srv     *httptest.Server
}

func newFakeAgent(t *testing.T) *fakeAgent {
	t.Helper()
	a := &fakeAgent{}
	a.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.handler != nil {
			a.handler(w, r)
			return
		}
		raw, _ := io.ReadAll(io.LimitReader(r.Body, MaxRequestBytes))
		var op map[string]any
		_ = json.Unmarshal(raw, &op)
		a.mu.Lock()
		a.ops = append(a.ops, op)
		a.heads = append(a.heads, r.Header.Clone())
		status, body := a.status, a.body
		a.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if status == 0 {
			status = http.StatusOK
		}
		if body == "" {
			body = `{"protocol":2,"operationId":"","deploymentId":"","success":true}`
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(a.srv.Close)
	return a
}

func (a *fakeAgent) url() string { return a.srv.URL }

func (a *fakeAgent) received() ([]map[string]any, []http.Header) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]map[string]any(nil), a.ops...), append([]http.Header(nil), a.heads...)
}

type fakeServers struct{ rec server.Record }

func (f fakeServers) Get(_ context.Context, id string) (server.Record, error) {
	if f.rec.ID != id {
		return server.Record{}, server.ErrNotFound
	}
	return f.rec, nil
}

// fakeApplications resolves a deployment to the application it rolls out (#145).
type fakeApplications struct {
	rec deployment.Record
	err error
}

func (f fakeApplications) Get(_ context.Context, id string) (deployment.Record, error) {
	if f.err != nil {
		return deployment.Record{}, f.err
	}
	if id != f.rec.ID {
		return deployment.Record{}, deployment.ErrNotFound
	}
	return f.rec, nil
}

// scopedApplications resolves every deployment id to testApplicationID.
type scopedApplications struct{}

func (scopedApplications) Get(_ context.Context, id string) (deployment.Record, error) {
	return deployment.Record{ID: id, ApplicationID: testApplicationID}, nil
}

const testApplicationID = "app_0123456789abcdef01234567"

type staticCred struct{}

func (staticCred) Credential(context.Context, string) (Credential, error) {
	return Credential{Token: "ac_test", Version: 3}, nil
}

func newClient(t *testing.T, a *fakeAgent) *Client {
	t.Helper()
	return &Client{
		Servers:      fakeServers{rec: server.Record{ID: "srv_1", Address: "203.0.113.10"}},
		Applications: scopedApplications{},
		Credentials:  staticCred{},
		Endpoint:     func(context.Context, string, string) (string, error) { return a.url(), nil },
		Production:   false,
		Log:          discardLogger(),
		Now:          func() time.Time { return time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC) },
		NewNonce:     func() string { return "nonce-1" },
	}
}

func op() executor.Operation {
	return executor.Operation{
		OperationID:   "op_dep_0123456789abcdef01234567_CREATE_RUNTIME_1",
		CorrelationID: "req_0123456789abcdef",
		DeploymentID:  "dep_0123456789abcdef01234567",
		ServerID:      "srv_1",
	}
}

func TestCreateRuntimeDispatchesProtocolOperation(t *testing.T) {
	a := newFakeAgent(t)
	c := newClient(t, a)
	if err := c.CreateRuntime(context.Background(), executor.CreateRuntimeRequest{
		Operation: op(), ImageRef: "axiom-local/acme-web:e8e8e8e-1a2b3c4d", Container: "axiom-acme-web-0123", Port: 3000,
	}); err != nil {
		t.Fatalf("CreateRuntime: %v", err)
	}
	ops, heads := a.received()
	if len(ops) != 1 {
		t.Fatalf("agent received %d operations, want 1", len(ops))
	}
	got := ops[0]
	for key, want := range map[string]any{
		"protocol": float64(ProtocolVersion), "type": "CREATE_RUNTIME",
		"operationId":   "op_dep_0123456789abcdef01234567_CREATE_RUNTIME_1",
		"deploymentId":  "dep_0123456789abcdef01234567",
		"applicationId": testApplicationID, "serverId": "srv_1",
		"correlationId": "req_0123456789abcdef", "messageId": "op_dep_0123456789abcdef01234567_CREATE_RUNTIME_1",
		"sentAt": "2026-10-08T12:00:00Z",
	} {
		if got[key] != want {
			t.Fatalf("operation %s = %v, want %v (full: %v)", key, got[key], want, got)
		}
	}
	p, _ := got["payload"].(map[string]any)
	if p["imageRef"] != "axiom-local/acme-web:e8e8e8e-1a2b3c4d" || p["container"] != "axiom-acme-web-0123" || p["port"] != float64(3000) {
		t.Fatalf("payload = %v", p)
	}
	// No shell payload exists: the message carries only typed scalars.
	if _, leaked := got["command"]; leaked {
		t.Fatal("operation carries a command field; there must be no shell payload")
	}
	h := heads[0]
	if h.Get("Authorization") != "Bearer ac_test" || h.Get("X-Credential-Version") != "3" || h.Get("X-Nonce") == "" || h.Get("X-Timestamp") == "" {
		t.Fatalf("signed headers = %v", h)
	}
	if h.Get("Content-Type") != "application/json" {
		t.Fatalf("content type = %q", h.Get("Content-Type"))
	}
}

func TestNetworkStartUseClosedOperationSet(t *testing.T) {
	a := newFakeAgent(t)
	c := newClient(t, a)
	ctx := context.Background()
	if err := c.ConfigureNetwork(ctx, executor.NetworkRequest{
		Operation: executor.Operation{OperationID: "op_dep_0123456789abcdef01234567_NETWORK_1", DeploymentID: "dep_0123456789abcdef01234567", ServerID: "srv_1"},
		Container: "axiom-acme-web-0123", Proxy: "traefik", Domain: "app.acme.dev", TLS: true, Port: 443,
	}); err != nil {
		t.Fatalf("ConfigureNetwork: %v", err)
	}
	if err := c.StartRuntime(ctx, executor.StartRequest{
		Operation: executor.Operation{OperationID: "op_dep_0123456789abcdef01234567_START_1", DeploymentID: "dep_0123456789abcdef01234567", ServerID: "srv_1"},
		Container: "axiom-acme-web-0123",
	}); err != nil {
		t.Fatalf("StartRuntime: %v", err)
	}
	if err := c.StopContainer(ctx, executor.Operation{OperationID: "op_dep_0123456789abcdef01234567_STOP_1", DeploymentID: "dep_0123456789abcdef01234567", ServerID: "srv_1"}, "axiom-acme-web-0123"); err != nil {
		t.Fatalf("StopContainer: %v", err)
	}
	if err := c.RemoveContainer(ctx, executor.Operation{OperationID: "op_dep_0123456789abcdef01234567_REMOVE_1", DeploymentID: "dep_0123456789abcdef01234567", ServerID: "srv_1"}, "axiom-acme-web-0123"); err != nil {
		t.Fatalf("RemoveContainer: %v", err)
	}
	ops, _ := a.received()
	want := []string{"NETWORK", "START", "STOP", "REMOVE"}
	if len(ops) != len(want) {
		t.Fatalf("received %d operations, want %d", len(ops), len(want))
	}
	for i, typ := range want {
		if ops[i]["type"] != typ {
			t.Fatalf("operation %d type = %v, want %s", i, ops[i]["type"], typ)
		}
	}
}

func TestHealthCheckReturnsReportEvenWhenUnhealthy(t *testing.T) {
	a := newFakeAgent(t)
	a.body = `{"protocol":2,"success":false,"errorCode":"HEALTH_CHECK_FAILED","message":"status 503",
		"health":{"statusCode":503,"latencyMs":91,"attempt":2}}`
	c := newClient(t, a)
	report, err := c.HealthCheck(context.Background(), executor.HealthCheckRequest{
		Operation: executor.Operation{OperationID: "op_dep_0123456789abcdef01234567_VERIFY_1", DeploymentID: "dep_0123456789abcdef01234567", ServerID: "srv_1"},
		Domain:    "app.acme.dev", Path: "/healthz", TimeoutSeconds: 5,
	})
	if err != nil {
		t.Fatalf("an HTTP answer that fails the policy is a report, not an error: %v", err)
	}
	if report.StatusCode != 503 || report.LatencyMs != 91 || report.Attempt != 2 {
		t.Fatalf("report = %+v", report)
	}
	if _, reason := health.DefaultPolicy("http", "/healthz", "200-399", time.Second, 3, time.Second).Evaluate(report); reason == "" {
		t.Fatal("an unhealthy report must be rejected by the policy")
	}
}

func TestHealthCheckRejectsSuccessWithoutReport(t *testing.T) {
	a := newFakeAgent(t)
	a.body = `{"protocol":2,"success":true}`
	c := newClient(t, a)
	_, err := c.HealthCheck(context.Background(), executor.HealthCheckRequest{
		Operation: executor.Operation{OperationID: "op_dep_0123456789abcdef01234567_VERIFY_1", DeploymentID: "dep_0123456789abcdef01234567", ServerID: "srv_1"},
		Domain:    "app.acme.dev", Path: "/healthz", TimeoutSeconds: 5,
	})
	var e *Error
	if !errors.As(err, &e) || e.Code != CodeMalformed {
		t.Fatalf("VERIFY without a report must not be an implied pass, got %v", err)
	}
}

func TestRejectionAndFailureCarryAgentCode(t *testing.T) {
	a := newFakeAgent(t)
	c := newClient(t, a)

	a.body = `{"protocol":2,"accepted":false,"reason":"FORBIDDEN"}`
	err := c.StartRuntime(context.Background(), executor.StartRequest{
		Operation: executor.Operation{OperationID: "op_d_START_1", DeploymentID: "dep_1", ServerID: "srv_1"}, Container: "c"})
	var e *Error
	if !errors.As(err, &e) || e.Code != CodeRejected || e.Message != "FORBIDDEN" {
		t.Fatalf("rejection = %v", err)
	}

	a.body = `{"protocol":2,"success":false,"errorCode":"RUNTIME_IMAGE_MISSING","message":"no such image"}`
	err = c.CreateRuntime(context.Background(), executor.CreateRuntimeRequest{
		Operation: executor.Operation{OperationID: "op_d_CREATE_RUNTIME_1", DeploymentID: "dep_1", ServerID: "srv_1"},
		ImageRef:  "axiom-local/x:t", Container: "c", Port: 3000})
	if !errors.As(err, &e) || e.Code != CodeFailed || e.AgentCode != "RUNTIME_IMAGE_MISSING" {
		t.Fatalf("failure = %v", err)
	}
}

func TestAcknowledgementWithoutResultIsNotAnError(t *testing.T) {
	a := newFakeAgent(t)
	a.body = `{"protocol":2,"accepted":true}`
	c := newClient(t, a)
	if err := c.StartRuntime(context.Background(), executor.StartRequest{
		Operation: executor.Operation{OperationID: "op_d_START_1", DeploymentID: "dep_1", ServerID: "srv_1"}, Container: "c"}); err != nil {
		t.Fatalf("an accepted operation with no result yet is not a failure: %v", err)
	}
}

func TestResponseValidationFailsClosed(t *testing.T) {
	cases := []struct {
		name, body string
		want       string
	}{
		{"foreign deployment", `{"protocol":2,"success":true,"deploymentId":"dep_other"}`, CodeMalformed},
		{"foreign operation", `{"protocol":2,"success":true,"operationId":"op_other"}`, CodeMalformed},
		{"other protocol", `{"protocol":99,"success":true}`, CodeMalformed},
		{"neither ack nor result", `{"protocol":2,"operationId":"x"}`, CodeMalformed},
		{"not json", `not json`, CodeMalformed},
		{"oversized", `{"protocol":2,"success":true,"message":"` + strings.Repeat("x", MaxResponseBytes) + `"}`, CodeTooLarge},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := newFakeAgent(t)
			a.body = c.body
			cl := newClient(t, a)
			err := cl.StartRuntime(context.Background(), executor.StartRequest{
				Operation: executor.Operation{OperationID: "op_d_START_1", DeploymentID: "dep_1", ServerID: "srv_1"}, Container: "c"})
			var e *Error
			if !errors.As(err, &e) || e.Code != c.want {
				t.Fatalf("err = %v, want code %s", err, c.want)
			}
		})
	}
}

func TestTransportFailuresAreTyped(t *testing.T) {
	a := newFakeAgent(t)
	c := newClient(t, a)
	a.srv.Close() // nothing is listening any more
	err := c.StartRuntime(context.Background(), executor.StartRequest{
		Operation: executor.Operation{OperationID: "op_d_START_1", DeploymentID: "dep_1", ServerID: "srv_1"}, Container: "c"})
	var e *Error
	if !errors.As(err, &e) || (e.Code != CodeUnreachable && e.Code != CodeTimeout) {
		t.Fatalf("err = %v, want a typed transport failure", err)
	}
}

// TestClientAppliesItsOwnDeadline proves the client bounds an exchange that
// would otherwise hang: with no deadline on the caller's context, the client
// applies DefaultTimeout itself.
func TestClientAppliesItsOwnDeadline(t *testing.T) {
	a := newFakeAgent(t)
	// The agent accepts the operation and never answers; the client's own
	// deadline must end the exchange.
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	a.handler = func(http.ResponseWriter, *http.Request) { <-release }
	c := newClient(t, a)
	c.DefaultTimeout = 50 * time.Millisecond
	start := time.Now()
	err := c.StartRuntime(context.Background(), executor.StartRequest{
		Operation: executor.Operation{OperationID: "op_d_START_1", DeploymentID: "dep_1", ServerID: "srv_1"}, Container: "c"})
	var e *Error
	if !errors.As(err, &e) || e.Code != CodeTimeout {
		t.Fatalf("err = %v, want %s", err, CodeTimeout)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("client exceeded its own bound: %s", d)
	}
}

func TestNoCredentialFailsClosed(t *testing.T) {
	a := newFakeAgent(t)
	c := newClient(t, a)
	c.Credentials = nil
	err := c.StartRuntime(context.Background(), executor.StartRequest{
		Operation: executor.Operation{OperationID: "op_d_START_1", DeploymentID: "dep_1", ServerID: "srv_1"}, Container: "c"})
	var e *Error
	if !errors.As(err, &e) || e.Code != CodeNoCredential || !errors.Is(err, ErrNoCredential) {
		t.Fatalf("err = %v, want %s", err, CodeNoCredential)
	}
	if ops, _ := a.received(); len(ops) != 0 {
		t.Fatal("no operation may be dispatched without a credential")
	}
}

func TestUnknownServerIsTyped(t *testing.T) {
	a := newFakeAgent(t)
	c := newClient(t, a)
	err := c.StartRuntime(context.Background(), executor.StartRequest{
		Operation: executor.Operation{OperationID: "op_d_START_1", DeploymentID: "dep_1", ServerID: "srv_other"}, Container: "c"})
	var e *Error
	if !errors.As(err, &e) || e.Code != CodeNotFound {
		t.Fatalf("err = %v, want %s", err, CodeNotFound)
	}
}

func TestSchemePolicyFailsClosed(t *testing.T) {
	cases := []struct {
		name, base string
		production bool
		wantErr    bool
	}{
		{"https anywhere", "https://agent.example", true, false},
		{"http loopback in dev", "http://127.0.0.1:9443", false, false},
		{"http loopback in production", "http://127.0.0.1:9443", true, true},
		{"http remote in dev", "http://203.0.113.10", false, true},
		{"no scheme", "agent.example:9443", false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &Client{Servers: fakeServers{rec: server.Record{ID: "srv_1", Address: "203.0.113.10"}},
				Credentials: staticCred{}, Production: tc.production, Log: discardLogger()}
			base, err := c.endpoint(context.Background(), "srv_1", "203.0.113.10")
			if tc.base != "" {
				c.Endpoint = func(context.Context, string, string) (string, error) { return tc.base, nil }
				base, err = c.endpoint(context.Background(), "srv_1", "203.0.113.10")
			}
			if tc.wantErr {
				var e *Error
				if !errors.As(err, &e) || e.Code != CodeInsecure {
					t.Fatalf("err = %v, want %s", err, CodeInsecure)
				}
				return
			}
			if err != nil {
				t.Fatalf("err = %v, want success", err)
			}
			if base == "" {
				t.Fatal("endpoint must resolve")
			}
		})
	}
}

// TestDefaultEndpointUsesServerAddress proves the default resolution: the
// server domain stores an address without a scheme, and the agent endpoint is
// always https.
func TestDefaultEndpointUsesServerAddress(t *testing.T) {
	c := &Client{Servers: fakeServers{rec: server.Record{ID: "srv_1", Address: "203.0.113.10:8443"}},
		Credentials: staticCred{}, Production: true, Log: discardLogger()}
	base, err := c.endpoint(context.Background(), "srv_1", "203.0.113.10:8443")
	if err != nil || base != "https://203.0.113.10:8443" {
		t.Fatalf("endpoint = %q, %v", base, err)
	}
}

func TestInvalidRequestsNeverReachTheAgent(t *testing.T) {
	a := newFakeAgent(t)
	c := newClient(t, a)
	bad := []func(context.Context) error{
		func(ctx context.Context) error {
			return c.CreateRuntime(ctx, executor.CreateRuntimeRequest{
				Operation: executor.Operation{OperationID: "op_d_CREATE_RUNTIME_1", DeploymentID: "dep_1", ServerID: "srv_1"}, Container: "c"})
		},
		func(ctx context.Context) error {
			return c.CreateRuntime(ctx, executor.CreateRuntimeRequest{
				Operation: executor.Operation{OperationID: "op_d_CREATE_RUNTIME_1", DeploymentID: "dep_1", ServerID: "srv_1"},
				ImageRef:  "img", Container: "c", Port: 70000})
		},
		func(ctx context.Context) error {
			return c.ConfigureNetwork(ctx, executor.NetworkRequest{
				Operation: executor.Operation{OperationID: "op_d_NETWORK_1", DeploymentID: "dep_1", ServerID: "srv_1"}, Container: "c"})
		},
		func(ctx context.Context) error {
			return c.StartRuntime(ctx, executor.StartRequest{
				Operation: executor.Operation{DeploymentID: "dep_1", ServerID: "srv_1"}, Container: "c"})
		},
		func(ctx context.Context) error {
			_, err := c.HealthCheck(ctx, executor.HealthCheckRequest{
				Operation: executor.Operation{OperationID: "op_d_VERIFY_1", DeploymentID: "dep_1", ServerID: "srv_1"}, Domain: "app.acme.dev"})
			return err
		},
	}
	for i, fn := range bad {
		if err := fn(context.Background()); err == nil {
			t.Fatalf("case %d: invalid operation must be refused locally", i)
		}
	}
	if ops, _ := a.received(); len(ops) != 0 {
		t.Fatalf("%d invalid operations reached the agent", len(ops))
	}
}

func TestSatisfiesRuntimeAgentInterface(t *testing.T) {
	var _ executor.RuntimeAgent = (*Client)(nil)
}

// TestOperationCarriesTheApplicationScope proves the Engine resolves the
// application from the deployment and puts it on the wire, distinctly from the
// deployment (#145).
func TestOperationCarriesTheApplicationScope(t *testing.T) {
	a := newFakeAgent(t)
	c := newClient(t, a)
	c.Applications = fakeApplications{rec: deployment.Record{
		ID: "dep_0123456789abcdef01234567", ApplicationID: "app_ffffffffffffffffffffffff",
	}}
	if err := c.StartRuntime(context.Background(), executor.StartRequest{
		Operation: op(), Container: "axiom-acme-web-0123",
	}); err != nil {
		t.Fatalf("StartRuntime: %v", err)
	}
	ops, _ := a.received()
	if len(ops) != 1 {
		t.Fatalf("agent received %d operations, want 1", len(ops))
	}
	if got := ops[0]["applicationId"]; got != "app_ffffffffffffffffffffffff" {
		t.Fatalf("applicationId = %v, want the deployment's application", got)
	}
	if got := ops[0]["deploymentId"]; got != "dep_0123456789abcdef01234567" {
		t.Fatalf("deploymentId = %v", got)
	}
}

// TestMissingApplicationRefusesTheOperation is the Engine-side fail-closed
// half of #145: without an application the operation is never put on the wire.
func TestMissingApplicationRefusesTheOperation(t *testing.T) {
	cases := map[string]*Client{}
	for name, apps := range map[string]Applications{
		"no lookup":          nil,
		"unknown deployment": fakeApplications{err: deployment.ErrNotFound},
		"no application":     fakeApplications{rec: deployment.Record{ID: "dep_0123456789abcdef01234567"}},
	} {
		a := newFakeAgent(t)
		c := newClient(t, a)
		c.Applications = apps
		cases[name] = c
	}
	for name, c := range cases {
		err := c.CreateRuntime(context.Background(), executor.CreateRuntimeRequest{
			Operation: op(), ImageRef: "axiom-local/acme-web:e8e8e8e-1a2b3c4d", Container: "axiom-acme-web-0123", Port: 3000,
		})
		var e *Error
		if !errors.As(err, &e) || e.Code != CodeNoApplication {
			t.Fatalf("%s: err = %v, want %s", name, err, CodeNoApplication)
		}
	}
}
