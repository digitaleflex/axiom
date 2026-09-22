package deployment

import "fmt"

type State string

const (
	StatePending State = "PENDING"
	StatePlanning State = "PLANNING"
	StateBuilding State = "BUILDING"
	StateDeploying State = "DEPLOYING"
	StateVerifying State = "VERIFYING"
	StateLive State = "LIVE"
	StateFailed State = "FAILED"
	StateCancelled State = "CANCELLED"
)

func (s State) Terminal() bool {
	return s == StateLive || s == StateFailed || s == StateCancelled
}

func CanTransition(from, to State) bool {
	switch from {
	case StatePending:
		return to == StatePlanning || to == StateCancelled
	case StatePlanning:
		return to == StateBuilding || to == StateFailed || to == StateCancelled
	case StateBuilding:
		return to == StateDeploying || to == StateFailed || to == StateCancelled
	case StateDeploying:
		return to == StateVerifying || to == StateFailed || to == StateCancelled
	case StateVerifying:
		return to == StateLive || to == StateFailed || to == StateCancelled
	default:
		return false
	}
}

func Transition(from, to State) error {
	if from == to {
		return fmt.Errorf("invalid deployment transition: %s -> %s", from, to)
	}
	if !CanTransition(from, to) {
		return fmt.Errorf("invalid deployment transition: %s -> %s", from, to)
	}
	return nil
}
