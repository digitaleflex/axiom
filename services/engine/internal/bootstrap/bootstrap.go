// Package bootstrap is the Engine composition root (issue #114): it wires
// configuration, database lifecycle, domain services and the HTTP server.
// Domain packages never construct their own dependencies.
package bootstrap

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver

	"github.com/digitaleflex/axiom/services/engine/internal/api"
	"github.com/digitaleflex/axiom/services/engine/internal/api/sse"
	"github.com/digitaleflex/axiom/services/engine/internal/config"
	"github.com/digitaleflex/axiom/services/engine/internal/database"
	deploymentdb "github.com/digitaleflex/axiom/services/engine/internal/database/deployment"
	"github.com/digitaleflex/axiom/services/engine/internal/deployment"
	"github.com/digitaleflex/axiom/services/engine/internal/httpserver"
	"github.com/digitaleflex/axiom/services/engine/migrations"
)

// App is a fully wired Engine instance.
type App struct {
	cfg config.Config
	// streams is cancelled when shutdown begins so long-lived streams (SSE)
	// terminate instead of blocking graceful shutdown.
	streams       context.Context
	cancelStreams context.CancelFunc
	log           *slog.Logger
	db            *sql.DB
	handler       *httpserver.Handler
	server        *http.Server
}

// New wires the Engine. It fails fast when a required dependency is unavailable.
func New(ctx context.Context, cfg config.Config, log *slog.Logger) (*App, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}
	if log == nil {
		log = slog.Default()
	}

	db, err := openDatabase(ctx, cfg, log)
	if err != nil {
		return nil, err
	}

	streams, cancelStreams := context.WithCancel(context.Background())
	deps, err := buildAPIDeps(ctx, cfg, log, db, streams)
	if err != nil {
		cancelStreams()
		if db != nil {
			_ = db.Close()
		}
		return nil, err
	}
	handler := httpserver.NewHandler(cfg, db, api.New(deps))
	app := &App{
		cfg:           cfg,
		streams:       streams,
		cancelStreams: cancelStreams,
		log:           log,
		db:            db,
		handler:       handler,
		server: &http.Server{
			Addr:              cfg.Addr(),
			Handler:           handler,
			ReadHeaderTimeout: 5 * time.Second,
			IdleTimeout:       120 * time.Second,
			// No WriteTimeout: SSE streams are long-lived (ADR-0009).
		},
	}
	return app, nil
}

// localOperator is the interim principal until user authentication (#125).
var localOperator = api.Principal{UserID: "usr_local", Name: "Local operator"}

// buildAPIDeps wires stores and services. Without a database, data endpoints
// answer 503 SERVICE_UNAVAILABLE instead of serving in-memory state.
func buildAPIDeps(ctx context.Context, cfg config.Config, log *slog.Logger, db *sql.DB, streams context.Context) (api.Deps, error) {
	deps := api.Deps{Log: log}
	switch {
	case cfg.APIToken != "":
		deps.Auth = api.NewTokenAuthenticator(cfg.APIToken, localOperator)
	case cfg.Env == config.EnvDevelopment:
		log.Warn("API authentication disabled: development mode without AXIOM_API_TOKEN")
		deps.Auth = api.DevAuthenticator{Principal: localOperator}
	default:
		log.Warn("no API authenticator configured; all /api/v1 requests will be rejected")
	}
	if db == nil {
		return deps, nil
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO users (id, display_name) VALUES ($1, $2) ON CONFLICT (id) DO NOTHING`,
		localOperator.UserID, localOperator.Name); err != nil {
		return api.Deps{}, fmt.Errorf("ensure local operator: %w", err)
	}
	deps.Deployments = deployment.NewService(deploymentdb.New(db), deployment.NewEventBus())
	deps.EventStream = &sse.Handler{Store: deps.Deployments.Store(), Bus: deps.Deployments.Events(), Log: log, Shutdown: streams}
	deps.Applications = database.NewApplicationStore(db)
	deps.Servers = database.NewRepositories(db).Servers
	return deps, nil
}

// openDatabase connects and migrates. Optional databases degrade to nil with a warning.
func openDatabase(ctx context.Context, cfg config.Config, log *slog.Logger) (*sql.DB, error) {
	if cfg.Database.URL == "" {
		if cfg.Database.Required {
			return nil, errors.New("database is required but DATABASE_URL is not configured")
		}
		log.Warn("running without database", "env", cfg.Env)
		return nil, nil
	}

	connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	db, err := database.Open(connectCtx, "pgx", database.Config{
		URL:             cfg.Database.URL,
		MaxOpenConns:    cfg.Database.MaxOpenConns,
		MaxIdleConns:    cfg.Database.MaxIdleConns,
		ConnMaxLifetime: cfg.Database.ConnMaxLifetime,
	})
	if err == nil {
		if err = migrations.Run(connectCtx, db); err != nil {
			_ = db.Close()
			err = fmt.Errorf("database migrations failed: %w", err)
		}
	}
	if err != nil {
		if cfg.Database.Required {
			return nil, err
		}
		log.Warn("database unavailable, continuing without it", "error", err)
		return nil, nil
	}
	return db, nil
}

// Handler exposes the root HTTP handler (tests).
func (a *App) Handler() http.Handler { return a.handler }

// DB exposes the database handle; nil when running without database.
func (a *App) DB() *sql.DB { return a.db }

// Run listens until ctx is cancelled, then drains and shuts down gracefully.
// The listener is bound before Run returns control so port errors surface immediately.
func (a *App) Run(ctx context.Context) error {
	ln, err := net.Listen("tcp", a.server.Addr)
	if err != nil {
		a.close()
		return fmt.Errorf("listen %s: %w", a.server.Addr, err)
	}
	return a.Serve(ctx, ln)
}

// Serve is Run with a caller-provided listener.
func (a *App) Serve(ctx context.Context, ln net.Listener) error {
	defer a.close()
	a.log.Info("axiom engine started", "addr", ln.Addr().String(), "env", a.cfg.Env, "version", a.cfg.Version, "database", a.db != nil)

	serveErr := make(chan error, 1)
	go func() { serveErr <- a.server.Serve(ln) }()

	select {
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("http server: %w", err)
	case <-ctx.Done():
	}

	a.handler.SetDraining()
	a.cancelStreams()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), a.cfg.ShutdownTimeout)
	defer cancel()
	if err := a.server.Shutdown(shutdownCtx); err != nil {
		// Long-lived streams that do not finish in time are closed forcibly.
		_ = a.server.Close()
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	a.log.Info("axiom engine stopped")
	return nil
}

func (a *App) close() {
	a.cancelStreams()
	if a.db != nil {
		if err := a.db.Close(); err != nil {
			a.log.Error("closing database", "error", err)
		}
		a.db = nil
	}
}
