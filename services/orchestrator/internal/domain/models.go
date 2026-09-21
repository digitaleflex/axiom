package domain

type Run struct {
	ID        string
	ProjectID string
	Objective string
	State     string
}

type Task struct {
	ID               string
	ProjectID        string
	Role             string
	Objective        string
	Dependencies     []string
	RequiredArtifacts []string
	RequiredCapabilities []string
	ApprovalRequired bool
	Status           string
}

type ArtifactRef struct {
	ID      string
	Type    string
	Version string
}

type Execution struct {
	ID            string
	TaskID        string
	IdempotencyKey string
	Attempt       int
}
