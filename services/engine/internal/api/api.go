// Package api implements the public REST contract /api/v1
// (docs/architecture/api-contract.md). It exposes resources, never
// infrastructure internals, and renders every error with the stable envelope.
package api

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/digitaleflex/axiom/services/engine/internal/application"
	"github.com/digitaleflex/axiom/services/engine/internal/deployment"
	"github.com/digitaleflex/axiom/services/engine/internal/server"
)

// Servers manages server records: registration, reads, rename and removal.
type Servers interface {
	Register(ctx context.Context, ownerID, name, address string) (server.Record, error)
	Get(ctx context.Context, id string) (server.Record, error)
	Rename(ctx context.Context, id, name string) (server.Record, error)
	Remove(ctx context.Context, id string) error
	ListFiltered(ctx context.Context, status string, limit, offset int) ([]server.Record, int, error)
}

// ServerStore is the read model for servers.
type ServerStore = Servers

// Deps are the API dependencies, injected by the composition root.
// A nil dependency makes the corresponding endpoints answer 503.
type Deps struct {
	Log          *slog.Logger
	Auth         Authenticator
	Deployments  *deployment.Service
	Applications application.Store
	Servers      ServerStore
	EventStream  http.Handler // GET /deployments/{id}/events/stream (SSE, #118)
	GitHub       GitHubConnections
	Repositories RepositoryDiscovery
	Analyses     Analyses
	Plans        Plans
	// ConsoleURL is where the GitHub callback redirects the browser.
	ConsoleURL string
	// SecureCookies sets the Secure attribute on cookies (production).
	SecureCookies bool
}

// API serves /api/v1.
type API struct {
	log          *slog.Logger
	auth         Authenticator
	deployments  *deployment.Service
	applications application.Store
	servers      ServerStore
	github       GitHubConnections
	repos        RepositoryDiscovery
	analyses     Analyses
	plans        Plans
	consoleURL   string
	secure       bool
	mux          *http.ServeMux
}

// New builds the API handler with its middleware chain:
// request ID → recover → access log → authentication → routes.
func New(d Deps) http.Handler {
	a := &API{
		log: d.Log, auth: d.Auth, deployments: d.Deployments,
		applications: d.Applications, servers: d.Servers, github: d.GitHub, repos: d.Repositories, analyses: d.Analyses, plans: d.Plans,
		consoleURL: d.ConsoleURL, secure: d.SecureCookies, mux: http.NewServeMux(),
	}
	if a.log == nil {
		a.log = slog.Default()
	}
	if a.auth == nil {
		a.auth = denyAll{}
	}

	r := a.mux
	r.HandleFunc("GET /api/v1/auth/me", a.wrap(a.me))
	r.HandleFunc("POST /api/v1/auth/logout", a.wrap(a.logout))

	r.HandleFunc("POST /api/v1/github/connections", a.wrap(a.startGitHubConnection))
	r.HandleFunc("GET /api/v1/github/connections", a.wrap(a.listGitHubConnections))
	r.HandleFunc("DELETE /api/v1/github/connections/{connectionID}", a.wrap(a.disconnectGitHub))
	r.HandleFunc("GET /api/v1/github/connections/{connectionID}/repositories", a.wrap(a.listRepositories))
	r.HandleFunc("GET /api/v1/repositories/{repositoryID}", a.wrap(a.getRepository))
	r.HandleFunc("GET /api/v1/repositories/{repositoryID}/refs", a.wrap(a.listRefs))
	r.HandleFunc("GET "+githubCallbackPath, a.githubCallback) // public: protected by single-use state + browser cookie

	r.HandleFunc("GET /api/v1/applications", a.wrap(a.listApplications))
	r.HandleFunc("POST /api/v1/applications", a.wrap(a.createApplication))
	r.HandleFunc("GET /api/v1/applications/{applicationID}", a.wrap(a.getApplication))

	r.HandleFunc("POST /api/v1/applications/{applicationID}/analysis", a.wrap(a.startAnalysis))
	r.HandleFunc("GET /api/v1/applications/{applicationID}/analysis/{analysisID}", a.wrap(a.getAnalysis))
	r.HandleFunc("GET /api/v1/applications/{applicationID}/profile", a.wrap(a.getProfile))
	r.HandleFunc("PUT /api/v1/applications/{applicationID}/profile/overrides", a.wrap(a.putOverrides))

	r.HandleFunc("POST /api/v1/applications/{applicationID}/deployment-plans", a.wrap(a.createPlan))
	r.HandleFunc("GET /api/v1/deployment-plans/{planID}", a.wrap(a.getPlan))

	r.HandleFunc("POST /api/v1/servers", a.wrap(a.registerServer))
	r.HandleFunc("GET /api/v1/servers", a.wrap(a.listServers))
	r.HandleFunc("GET /api/v1/servers/{serverID}", a.wrap(a.getServer))
	r.HandleFunc("PATCH /api/v1/servers/{serverID}", a.wrap(a.renameServer))
	r.HandleFunc("DELETE /api/v1/servers/{serverID}", a.wrap(a.removeServer))
	r.HandleFunc("GET /api/v1/servers/{serverID}/health", a.wrap(a.serverHealth))

	r.HandleFunc("POST /api/v1/applications/{applicationID}/deployments", a.wrap(a.createDeployment))
	r.HandleFunc("GET /api/v1/applications/{applicationID}/deployments", a.wrap(a.listDeployments))
	r.HandleFunc("GET /api/v1/deployments/{deploymentID}", a.wrap(a.getDeployment))
	r.HandleFunc("POST /api/v1/deployments/{deploymentID}/cancel", a.wrap(a.cancelDeployment))
	r.HandleFunc("GET /api/v1/deployments/{deploymentID}/steps", a.wrap(a.deploymentSteps))
	r.HandleFunc("GET /api/v1/deployments/{deploymentID}/events", a.wrap(a.deploymentEvents))
	r.HandleFunc("GET /api/v1/deployments/{deploymentID}/logs", a.wrap(a.deploymentLogs))
	r.HandleFunc("GET /api/v1/deployments/{deploymentID}/health", a.wrap(a.deploymentHealth))
	if d.EventStream != nil {
		r.Handle("GET /api/v1/deployments/{deploymentID}/events/stream", a.ownedDeployment(d.EventStream))
	} else {
		r.HandleFunc("GET /api/v1/deployments/{deploymentID}/events/stream", a.wrap(func(http.ResponseWriter, *http.Request) error { return errUnavailable }))
	}

	// Unknown /api/v1 paths use the error envelope instead of plain-text 404s.
	r.HandleFunc("/api/v1/", a.wrap(func(_ http.ResponseWriter, r *http.Request) error {
		return newError(http.StatusNotFound, CodeNotFound, "no such endpoint", map[string]any{"path": r.URL.Path})
	}))

	var h http.Handler = r
	h = a.authenticate(h)
	h = a.accessLog(h)
	h = a.recoverer(h)
	h = requestIDMiddleware(h)
	return h
}
