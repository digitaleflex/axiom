package gates

type Result string

const (
	Passed Result = "PASSED"
	Failed Result = "FAILED"
	Pending Result = "PENDING"
)

type Gate struct { ID string; Required bool }
type Evaluation struct { GateID string; Result Result; Reason string }

func Evaluate(g Gate, e Evaluation) bool {
	if !g.Required { return true }
	return e.GateID == g.ID && e.Result == Passed
}