package executor

import (
	"context"
	"log/slog"
	"time"

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

// Operation carries the envelope every bounded agent operation needs:
// an explicit type (the method), a deterministic idempotency key and the
// correlation ID tracing the operation to its API request (#75, #80).
type Operation struct {
	OperationID   string
	CorrelationID string
	DeploymentID  string
	ServerID      string
}

type CreateRuntimeRequest struct {
	Operation
	ImageRef  string
	Container string
	Port      int
}

type NetworkRequest struct {
	Operation
	Container string
	Proxy     string
	Domain    string
	TLS       bool
	Port      int
}

type StartRequest struct {
	Operation
	Container string
}

type HealthCheckRequest struct {
	Operation
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
	// Log receives step retry and execution events (structured, no secrets).
	Log *slog.Logger
	// Backoff waits between attempts; nil disables waiting.
	Backoff func(attempt int) time.Duration
	// Timeouts overrides per-step attempt timeouts ("" disables).
	Timeouts map[string]time.Duration
}

type Request struct {
	DeploymentID string
	// CorrelationID traces the execution to its API request; generated when empty.
	CorrelationID string
	AppSlug       string
	Container     string
	Source        build.Source
	Plan          planner.Plan
}

type Result struct {
	Deployment deployment.Record
	ImageRef   string
	ArtifactID string
	Artifact   build.Artifact
}
