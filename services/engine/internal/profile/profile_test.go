package profile

import (
	"testing"

	"github.com/digitaleflex/axiom/services/engine/internal/analyzer"
)

func TestBuildApplicationProfile(t *testing.T) {
	result := analyzer.New().Analyze(analyzer.RepositorySnapshot{
		Ref: "main",
		Files: []analyzer.File{
			{Path: "package.json"},
			{Path: "pnpm-lock.yaml"},
			{Path: "next.config.ts"},
			{Path: "src/app/page.tsx"},
		},
	})
	p, err := NewBuilder().Build(result)
	if err != nil { t.Fatal(err) }
	if p.Framework != "Next.js" || p.PackageManager != "pnpm" || p.Port != 3000 {
		t.Fatalf("unexpected profile: %+v", p)
	}
}
