// Package agentclient is the Engine-side HTTP client for Runtime Agent
// operations (issues #75, #80, #100). It is the executor.RuntimeAgent
// implementation that turns one executor request into exactly one
// protocol.Operation message and reads back its acknowledgement/result.
//
// The client is deliberately bounded and dumb: it speaks only the closed
// operation set of docs/architecture/agent-protocol.md §6, it never carries a
// command string (there is no shell anywhere in this package), every request
// has a deadline, and every response body is size-limited and schema-checked
// before it is trusted.
//
// The wire structs below mirror services/agent/internal/protocol. The two Go
// modules cannot share a package (separate modules, ownership boundary); the
// conformance table is docs/architecture/agent-protocol.md §6 and field names
// are identical on both sides by contract.
//
// TODO(#77, ADR-0008): the Engine→Agent credential direction is still an open
// question ("mTLS vs token-based agent credentials"). agentauth owns the
// agent→Engine direction only and persists credential HASHES, so the Engine
// holds no plaintext it could present to an agent. Until that contract exists,
// CredentialProvider is a seam: the composition root injects a provider that
// refuses, which fails the runtime step with the stable code
// AGENT_NO_CREDENTIAL rather than dispatching unauthenticated work.
package agentclient

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/digitaleflex/axiom/services/engine/internal/deployment"
	"github.com/digitaleflex/axiom/services/engine/internal/executor"
	"github.com/digitaleflex/axiom/services/engine/internal/health"
	"github.com/digitaleflex/axiom/services/engine/internal/server"
)

// Stable error codes. They are logged and surfaced as the executor's
// RUNTIME_FAILED class; API clients branch on that class (API contract §18),
// never on these codes.
const (
	CodeNoCredential = "AGENT_NO_CREDENTIAL"
	CodeNotFound     = "AGENT_NOT_FOUND"
	CodeUnreachable  = "AGENT_UNREACHABLE"
	CodeTimeout      = "AGENT_TIMEOUT"
	CodeRejected     = "AGENT_OPERATION_REJECTED"
	CodeFailed       = "AGENT_OPERATION_FAILED"
	CodeMalformed    = "AGENT_MALFORMED_RESPONSE"
	CodeTooLarge     = "AGENT_RESPONSE_TOO_LARGE"
	CodeInsecure     = "AGENT_INSECURE_ENDPOINT"
	CodeInternal     = "AGENT_INTERNAL"
	// CodeNoApplication reports that the application a deployment rolls out
	// could not be resolved (#145). The operation is never put on the wire
	// without it: the agent requires applicationId on every operation.
	CodeNoApplication = "AGENT_NO_APPLICATION"
)

// Bounds. Every one of them fails closed.
const (
	// MaxResponseBytes bounds an agent response body (ack or result).
	MaxResponseBytes = 64 << 10
	// MaxRequestBytes bounds an outbound operation body.
	MaxRequestBytes = 64 << 10
	// DefaultTimeout bounds one operation exchange when the caller supplies
	// no earlier deadline. The executor normally imposes a tighter per-step
	// deadline (executor.CreateRuntimeTimeout and friends).
	DefaultTimeout = 30 * time.Second
)

// ProtocolVersion mirrors protocol.Version (services/agent/internal/protocol).
// The two modules cannot share the constant, exactly as agentauth does.
//
// Operation.ApplicationID is mandatory (#145): the application scope is
// required from the V2 envelope onwards, and an operation that does not name
// the application it deploys is refused by the agent with INCOMPLETE_SCOPE.
// MinVersion stays at 1, so no existing V1 peer is broken by the bump.
const ProtocolVersion = 2

// DefaultOperationPath is where an operation is posted.
//
// TODO(#75 follow-up, ADR-0008): the transport is not decided yet — ADR-0008
// is still "Proposed" and the agent exposes no inbound listener. This path is
// therefore provisional and injectable (Client.Path); align it with the agent
// transport when that issue lands.
const DefaultOperationPath = "/api/v1/agent/operations"

// Closed operation types (protocol.Operation types, lines 32-39 of
// services/agent/internal/protocol/protocol.go).
const (
	opCreateRuntime    = "CREATE_RUNTIME"
	opConfigureNetwork = "NETWORK"
	opStartRuntime     = "START"
	opVerifyHealth     = "VERIFY"
	opStopRuntime      = "STOP"
	opRemoveRuntime    = "REMOVE"
)

// Error is a typed agent-transport failure with a stable code.
type Error struct {
	Code    string
	Message string
	// AgentCode is the stable error code the agent reported, when any
	// (e.g. RUNTIME_IMAGE_MISSING, HEALTH_CHECK_FAILED).
	AgentCode string
	Cause     error
}

func (e *Error) Error() string {
	if e.AgentCode != "" {
		return e.Code + "/" + e.AgentCode + ": " + e.Message
	}
	return e.Code + ": " + e.Message
}

func (e *Error) Unwrap() error { return e.Cause }

// Sentinels callers may test for.
var (
	ErrNoCredential  = errors.New("agentclient: no credential available for the agent")
	ErrNotFound      = errors.New("agentclient: server not found")
	ErrTimeout       = errors.New("agentclient: operation timed out")
	ErrUnreachable   = errors.New("agentclient: agent endpoint unreachable")
	ErrNoApplication = errors.New("agentclient: no application for this deployment")
)

// Servers resolves the target server record. It is satisfied by
// *server.Service and by test fakes.
type Servers interface {
	Get(ctx context.Context, id string) (server.Record, error)
}

// Applications resolves the application a deployment rolls out (#145). It is
// satisfied by a deployment lookup and by test fakes.
//
// The application ID is NOT carried by executor.Operation, so the client
// resolves it from the deployment itself. There is no fallback: an operation
// whose application cannot be resolved is refused with CodeNoApplication
// rather than sent without the scope the agent now requires.
type Applications interface {
	Get(ctx context.Context, id string) (deployment.Record, error)
}

// Credential is the Engine's outbound credential for one server's agent.
type Credential struct {
	Token string
	// Version is the credential version the agent should accept, so it can
	// detect a stale Engine without a second round trip.
	Version int
}

// CredentialProvider resolves the outbound credential for a server's agent.
//
// TODO(#77): no production provider exists — see the package comment.
type CredentialProvider interface {
	Credential(ctx context.Context, serverID string) (Credential, error)
}

// EndpointFunc returns the base URL of the agent serving a server address.
type EndpointFunc func(ctx context.Context, serverID, address string) (string, error)

// Client posts protocol operations to a Runtime Agent. It implements
// executor.RuntimeAgent.
type Client struct {
	Servers     Servers
	Credentials CredentialProvider
	// Applications resolves the application each deployment belongs to (#145).
	// Required: without it every operation is refused with
	// AGENT_NO_APPLICATION.
	Applications Applications
	// Endpoint resolves the agent base URL. Defaults to https://<address>.
	Endpoint EndpointFunc
	HTTP     *http.Client
	// Production requires https for every agent endpoint, mirroring the
	// agent's own outbound policy (services/agent/internal/security/transport).
	Production bool
	// Path overrides DefaultOperationPath.
	Path string
	// DefaultTimeout bounds one exchange when the caller supplies no earlier
	// deadline. Zero means the package DefaultTimeout.
	DefaultTimeout time.Duration
	Log            *slog.Logger

	Now      func() time.Time
	NewNonce func() string
}

var _ executor.RuntimeAgent = (*Client)(nil)

func (c *Client) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now().UTC()
}

func (c *Client) log() *slog.Logger {
	if c.Log != nil {
		return c.Log
	}
	return slog.Default()
}

func (c *Client) timeout() time.Duration {
	if c.DefaultTimeout > 0 {
		return c.DefaultTimeout
	}
	return DefaultTimeout
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	// No redirects: an agent endpoint never redirects, and following one
	// would replay a credential against an unvouched host.
	return &http.Client{Timeout: c.timeout(), CheckRedirect: noRedirect}
}

func noRedirect(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

func (c *Client) path() string {
	if c.Path != "" {
		return c.Path
	}
	return DefaultOperationPath
}

func (c *Client) nonce() string {
	if c.NewNonce != nil {
		return c.NewNonce()
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Never reuse a nonce: fail the request instead.
		return ""
	}
	return hex.EncodeToString(b)
}

// --- wire format (mirror of services/agent/internal/protocol) ----------------

type payload struct {
	ImageRef       string `json:"imageRef,omitempty"`
	Container      string `json:"container,omitempty"`
	Port           int    `json:"port,omitempty"`
	Proxy          string `json:"proxy,omitempty"`
	Domain         string `json:"domain,omitempty"`
	TLS            bool   `json:"tls,omitempty"`
	Path           string `json:"path,omitempty"`
	TimeoutSeconds int    `json:"timeoutSeconds,omitempty"`
}

// operation is protocol.Operation. The envelope fields are declared inline
// because Go's JSON encoder cannot inline an embedded struct declared in
// another package with an explicit tag; the wire shape is identical.
type operation struct {
	Protocol      int    `json:"protocol"`
	MessageID     string `json:"messageId"`
	SentAt        string `json:"sentAt"`
	CorrelationID string `json:"correlationId,omitempty"`
	OperationID   string `json:"operationId"`
	Type          string `json:"type"`
	DeploymentID  string `json:"deploymentId"`
	// ApplicationID is required (#145): the agent stamps it on the ownership
	// labels and refuses any operation that omits it.
	ApplicationID string  `json:"applicationId"`
	ServerID      string  `json:"serverId"`
	Payload       payload `json:"payload"`
}

// operationResponse is the union of protocol.Acknowledgement and
// protocol.Result: an agent acknowledges an operation before executing it and
// reports a result afterwards, and both share the envelope plus operationId and
// deploymentId. One struct lets the client accept either without trusting a
// field it does not understand.
type operationResponse struct {
	Protocol     int           `json:"protocol"`
	MessageID    string        `json:"messageId"`
	OperationID  string        `json:"operationId"`
	DeploymentID string        `json:"deploymentId"`
	Accepted     *bool         `json:"accepted,omitempty"`
	Reason       string        `json:"reason,omitempty"`
	Success      *bool         `json:"success,omitempty"`
	ErrorCode    string        `json:"errorCode,omitempty"`
	Message      string        `json:"message,omitempty"`
	Health       *healthReport `json:"health,omitempty"`
	FinishedAt   string        `json:"finishedAt,omitempty"`
}

// healthReport is protocol.HealthReport.
type healthReport struct {
	StatusCode int   `json:"statusCode"`
	LatencyMs  int64 `json:"latencyMs"`
	Attempt    int   `json:"attempt"`
}

// --- executor.RuntimeAgent --------------------------------------------------

// CreateRuntime implements executor.RuntimeAgent (CREATE_RUNTIME).
func (c *Client) CreateRuntime(ctx context.Context, req executor.CreateRuntimeRequest) error {
	return c.dispatch(ctx, spec{
		op:  req.Operation,
		typ: opCreateRuntime,
		body: payload{
			ImageRef:  req.ImageRef,
			Container: req.Container,
			Port:      req.Port,
		},
	})
}

// ConfigureNetwork implements executor.RuntimeAgent (NETWORK).
func (c *Client) ConfigureNetwork(ctx context.Context, req executor.NetworkRequest) error {
	return c.dispatch(ctx, spec{
		op:  req.Operation,
		typ: opConfigureNetwork,
		body: payload{
			Container: req.Container,
			Proxy:     req.Proxy,
			Domain:    req.Domain,
			TLS:       req.TLS,
			Port:      req.Port,
		},
	})
}

// StartRuntime implements executor.RuntimeAgent (START).
func (c *Client) StartRuntime(ctx context.Context, req executor.StartRequest) error {
	return c.dispatch(ctx, spec{
		op:   req.Operation,
		typ:  opStartRuntime,
		body: payload{Container: req.Container},
	})
}

// HealthCheck implements executor.RuntimeAgent (VERIFY).
//
// Per the interface contract, a transport-level failure is an error while an
// HTTP response that merely fails the health policy is a report: whenever the
// agent answers with a probe report it is returned to the caller (the
// executor) for policy evaluation, whatever the reported success flag says.
func (c *Client) HealthCheck(ctx context.Context, req executor.HealthCheckRequest) (health.ProbeReport, error) {
	resp, err := c.exchange(ctx, spec{
		op:  req.Operation,
		typ: opVerifyHealth,
		body: payload{
			Domain:         req.Domain,
			Path:           req.Path,
			TimeoutSeconds: req.TimeoutSeconds,
		},
	})
	if err != nil {
		return health.ProbeReport{}, err
	}
	if resp.Health != nil {
		return health.NewProbeReport(resp.Health.StatusCode, resp.Health.LatencyMs, "", resp.Health.Attempt), nil
	}
	if resp.Success != nil && *resp.Success {
		// A VERIFY that succeeds without a report is not a usable probe result:
		// silently passing it would be an implied pass (API contract §16).
		return health.ProbeReport{}, &Error{Code: CodeMalformed, Message: "agent acknowledged VERIFY without a health report"}
	}
	return health.ProbeReport{}, resultError(resp)
}

// StopContainer implements the closed-set STOP operation for a container.
// The executor does not call it yet (rollback is not wired); it exists so the
// closed operation set is complete on the Engine side of the boundary.
func (c *Client) StopContainer(ctx context.Context, op executor.Operation, container string) error {
	return c.dispatch(ctx, spec{op: op, typ: opStopRuntime, body: payload{Container: container}})
}

// RemoveContainer implements the closed-set REMOVE operation for a container.
func (c *Client) RemoveContainer(ctx context.Context, op executor.Operation, container string) error {
	return c.dispatch(ctx, spec{op: op, typ: opRemoveRuntime, body: payload{Container: container}})
}

type spec struct {
	op   executor.Operation
	typ  string
	body payload
}

// dispatch runs one operation and turns a rejection or failed result into a
// typed error.
func (c *Client) dispatch(ctx context.Context, s spec) error {
	resp, err := c.exchange(ctx, s)
	if err != nil {
		return err
	}
	return resultError(resp)
}

// resultError turns a rejection or a failed result into a typed error. An ack
// that accepts the operation without a result is not an error: the agent took
// the work.
func resultError(resp operationResponse) error {
	if resp.Accepted != nil && !*resp.Accepted {
		msg := resp.Reason
		if msg == "" {
			msg = "agent rejected the operation"
		}
		return &Error{Code: CodeRejected, Message: msg}
	}
	if resp.Success != nil && !*resp.Success {
		msg := resp.Message
		if msg == "" {
			msg = "agent reported a failed operation"
		}
		return &Error{Code: CodeFailed, AgentCode: resp.ErrorCode, Message: msg}
	}
	return nil
}

// exchange posts one operation and returns the agent's answer.
func (c *Client) exchange(ctx context.Context, s spec) (operationResponse, error) {
	if ctx == nil {
		return operationResponse{}, &Error{Code: CodeInternal, Message: "context is required"}
	}
	if c.Servers == nil {
		return operationResponse{}, &Error{Code: CodeInternal, Message: "agent client has no server lookup"}
	}
	if err := validate(s); err != nil {
		return operationResponse{}, err
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.timeout())
		defer cancel()
	}

	srv, err := c.Servers.Get(ctx, s.op.ServerID)
	if err != nil {
		if errors.Is(err, server.ErrNotFound) {
			return operationResponse{}, &Error{Code: CodeNotFound, Message: "server not found", Cause: ErrNotFound}
		}
		return operationResponse{}, &Error{Code: CodeInternal, Message: "resolve server", Cause: err}
	}

	cred, err := c.credential(ctx, s.op.ServerID)
	if err != nil {
		return operationResponse{}, err
	}

	// The application scope (#145) is resolved before the message is built: an
	// operation without it is refused by the agent, so it is never sent.
	applicationID, err := c.applicationID(ctx, s.op.DeploymentID)
	if err != nil {
		return operationResponse{}, err
	}

	base, err := c.endpoint(ctx, s.op.ServerID, srv.Address)
	if err != nil {
		return operationResponse{}, err
	}

	msg := operation{
		Protocol:      ProtocolVersion,
		MessageID:     s.op.OperationID,
		SentAt:        c.now().Format(time.RFC3339),
		CorrelationID: s.op.CorrelationID,
		OperationID:   s.op.OperationID,
		Type:          s.typ,
		DeploymentID:  s.op.DeploymentID,
		ApplicationID: applicationID,
		ServerID:      s.op.ServerID,
		Payload:       s.body,
	}
	raw, err := json.Marshal(msg)
	if err != nil {
		return operationResponse{}, &Error{Code: CodeInternal, Message: "encode operation", Cause: err}
	}
	if len(raw) > MaxRequestBytes {
		return operationResponse{}, &Error{Code: CodeInternal, Message: "operation body exceeds the maximum size"}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+c.path(), bytes.NewReader(raw))
	if err != nil {
		return operationResponse{}, &Error{Code: CodeInternal, Message: "build request", Cause: err}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if cred.Token != "" {
		req.Header.Set("Authorization", "Bearer "+cred.Token)
	}
	req.Header.Set("X-Credential-Version", fmt.Sprintf("%d", cred.Version))
	req.Header.Set("X-Timestamp", c.now().Format(time.RFC3339))
	if n := c.nonce(); n != "" {
		req.Header.Set("X-Nonce", n)
	}

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return operationResponse{}, c.transportError(ctx, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBytes+1))
	if err != nil {
		return operationResponse{}, c.transportError(ctx, err)
	}
	if len(body) > MaxResponseBytes {
		return operationResponse{}, &Error{Code: CodeTooLarge, Message: "agent response exceeds the maximum size"}
	}

	switch {
	case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusForbidden:
		return operationResponse{}, &Error{Code: CodeRejected, Message: "agent refused the credential (HTTP " + resp.Status + ")"}
	case resp.StatusCode >= 500:
		return operationResponse{}, &Error{Code: CodeUnreachable, Message: "agent returned HTTP " + resp.Status}
	case resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted:
		return operationResponse{}, &Error{Code: CodeRejected, Message: "agent returned HTTP " + resp.Status}
	}

	var out operationResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return operationResponse{}, &Error{Code: CodeMalformed, Message: "decode agent response", Cause: err}
	}
	// Binding: an answer about another deployment or operation is never ours.
	if out.DeploymentID != "" && out.DeploymentID != s.op.DeploymentID {
		return operationResponse{}, &Error{Code: CodeMalformed, Message: "agent response names another deployment"}
	}
	if out.OperationID != "" && out.OperationID != s.op.OperationID {
		return operationResponse{}, &Error{Code: CodeMalformed, Message: "agent response names another operation"}
	}
	if out.Protocol != 0 && out.Protocol != ProtocolVersion {
		return operationResponse{}, &Error{Code: CodeMalformed, Message: "agent speaks another protocol version"}
	}
	if out.Accepted == nil && out.Success == nil {
		return operationResponse{}, &Error{Code: CodeMalformed, Message: "agent response carries neither an acknowledgement nor a result"}
	}
	c.log().Info("agent operation completed",
		"deploymentId", s.op.DeploymentID, "operationId", s.op.OperationID, "operationType", s.typ)
	return out, nil
}

// applicationID resolves the application a deployment rolls out. It fails
// closed: no resolver, an unknown deployment or an empty application ID all
// refuse the operation instead of sending it without a scope.
func (c *Client) applicationID(ctx context.Context, deploymentID string) (string, error) {
	if c.Applications == nil {
		return "", &Error{Code: CodeNoApplication, Message: "agent client has no application lookup", Cause: ErrNoApplication}
	}
	rec, err := c.Applications.Get(ctx, deploymentID)
	if err != nil {
		return "", &Error{Code: CodeNoApplication, Message: "resolve the deployment's application", Cause: err}
	}
	if rec.ApplicationID == "" {
		return "", &Error{Code: CodeNoApplication, Message: "deployment " + deploymentID + " names no application", Cause: ErrNoApplication}
	}
	return rec.ApplicationID, nil
}

func (c *Client) credential(ctx context.Context, serverID string) (Credential, error) {
	if c.Credentials == nil {
		return Credential{}, &Error{Code: CodeNoCredential, Message: ErrNoCredential.Error(), Cause: ErrNoCredential}
	}
	cred, err := c.Credentials.Credential(ctx, serverID)
	if err != nil || cred.Token == "" {
		return Credential{}, &Error{Code: CodeNoCredential, Message: ErrNoCredential.Error(), Cause: err}
	}
	return cred, nil
}

// endpoint resolves and validates the agent base URL.
func (c *Client) endpoint(ctx context.Context, serverID, address string) (string, error) {
	var (
		base string
		err  error
	)
	if c.Endpoint != nil {
		base, err = c.Endpoint(ctx, serverID, address)
	} else {
		base, err = defaultEndpoint(address)
	}
	if err != nil {
		return "", &Error{Code: CodeInternal, Message: "resolve agent endpoint", Cause: err}
	}
	if err := c.checkScheme(base); err != nil {
		return "", err
	}
	return strings.TrimRight(base, "/"), nil
}

// checkScheme fails closed: https always, http only for loopback outside
// production (same rule the agent applies to the Engine endpoint).
func (c *Client) checkScheme(base string) error {
	switch {
	case strings.HasPrefix(base, "https://"):
		return nil
	case strings.HasPrefix(base, "http://"):
		if c.Production {
			return &Error{Code: CodeInsecure, Message: "http is not permitted for an agent endpoint in production"}
		}
		if !isLoopback(hostOf(base[len("http://"):])) {
			return &Error{Code: CodeInsecure, Message: "http is only permitted for a loopback agent endpoint"}
		}
		return nil
	default:
		return &Error{Code: CodeInsecure, Message: "agent endpoint must be an http(s) URL"}
	}
}

func hostOf(raw string) string {
	if i := strings.IndexAny(raw, "/?#"); i >= 0 {
		raw = raw[:i]
	}
	if h, _, err := net.SplitHostPort(raw); err == nil {
		return h
	}
	return strings.Trim(raw, "[]")
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// defaultEndpoint derives https://<address> from a server address, which the
// server domain stores without a scheme (server.ValidAddress).
func defaultEndpoint(address string) (string, error) {
	address = strings.TrimSpace(address)
	if address == "" || strings.Contains(address, "://") {
		return "", fmt.Errorf("agentclient: invalid server address %q", address)
	}
	return "https://" + address, nil
}

// transportError classifies a transport failure without leaking internals.
func (c *Client) transportError(ctx context.Context, err error) error {
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.Is(ctx.Err(), context.DeadlineExceeded), errors.Is(err, ErrTimeout):
		return &Error{Code: CodeTimeout, Message: ErrTimeout.Error(), Cause: err}
	case errors.Is(err, context.Canceled):
		return &Error{Code: CodeTimeout, Message: "operation cancelled", Cause: err}
	default:
		return &Error{Code: CodeUnreachable, Message: ErrUnreachable.Error(), Cause: err}
	}
}

// validate rejects an operation the Engine must never put on the wire. The
// agent re-validates everything (protocol.Operation.Validate); this is the
// first gate, and it fails closed on the unknown-type case: there is no shell
// path and no command string anywhere in this package.
func validate(s spec) error {
	op := s.op
	switch {
	case op.OperationID == "":
		return &Error{Code: CodeInternal, Message: "operation id is required"}
	case op.DeploymentID == "":
		return &Error{Code: CodeInternal, Message: "deployment id is required"}
	case op.ServerID == "":
		return &Error{Code: CodeInternal, Message: "server id is required"}
	}
	switch s.typ {
	case opCreateRuntime:
		if s.body.ImageRef == "" || s.body.Container == "" || !validPort(s.body.Port) {
			return &Error{Code: CodeInternal, Message: "CREATE_RUNTIME requires imageRef, container and a valid port"}
		}
	case opConfigureNetwork:
		if s.body.Container == "" || s.body.Proxy == "" || s.body.Domain == "" || !validPort(s.body.Port) {
			return &Error{Code: CodeInternal, Message: "NETWORK requires container, proxy, domain and a valid port"}
		}
	case opStartRuntime, opStopRuntime, opRemoveRuntime:
		if s.body.Container == "" {
			return &Error{Code: CodeInternal, Message: s.typ + " requires a container"}
		}
	case opVerifyHealth:
		if s.body.Domain == "" || s.body.TimeoutSeconds < 1 || s.body.TimeoutSeconds > 600 {
			return &Error{Code: CodeInternal, Message: "VERIFY requires a domain and a timeout between 1 and 600 seconds"}
		}
	default:
		return &Error{Code: CodeInternal, Message: "unknown operation type " + s.typ}
	}
	return nil
}

func validPort(p int) bool { return p >= 1 && p <= 65535 }
