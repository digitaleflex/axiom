// Package execution defines the provider-neutral boundary between Axiom's
// deployment planner and an external runtime platform.
package execution

import (
	"context"
	"time"
)

// ApplicationSpec is the resolved runtime configuration Axiom is allowed to
// pass to an execution provider. Secrets are deliberately excluded; they must
// be provisioned through the provider's dedicated secret/configuration API.
type ApplicationSpec struct {
	Name          string
	Description   string
	Image         string
	ImageTag      string
	Port          int
	Domain        string
	HealthPath    string
	ProjectID     string
	ServerID      string
	Environment   string
	DestinationID string
	MemoryLimit   string
	CPULimit      string
}

// Application is a provider resource linked to an Axiom application.
type Application struct {
	Provider   string
	ProviderID string
	Name       string
	URL        string
	RawStatus  string
}

// Deployment is a provider-side deployment operation.
type Deployment struct {
	Provider      string
	ProviderID    string
	ApplicationID string
	Status        Status
	RawStatus     string
	URL           string
	Message       string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Status is Axiom's normalized view of provider progress. SUCCEEDED is not
// LIVE: Axiom must run its own external health check before declaring LIVE.
type Status string

const (
	StatusQueued    Status = "QUEUED"
	StatusBuilding  Status = "BUILDING"
	StatusDeploying Status = "DEPLOYING"
	StatusSucceeded Status = "SUCCEEDED"
	StatusFailed    Status = "FAILED"
	StatusCancelled Status = "CANCELLED"
	StatusUnknown   Status = "UNKNOWN"
)

// Adapter is the minimal runtime-provider contract required by the first
// Coolify-backed deployment slice. Implementations must be idempotent where
// possible and must never expose provider credentials in errors or logs.
type Adapter interface {
	CreateApplication(ctx context.Context, spec ApplicationSpec) (Application, error)
	Deploy(ctx context.Context, applicationID, image, tag string) (Deployment, error)
	GetDeployment(ctx context.Context, deploymentID string) (Deployment, error)
	GetApplication(ctx context.Context, applicationID string) (Application, error)
}
