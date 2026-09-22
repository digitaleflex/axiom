package agent

import (
	"context"
	"log/slog"
	"testing"
)

type fakeRuntime struct{}

func (fakeRuntime) Capabilities(context.Context) ([]string, error) {
	return []string{"docker", "traefik"}, nil
}
func (fakeRuntime) Prepare(context.Context, string) error { return nil }
func (fakeRuntime) CreateRuntime(context.Context, string, string, string, int) error { return nil }
func (fakeRuntime) ConfigureNetwork(context.Context, string, string, string, string, bool, int) error { return nil }
func (fakeRuntime) StartRuntime(context.Context, string, string) error { return nil }
func (fakeRuntime) HealthCheck(context.Context, string, string, string, int) error { return nil }

func TestAgentRequiresServerID(t *testing.T) {
	a := New(Config{}, fakeRuntime{}, slog.Default())
	if err := a.Run(context.Background()); err == nil {
		t.Fatal("expected missing server ID error")
	}
}

func TestAgentStopsOnContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	a := New(Config{ServerID: "srv_1", Version: "0.1.0"}, fakeRuntime{}, slog.Default())
	if err := a.Run(ctx); err == nil {
		t.Fatal("expected context cancellation")
	}
}
