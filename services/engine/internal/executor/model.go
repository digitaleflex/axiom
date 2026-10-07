package executor

import (
	"context"

	"github.com/digitaleflex/axiom/services/engine/internal/build"
	"github.com/digitaleflex/axiom/services/engine/internal/deployment"
	"github.com/digitaleflex/axiom/services/engine/internal/planner"
)

type RuntimeAgent interface {
	CreateRuntime(ctx context.Context, req CreateRuntimeRequest) error
	ConfigureNetwork(ctx context.Context, req NetworkRequest) error
	StartRuntime(ctx context.Context, req StartRequest) error
	HealthCheck(ctx context.Context, req HealthCheckRequest) error
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
	DeploymentID   string
	ServerID       string
	Domain         string
	Path           string
	TimeoutSeconds int
}

// BuildRunner builds the deployment image. Implemented by build.Engine.
type BuildRunner interface {
	Build(ctx context.Context, in build.Input) (build.Result, error)
}

type PlanExecutor struct {
	deployments *deployment.Service
	builder     BuildRunner
	agent       RuntimeAgent
}

type Request struct {
	DeploymentID string
	AppSlug      string
	Container    string
	Source       build.Source
	Plan         planner.Plan
}

type Result struct {
	Deployment deployment.Record
	ImageRef   string
	ArtifactID string
	Artifact   build.Artifact
}
