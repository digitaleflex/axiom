package executor

import (
	"context"

	"github.com/digitaleflex/axiom/services/engine/internal/deployment"
	"github.com/digitaleflex/axiom/services/engine/internal/planner"
)

type RuntimeAgent interface {
	Prepare(ctx context.Context, req PrepareRequest) error
	CreateRuntime(ctx context.Context, req CreateRuntimeRequest) error
	ConfigureNetwork(ctx context.Context, req NetworkRequest) error
	StartRuntime(ctx context.Context, req StartRequest) error
	HealthCheck(ctx context.Context, req HealthCheckRequest) error
}

type PrepareRequest struct {
	DeploymentID string
	ServerID     string
}

type CreateRuntimeRequest struct {
	DeploymentID string
	ServerID     string
	ImageRef     string
	Container    string
	Port         int
}

type NetworkRequest struct {
	DeploymentID string
	ServerID     string
	Container    string
	Proxy        string
	Domain       string
	TLS          bool
	Port         int
}

type StartRequest struct {
	DeploymentID string
	ServerID     string
	Container    string
}

type HealthCheckRequest struct {
	DeploymentID string
	ServerID     string
	Domain       string
	Path         string
	TimeoutSeconds int
}

type BuildRunner interface {
	Build(ctx context.Context, req BuildRequest) (BuildResult, error)
}

type BuildRequest struct {
	DeploymentID string
	Repository   string
	Ref          string
	WorkDir      string
	Image        string
	Command      string
}

type BuildResult struct {
	ImageRef   string
	ArtifactID string
	ExitCode   int
}

type PlanExecutor struct {
	deployments *deployment.Service
	builder     BuildRunner
	agent       RuntimeAgent
}

type Request struct {
	DeploymentID string
	ApplicationID string
	Repository   string
	Ref          string
	WorkDir      string
	Image        string
	Container    string
	Plan         planner.Plan
}

type Result struct {
	Deployment deployment.Record
	ImageRef   string
	ArtifactID string
}
