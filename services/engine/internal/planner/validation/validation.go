// Package validation guarantees that only well-formed, safe deployment plans
// reach the executor (issue #97). Errors are field-addressable so the UI can
// point to the configuration field to fix.
package validation

import (
	"regexp"
	"strings"

	"github.com/digitaleflex/axiom/services/engine/internal/profile/presets"
)

// Input is the subset of a plan that is validated.
type Input struct {
	SchemaVersion      int
	ApplicationID      string
	ProfileVersion     int
	Commit             string
	ServerID           string
	ServerCapabilities []string
	Environment        string
	Preset             string
	BuildStrategy      string
	BuildCommand       string
	StartCommand       string
	Port               int
	Domain             string
	HealthType         string
	HealthPath         string
	HealthTimeout      int
	HealthRetries      int
	RollbackStrategy   string
	Steps              []string
	Configuration      []string
}

// FieldError is one invalid field.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// Errors is a non-empty list of field errors.
type Errors []FieldError

func (e Errors) Error() string {
	parts := make([]string, len(e))
	for i, f := range e {
		parts[i] = f.Field + ": " + f.Message
	}
	return "invalid deployment plan: " + strings.Join(parts, "; ")
}

var (
	canonical = []string{"BUILD", "CREATE_RUNTIME", "NETWORK", "START", "VERIFY"}
	shaRe     = regexp.MustCompile(`^[0-9a-f]{40}$`)
	domainRe  = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)
	envNameRe = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)
	presetSet = map[string]string{presets.NextJS: "source", presets.Vite: "source", presets.Node: "source", presets.Go: "source",
		presets.Dockerfile: "dockerfile", presets.Compose: "compose"}
)

// Validate returns all problems (nil when valid).
func Validate(in Input) Errors {
	var errs Errors
	add := func(field, msg string) { errs = append(errs, FieldError{Field: field, Message: msg}) }

	if in.SchemaVersion != 1 {
		add("schemaVersion", "unsupported plan schema version")
	}
	if in.ApplicationID == "" {
		add("applicationId", "required")
	}
	if in.ProfileVersion < 1 {
		add("applicationProfileVersion", "must reference a persisted profile revision")
	}
	if !shaRe.MatchString(in.Commit) {
		add("source.commit", "must be an exact 40-character commit SHA")
	}
	if in.ServerID == "" {
		add("serverId", "required")
	}
	switch in.Environment {
	case "production", "staging", "preview":
	default:
		add("environment", "must be production, staging or preview")
	}
	strategy, known := presetSet[in.Preset]
	if !known {
		add("strategy", "unknown preset")
	} else if in.BuildStrategy != strategy {
		add("build.strategy", "does not match the preset")
	}
	if in.BuildStrategy == "source" && in.Preset != presets.Vite && in.StartCommand == "" {
		add("runtime.startCommand", "required for source builds")
	}
	for field, cmd := range map[string]string{"build.command": in.BuildCommand, "runtime.startCommand": in.StartCommand} {
		if cmd != "" {
			if err := presets.ValidateCommand(cmd); err != nil {
				add(field, err.Error())
			}
		}
	}
	if in.Port < 1 || in.Port > 65535 {
		add("runtime.port", "must be between 1 and 65535")
	}
	if !domainRe.MatchString(in.Domain) || len(in.Domain) > 253 {
		add("network.domain", "must be a lowercase hostname such as app.example.com")
	}
	switch in.HealthType {
	case "http":
		if !strings.HasPrefix(in.HealthPath, "/") {
			add("healthCheck.path", "must start with /")
		}
	case "tcp":
	default:
		add("healthCheck.type", "must be http or tcp")
	}
	if in.HealthTimeout < 1 || in.HealthTimeout > 60 || in.HealthRetries < 1 || in.HealthRetries > 100 {
		add("healthCheck", "timeout must be 1-60 s and retries 1-100")
	}
	if in.RollbackStrategy == "" {
		add("rollback.strategy", "required")
	}
	validateSteps(in.Steps, add)
	for _, name := range in.Configuration {
		if !envNameRe.MatchString(name) {
			add("runtime.configuration", "invalid variable name "+name)
		}
	}
	needs := []string{"docker", "traefik"}
	if in.Preset == presets.Compose {
		needs = append(needs, "docker_compose")
	}
	for _, c := range needs {
		found := false
		for _, have := range in.ServerCapabilities {
			found = found || have == c
		}
		if !found {
			add("serverId", "server lacks capability "+c)
		}
	}
	return errs
}

// validateSteps enforces known names, canonical order, no duplicates,
// and the presence of the steps that make LIVE meaningful.
func validateSteps(steps []string, add func(string, string)) {
	if len(steps) == 0 {
		add("steps", "required")
		return
	}
	last := -1
	seen := map[string]bool{}
	for _, s := range steps {
		idx := -1
		for i, c := range canonical {
			if c == s {
				idx = i
			}
		}
		switch {
		case idx < 0:
			add("steps", "unknown step "+s)
		case seen[s]:
			add("steps", "duplicate step "+s)
		case idx < last:
			add("steps", "step "+s+" is out of order")
		}
		seen[s] = true
		if idx > last {
			last = idx
		}
	}
	for _, required := range []string{"CREATE_RUNTIME", "START", "VERIFY"} {
		if !seen[required] {
			add("steps", "missing required step "+required)
		}
	}
}
