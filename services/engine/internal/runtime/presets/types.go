// Package presets holds the shared types for Axiom's runtime preset
// packages (#121+). Each supported runtime gets its own sub-package with
// safe, explicit, testable build/runtime presets; the shared vocabulary
// lives here so presets stay consistent.
package presets

// Package managers with dedicated runtime presets.
const (
	PackageManagerNPM  = "npm"
	PackageManagerPNPM = "pnpm"
	PackageManagerYarn = "yarn"
	PackageManagerBun  = "bun"
)

// Frameworks with dedicated runtime presets.
const (
	FrameworkNextJS = "Next.js"
	FrameworkVite   = "Vite"
)
