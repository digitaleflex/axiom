// Package api implements the public REST contract /api/v1
// (docs/architecture/api-contract.md). It exposes resources, never
// infrastructure internals, and renders every error with the stable envelope.
package api

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/digitaleflex/axiom/services/engine/internal/agentauth"
	"github.com/digitaleflex/axiom/services/engine/internal/agentpoll"
	"github.com/digitaleflex/axiom/services/engine/internal/application"
	"github.com/digitaleflex/axiom/services/engine/internal/audit"
	"github.com/digitaleflex/axiom/services/engine/internal/auth"
	"github.com/digitaleflex/axiom/services/engine/internal/authz"
	"github.com/digitaleflex/axiom/services/engine/internal/deployment"
	"github.com/digitaleflex/axiom/services/engine/internal/diagnostics"
	"github.com/digitaleflex/axiom/services/engine/internal/server"
)

// Servers manages server records: registration, reads, rename and removal.
type Servers interface {
	Register(ctx context.Context, ownerID, name, address string) (server.Record, error)
	Get(ctx context.Context, id string) (server.Record, error)
	Rename(ctx context.Context, id, name string) (server.Record, error)
	Remove(ctx context.Context, id string) error
	// ListFiltered lists the servers owned by ownerID (strict ownership
	// scope), optionally filtered by status. The API only ever lists the
	// caller's own servers: cross-owner enumeration is not exposed.
	ListFiltered(ctx context.Context, ownerID, status string, limit, offset int) ([]server.Record, int, error)
	// UpdateHealth persists agent heartbeat liveness (#78).
	UpdateHealth(ctx context.Context, id string, health server.Health) error
}

// ServerStore is the read model for servers.
type ServerStore = Servers

// AuditService records privileged operation events (#128). It is satisfied
// by *audit.Service and by test fakes.
type AuditService interface {
	Record(ctx context.Context, e audit.Event) error
	List(ctx context.Context, f audit.Filter) ([]audit.Event, error)
}

// Deps are the API dependencies, injected by the composition root.
// A nil dependency makes the corresponding endpoints answer 503.
type Deps struct {
	Log  *slog.Logger
	Auth Authenticator
	// Sessions is the user authentication & session service (#125). A nil
	// service makes the auth endpoints answer 503.
	Sessions     *auth.Service
	Deployments  *deployment.Service
	Applications application.Store
	Servers      ServerStore
	EventStream  http.Handler // GET /deployments/{id}/events/stream (SSE, #118)
	GitHub       GitHubConnections
	Repositories RepositoryDiscovery
	Analyses     Analyses
	Plans        Plans
	Domains      Domains
	Logs         LogStore
	AppConfig    AppConfig
	// Agents owns agent registration and credential lifecycle (#76/#77).
	Agents *agentauth.Service
	// AgentKeys issues the per-agent operation signing key (ADR-0008). The
	// plaintext key is returned to the agent exactly once, at registration and
	// at every rotation. A nil service makes the agent registration endpoints
	// answer 503: an agent registered without a key could never accept a
	// dispatched operation.
	AgentKeys AgentKeys
	AgentPoll *agentpoll.Manager
	// Authz is the authorization boundary (#127). A nil resolver denies
	// every non-owner action (fail closed).
	Authz *authz.Resolver
	// Audit records privileged operation events (#128). A nil service
	// falls back to structured log lines only.
	Audit AuditService
	// Runner starts the background execution of a deployment (#100). A nil
	// runner makes POST /applications/{id}/deployments persist the record and
	// leave it PENDING, which is only correct for a deliberately
	// execution-less Engine (tests, read-only tooling).
	Runner DeploymentRunner
	// Sources streams the archive of the commit a deployment builds (#98).
	// Without it a triggered execution fails at the build step.
	Sources SourceFetcher
	// Diagnostics serves the read-only deployment diagnostic (#102): the
	// steps, durations, error codes, target server state, last health probe
	// and the conclusion an operator can draw from a failure. A nil service
	// makes the diagnostic endpoints answer 503; it never changes behaviour
	// of any other endpoint.
	Diagnostics *diagnostics.Service
	// ConsoleURL is where the GitHub callback redirects the browser.
	ConsoleURL string
	// SecureCookies sets the Secure attribute on cookies (production).
	SecureCookies bool
}

// API serves /api/v1.
type API struct {
	log              *slog.Logger
	auth             Authenticator
	authSvc          *auth.Service
	authz            *authz.Resolver
	auditSvc         AuditService
	deployments      *deployment.Service
	applications     application.Store
	servers          ServerStore
	github           GitHubConnections
	repos            RepositoryDiscovery
	analyses         Analyses
	plans            Plans
	domains          Domains
	logs             LogStore
	appConfig        AppConfig
	agents           *agentauth.Service
	agentKeys        AgentKeys
	agentPollManager *agentpoll.Manager
	runner           DeploymentRunner
	sources          SourceFetcher
	diagnostics      *diagnostics.Service
	consoleURL       string
	secure           bool
	mux              *http.ServeMux
}

// New builds the API handler with its middleware chain:
// request ID → recover → access log → authentication → routes.
func New(d Deps) http.Handler {
	a := &API{
		log: d.Log, auth: d.Auth, authSvc: d.Sessions, authz: d.Authz, auditSvc: d.Audit, deployments: d.Deployments,
		applications: d.Applications, servers: d.Servers, github: d.GitHub, repos: d.Repositories, analyses: d.Analyses, plans: d.Plans, domains: d.Domains, logs: d.Logs, appConfig: d.AppConfig,
		agents:           d.Agents,
		agentKeys:        d.AgentKeys,
		agentPollManager: d.AgentPoll,
		runner:           d.Runner,
		sources:          d.Sources,
		diagnostics:      d.Diagnostics,
		consoleURL:       d.ConsoleURL, secure: d.SecureCookies, mux: http.NewServeMux(),
	}
	if a.log == nil {
		a.log = slog.Default()
	}
	if a.auth == nil {
		a.auth = denyAll{}
	}
	if a.authz == nil {
		a.authz = authz.NewResolver(nil) // V0.1: single-user ownership
	}
	// Diagnostics (#102) is composed from dependencies the API already
	// holds, so it is never left unwired while a deployment store exists.
	// Each section degrades on its own: a nil logs or servers store makes
	// that section unavailable, not the whole report.
	if a.diagnostics == nil && a.deployments != nil {
		var logReader diagnostics.LogReader
		if a.logs != nil {
			logReader = a.logs
		}
		a.diagnostics = diagnostics.New(a.deployments.Store(), a.servers, logReader, a.probeLookupFor())
	}

	r := a.mux
	r.HandleFunc("POST /api/v1/auth/register", a.wrap(a.register))
	r.HandleFunc("POST /api/v1/auth/login", a.wrap(a.login))
	r.HandleFunc("GET /api/v1/auth/me", a.wrap(a.me))
	r.HandleFunc("POST /api/v1/auth/logout", a.wrap(a.logout))
	r.HandleFunc("GET /api/v1/auth/sessions", a.wrap(a.listSessions))
	r.HandleFunc("DELETE /api/v1/auth/sessions", a.wrap(a.revokeOtherSessions))
	r.HandleFunc("DELETE /api/v1/auth/sessions/{sessionID}", a.wrap(a.revokeSession))

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
	r.HandleFunc("GET /api/v1/applications/{applicationID}/configuration", a.wrap(a.listAppConfig))
	r.HandleFunc("PUT /api/v1/applications/{applicationID}/configuration/{name}", a.wrap(a.setAppConfig))
	r.HandleFunc("DELETE /api/v1/applications/{applicationID}/configuration/{name}", a.wrap(a.deleteAppConfig))

	r.HandleFunc("GET /api/v1/applications/{applicationID}/profile", a.wrap(a.getProfile))
	r.HandleFunc("PUT /api/v1/applications/{applicationID}/profile/overrides", a.wrap(a.putOverrides))

	r.HandleFunc("GET /api/v1/applications/{applicationID}/domains", a.wrap(a.listDomains))
	r.HandleFunc("POST /api/v1/applications/{applicationID}/domains", a.wrap(a.addDomain))
	r.HandleFunc("DELETE /api/v1/domains/{domainID}", a.wrap(a.removeDomain))
	r.HandleFunc("POST /api/v1/domains/{domainID}/primary", a.wrap(a.setPrimaryDomain))
	r.HandleFunc("POST /api/v1/domains/{domainID}/check", a.wrap(a.checkDomain))

	r.HandleFunc("POST /api/v1/applications/{applicationID}/deployment-plans", a.wrap(a.createPlan))
	r.HandleFunc("GET /api/v1/deployment-plans/{planID}", a.wrap(a.getPlan))

	r.HandleFunc("GET /api/v1/audit", a.wrap(a.listAuditEvents))

	r.HandleFunc("POST /api/v1/servers", a.wrap(a.registerServer))
	r.HandleFunc("GET /api/v1/servers", a.wrap(a.listServers))
	r.HandleFunc("GET /api/v1/servers/{serverID}", a.wrap(a.getServer))
	r.HandleFunc("PATCH /api/v1/servers/{serverID}", a.wrap(a.renameServer))
	r.HandleFunc("DELETE /api/v1/servers/{serverID}", a.wrap(a.removeServer))
	r.HandleFunc("GET /api/v1/servers/{serverID}/health", a.wrap(a.serverHealth))

	r.HandleFunc("POST /api/v1/servers/{serverID}/bootstrap", a.wrap(a.bootstrapServer))

	// Agent-facing endpoints authenticate with agent credentials in the handler
	// (#76/#77), not with the user authenticator.
	r.HandleFunc("POST /api/v1/agent/register", a.wrap(a.agentRegister))
	r.HandleFunc("POST /api/v1/agent/rotate", a.wrap(a.agentRotate))
	r.HandleFunc("POST /api/v1/agent/heartbeat", a.wrap(a.agentHeartbeat))
	r.HandleFunc("POST /api/v1/agent/poll", a.wrap(a.agentPoll))
	r.HandleFunc("POST /api/v1/agent/result", a.wrap(a.agentResult))
	r.HandleFunc("GET /api/v1/agent/status", a.wrap(a.agentStatus))

	r.HandleFunc("POST /api/v1/applications/{applicationID}/deployments", a.wrap(a.createDeployment))
	r.HandleFunc("GET /api/v1/applications/{applicationID}/deployments", a.wrap(a.listDeployments))
	r.HandleFunc("GET /api/v1/deployments/{deploymentID}", a.wrap(a.getDeployment))
	r.HandleFunc("POST /api/v1/deployments/{deploymentID}/cancel", a.wrap(a.cancelDeployment))
	r.HandleFunc("GET /api/v1/deployments/{deploymentID}/steps", a.wrap(a.deploymentSteps))
	r.HandleFunc("GET /api/v1/deployments/{deploymentID}/events", a.wrap(a.deploymentEvents))
	r.HandleFunc("GET /api/v1/deployments/{deploymentID}/logs", a.wrap(a.deploymentLogs))
	r.HandleFunc("GET /api/v1/deployments/{deploymentID}/health", a.wrap(a.deploymentHealth))
	// Read-only diagnostics (#102): why a deployment failed, without SSH.
	// One route per page; neither mutates any state.
	r.HandleFunc("GET /api/v1/deployments/{deploymentID}/diagnostics", a.wrap(a.deploymentDiagnostic))
	r.HandleFunc("GET /api/v1/deployments/{deploymentID}/diagnostics/logs", a.wrap(a.deploymentDiagnosticLogs))
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
	h = a.authenticateWithAgents(h)
	h = a.accessLog(h)
	h = a.recoverer(h)
	h = requestIDMiddleware(h)
	return h
}
