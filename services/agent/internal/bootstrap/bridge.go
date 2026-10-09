package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/digitaleflex/axiom/services/agent/internal/capabilities"
	"github.com/digitaleflex/axiom/services/agent/internal/config"
	"github.com/digitaleflex/axiom/services/agent/internal/dispatcher"
	"github.com/digitaleflex/axiom/services/agent/internal/health"
	"github.com/digitaleflex/axiom/services/agent/internal/heartbeat"
	"github.com/digitaleflex/axiom/services/agent/internal/logs"
	"github.com/digitaleflex/axiom/services/agent/internal/protocol"
	"github.com/digitaleflex/axiom/services/agent/internal/recovery"
	"github.com/digitaleflex/axiom/services/agent/internal/runtime/docker"
	"github.com/digitaleflex/axiom/services/agent/internal/runtime/traefik"
	"github.com/digitaleflex/axiom/services/agent/internal/security/auth"
	"github.com/digitaleflex/axiom/services/agent/internal/security/ownership"
)

// Stable codes the bridge maps onto adapter failures that carry no code of
// their own. The canonical wire set is still owned by #84/#85 (failure matrix
// gap 4); until then these are the codes the dispatcher reports.
const (
	CodeHealthFailed  = "HEALTH_CHECK_FAILED"
	CodeNetworkFailed = "NETWORK_CONFIG_FAILED"
)

// errUnscopedBridge guards the bridge against executing an operation whose
// scope was never resolved (#145). The dispatcher refuses such an operation at
// validation time; this is the defense-in-depth backstop, so a future caller
// that reaches the bridge unscoped gets a stable code instead of an
// un-attributable ownership label.
var errUnscopedBridge = errors.New("bootstrap: operation carries no application scope")

// codedError implements dispatcher.ErrorCoder so an adapter failure surfaces a
// stable machine code instead of degrading to INTERNAL.
type codedError struct {
	code string
	err  error
}

func (e *codedError) Error() string     { return e.code + ": " + e.err.Error() }
func (e *codedError) ErrorCode() string { return e.code }
func (e *codedError) Unwrap() error     { return e.err }

// runtimeBridge is the production dispatcher.Adapter over the real runtime
// adapters (Docker #83, Traefik #84, health #85). It is what the integration
// harness used to fake, and what closes that gap in the failure matrix.
//
// It implements dispatcher.ScopedAdapter: the closed operation payloads carry
// no resource or server identity, so the bridge resolves a scoped view from
// the operation before executing.
type runtimeBridge struct {
	docker  *docker.Adapter
	traefik *traefik.Adapter
	health  *health.Checker

	applicationID string
	deploymentID  string
	serverID      string
}

var _ dispatcher.ScopedAdapter = (*runtimeBridge)(nil)

// WithScope returns a copy of the bridge bound to one operation's scope. The
// application and the deployment are distinct identities (#145): the
// application is carried through verbatim from the protocol operation, never
// derived from the deployment. The receiver is never mutated, so concurrent
// operations never share ownership labels.
func (b *runtimeBridge) WithScope(applicationID, deploymentID, serverID string) dispatcher.Adapter {
	scoped := *b
	scoped.applicationID = applicationID
	scoped.deploymentID = deploymentID
	scoped.serverID = serverID
	return &scoped
}

// scope is the operation scope this bridge executes in.
func (b *runtimeBridge) scope() ownership.Scope {
	return ownership.Scope{ApplicationID: b.applicationID, DeploymentID: b.deploymentID}
}

// CreateRuntime implements dispatcher.Adapter (CREATE_RUNTIME).
func (b *runtimeBridge) CreateRuntime(ctx context.Context, p dispatcher.CreateParams) error {
	if b.applicationID == "" {
		return &codedError{code: dispatcher.CodeIncompleteScope, err: errUnscopedBridge}
	}
	_, err := b.docker.Create(ctx, docker.CreateSpec{
		DeploymentID:  b.deploymentID,
		ApplicationID: b.applicationID,
		ServerID:      b.serverID,
		Container:     p.Container,
		ImageRef:      p.ImageRef,
		Port:          p.Port,
	})
	return mapDocker(err)
}

// ConfigureNetwork implements dispatcher.Adapter (NETWORK).
func (b *runtimeBridge) ConfigureNetwork(ctx context.Context, p dispatcher.NetworkParams) error {
	if b.applicationID == "" {
		return &codedError{code: dispatcher.CodeIncompleteScope, err: errUnscopedBridge}
	}
	err := b.traefik.Configure(ctx, traefik.Request{
		Container:     p.Container,
		Domain:        p.Domain,
		Port:          p.Port,
		TLS:           p.TLS,
		ApplicationID: b.applicationID,
		DeploymentID:  b.deploymentID,
		ServerID:      b.serverID,
	})
	if err == nil {
		return nil
	}
	if errors.Is(err, ownership.ErrNotManaged) {
		return &codedError{code: docker.CodeNotManaged, err: err}
	}
	return &codedError{code: CodeNetworkFailed, err: err}
}

// StartRuntime implements dispatcher.Adapter (START).
func (b *runtimeBridge) StartRuntime(ctx context.Context, p dispatcher.StartParams) error {
	return mapDocker(b.docker.Start(ctx, p.Container))
}

// StopRuntime implements dispatcher.Adapter (STOP).
func (b *runtimeBridge) StopRuntime(ctx context.Context, p dispatcher.StopParams) error {
	return mapDocker(b.docker.Stop(ctx, p.Container))
}

// RemoveRuntime implements dispatcher.Adapter (REMOVE).
func (b *runtimeBridge) RemoveRuntime(ctx context.Context, p dispatcher.RemoveParams) error {
	return mapDocker(b.docker.Remove(ctx, p.Container))
}

// HealthCheck implements dispatcher.Adapter (VERIFY). The probe report is
// returned even when the adapter fails, so a failing probe reaches the Engine
// as an actionable report rather than a bare transport failure.
func (b *runtimeBridge) HealthCheck(ctx context.Context, p dispatcher.VerifyParams) (dispatcher.HealthReport, error) {
	path := p.Path
	if path == "" {
		path = "/"
	}
	rep, err := b.health.Check(ctx, health.Spec{
		Type:           health.TypeHTTP,
		URL:            "http://" + p.Domain + path,
		Timeout:        time.Duration(p.TimeoutSeconds) * time.Second,
		ExpectedStatus: health.DefaultExpectedStatus,
	})
	report := dispatcher.HealthReport{
		StatusCode: rep.StatusCode,
		LatencyMs:  rep.LatencyMs,
		Attempt:    rep.Attempt,
	}
	if err != nil {
		return report, &codedError{code: CodeHealthFailed, err: err}
	}
	return report, nil
}

// mapDocker surfaces the Docker adapter's stable code verbatim (the adapter now
// implements dispatcher.ErrorCoder directly).
func mapDocker(err error) error {
	if err == nil {
		return nil
	}
	var de *docker.Error
	if errors.As(err, &de) && de.Code != "" {
		return err
	}
	return &codedError{code: dispatcher.CodeInternal, err: err}
}

// dockerRuntime adapts the Docker adapter to recovery.Runtime so the reconciler
// can list, inspect and (only under an explicit allow-list) remove
// Axiom-managed containers. The composition root never enables cleanup, so
// Remove is unreachable in production.
type dockerRuntime struct {
	adapter *docker.Adapter
	timeout time.Duration
}

var _ recovery.Runtime = (*dockerRuntime)(nil)

// List returns every Axiom-managed container on the host.
func (r *dockerRuntime) List() ([]recovery.ContainerInfo, error) {
	ctx, cancel := r.ctx()
	defer cancel()
	containers, err := r.adapter.ListManaged(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]recovery.ContainerInfo, 0, len(containers))
	for _, c := range containers {
		out = append(out, recovery.ContainerInfo{Name: c.Name, Labels: c.Labels})
	}
	return out, nil
}

// Inspect returns one container's labels.
func (r *dockerRuntime) Inspect(name string) (recovery.ContainerInfo, error) {
	ctx, cancel := r.ctx()
	defer cancel()
	info, err := r.adapter.Inspect(ctx, name)
	if err != nil {
		return recovery.ContainerInfo{}, err
	}
	return recovery.ContainerInfo{Name: info.Name, Labels: info.Labels}, nil
}

// Remove deletes a managed container.
func (r *dockerRuntime) Remove(name string) error {
	ctx, cancel := r.ctx()
	defer cancel()
	return r.adapter.Remove(ctx, name)
}

// ctx bounds one reconciliation call.
func (r *dockerRuntime) ctx() (context.Context, context.CancelFunc) {
	timeout := r.timeout
	if timeout <= 0 {
		timeout = DefaultDockerTimeout
	}
	return context.WithTimeout(context.Background(), timeout)
}

// dockerLogSource backs the bounded log fetcher (#86) with `docker logs
// --timestamps --tail n`, one already-validated container at a time.
type dockerLogSource struct {
	runner docker.Runner
}

var _ logs.Docker = (*dockerLogSource)(nil)

// Logs implements logs.Docker.
func (s *dockerLogSource) Logs(ctx context.Context, container string, tail int, timestamps bool) (string, error) {
	if !ownership.ValidateName(container) {
		return "", fmt.Errorf("logs: invalid container name %q", container)
	}
	if tail <= 0 {
		tail = logs.DefaultLines
	}
	args := []string{"logs", "--tail", strconv.Itoa(tail)}
	if timestamps {
		args = append(args, "--timestamps")
	}
	args = append(args, container)
	out, exit, err := s.runner.Run(ctx, args...)
	if err != nil {
		return "", &docker.Error{Code: docker.CodeDockerFailed, Message: "docker logs failed to start", Cause: err, Log: out}
	}
	if exit != 0 {
		return "", &docker.Error{Code: docker.CodeDockerFailed, Message: fmt.Sprintf("docker logs exited with code %d", exit), ExitCode: exit, Log: out}
	}
	return out, nil
}

// newHeartbeat builds the signed liveness loop: capability and resource
// samples ride on every heartbeat, and a probe failure forces DEGRADED.
func newHeartbeat(cfg config.Config, id protocol.AgentIdentity, client *auth.Client, d capabilities.Discoverer, log *slog.Logger) *heartbeat.Loop {
	loop := heartbeat.NewLoop(id, cfg.HeartbeatInterval, heartbeat.NewHTTPTransport(cfg.EngineURL, client), log)
	loop.Resources = func(ctx context.Context) (capabilities.Report, error) {
		return d.Discover(ctx), nil
	}
	return loop
}
