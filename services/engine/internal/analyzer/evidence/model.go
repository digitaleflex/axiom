// Package evidence turns a repository snapshot into explainable findings
// (issue #94): every finding carries evidence records, a bounded confidence,
// and an explicit state. Conflicts are surfaced as "ambiguous", never resolved
// silently; unsupported stacks are reported as "unsupported", never deployable.
// The analyzer reads files only — it never executes repository code.
package evidence

// Version identifies the analyzer rules; it changes when results may change.
const Version = "evidence/1.0.0"

// Finding states (schemas/artifact.schema.json RepositoryAnalysis).
const (
	StateDetected      = "detected"
	StateAmbiguous     = "ambiguous"
	StateNotDetected   = "not_detected"
	StateUnsupported   = "unsupported"
	StateNotApplicable = "not_applicable"
)

// Evidence sources.
const (
	SourceManifest        = "manifest"
	SourceLockfile        = "lockfile"
	SourceDockerfile      = "dockerfile"
	SourceCompose         = "compose"
	SourceFrameworkConfig = "framework_config"
	SourceScript          = "script"
	SourceSource          = "source"
	SourceAxiomYAML       = "axiom_yaml"
)

// Finding kinds.
const (
	KindLanguage       = "language"
	KindRuntimeVersion = "runtime_version"
	KindFramework      = "framework"
	KindPackageManager = "package_manager"
	KindContainer      = "container_strategy"
	KindBuildScript    = "build_script"
	KindStartScript    = "start_script"
	KindPort           = "port"
	KindServices       = "services"
	KindPublicService  = "public_service"
	KindEntrypoint     = "entrypoints"
	KindConfiguration  = "configuration"
	KindWorkspace      = "workspace"
	KindManifest       = "project_manifest"
)

// KindOrder is the canonical, deterministic finding order.
var KindOrder = []string{KindLanguage, KindRuntimeVersion, KindFramework, KindPackageManager, KindContainer,
	KindBuildScript, KindStartScript, KindEntrypoint, KindPort, KindServices, KindPublicService, KindConfiguration, KindWorkspace, KindManifest}

// Evidence explains why a fact is believed.
type Evidence struct {
	Source      string `json:"source"`
	Path        string `json:"path"`
	Lines       string `json:"lines,omitempty"`
	Effect      string `json:"effect"` // supports | conflicts
	Rule        string `json:"rule"`
	Explanation string `json:"explanation"`
	Value       string `json:"value,omitempty"` // candidate value this evidence supports
}

// Finding is one detected characteristic.
type Finding struct {
	Kind       string     `json:"kind"`
	State      string     `json:"state"`
	Value      string     `json:"value,omitempty"`
	Values     []string   `json:"values,omitempty"` // multi-valued kinds (services, configuration)
	Confidence float64    `json:"confidence"`
	Candidates []string   `json:"candidates,omitempty"`
	Reason     string     `json:"reason,omitempty"` // unsupported / not detected explanation
	Evidence   []Evidence `json:"evidence"`
}

// Result is a RepositoryAnalysis payload.
type Result struct {
	AnalyzerVersion string    `json:"analyzerVersion"`
	Root            string    `json:"root"`
	Findings        []Finding `json:"findings"`
	Warnings        []string  `json:"warnings"`
}

// Get returns the finding of a kind.
func (r Result) Get(kind string) (Finding, bool) {
	for _, f := range r.Findings {
		if f.Kind == kind {
			return f, true
		}
	}
	return Finding{}, false
}
