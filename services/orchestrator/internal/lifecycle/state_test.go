package lifecycle

import "testing"

func TestCanTransition(t *testing.T) {
	valid := [][2]RunState{
		{RunCreated, RunPlanning},
		{RunPlanning, RunReady},
		{RunReady, RunRunning},
		{RunRunning, RunWaitingGate},
		{RunWaitingGate, RunRevisionRequired},
		{RunFailed, RunRetrying},
		{RunRetrying, RunRunning},
	}
	for _, tc := range valid {
		if err := ValidateTransition(tc[0], tc[1]); err != nil {
			t.Fatalf("expected %s -> %s to be valid: %v", tc[0], tc[1], err)
		}
	}
}

func TestRejectsInvalidTransition(t *testing.T) {
	if err := ValidateTransition(RunCompleted, RunRunning); err == nil {
		t.Fatal("expected terminal state transition to be rejected")
	}
}
