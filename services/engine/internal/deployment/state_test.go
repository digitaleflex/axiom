package deployment

import "testing"

func TestValidDeploymentTransitions(t *testing.T) {
	tests := [][2]State{
		{StatePending, StateAnalyzing},
		{StateAnalyzing, StatePlanning},
		{StatePlanning, StateBuilding},
		{StateBuilding, StateDeploying},
		{StateDeploying, StateVerifying},
		{StateVerifying, StateLive},
		{StateVerifying, StateFailed},
	}
	for _, tt := range tests {
		if err := Transition(tt[0], tt[1]); err != nil {
			t.Errorf("%s -> %s should be valid: %v", tt[0], tt[1], err)
		}
	}
}

func TestInvalidDeploymentTransitions(t *testing.T) {
	tests := [][2]State{
		{StatePending, StateLive},
		{StateLive, StateLive},
		{StateFailed, StateDeploying},
		{StateCancelled, StatePlanning},
	}
	for _, tt := range tests {
		if err := Transition(tt[0], tt[1]); err == nil {
			t.Errorf("%s -> %s should be rejected", tt[0], tt[1])
		}
	}
}

func TestFailureAndCancellationRules(t *testing.T) {
	for _, s := range []State{StatePending, StateAnalyzing, StatePlanning, StateBuilding, StateDeploying, StateVerifying} {
		if err := Transition(s, StateFailed); err != nil {
			t.Errorf("%s -> FAILED should be valid: %v", s, err)
		}
	}
	if err := Transition(StateBuilding, StateCancelled); err != nil {
		t.Errorf("BUILDING -> CANCELLED should be valid: %v", err)
	}
	if err := Transition(StateVerifying, StateCancelled); err == nil {
		t.Error("VERIFYING -> CANCELLED must be rejected: outcome belongs to health verification")
	}
	if err := Transition(StateDeploying, StateLive); err == nil {
		t.Error("LIVE must only be reachable from VERIFYING")
	}
	if !State("LIVE").Valid() || State("RUNNING").Valid() {
		t.Error("Valid() must accept canonical states only")
	}
}
