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
// environment, writes an iidfile and exits with the given code.
func stubDocker(t *testing.T, exitCode int) (dir string) {
	t.Helper()
	dir = t.TempDir()
	script := `#!/bin/sh
echo "ARGS:$@" > "$STUB_OUT/args"
env > "$STUB_OUT/env"
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
	stub := stubDocker(t, 0)
	out := t.TempDir()
	t.Setenv("STUB_OUT", out)
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
	stub := stubDocker(t, 3)
	out := t.TempDir()
	t.Setenv("STUB_OUT", out)
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
