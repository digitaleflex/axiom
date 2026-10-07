// Command engine runs the Axiom Engine (control plane).
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/digitaleflex/axiom/services/engine/internal/bootstrap"
	"github.com/digitaleflex/axiom/services/engine/internal/config"
	"github.com/digitaleflex/axiom/services/engine/internal/logger"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "axiom engine:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}
	log := logger.New(cfg.LogLevel)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	app, err := bootstrap.New(ctx, cfg, log)
	if err != nil {
		log.Error("startup failed", "error", err)
		return err
	}
	return app.Run(ctx)
}
