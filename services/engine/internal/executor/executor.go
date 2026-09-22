package executor

import (
	"context"
	"fmt"

	"github.com/digitaleflex/axiom/services/engine/internal/deployment"
)

func New(deployments *deployment.Service, builder BuildRunner, agent RuntimeAgent) *PlanExecutor {
	return &PlanExecutor{deployments: deployments, builder: builder, agent: agent}
}

func (e *PlanExecutor) Execute(ctx context.Context, req Request) (Result, error) {
	if e.deployments == nil {
		return Result{}, fmt.Errorf("deployment service is required")
	}
	if e.builder == nil {
		return Result{}, fmt.Errorf("build runner is required")
	}
	if e.agent == nil {
		return Result{}, fmt.Errorf("runtime agent is required")
	}
	if req.DeploymentID == "" || req.Plan.ID == "" || req.Plan.ServerID == "" {
		return Result{}, fmt.Errorf("deploymentID, plan ID and server ID are required")
	}

	record, err := e.deployments.Transition(ctx, req.DeploymentID, deployment.StateAnalyzing)
	if err != nil {
		return Result{}, err
	}

	if err := e.runStep(ctx, record, "PREPARE", func() error {
		return e.agent.Prepare(ctx, PrepareRequest{DeploymentID: req.DeploymentID, ServerID: req.Plan.ServerID})
	}); err != nil {
		return e.fail(ctx, req.DeploymentID, err)
	}

	record, err = e.deployments.Transition(ctx, req.DeploymentID, deployment.StatePlanning)
	if err != nil {
		return Result{}, err
	}

	if err := e.runStep(ctx, record, "BUILD", func() error {
		return nil
	}); err != nil {
		return e.fail(ctx, req.DeploymentID, err)
	}

	record, err = e.deployments.Transition(ctx, req.DeploymentID, deployment.StateBuilding)
	if err != nil {
		return Result{}, err
	}

	build, err := e.builder.Build(ctx, BuildRequest{
		DeploymentID: req.DeploymentID,
		Repository: req.Repository,
		Ref: req.Ref,
		WorkDir: req.WorkDir,
		Image: req.Image,
		Command: req.Plan.Build.Command,
	})
	if err != nil {
		return e.fail(ctx, req.DeploymentID, fmt.Errorf("build: %w", err))
	}

	record, err = e.deployments.Transition(ctx, req.DeploymentID, deployment.StateDeploying)
	if err != nil {
		return Result{}, err
	}

	if err := e.runStep(ctx, record, "CREATE_RUNTIME", func() error {
		return e.agent.CreateRuntime(ctx, CreateRuntimeRequest{
			DeploymentID: req.DeploymentID,
			ServerID: req.Plan.ServerID,
			ImageRef: build.ImageRef,
			Container: req.Container,
			Port: req.Plan.Runtime.Port,
		})
	}); err != nil {
		return e.fail(ctx, req.DeploymentID, err)
	}

	if err := e.runStep(ctx, record, "NETWORK", func() error {
		return e.agent.ConfigureNetwork(ctx, NetworkRequest{
			DeploymentID: req.DeploymentID,
			ServerID: req.Plan.ServerID,
			Container: req.Container,
			Proxy: req.Plan.Network.Proxy,
			Domain: req.Plan.Network.Domain,
			TLS: req.Plan.Network.TLS,
			Port: req.Plan.Network.ExposedPort,
		})
	}); err != nil {
		return e.fail(ctx, req.DeploymentID, err)
	}

	if err := e.runStep(ctx, record, "START", func() error {
		return e.agent.StartRuntime(ctx, StartRequest{
			DeploymentID: req.DeploymentID,
			ServerID: req.Plan.ServerID,
			Container: req.Container,
		})
	}); err != nil {
		return e.fail(ctx, req.DeploymentID, err)
	}

	record, err = e.deployments.Transition(ctx, req.DeploymentID, deployment.StateVerifying)
	if err != nil {
		return Result{}, err
	}

	if err := e.runStep(ctx, record, "VERIFY", func() error {
		return e.agent.HealthCheck(ctx, HealthCheckRequest{
			DeploymentID: req.DeploymentID,
			ServerID: req.Plan.ServerID,
			Domain: req.Plan.Network.Domain,
			Path: req.Plan.Health.Path,
			TimeoutSeconds: req.Plan.Health.TimeoutSeconds,
		})
	}); err != nil {
		return e.fail(ctx, req.DeploymentID, err)
	}

	record, err = e.deployments.Transition(ctx, req.DeploymentID, deployment.StateLive)
	if err != nil {
		return Result{}, err
	}

	return Result{Deployment: record, ImageRef: build.ImageRef, ArtifactID: build.ArtifactID}, nil
}

func (e *PlanExecutor) runStep(ctx context.Context, record deployment.Record, step string, fn func() error) error {
	e.publishStep(record, step, "started", nil)
	if err := fn(); err != nil {
		e.publishStep(record, step, "failed", err.Error())
		return fmt.Errorf("%s: %w", step, err)
	}
	e.publishStep(record, step, "completed", nil)
	return nil
}

func (e *PlanExecutor) fail(ctx context.Context, id string, cause error) (Result, error) {
	_, transitionErr := e.deployments.Transition(ctx, id, deployment.StateFailed)
	if transitionErr != nil {
		return Result{}, fmt.Errorf("%w; marking deployment failed: %v", cause, transitionErr)
	}
	return Result{}, cause
}

func (e *PlanExecutor) publishStep(record deployment.Record, step, status string, message any) {
	e.deployments.Events().Publish(deployment.Event{
		ID:           "step_" + record.ID + "_" + step + "_" + status,
		Type:         "deployment.step",
		Version:      1,
		DeploymentID: record.ID,
		Data: map[string]any{
			"step":    step,
			"status":  status,
			"message": message,
		},
	})
}
