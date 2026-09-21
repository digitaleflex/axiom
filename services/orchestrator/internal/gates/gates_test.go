package gates

import "testing"

func TestRequiredGate(t *testing.T) {
	if !Evaluate(Gate{ID:"security", Required:true}, Evaluation{GateID:"security", Result:Passed}) { t.Fatal("expected gate to pass") }
	if Evaluate(Gate{ID:"security", Required:true}, Evaluation{GateID:"security", Result:Failed}) { t.Fatal("expected gate to fail") }
}