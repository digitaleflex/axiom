package orchestrator

import (
	"fmt"

	"github.com/digitaleflex/axiom/services/orchestrator/internal/domain"
	"github.com/digitaleflex/axiom/services/orchestrator/internal/lifecycle"
)

type MemoryTransitioner struct{}

func (MemoryTransitioner) Transition(run *domain.Run, to lifecycle.RunState) error {
	if run == nil {
		return fmt.Errorf("run is nil")
	}
	from := lifecycle.RunState(run.State)
	if err := lifecycle.ValidateTransition(from, to); err != nil {
		return err
	}
	run.State = string(to)
	return nil
}
