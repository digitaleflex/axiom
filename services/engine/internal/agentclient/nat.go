// Package agentclient implements the Engine-side agent transport.
package agentclient

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/digitaleflex/axiom/services/engine/internal/agentpoll"
	"github.com/digitaleflex/axiom/services/engine/internal/executor"
	"github.com/digitaleflex/axiom/services/engine/internal/health"
)

// ErrNoAgentIdentity signals that no agent identity is bound to the target server.
var ErrNoAgentIdentity = errors.New("agentclient: no agent identity for server")

// natAgent implements executor.RuntimeAgent by pushing operations to the
// agentpoll.Manager (NAT mode). The agent pulls operations via long-poll
// and reports results via POST /api/v1/agent/result.
type natAgent struct {
	pool      *agentpoll.Manager
	servers   Servers
	agentKeys SigningKeys
	log       *slog.Logger
}

var _ executor.RuntimeAgent = (*natAgent)(nil)

// NewNATAgent returns a RuntimeAgent that dispatches operations through the
// agentpoll queue (NAT mode). When pool is nil, the caller should fall back
// to the loopback Client.
func NewNATAgent(pool *agentpoll.Manager, servers Servers, agentKeys SigningKeys, log *slog.Logger) executor.RuntimeAgent {
	if pool == nil {
		return nil
	}
	return &natAgent{pool: pool, servers: servers, agentKeys: agentKeys, log: log}
}

// agentIDForServer resolves the agent identity bound to a server.
func (n *natAgent) agentIDForServer(ctx context.Context, serverID string) (string, error) {
	if n.servers == nil {
		return "", ErrNoAgentIdentity
	}
	if _, err := n.servers.Get(ctx, serverID); err != nil {
		return "", err
	}
	// The server record stores the agent status; we need the agent ID.
	// The agentauth.Service.Status returns the agent ID bound to the server.
	// We access it via the SigningKeys seam which wraps agentauth.
	agentID, _, err := n.agentKeys.SigningKey(ctx, serverID)
	if err != nil {
		return "", err
	}
	if agentID == "" {
		return "", ErrNoAgentIdentity
	}
	return agentID, nil
}

// buildOperation creates a protocol.Operation JSON payload for the given request.
func (n *natAgent) buildOperation(op executor.Operation, typ string, payload any) ([]byte, error) {
	// The operation envelope mirrors protocol.Operation (services/agent/internal/protocol).
	// We construct it manually to avoid importing the agent module.
	envelope := map[string]any{
		"protocol":      2,
		"messageId":     op.OperationID,
		"sentAt":        time.Now().UTC().Format(time.RFC3339),
		"correlationId": op.CorrelationID,
		"operationId":   op.OperationID,
		"type":          typ,
		"deploymentId":  op.DeploymentID,
		"serverId":      op.ServerID,
		"payload":       payload,
	}
	// ApplicationID must be resolved from the deployment.
	// For now, we leave it empty; the agentpoll queue stores it separately.
	return json.Marshal(envelope)
}

// CreateRuntime implements executor.RuntimeAgent (CREATE_RUNTIME).
func (n *natAgent) CreateRuntime(ctx context.Context, req executor.CreateRuntimeRequest) error {
	agentID, err := n.agentIDForServer(ctx, req.Operation.ServerID)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"imageRef":  req.ImageRef,
		"container": req.Container,
		"port":      req.Port,
	}
	body, err := n.buildOperation(req.Operation, "CREATE_RUNTIME", payload)
	if err != nil {
		return err
	}
	op := agentpoll.Operation{
		OperationID:   req.Operation.OperationID,
		DeploymentID:  req.Operation.DeploymentID,
		ApplicationID: "", // Will be filled by the agentpoll route from deployment record
		Body:          body,
	}
	return n.pool.Enqueue(ctx, agentID, op)
}

// ConfigureNetwork implements executor.RuntimeAgent (NETWORK).
func (n *natAgent) ConfigureNetwork(ctx context.Context, req executor.NetworkRequest) error {
	agentID, err := n.agentIDForServer(ctx, req.Operation.ServerID)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"container": req.Container,
		"proxy":     req.Proxy,
		"domain":    req.Domain,
		"tls":       req.TLS,
		"port":      req.Port,
	}
	body, err := n.buildOperation(req.Operation, "NETWORK", payload)
	if err != nil {
		return err
	}
	op := agentpoll.Operation{
		OperationID:   req.Operation.OperationID,
		DeploymentID:  req.Operation.DeploymentID,
		ApplicationID: "",
		Body:          body,
	}
	return n.pool.Enqueue(ctx, agentID, op)
}

// StartRuntime implements executor.RuntimeAgent (START).
func (n *natAgent) StartRuntime(ctx context.Context, req executor.StartRequest) error {
	agentID, err := n.agentIDForServer(ctx, req.Operation.ServerID)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"container": req.Container,
	}
	body, err := n.buildOperation(req.Operation, "START", payload)
	if err != nil {
		return err
	}
	op := agentpoll.Operation{
		OperationID:   req.Operation.OperationID,
		DeploymentID:  req.Operation.DeploymentID,
		ApplicationID: "",
		Body:          body,
	}
	return n.pool.Enqueue(ctx, agentID, op)
}

// HealthCheck implements executor.RuntimeAgent (VERIFY).
func (n *natAgent) HealthCheck(ctx context.Context, req executor.HealthCheckRequest) (health.ProbeReport, error) {
	agentID, err := n.agentIDForServer(ctx, req.Operation.ServerID)
	if err != nil {
		return health.ProbeReport{}, err
	}
	payload := map[string]any{
		"domain":          req.Domain,
		"path":            req.Path,
		"timeoutSeconds":  req.TimeoutSeconds,
	}
	body, err := n.buildOperation(req.Operation, "VERIFY", payload)
	if err != nil {
		return health.ProbeReport{}, err
	}
	op := agentpoll.Operation{
		OperationID:   req.Operation.OperationID,
		DeploymentID:  req.Operation.DeploymentID,
		ApplicationID: "",
		Body:          body,
	}
	if err := n.pool.Enqueue(ctx, agentID, op); err != nil {
		return health.ProbeReport{}, err
	}
	// In NAT mode, the health check result comes back via the result queue.
	// We wait for the result with a timeout matching the request.
	resultOp, err := n.pool.GetResult(ctx, agentID, req.Operation.OperationID)
	if err != nil {
		return health.ProbeReport{}, err
	}
	// Parse the result body (acknowledgement + result).
	var result struct {
		Health *healthReport `json:"health"`
		Success *bool `json:"success"`
		ErrorCode string `json:"errorCode"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(resultOp.Body, &result); err != nil {
		return health.ProbeReport{}, err
	}
	if result.Success != nil && !*result.Success {
		return health.ProbeReport{}, errors.New(result.Message)
	}
	if result.Health != nil {
		return health.NewProbeReport(result.Health.StatusCode, result.Health.LatencyMs, "", result.Health.Attempt), nil
	}
	return health.ProbeReport{}, errors.New("agent acknowledged VERIFY without a health report")
}