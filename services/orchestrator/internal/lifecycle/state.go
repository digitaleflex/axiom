package lifecycle

import "fmt"

type RunState string

const (
	RunCreated RunState = "CREATED"
	RunPlanning RunState = "PLANNING"
	RunReady RunState = "READY"
	RunRunning RunState = "RUNNING"
	RunWaitingGate RunState = "WAITING_GATE"
	RunWaitingApproval RunState = "WAITING_APPROVAL"
	RunBlocked RunState = "BLOCKED"
	RunFailed RunState = "FAILED"
	RunRetrying RunState = "RETRYING"
	RunRevisionRequired RunState = "REVISION_REQUIRED"
	RunCompleted RunState = "COMPLETED"
	RunCancelled RunState = "CANCELLED"
)

func CanTransition(from, to RunState) bool {
	switch from {
	case RunCreated:
		return to == RunPlanning
	case RunPlanning:
		return to == RunReady || to == RunBlocked || to == RunFailed
	case RunReady:
		return to == RunRunning || to == RunCancelled
	case RunRunning:
		return to == RunWaitingGate || to == RunWaitingApproval || to == RunBlocked || to == RunFailed || to == RunCompleted || to == RunCancelled
	case RunWaitingGate:
		return to == RunRunning || to == RunRevisionRequired || to == RunFailed
	case RunWaitingApproval:
		return to == RunRunning || to == RunCancelled
	case RunBlocked:
		return to == RunRunning || to == RunCancelled
	case RunFailed:
		return to == RunRetrying || to == RunCancelled
	case RunRetrying:
		return to == RunRunning || to == RunFailed
	case RunRevisionRequired:
		return to == RunPlanning || to == RunRunning
	default:
		return false
	}
}

func ValidateTransition(from, to RunState) error {
	if !CanTransition(from, to) {
		return fmt.Errorf("invalid run state transition: %s -> %s", from, to)
	}
	return nil
}
