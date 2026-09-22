package agent

import (
	"context"
	"fmt"
	"log/slog"
	"runtime"
)

type LocalRuntime struct {
	log *slog.Logger
}

func NewRuntime(log *slog.Logger) *LocalRuntime {
	if log == nil {
		log = slog.Default()
	}
	return &LocalRuntime{log: log}
}

func (r *LocalRuntime) Capabilities(context.Context) ([]string, error) {
	return []string{"docker"}, nil
}

func (r *LocalRuntime) Prepare(_ context.Context, deploymentID string) error {
	r.log.Info("prepare runtime", "deployment_id", deploymentID)
	return nil
}

func (r *LocalRuntime) CreateRuntime(_ context.Context, deploymentID, image, container string, port int) error {
	if image == "" || container == "" || port <= 0 {
		return fmt.Errorf("image, container and valid port are required")
	}
	r.log.Info("create runtime", "deployment_id", deploymentID, "image", image, "container", container, "port", port)
	return nil
}

func (r *LocalRuntime) ConfigureNetwork(_ context.Context, deploymentID, container, proxy, domain string, tls bool, port int) error {
	if container == "" || proxy == "" || domain == "" || port <= 0 {
		return fmt.Errorf("container, proxy, domain and valid port are required")
	}
	r.log.Info("configure network", "deployment_id", deploymentID, "container", container, "proxy", proxy, "domain", domain, "tls", tls, "port", port)
	return nil
}

func (r *LocalRuntime) StartRuntime(_ context.Context, deploymentID, container string) error {
	if container == "" {
		return fmt.Errorf("container is required")
	}
	r.log.Info("start runtime", "deployment_id", deploymentID, "container", container)
	return nil
}

func (r *LocalRuntime) HealthCheck(_ context.Context, deploymentID, domain, path string, timeoutSeconds int) error {
	if domain == "" || path == "" || timeoutSeconds <= 0 {
		return fmt.Errorf("domain, path and positive timeout are required")
	}
	r.log.Info("health check", "deployment_id", deploymentID, "domain", domain, "path", path, "timeout_seconds", timeoutSeconds)
	return nil
}

var _ = runtime.GOOS
