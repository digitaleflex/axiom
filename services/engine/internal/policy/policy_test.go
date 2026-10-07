package policy

import (
	"strings"
	"testing"

	"github.com/digitaleflex/axiom/services/engine/internal/planner"
	"github.com/digitaleflex/axiom/services/engine/internal/profile"
)

func validPlan() planner.Plan {
	return planner.Plan{
		ID: "plan_1", SchemaVersion: 1, Status: "READY", ApplicationID: "app_1",
		ApplicationProfileVersion: 1, Strategy: "nextjs",
		Source:   profile.Source{RepositoryID: "repo_1", Ref: "main", Commit: strings.Repeat("a", 40)},
		ServerID: "srv_1", Environment: "production",
		Build:    planner.BuildPlan{Strategy: "source", PackageManager: "pnpm", Command: "pnpm run build"},
		Runtime:  planner.RuntimePlan{Type: "node", StartCommand: "pnpm start", Port: 3000, Configuration: []string{"DATABASE_URL"}},
		Network:  planner.NetworkPlan{Proxy: "traefik", Domain: "app.acme.dev", TLS: true, ExposedPort: 3000},
		Health:   planner.HealthPlan{Type: "http", Path: "/", ExpectedStatus: "200-399", TimeoutSeconds: 5, IntervalSeconds: 3, Retries: 20},
		Rollback: planner.RollbackPlan{Strategy: "keep_previous_until_verified"},
		Steps:    []string{"BUILD", "CREATE_RUNTIME", "NETWORK", "START", "VERIFY"},
		Fingerprint: planner.Fingerprint(planner.Plan{
			SchemaVersion: 1, ApplicationID: "app_1", ApplicationProfileVersion: 1, Strategy: "nextjs",
			Source:   profile.Source{RepositoryID: "repo_1", Ref: "main", Commit: strings.Repeat("a", 40)},
			ServerID: "srv_1", Environment: "production",
			Build:    planner.BuildPlan{Strategy: "source", PackageManager: "pnpm", Command: "pnpm run build"},
			Runtime:  planner.RuntimePlan{Type: "node", StartCommand: "pnpm start", Port: 3000, Configuration: []string{"DATABASE_URL"}},
			Network:  planner.NetworkPlan{Proxy: "traefik", Domain: "app.acme.dev", TLS: true, ExposedPort: 3000},
			Health:   planner.HealthPlan{Type: "http", Path: "/", ExpectedStatus: "200-399", TimeoutSeconds: 5, IntervalSeconds: 3, Retries: 20},
			Rollback: planner.RollbackPlan{Strategy: "keep_previous_until_verified"},
			Steps:    []string{"BUILD", "CREATE_RUNTIME", "NETWORK", "START", "VERIFY"},
		}),
	}
}

func TestAllowValidPlan(t *testing.T) {
	if d := Evaluate(validPlan(), "srv_1"); !d.Allow {
		t.Fatalf("valid plan denied: %v", d.Reasons)
	}
}

func TestDenyCases(t *testing.T) {
	cases := map[string]func(*planner.Plan) string{
		"unready":      func(p *planner.Plan) string { p.Status = "INVALID"; return "srv_1" },
		"tampered":     func(p *planner.Plan) string { p.Runtime.Port = 9999; return "srv_1" },
		"strategy":     func(p *planner.Plan) string { p.Strategy = "kubernetes"; p.Fingerprint = ""; return "srv_1" },
		"environment":  func(p *planner.Plan) string { p.Environment = "dev"; p.Fingerprint = ""; return "srv_1" },
		"wrong server": func(p *planner.Plan) string { return "srv_2" },
	}
	for name, mutate := range cases {
		p := validPlan()
		server := mutate(&p)
		if d := Evaluate(p, server); d.Allow {
			t.Errorf("%s: allowed", name)
		} else if len(d.Reasons) == 0 {
			t.Errorf("%s: no reasons", name)
		}
	}
}

func TestDenySecretInPlan(t *testing.T) {
	for _, cmd := range []string{
		"pnpm build --token=abc123",
		"export DB_PASSWORD=hunter2 && pnpm start",
		"echo -----BEGIN RSA PRIVATE KEY-----",
	} {
		p := validPlan()
		p.Build.Command = cmd
		p.Fingerprint = ""
		if d := Evaluate(p, "srv_1"); d.Allow {
			t.Errorf("allowed command %q", cmd)
		} else if found := strings.Join(d.Reasons, " "); !strings.Contains(found, "secret") {
			t.Errorf("reason must name the secret problem: %v", d.Reasons)
		}
	}
	// Names alone are fine.
	p := validPlan()
	p.Build.Command = "pnpm build"
	p.Fingerprint = ""
	if d := Evaluate(p, "srv_1"); !d.Allow {
		t.Errorf("names-only plan denied: %v", d.Reasons)
	}
}
