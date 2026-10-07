package profile

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/digitaleflex/axiom/services/engine/internal/analyzer/evidence"
	"github.com/digitaleflex/axiom/services/engine/internal/profile/presets"
)

// Hints are explicit values from the manifest (axiom.yaml, #124) or user overrides.
type Hints struct {
	PackageManager *string `json:"packageManager,omitempty"`
	BuildCommand   *string `json:"buildCommand,omitempty"`
	StartCommand   *string `json:"startCommand,omitempty"`
	Port           *int    `json:"port,omitempty"`
	HealthPath     *string `json:"healthPath,omitempty"`
	Entrypoint     *string `json:"entrypoint,omitempty"`    // Go main package
	Strategy       *string `json:"strategy,omitempty"`      // dockerfile | compose | source
	PublicService  *string `json:"publicService,omitempty"` // compose
}

// Inputs are the explicit sources layered over detection.
type Inputs struct {
	Manifest  Hints
	Overrides Hints
}

// LowConfidence is the threshold under which a detected required value must be confirmed.
const LowConfidence = 0.6

// Build produces the canonical profile from an analysis result. It never
// presents defaults as detected values and never marks unsupported stacks ready.
func Build(res evidence.Result, src Source, in Inputs) Profile {
	b := &builder{res: res, in: in}
	p := Profile{SchemaVersion: SchemaVersion, AnalyzerVersion: res.AnalyzerVersion, Source: src, Blocking: []Issue{}, Services: []string{}, Configuration: []ConfigRequirement{}}
	p.Source.Root = res.Root

	facts := b.facts()
	// Strategy hints (manifest/override) can resolve container ambiguity.
	if s := b.hintString(func(h Hints) *string { return h.Strategy }); s != "" {
		switch s {
		case "dockerfile", "compose":
			facts.Container = s
		case "source":
			facts.Container = ""
		}
	}
	preset, rej := presets.Resolve(facts)

	p.Language = b.detected(evidence.KindLanguage)
	p.RuntimeVersion = b.detected(evidence.KindRuntimeVersion)
	p.Framework = b.detected(evidence.KindFramework)
	if f, ok := res.Get(evidence.KindServices); ok && f.State == evidence.StateDetected {
		p.Services = append(p.Services, f.Values...)
	}
	if f, ok := res.Get(evidence.KindConfiguration); ok && f.State == evidence.StateDetected {
		for _, name := range f.Values {
			p.Configuration = append(p.Configuration, ConfigRequirement{Name: name, Required: true, Secret: true})
		}
	}

	if rej != nil {
		p.Status = StatusUnsupported
		p.Unsupported = &Unsupported{Code: rej.Code, Message: rej.Message, Detected: rej.Detected, Alternatives: rej.Alternatives}
		p.Summary = "Axiom can't deploy this repository in V0.1: " + rej.Message + "."
		p.Confidence = b.confidence(p)
		return p
	}
	p.Preset = preset.Name
	p.ContainerStrategy = Field[string]{Value: preset.Strategy, Provenance: ProvenanceDefault}
	if f, ok := res.Get(evidence.KindContainer); ok && f.State == evidence.StateDetected && f.Value == preset.Strategy {
		p.ContainerStrategy = Field[string]{Value: preset.Strategy, Provenance: ProvenanceDetected, Confidence: f.Confidence}
	} else if facts.Container != "" && facts.Container == preset.Strategy {
		p.ContainerStrategy.Provenance = b.hintProvenance(func(h Hints) *string { return h.Strategy })
	}
	if f, ok := res.Get(evidence.KindContainer); ok && f.State == evidence.StateAmbiguous && b.hintString(func(h Hints) *string { return h.Strategy }) == "" {
		b.issue(Issue{Field: "containerStrategy", Code: "ambiguous", Message: "Both a Dockerfile and a Compose file were found; choose how to run the application", Options: f.Candidates})
	}

	// Package manager (Node presets).
	if preset.Runtime == "node" || preset.Runtime == "static" {
		p.PackageManager = b.layer("packageManager", b.detected(evidence.KindPackageManager), "", func(h Hints) *string { return h.PackageManager }, presets.ValidatePackageManager)
		b.requireResolved("packageManager", evidence.KindPackageManager, &p.PackageManager)
		if pm := p.PackageManager.Value; pm != "" && pm != facts.PackageManager {
			// Recompute command templates for the effective package manager.
			facts.PackageManager = pm
			preset, _ = presets.Resolve(facts)
		}
	} else if preset.Runtime == "go" {
		p.PackageManager = Field[string]{Value: "go", Provenance: ProvenanceDetected, Confidence: 1}
	}

	// Go entrypoint choice.
	if preset.Name == presets.Go {
		entry := b.hintString(func(h Hints) *string { return h.Entrypoint })
		switch {
		case entry != "":
			preset.BuildCommand = presets.GoBuild(entry)
		case len(preset.EntrypointCandidates) > 1:
			b.issue(Issue{Field: "entrypoint", Code: "ambiguous", Message: "Several main packages were found; choose the one to deploy", Options: preset.EntrypointCandidates})
		}
	}

	// Commands: detected scripts map to preset templates (detected provenance),
	// otherwise preset defaults; hints override after validation.
	buildDet := Field[string]{}
	if preset.BuildCommand != "" {
		buildDet = Field[string]{Value: preset.BuildCommand, Provenance: ProvenanceDefault}
		if f, ok := res.Get(evidence.KindBuildScript); ok && f.State == evidence.StateDetected {
			buildDet.Provenance, buildDet.Confidence = ProvenanceDetected, f.Confidence
		}
	}
	p.BuildCommand = b.layer("buildCommand", buildDet, "", func(h Hints) *string { return h.BuildCommand }, presets.ValidateCommand)
	startDet := Field[string]{}
	if preset.StartCommand != "" {
		startDet = Field[string]{Value: preset.StartCommand, Provenance: ProvenanceDefault}
		if f, ok := res.Get(evidence.KindStartScript); ok && f.State == evidence.StateDetected {
			startDet.Provenance, startDet.Confidence = ProvenanceDetected, f.Confidence
		}
	}
	p.StartCommand = b.layer("startCommand", startDet, "", func(h Hints) *string { return h.StartCommand }, presets.ValidateCommand)

	// Port.
	portDet := b.detected(evidence.KindPort)
	port := Field[int]{}
	switch {
	case portDet.Set():
		n, _ := strconv.Atoi(portDet.Value)
		port = Field[int]{Value: n, Provenance: ProvenanceDetected, Confidence: portDet.Confidence}
	case preset.DefaultPort > 0:
		port = Field[int]{Value: preset.DefaultPort, Provenance: ProvenanceDefault}
	}
	for _, src := range []struct {
		h    Hints
		prov string
	}{{in.Manifest, ProvenanceManifest}, {in.Overrides, ProvenanceOverride}} {
		if src.h.Port == nil {
			continue
		}
		v := *src.h.Port
		if v < 1 || v > 65535 {
			b.issue(Issue{Field: "port", Code: "invalid_override", Message: "port must be between 1 and 65535"})
			continue
		}
		old := port
		port = Field[int]{Value: v, Provenance: src.prov}
		if old.Set() && old.Value != v {
			port.Replaced = &old.Value
		}
	}
	p.Port = port
	if pf, ok := res.Get(evidence.KindPort); ok && pf.State == evidence.StateAmbiguous && port.Provenance != ProvenanceOverride && port.Provenance != ProvenanceManifest {
		b.issue(Issue{Field: "port", Code: "ambiguous", Message: "Several ports were found; choose the port the application listens on", Options: pf.Candidates})
	} else if !port.Set() {
		b.issue(Issue{Field: "port", Code: "not_detected", Message: "Axiom could not find the port the application listens on (e.g. Dockerfile EXPOSE)"})
	} else if port.Provenance == ProvenanceDetected && port.Confidence < LowConfidence {
		b.issue(Issue{Field: "port", Code: "low_confidence", Message: "Confirm the detected port", Options: []string{strconv.Itoa(port.Value)}})
	}

	// Compose public service.
	if preset.Name == presets.Compose {
		if svc := b.hintString(func(h Hints) *string { return h.PublicService }); svc != "" {
			p.PublicService = svc
		} else if f, ok := res.Get(evidence.KindPublicService); ok && f.State == evidence.StateDetected {
			p.PublicService = f.Value
		} else {
			opts := p.Services
			if ok && f.State == evidence.StateAmbiguous {
				opts = f.Candidates
			}
			b.issue(Issue{Field: "publicService", Code: "ambiguous", Message: "Choose the service that receives public traffic", Options: opts})
		}
	}

	// Health check.
	health := Field[HealthCheck]{Value: HealthCheck{Type: preset.HealthType, Path: preset.HealthPath}, Provenance: ProvenanceDefault}
	if path, prov := b.hintStringProv(func(h Hints) *string { return h.HealthPath }); prov != "" {
		if !strings.HasPrefix(path, "/") || len(path) > 200 {
			b.issue(Issue{Field: "healthCheck", Code: "invalid_override", Message: "health path must start with / (max 200 characters)"})
		} else {
			old := health.Value
			health = Field[HealthCheck]{Value: HealthCheck{Type: "http", Path: path}, Provenance: prov, Replaced: &old}
		}
	}
	p.HealthCheck = health

	// Language-level blocking facts.
	if f, ok := res.Get(evidence.KindFramework); ok && f.State == evidence.StateAmbiguous && preset.Strategy == presets.StrategySource {
		b.issue(Issue{Field: "framework", Code: "ambiguous", Message: "Several frameworks were detected", Options: f.Candidates})
	}

	if b.issues != nil {
		p.Blocking = b.issues
	}
	p.Status = StatusReady
	if len(p.Blocking) > 0 {
		p.Status = StatusNeedsReview
	}
	p.Confidence = b.confidence(p)
	p.Summary = summary(p, preset)
	return p
}

type builder struct {
	res    evidence.Result
	in     Inputs
	issues []Issue
}

func (b *builder) issue(i Issue) { b.issues = append(b.issues, i) }

func (b *builder) facts() presets.Facts {
	f := presets.Facts{}
	if x, ok := b.res.Get(evidence.KindLanguage); ok && x.State == evidence.StateDetected {
		f.Language = x.Value
	}
	if x, ok := b.res.Get(evidence.KindFramework); ok {
		f.Framework, f.FrameworkState = x.Value, x.State
	}
	if x, ok := b.res.Get(evidence.KindPackageManager); ok && x.State == evidence.StateDetected {
		f.PackageManager = x.Value
	}
	if pm := b.hintString(func(h Hints) *string { return h.PackageManager }); pm != "" && presets.ValidatePackageManager(pm) == nil {
		f.PackageManager = pm
	}
	if x, ok := b.res.Get(evidence.KindContainer); ok {
		f.ContainerState = x.State
		if x.State == evidence.StateDetected {
			f.Container = x.Value
		}
	}
	if x, ok := b.res.Get(evidence.KindBuildScript); ok {
		f.HasBuildScript = x.State == evidence.StateDetected
	}
	if x, ok := b.res.Get(evidence.KindStartScript); ok {
		f.HasStartScript = x.State == evidence.StateDetected
	}
	if b.hintString(func(h Hints) *string { return h.StartCommand }) != "" {
		f.HasStartScript = true // an explicit start command makes generic Node deployable
	}
	if x, ok := b.res.Get(evidence.KindEntrypoint); ok && x.State == evidence.StateDetected {
		f.GoEntrypoints = x.Values
	}
	if x, ok := b.res.Get(evidence.KindServices); ok && x.State == evidence.StateDetected {
		f.ComposeServices = x.Values
	}
	if x, ok := b.res.Get(evidence.KindWorkspace); ok && x.State == evidence.StateAmbiguous {
		f.RootAmbiguous, f.RootCandidates = true, x.Candidates
	}
	return f
}

// detected converts a single-valued finding into a field (empty when not detected).
func (b *builder) detected(kind string) Field[string] {
	f, ok := b.res.Get(kind)
	if !ok {
		return Field[string]{}
	}
	switch f.State {
	case evidence.StateDetected, evidence.StateUnsupported:
		return Field[string]{Value: f.Value, Provenance: ProvenanceDetected, Confidence: f.Confidence, Candidates: f.Candidates}
	case evidence.StateAmbiguous:
		return Field[string]{Candidates: f.Candidates, Confidence: f.Confidence}
	}
	return Field[string]{}
}

// layer applies manifest then override hints over a base field, validating them.
func (b *builder) layer(name string, base Field[string], _ string, pick func(Hints) *string, validate func(string) error) Field[string] {
	out := base
	for _, src := range []struct {
		h    Hints
		prov string
	}{{b.in.Manifest, ProvenanceManifest}, {b.in.Overrides, ProvenanceOverride}} {
		v := pick(src.h)
		if v == nil {
			continue
		}
		val := strings.TrimSpace(*v)
		if err := validate(val); err != nil {
			b.issue(Issue{Field: name, Code: "invalid_override", Message: err.Error()})
			continue
		}
		prev := out
		out = Field[string]{Value: val, Provenance: src.prov, Candidates: base.Candidates}
		if prev.Set() && prev.Value != val {
			r := prev.Value
			out.Replaced = &r
		}
	}
	return out
}

// requireResolved adds issues for ambiguous/low-confidence/missing required detections.
func (b *builder) requireResolved(name, kind string, f *Field[string]) {
	if f.Provenance == ProvenanceOverride || f.Provenance == ProvenanceManifest {
		return
	}
	ev, _ := b.res.Get(kind)
	switch {
	case ev.State == evidence.StateAmbiguous:
		b.issue(Issue{Field: name, Code: "ambiguous", Message: fmt.Sprintf("Conflicting evidence for %s", name), Options: ev.Candidates})
	case !f.Set():
		b.issue(Issue{Field: name, Code: "not_detected", Message: fmt.Sprintf("Axiom could not detect the %s", name)})
	case f.Provenance == ProvenanceDetected && f.Confidence < LowConfidence:
		b.issue(Issue{Field: name, Code: "low_confidence", Message: fmt.Sprintf("Confirm the detected %s", name), Options: []string{f.Value}})
	}
}

func (b *builder) hintString(pick func(Hints) *string) string {
	v, _ := b.hintStringProv(pick)
	return v
}

func (b *builder) hintProvenance(pick func(Hints) *string) string {
	_, p := b.hintStringProv(pick)
	return p
}

func (b *builder) hintStringProv(pick func(Hints) *string) (string, string) {
	if v := pick(b.in.Overrides); v != nil && strings.TrimSpace(*v) != "" {
		return strings.TrimSpace(*v), ProvenanceOverride
	}
	if v := pick(b.in.Manifest); v != nil && strings.TrimSpace(*v) != "" {
		return strings.TrimSpace(*v), ProvenanceManifest
	}
	return "", ""
}

// confidence is the lowest confidence among detected values (defaults and
// explicit values do not raise or lower it); 0 when nothing was detected.
func (b *builder) confidence(p Profile) float64 {
	min := -1.0
	for _, f := range []Field[string]{p.Language, p.Framework, p.PackageManager, p.BuildCommand, p.StartCommand} {
		if f.Provenance == ProvenanceDetected && (min < 0 || f.Confidence < min) {
			min = f.Confidence
		}
	}
	if p.Port.Provenance == ProvenanceDetected && (min < 0 || p.Port.Confidence < min) {
		min = p.Port.Confidence
	}
	if min < 0 {
		return 0
	}
	return min
}

func summary(p Profile, pr presets.Preset) string {
	stack := p.Framework.Value
	if stack == "" {
		stack = p.Language.Value
	}
	switch pr.Name {
	case presets.Dockerfile:
		return fmt.Sprintf("Axiom will build the repository Dockerfile and run the image on port %d.", p.Port.Value)
	case presets.Compose:
		return fmt.Sprintf("Axiom will run the Compose services %s; %s receives public traffic on port %d.", strings.Join(p.Services, ", "), nonEmpty(p.PublicService, "the public service"), p.Port.Value)
	case presets.Vite:
		return fmt.Sprintf("Axiom will build this %s app with %s and serve the static output on port %d.", stack, p.PackageManager.Value, p.Port.Value)
	case presets.Go:
		return fmt.Sprintf("Axiom will compile this Go application and run it as a container on port %d.", p.Port.Value)
	default:
		return fmt.Sprintf("Axiom will build this %s app with %s and run it as a container on port %d.", stack, p.PackageManager.Value, p.Port.Value)
	}
}

func nonEmpty(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
