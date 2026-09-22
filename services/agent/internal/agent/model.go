package agent

import "context"

type Config struct {
	ServerID  string
	EngineURL string
	Version   string
	Token     string
}

type Registration struct {
	ServerID      string
	AgentVersion  string
	Capabilities  []string
	CPUCount      int
	MemoryMB      int
	DiskFreeMB    int
}

type Operation struct {
	ID           string
	Type         string
	DeploymentID string
	ServerID     string
	Payload      map[string]any
}

type Result struct {
	OperationID  string
	DeploymentID string
	Success      bool
	Message      string
}

type Runtime interface {
	Capabilities(ctx context.Context) ([]string, error)
	Prepare(ctx context.Context, deploymentID string) error
	CreateRuntime(ctx context.Context, deploymentID, image, container string, port int) error
	ConfigureNetwork(ctx context.Context, deploymentID, container, proxy, domain string, tls bool, port int) error
	StartRuntime(ctx context.Context, deploymentID, container string) error
	HealthCheck(ctx context.Context, deploymentID, domain, path string, timeoutSeconds int) error
}
