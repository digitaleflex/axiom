package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/digitaleflex/axiom/services/agent/internal/agent"
	"github.com/digitaleflex/axiom/services/agent/internal/config"
)

func main() {
	cfg := config.Load()
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	runtime := agent.NewRuntime(log)
	a := agent.New(agent.Config{ServerID: cfg.ServerID, EngineURL: cfg.EngineURL, Version: cfg.Version, Token: cfg.Token}, runtime, log)

	log.Info("axiom runtime agent started", "server_id", cfg.ServerID, "version", cfg.Version)

	if err := a.Run(ctx); err != nil {
		log.Error("axiom runtime agent stopped", "error", err)
		os.Exit(1)
	}
}
