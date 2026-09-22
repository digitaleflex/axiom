package agent

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

type Agent struct {
	cfg     Config
	runtime Runtime
	log     *slog.Logger
}

func New(cfg Config, runtime Runtime, log *slog.Logger) *Agent {
	if log == nil {
		log = slog.Default()
	}
	return &Agent{cfg: cfg, runtime: runtime, log: log}
}

func (a *Agent) Run(ctx context.Context) error {
	if a.cfg.ServerID == "" {
		return fmt.Errorf("AXIOM_SERVER_ID is required")
	}
	if a.runtime == nil {
		return fmt.Errorf("runtime is required")
	}

	capabilities, err := a.runtime.Capabilities(ctx)
	if err != nil {
		return fmt.Errorf("discover capabilities: %w", err)
	}

	a.log.Info("runtime capabilities discovered", "capabilities", capabilities)

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			a.log.Info("agent heartbeat", "server_id", a.cfg.ServerID, "version", a.cfg.Version)
		}
	}
}
