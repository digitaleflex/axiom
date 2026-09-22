package build

import (
	"context"
	"fmt"
)

type Source interface {
	Checkout(ctx context.Context, repository, ref, destination string) error
}

type Runner interface {
	BuildImage(ctx context.Context, workDir, image string) (exitCode int, err error)
}

type Logger interface {
	Log(ctx context.Context, event LogEvent)
}

type Engine struct {
	source Source
	runner Runner
	logger Logger
}

func New(source Source, runner Runner, logger Logger) *Engine {
	return &Engine{source: source, runner: runner, logger: logger}
}

func (e *Engine) Build(ctx context.Context, req Request) (Result, error) {
	if req.DeploymentID == "" || req.Repository == "" || req.Ref == "" || req.WorkDir == "" || req.Image == "" {
		return Result{}, fmt.Errorf("deploymentID, repository, ref, workDir and image are required")
	}
	if e.source == nil || e.runner == nil {
		return Result{}, fmt.Errorf("build engine dependencies are not configured")
	}
	if e.logger != nil {
		e.logger.Log(ctx, LogEvent{DeploymentID: req.DeploymentID, Level: "INFO", Step: "BUILD", Message: "checking out source"})
	}
	if err := e.source.Checkout(ctx, req.Repository, req.Ref, req.WorkDir); err != nil {
		return Result{}, fmt.Errorf("source checkout: %w", err)
	}
	if e.logger != nil {
		e.logger.Log(ctx, LogEvent{DeploymentID: req.DeploymentID, Level: "INFO", Step: "BUILD", Message: "building image"})
	}
	exitCode, err := e.runner.BuildImage(ctx, req.WorkDir, req.Image)
	if err != nil {
		return Result{ExitCode: exitCode}, fmt.Errorf("image build: %w", err)
	}
	if exitCode != 0 {
		return Result{ExitCode: exitCode}, fmt.Errorf("image build exited with code %d", exitCode)
	}
	return Result{ImageRef: req.Image, ExitCode: 0, ArtifactID: "artifact:" + req.Image}, nil
}
