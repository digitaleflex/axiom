package orchestrator

import (
	"fmt"

	orchcontext "github.com/digitaleflex/axiom/services/orchestrator/internal/context"
	"github.com/digitaleflex/axiom/services/orchestrator/internal/domain"
	"github.com/digitaleflex/axiom/services/orchestrator/internal/lifecycle"
	"github.com/digitaleflex/axiom/services/orchestrator/internal/planner"
	"github.com/digitaleflex/axiom/services/orchestrator/internal/router"
)

// RunEngine coordinates the execution loop for a single orchestration run.
// Persistence is deliberately injected so the execution policy remains
// independent from a database or queue implementation.
type RunEngine struct {
	Engine    Engine
	Transition Transitioner
}

type Transitioner interface {
	Transition(run *domain.Run, to lifecycle.RunState) error
}

func (e RunEngine) Execute(run *domain.Run, tasks []domain.Task, ctx orchcontext.Snapshot) error {
	if run == nil {
		return fmt.Errorf("run is nil")
	}
	if e.Transition == nil {
		return fmt.Errorf("transitioner is nil")
	}
	if err := e.Transition.Transition(run, lifecycle.RunPlanning); err != nil {
		return err
	}

	plannerTasks := make([]planner.Task, 0, len(tasks))
	for _, task := range tasks {
		plannerTasks = append(plannerTasks, planner.Task{ID: task.ID, Dependencies: task.Dependencies, Status: task.Status})
	}
	if _, err := planner.ReadyTasks(plannerTasks); err != nil {
		_ = e.Transition.Transition(run, lifecycle.RunFailed)
		return err
	}
	if err := e.Transition.Transition(run, lifecycle.RunReady); err != nil {
		return err
	}

	for {
		plannerTasks = plannerTasks[:0]
		for _, task := range tasks {
			plannerTasks = append(plannerTasks, planner.Task{ID: task.ID, Dependencies: task.Dependencies, Status: task.Status})
		}
		ready, err := planner.ReadyTasks(plannerTasks)
		if err != nil {
			_ = e.Transition.Transition(run, lifecycle.RunFailed)
			return err
		}
		if len(ready) == 0 {
			for _, task := range tasks {
				if task.Status != "COMPLETED" {
					return fmt.Errorf("run blocked: no ready tasks remain")
				}
			}
			return e.Transition.Transition(run, lifecycle.RunCompleted)
		}

		if err := e.Transition.Transition(run, lifecycle.RunRunning); err != nil {
			return err
		}
		for _, readyTask := range ready {
			idx := indexTask(tasks, readyTask.ID)
			if idx < 0 {
				return fmt.Errorf("planned task disappeared: %s", readyTask.ID)
			}
			if _, err := e.Engine.ExecuteTask(run, tasks[idx], ctx); err != nil {
				_ = e.Transition.Transition(run, lifecycle.RunFailed)
				return err
			}
			tasks[idx].Status = "COMPLETED"
		}
	}
}

func indexTask(tasks []domain.Task, id string) int {
	for i := range tasks {
		if tasks[i].ID == id {
			return i
		}
	}
	return -1
}

// Keep router imported here as part of the orchestration boundary contract.
var _ = router.Agent{}
