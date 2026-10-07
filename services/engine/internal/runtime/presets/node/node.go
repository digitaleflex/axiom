// Package node provides the safe, explicit Node.js production presets
// (issue #121): npm/pnpm/yarn/bun, Next.js and Vite runtime modes, pinned
// base images, lockfile-aware installs and non-root containers.
//
// Design decisions (documented per #121):
//
//   - Base images are pinned constants (node:20-alpine, nginx:1.27-alpine).
//   - Dependencies install from the lockfile when one is present
//     (npm ci / pnpm fetch / yarn install / bun install), so builds are
//     reproducible; without a lockfile the plain install command is used.
//   - The runtime stage is minimal: package.json + lockfile + build output
//   - production-only node_modules. Production dependencies are installed
//     fresh with --prod/--production flags, never copied from the build
//     stage with dev dependencies.
//   - Containers run as the non-root user "node" (uid 1000) with
//     NODE_ENV=production.
//   - There is deliberately no HEALTHCHECK instruction: minimal images ship
//     without curl/wget, and Axiom probes health externally (the profile
//     health check) instead of baking a probe into the image.
//   - Next.js note: the runtime stage copies package.json, the lockfile and
//     the .next build output. `next start` serves files from the public/
//     directory at runtime; repositories that rely on public/ assets should
//     use the dockerfile strategy instead.
package node

import (
	"fmt"

	"github.com/digitaleflex/axiom/services/engine/internal/runtime/presets"
)

// Pinned base images (#121).
const (
	Node20Alpine = "node:20-alpine"
	NginxAlpine  = "nginx:1.27-alpine"
)

// Facts are the analyzer/profile conclusions the preset resolves from
// ("" or 0 when unknown).
type Facts struct {
	PackageManager string // npm | pnpm | yarn | bun (empty = npm)
	HasBuildScript bool
	HasStartScript bool
	Framework      string // presets.FrameworkNextJS | presets.FrameworkVite | ""
	Port           int    // 0 = preset default
	HasLockfile    bool
	// Explicit commands (manifest/override) take precedence over
	// script-derived commands.
	BuildCommand string
	StartCommand string
}

// NodePreset is a resolved, safe production preset.
type NodePreset struct {
	PackageManager string
	BuildCommand   string
	StartCommand   string
	Port           int
	HealthPath     string
	OutputDir      string // ".next" (Next.js), "dist" (Vite), "" = run from source
}

// Resolve selects the safe preset for the facts. Unsupported configurations
// fail with an actionable error.
func Resolve(f Facts) (NodePreset, error) {
	pm := f.PackageManager
	if pm == "" {
		pm = presets.PackageManagerNPM
	}
	if !validPackageManager(pm) {
		return NodePreset{}, fmt.Errorf("unsupported package manager %q: must be npm, pnpm, yarn or bun", pm)
	}
	p := NodePreset{PackageManager: pm, HealthPath: "/"}
	switch f.Framework {
	case presets.FrameworkNextJS:
		p.OutputDir = ".next"
		p.Port = 3000
		p.BuildCommand = f.BuildCommand
		if p.BuildCommand == "" {
			if f.HasBuildScript {
				p.BuildCommand = run(pm, "build")
			} else {
				p.BuildCommand = exec(pm, "next build")
			}
		}
		p.StartCommand = f.StartCommand
		if p.StartCommand == "" {
			if f.HasStartScript {
				p.StartCommand = run(pm, "start")
			} else {
				p.StartCommand = exec(pm, "next start")
			}
		}
	case presets.FrameworkVite:
		p.OutputDir = "dist"
		p.Port = 8080
		p.BuildCommand = f.BuildCommand
		if p.BuildCommand == "" {
			if f.HasBuildScript {
				p.BuildCommand = run(pm, "build")
			} else {
				p.BuildCommand = exec(pm, "vite build")
			}
		}
		// Static output: nginx serves the build, there is no start command.
		if f.StartCommand != "" {
			return NodePreset{}, fmt.Errorf("framework Vite builds static output; a start command makes no sense")
		}
	default:
		if f.StartCommand == "" && !f.HasStartScript {
			return NodePreset{}, fmt.Errorf("no start command: add a \"start\" script to package.json or set start.command in axiom.yaml")
		}
		p.Port = 3000
		p.BuildCommand = f.BuildCommand
		if p.BuildCommand == "" && f.HasBuildScript {
			p.BuildCommand = run(pm, "build")
		}
		p.StartCommand = f.StartCommand
		if p.StartCommand == "" {
			p.StartCommand = run(pm, "start")
		}
	}
	if f.Port > 0 && f.Framework != presets.FrameworkVite {
		p.Port = f.Port
	}
	return p, nil
}

func validPackageManager(pm string) bool {
	switch pm {
	case presets.PackageManagerNPM, presets.PackageManagerPNPM, presets.PackageManagerYarn, presets.PackageManagerBun:
		return true
	}
	return false
}

// run returns "<pm> run <script>" with the conventional short forms.
func run(pm, script string) string {
	switch {
	case script == "start" && (pm == presets.PackageManagerNPM || pm == presets.PackageManagerPNPM || pm == presets.PackageManagerYarn):
		return pm + " start"
	default:
		return pm + " run " + script
	}
}

func exec(pm, cmd string) string {
	switch pm {
	case presets.PackageManagerPNPM:
		return "pnpm exec " + cmd
	case presets.PackageManagerYarn:
		return "yarn " + cmd
	case presets.PackageManagerBun:
		return "bunx " + cmd
	default:
		return "npx " + cmd
	}
}
