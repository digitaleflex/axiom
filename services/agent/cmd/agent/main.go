package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/digitaleflex/axiom/services/agent/internal/bootstrap"
	"github.com/digitaleflex/axiom/services/agent/internal/config"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	cfg, err := config.Load()
	if err != nil {
		log.Error("invalid agent configuration", "error", err.Error())
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// The composition root constructs identity, credentials, state,
	// capabilities, the Docker/Traefik/health adapters, the dispatcher, restart
	// recovery and the inbound operation listener, then owns start/drain/stop.
	app, err := bootstrap.New(ctx, cfg, log)
	if err != nil {
		log.Error("axiom runtime agent could not start", "error", err.Error())
		os.Exit(1)
	}

	if err := app.Run(ctx); err != nil {
		log.Error("axiom runtime agent stopped", "error", err.Error())
		os.Exit(1)
	}
}
