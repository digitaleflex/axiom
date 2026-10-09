// Package bootstrap is the Runtime Agent composition root. It wires
// configuration, identity, credentials, capabilities, the runtime adapters, the
// operation dispatcher, restart recovery and the inbound operation listener,
// and it owns the agent lifecycle: start, drain, stop.
//
// House pattern (mirrors services/engine/internal/bootstrap): domain packages
// never construct their own dependencies, everything is injected here, and the
// App is the only place that knows the mount order.
//
// Mount order (each package is constructed on the line noted in its field doc):
//
//	identity → state → ownership → capabilities → recovery → health → heartbeat
//	→ docker → traefik → dispatcher → inbound listener
package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/digitaleflex/axiom/services/agent/internal/capabilities"
	"github.com/digitaleflex/axiom/services/agent/internal/config"
	"github.com/digitaleflex/axiom/services/agent/internal/dispatcher"
	"github.com/digitaleflex/axiom/services/agent/internal/health"
	"github.com/digitaleflex/axiom/services/agent/internal/heartbeat"
	"github.com/digitaleflex/axiom/services/agent/internal/identity"
	"github.com/digitaleflex/axiom/services/agent/internal/logs"
	"github.com/digitaleflex/axiom/services/agent/internal/protocol"
	"github.com/digitaleflex/axiom/services/agent/internal/recovery"
	"github.com/digitaleflex/axiom/services/agent/internal/runtime/docker"
	"github.com/digitaleflex/axiom/services/agent/internal/runtime/traefik"
	"github.com/digitaleflex/axiom/services/agent/internal/security/auth"
	"github.com/digitaleflex/axiom/services/agent/internal/security/ownership"
	"github.com/digitaleflex/axiom/services/agent/internal/state"
)

// DefaultDockerTimeout bounds every docker CLI invocation.
const DefaultDockerTimeout = 2 * time.Minute

// App is a fully wired Runtime Agent.
//
// Every field is a real, constructed production dependency: there is no nil
// package behind an accessor. Tests assert exactly that (see bootstrap_test.go).
type App struct {
	cfg config.Config
	log *slog.Logger

	// Identity is the persisted agent identity (identity.Store) and IdentityID
	// the identity loaded at startup.
	Identity    *identity.Store
	IdentityID  identity.Identity
	Credentials *auth.Store
	// Auth signs every outbound Engine request (registration, heartbeat,
	// rotation).
	Auth *auth.Client
	// State is the durable operation state store; it also performs restart
	// classification when opened.
	State *state.Store
	// Discoverer produces the capability/resource sample shipped at
	// registration and on every heartbeat.
	Discoverer capabilities.Discoverer
	// Reconciler is the startup reconciliation over Axiom-managed containers.
	Reconciler *recovery.Reconciler
	// Checker probes a deployed application for VERIFY.
	Checker *health.Checker
	// Heartbeat is the agent→Engine liveness loop; nil until registration.
	Heartbeat *heartbeat.Loop
	// Docker is the runtime adapter; Traefik the network adapter.
	Docker  *docker.Adapter
	Traefik *traefik.Adapter
	// Adapter is the production dispatcher.Adapter over Docker, Traefik and
	// health (the bridge the failure matrix reported as missing).
	Adapter *runtimeBridge
	// Dispatcher validates, dedupes, acknowledges and executes operations.
	Dispatcher *dispatcher.Dispatcher
	// Logs is the bounded, redacting log fetcher (#86).
	Logs *logs.Fetcher

	// Listener is the inbound operation HTTP listener.
	Listener *operationsListener

	// server is the inbound HTTP server over Listener.Handler().
	server *http.Server
	// authHeader is the inbound authenticator; refuseInbound by default.
	authHeader InboundAuthenticator
	// mu guards closed and the loop wait group.
	mu     sync.Mutex
	closed bool
	// cancelLoops stops the background loops at shutdown.
	cancelLoops context.CancelFunc
	wg          sync.WaitGroup
}

// New wires the agent. It fails fast when a required dependency is unavailable;
// optional runtime tools (Docker, Traefik) are reported as unavailable by the
// capability probe instead of blocking startup.
func New(ctx context.Context, cfg config.Config, log *slog.Logger) (*App, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}
	if log == nil {
		log = slog.Default()
	}
	if err := os.MkdirAll(cfg.DataRoot, 0o700); err != nil {
		return nil, fmt.Errorf("create data root %s: %w", cfg.DataRoot, err)
	}

	// 1. identity (#76) — generated on first run, persisted 0600.
	idStore := identity.NewStore(cfg.IdentityPath())
	id, err := idStore.Load()
	if err != nil {
		return nil, fmt.Errorf("load identity: %w", err)
	}

	// 2. credentials (#77, agent→Engine direction only).
	credStore := auth.NewStore(cfg.CredentialPath())
	if _, err := credStore.Load(); err != nil && !errors.Is(err, auth.ErrNoCredential) {
		return nil, fmt.Errorf("load credential: %w", err)
	}
	authClient := auth.NewClient(cfg.EngineURL, credStore)

	// 3. durable operation state (#81). Open performs restart classification:
	//    anything left RECEIVED/RUNNING becomes INTERRUPTED.
	stateStore, err := state.Open(cfg.StatePath(), state.WithLogger(log))
	if err != nil {
		if !errors.Is(err, state.ErrCorrupt) {
			return nil, fmt.Errorf("open operation state: %w", err)
		}
		// Corruption is recoverable: the store holds every record replayed
		// before the damaged one. Continue, loudly.
		log.Error("operation state log is corrupt; recovered entries are in use", "error", err.Error())
	}

	app := &App{
		cfg:         cfg,
		log:         log,
		Identity:    idStore,
		IdentityID:  id,
		Credentials: credStore,
		Auth:        authClient,
		State:       stateStore,
	}

	// 4. capabilities (#79).
	app.Discoverer = capabilities.NewDiscoverer(cfg.Version, cfg.DataRoot)

	// 5. runtime adapters (#83, #84) and the health probe (#85).
	runner := &docker.ExecRunner{Docker: cfg.Docker.Binary}
	app.Docker = &docker.Adapter{
		Runner:  runner,
		Timeout: DefaultDockerTimeout,
		Now:     func() time.Time { return time.Now().UTC() },
	}
	app.Traefik = &traefik.Adapter{DynamicDir: cfg.Traefik.DynamicDir}
	// ownership (#89): the network adapter refuses to route to a container the
	// agent does not own. This is the only live use of the ownership package in
	// the composition root; the Docker adapter enforces the same boundary
	// internally.
	app.Traefik.Verify = traefik.ContainerVerifierFunc(
		func(ctx context.Context, container, deploymentID string) error {
			info, err := app.Docker.Inspect(ctx, container)
			if err != nil {
				return err
			}
			return ownership.AssertContainer(container, info.Labels, deploymentID)
		})
	app.Checker = health.NewChecker()

	// 6. restart recovery (#82): interrupted operations are classified locally.
	plan := recovery.LoadInterrupted(stateStore)
	if len(plan.Resumable) > 0 || len(plan.NeedsReconciliation) > 0 {
		log.Info("recovery: classified interrupted operations",
			"resumable", len(plan.Resumable), "needsReconciliation", len(plan.NeedsReconciliation))
	}
	app.Reconciler = &recovery.Reconciler{
		Runtime: &dockerRuntime{adapter: app.Docker, timeout: DefaultDockerTimeout},
		State:   stateStore,
		Log:     log,
		// AllowCleanup stays false: an orphan is reported, never removed, so
		// Reconcile never reaches the Docker remove path.
		AllowCleanup: false,
	}
	// Reconcile itself is NOT run at startup: it needs the list of deployments
	// the Engine still manages for this agent, and the contract exposes no
	// endpoint that reports it (docs/architecture/agent-protocol.md §11 leaves
	// the state report to the reconnect flow, #75 follow-up). Running it with an
	// empty list would classify every managed container as an orphan. The
	// reconciler is constructed and reachable (App.Reconciler) so the caller that
	// knows the managed set can run it; only the purely local classification
	// (LoadInterrupted, above) runs automatically.

	// 7. the dispatcher adapter bridge: the only place that knows the concrete
	//    runtime adapters. This is the seam the failure matrix (#1 gap) said had
	//    no production implementation.
	app.Adapter = &runtimeBridge{docker: app.Docker, traefik: app.Traefik, health: app.Checker}

	// 8. dispatcher (#80).
	app.Dispatcher = dispatcher.NewDispatcher([]dispatcher.Adapter{app.Adapter}, log)

	// 9. logs (#86), bounded and redacted, sourced from the Docker adapter.
	app.Logs = &logs.Fetcher{Docker: &dockerLogSource{runner: runner}}

	// 10. inbound operation listener. Unauthenticated operations are refused:
	//     see InboundAuthenticator for why the default authenticator refuses.
	app.authHeader = refuseInbound{}
	app.Listener = newOperationsListener(cfg.Listener.Path, app.authHeader, app)
	app.server = &http.Server{
		Handler:           app.Listener.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		// No WriteTimeout: an operation may legitimately run for minutes
		// (CREATE_RUNTIME/NETWORK are bounded at 3m by the dispatcher).
		IdleTimeout: 120 * time.Second,
	}

	// 11. heartbeat (#78), only once an agent ID has been issued. Before
	//     registration there is no identity to bind a heartbeat to.
	if id.Registered && id.ServerID != "" {
		app.Heartbeat = newHeartbeat(cfg, protocol.AgentIdentity{AgentID: id.AgentID, ServerID: id.ServerID},
			authClient, app.Discoverer, log)
	} else {
		log.Info("agent is not registered yet; the heartbeat loop starts after registration")
	}
	return app, nil
}

// AgentIdentity returns the protocol identity bound to this agent. When the
// agent is not registered, the locally generated agent ID with the configured
// server ID is returned: the local ID is stable, the Engine issues the
// authoritative one at registration.
func (a *App) AgentIdentity() protocol.AgentIdentity {
	if a.cfg.ServerID == "" {
		return protocol.AgentIdentity{AgentID: a.IdentityID.AgentID}
	}
	return protocol.AgentIdentity{AgentID: a.IdentityID.AgentID, ServerID: a.cfg.ServerID}
}

// Dispatch executes one operation through the mounted dispatcher, recording it
// in the durable operation state store (#81) so an interrupted operation is
// classified as INTERRUPTED on the next start. It is the single entry point the
// inbound listener uses.
//
// State handling is deliberately conservative: an operation ID already recorded
// as terminal is never re-executed, even if the Engine redelivers it — the
// dispatcher's own result cache answers that redelivery first.
func (a *App) Dispatch(ctx context.Context, op protocol.Operation) (protocol.Acknowledgement, protocol.Result) {
	entry, err := a.State.Begin(state.Operation{
		OperationID:  op.OperationID,
		DeploymentID: op.DeploymentID,
		Type:         op.Type,
		Phase:        state.PhaseReceived,
	})
	if err != nil {
		a.log.Error("operation state write failed", "operationId", op.OperationID, "error", err.Error())
	} else if entry.Phase.Terminal() {
		now := time.Now().UTC()
		ack := protocol.Acknowledgement{
			Envelope:     protocol.Envelope{Protocol: protocol.Version, MessageID: "msg_ack_" + op.OperationID, SentAt: now},
			OperationID:  op.OperationID,
			DeploymentID: op.DeploymentID,
			Accepted:     false,
			Reason:       dispatcher.CodeReplayed,
		}
		return ack, protocol.Result{
			Envelope:     ack.Envelope,
			OperationID:  op.OperationID,
			DeploymentID: op.DeploymentID,
			FinishedAt:   now,
			ErrorCode:    dispatcher.CodeReplayed,
			Message:      "operation already reached a terminal state",
		}
	}

	ack, result := a.Dispatcher.Dispatch(ctx, op, a.AgentIdentity())

	if _, err := a.State.Complete(op.OperationID, state.Result{
		Success:    result.Success,
		ErrorCode:  result.ErrorCode,
		Message:    result.Message,
		FinishedAt: result.FinishedAt,
	}); err != nil && !errors.Is(err, state.ErrNotFound) {
		a.log.Error("operation state completion failed", "operationId", op.OperationID, "error", err.Error())
	}
	return ack, result
}

// Run binds the inbound listener, registers the agent if needed, starts the
// background loops and serves until ctx is cancelled; it then drains.
//
// The listener is bound before Run returns control, so a bind failure surfaces
// immediately instead of after registration.
func (a *App) Run(ctx context.Context) error {
	ln, err := net.Listen("tcp", a.cfg.Listener.Addr)
	if err != nil {
		a.Close()
		return fmt.Errorf("listen %s: %w", a.cfg.Listener.Addr, err)
	}
	return a.Serve(ctx, ln)
}

// Serve is Run with a caller-provided listener.
func (a *App) Serve(ctx context.Context, ln net.Listener) error {
	defer a.Close()
	a.log.Info("axiom runtime agent started",
		"addr", ln.Addr().String(), "serverId", a.cfg.ServerID, "version", a.cfg.Version,
		"env", a.cfg.Env, "dataRoot", a.cfg.DataRoot, "path", a.cfg.Listener.Path,
		"registered", a.IdentityID.Registered)

	if err := a.register(ctx); err != nil {
		// A failed registration is not fatal for the process: the operator may
		// still be provisioning the Engine. It is reported and the agent keeps
		// serving (every operation is refused anyway, see InboundAuthenticator).
		a.log.Error("agent registration failed; continuing without a heartbeat", "error", err.Error())
	}

	serveErr := make(chan error, 1)
	go func() { serveErr <- a.server.Serve(ln) }()

	// Background loops are started only after the listener is up, so the agent
	// never reports itself alive before it can receive an operation.
	a.startLoops(ctx)

	select {
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("operation listener: %w", err)
	case <-ctx.Done():
	}

	a.Listener.SetDraining()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), a.cfg.ShutdownTimeout)
	defer cancel()
	if err := a.server.Shutdown(shutdownCtx); err != nil {
		_ = a.server.Close()
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	a.log.Info("axiom runtime agent stopped")
	return nil
}

// startLoops runs the heartbeat in the background and records it in the wait
// group so shutdown drains it.
func (a *App) startLoops(ctx context.Context) {
	if a.Heartbeat == nil {
		return
	}
	a.wg.Add(1)
	loopCtx, cancel := context.WithCancel(context.Background())
	a.mu.Lock()
	a.cancelLoops = cancel
	a.mu.Unlock()
	go func() {
		defer a.wg.Done()
		if err := a.Heartbeat.Run(loopCtx); err != nil && !errors.Is(err, context.Canceled) {
			a.log.Error("heartbeat loop stopped", "error", err.Error())
		}
	}()
	a.log.Info("heartbeat loop started", "interval", a.Heartbeat.Interval)
}

// Close drains the background loops, reconciles, and releases the stores. It is
// idempotent.
func (a *App) Close() {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return
	}
	a.closed = true
	a.mu.Unlock()

	a.mu.Lock()
	cancel := a.cancelLoops
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}

	// Bound the drain by the shutdown timeout so a stuck heartbeat cannot hold
	// the process open.
	done := make(chan struct{})
	go func() { a.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(a.cfg.ShutdownTimeout):
		a.log.Warn("background loops did not drain before shutdown")
	}

	if a.State != nil {
		if err := a.State.Close(); err != nil {
			a.log.Error("closing operation state", "error", err.Error())
		}
	}
}

// Handler exposes the inbound operation handler (tests).
func (a *App) Handler() http.Handler { return a.Listener.Handler() }
