package manifest

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/digitaleflex/axiom/services/engine/internal/analyzer/evidence"
	"github.com/digitaleflex/axiom/services/engine/internal/analyzer/snapshot"
	"github.com/digitaleflex/axiom/services/engine/internal/profile"
)

// TestParseExamples loads every example in schemas/examples/axiom/ and
// asserts the outcome documented in each file's leading comment: valid-*
// files must parse, invalid-* files must fail with the expected code.
func TestParseExamples(t *testing.T) {
	dir := "../../../../schemas/examples/axiom"
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	expectedRe := regexp.MustCompile(`expected: ([A-Z_]+)`)
	found := 0
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		found++
		content, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		name := e.Name()
		t.Run(name, func(t *testing.T) {
			m, err := Parse(content)
			if strings.HasPrefix(name, "valid-") {
				if err != nil {
					t.Fatalf("expected a valid manifest, got %v", err)
				}
				if m.Version != 1 {
					t.Fatalf("version = %d", m.Version)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected an invalid manifest, got %+v", m)
			}
			merr, ok := err.(*Error)
			if !ok {
				t.Fatalf("error type = %T, want *manifest.Error", err)
			}
			loc := expectedRe.FindSubmatch(content)
			if loc == nil {
				t.Fatalf("no \"expected: CODE\" comment in %s", name)
			}
			if merr.Code != string(loc[1]) {
				t.Fatalf("code = %s, want %s (%s)", merr.Code, loc[1], merr.Message)
			}
		})
	}
	if found == 0 {
		t.Fatal("no example files found")
	}
}

func TestParseErrors(t *testing.T) {
	cases := []struct {
		name    string
		content string
		code    string
	}{
		{"not yaml", "version: [unclosed\n", CodeParseError},
		{"empty", "", CodeParseError},
		{"scalar document", "just a string\n", CodeParseError},
		{"version missing", "strategy: nextjs\n", CodeVersionUnsupported},
		{"version 2", "version: 2\n", CodeVersionUnsupported},
		{"version string", `version: "1"` + "\n", CodeVersionUnsupported},
		{"unknown top-level field", "version: 1\nreplicas: 3\n", CodeSchemaInvalid},
		{"unknown nested field", "version: 1\napp: {name: web, replicas: 2}\n", CodeSchemaInvalid},
		{"bad app.name", "version: 1\napp: {name: Web}\n", CodeSchemaInvalid},
		{"app.name too long", "version: 1\napp: {name: " + strings.Repeat("a", 70) + "}\n", CodeSchemaInvalid},
		{"app.root absolute", "version: 1\napp: {root: /etc}\n", CodeSchemaInvalid},
		{"app.root traversal", "version: 1\napp: {root: ../outside}\n", CodeSchemaInvalid},
		{"app.root dotdot in name", "version: 1\napp: {root: apps/../web}\n", CodeSchemaInvalid},
		{"bad strategy", "version: 1\nstrategy: rails\n", CodeSchemaInvalid},
		{"bad language", "version: 1\nruntime: {language: python}\n", CodeSchemaInvalid},
		{"bad runtime version", "version: 1\nruntime: {version: \"1.2.3.4\"}\n", CodeSchemaInvalid},
		{"bad package manager", "version: 1\nruntime: {packageManager: cargo}\n", CodeSchemaInvalid},
		{"port too low", "version: 1\nport: 0\n", CodeSchemaInvalid},
		{"port too high", "version: 1\nport: 65536\n", CodeSchemaInvalid},
		{"port not integer", "version: 1\nport: \"3000\"\n", CodeSchemaInvalid},
		{"command with backtick", "version: 1\nbuild: {command: \"npm run `id`\"}\n", CodeSchemaInvalid},
		{"command with newline", "version: 1\nstart: {command: \"npm start\\n\"}\n", CodeSchemaInvalid},
		{"command too long", "version: 1\nbuild: {command: \"" + strings.Repeat("a", 501) + "\"}\n", CodeSchemaInvalid},
		{"dockerfile absolute", "version: 1\nbuild: {dockerfile: /Dockerfile}\n", CodeSchemaInvalid},
		{"dockerfile traversal", "version: 1\nbuild: {dockerfile: ../../etc/Dockerfile}\n", CodeSchemaInvalid},
		{"env value", "version: 1\nenv:\n  - name: DATABASE_URL\n    value: postgres://x\n", CodeSecretValue},
		{"env value beats schema", "version: 1\nenv:\n  - value: postgres://x\n", CodeSecretValue},
		{"env bad name", "version: 1\nenv:\n  - name: databaseUrl\n", CodeSchemaInvalid},
		{"env duplicate", "version: 1\nenv:\n  - name: A\n  - name: A\n", CodeSchemaInvalid},
		{"env description too long", "version: 1\nenv:\n  - name: A\n    description: \"" + strings.Repeat("a", 201) + "\"\n", CodeSchemaInvalid},
		{"services without compose", "version: 1\nstrategy: node\nservices: {file: docker-compose.yml}\n", CodeSchemaInvalid},
		{"services without strategy", "version: 1\nservices: {file: docker-compose.yml}\n", CodeSchemaInvalid},
		{"services bad include", "version: 1\nstrategy: compose\nservices: {include: [\"-web\"]}\n", CodeSchemaInvalid},
		{"services bad public", "version: 1\nstrategy: compose\nservices: {public: \"-web\"}\n", CodeSchemaInvalid},
		{"bad domain", "version: 1\ndomains: [\"Web.COM\"]\n", CodeSchemaInvalid},
		{"domain not fqdn", "version: 1\ndomains: [\"localhost\"]\n", CodeSchemaInvalid},
		{"health type missing", "version: 1\nhealth: {path: /healthz}\n", CodeSchemaInvalid},
		{"health bad type", "version: 1\nhealth: {type: grpc}\n", CodeSchemaInvalid},
		{"health http without path", "version: 1\nhealth: {type: http}\n", CodeSchemaInvalid},
		{"health path not slash", "version: 1\nhealth: {type: http, path: healthz}\n", CodeSchemaInvalid},
		{"health bad status", "version: 1\nhealth: {type: http, path: /, expectedStatus: 99}\n", CodeSchemaInvalid},
		{"health bad timeout", "version: 1\nhealth: {type: tcp, timeoutSeconds: 0}\n", CodeSchemaInvalid},
		{"health bad retries", "version: 1\nhealth: {type: tcp, retries: 21}\n", CodeSchemaInvalid},
		{"dockerfile strategy with runtime", "version: 1\nstrategy: dockerfile\nruntime: {language: go}\n", CodeSchemaInvalid},
		{"dockerfile strategy with build command", "version: 1\nstrategy: dockerfile\nbuild: {command: make}\n", CodeSchemaInvalid},
		{"bad architecture", "version: 1\nconstraints: {architecture: linux/riscv64}\n", CodeSchemaInvalid},
		{"memory too low", "version: 1\nconstraints: {minMemoryMB: 32}\n", CodeSchemaInvalid},
		{"disk too low", "version: 1\nconstraints: {minDiskGB: 0}\n", CodeSchemaInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.content))
			if err == nil {
				t.Fatalf("expected %s, got a valid manifest", tc.code)
			}
			merr, ok := err.(*Error)
			if !ok {
				t.Fatalf("error type = %T, want *manifest.Error", err)
			}
			if merr.Code != tc.code {
				t.Fatalf("code = %s, want %s (%s)", merr.Code, tc.code, merr.Message)
			}
		})
	}
}

func TestValidDetails(t *testing.T) {
	m, err := Parse([]byte("version: 1\napp: {name: shop-web, root: apps/web}\nstrategy: vite\n"))
	if err != nil {
		t.Fatal(err)
	}
	if m.App.Name != "shop-web" || m.App.Root != "apps/web" || m.Strategy != "vite" {
		t.Fatalf("manifest = %+v", m)
	}
	if m.AppRoot() != "apps/web" {
		t.Fatalf("AppRoot() = %q", m.AppRoot())
	}
}

func TestToHints(t *testing.T) {
	content, err := os.ReadFile("../../../../schemas/examples/axiom/valid-go.yaml")
	if err != nil {
		t.Fatal(err)
	}
	m, err := Parse(content)
	if err != nil {
		t.Fatal(err)
	}
	h := ToHints(m)
	if h.Strategy == nil || *h.Strategy != "go" {
		t.Fatalf("strategy hint = %v", h.Strategy)
	}
	if h.BuildCommand == nil || *h.BuildCommand != "go build -o app ./cmd/server" {
		t.Fatalf("build hint = %v", h.BuildCommand)
	}
	if h.StartCommand == nil || *h.StartCommand != "./app" {
		t.Fatalf("start hint = %v", h.StartCommand)
	}
	if h.Port == nil || *h.Port != 8080 {
		t.Fatalf("port hint = %v", h.Port)
	}
	if h.HealthPath == nil || *h.HealthPath != "/healthz" {
		t.Fatalf("health hint = %v", h.HealthPath)
	}
	if h.PackageManager != nil {
		t.Fatalf("package manager hint = %v (go manifest sets none)", h.PackageManager)
	}
}

func TestToHintsOmitsUnsafeCommands(t *testing.T) {
	m, err := Parse([]byte("version: 1\nbuild: {command: \"npm run build && curl http://x\"}\nstart: {command: npm start}\n"))
	if err != nil {
		t.Fatal(err) // schema-valid: single printable line
	}
	h := ToHints(m)
	if h.BuildCommand != nil {
		t.Fatalf("unsafe build command must be omitted, got %q", *h.BuildCommand)
	}
	if h.StartCommand == nil || *h.StartCommand != "npm start" {
		t.Fatalf("safe start command must be kept, got %v", h.StartCommand)
	}
}

func strPtr(s string) *string { return &s }

// TestPrecedence verifies the override chain through profile.Build: manifest
// beats detected evidence, and user overrides beat the manifest.
func TestPrecedence(t *testing.T) {
	files := map[string]string{
		"package.json": `{"scripts":{"build":"next build","start":"next start"},"dependencies":{"next":"14"}}`,
	}
	res := evidence.Analyze(snap(files), evidence.Options{})
	src := profile.Source{RepositoryID: "repo_1", Ref: "main", Commit: strings.Repeat("a", 40)}

	// Detected baseline: npm scripts.
	p := profile.Build(res, src, profile.Inputs{})
	if p.BuildCommand.Value != "npm run build" || p.BuildCommand.Provenance != profile.ProvenanceDetected {
		t.Fatalf("detected build = %s (%s)", p.BuildCommand.Value, p.BuildCommand.Provenance)
	}
	if p.StartCommand.Value != "npm start" || p.StartCommand.Provenance != profile.ProvenanceDetected {
		t.Fatalf("detected start = %s (%s)", p.StartCommand.Value, p.StartCommand.Provenance)
	}

	// Manifest beats detected.
	man, err := Parse([]byte("version: 1\nstrategy: nextjs\nbuild: {command: npm run build --prod}\nstart: {command: npm run start:prod}\nport: 4000\n"))
	if err != nil {
		t.Fatal(err)
	}
	h := ToHints(man)
	p = profile.Build(res, src, profile.Inputs{Manifest: h})
	if p.BuildCommand.Value != "npm run build --prod" || p.BuildCommand.Provenance != profile.ProvenanceManifest {
		t.Fatalf("manifest build = %s (%s)", p.BuildCommand.Value, p.BuildCommand.Provenance)
	}
	if p.StartCommand.Value != "npm run start:prod" || p.StartCommand.Provenance != profile.ProvenanceManifest {
		t.Fatalf("manifest start = %s (%s)", p.StartCommand.Value, p.StartCommand.Provenance)
	}
	if p.Port.Value != 4000 || p.Port.Provenance != profile.ProvenanceManifest {
		t.Fatalf("manifest port = %d (%s)", p.Port.Value, p.Port.Provenance)
	}

	// Override beats manifest.
	p = profile.Build(res, src, profile.Inputs{Manifest: h, Overrides: profile.Hints{StartCommand: strPtr("npm run start:debug")}})
	if p.StartCommand.Value != "npm run start:debug" || p.StartCommand.Provenance != profile.ProvenanceOverride {
		t.Fatalf("override start = %s (%s)", p.StartCommand.Value, p.StartCommand.Provenance)
	}
	// Non-overridden manifest values survive.
	if p.BuildCommand.Value != "npm run build --prod" || p.BuildCommand.Provenance != profile.ProvenanceManifest {
		t.Fatalf("manifest build after override = %s (%s)", p.BuildCommand.Value, p.BuildCommand.Provenance)
	}
}

func TestCheckConflict(t *testing.T) {
	m := Manifest{Strategy: "compose"}
	if err := m.CheckConflict(func(string) bool { return false }); err == nil {
		t.Fatal("compose without a compose file must conflict")
	} else if merr, ok := err.(*Error); !ok || merr.Code != CodeConflict {
		t.Fatalf("conflict error = %v", err)
	}
	if err := m.CheckConflict(func(string) bool { return true }); err != nil {
		t.Fatalf("compose with a compose file must not conflict: %v", err)
	}
	m = Manifest{Strategy: "nextjs"}
	if err := m.CheckConflict(func(string) bool { return false }); err != nil {
		t.Fatalf("non-compose strategy must not conflict: %v", err)
	}
}

func snap(files map[string]string) snapshot.Snapshot {
	s := snapshot.Snapshot{}
	for p, c := range files {
		s.Files = append(s.Files, snapshot.File{Path: p, Size: int64(len(c)), Content: []byte(c), Retained: true})
	}
	sort.Slice(s.Files, func(i, j int) bool { return s.Files[i].Path < s.Files[j].Path })
	return s
}

// TestLoad verifies snapshot lookup: the root-level manifest wins, an
// invalid manifest surfaces its code, and app.root is returned.
func TestLoad(t *testing.T) {
	content, err := os.ReadFile("../../../../schemas/examples/axiom/valid-monorepo.yaml")
	if err != nil {
		t.Fatal(err)
	}
	// Root-level manifest wins over <root>/axiom.yaml.
	s := snap(map[string]string{
		"axiom.yaml":            string(content),
		"apps/web/axiom.yaml":   "version: 1\nstrategy: node\n",
		"apps/web/package.json": "{}",
	})
	h, root, err := Load(s, "")
	if err != nil {
		t.Fatal(err)
	}
	if root != "apps/web" {
		t.Fatalf("root = %q", root)
	}
	if h.Strategy == nil || *h.Strategy != "vite" {
		t.Fatalf("hints = %v (root manifest must win)", h.Strategy)
	}

	// Fallback to <root>/axiom.yaml when no root-level manifest exists.
	s = snap(map[string]string{
		"apps/web/axiom.yaml":   "version: 1\nstrategy: node\nport: 4000\n",
		"apps/web/package.json": "{}",
	})
	h, root, err = Load(s, "apps/web")
	if err != nil {
		t.Fatal(err)
	}
	if root != "" || h.Strategy == nil || *h.Strategy != "node" || h.Port == nil || *h.Port != 4000 {
		t.Fatalf("hints = %+v root = %q", h, root)
	}

	// No manifest: no hints, no error.
	h, root, err = Load(snap(map[string]string{"package.json": "{}"}), "")
	if err != nil || root != "" || h.Strategy != nil {
		t.Fatalf("no manifest = %+v, %q, %v", h, root, err)
	}

	// An invalid manifest blocks with its code.
	_, _, err = Load(snap(map[string]string{"axiom.yaml": "version: 2\n"}), "")
	if err == nil {
		t.Fatal("invalid manifest must fail")
	} else if merr, ok := err.(*Error); !ok || merr.Code != CodeVersionUnsupported {
		t.Fatalf("error = %v", err)
	}

	// strategy: compose without a compose file conflicts.
	_, _, err = Load(snap(map[string]string{"axiom.yaml": "version: 1\nstrategy: compose\n"}), "")
	if err == nil {
		t.Fatal("compose without a compose file must fail")
	} else if merr, ok := err.(*Error); !ok || merr.Code != CodeConflict {
		t.Fatalf("error = %v", err)
	}
	_, _, err = Load(snap(map[string]string{"axiom.yaml": "version: 1\nstrategy: compose\n", "docker-compose.yml": "services: {}\n"}), "")
	if err != nil {
		t.Fatalf("compose with a compose file must pass: %v", err)
	}
}
