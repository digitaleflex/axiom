package build

type Request struct {
	DeploymentID string
	Repository   string
	Ref          string
	WorkDir      string
	Image        string
	Command      string
}

type Result struct {
	ImageRef   string
	ExitCode   int
	ArtifactID string
}

type LogEvent struct {
	DeploymentID string
	Level        string
	Message      string
	Step         string
}
