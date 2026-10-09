package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/digitaleflex/axiom/services/engine/internal/agentauth"
	"github.com/digitaleflex/axiom/services/engine/internal/agentkey"
	"github.com/digitaleflex/axiom/services/engine/internal/authz"
)

// AgentKeys issues the per-agent operation signing key (ADR-0008). The
// plaintext key is returned to the agent exactly once — at registration and at
// every rotation, in the "operationSigningKey" field (lowercase hex) — and
// must never be logged. *agentkey.Service implements it.
type AgentKeys interface {
	Issue(ctx context.Context, agentID string) (agentkey.Key, error)
}

// agentPublicPaths are agent-facing endpoints that authenticate with the
// agent's own bootstrap token or credential inside the handler, not with the
// user authenticator (#76/#77). Transport hardening (mTLS, request signing) is
// tracked by #88.
func isAgentPublicPath(path string) bool {
	return path == "/api/v1/agent/register" || path == "/api/v1/agent/rotate" || path == "/api/v1/agent/heartbeat"
}

// authenticateWithAgents lets the agent-facing endpoints bypass the user
// authenticator while every other route keeps the existing chain.
func (a *API) authenticateWithAgents(next http.Handler) http.Handler {
	authed := a.authenticate(next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isAgentPublicPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		authed.ServeHTTP(w, r)
	})
}

// --- bootstrap & registration -------------------------------------------------

// bootstrapServer issues a single-use bootstrap token for a pending server.
// User-authenticated. 404 unknown server, 409 non-pending server.
func (a *API) bootstrapServer(w http.ResponseWriter, r *http.Request) (err error) {
	target := r.PathValue("serverID")
	defer func() { a.audit(r, "server.bootstrap", target, "", err) }()
	if a.agents == nil {
		return errUnavailable
	}
	// authz defense-in-depth: the caller must own the server.
	if a.servers != nil {
		if s, loadErr := a.servers.Get(r.Context(), target); loadErr == nil {
			if ok, _ := a.authorize(r, authz.ActionServerWrite, authz.Resource{
				Type: "server", ID: s.ID, OwnerID: s.OwnerID,
			}); !ok {
				return errNotFound("server", target)
			}
		}
	}
	token, expires, err := a.agents.Issue(r.Context(), target)
	switch {
	case errors.Is(err, agentauth.ErrServerNotFound):
		return errNotFound("server", target)
	case errors.Is(err, agentauth.ErrServerNotPending):
		return newError(http.StatusConflict, CodeConflict, "server must be pending to issue a bootstrap token", map[string]any{"reason": "server_not_pending"})
	case err != nil:
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"token": token, "expiresAt": expires.UTC().Format(time.RFC3339)})
	return nil
}

// agentRegister redeems a bootstrap token (Authorization: Bearer <bootstrap>)
// and binds the agent identity to its server. Agent-facing: it does not use the
// user authenticator.
func (a *API) agentRegister(w http.ResponseWriter, r *http.Request) error {
	// The registration is refused before anything is consumed when the
	// operation signing key store is unwired: an agent registered without a
	// key could never accept a dispatched operation (ADR-0008).
	if a.agents == nil || a.agentKeys == nil {
		return errUnavailable
	}
	token, ok := bearerToken(r)
	if !ok {
		a.agentAuthFailure(r, "agent.register", "", "missing_credential")
		return agentUnauthorized("bootstrap credential is required")
	}
	var in struct {
		ServerID     string   `json:"serverId"`
		AgentVersion string   `json:"agentVersion"`
		Capabilities []string `json:"capabilities"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	fields := map[string]any{}
	if strings.TrimSpace(in.ServerID) == "" {
		fields["serverId"] = "required"
	}
	if strings.TrimSpace(in.AgentVersion) == "" {
		fields["agentVersion"] = "required"
	}
	if len(in.Capabilities) == 0 {
		fields["capabilities"] = "at least one capability is required"
	}
	if len(fields) > 0 {
		return errValidation("invalid registration request", map[string]any{"fields": fields})
	}

	reg, err := a.agents.Redeem(r.Context(), token, in.ServerID, in.AgentVersion, in.Capabilities)
	switch {
	case errors.Is(err, agentauth.ErrTokenInvalid), errors.Is(err, agentauth.ErrTokenUsed),
		errors.Is(err, agentauth.ErrTokenExpired), errors.Is(err, agentauth.ErrServerMismatch):
		a.agentAuthFailure(r, "agent.register", in.ServerID, tokenReason(err))
		return agentUnauthorized("bootstrap credential is invalid")
	case errors.Is(err, agentauth.ErrServerRevoked):
		a.agentAuthFailure(r, "agent.register", in.ServerID, "revoked")
		return agentUnauthorized("server is revoked")
	case errors.Is(err, agentauth.ErrServerNotFound):
		return errNotFound("server", in.ServerID)
	case err != nil:
		return err
	}
	// The operation signing key is issued with the credential and returned
	// exactly once. If issuance fails the registration answers an error: the
	// single-use bootstrap token is spent, and a fresh one recovers —
	// re-registration keeps the agent identity and reissues everything.
	key, err := a.agentKeys.Issue(r.Context(), reg.AgentID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, registrationPayload(reg, key))
	return nil
}

// --- rotation & status --------------------------------------------------------

// agentRotate exchanges a valid current credential for the next one. The old
// credential stays valid for the grace window. Agent-facing.
func (a *API) agentRotate(w http.ResponseWriter, r *http.Request) error {
	if a.agents == nil || a.agentKeys == nil {
		return errUnavailable
	}
	agentID := r.Header.Get("X-Agent-ID")
	credential, ok := bearerToken(r)
	if !ok || agentID == "" {
		a.agentAuthFailure(r, "agent.rotate", agentID, "missing_credential")
		return agentUnauthorized("agent credential is required")
	}
	reg, err := a.agents.Rotate(r.Context(), agentID, credential, r.Header.Get("X-Nonce"), r.Header.Get("X-Timestamp"))
	switch {
	case errors.Is(err, agentauth.ErrReplay):
		a.agentAuthFailure(r, "agent.rotate", agentID, "replay")
		return agentUnauthorizedReason("request was replayed", "replay")
	case errors.Is(err, agentauth.ErrClockSkew):
		a.agentAuthFailure(r, "agent.rotate", agentID, "clock_skew")
		return agentUnauthorizedReason("request timestamp is outside the allowed skew", "clock_skew")
	case errors.Is(err, agentauth.ErrRevoked):
		a.agentAuthFailure(r, "agent.rotate", agentID, "revoked")
		return agentUnauthorizedReason("credential is revoked", "revoked")
	case errors.Is(err, agentauth.ErrCredentialExpired):
		a.agentAuthFailure(r, "agent.rotate", agentID, "expired")
		return agentUnauthorizedReason("credential is expired", "expired")
	case errors.Is(err, agentauth.ErrCredentialInvalid), errors.Is(err, agentauth.ErrIdentityNotFound):
		a.agentAuthFailure(r, "agent.rotate", agentID, "invalid")
		return agentUnauthorized("credential is invalid")
	case err != nil:
		return err
	}
	// Rotation reissues the operation signing key the way it reissues the
	// credential (ADR-0008): a fresh key is returned exactly once.
	key, err := a.agentKeys.Issue(r.Context(), reg.AgentID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, registrationPayload(reg, key))
	return nil
}

// agentStatus distinguishes registered, revoked and unknown agents. The query
// accepts agentId and/or serverId. User-authenticated.
func (a *API) agentStatus(w http.ResponseWriter, r *http.Request) error {
	if a.agents == nil {
		return errUnavailable
	}
	agentID := r.URL.Query().Get("agentId")
	serverID := r.URL.Query().Get("serverId")
	if agentID == "" && serverID == "" {
		return errInvalid("agentId or serverId is required")
	}
	res, err := a.agents.Status(r.Context(), agentID, serverID)
	if err != nil {
		return err
	}
	out := map[string]any{"status": res.Status}
	if res.AgentID != "" {
		out["agentId"] = res.AgentID
	}
	if res.ServerID != "" {
		out["serverId"] = res.ServerID
	}
	if res.AgentVersion != "" {
		out["agentVersion"] = res.AgentVersion
	}
	if res.CredentialVersion > 0 {
		out["credentialVersion"] = res.CredentialVersion
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

// --- helpers ------------------------------------------------------------------

// registrationPayload renders a redeem/rotate response. It contains the
// plaintext credential and the plaintext operation signing key (lowercase
// hex, ADR-0008) and must never be logged: both are returned exactly once.
func registrationPayload(reg agentauth.Registration, operationSigningKey agentkey.Key) map[string]any {
	return map[string]any{
		"agentId":                  reg.AgentID,
		"serverId":                 reg.ServerID,
		"credential":               reg.Credential,
		"credentialVersion":        reg.CredentialVersion,
		"credentialExpiresAt":      reg.CredentialExpiresAt.UTC().Format(time.RFC3339),
		"operationSigningKey":      operationSigningKey.Hex(),
		"negotiated":               reg.Negotiated,
		"heartbeatIntervalSeconds": reg.HeartbeatIntervalSeconds,
	}
}

// bearerToken extracts a bearer token from the raw Authorization header. The
// agent-facing endpoints use this instead of the user authenticator (#76/#77).
func bearerToken(r *http.Request) (string, bool) {
	const prefix = "Bearer "
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, prefix) {
		return "", false
	}
	token := strings.TrimSpace(strings.TrimPrefix(h, prefix))
	if token == "" {
		return "", false
	}
	return token, true
}

func agentUnauthorized(message string) *Error {
	return newError(http.StatusUnauthorized, CodeUnauthorized, message, nil)
}

func agentUnauthorizedReason(message, reason string) *Error {
	return newError(http.StatusUnauthorized, CodeUnauthorized, message, map[string]any{"reason": reason})
}

func tokenReason(err error) string {
	switch {
	case errors.Is(err, agentauth.ErrTokenUsed):
		return "token_used"
	case errors.Is(err, agentauth.ErrTokenExpired):
		return "token_expired"
	case errors.Is(err, agentauth.ErrServerMismatch):
		return "server_mismatch"
	default:
		return "token_invalid"
	}
}

// agentAuthFailure emits a structured audit line for a failed agent
// authentication. It never includes the credential, token or request body.
func (a *API) agentAuthFailure(r *http.Request, action, agentID, reason string) {
	a.log.Warn("audit",
		"requestId", requestID(r.Context()),
		"action", action,
		"actor", "agent",
		"agentId", agentID,
		"result", "error",
		"reason", reason,
	)
}
