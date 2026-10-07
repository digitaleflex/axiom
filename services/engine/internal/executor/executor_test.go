package executor

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/digitaleflex/axiom/services/engine/internal/build"
	"github.com/digitaleflex/axiom/services/engine/internal/deployment"
	"github.com/digitaleflex/axiom/services/engine/internal/planner"
	"github.com/digitaleflex/axiom/services/engine/internal/profile"
)

var commit = strings.Repeat("a", 40)

type fakeSource struct{}

func (fakeSource) Archive(context.Context) (io.ReadCloser, error) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: "repo/", Typeflag: tar.TypeDir, Mode: 0o755})
	body := `{"scripts":{"build":"next build","start":"next start"},"dependencies":{"next":"14"}}`
	_ = tw.WriteHeader(&tar.Header{Name: "repo/package.json", Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(body))})
	_, _ = tw.Write([]byte(body))
	_ = tw.Close()
	_ = gz.Close()
	return io.NopCloser(&buf), nil
}

type fakeBuilder struct{ err error }

func (b fakeBuilder) Build(_ context.Context, in build.Input) (build.Result, error) {
	if b.err != nil {
		return build.Result{}, b.err
	}
	return build.Result{
		ImageRef: "axiom-local/acme-web:" + in.Commit[:7] + "-dep",
		Artifact: build.Artifact{Image: "img", Digest: "sha256:" + strings.Repeat("e", 64), Commit: in.Commit, DeploymentID: in.DeploymentID, Strategy: "source"},
	}, nil
}

type fakeAgent struct {
	steps []string
	image string
}

func (a *fakeAgent) CreateRuntime(_ context.Context, req CreateRuntimeRequest) error {
	a.steps = append(a.steps, "CREATE_RUNTIME")
	a.image = req.ImageRef
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
	return nil
}

var allSteps = []string{"BUILD", "CREATE_RUNTIME", "NETWORK", "START", "VERIFY"}

func setup(t *testing.T) (*deployment.Service, deployment.Record, planner.Plan) {
	t.Helper()
	store := deployment.NewMemoryStore()
	store.AddPlan(deployment.MemoryPlan{ID: "plan_1", ApplicationID: "app_1", ServerID: "srv_1", Environment: "production", Steps: allSteps})
	svc := deployment.NewService(store, nil)
	rec, _, err := svc.Create(context.Background(), deployment.CreateInput{ApplicationID: "app_1", PlanID: "plan_1"})
	if err != nil {
		t.Fatal(err)
	}
	plan := planner.Plan{
		ID: "plan_1", ApplicationID: "app_1", Strategy: "nextjs",
		Source:   profile.Source{RepositoryID: "repo_1", Ref: "main", Commit: commit},
		ServerID: "srv_1",
		Build:    planner.BuildPlan{Strategy: "source", PackageManager: "pnpm", Command: "pnpm run build"},
		Runtime:  planner.RuntimePlan{Type: "node", StartCommand: "pnpm start", Port: 3000},
		Network:  planner.NetworkPlan{Proxy: "traefik", Domain: "app.example.com", TLS: true, ExposedPort: 3000},
		Health:   planner.HealthPlan{Type: "http", Path: "/", TimeoutSeconds: 5},
		Rollback: planner.RollbackPlan{Strategy: "keep_previous_until_verified"},
		Steps:    allSteps,
	}
	return svc, rec, plan
}

func TestExecuteReachesLive(t *testing.T) {
	svc, rec, plan := setup(t)
	agent := &fakeAgent{}
	res, err := New(svc, fakeBuilder{}, agent).Execute(context.Background(), Request{
		DeploymentID: rec.ID, AppSlug: "acme-web", Container: "axiom-app-1", Source: fakeSource{}, Plan: plan,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Deployment.Status != deployment.StateLive || res.Artifact.Digest == "" || res.Artifact.Commit != commit || res.Artifact.Strategy != "source" {
		t.Fatalf("result = %+v", res)
	}
	if agent.image != res.ImageRef {
		t.Fatalf("agent received %q, built %q", agent.image, res.ImageRef)
	}
	if want := "CREATE_RUNTIME,NETWORK,START,VERIFY"; strings.Join(agent.steps, ",") != want {
		t.Fatalf("agent steps = %v", agent.steps)
	}
	steps, _ := svc.Store().Steps(context.Background(), rec.ID)
	for _, s := range steps {
		if s.Status != deployment.StepCompleted {
			t.Fatalf("step %s = %s", s.Name, s.Status)
		}
	}
}

func TestHealthFailureNeverReachesLive(t *testing.T) {
	svc, rec, plan := setup(t)
	_, err := New(svc, fakeBuilder{}, &failAgent{step: "VERIFY"}).Execute(context.Background(), Request{
		DeploymentID: rec.ID, Container: "c", Source: fakeSource{}, Plan: plan,
	})
	if err == nil {
		t.Fatal("expected failure")
	}
	got, _ := svc.Get(context.Background(), rec.ID)
	if got.Status != deployment.StateFailed || got.ErrorCode != ErrorHealthCheckFailed {
		t.Fatalf("status=%s code=%s", got.Status, got.ErrorCode)
	}
}

type failAgent struct{ step string }

func (a *failAgent) CreateRuntime(context.Context, CreateRuntimeRequest) error { return nil }
func (a *failAgent) ConfigureNetwork(context.Context, NetworkRequest) error    { return nil }
func (a *failAgent) StartRuntime(context.Context, StartRequest) error          { return nil }
func (a *failAgent) HealthCheck(context.Context, HealthCheckRequest) error {
	return errors.New("connection refused")
}

func TestBuildFailureRecordsCode(t *testing.T) {
	svc, rec, plan := setup(t)
	agent := &fakeAgent{}
	_, err := New(svc, fakeBuilder{err: &build.Error{Code: build.CodeBuildFailed, Message: "exit 1", ExitCode: 1}}, agent).Execute(context.Background(), Request{
		DeploymentID: rec.ID, Container: "c", Source: fakeSource{}, Plan: plan,
	})
	if err == nil {
		t.Fatal("expected failure")
	}
	got, _ := svc.Get(context.Background(), rec.ID)
	if got.Status != deployment.StateFailed || got.ErrorCode != ErrorBuildFailed || len(agent.steps) != 0 {
		t.Fatalf("status=%s code=%s agent=%v", got.Status, got.ErrorCode, agent.steps)
	}
	steps, _ := svc.Store().Steps(context.Background(), rec.ID)
	if steps[0].Status != deployment.StepFailed {
		t.Fatalf("BUILD step = %s", steps[0].Status)
	}
}
