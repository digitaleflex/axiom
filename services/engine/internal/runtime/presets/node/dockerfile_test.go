package node

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/digitaleflex/axiom/services/engine/internal/runtime/presets"
)

var update = flag.Bool("update", false, "rewrite golden files")

func factsFor(pm, framework string, lockfile bool) Facts {
	f := Facts{PackageManager: pm, Framework: framework, HasLockfile: lockfile, HasBuildScript: true}
	if framework != presets.FrameworkVite {
		f.HasStartScript = true
	}
	return f
}

// TestGoldenDockerfiles renders one Dockerfile per package manager ×
// framework and compares it with the golden file (go test -update to
// regenerate).
func TestGoldenDockerfiles(t *testing.T) {
	for _, pm := range []string{presets.PackageManagerNPM, presets.PackageManagerPNPM, presets.PackageManagerYarn, presets.PackageManagerBun} {
		for _, fw := range []string{presets.FrameworkNextJS, presets.FrameworkVite} {
			name := pm + "-" + strings.ToLower(fw)
			t.Run(name, func(t *testing.T) {
				df, err := DockerfileForFacts(factsFor(pm, fw, true))
				if err != nil {
					t.Fatal(err)
				}
				path := filepath.Join("testdata", "golden", name+".dockerfile")
				if *update {
					if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, []byte(df), 0o644); err != nil {
						t.Fatal(err)
					}
					return
				}
				want, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("missing golden file (run go test -update): %v", err)
				}
				if string(want) != df {
					t.Fatalf("dockerfile changed for %s:\n%s", name, df)
				}
			})
		}
	}
}

// TestDockerfileConstructs asserts the safe constructs (#121) on every
// package manager × framework combination.
func TestDockerfileConstructs(t *testing.T) {
	for _, pm := range []string{presets.PackageManagerNPM, presets.PackageManagerPNPM, presets.PackageManagerYarn, presets.PackageManagerBun} {
		for _, fw := range []string{presets.FrameworkNextJS, presets.FrameworkVite} {
			name := pm + "-" + strings.ToLower(fw)
			t.Run(name, func(t *testing.T) {
				df, err := DockerfileForFacts(factsFor(pm, fw, true))
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(df, "curl") || strings.Contains(df, "wget") {
					t.Fatalf("no curl/wget in minimal images:\n%s", df)
				}
				if n := strings.Count(df, "FROM "); n < 3 {
					t.Fatalf("multi-stage build expected, got %d FROMs:\n%s", n, df)
				}
				if fw == presets.FrameworkVite {
					assertContains(t, df, "FROM "+NginxAlpine)
					assertContains(t, df, "COPY --from=build /app/dist /usr/share/nginx/html")
					assertContains(t, df, "EXPOSE 80")
					assertContains(t, df, depsInstall(pm, true))
					return
				}
				assertContains(t, df, "FROM "+Node20Alpine+" AS deps")
				assertContains(t, df, "FROM "+Node20Alpine+" AS build")
				assertContains(t, df, "ENV NODE_ENV=production")
				assertContains(t, df, "USER node")
				assertContains(t, df, "EXPOSE 3000")
				assertContains(t, df, depsInstall(pm, true))
				assertContains(t, df, prodInstall(pm, true))
				assertContains(t, df, "CMD")
			})
		}
	}
}

// TestLockfileAwareInstall verifies the install command follows lockfile
// presence for every package manager.
func TestLockfileAwareInstall(t *testing.T) {
	for _, pm := range []string{presets.PackageManagerNPM, presets.PackageManagerPNPM, presets.PackageManagerYarn, presets.PackageManagerBun} {
		withLock, err := DockerfileForFacts(factsFor(pm, presets.FrameworkNextJS, true))
		if err != nil {
			t.Fatal(err)
		}
		withoutLock, err := DockerfileForFacts(factsFor(pm, presets.FrameworkNextJS, false))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(withLock, depsInstall(pm, true)) || !strings.Contains(withLock, prodInstall(pm, true)) {
			t.Fatalf("%s: lockfile install commands missing:\n%s", pm, withLock)
		}
		if !strings.Contains(withoutLock, depsInstall(pm, false)) || !strings.Contains(withoutLock, prodInstall(pm, false)) {
			t.Fatalf("%s: lockfile-less install commands missing:\n%s", pm, withoutLock)
		}
		if strings.Contains(withoutLock, "--frozen-lockfile") {
			t.Fatalf("%s: lockfile-less template must not use --frozen-lockfile:\n%s", pm, withoutLock)
		}
	}
}

func assertContains(t *testing.T, df, want string) {
	t.Helper()
	if !strings.Contains(df, want) {
		t.Fatalf("dockerfile must contain %q:\n%s", want, df)
	}
}
