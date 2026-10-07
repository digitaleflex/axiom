package executor

import (
	"context"
	"errors"
	"fmt"

	"github.com/digitaleflex/axiom/services/engine/internal/deployment"
)

// Error codes recorded on failure (docs/architecture/api-contract.md §18).
const (
	ErrorBuildFailed       = "BUILD_FAILED"
	ErrorRuntimeFailed     = "RUNTIME_FAILED"
	ErrorHealthCheckFailed = "HEALTH_CHECK_FAILED"
)

func New(deployments *deployment.Service, builder BuildRunner, agent RuntimeAgent) *PlanExecutor {
	return &PlanExecutor{deployments: deployments, builder: builder, agent: agent}
}

// Execute runs a plan for an existing PENDING deployment. Steps and state
// transitions are persisted through the deployment service; LIVE is recorded
// only after the VERIFY step succeeds.
func (e *PlanExecutor) Execute(ctx context.Context, req Request) (Result, error) {
	if e.deployments == nil || e.builder == nil || e.agent == nil {
		return Result{}, errors.New("deployment service, build runner and runtime agent are required")
	}
	if req.DeploymentID == "" || req.Plan.ID == "" || req.Plan.ServerID == "" {
		return Result{}, errors.New("deploymentID, plan ID and server ID are required")
	}
	id := req.DeploymentID

	for _, s := range []deployment.State{deployment.StateAnalyzing, deployment.StatePlanning, deployment.StateBuilding} {
		if _, err := e.deployments.Transition(ctx, id, s); err != nil {
			return Result{}, err
		}
	}

	var build BuildResult
	if err := e.step(ctx, id, "BUILD", func() error {
		var err error
		build, err = e.builder.Build(ctx, BuildRequest{
			DeploymentID: id, Repository: req.Repository, Ref: req.Ref, WorkDir: req.WorkDir,
			Image: req.Image, Command: req.Plan.Build.Command,
		})
		return err
	}); err != nil {
		return e.fail(ctx, id, ErrorBuildFailed, err)
	}

	if _, err := e.deployments.Transition(ctx, id, deployment.StateDeploying); err != nil {
		return Result{}, err
	}
	runtimeSteps := []struct {
		name string
		run  func() error
	}{
		{"CREATE_RUNTIME", func() error {
			return e.agent.CreateRuntime(ctx, CreateRuntimeRequest{DeploymentID: id, ServerID: req.Plan.ServerID, ImageRef: build.ImageRef, Container: req.Container, Port: req.Plan.Runtime.Port})
		}},
		{"NETWORK", func() error {
			return e.agent.ConfigureNetwork(ctx, NetworkRequest{DeploymentID: id, ServerID: req.Plan.ServerID, Container: req.Container, Proxy: req.Plan.Network.Proxy, Domain: req.Plan.Network.Domain, TLS: req.Plan.Network.TLS, Port: req.Plan.Network.ExposedPort})
		}},
		{"START", func() error {
			return e.agent.StartRuntime(ctx, StartRequest{DeploymentID: id, ServerID: req.Plan.ServerID, Container: req.Container})
		}},
	}
	for _, s := range runtimeSteps {
		if err := e.step(ctx, id, s.name, s.run); err != nil {
			return e.fail(ctx, id, ErrorRuntimeFailed, err)
		}
	}

	if _, err := e.deployments.Transition(ctx, id, deployment.StateVerifying); err != nil {
		return Result{}, err
	}
	if err := e.step(ctx, id, "VERIFY", func() error {
		return e.agent.HealthCheck(ctx, HealthCheckRequest{DeploymentID: id, ServerID: req.Plan.ServerID, Domain: req.Plan.Network.Domain, Path: req.Plan.Health.Path, TimeoutSeconds: req.Plan.Health.TimeoutSeconds})
	}); err != nil {
		return e.fail(ctx, id, ErrorHealthCheckFailed, err)
	}

	url := ""
	if req.Plan.Network.Domain != "" {
		scheme := "http"
		if req.Plan.Network.TLS {
			scheme = "https"
		}
		url = scheme + "://" + req.Plan.Network.Domain
	}
	record, err := e.deployments.MarkLive(ctx, id, url)
	if err != nil {
		return Result{}, err
	}
	return Result{Deployment: record, ImageRef: build.ImageRef, ArtifactID: build.ArtifactID}, nil
}

// step records RUNNING, runs fn, then records COMPLETED or FAILED.
func (e *PlanExecutor) step(ctx context.Context, id, name string, fn func() error) error {
	if err := e.deployments.RecordStep(ctx, id, deployment.StepChange{Name: name, Status: deployment.StepRunning}); err != nil {
		return fmt.Errorf("%s: record start: %w", name, err)
	}
	if err := fn(); err != nil {
		_ = e.deployments.RecordStep(ctx, id, deployment.StepChange{Name: name, Status: deployment.StepFailed})
		return fmt.Errorf("%s: %w", name, err)
	}
	return e.deployments.RecordStep(ctx, id, deployment.StepChange{Name: name, Status: deployment.StepCompleted})
}

func (e *PlanExecutor) fail(ctx context.Context, id, code string, cause error) (Result, error) {
	if _, err := e.deployments.Fail(ctx, id, code); err != nil {
		return Result{}, fmt.Errorf("%w; marking deployment failed: %v", cause, err)
	}
	return Result{}, cause
}
