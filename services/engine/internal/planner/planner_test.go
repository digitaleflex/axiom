package planner

import (
	"errors"
	"strings"
	"testing"

	"github.com/digitaleflex/axiom/services/engine/internal/planner/validation"
	"github.com/digitaleflex/axiom/services/engine/internal/profile"
)

func readyProfile() profile.Profile {
	return profile.Profile{
		Status: profile.StatusReady, Preset: "nextjs", Version: 3, AnalysisID: "analysis_1",
		Source:            profile.Source{RepositoryID: "repo_1", Ref: "main", Commit: strings.Repeat("a", 40)},
		PackageManager:    profile.Field[string]{Value: "pnpm", Provenance: profile.ProvenanceDetected},
		BuildCommand:      profile.Field[string]{Value: "pnpm run build", Provenance: profile.ProvenanceDetected},
		StartCommand:      profile.Field[string]{Value: "pnpm start", Provenance: profile.ProvenanceDetected},
		Port:              profile.Field[int]{Value: 3000, Provenance: profile.ProvenanceDefault},
		ContainerStrategy: profile.Field[string]{Value: "source", Provenance: profile.ProvenanceDefault},
		HealthCheck:       profile.Field[profile.HealthCheck]{Value: profile.HealthCheck{Type: "http", Path: "/"}, Provenance: profile.ProvenanceDefault},
		Configuration:     []profile.ConfigRequirement{{Name: "DATABASE_URL", Required: true, Secret: true}},
	}
}

func readyServer() ServerProfile {
	return ServerProfile{ID: "srv_1", Status: "READY", Capabilities: []string{"docker", "traefik", "tls"}}
}

func request() Request {
	return Request{ApplicationID: "app_1", Profile: readyProfile(), Server: readyServer(), Environment: "production", Domain: "App.Example.com"}
}

func TestGeneratePlan(t *testing.T) {
	plan, err := New().Generate(request())
	if err != nil {
		t.Fatal(err)
	}
	if plan.Strategy != "nextjs" || plan.ApplicationProfileVersion != 3 || plan.Network.Domain != "app.example.com" || !plan.Network.TLS ||
		plan.Runtime.Port != 3000 || plan.Runtime.Configuration[0] != "DATABASE_URL" || plan.Status != "READY" {
		t.Fatalf("unexpected plan: %+v", plan)
	}
	if strings.Join(plan.Steps, ",") != "BUILD,CREATE_RUNTIME,NETWORK,START,VERIFY" || len(plan.FailureBoundaries) != 5 {
		t.Fatalf("steps = %v boundaries = %d", plan.Steps, len(plan.FailureBoundaries))
	}
	if !strings.HasPrefix(plan.Fingerprint, "sha256:") || plan.Rollback.Strategy == "" {
		t.Fatalf("fingerprint/rollback = %s %+v", plan.Fingerprint, plan.Rollback)
	}
}

func TestDeterministicFingerprint(t *testing.T) {
	a, _ := New().Generate(request())
	b, _ := New().Generate(request())
	if a.Fingerprint != b.Fingerprint {
		t.Fatal("identical inputs must produce identical fingerprints")
	}
	a.ID, b.ID = "plan_1", "plan_2"
	if Fingerprint(a) != Fingerprint(b) {
		t.Fatal("IDs must not affect the fingerprint")
	}
	req := request()
	req.Environment = "staging"
	c, _ := New().Generate(req)
	if c.Fingerprint == a.Fingerprint {
		t.Fatal("different inputs must change the fingerprint")
	}
}

func TestEligibility(t *testing.T) {
	req := request()
	req.Server = ServerProfile{ID: "srv_1", Status: "OFFLINE", Capabilities: []string{"docker"}}
	_, err := New().Generate(req)
	var el *EligibilityError
	if !errors.As(err, &el) || len(el.Reasons) != 2 || !errors.Is(err, ErrNotEligible) {
		t.Fatalf("all failing requirements must be listed: %v", err)
	}
	req = request()
	req.Profile.Preset = "compose"
	req.Profile.ContainerStrategy.Value = "compose"
	if _, err := New().Generate(req); !errors.As(err, &el) || !strings.Contains(err.Error(), "docker_compose") {
		t.Fatalf("compose requires docker_compose: %v", err)
	}
}

func TestRejectsNotReadyProfileAndInvalidInputs(t *testing.T) {
	req := request()
	req.Profile.Status = profile.StatusNeedsReview
	if _, err := New().Generate(req); !errors.Is(err, ErrProfileNotReady) {
		t.Fatalf("needs_review must be refused: %v", err)
	}
	req = request()
	req.Domain = "not a domain"
	req.Environment = "dev"
	_, err := New().Generate(req)
	var verrs validation.Errors
	if !errors.As(err, &verrs) || len(verrs) != 2 {
		t.Fatalf("expected field errors for domain and environment, got %v", err)
	}
	req = request()
	req.Profile.Source.Commit = "main"
	if _, err := New().Generate(req); !errors.As(err, &verrs) || verrs[0].Field != "source.commit" {
		t.Fatalf("plans must pin an exact commit: %v", err)
	}
}
