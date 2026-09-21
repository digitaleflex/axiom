package main

import (
	"context"
	"errors"
	"os/signal"
	"syscall"
	"time"

	"github.com/digitaleflex/axiom/services/engine/internal/config"
	"github.com/digitaleflex/axiom/services/engine/internal/httpserver"
	"github.com/digitaleflex/axiom/services/engine/internal/logger"
)

func main() {
	cfg := config.Load()
	log := logger.New()
	server := httpserver.New(cfg)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	serverErr := make(chan error, 1)
	go func() {
		serverErr <- server.ListenAndServe()
	}()

	log.Info("axiom engine started", "host", cfg.Host, "port", cfg.Port, "version", cfg.Version)

	select {
	case err := <-serverErr:
		if !errors.Is(err, context.Canceled) {
			log.Error("axiom engine stopped unexpectedly", "error", err)
		}
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := server.ShutdownContext(shutdownCtx); err != nil {
			log.Error("axiom engine shutdown failed", "error", err)
			return
		}
		log.Info("axiom engine stopped")
	}
}
