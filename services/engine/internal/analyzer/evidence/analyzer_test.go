package evidence

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/digitaleflex/axiom/services/engine/internal/analyzer/snapshot"
)

// snap builds a snapshot from path -> content ("" content = metadata only).
func snap(files map[string]string) snapshot.Snapshot {
	s := snapshot.Snapshot{Ref: "main", Commit: strings.Repeat("a", 40)}
	for p, c := range files {
		f := snapshot.File{Path: p, Size: int64(len(c))}
		if c != "" {
			f.Content, f.Retained = []byte(c), true
		}
		s.Files = append(s.Files, f)
	}
	sort.Slice(s.Files, func(i, j int) bool { return s.Files[i].Path < s.Files[j].Path })
	return s
}

func get(t *testing.T, r Result, kind string) Finding {
	t.Helper()
	f, ok := r.Get(kind)
	if !ok {
		t.Fatalf("missing finding %s", kind)
	}
	return f
}

func assertFinding(t *testing.T, r Result, kind, state, value string) Finding {
	t.Helper()
	f := get(t, r, kind)
	if f.State != state || f.Value != value {
		t.Fatalf("%s = %s/%q (candidates %v), want %s/%q", kind, f.State, f.Value, f.Candidates, state, value)
	}
	return f
}

const nextPkg = `{
  "name": "acme-web",
  "packageManager": "pnpm@9.1.0",
  "engines": { "node": ">=20" },
  "scripts": { "build": "next build", "start": "next start -p 3000" },
  "dependencies": { "next": "14.2.0", "react": "18.3.0" },
  "devDependencies": { "typescript": "5.4.0" }
}`

func TestNextJSWithEvidence(t *testing.T) {
	r := Analyze(snap(map[string]string{
		"package.json": nextPkg, "pnpm-lock.yaml": "lockfileVersion: '9.0'", "tsconfig.json": "{}",
		"next.config.mjs": "export default {}", "app/page.tsx": "x",
		".env.example": "DATABASE_URL=postgres://example\n# comment\nNEXTAUTH_SECRET=\n",
	}), Options{})

	lang := assertFinding(t, r, KindLanguage, StateDetected, "TypeScript")
	if lang.Confidence < 0.9 || len(lang.Evidence) == 0 {
		t.Fatalf("language confidence/evidence = %+v", lang)
	}
	fw := assertFinding(t, r, KindFramework, StateDetected, "Next.js")
	if len(fw.Evidence) != 2 || fw.Evidence[0].Path != "next.config.mjs" || fw.Evidence[1].Lines == "" {
		t.Fatalf("framework evidence = %+v", fw.Evidence)
	}
	assertFinding(t, r, KindPackageManager, StateDetected, "pnpm")
	assertFinding(t, r, KindRuntimeVersion, StateDetected, "node >=20")
	assertFinding(t, r, KindBuildScript, StateDetected, "build")
	assertFinding(t, r, KindStartScript, StateDetected, "start")
	assertFinding(t, r, KindPort, StateDetected, "3000")
	assertFinding(t, r, KindContainer, StateNotDetected, "")
	cfg := get(t, r, KindConfiguration)
	if !reflect.DeepEqual(cfg.Values, []string{"DATABASE_URL", "NEXTAUTH_SECRET"}) {
		t.Fatalf("configuration = %v", cfg.Values)
	}
	raw, _ := json.Marshal(r)
	if strings.Contains(string(raw), "postgres://example") {
		t.Fatal("example values must never be recorded")
	}
	for _, f := range r.Findings {
		if f.Confidence < 0 || f.Confidence > 1 {
			t.Fatalf("confidence out of bounds: %+v", f)
		}
		if f.State == StateDetected && len(f.Evidence) == 0 {
			t.Fatalf("detected finding without evidence: %+v", f)
		}
	}
}

func TestConflictingLockfilesAreAmbiguous(t *testing.T) {
	r := Analyze(snap(map[string]string{
		"package.json": `{"dependencies":{"vite":"5"}}`, "package-lock.json": "{}", "pnpm-lock.yaml": "x",
	}), Options{})
	pm := get(t, r, KindPackageManager)
	if pm.State != StateAmbiguous || !reflect.DeepEqual(pm.Candidates, []string{"npm", "pnpm"}) || pm.Value != "" {
		t.Fatalf("package manager = %+v", pm)
	}
	if len(pm.Evidence) != 2 {
		t.Fatalf("each candidate must keep its evidence: %+v", pm.Evidence)
	}
}

func TestPackageManagerFieldResolvesConflict(t *testing.T) {
	r := Analyze(snap(map[string]string{
		"package.json": `{"packageManager":"yarn@4.1.0"}`, "package-lock.json": "{}", "yarn.lock": "x",
	}), Options{})
	pm := assertFinding(t, r, KindPackageManager, StateDetected, "yarn")
	conflicts := 0
	for _, e := range pm.Evidence {
		if e.Effect == "conflicts" {
			conflicts++
		}
	}
	if conflicts != 1 || pm.Confidence >= 1 {
		t.Fatalf("conflict must be recorded and lower confidence: %+v", pm)
	}
}

func TestMultipleFrameworksAmbiguous(t *testing.T) {
	r := Analyze(snap(map[string]string{
		"package.json": `{"dependencies":{"astro":"4","@sveltejs/kit":"2"}}`,
	}), Options{})
	if f := get(t, r, KindFramework); f.State != StateAmbiguous || len(f.Candidates) != 2 {
		t.Fatalf("framework = %+v", f)
	}
	// Vite inside a Next.js app is a tool, not a competing framework.
	r = Analyze(snap(map[string]string{"package.json": `{"dependencies":{"next":"14"},"devDependencies":{"vite":"5"}}`, "next.config.js": "x"}), Options{})
	assertFinding(t, r, KindFramework, StateDetected, "Next.js")
}

func TestMissingManifests(t *testing.T) {
	r := Analyze(snap(map[string]string{"README.md": "# hello", "index.html": "<html>"}), Options{})
	assertFinding(t, r, KindLanguage, StateNotDetected, "")
	assertFinding(t, r, KindFramework, StateNotDetected, "")
	if w := get(t, r, KindWorkspace); w.State != StateNotDetected {
		t.Fatalf("workspace = %+v", w)
	}
}

func TestDockerOnlyProject(t *testing.T) {
	r := Analyze(snap(map[string]string{
		"Dockerfile":       "FROM python:3.12\nCOPY . .\nEXPOSE 8000\nCMD [\"python\", \"app.py\"]\n",
		"requirements.txt": "flask==3.0\n",
	}), Options{})
	assertFinding(t, r, KindContainer, StateDetected, "dockerfile")
	port := assertFinding(t, r, KindPort, StateDetected, "8000")
	if port.Evidence[0].Lines != "3" || port.Evidence[0].Source != SourceDockerfile {
		t.Fatalf("port evidence = %+v", port.Evidence)
	}
	assertFinding(t, r, KindLanguage, StateDetected, "Python")
	if f := get(t, r, KindFramework); f.State != StateUnsupported || f.Value != "Flask" || f.Reason == "" {
		t.Fatalf("unsupported framework must be explicit: %+v", f)
	}
	if f := get(t, r, KindBuildScript); f.State != StateNotApplicable {
		t.Fatalf("scripts not applicable for Python: %+v", f)
	}
}

func TestComposeServices(t *testing.T) {
	r := Analyze(snap(map[string]string{
		"compose.yaml": "services:\n  web:\n    build: .\n    ports:\n      - \"8080:3000\"\n  redis:\n    image: redis:7\n    ports: [\"6379:6379\"]\n",
		"package.json": `{"dependencies":{"express":"4"},"scripts":{"start":"node server.js"}}`,
	}), Options{})
	assertFinding(t, r, KindContainer, StateDetected, "compose")
	if svc := get(t, r, KindServices); !reflect.DeepEqual(svc.Values, []string{"redis", "web"}) {
		t.Fatalf("services = %v", svc.Values)
	}
	assertFinding(t, r, KindPort, StateDetected, "3000") // container port of the built service only
	assertFinding(t, r, KindFramework, StateDetected, "Express")
}

func TestMonorepoRootAmbiguity(t *testing.T) {
	files := map[string]string{
		"pnpm-workspace.yaml":     "packages: ['apps/*']",
		"apps/web/package.json":   `{"dependencies":{"next":"14"},"scripts":{"build":"next build","start":"next start"}}`,
		"apps/web/next.config.js": "x",
		"apps/api/go.mod":         "module api\n\ngo 1.23\n",
		"apps/api/main.go":        "package main\nfunc main(){ http.ListenAndServe(\":8080\", nil) }",
	}
	r := Analyze(snap(files), Options{})
	ws := get(t, r, KindWorkspace)
	if ws.State != StateAmbiguous || !reflect.DeepEqual(ws.Candidates, []string{"apps/api", "apps/web"}) || len(r.Warnings) == 0 {
		t.Fatalf("workspace = %+v warnings=%v", ws, r.Warnings)
	}
	// Choosing a root analyzes only that application.
	r = Analyze(snap(files), Options{Root: "apps/api"})
	assertFinding(t, r, KindLanguage, StateDetected, "Go")
	assertFinding(t, r, KindRuntimeVersion, StateDetected, "go 1.23")
	port := assertFinding(t, r, KindPort, StateDetected, "8080")
	if port.Evidence[0].Path != "apps/api/main.go" {
		t.Fatalf("evidence paths must be repository-relative: %+v", port.Evidence)
	}
	r = Analyze(snap(files), Options{Root: "apps/web"})
	assertFinding(t, r, KindFramework, StateDetected, "Next.js")
	if r.Root != "apps/web" {
		t.Fatalf("root = %q", r.Root)
	}
}

func TestGoService(t *testing.T) {
	r := Analyze(snap(map[string]string{
		"go.mod":             "module example.com/svc\n\ngo 1.23.2\n\nrequire github.com/go-chi/chi/v5 v5.0.12\n",
		"cmd/server/main.go": "package main\nfunc main() { srv := &http.Server{Addr: \":9000\"}; _ = srv }",
		"internal/x_test.go": "package x\n// ListenAndServe(\":1\")",
		"vendor/lib/lib.go":  "package lib\n// ListenAndServe(\":7777\")",
	}), Options{})
	assertFinding(t, r, KindLanguage, StateDetected, "Go")
	assertFinding(t, r, KindPackageManager, StateDetected, "go")
	assertFinding(t, r, KindFramework, StateDetected, "Chi")
	assertFinding(t, r, KindPort, StateDetected, "9000")
	if f := get(t, r, KindStartScript); f.State != StateNotApplicable {
		t.Fatalf("Node scripts not applicable to Go: %+v", f)
	}
}

func TestSecretsAreNeverRead(t *testing.T) {
	r := Analyze(snap(map[string]string{
		"package.json": `{"scripts":{"start":"node index.js"}}`,
		".env":         "SECRET_TOKEN=abc\nDATABASE_URL=postgres://u:p@h/db",
		".env.local":   "OTHER=1",
	}), Options{})
	raw, _ := json.Marshal(r)
	if strings.Contains(string(raw), "SECRET_TOKEN") || strings.Contains(string(raw), ".env.local") || strings.Contains(string(raw), "\".env\"") {
		t.Fatalf("secret files must never be read: %s", raw)
	}
}

func TestDeterministic(t *testing.T) {
	files := map[string]string{"package.json": nextPkg, "pnpm-lock.yaml": "x", "yarn.lock": "y", "next.config.ts": "x", "Dockerfile": "EXPOSE 3000"}
	first, _ := json.Marshal(Analyze(snap(files), Options{}))
	for i := 0; i < 20; i++ {
		again, _ := json.Marshal(Analyze(snap(files), Options{}))
		if string(again) != string(first) {
			t.Fatal("analysis must be deterministic")
		}
	}
	var r Result
	_ = json.Unmarshal(first, &r)
	if len(r.Findings) != len(KindOrder) || r.AnalyzerVersion != Version {
		t.Fatalf("result shape: %d findings, version %s", len(r.Findings), r.AnalyzerVersion)
	}
}

func TestInvalidManifestsWarn(t *testing.T) {
	r := Analyze(snap(map[string]string{"package.json": "{not json", "compose.yml": "services: [::"}), Options{})
	if len(r.Warnings) < 2 {
		t.Fatalf("warnings = %v", r.Warnings)
	}
}
