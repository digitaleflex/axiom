package executor

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/digitaleflex/axiom/services/engine/internal/deployment"
	"github.com/digitaleflex/axiom/services/engine/internal/planner"
)

type fakeBuilder struct{ err error }

func (b fakeBuilder) Build(context.Context, BuildRequest) (BuildResult, error) {
	return BuildResult{ImageRef: "registry.example/app:1", ArtifactID: "artifact-1"}, b.err
}

type fakeAgent struct {
	steps     []string
	healthErr error
}

func (a *fakeAgent) CreateRuntime(context.Context, CreateRuntimeRequest) error {
	a.steps = append(a.steps, "CREATE_RUNTIME")
	return nil
}
func (a *fakeAgent) ConfigureNetwork(context.Context, NetworkRequest) error {
	a.steps = append(a.steps, "NETWORK")
	return nil
}
func (a *fakeAgent) StartRuntime(context.Context, StartRequest) error {
	a.steps = append(a.steps, "START")
	return nil
}
func (a *fakeAgent) HealthCheck(context.Context, HealthCheckRequest) error {
	a.steps = append(a.steps, "VERIFY")
	return a.healthErr
}

var allSteps = []string{"BUILD", "CREATE_RUNTIME", "NETWORK", "START", "VERIFY"}

func setup(t *testing.T) (*deployment.Service, deployment.Record) {
	t.Helper()
	store := deployment.NewMemoryStore()
	store.AddPlan(deployment.MemoryPlan{ID: "plan_1", ApplicationID: "app_1", ServerID: "srv_1", Environment: "production", Steps: allSteps})
	svc := deployment.NewService(store, nil)
	rec, _, err := svc.Create(context.Background(), deployment.CreateInput{ApplicationID: "app_1", PlanID: "plan_1"})
	if err != nil {
		t.Fatal(err)
	}
	return svc, rec
}

func request(id string) Request {
	return Request{
		DeploymentID: id, Repository: "org/repo", Ref: "main", WorkDir: "/tmp/build",
		Image: "registry.example/app:1", Container: "axiom-app-1",
		Plan: planner.Plan{
			ID: "plan_1", ServerID: "srv_1",
			Build:   planner.BuildPlan{Command: "pnpm build"},
			Runtime: planner.RuntimePlan{Port: 3000},
			Network: planner.NetworkPlan{Proxy: "traefik", Domain: "app.example.com", TLS: true, ExposedPort: 3000},
			Health:  planner.HealthPlan{Path: "/", TimeoutSeconds: 30},
		},
	}
}

func TestExecuteReachesLive(t *testing.T) {
	svc, rec := setup(t)
	agent := &fakeAgent{}
	if _, err := New(svc, fakeBuilder{}, agent).Execute(context.Background(), request(rec.ID)); err != nil {
		t.Fatal(err)
	}
	got, _ := svc.Get(context.Background(), rec.ID)
	if got.Status != deployment.StateLive || got.URL != "https://app.example.com" || got.CompletedAt == nil {
		t.Fatalf("unexpected record: %+v", got)
	}
	if want := "CREATE_RUNTIME,NETWORK,START,VERIFY"; strings.Join(agent.steps, ",") != want {
		t.Fatalf("agent steps = %v, want %s", agent.steps, want)
	}
	steps, _ := svc.Store().Steps(context.Background(), rec.ID)
	for _, s := range steps {
		if s.Status != deployment.StepCompleted {
			t.Fatalf("step %s = %s, want COMPLETED", s.Name, s.Status)
		}
	}
}

func TestHealthFailureNeverReachesLive(t *testing.T) {
	svc, rec := setup(t)
	_, err := New(svc, fakeBuilder{}, &fakeAgent{healthErr: errors.New("connection refused")}).Execute(context.Background(), request(rec.ID))
	if err == nil {
		t.Fatal("expected failure")
	}
	got, _ := svc.Get(context.Background(), rec.ID)
	if got.Status != deployment.StateFailed || got.ErrorCode != ErrorHealthCheckFailed {
		t.Fatalf("status=%s code=%s, want FAILED/HEALTH_CHECK_FAILED", got.Status, got.ErrorCode)
	}
}

func TestBuildFailureRecordsCode(t *testing.T) {
	svc, rec := setup(t)
	agent := &fakeAgent{}
	_, err := New(svc, fakeBuilder{err: errors.New("exit 1")}, agent).Execute(context.Background(), request(rec.ID))
	if err == nil {
		t.Fatal("expected failure")
	}
	got, _ := svc.Get(context.Background(), rec.ID)
	if got.Status != deployment.StateFailed || got.ErrorCode != ErrorBuildFailed || len(agent.steps) != 0 {
		t.Fatalf("status=%s code=%s agent=%v", got.Status, got.ErrorCode, agent.steps)
	}
}
