package bootstrap

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitaleflex/axiom/services/agent/internal/config"
	"github.com/digitaleflex/axiom/services/agent/internal/protocol"
)

// testConfig returns a valid agent configuration rooted in a temp dir, with a
// fake Engine for the heartbeat transport. It never binds a fixed port.
func testConfig(t *testing.T, engineURL string) config.Config {
	t.Helper()
	dir := t.TempDir()
	cfg, err := config.LoadFrom(func(k string) (string, bool) {
		switch k {
		case "AXIOM_ENV":
			return "test", true
		case "AXIOM_SERVER_ID":
			return "srv_test", true
		case "AXIOM_ENGINE_URL":
			return engineURL, true
		case "AXIOM_AGENT_TOKEN":
			return "ac_bootstrap_test", true
		case "AXIOM_AGENT_DATA_DIR":
			return dir, true
		case "AXIOM_TRAEFIK_DYNAMIC_DIR":
			return filepath.Join(dir, "traefik"), true
		case "AXIOM_AGENT_LISTEN_ADDR":
			return "127.0.0.1:0", true
		case "AXIOM_SHUTDOWN_TIMEOUT":
			return "2s", true
		}
		return "", false
	})
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	return cfg
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// fakeEngine answers registration and heartbeat with the real protocol shapes.
type fakeEngine struct {
	*httptest.Server
	agentID  string
	token    string
	interval int
	// operationSigningKey is the ADR-0008 key handed at registration; empty
	// means the Engine issues none (the agent then stays closed).
	operationSigningKey string

	mu         sync.Mutex
	registered []protocol.RegistrationRequest
	beats      []protocol.Heartbeat
}

func newFakeEngine(t *testing.T) *fakeEngine {
	e := &fakeEngine{
		agentID:  "agent_0123456789abcdef01234567",
		token:    "ac_" + strings.Repeat("c", 64),
		interval: 1,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/agent/register", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			http.Error(w, "no credential", http.StatusUnauthorized)
			return
		}
		var in protocol.RegistrationRequest
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		e.mu.Lock()
		e.registered = append(e.registered, in)
		e.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"agentId": e.agentID, "serverId": in.ServerID,
			"credential": e.token, "credentialVersion": 1,
			"credentialExpiresAt": time.Now().Add(time.Hour).UTC(),
			"operationSigningKey": e.operationSigningKey,
			"negotiated":          protocol.Version, "heartbeatIntervalSeconds": e.interval,
		})
	})
	mux.HandleFunc("/api/v1/agent/heartbeat", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" || r.Header.Get("X-Agent-ID") == "" {
			http.Error(w, "no credential", http.StatusUnauthorized)
			return
		}
		var in protocol.Heartbeat
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		e.mu.Lock()
		e.beats = append(e.beats, in)
		e.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "serverStatus": "READY"})
	})
	e.Server = httptest.NewServer(mux)
	t.Cleanup(e.Close)
	return e
}

func (e *fakeEngine) registrationCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.registered)
}

func (e *fakeEngine) heartbeatCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.beats)
}

// TestNewMountsEveryPackage is the wiring assertion: a test that only checks
// New() != nil proves nothing. Each field named in the mount order must be a
// real, constructed dependency.
func TestNewMountsEveryPackage(t *testing.T) {
	engine := newFakeEngine(t)
	cfg := testConfig(t, engine.URL)
	app, err := New(context.Background(), cfg, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()

	checks := []struct {
		name string
		ok   bool
	}{
		{"identity", app.Identity != nil},
		{"credentials", app.Credentials != nil},
		{"auth client", app.Auth != nil},
		{"state", app.State != nil},
		{"capabilities discoverer", app.Discoverer.Runner != nil},
		{"recovery reconciler", app.Reconciler != nil && app.Reconciler.Runtime != nil && app.Reconciler.State != nil},
		{"health checker", app.Checker != nil},
		{"docker adapter", app.Docker != nil && app.Docker.Runner != nil},
		{"traefik adapter", app.Traefik != nil && app.Traefik.Verify != nil && app.Traefik.DynamicDir != ""},
		{"runtime bridge", app.Adapter != nil && app.Adapter.docker != nil && app.Adapter.traefik != nil && app.Adapter.health != nil},
		{"dispatcher", app.Dispatcher != nil},
		{"dispatcher adapters", len(app.Dispatcher.Adapters) == 1},
		{"logs fetcher", app.Logs != nil && app.Logs.Docker != nil},
		{"listener", app.Listener != nil},
	}
	for _, c := range checks {
		if !c.ok {
			t.Errorf("%s is not mounted", c.name)
		}
	}
	// The listener's authenticator must be the fail-closed one (#77/ADR-0008).
	if err := app.Listener.auth.Authenticate(&http.Request{}); err == nil {
		t.Error("the inbound listener must refuse unauthenticated operations")
	}
}

// TestNewRejectsInvalidConfig proves the composition root fails fast.
func TestNewRejectsInvalidConfig(t *testing.T) {
	// Load already refuses an empty environment, and New refuses whatever
	// reaches it unvalidated: both layers fail fast.
	if _, err := config.LoadFrom(func(string) (string, bool) { return "", false }); err == nil {
		t.Fatal("expected config.LoadFrom to reject an empty environment")
	}
	if _, err := New(context.Background(), config.Config{}, discardLogger()); err == nil {
		t.Fatal("expected an invalid configuration error")
	}
}

// TestServeDispatchesOperationsAndDrains runs the real listener: an
// unauthenticated operation is refused, an authenticated one reaches the real
// dispatcher and comes back as a stable rejection, and shutdown drains the
// heartbeat loop.
func TestServeDispatchesOperationsAndDrains(t *testing.T) {
	engine := newFakeEngine(t)
	cfg := testConfig(t, engine.URL)
	app, err := New(context.Background(), cfg, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	// Inject a permissive authenticator to prove the dispatcher path is live;
	// production keeps refuseInbound (asserted in TestNewMountsEveryPackage).
	app.Listener.auth = allowInbound{}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- app.Serve(ctx, ln) }()

	base := "http://" + ln.Addr().String()
	op := validOperation()

	// Unauthenticated operations are refused with 401 UNAUTHORIZED.
	app.Listener.auth = refuseInbound{}
	resp, err := http.Post(base+cfg.Listener.Path, "application/json", bytes.NewReader(op))
	if err != nil {
		t.Fatal(err)
	}
	unauthBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated operation = %d, want 401", resp.StatusCode)
	}
	if !bytes.Contains(unauthBody, []byte(protocol.CodeUnauthorized)) {
		t.Fatalf("401 body carries no UNAUTHORIZED code: %s", unauthBody)
	}

	// Authenticated operations reach the real dispatcher. Docker is absent on
	// the test host, so the CREATE_RUNTIME fails with a stable runtime code —
	// which proves the operation travelled through the dispatcher and the
	// Docker adapter rather than being short-circuited.
	app.Listener.auth = allowInbound{}
	resp, err = http.Post(base+cfg.Listener.Path, "application/json", bytes.NewReader(op))
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		OperationID  string `json:"operationId"`
		DeploymentID string `json:"deploymentId"`
		Success      bool   `json:"success"`
		ErrorCode    string `json:"errorCode"`
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("authenticated operation = %d, want 200", resp.StatusCode)
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode result: %v (%s)", err, body)
	}
	if out.OperationID != testOperationID || out.DeploymentID != testDeploymentID {
		t.Fatalf("result names another operation: %s", body)
	}
	if out.Success {
		t.Fatal("CREATE_RUNTIME cannot succeed without a container runtime")
	}
	if out.ErrorCode == "" {
		t.Fatalf("rejected operation carries no stable code: %s", body)
	}

	// Unknown paths are not served at all.
	notFound, err := http.Get(base + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	notFound.Body.Close()
	if notFound.StatusCode != http.StatusNotFound {
		t.Fatalf("/metrics = %d, want 404", notFound.StatusCode)
	}

	// The agent registered and the heartbeat loop is really running.
	waitFor(t, 3*time.Second, func() bool { return engine.registrationCount() == 1 })
	waitFor(t, 3*time.Second, func() bool { return engine.heartbeatCount() >= 1 })

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("agent did not stop")
	}

	// Drained: the heartbeat loop is stopped and the state store is closed.
	app.mu.Lock()
	cancelLoops := app.cancelLoops
	app.mu.Unlock()
	if cancelLoops == nil {
		t.Fatal("shutdown did not cancel the background loops")
	}
	drained := make(chan struct{})
	go func() { app.wg.Wait(); close(drained) }()
	select {
	case <-drained:
	case <-time.After(2 * time.Second):
		t.Fatal("background loops did not drain")
	}
}

// TestListenerRejectsMalformedOperations proves the strict decode.
func TestListenerRejectsMalformedOperations(t *testing.T) {
	engine := newFakeEngine(t)
	app, err := New(context.Background(), testConfig(t, engine.URL), discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	app.Listener.auth = allowInbound{}
	app.Listener.log = discardLogger()

	for name, body := range map[string]string{
		"unknown field": `{"protocol":1,"messageId":"m","sentAt":"2026-01-02T03:04:05Z","operationId":"op_dep_0123456789abcdef01234567_START_1","type":"START","deploymentId":"dep_0123456789abcdef01234567","serverId":"srv_test","payload":{"container":"axiom-app-1"},"shell":"rm -rf /"}`,
		"trailing junk": `{"protocol":1,"messageId":"m","sentAt":"2026-01-02T03:04:05Z","operationId":"op_dep_0123456789abcdef01234567_START_1","type":"START","deploymentId":"dep_0123456789abcdef01234567","serverId":"srv_test","payload":{"container":"axiom-app-1"}}{}`,
	} {
		req := httptest.NewRequest(http.MethodPost, app.cfg.Listener.Path, strings.NewReader(body))
		rr := httptest.NewRecorder()
		app.Listener.ServeHTTP(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("%s = %d, want 400", name, rr.Code)
		}
	}

	req := httptest.NewRequest(http.MethodGet, app.cfg.Listener.Path, nil)
	rr := httptest.NewRecorder()
	app.Listener.ServeHTTP(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET on the operation path = %d, want 405", rr.Code)
	}
}

// TestRegisteredIdentityStartsHeartbeatAtStartup proves the heartbeat is wired
// from a persisted registration, not only from a fresh registration.
func TestRegisteredIdentityStartsHeartbeatAtStartup(t *testing.T) {
	engine := newFakeEngine(t)
	cfg := testConfig(t, engine.URL)

	first, err := New(context.Background(), cfg, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	if err := first.register(context.Background()); err != nil {
		t.Fatal(err)
	}
	first.Close()

	second, err := New(context.Background(), cfg, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if second.Heartbeat == nil {
		t.Fatal("a registered agent must have a heartbeat loop")
	}
	if second.Heartbeat.Identity.AgentID != engine.agentID {
		t.Fatalf("heartbeat identity = %q, want %q", second.Heartbeat.Identity.AgentID, engine.agentID)
	}
	// Registration is not repeated for an already-registered agent.
	if err := second.register(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := engine.registrationCount(); n != 1 {
		t.Fatalf("registration count = %d, want 1", n)
	}
}

// TestDispatchRecordsDurableState proves the operation reaches the durable
// state store and that a terminal operation is never re-executed.
func TestDispatchRecordsDurableState(t *testing.T) {
	engine := newFakeEngine(t)
	app, err := New(context.Background(), testConfig(t, engine.URL), discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()

	var op protocol.Operation
	if err := json.Unmarshal(validOperation(), &op); err != nil {
		t.Fatal(err)
	}
	_, first := app.Dispatch(context.Background(), op)
	if first.ErrorCode == "" {
		t.Fatalf("expected a stable runtime failure code, got %s", first.ErrorCode)
	}
	entry, ok := app.State.Get(op.OperationID)
	if !ok {
		t.Fatal("the operation was not recorded in the durable state store")
	}
	if !entry.Phase.Terminal() {
		t.Fatalf("recorded phase = %q, want a terminal phase", entry.Phase)
	}

	// Redelivery of a terminal operation is refused by the state store.
	_, second := app.Dispatch(context.Background(), op)
	if second.ErrorCode != "REPLAYED" {
		t.Fatalf("redelivered operation = %q, want REPLAYED", second.ErrorCode)
	}

	// The record survives a restart as a terminal entry.
	app.Close()
	restarted, err := New(context.Background(), app.cfg, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	after, ok := restarted.State.Get(op.OperationID)
	if !ok || after.Phase != entry.Phase {
		t.Fatalf("recovered entry = %+v, want %q", after, entry.Phase)
	}
}

// allowInbound is a test authenticator that accepts everything. It exists only
// to prove the listener→dispatcher path; production uses refuseInbound (no
// key) or signedInbound (key registered).
type allowInbound struct{}

func (allowInbound) Authenticate(*http.Request) error { return nil }

func (allowInbound) VerifyOperation(*http.Request, protocol.Operation, []byte) error { return nil }

const (
	testOperationID   = "op_dep_0123456789abcdef01234567_CREATE_RUNTIME_1"
	testDeploymentID  = "dep_0123456789abcdef01234567"
	testApplicationID = "app_0123456789abcdef01234567"
)

// validOperation returns a well-formed CREATE_RUNTIME operation bound to
// srv_test.
func validOperation() []byte {
	op := protocol.Operation{
		Envelope: protocol.Envelope{
			Protocol: protocol.Version, MessageID: "msg_op_0001",
			SentAt: time.Now().UTC(),
		},
		OperationID:   testOperationID,
		Type:          protocol.OpCreateRuntime,
		DeploymentID:  testDeploymentID,
		ApplicationID: testApplicationID,
		ServerID:      "srv_test",
		Payload: protocol.Payload{
			ImageRef:  "sha256:" + strings.Repeat("a", 64),
			Container: "axiom-app-1",
			Port:      3000,
		},
	}
	raw, _ := json.Marshal(op)
	return raw
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met before the deadline")
}
