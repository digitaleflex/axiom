package planner

import (
	"errors"
	"testing"

	"github.com/digitaleflex/axiom/services/engine/internal/planner/validation"
	"github.com/digitaleflex/axiom/services/engine/internal/profile"
)

func composeProfile() profile.Profile {
	p := readyProfile()
	p.Preset = "compose"
	p.ContainerStrategy = profile.Field[string]{Value: "compose", Provenance: profile.ProvenanceDetected}
	p.BuildCommand = profile.Field[string]{}
	p.StartCommand = profile.Field[string]{}
	p.Services = []string{"web", "db"}
	p.PublicService = "web"
	return p
}

func composeRequest() Request {
	return Request{
		ApplicationID: "app_1",
		Profile:       composeProfile(),
		Server:        ServerProfile{ID: "srv_1", Status: "READY", Capabilities: []string{"docker", "traefik", "tls", "docker_compose"}},
		Environment:   "production",
		Domain:        "app.example.com",
	}
}

func hasField(errs validation.Errors, field string) bool {
	for _, e := range errs {
		if e.Field == field {
			return true
		}
	}
	return false
}

func TestGenerateComposePlanCarriesSelection(t *testing.T) {
	plan, err := New().Generate(composeRequest())
	if err != nil {
		t.Fatal(err)
	}
	if plan.Build.Strategy != "compose" || plan.Runtime.Type != "compose" {
		t.Fatalf("strategy/runtime = %s/%s", plan.Build.Strategy, plan.Runtime.Type)
	}
	if len(plan.Runtime.Services) != 2 || plan.Runtime.Services[0] != "web" || plan.Runtime.Services[1] != "db" {
		t.Fatalf("services = %v", plan.Runtime.Services)
	}
	if plan.Network.PublicService != "web" {
		t.Fatalf("public = %q", plan.Network.PublicService)
	}
}

func TestGenerateComposeRequiresServices(t *testing.T) {
	req := composeRequest()
	req.Profile.Services = nil
	req.Profile.PublicService = ""
	_, err := New().Generate(req)
	var verrs validation.Errors
	if !errors.As(err, &verrs) || !hasField(verrs, "runtime.services") {
		t.Fatalf("empty selection must be rejected: %v", err)
	}
}

func TestGenerateComposePublicMustBeSelected(t *testing.T) {
	req := composeRequest()
	req.Profile.PublicService = "ghost"
	_, err := New().Generate(req)
	var verrs validation.Errors
	if !errors.As(err, &verrs) || !hasField(verrs, "network.publicService") {
		t.Fatalf("public outside the selection must be rejected: %v", err)
	}
}

func TestGenerateComposeRejectsDuplicateServices(t *testing.T) {
	req := composeRequest()
	req.Profile.Services = []string{"web", "web"}
	_, err := New().Generate(req)
	var verrs validation.Errors
	if !errors.As(err, &verrs) || !hasField(verrs, "runtime.services") {
		t.Fatalf("duplicate services must be rejected: %v", err)
	}
}

func TestValidateComposeSelectionDependencyOrder(t *testing.T) {
	base := Plan{
		Build:   BuildPlan{Strategy: "compose"},
		Runtime: RuntimePlan{Services: []string{"api", "db"}},
		Network: NetworkPlan{PublicService: "api"},
	}
	if errs := validateComposeSelection(base); errs != nil {
		t.Fatalf("valid selection rejected: %v", errs)
	}
	// A dependency order that is a permutation of the selection is accepted.
	base.Runtime.DependencyOrder = []string{"db", "api"}
	if errs := validateComposeSelection(base); errs != nil {
		t.Fatalf("valid order rejected: %v", errs)
	}
	// An order that drops or adds a service is rejected.
	base.Runtime.DependencyOrder = []string{"db"}
	if errs := validateComposeSelection(base); !hasField(errs, "runtime.dependencyOrder") {
		t.Fatalf("incomplete order must be rejected: %v", errs)
	}
}

func TestValidateComposeSelectionIgnoresNonCompose(t *testing.T) {
	p := Plan{Build: BuildPlan{Strategy: "source"}, Runtime: RuntimePlan{}}
	if errs := validateComposeSelection(p); errs != nil {
		t.Fatalf("non-compose plans must not be checked: %v", errs)
	}
}
