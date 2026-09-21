package adapters

type Agent struct {
	ID string
	Role string
	Capabilities map[string]bool
	Permissions map[string]bool
}

type Executor interface {
	Execute(task Task, agent Agent) (Result, error)
}

type Task struct {
	ID string
	Role string
	Capabilities []string
	Permissions []string
	Objective string
}

type Result struct {
	ExecutionID string
	Artifacts []Artifact
}

type Artifact struct {
	ID string
	Type string
	Version string
	Valid bool
}
