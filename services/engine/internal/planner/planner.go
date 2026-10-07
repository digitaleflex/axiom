// Package planner turns a ready Application Profile, an eligible server and
// deployment inputs into a deterministic, reviewable Deployment Plan (#60).
package planner

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/digitaleflex/axiom/services/engine/internal/planner/validation"
)

// Errors.
var (
	ErrProfileNotReady = errors.New("application profile is not ready for planning")
	ErrNotEligible     = errors.New("server is not eligible for this deployment")
)

// EligibilityError lists every failing requirement.
type EligibilityError struct{ Reasons []string }

func (e *EligibilityError) Error() string {
	return ErrNotEligible.Error() + ": " + strings.Join(e.Reasons, "; ")
}
func (e *EligibilityError) Unwrap() error { return ErrNotEligible }

// Engine generates plans.
type Engine struct{}

func New() *Engine { return &Engine{} }

// Generate builds and validates a plan. The plan ID is assigned by the caller
// on persistence; the fingerprint covers every field except ID and status.
func (e *Engine) Generate(req Request) (Plan, error) {
	p := req.Profile
	if !p.Deployable() {
		return Plan{}, ErrProfileNotReady
	}
	if reasons := Eligibility(p, req.Server, req.Domain); len(reasons) > 0 {
		return Plan{}, &EligibilityError{Reasons: reasons}
	}
	config := make([]string, 0, len(p.Configuration))
	for _, c := range p.Configuration {
		config = append(config, c.Name)
	}
	plan := Plan{
		SchemaVersion:             SchemaVersion,
		Status:                    "READY",
		ApplicationID:             req.ApplicationID,
		ApplicationProfileVersion: p.Version,
		AnalysisID:                p.AnalysisID,
		Source:                    p.Source,
		ServerID:                  req.Server.ID,
		Environment:               req.Environment,
		Strategy:                  p.Preset,
		Build: BuildPlan{Strategy: p.ContainerStrategy.Value, PackageManager: p.PackageManager.Value,
			Command: p.BuildCommand.Value, OutputDir: outputDir(p)},
		Runtime: RuntimePlan{Type: runtimeType(p), StartCommand: p.StartCommand.Value, Port: p.Port.Value,
			Configuration: config, Services: p.Services},
		Network: NetworkPlan{Proxy: "traefik", Domain: strings.ToLower(req.Domain), TLS: true, ExposedPort: p.Port.Value, PublicService: p.PublicService},
		Health: HealthPlan{Type: p.HealthCheck.Value.Type, Path: p.HealthCheck.Value.Path, ExpectedStatus: "200-399",
			TimeoutSeconds: 5, IntervalSeconds: 3, Retries: 20},
		Rollback: RollbackPlan{Strategy: "keep_previous_until_verified",
			Description: "The currently serving deployment keeps receiving traffic until the new one passes verification; on failure it is not replaced."},
		Steps: append([]string(nil), CanonicalSteps...),
		FailureBoundaries: map[string]string{
			"BUILD":          "Nothing changes on the server. The current deployment keeps serving.",
			"CREATE_RUNTIME": "The new runtime is discarded. The current deployment keeps serving.",
			"NETWORK":        "Traffic is not switched. The current deployment keeps serving.",
			"START":          "The new runtime is stopped. The current deployment keeps serving.",
			"VERIFY":         "The deployment does not go LIVE. The current deployment keeps serving.",
		},
	}
	if errs := validation.Validate(toInput(plan, req.Server)); len(errs) > 0 {
		return Plan{}, errs
	}
	plan.Fingerprint = Fingerprint(plan)
	return plan, nil
}

// Fingerprint hashes the canonical JSON of the plan without ID, status and fingerprint.
func Fingerprint(p Plan) string {
	p.ID, p.Status, p.Fingerprint = "", "", ""
	raw, _ := json.Marshal(p) // map keys are sorted by encoding/json
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Eligibility returns every unmet requirement of server for the profile.
func Eligibility(p ApplicationProfile, s ServerProfile, domain string) []string {
	var reasons []string
	switch s.Status {
	case "READY", "DEGRADED":
	default:
		reasons = append(reasons, "server is "+nonEmpty(s.Status, "UNKNOWN"))
	}
	required := []string{"docker"}
	if domain != "" {
		required = append(required, "traefik")
	}
	if p.Preset == "compose" {
		required = append(required, "docker_compose")
	}
	for _, c := range required {
		if !contains(s.Capabilities, c) {
			reasons = append(reasons, "missing capability: "+c)
		}
	}
	return reasons
}

func toInput(p Plan, s ServerProfile) validation.Input {
	return validation.Input{
		SchemaVersion: p.SchemaVersion, ApplicationID: p.ApplicationID, ProfileVersion: p.ApplicationProfileVersion,
		Commit: p.Source.Commit, ServerID: p.ServerID, ServerCapabilities: s.Capabilities, Environment: p.Environment,
		Preset: p.Strategy, BuildStrategy: p.Build.Strategy, BuildCommand: p.Build.Command, StartCommand: p.Runtime.StartCommand,
		Port: p.Runtime.Port, Domain: p.Network.Domain, HealthType: p.Health.Type, HealthPath: p.Health.Path,
		HealthTimeout: p.Health.TimeoutSeconds, HealthRetries: p.Health.Retries, RollbackStrategy: p.Rollback.Strategy,
		Steps: p.Steps, Configuration: p.Runtime.Configuration,
	}
}

func outputDir(p ApplicationProfile) string {
	if p.Preset == "vite" {
		return "dist"
	}
	return ""
}

func runtimeType(p ApplicationProfile) string {
	switch p.Preset {
	case "nextjs", "node":
		return "node"
	case "vite":
		return "static"
	case "go":
		return "go"
	case "dockerfile":
		return "image"
	case "compose":
		return "compose"
	}
	return ""
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func nonEmpty(s, d string) string {
	if s == "" {
		return d
	}
	return s
}
