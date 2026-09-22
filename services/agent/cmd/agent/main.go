package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/digitaleflex/axiom/services/agent/internal/config"
	"github.com/digitaleflex/axiom/services/agent/internal/agent"
)

func main() {
	cfg := config.Load()
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	runtime := agent.NewRuntime(log)
	a := agent.New(cfg, runtime, log)

	log.Info("axiom runtime agent started", "server_id", cfg.ServerID, "version", cfg.Version)

	if err := a.Run(ctx); err != nil {
		log.Error("axiom runtime agent stopped", "error", err)
		os.Exit(1)
	}
}
