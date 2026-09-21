package main

import (
	"context"
	"errors"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/stdlib"

	"github.com/digitaleflex/axiom/services/engine/internal/config"
	"github.com/digitaleflex/axiom/services/engine/internal/database"
	"github.com/digitaleflex/axiom/services/engine/internal/httpserver"
	"github.com/digitaleflex/axiom/services/engine/internal/logger"
)

func main() {
	cfg := config.Load()
	log := logger.New()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var dbClose func() error
	var db = openDatabase(ctx, cfg, log, &dbClose)
	server := httpserver.New(cfg, db)

	serverErr := make(chan error, 1)
	go func() {
		serverErr <- server.ListenAndServe()
	}()

	log.Info("axiom engine started", "host", cfg.Host, "port", cfg.Port, "version", cfg.Version, "database", db != nil)

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
		if dbClose != nil {
			if err := dbClose(); err != nil {
				log.Error("database close failed", "error", err)
			}
		}
		log.Info("axiom engine stopped")
	}
}

func openDatabase(ctx context.Context, cfg config.Config, log interface{ Error(string, ...any) }) interfaceDatabase {
	if cfg.Database.URL == "" {
		if cfg.Database.Required {
			log.Error("database is required but DATABASE_URL is not configured")
		}
		return nil
	}

	db, err := database.Open(ctx, stdlib.GetDefaultDriver(), database.Config{
		URL:             cfg.Database.URL,
		MaxOpenConns:    cfg.Database.MaxOpenConns,
		MaxIdleConns:    cfg.Database.MaxIdleConns,
		ConnMaxLifetime: cfg.Database.ConnMaxLifetime,
	})
	if err != nil {
		log.Error("database connection failed", "error", err)
		if cfg.Database.Required {
			return nil
		}
		return nil
	}
	return db
}

type interfaceDatabase interface {
	PingContext(context.Context) error
}
