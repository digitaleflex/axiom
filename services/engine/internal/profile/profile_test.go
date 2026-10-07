package profile

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/digitaleflex/axiom/services/engine/internal/analyzer/evidence"
	"github.com/digitaleflex/axiom/services/engine/internal/analyzer/snapshot"
)

var update = flag.Bool("update", false, "rewrite golden files")

func snap(files map[string]string) snapshot.Snapshot {
	s := snapshot.Snapshot{}
	for p, c := range files {
		s.Files = append(s.Files, snapshot.File{Path: p, Size: int64(len(c)), Content: []byte(c), Retained: true})
	}
	sort.Slice(s.Files, func(i, j int) bool { return s.Files[i].Path < s.Files[j].Path })
	return s
}

func build(files map[string]string, in Inputs) Profile {
	src := Source{RepositoryID: "repo_1", Ref: "main", Commit: strings.Repeat("a", 40)}
	return Build(evidence.Analyze(snap(files), evidence.Options{}), src, in)
}

var fixtures = map[string]map[string]string{
	"nextjs": {
		"package.json":   `{"packageManager":"pnpm@9.1.0","engines":{"node":"20"},"scripts":{"build":"next build","start":"next start"},"dependencies":{"next":"14.2.0"},"devDependencies":{"typescript":"5"}}`,
		"pnpm-lock.yaml": "x", "tsconfig.json": "{}", "next.config.mjs": "x",
		".env.example": "DATABASE_URL=\nNEXTAUTH_SECRET=\n",
	},
	"vite": {
		"package.json": `{"scripts":{"build":"vite build"},"devDependencies":{"vite":"5"}}`, "package-lock.json": "{}", "vite.config.ts": "x",
	},
	"go": {
		"go.mod":             "module example.com/svc\n\ngo 1.23\n",
		"cmd/server/main.go": "package main\nfunc main() { http.ListenAndServe(\":9000\", nil) }",
	},
	"docker": {
		"Dockerfile": "FROM python:3.12\nEXPOSE 8000\n", "requirements.txt": "django==5\n",
	},
	"unsupported": {
		"pyproject.toml": "[project]\ndependencies=['fastapi']\n",
	},
}

func TestGoldenProfiles(t *testing.T) {
	for name, files := range fixtures {
		t.Run(name, func(t *testing.T) {
			got, _ := json.MarshalIndent(build(files, Inputs{}), "", "  ")
			path := filepath.Join("testdata", "golden", name+".json")
			if *update {
				_ = os.MkdirAll(filepath.Dir(path), 0o755)
				if err := os.WriteFile(path, append(got, '\n'), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("missing golden file (run go test -update): %v", err)
			}
			if strings.TrimSpace(string(want)) != strings.TrimSpace(string(got)) {
				t.Fatalf("profile changed for %s:\n%s", name, got)
			}
		})
	}
}

func TestReadyNextJSProfile(t *testing.T) {
	p := build(fixtures["nextjs"], Inputs{})
	if p.Status != StatusReady || p.Preset != "nextjs" || p.SchemaVersion != 1 {
		t.Fatalf("status=%s preset=%s blocking=%+v", p.Status, p.Preset, p.Blocking)
	}
	if p.BuildCommand.Value != "pnpm run build" || p.BuildCommand.Provenance != ProvenanceDetected {
		t.Fatalf("build = %+v", p.BuildCommand)
	}
	if p.Port.Value != 3000 || p.Port.Provenance != ProvenanceDefault {
		t.Fatalf("default port must be labelled default, never detected: %+v", p.Port)
	}
	if p.HealthCheck.Provenance != ProvenanceDefault || p.HealthCheck.Value.Path != "/" {
		t.Fatalf("health = %+v", p.HealthCheck)
	}
	if len(p.Configuration) != 2 || !p.Configuration[0].Secret {
		t.Fatalf("configuration = %+v", p.Configuration)
	}
	if !strings.Contains(p.Summary, "Next.js") || !strings.Contains(p.Summary, "pnpm") || !p.Deployable() {
		t.Fatalf("summary = %q", p.Summary)
	}
}

func TestUnsupportedNeverDeployable(t *testing.T) {
	p := build(fixtures["unsupported"], Inputs{})
	if p.Status != StatusUnsupported || p.Deployable() || p.Unsupported == nil || p.Unsupported.Code == "" || len(p.Unsupported.Alternatives) == 0 {
		t.Fatalf("unsupported = %+v", p)
	}
	d := build(fixtures["docker"], Inputs{})
	if d.Status != StatusReady || d.Preset != "dockerfile" || d.Port.Value != 8000 || d.Port.Provenance != ProvenanceDetected {
		t.Fatalf("Dockerfile makes any stack deployable: %+v", d)
	}
}

func TestAmbiguityBlocksUntilResolved(t *testing.T) {
	files := map[string]string{"package.json": `{"scripts":{"start":"node index.js"}}`, "package-lock.json": "{}", "yarn.lock": "x"}
	p := build(files, Inputs{})
	if p.Status != StatusNeedsReview || p.Blocking[0].Field != "packageManager" || p.Blocking[0].Code != "ambiguous" || len(p.Blocking[0].Options) != 2 {
		t.Fatalf("blocking = %+v", p.Blocking)
	}
	pm := "yarn"
	p = build(files, Inputs{Overrides: Hints{PackageManager: &pm}})
	if p.Status != StatusReady || p.PackageManager.Value != "yarn" || p.PackageManager.Provenance != ProvenanceOverride || p.StartCommand.Value != "yarn start" {
		t.Fatalf("override must resolve: %+v / %+v", p.Blocking, p.PackageManager)
	}
}

func TestOverridesAndManifestPrecedence(t *testing.T) {
	manifestPort, overridePort := 4000, 5000
	start := "node dist/server.js"
	p := build(fixtures["nextjs"], Inputs{Manifest: Hints{Port: &manifestPort}, Overrides: Hints{Port: &overridePort, StartCommand: &start}})
	if p.Port.Value != 5000 || p.Port.Provenance != ProvenanceOverride || p.Port.Replaced == nil || *p.Port.Replaced != 4000 {
		t.Fatalf("override > manifest > default: %+v", p.Port)
	}
	if p.StartCommand.Value != start || p.StartCommand.Replaced == nil || *p.StartCommand.Replaced != "pnpm start" {
		t.Fatalf("start = %+v", p.StartCommand)
	}
	bad := "echo `id`"
	p = build(fixtures["nextjs"], Inputs{Overrides: Hints{BuildCommand: &bad}})
	if p.Status != StatusNeedsReview || p.BuildCommand.Value != "pnpm run build" || p.Blocking[0].Code != "invalid_override" {
		t.Fatalf("unsafe override must be rejected, not applied: %+v %+v", p.BuildCommand, p.Blocking)
	}
}

func TestPortRules(t *testing.T) {
	// Dockerfile without EXPOSE: port required.
	p := build(map[string]string{"Dockerfile": "FROM alpine\n"}, Inputs{})
	if p.Status != StatusNeedsReview || p.Blocking[0].Field != "port" || p.Blocking[0].Code != "not_detected" {
		t.Fatalf("missing port: %+v", p.Blocking)
	}
	port := 8080
	if p = build(map[string]string{"Dockerfile": "FROM alpine\n"}, Inputs{Overrides: Hints{Port: &port}}); p.Status != StatusReady {
		t.Fatalf("port override must resolve: %+v", p.Blocking)
	}
	// Conflicting ports.
	p = build(map[string]string{"Dockerfile": "EXPOSE 3000\n", "package.json": `{"scripts":{"start":"node x --port 4000"}}`}, Inputs{})
	found := false
	for _, i := range p.Blocking {
		found = found || (i.Field == "port" && i.Code == "ambiguous")
	}
	if !found {
		t.Fatalf("conflicting ports must block: %+v", p.Blocking)
	}
}

func TestGoEntrypointsAndCompose(t *testing.T) {
	files := map[string]string{"go.mod": "module m\n\ngo 1.23\n", "cmd/api/main.go": "package main\nfunc main() {}", "cmd/worker/main.go": "package main\nfunc main() {}"}
	p := build(files, Inputs{})
	if p.Status != StatusNeedsReview || p.Blocking[0].Field != "entrypoint" {
		t.Fatalf("entrypoint choice: %+v", p.Blocking)
	}
	entry := "./cmd/api"
	p = build(files, Inputs{Overrides: Hints{Entrypoint: &entry}})
	if p.Status != StatusReady || p.BuildCommand.Value != "go build -trimpath -o ./axiom-app ./cmd/api" {
		t.Fatalf("entrypoint override: %+v %+v", p.BuildCommand, p.Blocking)
	}
	c := build(map[string]string{"compose.yaml": "services:\n  web:\n    build: .\n    ports: [\"80:3000\"]\n  db:\n    image: postgres\n"}, Inputs{})
	if c.Status != StatusReady || c.Preset != "compose" || c.PublicService != "web" || c.Port.Value != 3000 || len(c.Services) != 2 {
		t.Fatalf("compose: %+v", c)
	}
}

func TestMonorepoRequiresRoot(t *testing.T) {
	p := build(map[string]string{"apps/web/package.json": `{"dependencies":{"next":"14"}}`, "apps/api/go.mod": "module m\n"}, Inputs{})
	if p.Status != StatusUnsupported || p.Unsupported.Code != "AMBIGUOUS_APPLICATION_ROOT" {
		t.Fatalf("monorepo: %+v", p.Unsupported)
	}
}
