// Package tests is the Wave-4 integration harness for the Runtime Agent
// (issue #90). It wires the real internal packages together and fakes only the
// OS/network boundary:
//
//   - a scripted Docker Runner that simulates a container engine (argv→output);
//   - an httptest "Engine" that records every signed request;
//   - temp dirs for identity, credential, state and Traefik dynamic config.
//
// The tests are deterministic, race-clean and need no external services.
package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitaleflex/axiom/services/agent/internal/dispatcher"
	"github.com/digitaleflex/axiom/services/agent/internal/health"
	"github.com/digitaleflex/axiom/services/agent/internal/identity"
	"github.com/digitaleflex/axiom/services/agent/internal/protocol"
	"github.com/digitaleflex/axiom/services/agent/internal/runtime/docker"
	"github.com/digitaleflex/axiom/services/agent/internal/runtime/traefik"
	"github.com/digitaleflex/axiom/services/agent/internal/security/auth"
	"github.com/digitaleflex/axiom/services/agent/internal/security/ownership"
)

// Fixed fixtures shared by every scenario.
const (
	testDeployment = "dep_0123456789abcdef01234567"
	testServer     = "srv_test"
	testApp        = "app"
	testContainer  = "axiom-app-1"
	testAgentID    = "agent_0123456789abcdef01234567"

	// Harness-level stable codes for adapter failures that have no code in the
	// production adapter packages (see the failure matrix "gaps" section).
	codeHealthFailed  = "HEALTH_CHECK_FAILED"
	codeNetworkFailed = "NETWORK_CONFIG_FAILED"
)

// fixedNow is the deterministic clock shared by the dispatcher and operations.
var fixedNow = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

var testIdentity = protocol.AgentIdentity{AgentID: testAgentID, ServerID: testServer}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// ---------------------------------------------------------------------------
// Docker simulator (the OS/process boundary)
// ---------------------------------------------------------------------------

// simContainer is one simulated container.
type simContainer struct {
	name    string
	image   string
	imageID string
	running bool
	labels  map[string]string
	logs    string
}

func (c *simContainer) inspectJSON() string {
	labels := c.labels
	if labels == nil {
		labels = map[string]string{}
	}
	status := "exited"
	if c.running {
		status = "running"
	}
	doc := []map[string]any{{
		"Id":    c.imageID,
		"Name":  "/" + c.name,
		"Image": c.imageID,
		"Config": map[string]any{
			"Image":  c.image,
			"Labels": labels,
		},
		"State":           map[string]any{"Status": status, "Running": c.running},
		"NetworkSettings": map[string]any{"Ports": map[string]any{}},
	}}
	b, _ := json.Marshal(doc)
	return string(b)
}

// simDocker is a scripted, in-memory Docker CLI. It records every argv and
// simulates image presence, container lifecycle, labels and log output.
type simDocker struct {
	mu         sync.Mutex
	containers map[string]*simContainer
	images     map[string]bool
	calls      [][]string

	// failPull makes `docker pull` exit non-zero (image missing).
	failPull bool
	// failStart names containers whose `docker start` must fail.
	failStart map[string]bool
}

func newSimDocker() *simDocker {
	return &simDocker{
		containers: map[string]*simContainer{},
		images:     map[string]bool{},
		failStart:  map[string]bool{},
	}
}

// seed inserts a pre-existing container with the given labels.
func (s *simDocker) seed(name string, labels map[string]string, running bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.containers[name] = &simContainer{
		name: name, image: "busybox:latest", imageID: "sha256:" + strings.Repeat("b", 64),
		running: running, labels: labels,
	}
}

// Run implements docker.Runner.
func (s *simDocker) Run(_ context.Context, argv ...string) (string, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, append([]string(nil), argv...))
	if len(argv) == 0 {
		return "", 1, errors.New("sim: empty argv")
	}
	switch argv[0] {
	case "image":
		ref := argv[len(argv)-1]
		if s.images[ref] {
			return "[]", 0, nil
		}
		return "Error: No such image\n", 1, errors.New("exit status 1")
	case "pull":
		if s.failPull {
			return "Error: pull access denied\n", 1, errors.New("exit status 1")
		}
		s.images[argv[1]] = true
		return "", 0, nil
	case "inspect":
		c, ok := s.containers[argv[1]]
		if !ok {
			return "Error: No such container\n", 1, errors.New("exit status 1")
		}
		return c.inspectJSON(), 0, nil
	case "create":
		return s.handleCreate(argv)
	case "start":
		c, ok := s.containers[argv[1]]
		if !ok {
			return "Error: No such container\n", 1, errors.New("exit status 1")
		}
		if s.failStart[argv[1]] {
			return "Error: cannot start container\n", 1, errors.New("exit status 1")
		}
		c.running = true
		return argv[1] + "\n", 0, nil
	case "stop":
		c, ok := s.containers[argv[1]]
		if !ok {
			return "Error: No such container\n", 1, errors.New("exit status 1")
		}
		c.running = false
		return argv[1] + "\n", 0, nil
	case "rm":
		delete(s.containers, argv[len(argv)-1])
		return argv[len(argv)-1] + "\n", 0, nil
	case "logs":
		c, ok := s.containers[argv[len(argv)-1]]
		if !ok {
			return "Error: No such container\n", 1, errors.New("exit status 1")
		}
		return c.logs, 0, nil
	case "ps":
		return s.handlePS(argv), 0, nil
	}
	return "", 0, nil
}

func (s *simDocker) handleCreate(argv []string) (string, int, error) {
	c := &simContainer{labels: map[string]string{}, imageID: "sha256:" + strings.Repeat("a", 64)}
	for i := 1; i < len(argv); i++ {
		switch argv[i] {
		case "--name":
			i++
			c.name = argv[i]
		case "-p", "--memory", "--cpus", "-e":
			i++ // value consumed, not simulated
		case "--label":
			i++
			kv := strings.SplitN(argv[i], "=", 2)
			if len(kv) == 2 {
				c.labels[kv[0]] = kv[1]
			}
		default:
			if c.image == "" {
				c.image = argv[i]
			}
		}
	}
	if c.name == "" {
		return "Error: name required\n", 1, errors.New("exit status 1")
	}
	s.containers[c.name] = c
	return "container-id-" + c.name + "\n", 0, nil
}

// handlePS applies the `label=k=v` filters and returns the matching names.
func (s *simDocker) handlePS(argv []string) string {
	var filters [][2]string
	for i := 0; i < len(argv); i++ {
		if argv[i] == "--filter" && i+1 < len(argv) {
			i++
			if kv := strings.SplitN(strings.TrimPrefix(argv[i], "label="), "=", 2); len(kv) == 2 {
				filters = append(filters, [2]string{kv[0], kv[1]})
			}
		}
	}
	var names []string
	for name, c := range s.containers {
		match := true
		for _, f := range filters {
			if c.labels[f[0]] != f[1] {
				match = false
				break
			}
		}
		if match {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return strings.Join(names, "\n")
}

// callsWith returns the recorded argv calls whose first token equals sub.
func (s *simDocker) callsWith(sub string) [][]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out [][]string
	for _, c := range s.calls {
		if len(c) > 0 && c[0] == sub {
			out = append(out, c)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Bridge: dispatcher.Adapter over the real runtime adapters
// ---------------------------------------------------------------------------

// codedError implements dispatcher.ErrorCoder so adapter failures surface a
// stable machine code through the real dispatcher.
type codedError struct {
	code string
	err  error
}

func (e *codedError) Error() string     { return e.code + ": " + e.err.Error() }
func (e *codedError) ErrorCode() string { return e.code }
func (e *codedError) Unwrap() error     { return e.err }

// bridge is the integration seam that the production wiring will eventually
// provide: it adapts the real docker/traefik/health packages to
// dispatcher.Adapter and translates adapter errors into stable codes.
type bridge struct {
	docker  *docker.Adapter
	traefik *traefik.Adapter
	health  *health.Checker

	DeploymentID  string
	ApplicationID string
	ServerID      string
	// HealthURL overrides the VERIFY target (tests point it at an httptest
	// server). Empty falls back to http://<domain><path>.
	HealthURL string
}

func (b *bridge) CreateRuntime(ctx context.Context, p dispatcher.CreateParams) error {
	_, err := b.docker.Create(ctx, docker.CreateSpec{
		DeploymentID:  b.DeploymentID,
		ApplicationID: b.ApplicationID,
		ServerID:      b.ServerID,
		Container:     p.Container,
		ImageRef:      p.ImageRef,
		Port:          p.Port,
	})
	return b.mapDocker(err)
}

func (b *bridge) ConfigureNetwork(ctx context.Context, p dispatcher.NetworkParams) error {
	err := b.traefik.Configure(ctx, traefik.Request{
		Container:    p.Container,
		Domain:       p.Domain,
		Port:         p.Port,
		TLS:          p.TLS,
		DeploymentID: b.DeploymentID,
		ServerID:     b.ServerID,
	})
	return b.mapTraefik(err)
}

func (b *bridge) StartRuntime(ctx context.Context, p dispatcher.StartParams) error {
	return b.mapDocker(b.docker.Start(ctx, p.Container))
}

func (b *bridge) StopRuntime(ctx context.Context, p dispatcher.StopParams) error {
	return b.mapDocker(b.docker.Stop(ctx, p.Container))
}

func (b *bridge) RemoveRuntime(ctx context.Context, p dispatcher.RemoveParams) error {
	return b.mapDocker(b.docker.Remove(ctx, p.Container))
}

func (b *bridge) HealthCheck(ctx context.Context, p dispatcher.VerifyParams) (dispatcher.HealthReport, error) {
	target := b.HealthURL
	if target == "" {
		path := p.Path
		if path == "" {
			path = "/"
		}
		target = "http://" + p.Domain + path
	}
	rep, err := b.health.Check(ctx, health.Spec{
		URL:            target,
		Timeout:        time.Duration(p.TimeoutSeconds) * time.Second,
		Retries:        0,
		ExpectedStatus: "200-399",
	})
	if err != nil {
		return dispatcher.HealthReport{}, &codedError{code: codeHealthFailed, err: err}
	}
	return dispatcher.HealthReport{
		StatusCode: rep.StatusCode,
		LatencyMs:  rep.LatencyMs,
		Attempt:    rep.Attempt,
	}, nil
}

// mapDocker surfaces the Docker adapter's stable code verbatim.
func (b *bridge) mapDocker(err error) error {
	if err == nil {
		return nil
	}
	var de *docker.Error
	if errors.As(err, &de) && de.Code != "" {
		return &codedError{code: de.Code, err: err}
	}
	return &codedError{code: dispatcher.CodeInternal, err: err}
}

// mapTraefik surfaces a stable code for the Traefik adapter's typed errors.
func (b *bridge) mapTraefik(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ownership.ErrNotManaged) {
		return &codedError{code: docker.CodeNotManaged, err: err}
	}
	return &codedError{code: codeNetworkFailed, err: err}
}

// ---------------------------------------------------------------------------
// Harness
// ---------------------------------------------------------------------------

type harness struct {
	t      *testing.T
	dir    string
	sim    *simDocker
	docker *docker.Adapter
	trf    *traefik.Adapter
	check  *health.Checker
	br     *bridge
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	dir := t.TempDir()
	sim := newSimDocker()
	d := &docker.Adapter{
		Runner: sim,
		Now:    func() time.Time { return fixedNow },
	}
	trf := &traefik.Adapter{DynamicDir: filepath.Join(dir, "dynamic")}
	trf.Verify = traefik.ContainerVerifierFunc(func(ctx context.Context, container, dep string) error {
		info, err := d.Inspect(ctx, container)
		if err != nil {
			return err
		}
		return ownership.AssertContainer(container, info.Labels, dep)
	})
	check := &health.Checker{
		Sleep: func(ctx context.Context, _ time.Duration) error { return ctx.Err() },
	}
	br := &bridge{
		docker:        d,
		traefik:       trf,
		health:        check,
		DeploymentID:  testDeployment,
		ApplicationID: testApp,
		ServerID:      testServer,
	}
	return &harness{t: t, dir: dir, sim: sim, docker: d, trf: trf, check: check, br: br}
}

func (h *harness) dispatcher() *dispatcher.Dispatcher {
	return dispatcher.NewDispatcher(
		[]dispatcher.Adapter{h.br},
		discardLogger(),
		dispatcher.WithClock(func() time.Time { return fixedNow }),
		dispatcher.WithIDGen(func() string { return "msg_test_0001" }),
	)
}

// newOperation builds a valid, fresh operation bound to serverID.
func newOperation(opID, opType string, payload protocol.Payload, serverID string) protocol.Operation {
	return protocol.Operation{
		Envelope: protocol.Envelope{
			Protocol:  protocol.Version,
			MessageID: "msg_op_0001",
			SentAt:    fixedNow,
		},
		OperationID:  opID,
		Type:         opType,
		DeploymentID: testDeployment,
		ServerID:     serverID,
		Payload:      payload,
	}
}

// opID builds a canonical operation ID for the test deployment.
func opID(step string, attempt int) string {
	return fmt.Sprintf("op_%s_%s_%d", testDeployment, step, attempt)
}

// ---------------------------------------------------------------------------
// Fake Engine (the network boundary)
// ---------------------------------------------------------------------------

type recordedRequest struct {
	method string
	path   string
	header http.Header
	body   []byte
}

// fakeEngine is an httptest Engine that records every signed request and can
// script registration and heartbeat responses.
type fakeEngine struct {
	t      *testing.T
	server *httptest.Server

	mu       sync.Mutex
	requests []recordedRequest

	agentID    string
	credential string
	serverID   string

	// failAfter is the number of heartbeat requests answered 200 before a 401;
	// negative means never fail.
	failAfter  int
	heartbeats int
}

func newFakeEngine(t *testing.T, serverID string) *fakeEngine {
	t.Helper()
	e := &fakeEngine{
		t:          t,
		agentID:    testAgentID,
		credential: "ac_" + strings.Repeat("c", 64),
		serverID:   serverID,
		failAfter:  -1,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/agent/register", e.handleRegister)
	mux.HandleFunc("/api/v1/agent/heartbeat", e.handleHeartbeat)
	e.server = httptest.NewServer(mux)
	t.Cleanup(e.server.Close)
	return e
}

func (e *fakeEngine) URL() string { return e.server.URL }

func (e *fakeEngine) record(r *http.Request) {
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	r.Body = io.NopCloser(bytes.NewReader(body))
	e.mu.Lock()
	defer e.mu.Unlock()
	e.requests = append(e.requests, recordedRequest{
		method: r.Method,
		path:   r.URL.Path,
		header: r.Header.Clone(),
		body:   body,
	})
}

func (e *fakeEngine) requestFor(path string) (recordedRequest, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, req := range e.requests {
		if req.path == path {
			return req, true
		}
	}
	return recordedRequest{}, false
}

func (e *fakeEngine) count(path string) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	n := 0
	for _, req := range e.requests {
		if req.path == path {
			n++
		}
	}
	return n
}

func (e *fakeEngine) handleRegister(w http.ResponseWriter, r *http.Request) {
	e.record(r)
	// The bootstrap credential rides on the transport (Authorization), never
	// in the body.
	if _, ok := bearer(r); !ok {
		http.Error(w, "missing credential", http.StatusUnauthorized)
		return
	}
	var in struct {
		ServerID     string   `json:"serverId"`
		AgentVersion string   `json:"agentVersion"`
		Capabilities []string `json:"capabilities"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if in.ServerID != e.serverID || in.AgentVersion == "" || len(in.Capabilities) == 0 {
		http.Error(w, "invalid registration", http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"agentId":                  e.agentID,
		"serverId":                 e.serverID,
		"credential":               e.credential,
		"credentialVersion":        1,
		"credentialExpiresAt":      time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339),
		"negotiated":               protocol.Version,
		"heartbeatIntervalSeconds": 30,
	})
}

func (e *fakeEngine) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	e.record(r)

	e.mu.Lock()
	n := e.heartbeats
	e.heartbeats++
	fail := e.failAfter >= 0 && n >= e.failAfter
	e.mu.Unlock()

	if _, ok := bearer(r); !ok || r.Header.Get("X-Agent-ID") == "" {
		http.Error(w, "missing credential", http.StatusUnauthorized)
		return
	}
	if fail {
		http.Error(w, "credential revoked", http.StatusUnauthorized)
		return
	}
	var hb protocol.Heartbeat
	if err := json.NewDecoder(r.Body).Decode(&hb); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if err := hb.Validate(time.Now().UTC()); err != nil {
		http.Error(w, "invalid heartbeat: "+err.Error(), http.StatusBadRequest)
		return
	}
	if hb.AgentID != e.agentID || hb.ServerID != e.serverID {
		http.Error(w, "identity mismatch", http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "serverStatus": "READY"})
}

func bearer(r *http.Request) (string, bool) {
	const prefix = "Bearer "
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, prefix) {
		return "", false
	}
	token := strings.TrimSpace(strings.TrimPrefix(h, prefix))
	return token, token != ""
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// ---------------------------------------------------------------------------
// Registration helper (models the agent side of the register exchange)
// ---------------------------------------------------------------------------

type registerResponse struct {
	AgentID                  string    `json:"agentId"`
	ServerID                 string    `json:"serverId"`
	Credential               string    `json:"credential"`
	CredentialVersion        int       `json:"credentialVersion"`
	CredentialExpiresAt      time.Time `json:"credentialExpiresAt"`
	Negotiated               int       `json:"negotiated"`
	HeartbeatIntervalSeconds int       `json:"heartbeatIntervalSeconds"`
}

// registerAgent runs the full registration exchange using the real identity,
// auth and protocol packages against the fake Engine, persisting the issued
// identity and credential.
func registerAgent(t *testing.T, engine *fakeEngine, idStore *identity.Store, credStore *auth.Store, caps []string) (protocol.AgentIdentity, auth.Credential) {
	t.Helper()
	local, err := idStore.Load()
	if err != nil {
		t.Fatalf("identity load: %v", err)
	}
	bootstrap := auth.Credential{
		Token:     "ac_bootstrap_test",
		Version:   1,
		ExpiresAt: time.Now().Add(time.Hour),
		AgentID:   local.AgentID,
		ServerID:  testServer,
	}
	if err := credStore.Save(bootstrap); err != nil {
		t.Fatalf("save bootstrap: %v", err)
	}

	client := auth.NewClient(engine.URL(), credStore)
	req := protocol.RegistrationRequest{
		Envelope: protocol.Envelope{
			Protocol:  protocol.Version,
			MessageID: "msg_register_0001",
			SentAt:    time.Now().UTC(),
		},
		AgentIdentity: protocol.AgentIdentity{AgentID: local.AgentID, ServerID: testServer},
		AgentVersion:  "0.1.0",
		Capabilities:  caps,
	}
	if err := req.Validate(time.Now().UTC()); err != nil {
		t.Fatalf("registration request invalid: %v", err)
	}
	body, _ := json.Marshal(req)
	httpReq, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		engine.URL()+"/api/v1/agent/register", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("register request: %v", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(httpReq)
	if err != nil {
		t.Fatalf("register send: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("register status = %d, want 201", resp.StatusCode)
	}
	var out registerResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("register decode: %v", err)
	}
	issued := auth.Credential{
		Token:     out.Credential,
		Version:   out.CredentialVersion,
		ExpiresAt: out.CredentialExpiresAt,
		AgentID:   out.AgentID,
		ServerID:  out.ServerID,
	}
	if err := credStore.Save(issued); err != nil {
		t.Fatalf("save issued credential: %v", err)
	}
	if err := idStore.Save(identity.Identity{AgentID: out.AgentID, ServerID: out.ServerID, Registered: true}); err != nil {
		t.Fatalf("persist identity: %v", err)
	}
	return protocol.AgentIdentity{AgentID: out.AgentID, ServerID: out.ServerID}, issued
}

// ---------------------------------------------------------------------------
// Other fakes
// ---------------------------------------------------------------------------

// fakeLogSource implements logs.Docker.
type fakeLogSource struct {
	mu    sync.Mutex
	lines []string
	calls int
}

func (f *fakeLogSource) Logs(_ context.Context, _ string, _ int, _ bool) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return strings.Join(f.lines, "\n") + "\n", nil
}

func (f *fakeLogSource) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}
