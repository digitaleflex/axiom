package executor

import (
	"context"
	"testing"

	"github.com/digitaleflex/axiom/services/engine/internal/deployment"
	"github.com/digitaleflex/axiom/services/engine/internal/planner"
)

type fakeDeploymentRepo struct {
	records map[string]deployment.Record
}

func (r *fakeDeploymentRepo) Create(_ context.Context, id, applicationID, serverID, environment string) error {
	if r.records == nil {
		r.records = map[string]deployment.Record{}
	}
	r.records[id] = deployment.Record{
		ID: id, ApplicationID: applicationID, ServerID: serverID,
		Environment: environment, Status: deployment.StatePending,
	}
	return nil
}
func (r *fakeDeploymentRepo) SetStatus(_ context.Context, id, status string) error {
	rec := r.records[id]
	rec.Status = deployment.State(status)
	r.records[id] = rec
	return nil
}
func (r *fakeDeploymentRepo) GetDomainRecord(_ context.Context, id string) (deployment.Record, error) {
	return r.records[id], nil
}

type fakeBuilder struct{}
func (fakeBuilder) Build(context.Context, BuildRequest) (BuildResult, error) {
	return BuildResult{ImageRef: "registry.example/app:1", ArtifactID: "artifact-1"}, nil
}

type fakeAgent struct {
	steps []string
}
func (a *fakeAgent) Prepare(context.Context, PrepareRequest) error { a.steps = append(a.steps, "PREPARE"); return nil }
func (a *fakeAgent) CreateRuntime(context.Context, CreateRuntimeRequest) error { a.steps = append(a.steps, "CREATE_RUNTIME"); return nil }
func (a *fakeAgent) ConfigureNetwork(context.Context, NetworkRequest) error { a.steps = append(a.steps, "NETWORK"); return nil }
func (a *fakeAgent) StartRuntime(context.Context, StartRequest) error { a.steps = append(a.steps, "START"); return nil }
func (a *fakeAgent) HealthCheck(context.Context, HealthCheckRequest) error { a.steps = append(a.steps, "VERIFY"); return nil }

func TestExecuteReachesLive(t *testing.T) {
	repo := &fakeDeploymentRepo{}
	service := deployment.NewService(repo, nil)
	record, err := service.Create(context.Background(), "app_1", "srv_1", "production", "plan_1")
	if err != nil { t.Fatal(err) }

	agent := &fakeAgent{}
	executor := New(service, fakeBuilder{}, agent)
	_, err = executor.Execute(context.Background(), Request{
		DeploymentID: record.ID,
		Repository: "org/repo",
		Ref: "main",
		WorkDir: "/tmp/build",
		Image: "registry.example/app:1",
		Container: "axiom-app-1",
		Plan: planner.Plan{
			ID: "plan_1", ServerID: "srv_1",
			Build: planner.BuildPlan{Command: "pnpm build"},
			Runtime: planner.RuntimePlan{Port: 3000},
			Network: planner.NetworkPlan{Proxy: "traefik", Domain: "app.example.com", TLS: true, ExposedPort: 3000},
			Health: planner.HealthPlan{Path: "/", TimeoutSeconds: 30},
		},
	})
	if err != nil { t.Fatal(err) }
	if got := repo.records[record.ID].Status; got != deployment.StateLive {
		t.Fatalf("status = %s, want %s", got, deployment.StateLive)
	}
	if len(agent.steps) != 5 {
		t.Fatalf("steps = %d, want 5", len(agent.steps))
	}
}
