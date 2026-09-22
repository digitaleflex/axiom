package planner

import "github.com/digitaleflex/axiom/services/engine/internal/profile"

type ApplicationProfile = profile.ApplicationProfile

type ServerProfile struct {
	ID           string
	Name         string
	Address      string
	Status       string
	AgentVersion string
	Capabilities []string
}

type Plan struct {
	ID                       string
	ApplicationProfileVersion int
	ServerID                 string
	Strategy                 string
	Build                    BuildPlan
	Runtime                  RuntimePlan
	Network                  NetworkPlan
	Health                   HealthPlan
	Rollback                 RollbackPlan
	Steps                    []Step
}

type BuildPlan struct {
	Strategy       string
	PackageManager string
	Command        string
}

type RuntimePlan struct {
	Type         string
	StartCommand string
	Port         int
}

type NetworkPlan struct {
	Proxy       string
	Domain      string
	TLS         bool
	ExposedPort int
}

type HealthPlan struct {
	Type           string
	Path           string
	TimeoutSeconds int
}

type RollbackPlan struct {
	Strategy string
}

type Step struct {
	Name  string
	Order int
}
