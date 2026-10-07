package compose

import (
	"strings"
	"testing"
)

func TestImageRef(t *testing.T) {
	if got := ImageRef("Acme Web", "my_service", "abcdef0123456789abcdef0123456789abcdef01"); got != "axiom-acme-web-my-service:abcdef0123456789abcdef0123456789abcdef01" {
		t.Fatalf("ImageRef = %q", got)
	}
}

func TestBuildPlanOrderAndDefaults(t *testing.T) {
	doc := parseGolden(t, "valid.yaml")
	sel := doc.SelectServices(nil, "")
	bp := doc.BuildPlan(sel, "acme", strings.Repeat("a", 40))

	// Only api has a build section; db uses a prebuilt image.
	if len(bp.Services) != 1 {
		t.Fatalf("build services = %+v", bp.Services)
	}
	b := bp.Services[0]
	if b.Service != "api" || b.Context != "." || b.Dockerfile != "Dockerfile" {
		t.Fatalf("build input = %+v", b)
	}
	if b.Image != "axiom-acme-api:"+strings.Repeat("a", 40) {
		t.Fatalf("image = %q", b.Image)
	}
	if bp.Public != "api" {
		t.Fatalf("public = %q", bp.Public)
	}
}

func TestBuildPlanContextDefaults(t *testing.T) {
	doc, err := Parse([]byte("services:\n  web:\n    build:\n      dockerfile: deploy/Dockerfile\n"))
	if err != nil {
		t.Fatal(err)
	}
	sel := doc.SelectServices(nil, "")
	bp := doc.BuildPlan(sel, "app", "deadbeef")
	if len(bp.Services) != 1 || bp.Services[0].Context != "." || bp.Services[0].Dockerfile != "deploy/Dockerfile" {
		t.Fatalf("build = %+v", bp.Services)
	}
}

func TestRewriteReplacesBuildWithImage(t *testing.T) {
	content := golden(t, "valid.yaml")
	doc := parseGolden(t, "valid.yaml")
	sel := doc.SelectServices(nil, "")
	bp := doc.BuildPlan(sel, "acme", strings.Repeat("b", 40))

	out, err := Rewrite(content, bp, nil)
	if err != nil {
		t.Fatal(err)
	}
	rewritten, err := Parse(out)
	if err != nil {
		t.Fatalf("rewritten document must stay parseable: %v\n%s", err, out)
	}
	api := rewritten.Services["api"]
	if api.Build != nil {
		t.Fatalf("build must be removed: %+v", api.Build)
	}
	if api.Image != "axiom-acme-api:"+strings.Repeat("b", 40) {
		t.Fatalf("image = %q\n%s", api.Image, out)
	}
	// Unmodelled configuration is preserved.
	if len(api.Environment) != 2 || api.Environment[0] != "DATABASE_URL" {
		t.Fatalf("environment lost: %v", api.Environment)
	}
	if len(api.DependsOn) != 1 || api.DependsOn[0] != "db" {
		t.Fatalf("depends_on lost: %v", api.DependsOn)
	}
	// The prebuilt service is untouched.
	if rewritten.Services["db"].Image != "postgres:16-alpine" {
		t.Fatalf("db image changed: %q", rewritten.Services["db"].Image)
	}
}

func TestRewriteKeepsOnlySelectedServices(t *testing.T) {
	content := golden(t, "valid.yaml")
	doc := parseGolden(t, "valid.yaml")
	sel := doc.SelectServices([]string{"db"}, "")
	bp := doc.BuildPlan(sel, "acme", "cafe")

	out, err := Rewrite(content, bp, sel.Services)
	if err != nil {
		t.Fatal(err)
	}
	rewritten, err := Parse(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(rewritten.Services) != 1 {
		t.Fatalf("services = %v\n%s", rewritten.ServiceNames(), out)
	}
	if _, ok := rewritten.Services["db"]; !ok {
		t.Fatalf("db missing: %s", out)
	}
}
