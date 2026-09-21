package orchestrator

import (
	"testing"
	"github.com/digitaleflex/axiom/services/orchestrator/internal/adapters"
	orchcontext "github.com/digitaleflex/axiom/services/orchestrator/internal/context"
	"github.com/digitaleflex/axiom/services/orchestrator/internal/domain"
	"github.com/digitaleflex/axiom/services/orchestrator/internal/lifecycle"
	"github.com/digitaleflex/axiom/services/orchestrator/internal/policy"
	"github.com/digitaleflex/axiom/services/orchestrator/internal/router"
)

type fakeExecutor struct{}
func (fakeExecutor) Execute(task adapters.Task, agent adapters.Agent) (adapters.Result, error) {
	return adapters.Result{ExecutionID: "exec-1", Artifacts: []adapters.Artifact{{ID:"a-1", Type:"architecture", Version:"1", Valid:true}}}, nil
}

func TestExecuteTask(t *testing.T) {
	e := Engine{
		Agents: []router.Agent{{ID:"architect-1", Role:"Architect", Capabilities:map[string]bool{"architecture":true}, Permissions:map[string]bool{"architecture":true}}},
		Executor: fakeExecutor{},
		Policy: policy.StaticPolicy{Rules: map[string]policy.Decision{"architecture:architecture": policy.Allow}},
	}
	run := &domain.Run{ID:"run-1", ProjectID:"project-1", State:string(lifecycle.RunRunning)}
	task := domain.Task{ID:"task-1", ProjectID:"project-1", Role:"Architect", Objective:"define architecture", RequiredCapabilities:[]string{"architecture"}}
	result, err := e.ExecuteTask(run, task, orchcontext.NewSnapshot("ctx-1", "project-1", "task-1", nil, nil))
	if err != nil { t.Fatal(err) }
	if result.ExecutionID != "exec-1" { t.Fatalf("unexpected execution: %s", result.ExecutionID) }
}
