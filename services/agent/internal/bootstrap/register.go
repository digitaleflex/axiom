package bootstrap

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/digitaleflex/axiom/services/agent/internal/identity"
	"github.com/digitaleflex/axiom/services/agent/internal/protocol"
	"github.com/digitaleflex/axiom/services/agent/internal/security/auth"
	"github.com/digitaleflex/axiom/services/agent/internal/security/transport"
)

// registerTimeout bounds the registration exchange.
const registerTimeout = 30 * time.Second

// register performs the agent→Engine registration exchange (#76) when the agent
// has no issued identity or no usable credential yet, and persists both the
// identity and the credential.
//
// The bootstrap credential (config.Token) rides on the transport
// (Authorization), never in the body. When the agent is already registered and
// holds a credential, registration is skipped: re-registering on every start
// would rotate the credential for nothing.
func (a *App) register(ctx context.Context) error {
	if a.IdentityID.Registered && a.IdentityID.ServerID != "" && a.hasCredential() {
		return nil
	}
	if a.cfg.Token == "" {
		return errors.New("AXIOM_AGENT_TOKEN is required to register the agent")
	}

	// The bootstrap credential is stored so the signed client can present it;
	// the Engine answers with the long-lived credential that replaces it.
	bootstrap := auth.Credential{
		Token:     a.cfg.Token,
		Version:   1,
		ExpiresAt: time.Now().Add(registerTimeout).UTC(),
		AgentID:   a.IdentityID.AgentID,
		ServerID:  a.cfg.ServerID,
	}
	if err := a.Credentials.Save(bootstrap); err != nil {
		return fmt.Errorf("store bootstrap credential: %w", err)
	}

	report := a.Discoverer.Discover(ctx)
	req := protocol.RegistrationRequest{
		Envelope: protocol.Envelope{
			Protocol:  protocol.Version,
			MessageID: "msg_register_" + a.IdentityID.AgentID,
			SentAt:    time.Now().UTC(),
		},
		AgentIdentity: protocol.AgentIdentity{AgentID: a.IdentityID.AgentID, ServerID: a.cfg.ServerID},
		AgentVersion:  a.cfg.Version,
		Capabilities:  report.Capabilities(),
	}
	if errs := req.Validate(time.Now().UTC()); errs != nil {
		return fmt.Errorf("build registration request: %w", errs)
	}
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("encode registration request: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, registerTimeout)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		a.cfg.EngineURL+"/api/v1/agent/register", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build registration request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := a.Auth.Do(httpReq) // signs with the bootstrap credential
	if err != nil {
		return fmt.Errorf("send registration request: %w", err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated:
	case http.StatusUnauthorized, http.StatusForbidden:
		return errors.New("engine rejected the bootstrap credential (HTTP " + resp.Status + ")")
	default:
		return fmt.Errorf("engine returned %s to the registration request", resp.Status)
	}

	var out struct {
		AgentID                  string    `json:"agentId"`
		ServerID                 string    `json:"serverId"`
		Credential               string    `json:"credential"`
		CredentialVersion        int       `json:"credentialVersion"`
		CredentialExpiresAt      time.Time `json:"credentialExpiresAt"`
		Negotiated               int       `json:"negotiated"`
		HeartbeatIntervalSeconds int       `json:"heartbeatIntervalSeconds"`
	}
	raw, err := transport.ReadAll(io.LimitReader(resp.Body, transport.MaxResponseBytes), transport.MaxResponseBytes)
	if err != nil {
		return fmt.Errorf("read registration response: %w", err)
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return fmt.Errorf("decode registration response: %w", err)
	}
	if !identity.ValidID(out.AgentID) {
		return errors.New("engine issued a malformed agent id")
	}
	if out.ServerID != a.cfg.ServerID {
		return errors.New("engine bound the agent to another server")
	}
	if !protocol.Compatible(protocol.MinVersion, out.Negotiated) {
		return fmt.Errorf("engine negotiated an unsupported protocol version %d", out.Negotiated)
	}
	if out.Credential == "" {
		return errors.New("engine issued no credential")
	}

	issued := auth.Credential{
		Token:     out.Credential,
		Version:   out.CredentialVersion,
		ExpiresAt: out.CredentialExpiresAt,
		AgentID:   out.AgentID,
		ServerID:  out.ServerID,
	}
	if err := a.Credentials.Save(issued); err != nil {
		return fmt.Errorf("persist issued credential: %w", err)
	}
	registered := identity.Identity{AgentID: out.AgentID, ServerID: out.ServerID, Registered: true}
	if err := a.Identity.Save(registered); err != nil {
		return fmt.Errorf("persist registered identity: %w", err)
	}
	a.IdentityID = registered

	// The heartbeat loop needs an issued identity; start it now that the agent
	// has one, using the interval the Engine negotiated.
	if a.Heartbeat == nil {
		interval := a.cfg.HeartbeatInterval
		if out.HeartbeatIntervalSeconds > 0 {
			interval = time.Duration(out.HeartbeatIntervalSeconds) * time.Second
		}
		loop := newHeartbeat(a.cfg, protocol.AgentIdentity{AgentID: out.AgentID, ServerID: out.ServerID}, a.Auth, a.Discoverer, a.log)
		loop.Interval = interval
		a.Heartbeat = loop
	}
	a.log.Info("agent registered", "agentId", out.AgentID, "serverId", out.ServerID,
		"credentialVersion", out.CredentialVersion, "negotiated", out.Negotiated,
		"heartbeatIntervalSeconds", out.HeartbeatIntervalSeconds)
	return nil
}

// hasCredential reports whether the agent holds a usable credential in memory.
func (a *App) hasCredential() bool {
	cred, ok := a.Credentials.Current()
	return ok && cred.Token != ""
}
