package planner

import "testing"

func TestGeneratePlan(t *testing.T) {
	engine := New()
	plan, err := engine.Generate(
		ApplicationProfile{
			Language: "TypeScript", Framework: "Next.js", PackageManager: "pnpm",
			BuildCommand: "pnpm build", StartCommand: "pnpm start", Port: 3000,
			ContainerStrategy: "docker", Confidence: 0.98,
		},
		ServerProfile{ID: "srv_1", Status: "READY"},
		"app.example.com",
	)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Strategy != "docker" || plan.Network.Proxy != "traefik" || !plan.Network.TLS {
		t.Fatalf("unexpected plan: %+v", plan)
	}
	if len(plan.Steps) != 5 {
		t.Fatalf("expected 5 steps, got %d", len(plan.Steps))
	}
}

func TestGeneratePlanRejectsUnreadyServer(t *testing.T) {
	_, err := New().Generate(
		ApplicationProfile{Language: "Go", BuildCommand: "go build ./...", StartCommand: "./app", Port: 8080, Confidence: 1},
		ServerProfile{ID: "srv_1", Status: "OFFLINE"},
		"app.example.com",
	)
	if err == nil {
		t.Fatal("expected unready server to be rejected")
	}
}
