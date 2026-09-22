package planner

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

type Engine struct{}

func New() *Engine { return &Engine{} }

func (e *Engine) Generate(profile ApplicationProfile, server ServerProfile, domain string) (Plan, error) {
	if err := validateProfile(profile); err != nil {
		return Plan{}, err
	}
	if err := validateServer(server); err != nil {
		return Plan{}, err
	}
	if domain == "" {
		return Plan{}, fmt.Errorf("domain is required")
	}
	id := "plan_" + id()
	return Plan{
		ID: id,
		ApplicationProfileVersion: 1,
		ServerID: server.ID,
		Strategy: "docker",
		Build: BuildPlan{Strategy: "docker", PackageManager: profile.PackageManager, Command: profile.BuildCommand},
		Runtime: RuntimePlan{Type: profile.Framework, StartCommand: profile.StartCommand, Port: profile.Port},
		Network: NetworkPlan{Proxy: "traefik", Domain: domain, TLS: true, ExposedPort: profile.Port},
		Health: HealthPlan{Type: "http", Path: "/", TimeoutSeconds: 30},
		Rollback: RollbackPlan{Strategy: "previous_release"},
		Steps: []Step{
			{Name: "BUILD", Order: 1},
			{Name: "CREATE_RUNTIME", Order: 2},
			{Name: "NETWORK", Order: 3},
			{Name: "START", Order: 4},
			{Name: "VERIFY", Order: 5},
		},
	}, nil
}

func id() string {
	b := make([]byte, 10)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", len(b))
	}
	return hex.EncodeToString(b)
}
