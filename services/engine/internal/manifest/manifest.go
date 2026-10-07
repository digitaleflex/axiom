// Package manifest parses and validates the optional axiom.yaml project
// manifest (issue #124; spec docs/architecture/axiom-yaml.md; schema
// schemas/axiom.schema.json). The manifest only adds hints or overrides —
// auto-detection remains the default path. Validation is deterministic and
// mirrors the JSON Schema: unknown fields are rejected, env entries carry
// names only (a value is a distinct error detected before any generic schema
// error), and paths cannot escape the repository.
package manifest

import (
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Error codes (docs/architecture/axiom-yaml.md §4).
const (
	CodeParseError          = "MANIFEST_PARSE_ERROR"
	CodeVersionUnsupported  = "MANIFEST_VERSION_UNSUPPORTED"
	CodeSchemaInvalid       = "MANIFEST_SCHEMA_INVALID"
	CodeSecretValue         = "MANIFEST_SECRET_VALUE"
	CodeStrategyUnsupported = "MANIFEST_STRATEGY_UNSUPPORTED"
	CodeConflict            = "MANIFEST_CONFLICT"
)

// Supported manifest version (spec §5).
const SupportedVersion = 1

// Error is a manifest validation failure with a stable machine-readable code.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

func fail(code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// Manifest is a validated axiom.yaml (version 1). Zero values mean "not set".
type Manifest struct {
	Version     int         `json:"version"`
	App         App         `json:"app,omitempty"`
	Strategy    string      `json:"strategy,omitempty"`
	Runtime     Runtime     `json:"runtime,omitempty"`
	Build       Build       `json:"build,omitempty"`
	Start       Start       `json:"start,omitempty"`
	Port        int         `json:"port,omitempty"`
	Env         []Env       `json:"env,omitempty"`
	Services    Services    `json:"services,omitempty"`
	Domains     []string    `json:"domains,omitempty"`
	Health      Health      `json:"health,omitempty"`
	Constraints Constraints `json:"constraints,omitempty"`
}

// App is the application identity and monorepo location.
type App struct {
	Name string `json:"name,omitempty"` // DNS-safe slug; default: repository name
	Root string `json:"root,omitempty"` // application directory (monorepos)
}

// Runtime describes the stack.
type Runtime struct {
	Language       string `json:"language,omitempty"`       // javascript | typescript | go
	Version        string `json:"version,omitempty"`        // e.g. "1.23"
	PackageManager string `json:"packageManager,omitempty"` // npm | pnpm | yarn | bun | go
}

// Build overrides the build step.
type Build struct {
	Command    string `json:"command,omitempty"`
	Dockerfile string `json:"dockerfile,omitempty"`
	Context    string `json:"context,omitempty"`
}

// Start overrides the start step.
type Start struct {
	Command string `json:"command,omitempty"`
}

// Env is one configuration requirement. Names only — values are never allowed.
type Env struct {
	Name        string `json:"name"`
	Required    bool   `json:"required,omitempty"`
	Secret      bool   `json:"secret,omitempty"`
	Description string `json:"description,omitempty"`
}

// Services selects Compose services (strategy: compose only).
type Services struct {
	File    string   `json:"file,omitempty"`
	Include []string `json:"include,omitempty"`
	Public  string   `json:"public,omitempty"`
}

// Health describes how the runtime is verified.
type Health struct {
	Type           string `json:"type"` // http | tcp
	Path           string `json:"path,omitempty"`
	ExpectedStatus int    `json:"expectedStatus,omitempty"`
	TimeoutSeconds int    `json:"timeoutSeconds,omitempty"`
	Retries        int    `json:"retries,omitempty"`
}

// Constraints are used for server eligibility.
type Constraints struct {
	Architecture string `json:"architecture,omitempty"` // linux/amd64 | linux/arm64
	MinMemoryMB  int    `json:"minMemoryMB,omitempty"`
	MinDiskGB    int    `json:"minDiskGB,omitempty"`
}

// Patterns mirroring schemas/axiom.schema.json. Go's regexp (RE2) has no
// lookahead, so the domain length bound (1–253) is checked in validDomain.
var (
	appNameRe        = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
	envNameRe        = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)
	serviceNameRe    = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)
	domainRe         = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)
	runtimeVersionRe = regexp.MustCompile(`^[0-9]+(\.[0-9]+){0,2}$`)
)

// validDomain mirrors the domains pattern, including its length bound.
func validDomain(s string) bool {
	return len(s) >= 1 && len(s) <= 253 && domainRe.MatchString(s)
}

// Parse validates axiom.yaml content and returns the manifest. The first
// error wins, in a deterministic order: YAML syntax, document shape, version,
// secret values, then the schema rules.
func Parse(content []byte) (Manifest, error) {
	var raw map[string]any
	if err := yaml.Unmarshal(content, &raw); err != nil {
		return Manifest{}, fail(CodeParseError, "axiom.yaml is not valid YAML: %v", err)
	}
	if raw == nil {
		return Manifest{}, fail(CodeParseError, "axiom.yaml is empty; expected a mapping of manifest fields")
	}
	if err := checkKeys(raw, "version", "app", "strategy", "runtime", "build", "start", "port", "env", "services", "domains", "health", "constraints"); err != nil {
		return Manifest{}, err
	}

	// Version first: a missing or unsupported version makes every other
	// check meaningless.
	v, ok := raw["version"]
	if !ok {
		return Manifest{}, fail(CodeVersionUnsupported, "version is required (supported: %d)", SupportedVersion)
	}
	n, ok := asInt(v)
	if !ok || n != SupportedVersion {
		return Manifest{}, fail(CodeVersionUnsupported, "manifest version %v is not supported by this Axiom version (supported: %d)", v, SupportedVersion)
	}

	// Secret values are detected before any generic schema error.
	if list, ok := raw["env"].([]any); ok {
		for i, e := range list {
			if m, ok := e.(map[string]any); ok {
				if _, has := m["value"]; has {
					return Manifest{}, fail(CodeSecretValue, "env entry %d carries a value; env entries are names only — values are entered in the Axiom Console", i)
				}
			}
		}
	}

	m := Manifest{Version: SupportedVersion}
	var hasRuntime, hasServices bool

	if v, ok := raw["app"]; ok {
		sec, err := parseSection(v, "app")
		if err != nil {
			return Manifest{}, err
		}
		if err := checkKeys(sec, "name", "root"); err != nil {
			return Manifest{}, err
		}
		if v, ok := sec["name"]; ok {
			s, ok := v.(string)
			if !ok || !appNameRe.MatchString(s) {
				return Manifest{}, fail(CodeSchemaInvalid, "app.name must be a DNS-safe slug: lowercase letters, digits and hyphens, starting and ending with a letter or digit")
			}
			m.App.Name = s
		}
		if v, ok := sec["root"]; ok {
			s, ok := v.(string)
			if !ok || !validAppRoot(s) {
				return Manifest{}, fail(CodeSchemaInvalid, "app.root must be a relative path inside the repository (no absolute paths, no \"..\")")
			}
			m.App.Root = s
		}
	}

	if v, ok := raw["strategy"]; ok {
		s, ok := v.(string)
		if !ok || !isStrategy(s) {
			return Manifest{}, fail(CodeSchemaInvalid, "strategy must be one of: nextjs, vite, node, go, dockerfile, compose")
		}
		m.Strategy = s
	}

	if v, ok := raw["runtime"]; ok {
		sec, err := parseSection(v, "runtime")
		if err != nil {
			return Manifest{}, err
		}
		hasRuntime = true
		if err := checkKeys(sec, "language", "version", "packageManager"); err != nil {
			return Manifest{}, err
		}
		if v, ok := sec["language"]; ok {
			s, ok := v.(string)
			if !ok || (s != "javascript" && s != "typescript" && s != "go") {
				return Manifest{}, fail(CodeSchemaInvalid, "runtime.language must be one of: javascript, typescript, go")
			}
			m.Runtime.Language = s
		}
		if v, ok := sec["version"]; ok {
			s, ok := v.(string)
			if !ok || !runtimeVersionRe.MatchString(s) {
				return Manifest{}, fail(CodeSchemaInvalid, "runtime.version must look like \"20\" or \"1.23.4\"")
			}
			m.Runtime.Version = s
		}
		if v, ok := sec["packageManager"]; ok {
			s, ok := v.(string)
			if !ok || !isPackageManager(s) {
				return Manifest{}, fail(CodeSchemaInvalid, "runtime.packageManager must be one of: npm, pnpm, yarn, bun, go")
			}
			m.Runtime.PackageManager = s
		}
	}

	if v, ok := raw["build"]; ok {
		sec, err := parseSection(v, "build")
		if err != nil {
			return Manifest{}, err
		}
		if err := checkKeys(sec, "command", "dockerfile", "context"); err != nil {
			return Manifest{}, err
		}
		if v, ok := sec["command"]; ok {
			s, ok := v.(string)
			if !ok || !validCommand(s) {
				return Manifest{}, fail(CodeSchemaInvalid, "build.command must be a single line of up to 500 characters (no newlines or backticks)")
			}
			m.Build.Command = s
		}
		if v, ok := sec["dockerfile"]; ok {
			s, ok := v.(string)
			if !ok || !validRelPath(s) {
				return Manifest{}, fail(CodeSchemaInvalid, "build.dockerfile must be a relative path inside the repository")
			}
			m.Build.Dockerfile = s
		}
		if v, ok := sec["context"]; ok {
			s, ok := v.(string)
			if !ok || !validRelPath(s) {
				return Manifest{}, fail(CodeSchemaInvalid, "build.context must be a relative path inside the repository")
			}
			m.Build.Context = s
		}
	}

	if v, ok := raw["start"]; ok {
		sec, err := parseSection(v, "start")
		if err != nil {
			return Manifest{}, err
		}
		if err := checkKeys(sec, "command"); err != nil {
			return Manifest{}, err
		}
		s, ok := sec["command"].(string)
		if !ok || !validCommand(s) {
			return Manifest{}, fail(CodeSchemaInvalid, "start.command must be a single line of up to 500 characters (no newlines or backticks)")
		}
		m.Start.Command = s
	}

	if v, ok := raw["port"]; ok {
		n, ok := asInt(v)
		if !ok || n < 1 || n > 65535 {
			return Manifest{}, fail(CodeSchemaInvalid, "port must be an integer between 1 and 65535")
		}
		m.Port = n
	}

	if v, ok := raw["env"]; ok {
		list, ok := v.([]any)
		if !ok {
			return Manifest{}, fail(CodeSchemaInvalid, "env must be a list of entries")
		}
		seen := map[string]bool{}
		for i, e := range list {
			em, ok := e.(map[string]any)
			if !ok {
				return Manifest{}, fail(CodeSchemaInvalid, "env[%d] must be a mapping", i)
			}
			if err := checkKeys(em, "name", "required", "secret", "description"); err != nil {
				return Manifest{}, err
			}
			name, ok := em["name"].(string)
			if !ok || !envNameRe.MatchString(name) {
				return Manifest{}, fail(CodeSchemaInvalid, "env[%d].name is required and must be an UPPER_SNAKE_CASE identifier", i)
			}
			if seen[name] {
				return Manifest{}, fail(CodeSchemaInvalid, "env entries must be unique; %q appears twice", name)
			}
			seen[name] = true
			entry := Env{Name: name}
			if v, ok := em["required"]; ok {
				b, ok := v.(bool)
				if !ok {
					return Manifest{}, fail(CodeSchemaInvalid, "env[%d].required must be a boolean", i)
				}
				entry.Required = b
			}
			if v, ok := em["secret"]; ok {
				b, ok := v.(bool)
				if !ok {
					return Manifest{}, fail(CodeSchemaInvalid, "env[%d].secret must be a boolean", i)
				}
				entry.Secret = b
			}
			if v, ok := em["description"]; ok {
				s, ok := v.(string)
				if !ok || len(s) > 200 {
					return Manifest{}, fail(CodeSchemaInvalid, "env[%d].description must be a string of up to 200 characters", i)
				}
				entry.Description = s
			}
			m.Env = append(m.Env, entry)
		}
	}

	if v, ok := raw["services"]; ok {
		sec, err := parseSection(v, "services")
		if err != nil {
			return Manifest{}, err
		}
		hasServices = true
		if err := checkKeys(sec, "file", "include", "public"); err != nil {
			return Manifest{}, err
		}
		if v, ok := sec["file"]; ok {
			s, ok := v.(string)
			if !ok || !validRelPath(s) {
				return Manifest{}, fail(CodeSchemaInvalid, "services.file must be a relative path inside the repository")
			}
			m.Services.File = s
		}
		if v, ok := sec["include"]; ok {
			list, ok := v.([]any)
			if !ok {
				return Manifest{}, fail(CodeSchemaInvalid, "services.include must be a list of service names")
			}
			seen := map[string]bool{}
			for i, e := range list {
				s, ok := e.(string)
				if !ok || !serviceNameRe.MatchString(s) {
					return Manifest{}, fail(CodeSchemaInvalid, "services.include[%d] must be a service name (letters, digits, dots, underscores, hyphens)", i)
				}
				if seen[s] {
					return Manifest{}, fail(CodeSchemaInvalid, "services.include entries must be unique; %q appears twice", s)
				}
				seen[s] = true
				m.Services.Include = append(m.Services.Include, s)
			}
		}
		if v, ok := sec["public"]; ok {
			s, ok := v.(string)
			if !ok || !serviceNameRe.MatchString(s) {
				return Manifest{}, fail(CodeSchemaInvalid, "services.public must be a service name (letters, digits, dots, underscores, hyphens)")
			}
			m.Services.Public = s
		}
	}

	if v, ok := raw["domains"]; ok {
		list, ok := v.([]any)
		if !ok {
			return Manifest{}, fail(CodeSchemaInvalid, "domains must be a list of hostnames")
		}
		seen := map[string]bool{}
		for i, e := range list {
			s, ok := e.(string)
			if !ok || !validDomain(s) {
				return Manifest{}, fail(CodeSchemaInvalid, "domains[%d] must be a lowercase fully-qualified domain name", i)
			}
			if seen[s] {
				return Manifest{}, fail(CodeSchemaInvalid, "domains entries must be unique; %q appears twice", s)
			}
			seen[s] = true
			m.Domains = append(m.Domains, s)
		}
	}

	if v, ok := raw["health"]; ok {
		sec, err := parseSection(v, "health")
		if err != nil {
			return Manifest{}, err
		}
		if err := checkKeys(sec, "type", "path", "expectedStatus", "timeoutSeconds", "retries"); err != nil {
			return Manifest{}, err
		}
		t, ok := sec["type"].(string)
		if !ok || (t != "http" && t != "tcp") {
			return Manifest{}, fail(CodeSchemaInvalid, "health.type is required and must be http or tcp")
		}
		m.Health.Type = t
		if v, ok := sec["path"]; ok {
			s, ok := v.(string)
			if !ok || !strings.HasPrefix(s, "/") {
				return Manifest{}, fail(CodeSchemaInvalid, "health.path must start with /")
			}
			m.Health.Path = s
		}
		if v, ok := sec["expectedStatus"]; ok {
			n, ok := asInt(v)
			if !ok || n < 100 || n > 599 {
				return Manifest{}, fail(CodeSchemaInvalid, "health.expectedStatus must be an integer between 100 and 599")
			}
			m.Health.ExpectedStatus = n
		}
		if v, ok := sec["timeoutSeconds"]; ok {
			n, ok := asInt(v)
			if !ok || n < 1 || n > 300 {
				return Manifest{}, fail(CodeSchemaInvalid, "health.timeoutSeconds must be an integer between 1 and 300")
			}
			m.Health.TimeoutSeconds = n
		}
		if v, ok := sec["retries"]; ok {
			n, ok := asInt(v)
			if !ok || n < 0 || n > 20 {
				return Manifest{}, fail(CodeSchemaInvalid, "health.retries must be an integer between 0 and 20")
			}
			m.Health.Retries = n
		}
	}

	if v, ok := raw["constraints"]; ok {
		sec, err := parseSection(v, "constraints")
		if err != nil {
			return Manifest{}, err
		}
		if err := checkKeys(sec, "architecture", "minMemoryMB", "minDiskGB"); err != nil {
			return Manifest{}, err
		}
		if v, ok := sec["architecture"]; ok {
			s, ok := v.(string)
			if !ok || (s != "linux/amd64" && s != "linux/arm64") {
				return Manifest{}, fail(CodeSchemaInvalid, "constraints.architecture must be linux/amd64 or linux/arm64")
			}
			m.Constraints.Architecture = s
		}
		if v, ok := sec["minMemoryMB"]; ok {
			n, ok := asInt(v)
			if !ok || n < 64 {
				return Manifest{}, fail(CodeSchemaInvalid, "constraints.minMemoryMB must be an integer of at least 64")
			}
			m.Constraints.MinMemoryMB = n
		}
		if v, ok := sec["minDiskGB"]; ok {
			n, ok := asInt(v)
			if !ok || n < 1 {
				return Manifest{}, fail(CodeSchemaInvalid, "constraints.minDiskGB must be an integer of at least 1")
			}
			m.Constraints.MinDiskGB = n
		}
	}

	// Conditional rules (schema allOf).
	if hasServices && m.Strategy != "compose" {
		return Manifest{}, fail(CodeSchemaInvalid, "services requires strategy: compose")
	}
	if m.Strategy == "dockerfile" {
		if hasRuntime {
			return Manifest{}, fail(CodeSchemaInvalid, "strategy dockerfile forbids runtime — the Dockerfile defines the stack")
		}
		if m.Build.Command != "" {
			return Manifest{}, fail(CodeSchemaInvalid, "strategy dockerfile forbids build.command — the Dockerfile defines the build")
		}
	}
	if m.Health.Type == "http" && m.Health.Path == "" {
		return Manifest{}, fail(CodeSchemaInvalid, "health.type http requires health.path")
	}

	// Strategy availability on this Axiom version. The schema enum already
	// restricts to V0.1 presets; this check keeps the code meaningful if a
	// schema-valid strategy ever becomes unavailable on a server.
	if m.Strategy != "" && !supportedStrategy(m.Strategy) {
		return Manifest{}, fail(CodeStrategyUnsupported, "strategy %q is not available on this Axiom version", m.Strategy)
	}

	return m, nil
}

// parseSection decodes one nested mapping, rejecting non-mapping values.
func parseSection(v any, name string) (map[string]any, error) {
	if v == nil {
		return nil, nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fail(CodeSchemaInvalid, "%s must be a mapping", name)
	}
	return m, nil
}

// checkKeys rejects unknown fields (additionalProperties: false).
func checkKeys(m map[string]any, allowed ...string) error {
	ok := make(map[string]bool, len(allowed))
	for _, k := range allowed {
		ok[k] = true
	}
	for k := range m {
		if !ok[k] {
			return fail(CodeSchemaInvalid, "unknown field %q — unknown fields are rejected", k)
		}
	}
	return nil
}

// validAppRoot mirrors the app.root pattern: relative, no ".." anywhere.
func validAppRoot(p string) bool {
	return p != "" && !strings.HasPrefix(p, "/") && !strings.ContainsRune(p, 0) && !strings.Contains(p, "..")
}

// validRelPath mirrors the relativePath pattern: non-empty, relative, and no
// ".." path segment (a ".." inside a file name such as "a..b" is allowed).
func validRelPath(p string) bool {
	if p == "" || strings.HasPrefix(p, "/") || strings.ContainsRune(p, 0) {
		return false
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return false
		}
	}
	return true
}

// validCommand mirrors the command pattern: a single printable line.
func validCommand(s string) bool {
	if len(s) < 1 || len(s) > 500 {
		return false
	}
	for _, r := range s {
		if r == '\n' || r == '\r' || r == '`' {
			return false
		}
	}
	return true
}

func isStrategy(s string) bool {
	switch s {
	case "nextjs", "vite", "node", "go", "dockerfile", "compose":
		return true
	}
	return false
}

func supportedStrategy(s string) bool { return isStrategy(s) }

func isPackageManager(s string) bool {
	switch s {
	case "npm", "pnpm", "yarn", "bun", "go":
		return true
	}
	return false
}

func asInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	}
	return 0, false
}
