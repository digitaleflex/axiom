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
	"os"
	"path/filepath"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver

	"github.com/digitaleflex/axiom/services/engine/internal/agentauth"
	"github.com/digitaleflex/axiom/services/engine/internal/agentclient"
	"github.com/digitaleflex/axiom/services/engine/internal/analysis"
	"github.com/digitaleflex/axiom/services/engine/internal/api"
	"github.com/digitaleflex/axiom/services/engine/internal/api/sse"
	"github.com/digitaleflex/axiom/services/engine/internal/audit"
	"github.com/digitaleflex/axiom/services/engine/internal/auth"
	"github.com/digitaleflex/axiom/services/engine/internal/authz"
	"github.com/digitaleflex/axiom/services/engine/internal/build"
	"github.com/digitaleflex/axiom/services/engine/internal/build/workspace"
	"github.com/digitaleflex/axiom/services/engine/internal/config"
	"github.com/digitaleflex/axiom/services/engine/internal/database"
	deploymentdb "github.com/digitaleflex/axiom/services/engine/internal/database/deployment"
	"github.com/digitaleflex/axiom/services/engine/internal/deployment"
	"github.com/digitaleflex/axiom/services/engine/internal/domains"
	"github.com/digitaleflex/axiom/services/engine/internal/executor"
	ghauth "github.com/digitaleflex/axiom/services/engine/internal/github/auth"
	"github.com/digitaleflex/axiom/services/engine/internal/github/repos"
	"github.com/digitaleflex/axiom/services/engine/internal/httpserver"
	"github.com/digitaleflex/axiom/services/engine/internal/logs"
	"github.com/digitaleflex/axiom/services/engine/internal/observability/metrics"
	"github.com/digitaleflex/axiom/services/engine/internal/planner"
	appconfig "github.com/digitaleflex/axiom/services/engine/internal/secrets"
	"github.com/digitaleflex/axiom/services/engine/internal/security/secrets"
	"github.com/digitaleflex/axiom/services/engine/internal/server"
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
	// runner owns the in-flight deployment executions (#100). Shutdown cancels
	// them and waits, so a deployment is never left half-executed because the
	// process went away.
	runner *executor.Runner
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
	registry, err := metrics.NewEngineRegistry()
	if err != nil {
		cancelStreams()
		if db != nil {
			_ = db.Close()
		}
		return nil, err
	}
	handler := httpserver.NewHandler(cfg, db, api.New(deps), httpserver.WithMetrics(registry))
	app := &App{
		cfg:           cfg,
		streams:       streams,
		cancelStreams: cancelStreams,
		log:           log,
		db:            db,
		handler:       handler,
		// buildAPIDeps always returns the concrete *executor.Runner; the
		// assertion keeps a test fake injectable through api.Deps.Runner.
		runner: runnerOf(deps.Runner),
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
	deps := api.Deps{Log: log, ConsoleURL: cfg.ConsoleURL, SecureCookies: cfg.CookieSecure}
	if db == nil {
		switch {
		case cfg.APIToken != "":
			deps.Auth = api.NewTokenAuthenticator(cfg.APIToken, localOperator)
		case cfg.Env == config.EnvDevelopment:
			log.Warn("API authentication disabled: development mode without AXIOM_API_TOKEN")
			deps.Auth = api.DevAuthenticator{Principal: localOperator}
		default:
			log.Warn("no API authenticator configured; all /api/v1 requests will be rejected")
		}
		return deps, nil
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO users (id, display_name) VALUES ($1, $2) ON CONFLICT (id) DO NOTHING`,
		localOperator.UserID, localOperator.Name); err != nil {
		return api.Deps{}, fmt.Errorf("ensure local operator: %w", err)
	}
	// User authentication & sessions (#125). The static AXIOM_API_TOKEN, when
	// configured, remains a documented machine-access escape hatch.
	authSvc := auth.NewService(auth.NewPGStore(db), auth.WithSessionTTL(cfg.SessionTTL))
	deps.Sessions = authSvc
	// Authorization (#127): V0.1 single-user ownership, no organizations.
	deps.Authz = authz.NewResolver(authz.NoRoles{})
	switch {
	case cfg.APIToken != "":
		deps.Auth = api.NewSessionAuthenticator(authSvc, cfg.APIToken, localOperator)
	case cfg.Env == config.EnvDevelopment:
		log.Warn("API authentication disabled: development mode without AXIOM_API_TOKEN")
		deps.Auth = api.DevAuthenticator{Principal: localOperator}
	default:
		deps.Auth = api.NewSessionAuthenticator(authSvc, "", localOperator)
	}
	deps.Deployments = deployment.NewService(deploymentdb.New(db), deployment.NewEventBus())
	deps.EventStream = &sse.Handler{Store: deps.Deployments.Store(), Bus: deps.Deployments.Events(), Log: log, Shutdown: streams}
	deps.Applications = database.NewApplicationStore(db)
	deps.Servers = server.NewService(database.NewRepositories(db).Servers)
	deps.Agents = agentauth.NewService(
		agentauth.NewPGStore(db),
		agentauth.WithServerLookup(agentauth.ServerLookupFunc(func(ctx context.Context, serverID string) (string, error) {
			rec, err := deps.Servers.Get(ctx, serverID)
			if errors.Is(err, server.ErrNotFound) {
				return "", agentauth.ErrServerNotFound
			}
			if err != nil {
				return "", err
			}
			return string(rec.Status), nil
		})),
	)
	if cfg.SecretKey != "" {
		key, err := secrets.ParseKey(cfg.SecretKey)
		if err != nil {
			return api.Deps{}, err
		}
		box, err := secrets.NewBox(key)
		if err != nil {
			return api.Deps{}, err
		}
		deps.AppConfig = &appconfig.Service{Store: &secrets.EncryptedStore{DB: db, Box: box}}
	}
	domainService := &domains.Service{Store: domains.PGStore{DB: db}, Resolver: stdResolver{}}
	deps.Domains = domainService
	// The build stage writes through the same durable log store the API reads
	// (#66), so the concrete store is kept alongside the narrow API view.
	logStore := logs.NewPGStore(db, 0)
	deps.Logs = logStore
	// Audit trail (#128): privileged operation events, persisted redacted.
	deps.Audit = audit.NewService(audit.NewPGStore(db))
	deps.Plans = &planner.Service{Engine: planner.New(), Profiles: analysis.PGStore{DB: db}, Servers: deps.Servers, Domains: domainService, DB: db, NewID: deployment.NewID}
	if cfg.GitHub.Enabled() {
		key, err := secrets.ParseKey(cfg.SecretKey)
		if err != nil {
			return api.Deps{}, err
		}
		box, err := secrets.NewBox(key)
		if err != nil {
			return api.Deps{}, err
		}
		ghService := &ghauth.Service{
			Store: ghauth.PGStore{DB: db},
			Provider: &ghauth.OAuthProvider{
				ClientID: cfg.GitHub.ClientID, ClientSecret: cfg.GitHub.ClientSecret, RedirectURL: cfg.GitHub.RedirectURL,
				OAuthURL: cfg.GitHub.OAuthURL, APIURL: cfg.GitHub.APIURL, Scopes: cfg.GitHub.Scopes,
			},
			Box: box, Log: log,
		}
		deps.GitHub = ghService
		repoService := &repos.Service{Tokens: ghService, Store: repos.PGStore{DB: db}, APIURL: cfg.GitHub.APIURL}
		deps.Repositories = repoService
		deps.Analyses = &analysis.Service{Store: analysis.PGStore{DB: db}, Repos: repoService, Log: log, NewID: deployment.NewID}
		// The build step fetches the archive of the exact commit the plan
		// pinned (#98); the same service serves analysis.
		deps.Sources = repoService
	} else {
		log.Info("GitHub integration disabled: AXIOM_GITHUB_CLIENT_ID not configured")
	}
	// Deployment execution (#100). Before this, createDeployment persisted a
	// PENDING record and nothing ever ran: no image was built and no SSE
	// event was produced. The runner starts in the background so the API
	// still answers 202 immediately.
	deps.Runner = newRunner(cfg, log, deps, logStore)
	return deps, nil
}

// newRunner composes the deployment execution pipeline: workspace → source →
// Dockerfile → image (build), the bounded agent transport (agentclient), and
// the background runner that owns at most one execution per deployment.
//
// It returns nil when no database is present (there is nothing to execute
// against), which leaves the API in its previous persist-only behavior.
func newRunner(cfg config.Config, log *slog.Logger, deps api.Deps, logStore logs.Appender) *executor.Runner {
	buildEngine := &build.Engine{
		Workspaces: &workspace.Manager{Root: filepath.Join(os.TempDir(), "axiom-build"), Limits: workspace.DefaultLimits},
		Builder:    &build.ExecBuilder{Docker: cfg.Docker.Binary},
		Log:        buildEvents{log: log},
		LogStore:   logStore,
	}
	agent := &agentclient.Client{
		Servers:     deps.Servers,
		Credentials: unavailableCredential{},
		Production:  cfg.Env == config.EnvProduction,
		Log:         log,
	}
	planExecutor := executor.New(deps.Deployments, buildEngine, agent)
	planExecutor.Log = log
	// Re-verify server eligibility right before dispatching: a server that
	// went offline between plan review and execution must not receive work.
	planExecutor.Servers = deps.Servers
	return executor.NewRunner(planExecutor)
}

// buildEvents forwards build lifecycle events to the structured log (#101).
// The durable record is the log store; this is the operational trace.
type buildEvents struct{ log *slog.Logger }

func (b buildEvents) Log(_ context.Context, ev build.LogEvent) {
	l := b.log
	if l == nil {
		l = slog.Default()
	}
	l.Info("build event", "deploymentId", ev.DeploymentID, "level", ev.Level, "step", ev.Step, "message", ev.Message)
}

// unavailableCredential refuses to produce an Engine→Agent credential.
//
// TODO(#77, ADR-0008): the Engine→Agent credential direction does not exist.
// agentauth (#77) owns the agent→Engine direction and stores credential
// hashes only, so the Engine has no plaintext it could present to an agent;
// ADR-0008 still lists "mTLS vs token-based agent credentials" as an open
// question and the agent exposes no inbound transport (the #75 follow-up).
//
// Refusing is the fail-closed choice: a deployment reaching CREATE_RUNTIME
// fails with the stable code AGENT_NO_CREDENTIAL and the executor records
// RUNTIME_FAILED, instead of dispatching an unauthenticated operation to a
// server. The build stage, which needs no agent, still runs for real.
type unavailableCredential struct{}

func (unavailableCredential) Credential(context.Context, string) (agentclient.Credential, error) {
	return agentclient.Credential{}, agentclient.ErrNoCredential
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

// Runner exposes the deployment execution runner (tests); nil when the Engine
// runs without a database.
func (a *App) Runner() *executor.Runner { return a.runner }

// runnerOf narrows the injected runner so the composition root can drain
// executions at shutdown.
func runnerOf(r api.DeploymentRunner) *executor.Runner {
	if r == nil {
		return nil
	}
	runner, _ := r.(*executor.Runner)
	return runner
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
	if a.runner != nil {
		// Bound the drain by the shutdown timeout so a slow build cannot hold
		// the process open; the deployment stays queryable in its last state.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), a.cfg.ShutdownTimeout)
		if err := a.runner.Shutdown(shutdownCtx); err != nil {
			a.log.Warn("deployment executions did not drain before shutdown", "error", err.Error())
		}
		cancel()
		a.runner = nil
	}
	if a.db != nil {
		if err := a.db.Close(); err != nil {
			a.log.Error("closing database", "error", err)
		}
		a.db = nil
	}
}

// stdResolver resolves DNS through the system resolver.
type stdResolver struct{}

func (stdResolver) LookupIP(ctx context.Context, host string) ([]string, error) {
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	out := make([]string, len(ips))
	for i, ip := range ips {
		out[i] = ip.String()
	}
	return out, nil
}
