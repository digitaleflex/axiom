package server

import "context"

type Status string

const (
	StatusUnknown  Status = "unknown"
	StatusReady    Status = "ready"
	StatusDegraded Status = "degraded"
	StatusOffline  Status = "offline"
)

type Capability string

const (
	CapabilityDocker  Capability = "docker"
	CapabilityCompose Capability = "docker_compose"
	CapabilityTraefik Capability = "traefik"
	CapabilityTLS     Capability = "tls"
)

type Record struct {
	ID           string
	Name         string
	Address      string
	Status       Status
	AgentVersion string
	Capabilities []Capability
	CPUCount     int
	MemoryMB     int
	DiskFreeMB   int
	LastSeenAt   string
}

type Repository interface {
	Get(ctx context.Context, id string) (Record, error)
	UpdateHealth(ctx context.Context, id string, health Health) error
}

type Health struct {
	Status       Status
	AgentVersion string
	Capabilities []Capability
	CPUCount     int
	MemoryMB     int
	DiskFreeMB   int
	LastSeenAt   string
}

type EligibilityRequest struct {
	RequiredCapabilities []Capability
}

type EligibilityResult struct {
	Eligible bool
	Reasons  []string
}
