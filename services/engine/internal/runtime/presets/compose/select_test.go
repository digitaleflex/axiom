package compose

import (
	"reflect"
	"testing"
)

func TestSelectServicesDefaultsToAllAndTopoOrder(t *testing.T) {
	doc := parseGolden(t, "valid.yaml")
	res := doc.SelectServices(nil, "")

	if !reflect.DeepEqual(res.Services, []string{"api", "db"}) {
		t.Fatalf("services = %v", res.Services)
	}
	// db is a dependency of api and must come first.
	if !reflect.DeepEqual(res.DependencyOrder, []string{"db", "api"}) {
		t.Fatalf("order = %v", res.DependencyOrder)
	}
	if res.Public != "api" {
		t.Fatalf("public = %q", res.Public)
	}
	if res.HasErrors() {
		t.Fatalf("valid document has errors: %+v", res.Errors())
	}
}

func TestSelectServicesClosesOverDependencies(t *testing.T) {
	doc := parseGolden(t, "valid.yaml")
	res := doc.SelectServices([]string{"api"}, "api")

	if !reflect.DeepEqual(res.Services, []string{"api", "db"}) {
		t.Fatalf("include api must pull db: %v", res.Services)
	}
	if !reflect.DeepEqual(res.DependencyOrder, []string{"db", "api"}) {
		t.Fatalf("order = %v", res.DependencyOrder)
	}
	if res.HasErrors() {
		t.Fatalf("unexpected errors: %+v", res.Errors())
	}
}

func TestSelectServicesUnknownInclude(t *testing.T) {
	doc := parseGolden(t, "valid.yaml")
	res := doc.SelectServices([]string{"missing"}, "")
	if sev, ok := severityOf(res.Issues, RuleUnknownService); !ok || sev != SeverityError {
		t.Fatalf("rules = %v", rules(res.Issues))
	}
}

func TestSelectServicesPublicNotIncluded(t *testing.T) {
	doc := parseGolden(t, "valid.yaml")
	res := doc.SelectServices([]string{"db"}, "api")
	if sev, ok := severityOf(res.Issues, RulePublicNotIncluded); !ok || sev != SeverityError {
		t.Fatalf("rules = %v", rules(res.Issues))
	}
}

func TestSelectServicesNoPublicWarns(t *testing.T) {
	doc := parseGolden(t, "valid.yaml")
	res := doc.SelectServices([]string{"db"}, "")
	if res.Public != "" {
		t.Fatalf("public = %q", res.Public)
	}
	if sev, ok := severityOf(res.Issues, RuleNoPublicService); !ok || sev != SeverityWarn {
		t.Fatalf("rules = %v", rules(res.Issues))
	}
}

func TestSelectServicesCycle(t *testing.T) {
	doc := parseGolden(t, "cycle.yaml")
	res := doc.SelectServices(nil, "")
	if sev, ok := severityOf(res.Issues, RuleDependencyCycle); !ok || sev != SeverityError {
		t.Fatalf("rules = %v", rules(res.Issues))
	}
	if !res.HasErrors() {
		t.Fatal("a cycle must be blocking")
	}
}

func TestSelectServicesUnknownDependency(t *testing.T) {
	doc, err := Parse([]byte("services:\n  web:\n    build: .\n    depends_on:\n      - ghost\n"))
	if err != nil {
		t.Fatal(err)
	}
	res := doc.SelectServices(nil, "")
	if sev, ok := severityOf(res.Issues, RuleUnknownDependency); !ok || sev != SeverityError {
		t.Fatalf("rules = %v", rules(res.Issues))
	}
}

func TestSelectServicesNonPublicPublishIsRejected(t *testing.T) {
	doc := parseGolden(t, "nonpublic-publish.yaml")
	// web is the first publishing service, so admin is non-public.
	res := doc.SelectServices(nil, "web")
	if res.Public != "web" {
		t.Fatalf("public = %q", res.Public)
	}
	if sev, ok := severityOf(res.Issues, RulePublicHostBind); !ok || sev != SeverityError {
		t.Fatalf("rules = %v", rules(res.Issues))
	}
}

func TestSelectServicesPublicMayPublish(t *testing.T) {
	// The public service may publish on all interfaces; a non-public service
	// that binds only localhost is fine too.
	doc, err := Parse([]byte("services:\n  web:\n    build: .\n    ports:\n      - \"8080:3000\"\n  admin:\n    build: ./admin\n    ports:\n      - \"127.0.0.1:9000:9000\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	res := doc.SelectServices(nil, "web")
	if res.Public != "web" {
		t.Fatalf("public = %q", res.Public)
	}
	if hasRule(res.Issues, RulePublicHostBind) {
		t.Fatalf("localhost-only non-public publish must be allowed: %v", rules(res.Issues))
	}
	if res.HasErrors() {
		t.Fatalf("unexpected errors: %+v", res.Errors())
	}
}

func TestSelectServicesDeterministic(t *testing.T) {
	doc := parseGolden(t, "valid.yaml")
	a := doc.SelectServices(nil, "")
	b := doc.SelectServices(nil, "")
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("selection is not deterministic:\n%+v\n%+v", a, b)
	}
}
