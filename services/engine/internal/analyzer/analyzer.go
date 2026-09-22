package analyzer

import (
	"path/filepath"
	"strings"
)

type Analyzer struct{}

func New() *Analyzer { return &Analyzer{} }

func (a *Analyzer) Analyze(snapshot RepositorySnapshot) Result {
	result := Result{Version: 1, Ref: snapshot.Ref}
	for _, file := range snapshot.Files {
		path := filepath.ToSlash(file.Path)
		base := filepath.Base(path)
		lower := strings.ToLower(base)

		switch lower {
		case "package.json":
			result.Findings = append(result.Findings, Finding{Kind: "manifest", Value: "node", Evidence: []string{path}, Confidence: 1})
		case "pnpm-lock.yaml":
			result.Findings = append(result.Findings, Finding{Kind: "package_manager", Value: "pnpm", Evidence: []string{path}, Confidence: 1})
		case "yarn.lock":
			result.Findings = append(result.Findings, Finding{Kind: "package_manager", Value: "yarn", Evidence: []string{path}, Confidence: 1})
		case "package-lock.json":
			result.Findings = append(result.Findings, Finding{Kind: "package_manager", Value: "npm", Evidence: []string{path}, Confidence: 1})
		case "bun.lock", "bun.lockb":
			result.Findings = append(result.Findings, Finding{Kind: "package_manager", Value: "bun", Evidence: []string{path}, Confidence: 1})
		case "dockerfile":
			result.Findings = append(result.Findings, Finding{Kind: "container", Value: "docker", Evidence: []string{path}, Confidence: 1})
		case "docker-compose.yml", "docker-compose.yaml", "compose.yml", "compose.yaml":
			result.Findings = append(result.Findings, Finding{Kind: "container", Value: "compose", Evidence: []string{path}, Confidence: 1})
		case "pnpm-workspace.yaml":
			result.Findings = append(result.Findings, Finding{Kind: "workspace", Value: "pnpm", Evidence: []string{path}, Confidence: 1})
		}

		ext := strings.ToLower(filepath.Ext(path))
		switch ext {
		case ".go":
			result.Findings = append(result.Findings, Finding{Kind: "language", Value: "Go", Evidence: []string{path}, Confidence: 0.95})
		case ".ts", ".tsx":
			result.Findings = append(result.Findings, Finding{Kind: "language", Value: "TypeScript", Evidence: []string{path}, Confidence: 0.95})
		case ".js", ".jsx":
			result.Findings = append(result.Findings, Finding{Kind: "language", Value: "JavaScript", Evidence: []string{path}, Confidence: 0.9})
		case ".py":
			result.Findings = append(result.Findings, Finding{Kind: "language", Value: "Python", Evidence: []string{path}, Confidence: 0.95})
		}

		switch {
		case strings.HasSuffix(path, "next.config.js"), strings.HasSuffix(path, "next.config.mjs"), strings.HasSuffix(path, "next.config.ts"):
			result.Findings = append(result.Findings, Finding{Kind: "framework", Value: "Next.js", Evidence: []string{path}, Confidence: 1})
		case strings.HasSuffix(path, "vite.config.ts"), strings.HasSuffix(path, "vite.config.js"):
			result.Findings = append(result.Findings, Finding{Kind: "framework", Value: "Vite", Evidence: []string{path}, Confidence: 1})
		}
	}

	if len(snapshot.Files) == 0 {
		result.Warnings = append(result.Warnings, "repository snapshot contains no files")
	}
	return result
}
