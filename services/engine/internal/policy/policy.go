// Package policy is the deployment policy gate (issue #67): the Engine
// cannot dispatch an operation unless the plan is authorized by policy.
// Policy is deterministic code, independent of model output: it re-verifies
// plan integrity and validity, binds the plan to its target, and refuses
// anything carrying secret values. Denials use POLICY_DENIED.
package policy

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/digitaleflex/axiom/services/engine/internal/planner"
	"github.com/digitaleflex/axiom/services/engine/internal/planner/validation"
	"github.com/digitaleflex/axiom/services/engine/internal/profile/presets"
)

// Decision is the outcome of policy evaluation.
type Decision struct {
	Allow   bool     `json:"allow"`
	Reasons []string `json:"reasons,omitempty"`
}

// Allowed is the positive decision.
func Allowed() Decision { return Decision{Allow: true} }

// Denied builds a negative decision.
func Denied(reasons ...string) Decision { return Decision{Reasons: reasons} }

// secretPatterns match values that must never appear in a plan. Plans carry
// configuration names, never values; anything matching is a leak or an
// injection attempt.
var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(password|passwd|pwd|secret|token|api[_-]?key)\s*[:=]\s*\S+`),
	regexp.MustCompile(`(?i)authorization\s*:\s*bearer\s+\S+`),
	regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`),
	regexp.MustCompile(`(?i)(aws_|github_|xox[bap]-)[A-Za-z0-9_\-]+`),
}

// Evaluate authorizes plan for execution on serverID.
func Evaluate(plan planner.Plan, serverID string) Decision {
	var reasons []string
	deny := func(format string, args ...any) { reasons = append(reasons, fmt.Sprintf(format, args...)) }

	if plan.Status != "READY" {
		deny("plan status is %q, must be READY", plan.Status)
	}
	if plan.Fingerprint != "" && planner.Fingerprint(plan) != plan.Fingerprint {
		deny("plan integrity check failed: fingerprint does not match plan contents")
	}
	switch plan.Strategy {
	case presets.NextJS, presets.Vite, presets.Node, presets.Go, presets.Dockerfile, presets.Compose:
	default:
		deny("unknown strategy %q", plan.Strategy)
	}
	switch plan.Environment {
	case "production", "staging", "preview":
	default:
		deny("unknown environment %q", plan.Environment)
	}
	if plan.ServerID != serverID {
		deny("plan targets server %q, not %q", plan.ServerID, serverID)
	}
	if errs := validation.Validate(toValidationInput(plan)); len(errs) > 0 {
		for _, e := range errs {
			deny("invalid plan: %s: %s", e.Field, e.Message)
		}
	}
	if found := scanSecrets(plan); found != "" {
		deny("plan contains a possible secret value (%s); plans carry names, never values", found)
	}
	if len(reasons) > 0 {
		return Denied(reasons...)
	}
	return Allowed()
}

// toValidationInput mirrors the planner's internal mapping (kept in sync by tests).
func toValidationInput(p planner.Plan) validation.Input {
	caps := []string{"docker", "traefik"}
	if p.Strategy == presets.Compose {
		caps = append(caps, "docker_compose")
	}
	return validation.Input{
		SchemaVersion: p.SchemaVersion, ApplicationID: p.ApplicationID,
		ProfileVersion: p.ApplicationProfileVersion, Commit: p.Source.Commit,
		ServerID: p.ServerID, ServerCapabilities: caps, Environment: p.Environment,
		Preset: p.Strategy, BuildStrategy: p.Build.Strategy, BuildCommand: p.Build.Command,
		StartCommand: p.Runtime.StartCommand, Port: p.Runtime.Port, Domain: p.Network.Domain,
		HealthType: p.Health.Type, HealthPath: p.Health.Path,
		HealthTimeout: p.Health.TimeoutSeconds, HealthRetries: p.Health.Retries,
		RollbackStrategy: p.Rollback.Strategy, Steps: p.Steps, Configuration: p.Runtime.Configuration,
	}
}

// scanSecrets serializes the plan and looks for secret values. It returns the
// pattern class found, never the value itself.
func scanSecrets(plan planner.Plan) string {
	raw, err := json.Marshal(plan)
	if err != nil {
		return "unserializable plan"
	}
	text := string(raw)
	for _, re := range secretPatterns {
		if loc := re.FindString(text); loc != "" {
			name := re.String()
			if i := strings.Index(name, ")"); i > 0 {
				name = name[:i]
			}
			return "pattern " + name
		}
	}
	return ""
}
