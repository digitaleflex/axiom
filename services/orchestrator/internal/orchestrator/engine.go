package orchestrator

import (
	"fmt"
	"github.com/digitaleflex/axiom/services/orchestrator/internal/adapters"
	orchcontext "github.com/digitaleflex/axiom/services/orchestrator/internal/context"
	"github.com/digitaleflex/axiom/services/orchestrator/internal/domain"
	"github.com/digitaleflex/axiom/services/orchestrator/internal/gates"
	"github.com/digitaleflex/axiom/services/orchestrator/internal/lifecycle"
	"github.com/digitaleflex/axiom/services/orchestrator/internal/policy"
	"github.com/digitaleflex/axiom/services/orchestrator/internal/router"
)

type Engine struct {
	Agents []router.Agent
	Executor adapters.Executor
	Policy policy.Policy
}

func (e Engine) ExecuteTask(run *domain.Run, task domain.Task, ctx orchcontext.Snapshot) (adapters.Result, error) {
	if run == nil { return adapters.Result{}, fmt.Errorf("run is nil") }
	if run.State != string(lifecycle.RUNNING) && run.State != string(lifecycle.READY) { return adapters.Result{}, fmt.Errorf("run %s is not executable in state %s", run.ID, run.State) }
	if e.Executor == nil || e.Policy == nil { return adapters.Result{}, fmt.Errorf("engine dependencies are incomplete") }

	agent, err := router.Match(router.Task{Role: task.Role, Capabilities: task.RequiredCapabilities}, e.Agents)
	if err != nil { return adapters.Result{}, err }
	for _, permission := range task.RequiredCapabilities {
		decision := e.Policy.Authorize(policy.Request{Principal: agent.ID, ProjectID: task.ProjectID, Capability: permission, Permission: permission})
		if decision == policy.Deny { return adapters.Result{}, fmt.Errorf("policy denied capability %q", permission) }
		if decision == policy.ApprovalRequired || task.ApprovalRequired { return adapters.Result{}, fmt.Errorf("human approval required for task %s", task.ID) }
	}
	result, err := e.Executor.Execute(adapters.Task{ID: task.ID, Role: task.Role, Capabilities: task.RequiredCapabilities, Objective: task.Objective}, adapters.Agent{ID: agent.ID, Role: agent.Role, Capabilities: agent.Capabilities, Permissions: agent.Permissions})
	if err != nil { return adapters.Result{}, err }
	for _, a := range result.Artifacts {
		if !gates.Evaluate(gates.Gate{ID: "artifact-valid", Required: true}, gates.Evaluation{GateID: "artifact-valid", Result: resultFor(a.Valid)}) { return adapters.Result{}, fmt.Errorf("artifact %s failed required gate", a.ID) }
	}
	_ = ctx
	return result, nil
}

func resultFor(valid bool) gates.Result { if valid { return gates.Passed }; return gates.Failed }
