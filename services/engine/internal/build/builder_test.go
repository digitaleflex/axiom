package build

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// stubDocker creates a fake `docker` executable that records its argv and
// environment into outDir (path baked into the script, so the stub needs no
// environment variables), writes an iidfile and exits with the given code.
func stubDocker(t *testing.T, exitCode int, outDir string) (dir string) {
	t.Helper()
	dir = t.TempDir()
	script := "#!/bin/sh\n" +
		"OUT=" + shellQuote(outDir) + "\n" +
		`echo "ARGS:$@" > "$OUT/args"
env > "$OUT/env"
iid=""
prev=""
for a in "$@"; do
  if [ "$prev" = "--iidfile" ]; then iid="$a"; fi
  prev="$a"
done
echo "sha256:STUBDIGEST" > "$iid"
echo "Step 1/2 : FROM x"
echo "Step 2/2 : DONE"
exit ` + itoa(exitCode) + "\n"
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func itoa(n int) string { return string(rune('0' + n)) }

func TestExecBuilderArgConstruction(t *testing.T) {
	out := t.TempDir()
	stub := stubDocker(t, 0, out)
	t.Setenv("AXIOM_SECRET_SHOULD_NOT_LEAK", "s3cret")
	ctxDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(ctxDir, "Dockerfile"), []byte("FROM x\n"), 0o644)
	b := &ExecBuilder{Docker: filepath.Join(stub, "docker"), Env: []string{"PATH=/usr/bin:/bin", "STUB_OUT=" + out}}
	res, err := b.Build(context.Background(), BuildSpec{
		ContextDir: ctxDir, Dockerfile: "Dockerfile",
		Tags:    []string{"axiom-local/app:abc-def"},
		Labels:  map[string]string{"axiom.deployment": "dep_1"},
		Timeout: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.ImageRef != "axiom-local/app:abc-def" || res.ImageID != "sha256:STUBDIGEST" || res.DurationMs < 0 {
		t.Fatalf("result = %+v", res)
	}
	args, _ := os.ReadFile(filepath.Join(out, "args"))
	for _, want := range []string{"build", "--iidfile", "-f", "Dockerfile", "-t", "axiom-local/app:abc-def", "--label", "axiom.deployment=dep_1", ctxDir} {
		if !strings.Contains(string(args), want) {
			t.Fatalf("argv missing %q: %s", want, args)
		}
	}
	env, _ := os.ReadFile(filepath.Join(out, "env"))
	if strings.Contains(string(env), "s3cret") || strings.Contains(string(env), "AXIOM_SECRET") {
		t.Fatalf("host secrets leaked into build environment: %s", env)
	}
}

func TestExecBuilderFailureKeepsExitCode(t *testing.T) {
	out := t.TempDir()
	stub := stubDocker(t, 3, out)
	ctxDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(ctxDir, "Dockerfile"), []byte("FROM x\n"), 0o644)
	b := &ExecBuilder{Docker: filepath.Join(stub, "docker"), Env: []string{"PATH=/usr/bin:/bin", "STUB_OUT=" + out}}
	_, err := b.Build(context.Background(), BuildSpec{ContextDir: ctxDir, Dockerfile: "Dockerfile", Tags: []string{"t"}, Timeout: time.Minute})
	var be *Error
	if !isBuildError(err, &be) || be.Code != CodeBuildFailed || be.ExitCode != 3 || !strings.Contains(be.Log, "Step 2/2") {
		t.Fatalf("error = %+v", err)
	}
	if _, err := b.Build(context.Background(), BuildSpec{}); !isBuildError(err, &be) || be.Code != CodeInvalidInput {
		t.Fatalf("empty spec: %v", err)
	}
}

func isBuildError(err error, target **Error) bool {
	if e, ok := err.(*Error); ok {
		*target = e
		return true
	}
	return false
}

// runStubBuild runs a stubbed `docker build` and returns the environment dump
// recorded by the child process.
func runStubBuild(t *testing.T, env []string) string {
	t.Helper()
	out := t.TempDir()
	stub := stubDocker(t, 0, out)
	ctxDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(ctxDir, "Dockerfile"), []byte("FROM x\n"), 0o644)
	b := &ExecBuilder{Docker: filepath.Join(stub, "docker"), Env: env}
	if _, err := b.Build(context.Background(), BuildSpec{
		ContextDir: ctxDir, Dockerfile: "Dockerfile",
		Tags: []string{"axiom-test/iso:latest"}, Timeout: time.Minute,
	}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(out, "env"))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// TestExecBuilderNilEnvDoesNotInheritEngineEnvironment is the non-regression
// test for the Engine environment leak: with Env == nil the docker child must
// NOT see any variable of the Engine process environment.
func TestExecBuilderNilEnvDoesNotInheritEngineEnvironment(t *testing.T) {
	t.Setenv("AXIOM_SECRET_KEY", "engine-secret-key")
	t.Setenv("DATABASE_URL", "postgres://engine:5432/axiom")
	t.Setenv("AXIOM_API_TOKEN", "engine-api-token")
	got := runStubBuild(t, nil)
	for _, leaked := range []string{
		"AXIOM_SECRET_KEY", "engine-secret-key",
		"DATABASE_URL", "postgres://engine",
		"AXIOM_API_TOKEN", "engine-api-token",
	} {
		if strings.Contains(got, leaked) {
			t.Fatalf("Engine environment leaked into the docker child (%q):\n%s", leaked, got)
		}
	}
	if !strings.Contains(got, "PATH=") {
		t.Fatalf("minimal build environment must still provide PATH:\n%s", got)
	}
}

// TestExecBuilderExplicitEnvIsCallerRequestedOnly checks that an explicit Env
// makes only the requested entries visible, and still never inherits the rest
// of the Engine environment.
func TestExecBuilderExplicitEnvIsCallerRequestedOnly(t *testing.T) {
	t.Setenv("AXIOM_SECRET_KEY", "engine-secret-key")
	t.Setenv("AXIOM_NOT_REQUESTED", "engine-untold-value")
	got := runStubBuild(t, []string{"CALLER_VAR=caller-value"})
	for _, want := range []string{"CALLER_VAR=caller-value", "PATH="} {
		if !strings.Contains(got, want) {
			t.Fatalf("requested entry %q missing from the docker child:\n%s", want, got)
		}
	}
	for _, leaked := range []string{"AXIOM_SECRET_KEY", "engine-secret-key", "AXIOM_NOT_REQUESTED", "engine-untold-value"} {
		if strings.Contains(got, leaked) {
			t.Fatalf("Engine environment leaked into the docker child (%q):\n%s", leaked, got)
		}
	}
}

func parseEnv(t *testing.T, env []string) map[string]string {
	t.Helper()
	m := make(map[string]string, len(env))
	for _, kv := range env {
		name, val, ok := strings.Cut(kv, "=")
		if !ok {
			t.Fatalf("malformed entry %q in child environment %v", kv, env)
		}
		if _, dup := m[name]; dup {
			t.Fatalf("duplicate key %q in child environment %v", name, env)
		}
		m[name] = val
	}
	return m
}

func TestChildEnv(t *testing.T) {
	parent := []string{
		"PATH=/engine/bin",
		"HOME=/home/engine",
		"DOCKER_HOST=unix:///var/run/docker.sock",
		"NO_PROXY=.internal",
		"AXIOM_SECRET_KEY=s3cret",
		"DATABASE_URL=postgres://engine",
		"NOT_ALLOWED=value",
	}

	// nil extra: only the allowlisted parent names survive.
	got := parseEnv(t, childEnv(parent, nil))
	if got["PATH"] != "/engine/bin" || got["HOME"] != "/home/engine" ||
		got["DOCKER_HOST"] != "unix:///var/run/docker.sock" || got["NO_PROXY"] != ".internal" {
		t.Fatalf("allowlisted values missing from child environment: %v", got)
	}
	for _, name := range []string{"AXIOM_SECRET_KEY", "DATABASE_URL", "NOT_ALLOWED"} {
		if _, ok := got[name]; ok {
			t.Fatalf("%s must never reach the child: %v", name, got)
		}
	}

	// extra overrides the base on collision and adds new entries.
	got = parseEnv(t, childEnv(parent, []string{"PATH=/custom/bin", "EXTRA=1"}))
	if got["PATH"] != "/custom/bin" || got["EXTRA"] != "1" {
		t.Fatalf("extra entries not applied: %v", got)
	}
	if _, ok := got["AXIOM_SECRET_KEY"]; ok {
		t.Fatalf("secret leaked even with extra set: %v", got)
	}

	// Collision keeps a single entry per key.
	for _, kv := range childEnv(parent, []string{"PATH=/custom/bin"}) {
		if strings.HasPrefix(kv, "PATH=") && kv != "PATH=/custom/bin" {
			t.Fatalf("PATH collision not resolved: %v", kv)
		}
	}

	// PATH default when neither side provides one.
	got = parseEnv(t, childEnv(nil, nil))
	if got["PATH"] != defaultPATH {
		t.Fatalf("PATH default = %q, want %q", got["PATH"], defaultPATH)
	}

	// Malformed entries are dropped instead of passed through.
	got = parseEnv(t, childEnv(nil, []string{"NO_EQUALS_SIGN"}))
	if len(got) != 1 || got["PATH"] != defaultPATH {
		t.Fatalf("malformed entry handling: %v", got)
	}
}

func TestRealDockerBuild(t *testing.T) {
	if os.Getenv("AXIOM_TEST_DOCKER") == "" {
		t.Skip("AXIOM_TEST_DOCKER is not set")
	}
	ctxDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(ctxDir, "Dockerfile"), []byte("FROM postgres:17-alpine\nRUN echo axiom-build-ok\n"), 0o644)
	b := &ExecBuilder{}
	res, err := b.Build(context.Background(), BuildSpec{
		ContextDir: ctxDir, Dockerfile: "Dockerfile",
		Tags:    []string{"axiom-test/real-build:latest"},
		Labels:  map[string]string{"axiom.test": "true"},
		Timeout: 5 * time.Minute,
	})
	if err != nil {
		t.Fatalf("real build (cached postgres base): %v", err)
	}
	if !strings.HasPrefix(res.ImageID, "sha256:") || res.DurationMs <= 0 {
		t.Fatalf("result = %+v", res)
	}
}

func TestDockerfileTemplates(t *testing.T) {
	for _, preset := range []string{"nextjs", "node", "vite", "go"} {
		df, err := dockerfileForPreset(preset, "pnpm run build", "pnpm start", 3000, "dist")
		if err != nil || !strings.Contains(df, "pnpm run build") {
			t.Fatalf("%s: %v %q", preset, err, df)
		}
	}
	if _, err := dockerfileForPreset("rails", "x", "y", 3000, ""); err == nil {
		t.Fatal("unknown preset must fail")
	}
	df, _ := dockerfileForPreset("vite", "npm run build", "", 8080, "")
	if !strings.Contains(df, "nginx") || !strings.Contains(df, "dist") {
		t.Fatalf("vite template = %q", df)
	}
}
