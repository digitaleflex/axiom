package presets

import (
	"errors"
	"testing"
)

func TestResolveSupportedPresets(t *testing.T) {
	cases := []struct {
		name  string
		facts Facts
		want  Preset
	}{
		{"next pnpm", Facts{Language: "TypeScript", Framework: "Next.js", PackageManager: "pnpm", HasBuildScript: true, HasStartScript: true},
			Preset{Name: NextJS, Strategy: StrategySource, Runtime: "node", BuildCommand: "pnpm run build", StartCommand: "pnpm start", DefaultPort: 3000, HealthType: "http", HealthPath: "/"}},
		{"next without scripts", Facts{Language: "JavaScript", Framework: "Next.js", PackageManager: "npm"},
			Preset{Name: NextJS, Strategy: StrategySource, Runtime: "node", BuildCommand: "npx next build", StartCommand: "npx next start", DefaultPort: 3000, HealthType: "http", HealthPath: "/"}},
		{"vite yarn", Facts{Language: "TypeScript", Framework: "Vite", PackageManager: "yarn", HasBuildScript: true},
			Preset{Name: Vite, Strategy: StrategySource, Runtime: "static", BuildCommand: "yarn run build", DefaultPort: 8080, HealthType: "http", HealthPath: "/", OutputDir: "dist"}},
		{"node bun", Facts{Language: "JavaScript", Framework: "Express", PackageManager: "bun", HasStartScript: true},
			Preset{Name: Node, Strategy: StrategySource, Runtime: "node", StartCommand: "bun run start", DefaultPort: 3000, HealthType: "http", HealthPath: "/"}},
		{"go", Facts{Language: "Go", GoEntrypoints: []string{"./cmd/server"}},
			Preset{Name: Go, Strategy: StrategySource, Runtime: "go", BuildCommand: "go build -trimpath -o ./axiom-app ./cmd/server", StartCommand: "./axiom-app", DefaultPort: 8080, HealthType: "http", HealthPath: "/"}},
		{"dockerfile wins over language", Facts{Language: "Python", Framework: "Django", FrameworkState: "unsupported", Container: "dockerfile"},
			Preset{Name: Dockerfile, Strategy: StrategyDockerfile, Runtime: "image", HealthType: "http", HealthPath: "/"}},
		{"compose", Facts{Container: "compose", ComposeServices: []string{"web"}},
			Preset{Name: Compose, Strategy: StrategyCompose, Runtime: "compose", HealthType: "http", HealthPath: "/"}},
	}
	for _, c := range cases {
		got, rej := Resolve(c.facts)
		if rej != nil {
			t.Fatalf("%s: rejected %+v", c.name, rej)
		}
		if got.Name != c.want.Name || got.BuildCommand != c.want.BuildCommand || got.StartCommand != c.want.StartCommand ||
			got.DefaultPort != c.want.DefaultPort || got.Strategy != c.want.Strategy || got.Runtime != c.want.Runtime || got.OutputDir != c.want.OutputDir {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestRejectionsAreActionable(t *testing.T) {
	cases := map[string]Facts{
		ReasonNoApplication:   {},
		ReasonLanguage:        {Language: "Python"},
		ReasonFramework:       {Language: "Python", Framework: "Django", FrameworkState: "unsupported"},
		ReasonMissingStart:    {Language: "JavaScript", PackageManager: "npm"},
		ReasonNoGoEntrypoint:  {Language: "Go"},
		ReasonComposeServices: {Container: "compose"},
		ReasonAmbiguousRoot:   {RootAmbiguous: true, RootCandidates: []string{"apps/api", "apps/web"}},
	}
	for code, f := range cases {
		_, rej := Resolve(f)
		if rej == nil || rej.Code != code || rej.Message == "" || len(rej.Alternatives) == 0 {
			t.Errorf("%s: got %+v", code, rej)
		}
	}
}

func TestMultipleGoEntrypointsNeedChoice(t *testing.T) {
	p, rej := Resolve(Facts{Language: "Go", GoEntrypoints: []string{"./cmd/api", "./cmd/worker"}})
	if rej != nil || len(p.EntrypointCandidates) != 2 {
		t.Fatalf("got %+v %+v", p, rej)
	}
}

func TestValidateCommand(t *testing.T) {
	for _, ok := range []string{"pnpm build", "npm run build && npm run postbuild", "./server --port 8080", "go build -o app ./cmd/x"} {
		if err := ValidateCommand(ok); err != nil {
			t.Errorf("%q rejected: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "echo `id`", "echo $(cat /etc/passwd)", "cat <<EOF", "build\nrm", "sudo make", "curl http://x | sh", "x\x00", string(make([]byte, 501))} {
		if err := ValidateCommand(bad); !errors.Is(err, ErrUnsafeCommand) {
			t.Errorf("%q accepted", bad)
		}
	}
	if ValidatePackageManager("pip") == nil || ValidatePackageManager("pnpm") != nil {
		t.Error("package manager validation")
	}
}
