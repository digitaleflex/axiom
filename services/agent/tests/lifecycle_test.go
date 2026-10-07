package tests

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitaleflex/axiom/services/agent/internal/capabilities"
	"github.com/digitaleflex/axiom/services/agent/internal/dispatcher"
	"github.com/digitaleflex/axiom/services/agent/internal/health"
	"github.com/digitaleflex/axiom/services/agent/internal/logs"
	"github.com/digitaleflex/axiom/services/agent/internal/protocol"
	"github.com/digitaleflex/axiom/services/agent/internal/recovery"
	"github.com/digitaleflex/axiom/services/agent/internal/runtime/docker"
	"github.com/digitaleflex/axiom/services/agent/internal/security/ownership"
	"github.com/digitaleflex/axiom/services/agent/internal/state"
)

// TestScenario_CapabilityDiscovery covers discovery with Docker absent and
// present. Discovery never fails: absent tools are reported unavailable.
func TestScenario_CapabilityDiscovery(t *testing.T) {
	t.Run("docker absent", func(t *testing.T) {
		d := capabilities.Discoverer{
			AgentVersion: "0.1.0",
			DataRoot:     t.TempDir(),
			Runner: capabilities.FuncRunner(func(context.Context, string, ...string) (string, error) {
				return "", errors.New("exec: executable file not found")
			}),
		}
		rep := d.Discover(context.Background())
		if rep.Docker.Available || rep.Docker.ComposeAvailable || rep.Traefik.Available {
			t.Fatalf("report claims unavailable tools: %+v", rep)
		}
		if caps := rep.Capabilities(); len(caps) != 0 {
			t.Fatalf("capabilities = %v, want none", caps)
		}
		if rep.CollectedAt.IsZero() {
			t.Fatal("report has no collection timestamp")
		}
	})

	t.Run("docker present", func(t *testing.T) {
		d := capabilities.Discoverer{
			AgentVersion: "0.1.0",
			DataRoot:     t.TempDir(),
			Runner: capabilities.FuncRunner(func(_ context.Context, name string, args ...string) (string, error) {
				switch {
				case name == "docker" && len(args) > 0 && args[0] == "version":
					return "27.0.1", nil
				case name == "docker" && len(args) > 0 && args[0] == "compose":
					return "2.29.0", nil
				case name == "traefik":
					return "Version:     v3.1.0\nCodename:    beaufort", nil
				}
				return "", errors.New("unexpected command")
			}),
		}
		rep := d.Discover(context.Background())
		if !rep.Docker.Available || !rep.Docker.ComposeAvailable || !rep.Traefik.Available {
			t.Fatalf("report = %+v, want docker/compose/traefik available", rep)
		}
		if !rep.TLS.Automatic {
			t.Fatal("TLS should be automatic when Traefik is present")
		}
		caps := rep.Capabilities()
		for _, want := range []string{capabilities.CapDocker, capabilities.CapDockerCompose, capabilities.CapTraefik, capabilities.CapTLS} {
			if !containsString(caps, want) {
				t.Fatalf("capabilities = %v, missing %s", caps, want)
			}
		}
		if rep.Traefik.Version != "v3.1.0" {
			t.Fatalf("traefik version = %q, want v3.1.0", rep.Traefik.Version)
		}
	})
}

func containsString(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

// TestScenario_RestartRecovery reopens the durable state store: RUNNING
// operations become INTERRUPTED, and only VERIFY is resumable.
func TestScenario_RestartRecovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.log")
	s, err := state.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	createID := opID("CREATE_RUNTIME", 1)
	verifyID := opID("VERIFY", 1)
	if _, err := s.Begin(state.Operation{OperationID: createID, DeploymentID: testDeployment, Type: protocol.OpCreateRuntime}); err != nil {
		t.Fatalf("begin create: %v", err)
	}
	if _, err := s.Begin(state.Operation{OperationID: verifyID, DeploymentID: testDeployment, Type: protocol.OpVerifyHealth}); err != nil {
		t.Fatalf("begin verify: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Restart: reopening classifies the active entries.
	s2, err := state.Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()

	create, ok := s2.Get(createID)
	if !ok || create.Phase != state.PhaseInterrupted || !create.Interrupted {
		t.Fatalf("create entry = %+v (ok=%v), want INTERRUPTED", create, ok)
	}
	if create.Resumable {
		t.Fatal("CREATE_RUNTIME must not be resumable")
	}
	verify, ok := s2.Get(verifyID)
	if !ok || verify.Phase != state.PhaseInterrupted || !verify.Interrupted {
		t.Fatalf("verify entry = %+v (ok=%v), want INTERRUPTED", verify, ok)
	}
	if !verify.Resumable {
		t.Fatal("VERIFY must be resumable")
	}

	plan := recovery.LoadInterrupted(s2)
	if !hasEntry(plan.Resumable, verifyID) {
		t.Fatalf("resumable plan = %+v, want VERIFY", plan.Resumable)
	}
	if !hasEntry(plan.NeedsReconciliation, createID) {
		t.Fatalf("reconciliation plan = %+v, want CREATE_RUNTIME", plan.NeedsReconciliation)
	}
}

func hasEntry(entries []state.Entry, id string) bool {
	for _, e := range entries {
		if e.OperationID == id {
			return true
		}
	}
	return false
}

// fakeReconRuntime implements recovery.Runtime.
type fakeReconRuntime struct {
	mu         sync.Mutex
	containers map[string]recovery.ContainerInfo
	removed    []string
}

func newFakeReconRuntime() *fakeReconRuntime {
	return &fakeReconRuntime{containers: map[string]recovery.ContainerInfo{}}
}

func (f *fakeReconRuntime) put(name string, labels map[string]string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.containers[name] = recovery.ContainerInfo{Name: name, Labels: labels}
}

func (f *fakeReconRuntime) List() ([]recovery.ContainerInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]recovery.ContainerInfo, 0, len(f.containers))
	for _, c := range f.containers {
		out = append(out, c)
	}
	return out, nil
}

func (f *fakeReconRuntime) Inspect(name string) (recovery.ContainerInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.containers[name]
	if !ok {
		return recovery.ContainerInfo{}, fmt.Errorf("not found: %s", name)
	}
	return c, nil
}

func (f *fakeReconRuntime) Remove(name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.containers, name)
	f.removed = append(f.removed, name)
	return nil
}

// TestScenario_Reconciliation classifies orphan, missing-state, unmanaged and
// consistent containers, and only removes allow-listed orphans.
func TestScenario_Reconciliation(t *testing.T) {
	const (
		depWithState = testDeployment
		depMissing   = "dep_aaaaaaaaaaaaaaaaaaaaaaaa"
		depOrphan    = "dep_bbbbbbbbbbbbbbbbbbbbbbbb"
	)

	st, err := state.Open(filepath.Join(t.TempDir(), "state.log"))
	if err != nil {
		t.Fatalf("open state: %v", err)
	}
	defer st.Close()
	createID := "op_" + depWithState + "_CREATE_RUNTIME_1"
	if _, err := st.Begin(state.Operation{OperationID: createID, DeploymentID: depWithState, Type: protocol.OpCreateRuntime}); err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := st.Complete(createID, state.Result{Success: true}); err != nil {
		t.Fatalf("complete: %v", err)
	}

	rt := newFakeReconRuntime()
	rt.put("axiom-consistent-1", ownership.NewLabels(depWithState, "app", "srv"))
	rt.put("axiom-missing-1", ownership.NewLabels(depMissing, "app", "srv"))
	rt.put("axiom-orphan-1", ownership.NewLabels(depOrphan, "app", "srv"))
	rt.put("foreign-1", map[string]string{"some": "label"})

	rec := &recovery.Reconciler{Runtime: rt, State: st}
	rep, err := rec.Reconcile(context.Background(), []string{depWithState, depMissing})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if rep.Scanned != 4 || rep.Managed != 3 || rep.Ignored != 1 {
		t.Fatalf("counts = scanned:%d managed:%d ignored:%d, want 4/3/1", rep.Scanned, rep.Managed, rep.Ignored)
	}
	if len(rep.Consistent) != 1 || rep.Consistent[0].Container != "axiom-consistent-1" {
		t.Fatalf("consistent = %+v", rep.Consistent)
	}
	if len(rep.MissingState) != 1 || rep.MissingState[0].Container != "axiom-missing-1" {
		t.Fatalf("missing state = %+v", rep.MissingState)
	}
	if len(rep.Orphans) != 1 || rep.Orphans[0].Container != "axiom-orphan-1" {
		t.Fatalf("orphans = %+v", rep.Orphans)
	}
	if len(rep.Removed) != 0 {
		t.Fatalf("removed without allow-list: %+v", rep.Removed)
	}

	// Allow-listed cleanup removes exactly the orphan.
	rec.AllowCleanup = true
	rec.RemoveDeployments = []string{depOrphan}
	rep2, err := rec.Reconcile(context.Background(), []string{depWithState, depMissing})
	if err != nil {
		t.Fatalf("reconcile cleanup: %v", err)
	}
	if len(rep2.Removed) != 1 || rep2.Removed[0].Container != "axiom-orphan-1" {
		t.Fatalf("removed = %+v, want axiom-orphan-1", rep2.Removed)
	}
	if len(rt.removed) != 1 || rt.removed[0] != "axiom-orphan-1" {
		t.Fatalf("runtime removals = %v", rt.removed)
	}
}

// TestScenario_HealthFailure verifies a failing probe surfaces as a structured
// health failure, and as HEALTH_CHECK_FAILED through the dispatcher.
func TestScenario_HealthFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	checker := &health.Checker{Sleep: func(ctx context.Context, _ time.Duration) error { return ctx.Err() }}
	_, err := checker.Check(context.Background(), health.Spec{
		URL:            srv.URL,
		Timeout:        2 * time.Second,
		Retries:        0,
		ExpectedStatus: "200-399",
	})
	var ue *health.UnhealthyError
	if !errors.As(err, &ue) {
		t.Fatalf("Check() = %v, want *health.UnhealthyError", err)
	}
	if ue.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", ue.StatusCode)
	}
	if !errors.Is(err, health.ErrUnhealthy) {
		t.Fatalf("Check() = %v, want errors.Is(ErrUnhealthy)", err)
	}

	h := newHarness(t)
	h.br.HealthURL = srv.URL
	d := h.dispatcher()
	op := newOperation(opID("VERIFY", 1), protocol.OpVerifyHealth,
		protocol.Payload{Domain: "app.example.com", Path: "/health", TimeoutSeconds: 5}, testServer)
	_, result := d.Dispatch(context.Background(), op, testIdentity)
	if result.Success || result.ErrorCode != codeHealthFailed {
		t.Fatalf("result = %+v, want %s", result, codeHealthFailed)
	}
}

// TestScenario_LogStreaming verifies the bounded stream channel and the
// truncation marker under overflow, plus redaction on fetch.
func TestScenario_LogStreaming(t *testing.T) {
	const n = 250
	lines := make([]string, n)
	for i := range lines {
		lines[i] = fmt.Sprintf("line-%03d", i)
	}
	src := &fakeLogSource{lines: lines}
	f := &logs.Fetcher{Docker: src, PollInterval: time.Millisecond}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := f.Stream(ctx, testContainer, true)
	if cap(ch) != logs.StreamBuffer {
		t.Fatalf("stream capacity = %d, want %d", cap(ch), logs.StreamBuffer)
	}
	// Wait until the first batch has been pushed and the producer has moved on
	// to a later poll. The cursor dedupes, so no further entries are queued:
	// the buffer is full and stable, making the overflow deterministic without
	// racing the consumer.
	waitFor(t, "first log batch", func() bool { return src.callCount() >= 2 })
	if len(ch) != logs.StreamBuffer {
		t.Fatalf("buffered entries = %d, want %d (bounded)", len(ch), logs.StreamBuffer)
	}
	var markers int
	for i := 0; i < logs.StreamBuffer; i++ {
		e := <-ch
		if e.Stream == logs.StreamSystem && e.Message == logs.TruncationMessage {
			markers++
		}
	}
	if markers == 0 {
		t.Fatal("overflow produced no truncation marker")
	}
	cancel()

	// Redaction runs before entries leave the agent.
	src2 := &fakeLogSource{lines: []string{"password=hunter2 token=abc"}}
	f2 := &logs.Fetcher{Docker: src2}
	entries, err := f2.Fetch(context.Background(), testContainer, logs.FetchOptions{})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	if strings.Contains(entries[0].Message, "hunter2") || strings.Contains(entries[0].Message, "abc") {
		t.Fatalf("secret leaked: %q", entries[0].Message)
	}
	if !strings.Contains(entries[0].Message, logs.RedactionMarker) {
		t.Fatalf("no redaction marker: %q", entries[0].Message)
	}
}

// TestGap_DockerErrorLacksStableCodeMethod documents a wiring gap: the Docker
// adapter exposes its stable code as the struct field *docker.Error.Code, but
// does not implement dispatcher.ErrorCoder's ErrorCode() method. A production
// dispatcher.Adapter bridge must therefore map it (the harness bridge does).
// If this test's log line flips to "gap closed", update the failure matrix.
func TestGap_DockerErrorLacksStableCodeMethod(t *testing.T) {
	var de error = &docker.Error{Code: docker.CodeDockerFailed}
	if _, ok := de.(dispatcher.ErrorCoder); ok {
		t.Log("gap closed: *docker.Error now implements dispatcher.ErrorCoder")
		return
	}
	t.Log("gap confirmed: *docker.Error does not implement dispatcher.ErrorCoder")
}
