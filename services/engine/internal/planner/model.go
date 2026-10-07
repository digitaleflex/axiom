package planner

import "github.com/digitaleflex/axiom/services/engine/internal/profile"

// ApplicationProfile is the canonical profile consumed by the planner (#95).
type ApplicationProfile = profile.Profile

// SchemaVersion of the serialized plan.
const SchemaVersion = 1

// Canonical plan steps in execution order (API contract §10).
var CanonicalSteps = []string{"BUILD", "CREATE_RUNTIME", "NETWORK", "START", "VERIFY"}

// ServerProfile is the planner's view of a target server.
type ServerProfile struct {
	ID           string
	Name         string
	Address      string
	Status       string // READY, DEGRADED, OFFLINE…
	AgentVersion string
	Capabilities []string
	MemoryMB     int
	DiskFreeMB   int
}

// Request is everything a plan is derived from. Identical requests yield
// identical plans (same fingerprint), independent of time or IDs.
type Request struct {
	ApplicationID string
	Profile       ApplicationProfile
	Server        ServerProfile
	Environment   string // production | staging | preview
	Domain        string
}

// Plan is an immutable, reviewable deployment plan.
type Plan struct {
	ID                        string            `json:"id"`
	SchemaVersion             int               `json:"schemaVersion"`
	Status                    string            `json:"status"`
	ApplicationID             string            `json:"applicationId"`
	ApplicationProfileVersion int               `json:"applicationProfileVersion"`
	AnalysisID                string            `json:"analysisId,omitempty"`
	Source                    profile.Source    `json:"source"`
	ServerID                  string            `json:"serverId"`
	Environment               string            `json:"environment"`
	Strategy                  string            `json:"strategy"` // preset
	Build                     BuildPlan         `json:"build"`
	Runtime                   RuntimePlan       `json:"runtime"`
	Network                   NetworkPlan       `json:"network"`
	Health                    HealthPlan        `json:"healthCheck"`
	Rollback                  RollbackPlan      `json:"rollback"`
	Steps                     []string          `json:"steps"`
	FailureBoundaries         map[string]string `json:"failureBoundaries"`
	Fingerprint               string            `json:"fingerprint"`
}

type BuildPlan struct {
	Strategy       string `json:"strategy"` // source | dockerfile | compose
	PackageManager string `json:"packageManager,omitempty"`
	Command        string `json:"command,omitempty"`
	OutputDir      string `json:"outputDir,omitempty"`
}

type RuntimePlan struct {
	Type          string   `json:"type"` // node | static | go | image | compose
	StartCommand  string   `json:"startCommand,omitempty"`
	Port          int      `json:"port"`
	Configuration []string `json:"configuration"` // variable names only
	Services      []string `json:"services,omitempty"`
	// DependencyOrder is the topological order of Services (compose only,
	// dependencies first). Empty for non-compose plans.
	DependencyOrder []string `json:"dependencyOrder,omitempty"`
}

type NetworkPlan struct {
	Proxy         string `json:"proxy"`
	Domain        string `json:"domain"`
	TLS           bool   `json:"tls"`
	ExposedPort   int    `json:"exposedPort"`
	PublicService string `json:"publicService,omitempty"`
}

type HealthPlan struct {
	Type            string `json:"type"`
	Path            string `json:"path,omitempty"`
	ExpectedStatus  string `json:"expectedStatus"`
	TimeoutSeconds  int    `json:"timeoutSeconds"`
	IntervalSeconds int    `json:"intervalSeconds"`
	Retries         int    `json:"retries"`
}

type RollbackPlan struct {
	Strategy    string `json:"strategy"`
	Description string `json:"description"`
}
