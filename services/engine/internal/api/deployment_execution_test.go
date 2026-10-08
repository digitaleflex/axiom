package api

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/digitaleflex/axiom/services/engine/internal/application"
	"github.com/digitaleflex/axiom/services/engine/internal/deployment"
	"github.com/digitaleflex/axiom/services/engine/internal/executor"
	"github.com/digitaleflex/axiom/services/engine/internal/planner"
	"github.com/digitaleflex/axiom/services/engine/internal/profile"
	"github.com/digitaleflex/axiom/services/engine/internal/server"
)

// recordingRunner captures the execution requests the API hands to the
// executor. It is the proof that the break point is closed: createDeployment
// must trigger execution exactly once per created deployment.
type recordingRunner struct {
	mu       sync.Mutex
	requests []executor.Request
	err      error
	started  chan struct{}
}

func newRecordingRunner() *recordingRunner {
	return &recordingRunner{started: make(chan struct{}, 8)}
}

func (r *recordingRunner) Start(_ context.Context, req executor.Request) error {
	r.mu.Lock()
	r.requests = append(r.requests, req)
	err := r.err
	r.mu.Unlock()
	select {
	case r.started <- struct{}{}:
	default:
	}
	return err
}

func (r *recordingRunner) all() []executor.Request {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]executor.Request(nil), r.requests...)
}

// fakeSources serves a fixed archive; it records that the build stage asked
// for the exact commit the plan pinned.
type fakeSources struct {
	mu    sync.Mutex
	calls []string
}

func (f *fakeSources) Archive(_ context.Context, userID, repoID, sha string) (io.ReadCloser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, userID+"/"+repoID+"/"+sha)
	return io.NopCloser(strings.NewReader("")), nil
}

func (f *fakeSources) seen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

// execPlans serves plan_1 with the fields the execution request needs.
type execPlans struct{}

func (execPlans) Create(_ context.Context, in planner.CreateInput) (planner.Plan, error) {
	return execPlan(in.ApplicationID), nil
}

func (execPlans) Get(_ context.Context, id string) (planner.PlanView, error) {
	if id != "plan_1" {
		return planner.PlanView{}, planner.ErrPlanNotFound
	}
	return planner.PlanView{Plan: execPlan("app_1")}, nil
}

func execPlan(appID string) planner.Plan {
	return planner.Plan{
		ID: "plan_1", Status: "READY", ApplicationID: appID,
		ServerID: "srv_1", Environment: "production",
		Source:  profile.Source{RepositoryID: "repo_1", Ref: "main", Commit: "e8e8e8e8e8e8e8e8e8e8e8e8e8e8e8e8e8e8e8e8"},
		Runtime: planner.RuntimePlan{Port: 3000},
		Network: planner.NetworkPlan{Proxy: "traefik", Domain: "app.acme.dev", ExposedPort: 443},
		Steps:   planner.CanonicalSteps,
	}
}

// execHarness wires an API with a recording runner.
func execHarness(t *testing.T) (*harness, *recordingRunner, *fakeSources) {
	t.Helper()
	store := deployment.NewMemoryStore()
	store.AddPlan(deployment.MemoryPlan{ID: "plan_1", ApplicationID: "app_1", ServerID: "srv_1",
		Environment: "production", Steps: []string{"BUILD", "CREATE_RUNTIME", "NETWORK", "START", "VERIFY"}})
	svc := deployment.NewService(store, nil)
	runner, sources := newRecordingRunner(), &fakeSources{}
	h := &harness{t: t, store: store, svc: svc}
	h.handler = New(Deps{
		Log:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		Auth:         NewTokenAuthenticator(token, Principal{UserID: "usr_1", Name: "Jane"}),
		Deployments:  svc,
		Applications: &fakeApps{items: map[string]application.Record{"app_1": {ID: "app_1", Name: "acme-web", RepositoryID: "repo_1", OwnerID: "usr_1"}}},
		Servers:      &fakeServers{items: []server.Record{{ID: "srv_1", Name: "srv-eu-1", Address: "203.0.113.10", OwnerID: "usr_1", Status: server.StatusReady}}},
		Plans:        execPlans{},
		Runner:       runner,
		Sources:      sources,
	})
	return h, runner, sources
}

// TestCreateDeploymentTriggersExecution is the proof that the break point is
// closed: the handler persists the record and then starts the execution with
// a fully-resolved request.
func TestCreateDeploymentTriggersExecution(t *testing.T) {
	h, runner, sources := execHarness(t)
	r := h.do("POST", "/api/v1/applications/app_1/deployments", map[string]any{"planId": "plan_1"}, nil)
	expect(t, r, 202, "")
	id := r.body["id"].(string)
	if r.body["status"] != "PENDING" {
		t.Fatalf("created deployment must be answered while PENDING: %v", r.body)
	}
	if len(runner.all()) != 1 {
		t.Fatalf("execution must be triggered exactly once, got %d", len(runner.all()))
	}
	req := runner.all()[0]
	if req.DeploymentID != id {
		t.Fatalf("execution targets %q, want %q", req.DeploymentID, id)
	}
	if req.AppSlug != "acme-web" {
		t.Fatalf("appSlug = %q", req.AppSlug)
	}
	if req.Plan.ID != "plan_1" || req.Plan.ServerID != "srv_1" {
		t.Fatalf("plan not carried: %+v", req.Plan)
	}
	if req.Container != "axiom-acme-web-"+lastN(strings.TrimPrefix(id, "dep_"), 12) {
		t.Fatalf("container = %q", req.Container)
	}
	if req.Source == nil {
		t.Fatal("execution request carries no build source")
	}
	// The source is resolved lazily: the archive is fetched by the build step,
	// never by the request handler.
	if len(sources.seen()) != 0 {
		t.Fatalf("handler must not fetch the archive: %v", sources.seen())
	}
	if _, err := req.Source.Archive(context.Background()); err != nil {
		t.Fatalf("source archive: %v", err)
	}
	want := "usr_1/repo_1/e8e8e8e8e8e8e8e8e8e8e8e8e8e8e8e8e8e8e8e8"
	if got := sources.seen(); len(got) != 1 || got[0] != want {
		t.Fatalf("archive fetched for %v, want [%s]", got, want)
	}
}

// TestCreateDeploymentReplayDoesNotExecute proves the idempotency contract:
// a replay returns the original record and must not start a second execution.
func TestCreateDeploymentReplayDoesNotExecute(t *testing.T) {
	h, runner, _ := execHarness(t)
	path := "/api/v1/applications/app_1/deployments"
	key := map[string]string{"Idempotency-Key": "deploy:plan_1"}

	r := h.do("POST", path, map[string]any{"planId": "plan_1"}, key)
	expect(t, r, 202, "")
	id := r.body["id"].(string)

	replay := h.do("POST", path, map[string]any{"planId": "plan_1"}, key)
	expect(t, replay, 202, "")
	if replay.hdr.Get("Idempotent-Replayed") != "true" || replay.body["id"] != id {
		t.Fatalf("replay must return the original deployment: %v", replay.body)
	}
	if got := runner.all(); len(got) != 1 || got[0].DeploymentID != id {
		t.Fatalf("replay must not trigger a second execution, got %d request(s)", len(got))
	}

	// A different request under the same key is still a conflict.
	expect(t, h.do("POST", path, map[string]any{"planId": "plan_1"}, map[string]string{"Idempotency-Key": "other"}), 409, CodeConflict)
}

// TestCreateDeploymentWithoutRunnerStaysPending documents the nil-runner
// behavior: an execution-less Engine persists and answers 202 without
// pretending an execution started.
func TestCreateDeploymentWithoutRunnerStaysPending(t *testing.T) {
	h := newHarness(t)
	r := h.do("POST", "/api/v1/applications/app_1/deployments", map[string]any{"planId": "plan_1"}, nil)
	expect(t, r, 202, "")
	rec, err := h.svc.Get(context.Background(), r.body["id"].(string))
	if err != nil || rec.Status != deployment.StatePending {
		t.Fatalf("deployment = %+v err = %v", rec, err)
	}
}

// TestCreateDeploymentStartFailureMarksFailed proves a deployment that was
// persisted but cannot start is not left PENDING forever.
func TestCreateDeploymentStartFailureMarksFailed(t *testing.T) {
	h, runner, _ := execHarness(t)
	runner.err = errors.New("runner is shut down")
	r := h.do("POST", "/api/v1/applications/app_1/deployments", map[string]any{"planId": "plan_1"}, nil)
	expect(t, r, 202, "")
	id := r.body["id"].(string)
	rec, err := h.svc.Get(context.Background(), id)
	if err != nil {
		t.Fatalf("load deployment: %v", err)
	}
	if rec.Status != deployment.StateFailed || rec.ErrorCode != CodeInternalError {
		t.Fatalf("unstartable deployment must be FAILED/INTERNAL_ERROR, got %s/%s", rec.Status, rec.ErrorCode)
	}
}

// TestStartExecutionMissingPlanFailsDeployment covers the wiring guard: a
// runner without a plan read model cannot build a request.
func TestStartExecutionMissingPlanFailsDeployment(t *testing.T) {
	store := deployment.NewMemoryStore()
	store.AddPlan(deployment.MemoryPlan{ID: "plan_1", ApplicationID: "app_1", ServerID: "srv_1",
		Environment: "production", Steps: []string{"BUILD"}})
	svc := deployment.NewService(store, nil)
	h := &harness{t: t, store: store, svc: svc}
	h.handler = New(Deps{
		Log:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		Auth:         NewTokenAuthenticator(token, Principal{UserID: "usr_1"}),
		Deployments:  svc,
		Applications: &fakeApps{items: map[string]application.Record{"app_1": {ID: "app_1", Name: "acme-web", OwnerID: "usr_1"}}},
		Runner:       newRecordingRunner(),
	})
	r := h.do("POST", "/api/v1/applications/app_1/deployments", map[string]any{"planId": "plan_1"}, nil)
	expect(t, r, 202, "")
	rec, err := h.svc.Get(context.Background(), r.body["id"].(string))
	if err != nil {
		t.Fatalf("load deployment: %v", err)
	}
	if rec.Status != deployment.StateFailed {
		t.Fatalf("deployment without a plan read model must fail, got %s", rec.Status)
	}
}

// lastN returns the trailing n characters of s (s when shorter).
func lastN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

func TestContainerName(t *testing.T) {
	cases := []struct {
		slug, id, want string
		bad            bool
	}{
		{slug: "acme-web", id: "dep_0123456789abcdef01234567", want: "axiom-acme-web-cdef01234567"},
		{slug: "Acme_Web", id: "dep_0123456789abcdef01234567", want: "axiom-acme-web-cdef01234567"},
		{slug: "", id: "dep_1", bad: true},
		{slug: "a", id: "", bad: true},
		{slug: strings.Repeat("x", 60), id: "dep_0123456789abcdef01234567", want: "axiom-" + strings.Repeat("x", 40) + "-cdef01234567"},
	}
	for _, c := range cases {
		got, err := ContainerName(c.slug, c.id)
		if c.bad {
			if err == nil {
				t.Fatalf("ContainerName(%q, %q) = %q, want error", c.slug, c.id, got)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Fatalf("ContainerName(%q, %q) = %q, %v; want %q", c.slug, c.id, got, err, c.want)
		}
		if len(got) > 63 {
			t.Fatalf("container name exceeds the agent's 63-character rule: %q", got)
		}
	}
}
