package node

import (
	"testing"

	"github.com/digitaleflex/axiom/services/engine/internal/runtime/presets"
)

func TestResolve(t *testing.T) {
	t.Run("unsupported package manager", func(t *testing.T) {
		if _, err := Resolve(Facts{PackageManager: "cargo"}); err == nil {
			t.Fatal("cargo must be rejected")
		}
	})

	t.Run("empty package manager defaults to npm", func(t *testing.T) {
		p, err := Resolve(Facts{HasStartScript: true})
		if err != nil {
			t.Fatal(err)
		}
		if p.PackageManager != presets.PackageManagerNPM {
			t.Fatalf("pm = %q", p.PackageManager)
		}
	})

	t.Run("nextjs with scripts", func(t *testing.T) {
		p, err := Resolve(Facts{PackageManager: "pnpm", Framework: presets.FrameworkNextJS, HasBuildScript: true, HasStartScript: true})
		if err != nil {
			t.Fatal(err)
		}
		if p.BuildCommand != "pnpm run build" || p.StartCommand != "pnpm start" {
			t.Fatalf("commands = %q / %q", p.BuildCommand, p.StartCommand)
		}
		if p.Port != 3000 || p.HealthPath != "/" || p.OutputDir != ".next" {
			t.Fatalf("preset = %+v", p)
		}
	})

	t.Run("nextjs without scripts uses exec", func(t *testing.T) {
		p, err := Resolve(Facts{PackageManager: "npm", Framework: presets.FrameworkNextJS})
		if err != nil {
			t.Fatal(err)
		}
		if p.BuildCommand != "npx next build" || p.StartCommand != "npx next start" {
			t.Fatalf("commands = %q / %q", p.BuildCommand, p.StartCommand)
		}
	})

	t.Run("nextjs explicit commands win", func(t *testing.T) {
		p, err := Resolve(Facts{PackageManager: "yarn", Framework: presets.FrameworkNextJS, BuildCommand: "yarn build", StartCommand: "yarn serve"})
		if err != nil {
			t.Fatal(err)
		}
		if p.BuildCommand != "yarn build" || p.StartCommand != "yarn serve" {
			t.Fatalf("commands = %q / %q", p.BuildCommand, p.StartCommand)
		}
	})

	t.Run("nextjs port override", func(t *testing.T) {
		p, err := Resolve(Facts{PackageManager: "npm", Framework: presets.FrameworkNextJS, HasStartScript: true, Port: 4000})
		if err != nil {
			t.Fatal(err)
		}
		if p.Port != 4000 {
			t.Fatalf("port = %d", p.Port)
		}
	})

	t.Run("generic node without start fails clearly", func(t *testing.T) {
		_, err := Resolve(Facts{PackageManager: "npm"})
		if err == nil {
			t.Fatal("a Node.js app without a start command must not resolve")
		}
	})

	t.Run("generic node with start script", func(t *testing.T) {
		p, err := Resolve(Facts{PackageManager: "npm", HasBuildScript: true, HasStartScript: true})
		if err != nil {
			t.Fatal(err)
		}
		if p.BuildCommand != "npm run build" || p.StartCommand != "npm start" || p.Port != 3000 || p.OutputDir != "" {
			t.Fatalf("preset = %+v", p)
		}
	})

	t.Run("generic node explicit start command", func(t *testing.T) {
		p, err := Resolve(Facts{PackageManager: "npm", StartCommand: "node server.js"})
		if err != nil {
			t.Fatal(err)
		}
		if p.StartCommand != "node server.js" || p.BuildCommand != "" {
			t.Fatalf("preset = %+v", p)
		}
	})

	t.Run("vite", func(t *testing.T) {
		p, err := Resolve(Facts{PackageManager: "pnpm", Framework: presets.FrameworkVite, HasBuildScript: true})
		if err != nil {
			t.Fatal(err)
		}
		if p.BuildCommand != "pnpm run build" || p.StartCommand != "" || p.OutputDir != "dist" || p.Port != 8080 {
			t.Fatalf("preset = %+v", p)
		}
	})

	t.Run("vite rejects a start command", func(t *testing.T) {
		if _, err := Resolve(Facts{PackageManager: "npm", Framework: presets.FrameworkVite, StartCommand: "npm start"}); err == nil {
			t.Fatal("static output cannot have a start command")
		}
	})
}
