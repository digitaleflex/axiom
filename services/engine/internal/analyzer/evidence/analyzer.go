package evidence

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/digitaleflex/axiom/services/engine/internal/analyzer/snapshot"
)

// Options configure an analysis.
type Options struct {
	// Root is the application directory relative to the repository root
	// ("" = repository root). Set from axiom.yaml app.root or a user override.
	Root string
}

// vote is evidence supporting one candidate value of a kind.
type vote struct {
	value    string
	weight   float64 // 0..1; 1 means an explicit declaration (decisive)
	evidence Evidence
}

type analysis struct {
	s        snapshot.Snapshot
	root     string
	votes    map[string][]vote
	multi    map[string]map[string][]Evidence // kind -> value -> evidence (multi-valued kinds)
	special  map[string]Finding               // findings decided directly (unsupported, workspace…)
	warnings []string
}

// Analyze inspects the snapshot. It is deterministic: the same snapshot and
// options always produce the same result.
func Analyze(s snapshot.Snapshot, opts Options) Result {
	root := strings.Trim(path.Clean("/"+opts.Root), "/")
	a := &analysis{s: s, root: root, votes: map[string][]vote{}, multi: map[string]map[string][]Evidence{}, special: map[string]Finding{}}

	a.detectWorkspace()
	pkg := a.detectNode()
	a.detectGo()
	a.detectOtherLanguages()
	a.detectFrameworkConfigs(pkg)
	a.detectContainers()
	a.detectConfiguration()
	a.detectProjectManifest()

	res := Result{AnalyzerVersion: Version, Root: root, Warnings: a.warnings}
	for _, kind := range KindOrder {
		if f, ok := a.special[kind]; ok {
			res.Findings = append(res.Findings, normalizeFinding(f))
			continue
		}
		if m, ok := a.multi[kind]; ok {
			res.Findings = append(res.Findings, multiFinding(kind, m))
			continue
		}
		res.Findings = append(res.Findings, decide(kind, a.votes[kind], a.notApplicable(kind)))
	}
	if res.Warnings == nil {
		res.Warnings = []string{}
	}
	return res
}

// --- file access --------------------------------------------------------------

// rel returns the root-relative path for p, or ok=false outside the root.
func (a *analysis) rel(p string) (string, bool) {
	if a.root == "" {
		return p, true
	}
	if strings.HasPrefix(p, a.root+"/") {
		return strings.TrimPrefix(p, a.root+"/"), true
	}
	return "", false
}

func (a *analysis) abs(rel string) string {
	if a.root == "" {
		return rel
	}
	return a.root + "/" + rel
}

// file returns a root-level file's content (only retained, non-sensitive text).
func (a *analysis) file(rel string) (snapshot.File, bool) {
	f, ok := a.s.Lookup(a.abs(rel))
	if !ok || sensitive(rel) {
		return snapshot.File{}, false
	}
	return f, true
}

func (a *analysis) exists(rel string) bool { _, ok := a.file(rel); return ok }

var ignoredDirs = map[string]bool{"node_modules": true, "vendor": true, ".git": true, "dist": true, "build": true, ".next": true, "coverage": true, "testdata": true}

// walk visits files under the root, skipping dependency/build directories and sensitive files.
func (a *analysis) walk(fn func(rel string, f snapshot.File)) {
	for _, f := range a.s.Files {
		rel, ok := a.rel(f.Path)
		if !ok || sensitive(rel) {
			continue
		}
		skip := false
		for _, part := range strings.Split(path.Dir(rel), "/") {
			if ignoredDirs[part] {
				skip = true
				break
			}
		}
		if !skip {
			fn(rel, f)
		}
	}
}

// sensitive files are never read: they may contain real secrets.
func sensitive(rel string) bool {
	base := strings.ToLower(path.Base(rel))
	switch {
	case base == ".env.example", base == ".env.sample", base == ".env.template", base == ".env.dist":
		return false
	case base == ".env", strings.HasPrefix(base, ".env."), strings.HasSuffix(base, ".pem"), strings.HasSuffix(base, ".key"),
		strings.HasPrefix(base, "id_rsa"), strings.HasPrefix(base, "id_ed25519"), base == ".npmrc", base == ".netrc":
		return true
	}
	return false
}

func (a *analysis) vote(kind, value string, w float64, e Evidence) {
	e.Value = value
	if e.Effect == "" {
		e.Effect = "supports"
	}
	a.votes[kind] = append(a.votes[kind], vote{value: value, weight: w, evidence: e})
}

func (a *analysis) addMulti(kind, value string, e Evidence) {
	if a.multi[kind] == nil {
		a.multi[kind] = map[string][]Evidence{}
	}
	e.Effect = "supports"
	e.Value = value
	a.multi[kind][value] = append(a.multi[kind][value], e)
}

// lineOf returns the 1-based line containing needle, or "".
func lineOf(content []byte, needle string) string {
	i := bytes.Index(content, []byte(needle))
	if i < 0 {
		return ""
	}
	return strconv.Itoa(bytes.Count(content[:i], []byte("\n")) + 1)
}

// --- workspace / monorepo -------------------------------------------------------

func (a *analysis) detectWorkspace() {
	var ev []Evidence
	value := ""
	for _, c := range []struct{ file, value, why string }{
		{"pnpm-workspace.yaml", "pnpm-workspace", "pnpm workspace definition"},
		{"turbo.json", "turborepo", "Turborepo configuration"},
		{"nx.json", "nx", "Nx workspace configuration"},
		{"lerna.json", "lerna", "Lerna configuration"},
		{"go.work", "go-workspace", "Go workspace file"},
	} {
		if a.exists(c.file) {
			ev = append(ev, Evidence{Source: SourceManifest, Path: a.abs(c.file), Effect: "supports", Rule: "workspace.file", Explanation: c.why, Value: c.value})
			if value == "" {
				value = c.value
			}
		}
	}
	if f, ok := a.file("package.json"); ok && f.Retained && bytes.Contains(f.Content, []byte(`"workspaces"`)) {
		ev = append(ev, Evidence{Source: SourceManifest, Path: a.abs("package.json"), Lines: lineOf(f.Content, `"workspaces"`), Effect: "supports", Rule: "workspace.package_json", Explanation: "package.json declares workspaces", Value: "npm-workspaces"})
		if value == "" {
			value = "npm-workspaces"
		}
	}

	// Application roots: directories (≤ 3 levels) with their own app manifest.
	rootHasApp := a.exists("package.json") || a.exists("go.mod") || a.exists("Dockerfile")
	var roots []string
	if !rootHasApp || value != "" {
		seen := map[string]bool{}
		a.walk(func(rel string, _ snapshot.File) {
			base, dir := path.Base(rel), path.Dir(rel)
			if dir == "." || strings.Count(dir, "/") > 2 || seen[dir] {
				return
			}
			if base == "package.json" || base == "go.mod" || base == "Dockerfile" {
				seen[dir] = true
				roots = append(roots, a.abs(dir))
			}
		})
		sort.Strings(roots)
	}

	switch {
	case value == "" && rootHasApp:
		a.special[KindWorkspace] = Finding{Kind: KindWorkspace, State: StateNotApplicable, Reason: "single application at the analyzed root"}
	case !rootHasApp && len(roots) > 0:
		a.special[KindWorkspace] = Finding{Kind: KindWorkspace, State: StateAmbiguous, Value: value, Candidates: roots, Confidence: 0.5, Evidence: ev,
			Reason: "no application at the analyzed root; choose an application directory (axiom.yaml app.root)"}
		a.warnings = append(a.warnings, "application root is ambiguous: "+strings.Join(roots, ", "))
	case value != "":
		a.special[KindWorkspace] = Finding{Kind: KindWorkspace, State: StateDetected, Value: value, Candidates: roots, Confidence: 0.9, Evidence: ev}
	default:
		a.special[KindWorkspace] = Finding{Kind: KindWorkspace, State: StateNotDetected, Reason: "no application manifest found"}
	}
}

// --- Node.js ------------------------------------------------------------------

type packageJSON struct {
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
	Scripts         map[string]string `json:"scripts"`
	Engines         map[string]string `json:"engines"`
	PackageManager  string            `json:"packageManager"`
	raw             []byte
	path            string
}

func (p *packageJSON) has(dep string) bool {
	if p == nil {
		return false
	}
	_, a := p.Dependencies[dep]
	_, b := p.DevDependencies[dep]
	return a || b
}

func (a *analysis) detectNode() *packageJSON {
	f, ok := a.file("package.json")
	if !ok {
		return nil
	}
	pkgPath := a.abs("package.json")
	if !f.Retained {
		a.warnings = append(a.warnings, "package.json too large to analyze")
		return nil
	}
	var pkg packageJSON
	if err := json.Unmarshal(f.Content, &pkg); err != nil {
		a.warnings = append(a.warnings, "package.json is not valid JSON")
		return nil
	}
	pkg.raw, pkg.path = f.Content, pkgPath

	// Language: TypeScript when declared, otherwise JavaScript.
	if tf, ok := a.file("tsconfig.json"); ok {
		_ = tf
		a.vote(KindLanguage, "TypeScript", 0.9, Evidence{Source: SourceManifest, Path: a.abs("tsconfig.json"), Rule: "node.language.tsconfig", Explanation: "tsconfig.json configures TypeScript"})
	}
	if pkg.has("typescript") {
		a.vote(KindLanguage, "TypeScript", 0.7, Evidence{Source: SourceManifest, Path: pkgPath, Lines: lineOf(f.Content, `"typescript"`), Rule: "node.language.typescript_dep", Explanation: "typescript is a dependency"})
	}
	if len(a.votes[KindLanguage]) == 0 {
		a.vote(KindLanguage, "JavaScript", 0.8, Evidence{Source: SourceManifest, Path: pkgPath, Rule: "node.language.package_json", Explanation: "package.json defines a Node.js project"})
	}

	// Runtime version.
	if v := pkg.Engines["node"]; v != "" {
		a.vote(KindRuntimeVersion, "node "+v, 1, Evidence{Source: SourceManifest, Path: pkgPath, Lines: lineOf(f.Content, `"node"`), Rule: "node.version.engines", Explanation: "engines.node declares the Node.js version"})
	} else {
		for _, name := range []string{".nvmrc", ".node-version"} {
			if vf, ok := a.file(name); ok && vf.Retained {
				if v := strings.TrimSpace(string(vf.Content)); v != "" && len(v) < 32 {
					a.vote(KindRuntimeVersion, "node "+strings.TrimPrefix(v, "v"), 0.9, Evidence{Source: SourceManifest, Path: a.abs(name), Lines: "1", Rule: "node.version.file", Explanation: name + " pins the Node.js version"})
				}
			}
		}
	}

	// Package manager: packageManager field is decisive; lockfiles are strong.
	if pm := pkg.PackageManager; pm != "" {
		name, _, _ := strings.Cut(pm, "@")
		switch name {
		case "npm", "pnpm", "yarn", "bun":
			a.vote(KindPackageManager, name, 1, Evidence{Source: SourceManifest, Path: pkgPath, Lines: lineOf(f.Content, `"packageManager"`), Rule: "node.pm.package_manager_field", Explanation: "packageManager field declares " + pm})
		}
	}
	for _, lf := range []struct{ file, pm string }{{"pnpm-lock.yaml", "pnpm"}, {"yarn.lock", "yarn"}, {"package-lock.json", "npm"}, {"npm-shrinkwrap.json", "npm"}, {"bun.lockb", "bun"}, {"bun.lock", "bun"}} {
		if a.existsAny(lf.file) {
			a.vote(KindPackageManager, lf.pm, 0.9, Evidence{Source: SourceLockfile, Path: a.abs(lf.file), Rule: "node.pm.lockfile", Explanation: lf.file + " lockfile is present"})
		}
	}
	if len(a.votes[KindPackageManager]) == 0 {
		a.vote(KindPackageManager, "npm", 0.4, Evidence{Source: SourceManifest, Path: pkgPath, Rule: "node.pm.no_lockfile", Explanation: "no lockfile; npm is the Node.js default"})
	}

	// Framework from dependencies.
	for _, fw := range []struct{ dep, name string }{
		{"next", "Next.js"}, {"vite", "Vite"}, {"@remix-run/node", "Remix"}, {"nuxt", "Nuxt"}, {"astro", "Astro"},
		{"@sveltejs/kit", "SvelteKit"}, {"@nestjs/core", "NestJS"}, {"express", "Express"}, {"fastify", "Fastify"}, {"koa", "Koa"},
	} {
		if pkg.has(fw.dep) {
			w := 0.8
			if fw.name == "Vite" && pkg.has("next") {
				w = 0.3 // vite as a tool inside another framework
			}
			if (fw.name == "Express" || fw.name == "Fastify" || fw.name == "Koa") && (pkg.has("next") || pkg.has("@nestjs/core")) {
				continue // server library used by a higher-level framework
			}
			a.vote(KindFramework, fw.name, w, Evidence{Source: SourceManifest, Path: pkgPath, Lines: lineOf(f.Content, `"`+fw.dep+`"`), Rule: "node.framework.dependency", Explanation: fw.dep + " is a dependency"})
		}
	}

	// Scripts.
	for _, sc := range []struct{ name, kind string }{{"build", KindBuildScript}, {"start", KindStartScript}} {
		if cmd := strings.TrimSpace(pkg.Scripts[sc.name]); cmd != "" {
			a.vote(sc.kind, sc.name, 0.95, Evidence{Source: SourceScript, Path: pkgPath, Lines: lineOf(f.Content, `"`+sc.name+`"`), Rule: "node.script." + sc.name, Explanation: fmt.Sprintf("scripts.%s runs %q", sc.name, truncate(cmd, 120))})
			if sc.kind == KindStartScript {
				a.portFromCommand(cmd, pkgPath, f.Content)
			}
		}
	}
	return &pkg
}

func (a *analysis) existsAny(rel string) bool { _, ok := a.s.Lookup(a.abs(rel)); return ok }

var portFlagRe = regexp.MustCompile(`(?:--port[= ]|-p[= ]?|PORT=)(\d{2,5})\b`)

func (a *analysis) portFromCommand(cmd, p string, content []byte) {
	if m := portFlagRe.FindStringSubmatch(cmd); m != nil {
		a.votePort(m[1], 0.7, Evidence{Source: SourceScript, Path: p, Lines: lineOf(content, m[0]), Rule: "port.start_script", Explanation: "start script sets port " + m[1]})
	}
}

func (a *analysis) votePort(v string, w float64, e Evidence) {
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 || n > 65535 {
		return
	}
	a.vote(KindPort, strconv.Itoa(n), w, e)
}

// --- Go -------------------------------------------------------------------------

var goMainRe = regexp.MustCompile(`(?m)^package\s+main\s*$`)
var goMainFuncRe = regexp.MustCompile(`(?m)^func\s+main\(\)`)
var goVersionRe = regexp.MustCompile(`(?m)^go\s+(\d+\.\d+(?:\.\d+)?)\s*$`)
var goListenRe = regexp.MustCompile(`(?:ListenAndServe(?:TLS)?\(|Addr:\s*|\.Run\(|\.Start\(|Listen\()\s*"(?:[\w.\-]*)?:(\d{2,5})"`)

func (a *analysis) detectGo() {
	f, ok := a.file("go.mod")
	if !ok {
		return
	}
	p := a.abs("go.mod")
	a.vote(KindLanguage, "Go", 1, Evidence{Source: SourceManifest, Path: p, Lines: "1", Rule: "go.language.go_mod", Explanation: "go.mod defines a Go module"})
	a.vote(KindPackageManager, "go", 1, Evidence{Source: SourceManifest, Path: p, Rule: "go.pm.modules", Explanation: "Go modules manage dependencies"})
	if !f.Retained {
		return
	}
	if m := goVersionRe.FindSubmatch(f.Content); m != nil {
		a.vote(KindRuntimeVersion, "go "+string(m[1]), 1, Evidence{Source: SourceManifest, Path: p, Lines: lineOf(f.Content, string(m[0])), Rule: "go.version.go_directive", Explanation: "go directive declares Go " + string(m[1])})
	}
	for _, fw := range []struct{ mod, name string }{{"github.com/gin-gonic/gin", "Gin"}, {"github.com/labstack/echo", "Echo"}, {"github.com/gofiber/fiber", "Fiber"}, {"github.com/go-chi/chi", "Chi"}} {
		if l := lineOf(f.Content, fw.mod); l != "" {
			a.vote(KindFramework, fw.name, 0.8, Evidence{Source: SourceManifest, Path: p, Lines: l, Rule: "go.framework.require", Explanation: fw.mod + " is required"})
		}
	}
	// Entrypoints (main packages) and listening port from source (bounded scan).
	scanned := 0
	a.walk(func(rel string, sf snapshot.File) {
		if scanned >= 200 || !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") || !sf.Retained {
			return
		}
		scanned++
		if goMainRe.Match(sf.Content) && goMainFuncRe.Match(sf.Content) {
			dir := path.Dir(rel)
			pkg := "."
			if dir != "." {
				pkg = "./" + dir
			}
			a.addMulti(KindEntrypoint, pkg, Evidence{Source: SourceSource, Path: a.abs(rel), Lines: lineOf(sf.Content, "func main("), Rule: "go.entrypoint.main", Explanation: "package main with func main()"})
		}
		if m := goListenRe.FindSubmatch(sf.Content); m != nil {
			a.votePort(string(m[1]), 0.6, Evidence{Source: SourceSource, Path: a.abs(rel), Lines: lineOf(sf.Content, string(m[0])), Rule: "port.go_listen", Explanation: "server listens on :" + string(m[1])})
		}
	})
}

// --- other languages (detected, not V0.1 presets) ---------------------------------

func (a *analysis) detectOtherLanguages() {
	for _, l := range []struct{ file, lang string }{
		{"pyproject.toml", "Python"}, {"requirements.txt", "Python"}, {"setup.py", "Python"}, {"Pipfile", "Python"},
		{"Gemfile", "Ruby"}, {"composer.json", "PHP"}, {"pom.xml", "Java"}, {"build.gradle", "Java"}, {"build.gradle.kts", "Kotlin"},
		{"Cargo.toml", "Rust"}, {"mix.exs", "Elixir"},
	} {
		if a.exists(l.file) {
			a.vote(KindLanguage, l.lang, 0.95, Evidence{Source: SourceManifest, Path: a.abs(l.file), Rule: "lang.manifest", Explanation: l.file + " indicates a " + l.lang + " project"})
		}
	}
	for _, fw := range []struct{ file, needle, name string }{
		{"requirements.txt", "django", "Django"}, {"requirements.txt", "flask", "Flask"}, {"requirements.txt", "fastapi", "FastAPI"},
		{"pyproject.toml", "django", "Django"}, {"pyproject.toml", "fastapi", "FastAPI"}, {"Gemfile", "rails", "Rails"},
		{"composer.json", "laravel/framework", "Laravel"}, {"pom.xml", "spring-boot", "Spring Boot"},
	} {
		if f, ok := a.file(fw.file); ok && f.Retained {
			if l := lineOf(bytes.ToLower(f.Content), fw.needle); l != "" {
				a.vote(KindFramework, fw.name, 0.8, Evidence{Source: SourceManifest, Path: a.abs(fw.file), Lines: l, Rule: "lang.framework", Explanation: fw.needle + " is a dependency"})
			}
		}
	}
}

// --- framework configuration files --------------------------------------------

func (a *analysis) detectFrameworkConfigs(pkg *packageJSON) {
	for _, c := range []struct{ prefix, name string }{{"next.config.", "Next.js"}, {"vite.config.", "Vite"}, {"nuxt.config.", "Nuxt"}, {"astro.config.", "Astro"}, {"svelte.config.", "SvelteKit"}, {"remix.config.", "Remix"}} {
		for _, ext := range []string{"js", "mjs", "cjs", "ts", "mts"} {
			name := c.prefix + ext
			if a.existsAny(name) {
				w := 0.9
				if c.name == "Vite" && pkg.has("next") {
					w = 0.3
				}
				a.vote(KindFramework, c.name, w, Evidence{Source: SourceFrameworkConfig, Path: a.abs(name), Rule: "framework.config_file", Explanation: name + " configures " + c.name})
			}
		}
	}
}

// --- containers -----------------------------------------------------------------

var exposeRe = regexp.MustCompile(`(?mi)^\s*EXPOSE\s+(\d{2,5})`)

func (a *analysis) detectContainers() {
	if f, ok := a.file("Dockerfile"); ok {
		p := a.abs("Dockerfile")
		a.vote(KindContainer, "dockerfile", 0.95, Evidence{Source: SourceDockerfile, Path: p, Rule: "container.dockerfile", Explanation: "a Dockerfile defines the image"})
		if f.Retained {
			if m := exposeRe.FindSubmatch(f.Content); m != nil {
				a.votePort(string(m[1]), 0.8, Evidence{Source: SourceDockerfile, Path: p, Lines: lineOf(f.Content, string(bytes.TrimSpace(m[0]))), Rule: "port.dockerfile_expose", Explanation: "Dockerfile EXPOSE " + string(m[1])})
			}
		}
	}
	for _, name := range []string{"compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml"} {
		f, ok := a.file(name)
		if !ok {
			continue
		}
		p := a.abs(name)
		a.vote(KindContainer, "compose", 0.95, Evidence{Source: SourceCompose, Path: p, Rule: "container.compose", Explanation: name + " defines services"})
		if !f.Retained {
			break
		}
		var doc struct {
			Services map[string]struct {
				Ports []any  `yaml:"ports"`
				Build any    `yaml:"build"`
				Image string `yaml:"image"`
			} `yaml:"services"`
		}
		if err := yaml.Unmarshal(f.Content, &doc); err != nil {
			a.warnings = append(a.warnings, name+" is not valid YAML")
			break
		}
		names := make([]string, 0, len(doc.Services))
		for n := range doc.Services {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			svc := doc.Services[n]
			a.addMulti(KindServices, n, Evidence{Source: SourceCompose, Path: p, Lines: lineOf(f.Content, n+":"), Rule: "compose.service", Explanation: "service " + n})
			if svc.Build != nil && len(svc.Ports) > 0 {
				a.vote(KindPublicService, n, 0.8, Evidence{Source: SourceCompose, Path: p, Lines: lineOf(f.Content, n+":"), Rule: "compose.public_service", Explanation: "service " + n + " is built from the repository and publishes ports"})
			}
			for _, port := range svc.Ports {
				if container := composeContainerPort(fmt.Sprint(port)); container != "" && svc.Build != nil {
					a.votePort(container, 0.7, Evidence{Source: SourceCompose, Path: p, Lines: lineOf(f.Content, fmt.Sprint(port)), Rule: "port.compose", Explanation: "service " + n + " publishes container port " + container})
				}
			}
		}
		break
	}
}

// composeContainerPort extracts the container port from "8080:3000", "3000", "127.0.0.1:80:8080/tcp".
func composeContainerPort(s string) string {
	s, _, _ = strings.Cut(s, "/")
	parts := strings.Split(s, ":")
	last := parts[len(parts)-1]
	if _, err := strconv.Atoi(last); err != nil {
		return ""
	}
	return last
}

// --- configuration requirements (names only) ------------------------------------

var envKeyRe = regexp.MustCompile(`^\s*(?:export\s+)?([A-Z_][A-Z0-9_]*)\s*=`)

func (a *analysis) detectConfiguration() {
	for _, name := range []string{".env.example", ".env.sample", ".env.template", ".env.dist"} {
		f, ok := a.file(name)
		if !ok || !f.Retained {
			continue
		}
		sc := bufio.NewScanner(bytes.NewReader(f.Content))
		line := 0
		for sc.Scan() {
			line++
			if m := envKeyRe.FindStringSubmatch(sc.Text()); m != nil {
				// Only the variable name is recorded, never the example value.
				a.addMulti(KindConfiguration, m[1], Evidence{Source: SourceManifest, Path: a.abs(name), Lines: strconv.Itoa(line), Rule: "config.env_example", Explanation: m[1] + " is listed in " + name})
			}
		}
	}
}

func (a *analysis) detectProjectManifest() {
	for _, name := range []string{"axiom.yaml", "axiom.yml"} {
		if a.existsAny(name) {
			a.special[KindManifest] = Finding{Kind: KindManifest, State: StateDetected, Value: a.abs(name), Confidence: 1,
				Evidence: []Evidence{{Source: SourceAxiomYAML, Path: a.abs(name), Effect: "supports", Rule: "manifest.present", Explanation: "project manifest present; validated by the manifest parser"}}}
			return
		}
	}
	a.special[KindManifest] = Finding{Kind: KindManifest, State: StateNotDetected, Reason: "no axiom.yaml (optional)"}
}

// notApplicable reports kinds that make no sense for the detected stack.
func (a *analysis) notApplicable(kind string) string {
	hasNode := len(a.votes[KindLanguage]) > 0 && (a.hasVote(KindLanguage, "TypeScript") || a.hasVote(KindLanguage, "JavaScript"))
	switch kind {
	case KindBuildScript, KindStartScript:
		if !hasNode {
			return "scripts apply to Node.js projects"
		}
	case KindEntrypoint:
		if !a.hasVote(KindLanguage, "Go") {
			return "entrypoints are detected for Go modules"
		}
	case KindPublicService:
		if !a.hasVote(KindContainer, "compose") {
			return "applies to Compose projects"
		}
	case KindPackageManager:
		if len(a.votes[KindLanguage]) > 0 && !hasNode && !a.hasVote(KindLanguage, "Go") {
			return "no supported package manager for this language"
		}
	}
	return ""
}

func (a *analysis) hasVote(kind, value string) bool {
	for _, v := range a.votes[kind] {
		if v.value == value {
			return true
		}
	}
	return false
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
