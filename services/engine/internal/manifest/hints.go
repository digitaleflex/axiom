package manifest

import (
	"github.com/digitaleflex/axiom/services/engine/internal/analyzer/snapshot"
	"github.com/digitaleflex/axiom/services/engine/internal/profile"
	"github.com/digitaleflex/axiom/services/engine/internal/profile/presets"
)

// AppRoot returns the application directory for monorepos ("" = repository
// root). The analysis service uses it to select the analyzed root.
func (m Manifest) AppRoot() string { return m.App.Root }

// ToHints converts the manifest into profile hints (precedence layer 2,
// below user overrides, above detection). Commands are validated with
// presets.ValidateCommand: an unsafe command is omitted so detection and
// preset defaults apply — a manifest can never smuggle an unsafe command
// into a profile.
func ToHints(m Manifest) profile.Hints {
	h := profile.Hints{}
	if m.Runtime.PackageManager != "" {
		pm := m.Runtime.PackageManager
		h.PackageManager = &pm
	}
	if cmd := m.Build.Command; cmd != "" && presets.ValidateCommand(cmd) == nil {
		h.BuildCommand = &cmd
	}
	if cmd := m.Start.Command; cmd != "" && presets.ValidateCommand(cmd) == nil {
		h.StartCommand = &cmd
	}
	if m.Port != 0 {
		p := m.Port
		h.Port = &p
	}
	if m.Health.Type == "http" && m.Health.Path != "" {
		p := m.Health.Path
		h.HealthPath = &p
	}
	if m.Strategy != "" {
		s := m.Strategy
		h.Strategy = &s
	}
	if m.Services.Public != "" {
		s := m.Services.Public
		h.PublicService = &s
	}
	return h
}

// ComposeFileNames are the repository files that define Compose services
// (mirrors the analyzer's detection).
var ComposeFileNames = []string{"compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml"}

// CheckConflict verifies the manifest against repository evidence the parser
// cannot see. It currently detects strategy: compose without a compose file
// (MANIFEST_CONFLICT): the manifest contradicts the repository
// irreconcilably. hasFile reports whether name exists at the analysis root.
func (m Manifest) CheckConflict(hasFile func(name string) bool) error {
	if m.Strategy != "compose" {
		return nil
	}
	for _, name := range ComposeFileNames {
		if hasFile(name) {
			return nil
		}
	}
	return fail(CodeConflict, "strategy is compose but the repository defines no compose file (%s)", joinNames(ComposeFileNames))
}

// Load finds and parses axiom.yaml in a snapshot. The root-level manifest
// wins over <root>/axiom.yaml (spec §1). It returns the manifest hints and
// app.root ("" when the manifest does not set one). A present but invalid
// manifest is an error: it blocks profile resolution and is never partially
// applied.
func Load(snap snapshot.Snapshot, root string) (profile.Hints, string, error) {
	seen := map[string]bool{}
	for _, p := range []string{"axiom.yaml", joinRoot(root, "axiom.yaml")} {
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		f, ok := snap.Lookup(p)
		if !ok || !f.Retained {
			continue
		}
		m, err := Parse(f.Content)
		if err != nil {
			return profile.Hints{}, "", err
		}
		if err := m.CheckConflict(func(name string) bool {
			_, ok := snap.Lookup(joinRoot(root, name))
			return ok
		}); err != nil {
			return profile.Hints{}, "", err
		}
		return ToHints(m), m.AppRoot(), nil
	}
	return profile.Hints{}, "", nil
}

func joinRoot(root, name string) string {
	if root == "" {
		return name
	}
	return root + "/" + name
}

func joinNames(names []string) string {
	out := ""
	for i, n := range names {
		if i > 0 {
			out += ", "
		}
		out += n
	}
	return out
}
