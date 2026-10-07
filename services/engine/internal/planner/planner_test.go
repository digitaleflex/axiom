package planner

import (
	"testing"

	"github.com/digitaleflex/axiom/services/engine/internal/profile"
)

func readyProfile() profile.Profile {
	return profile.Profile{
		Status: profile.StatusReady, Preset: "nextjs", Version: 3,
		PackageManager:    profile.Field[string]{Value: "pnpm", Provenance: profile.ProvenanceDetected},
		BuildCommand:      profile.Field[string]{Value: "pnpm run build", Provenance: profile.ProvenanceDetected},
		StartCommand:      profile.Field[string]{Value: "pnpm start", Provenance: profile.ProvenanceDetected},
		Port:              profile.Field[int]{Value: 3000, Provenance: profile.ProvenanceDefault},
		ContainerStrategy: profile.Field[string]{Value: "source", Provenance: profile.ProvenanceDefault},
		HealthCheck:       profile.Field[profile.HealthCheck]{Value: profile.HealthCheck{Type: "http", Path: "/"}, Provenance: profile.ProvenanceDefault},
	}
}

func TestGeneratePlan(t *testing.T) {
	plan, err := New().Generate(readyProfile(), ServerProfile{ID: "srv_1", Status: "READY"}, "app.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Strategy != "nextjs" || plan.ApplicationProfileVersion != 3 || plan.Network.Proxy != "traefik" || !plan.Network.TLS || plan.Runtime.Port != 3000 {
		t.Fatalf("unexpected plan: %+v", plan)
	}
	if len(plan.Steps) != 5 {
		t.Fatalf("expected 5 steps, got %d", len(plan.Steps))
	}
}

func TestGeneratePlanRejectsUnreadyInputs(t *testing.T) {
	if _, err := New().Generate(readyProfile(), ServerProfile{ID: "srv_1", Status: "OFFLINE"}, "app.example.com"); err == nil {
		t.Fatal("expected unready server to be rejected")
	}
	p := readyProfile()
	p.Status = profile.StatusNeedsReview
	if _, err := New().Generate(p, ServerProfile{ID: "srv_1", Status: "READY"}, "app.example.com"); err == nil {
		t.Fatal("profiles that need review must not be planned")
	}
}
