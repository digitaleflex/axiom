package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/digitaleflex/axiom/services/engine/internal/agentauth"
	"github.com/digitaleflex/axiom/services/engine/internal/server"
)

// heartbeatRequest mirrors protocol.Heartbeat (services/agent/internal/protocol).
// The two Go modules cannot share the package (ownership boundary), so the
// wire format is duplicated here with identical field names by design
// (docs/architecture/agent-protocol.md §5 conformance contract).
type heartbeatRequest struct {
	Protocol     int       `json:"protocol"`
	MessageID    string    `json:"messageId"`
	SentAt       time.Time `json:"sentAt"`
	AgentID      string    `json:"agentId"`
	ServerID     string    `json:"serverId"`
	Status       string    `json:"status"`
	Capabilities []string  `json:"capabilities"`
	CPUCount     int       `json:"cpuCount"`
	MemoryMB     int       `json:"memoryMb"`
	DiskFreeMB   int       `json:"diskFreeMb"`
}

// agentHeartbeat ingests one signed heartbeat (#78). It authenticates the
// agent credential (with timestamp/nonce replay checks via agentauth),
// validates the payload, persists liveness with last-write-wins semantics
// (duplicate or out-of-order heartbeats are idempotent), and answers with the
// effective server status.
//
// Status mapping: READY/DEGRADED/OFFLINE → the matching server statuses.
// Offline-after-timeout is NOT computed here: it is enforced at read time
// via server.EffectiveStatusAt (StaleAfter 5m), so the response reflects the
// fresh write and every subsequent read derives staleness itself.
func (a *API) agentHeartbeat(w http.ResponseWriter, r *http.Request) (err error) {
	target := r.Header.Get("X-Agent-ID")
	defer func() { a.audit(r, "agent.heartbeat", target, "", err) }()
	if a.agents == nil {
		return errUnavailable
	}
	agentID := r.Header.Get("X-Agent-ID")
	credential, ok := bearerToken(r)
	if !ok || agentID == "" {
		a.agentAuthFailure(r, "agent.heartbeat", agentID, "missing_credential")
		return agentUnauthorized("agent credential is required")
	}
	id, err := a.agents.Authenticate(r.Context(), agentID, credential, r.Header.Get("X-Nonce"), r.Header.Get("X-Timestamp"))
	switch {
	case errors.Is(err, agentauth.ErrIdentityNotFound):
		// Unknown agent: the credential does not exist (or was never issued).
		return errNotFound("agent", agentID)
	case errors.Is(err, agentauth.ErrReplay):
		a.agentAuthFailure(r, "agent.heartbeat", agentID, "replay")
		return agentUnauthorizedReason("request was replayed", "replay")
	case errors.Is(err, agentauth.ErrClockSkew):
		a.agentAuthFailure(r, "agent.heartbeat", agentID, "clock_skew")
		return agentUnauthorizedReason("request timestamp is outside the allowed skew", "clock_skew")
	case errors.Is(err, agentauth.ErrRevoked):
		a.agentAuthFailure(r, "agent.heartbeat", agentID, "revoked")
		return agentUnauthorizedReason("credential is revoked", "revoked")
	case errors.Is(err, agentauth.ErrCredentialInvalid):
		a.agentAuthFailure(r, "agent.heartbeat", agentID, "invalid")
		return agentUnauthorized("credential is invalid")
	case errors.Is(err, agentauth.ErrCredentialExpired):
		a.agentAuthFailure(r, "agent.heartbeat", agentID, "expired")
		return agentUnauthorizedReason("credential is expired", "expired")
	case err != nil:
		return err
	}

	var in heartbeatRequest
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	if err := validateHeartbeatEnvelope(in); err != nil {
		return err
	}
	// Server identity is bound to the authenticated agent identity.
	if in.AgentID != id.AgentID || in.ServerID != id.ServerID {
		return errInvalid("heartbeat identity does not match the authenticated agent")
	}
	var status server.Status
	switch in.Status {
	case "READY":
		status = server.StatusReady
	case "DEGRADED":
		status = server.StatusDegraded
	case "OFFLINE":
		status = server.StatusOffline
	default:
		return errInvalid("status must be READY, DEGRADED or OFFLINE")
	}
	if a.servers == nil {
		return errUnavailable
	}

	now := time.Now().UTC()
	capabilities := make([]server.Capability, 0, len(in.Capabilities))
	for _, c := range in.Capabilities {
		capabilities = append(capabilities, server.Capability(c))
	}
	// Last-write-wins: a duplicate or out-of-order heartbeat simply overwrites
	// the previous liveness row; no dedupe or ordering state is kept.
	if err := a.servers.UpdateHealth(r.Context(), id.ServerID, server.Health{
		Status:       status,
		AgentVersion: id.AgentVersion,
		Capabilities: capabilities,
		CPUCount:     in.CPUCount,
		MemoryMB:     in.MemoryMB,
		DiskFreeMB:   in.DiskFreeMB,
		LastSeenAt:   now.Format(time.RFC3339),
	}); err != nil {
		return err
	}

	// Read back the effective status: staleness (offline after timeout) is
	// derived at read time via server.EffectiveStatusAt, never stored.
	rec, err := a.servers.Get(r.Context(), id.ServerID)
	if errors.Is(err, server.ErrNotFound) {
		return errNotFound("server", id.ServerID)
	}
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":           true,
		"serverStatus": strings.ToUpper(string(server.EffectiveStatusAt(rec, now))),
	})
	return nil
}

// validateHeartbeatEnvelope enforces the protocol envelope rules the agent
// must satisfy (docs/architecture/agent-protocol.md §3): supported version,
// well-formed message ID and a fresh sentAt.
func validateHeartbeatEnvelope(in heartbeatRequest) error {
	if in.Protocol < 1 || in.Protocol > agentauth.ProtocolVersion {
		return errInvalid("unsupported protocol version")
	}
	if len(in.MessageID) == 0 || len(in.MessageID) > 128 {
		return errInvalid("messageId must be 1-128 characters")
	}
	for _, c := range in.MessageID {
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c == '.' || c == '-') {
			return errInvalid("messageId contains invalid characters")
		}
	}
	now := time.Now().UTC()
	if in.SentAt.IsZero() || in.SentAt.After(now.Add(agentauth.MaxClockSkew)) || now.Sub(in.SentAt) > 24*time.Hour {
		return errInvalid("sentAt is outside the allowed freshness window")
	}
	return nil
}
