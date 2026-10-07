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
		ID:                        id,
		ApplicationProfileVersion: profile.Version,
		ServerID:                  server.ID,
		Strategy:                  profile.Preset,
		Build:                     BuildPlan{Strategy: profile.ContainerStrategy.Value, PackageManager: profile.PackageManager.Value, Command: profile.BuildCommand.Value},
		Runtime:                   RuntimePlan{Type: profile.Preset, StartCommand: profile.StartCommand.Value, Port: profile.Port.Value},
		Network:                   NetworkPlan{Proxy: "traefik", Domain: domain, TLS: true, ExposedPort: profile.Port.Value},
		Health:                    HealthPlan{Type: profile.HealthCheck.Value.Type, Path: profile.HealthCheck.Value.Path, TimeoutSeconds: 30},
		Rollback:                  RollbackPlan{Strategy: "previous_release"},
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
