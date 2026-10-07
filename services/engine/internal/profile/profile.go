// Package profile defines the canonical, versioned Application Profile
// (issue #95) consumed by the planner, the UI and persistence. Consumers
// depend on this schema, never on analyzer internals.
package profile

// SchemaVersion is bumped on breaking changes to the serialized profile.
const SchemaVersion = 1

// Provenance of a value (highest precedence first).
const (
	ProvenanceOverride = "override"
	ProvenanceManifest = "manifest"
	ProvenanceDetected = "detected"
	ProvenanceDefault  = "default"
)

// Status of a profile.
const (
	StatusReady       = "ready"        // deployable as-is
	StatusNeedsReview = "needs_review" // blocking facts must be confirmed by the user
	StatusUnsupported = "unsupported"  // cannot resolve to a V0.1 preset
)

// Field is one profile value with its provenance and confidence.
type Field[T any] struct {
	Value      T        `json:"value"`
	Provenance string   `json:"provenance,omitempty"`
	Confidence float64  `json:"confidence,omitempty"`
	Candidates []string `json:"candidates,omitempty"`
	// Replaced records the lower-precedence value that was overridden (L3 in UI).
	Replaced *T `json:"replaced,omitempty"`
}

// Set reports whether the field has a value.
func (f Field[T]) Set() bool { return f.Provenance != "" }

// HealthCheck describes how the runtime is verified before LIVE.
type HealthCheck struct {
	Type string `json:"type"` // http | tcp
	Path string `json:"path,omitempty"`
}

// ConfigRequirement is a configuration variable name (never a value).
type ConfigRequirement struct {
	Name     string `json:"name"`
	Required bool   `json:"required"`
	Secret   bool   `json:"secret"`
}

// Issue is a blocking fact the user must resolve before planning.
type Issue struct {
	Field   string   `json:"field"`
	Code    string   `json:"code"` // ambiguous | low_confidence | not_detected | invalid_override
	Message string   `json:"message"`
	Options []string `json:"options,omitempty"`
}

// Unsupported explains why no V0.1 preset applies.
type Unsupported struct {
	Code         string   `json:"code"`
	Message      string   `json:"message"`
	Detected     string   `json:"detected,omitempty"`
	Alternatives []string `json:"alternatives"`
}

// Source correlates the profile to an exact repository commit.
type Source struct {
	RepositoryID string `json:"repositoryId"`
	Ref          string `json:"ref"`
	Commit       string `json:"commit"`
	Root         string `json:"root"`
}

// Profile is the canonical Application Profile.
type Profile struct {
	SchemaVersion   int    `json:"schemaVersion"`
	Version         int    `json:"version"` // revision per application, assigned on persistence
	AnalysisID      string `json:"analysisId,omitempty"`
	AnalyzerVersion string `json:"analyzerVersion"`
	Source          Source `json:"source"`

	Status      string       `json:"status"`
	Preset      string       `json:"preset,omitempty"`
	Summary     string       `json:"summary"`
	Unsupported *Unsupported `json:"unsupported,omitempty"`
	Blocking    []Issue      `json:"blocking"`

	Language          Field[string]       `json:"language"`
	RuntimeVersion    Field[string]       `json:"runtimeVersion"`
	Framework         Field[string]       `json:"framework"`
	PackageManager    Field[string]       `json:"packageManager"`
	BuildCommand      Field[string]       `json:"buildCommand"`
	StartCommand      Field[string]       `json:"startCommand"`
	Port              Field[int]          `json:"port"`
	ContainerStrategy Field[string]       `json:"containerStrategy"`
	HealthCheck       Field[HealthCheck]  `json:"healthCheck"`
	Services          []string            `json:"services"`
	PublicService     string              `json:"publicService,omitempty"`
	Configuration     []ConfigRequirement `json:"configuration"`

	Confidence float64 `json:"confidence"`
}

// Deployable reports whether the planner may consume the profile.
func (p Profile) Deployable() bool { return p.Status == StatusReady }
