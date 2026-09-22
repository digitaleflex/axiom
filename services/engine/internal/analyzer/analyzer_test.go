package analyzer

import "testing"

func TestAnalyzeDetectsNodeNextAndDocker(t *testing.T) {
	result := New().Analyze(RepositorySnapshot{
		Ref: "main",
		Files: []File{
			{Path: "package.json"},
			{Path: "pnpm-lock.yaml"},
			{Path: "next.config.ts"},
			{Path: "Dockerfile"},
			{Path: "src/app/page.tsx"},
		},
	})
	assertFinding(t, result, "package_manager", "pnpm")
	assertFinding(t, result, "framework", "Next.js")
	assertFinding(t, result, "container", "docker")
	assertFinding(t, result, "language", "TypeScript")
}

func assertFinding(t *testing.T, result Result, kind, value string) {
	t.Helper()
	for _, finding := range result.Findings {
		if finding.Kind == kind && finding.Value == value {
			return
		}
	}
	t.Fatalf("missing finding %s=%s", kind, value)
}
