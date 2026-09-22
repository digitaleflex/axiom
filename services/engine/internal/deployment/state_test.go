package deployment

import "testing"

func TestValidDeploymentTransitions(t *testing.T) {
	tests := [][2]State{
		{StatePending, StatePlanning},
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
