package deployment

import "fmt"

// State is the authoritative deployment status (docs/architecture/api-contract.md §12).
type State string

const (
	StatePending   State = "PENDING"
	StateAnalyzing State = "ANALYZING"
	StatePlanning  State = "PLANNING"
	StateBuilding  State = "BUILDING"
	StateDeploying State = "DEPLOYING"
	StateVerifying State = "VERIFYING"
	StateLive      State = "LIVE"
	StateFailed    State = "FAILED"
	StateCancelled State = "CANCELLED"
)

// next lists the forward progression of the canonical state machine.
var next = map[State]State{
	StatePending:   StateAnalyzing,
	StateAnalyzing: StatePlanning,
	StatePlanning:  StateBuilding,
	StateBuilding:  StateDeploying,
	StateDeploying: StateVerifying,
	StateVerifying: StateLive,
}

// Valid reports whether s is a known deployment state.
func (s State) Valid() bool {
	switch s {
	case StatePending, StateAnalyzing, StatePlanning, StateBuilding, StateDeploying,
		StateVerifying, StateLive, StateFailed, StateCancelled:
		return true
	}
	return false
}

// Terminal reports whether no further transition is possible.
func (s State) Terminal() bool {
	return s == StateLive || s == StateFailed || s == StateCancelled
}

// Cancellable reports whether a cancel request is permitted in state s.
// VERIFYING is not cancellable: the outcome is decided by health verification.
func (s State) Cancellable() bool {
	return !s.Terminal() && s != StateVerifying
}

// CanTransition reports whether from -> to is allowed.
// Forward moves follow the canonical order; any non-terminal state may fail;
// cancellable states may be cancelled. LIVE is reachable only from VERIFYING.
func CanTransition(from, to State) bool {
	if from.Terminal() || from == to {
		return false
	}
	switch to {
	case StateFailed:
		return true
	case StateCancelled:
		return from.Cancellable()
	default:
		return next[from] == to
	}
}

// Transition returns an error when from -> to is not allowed.
func Transition(from, to State) error {
	if !CanTransition(from, to) {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, from, to)
	}
	return nil
}
