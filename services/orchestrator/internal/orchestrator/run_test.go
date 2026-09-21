package orchestrator

import (
	"testing"

	orchcontext "github.com/digitaleflex/axiom/services/orchestrator/internal/context"
	"github.com/digitaleflex/axiom/services/orchestrator/internal/domain"
	"github.com/digitaleflex/axiom/services/orchestrator/internal/policy"
	"github.com/digitaleflex/axiom/services/orchestrator/internal/router"
	"github.com/digitaleflex/axiom/services/orchestrator/internal/adapters"
)

type fakeExecutor struct{ calls int }

func (f *fakeExecutor) Execute(task adapters.Task, agent adapters.Agent) (adapters.Result, error) {
	f.calls++
	return adapters.Result{ExecutionID: "exec-" + task.ID, Artifacts: []adapters.Artifact{{ID: "artifact-" + task.ID, Type: "result", Version: "1", Valid: true}}}, nil
}

func TestRunEngineExecutesDependencyChain(t *testing.T) {
	executor := &fakeExecutor{}
	engine := RunEngine{
		Engine: Engine{
			Agents: []router.Agent{{ID: "architect-1", Role: "Architect", Capabilities: map[string]bool{"architecture": true}, Permissions: map[string]bool{"architecture": true}}},
			Executor: executor,
			Policy: policy.StaticPolicy{Rules: map[string]policy.Decision{"architecture:architecture": policy.Allow}},
		},
		Transition: MemoryTransitioner{},
	}
	run := &domain.Run{ID: "run-1", ProjectID: "project-1", State: "CREATED"}
	tasks := []domain.Task{
		{ID: "t1", ProjectID: "project-1", Role: "Architect", Objective: "Define architecture", RequiredCapabilities: []string{"architecture"}, Status: "PENDING"},
		{ID: "t2", ProjectID: "project-1", Role: "Architect", Objective: "Refine architecture", RequiredCapabilities: []string{"architecture"}, Dependencies: []string{"t1"}, Status: "PENDING"},
	}

	if err := engine.Execute(run, tasks, orchcontext.NewSnapshot("ctx-1", "project-1", "t1", nil, nil)); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if run.State != "COMPLETED" {
		t.Fatalf("expected COMPLETED, got %s", run.State)
	}
	if executor.calls != 2 {
		t.Fatalf("expected 2 executions, got %d", executor.calls)
	}
}
