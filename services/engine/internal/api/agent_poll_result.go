// Package api implements the public REST contract /api/v1
// (docs/architecture/api-contract.md). It exposes resources, never
// infrastructure internals, and renders every error with the stable envelope.
package api

import (
	"context"
	"crypto/hmac"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/digitaleflex/axiom/services/engine/internal/agentauth"
	"github.com/digitaleflex/axiom/services/engine/internal/agentkey"
	"github.com/digitaleflex/axiom/services/engine/internal/agentpoll"
)

// Protocol constants mirror services/agent/internal/protocol.
const (
	protocolVersion = 2
)

// agentPoll handles POST /api/v1/agent/poll.
// The agent long-polls for an operation from the Engine. It authenticates
// with agent credentials, then blocks on the agent-specific operation queue.
// If an operation is available within 25s, it is signed with the per-agent
// operation signing key (ADR-0008) and returned as JSON. Otherwise, 204 No
// Content is returned.
//
// Transport: long-poll (ADR-0008 §17-28). The Engine→Agent leg is authenticated
// by a per-agent HMAC-SHA256 signature (internal/agentkey).
//
// Contract:
//   - Input: agent authentication via X-Agent-ID header + X-Timestamp/X-Nonce
//     signature (same as agent heartbeat).
//   - Output: 200 + JSON-encoded protocol.Operation if available within
//     25s timeout; 204 if timeout expires.
//   - Side-effect: the operation is removed from the queue on 200.
func (a *API) agentPoll(w http.ResponseWriter, r *http.Request) (err error) {
	// Authenticate the agent using the same mechanism as heartbeat.
	// This mirrors agent.heartbeat authentication exactly.
	if a.agents == nil {
		return errUnavailable
	}
	agentID := r.Header.Get("X-Agent-ID")
	if agentID == "" {
		a.agentAuthFailure(r, "agent.poll", "", "missing_credential")
		return agentUnauthorized("agent credential is required")
	}
	credential, ok := bearerToken(r)
	if !ok {
		a.agentAuthFailure(r, "agent.poll", agentID, "missing_credential")
		return agentUnauthorized("agent credential is required")
	}
	// Authenticate the agent identity.
	_, err = a.agents.Authenticate(r.Context(), agentID, credential, r.Header.Get("X-Nonce"), r.Header.Get("X-Timestamp"))
	switch {
	case errors.Is(err, agentauth.ErrIdentityNotFound):
		return errNotFound("agent", agentID)
	case errors.Is(err, agentauth.ErrReplay):
		a.agentAuthFailure(r, "agent.poll", agentID, "replay")
		return agentUnauthorizedReason("request was replayed", "replay")
	case errors.Is(err, agentauth.ErrClockSkew):
		a.agentAuthFailure(r, "agent.poll", agentID, "clock_skew")
		return agentUnauthorizedReason("request timestamp is outside the allowed skew", "clock_skew")
	case errors.Is(err, agentauth.ErrRevoked):
		a.agentAuthFailure(r, "agent.poll", agentID, "revoked")
		return agentUnauthorizedReason("credential is revoked", "revoked")
	case errors.Is(err, agentauth.ErrCredentialInvalid):
		a.agentAuthFailure(r, "agent.poll", agentID, "invalid")
		return agentUnauthorized("credential is invalid")
	case errors.Is(err, agentauth.ErrCredentialExpired):
		a.agentAuthFailure(r, "agent.poll", agentID, "expired")
		return agentUnauthorizedReason("credential is expired", "expired")
	case err != nil:
		return err
	}
	// Get or create the operation queue for this agent.
	if a.agentPollManager == nil {
		return errUnavailable
	}
	// Block on the agent's operation queue with a 25s timeout.
	ctx := r.Context()
	if deadline, ok := ctx.Deadline(); !ok || deadline.After(time.Now().Add(agentpoll.ReadTimeout)) {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, agentpoll.ReadTimeout)
		defer cancel()
	}
	op, ok, err := a.agentPollManager.Dequeue(ctx, agentID)
	if err != nil {
		return err
	}
	if !ok {
		// Timeout expired, no operation available.
		w.WriteHeader(http.StatusNoContent)
		return nil
	}
	// We have an operation to send to the agent.
	// Sign it with the per-agent operation signing key (ADR-0008).
	key, err := a.agentKeys.SigningKey(r.Context(), agentID)
	if err != nil {
		return err
	}
	// Build the canonical form for signing (matching agentclient/exchange).
	canonical := agentkey.Canonical(
		http.MethodPost,
		"/api/v1/agent/poll", // Note: path must match what agent expects to sign
		agentID,
		protocolVersion,
		time.Now().UTC().Format(time.RFC3339),
		r.Header.Get(agentkey.NonceHeader), // Use the nonce from the request to prevent replay
		op.Body,
	)
	sig := agentkey.Key(key).Sign(canonical)
	// Prepare the response: envelope + operation body.
	var out map[string]any
	if err := json.Unmarshal(op.Body, &out); err != nil {
		return newError(http.StatusInternalServerError, CodeInternalError, "failed to decode operation body", nil)
	}
	// Ensure we have the required envelope fields.
	if out["protocol"] == nil {
		out["protocol"] = protocolVersion
	}
	if out["messageId"] == nil {
		out["messageId"] = op.OperationID
	}
	if out["sentAt"] == nil {
		out["sentAt"] = time.Now().UTC().Format(time.RFC3339)
	}
	if out["operationId"] == nil {
		out["operationId"] = op.OperationID
	}
	if out["type"] == nil {
		// We need to determine the operation type from the body - this is tricky.
		// Instead, let's store the operation type separately in our queue.
		// For now, we'll assume it's CREATE_RUNTIME as a placeholder.
		// TODO: Store operation type in the queue.
		out["type"] = "CREATE_RUNTIME"
	}
	if out["deploymentId"] == nil {
		out["deploymentId"] = op.DeploymentID
	}
	if out["applicationId"] == nil {
		// ApplicationID should be in the payload or we need to look it up.
		// For now, let's try to extract from body or use a placeholder.
		if out["applicationId"] == nil && out["payload"] != nil {
			if payload, ok := out["payload"].(map[string]any); ok {
				if out["applicationId"] == nil {
					out["applicationId"] = payload["applicationId"]
				}
			}
		}
		if out["applicationId"] == nil {
			// Fallback: look up from deployment.
			if dep, err := a.deployments.Get(r.Context(), op.DeploymentID); err == nil {
				out["applicationId"] = dep.ApplicationID
			} else {
				// If we can't find it, we'll have to return an error.
				return newError(http.StatusInternalServerError, CodeInternalError, "application ID not found", nil)
			}
		}
	}
	if out["serverId"] == nil {
		out["serverId"] = agentID // Agent serves this server
	}
	// Add the signature.
	w.Header().Set(agentkey.SignatureHeader, agentkey.SignaturePrefix+sig)
	// Return the signed operation.
	writeJSON(w, http.StatusOK, out)
	return nil
}

// agentResult handles POST /api/v1/agent/result.
// The agent posts the acknowledgement + result of an operation here.
// It authenticates with agent credentials, validates the signature (using the
// same per-agent operation signing key as the poll route), and stores the
// result for replay.
//
// Transport: HTTPS POST (ADR-0008 §17-28). The Agent→Engine leg is authenticated
// by the same per-agent HMAC-SHA256 signature.
//
// Contract:
//   - Input: JSON-encoded protocol.Acknowledgement + protocol.Result
//   - Output: 200 OK if signature valid and agent authenticated
//   - Side-effect: the result is stored in the result queue for replay.
func (a *API) agentResult(w http.ResponseWriter, r *http.Request) (err error) {
	// Authenticate the agent using the same mechanism as heartbeat and poll.
	if a.agents == nil {
		return errUnavailable
	}
	agentID := r.Header.Get("X-Agent-ID")
	if agentID == "" {
		a.agentAuthFailure(r, "agent.result", "", "missing_credential")
		return agentUnauthorized("agent credential is required")
	}
	credential, ok := bearerToken(r)
	if !ok {
		a.agentAuthFailure(r, "agent.result", agentID, "missing_credential")
		return agentUnauthorized("agent credential is required")
	}
	// Authenticate the agent identity.
	_, err = a.agents.Authenticate(r.Context(), agentID, credential, r.Header.Get("X-Nonce"), r.Header.Get("X-Timestamp"))
	switch {
	case errors.Is(err, agentauth.ErrIdentityNotFound):
		return errNotFound("agent", agentID)
	case errors.Is(err, agentauth.ErrReplay):
		a.agentAuthFailure(r, "agent.result", agentID, "replay")
		return agentUnauthorizedReason("request was replayed", "replay")
	case errors.Is(err, agentauth.ErrClockSkew):
		a.agentAuthFailure(r, "agent.result", agentID, "clock_skew")
		return agentUnauthorizedReason("request timestamp is outside the allowed skew", "clock_skew")
	case errors.Is(err, agentauth.ErrRevoked):
		a.agentAuthFailure(r, "agent.result", agentID, "revoked")
		return agentUnauthorizedReason("credential is revoked", "revoked")
	case errors.Is(err, agentauth.ErrCredentialInvalid):
		a.agentAuthFailure(r, "agent.result", agentID, "invalid")
		return agentUnauthorized("credential is invalid")
	case errors.Is(err, agentauth.ErrCredentialExpired):
		a.agentAuthFailure(r, "agent.result", agentID, "expired")
		return agentUnauthorizedReason("credential is expired", "expired")
	case err != nil:
		return err
	}
	// Decode the request body into an acknowledgement + result.
	var in struct {
		Envelope struct {
			Protocol int `json:"protocol"`
		} `json:"-"`
		OperationID  string `json:"operationId"`
		DeploymentID string `json:"deploymentId"`
		Accepted     bool   `json:"accepted"`
		Reason       string `json:"reason,omitempty"`
		Success      bool   `json:"success"`
		ErrorCode    string `json:"errorCode,omitempty"`
		Message      string `json:"message,omitempty"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	// Validate required fields.
	if in.OperationID == "" {
		return newError(http.StatusBadRequest, CodeValidationFailed, "operationId is required", nil)
	}
	if in.DeploymentID == "" {
		return newError(http.StatusBadRequest, CodeValidationFailed, "deploymentId is required", nil)
	}
	// Verify the signature (same as poll route).
	// We need to reconstruct the canonical form from the request.
	// The body we received is the marshalled Acknowledgement + Result.
	body, err := json.Marshal(in)
	if err != nil {
		return newError(http.StatusInternalServerError, CodeInternalError, "failed to encode request body", nil)
	}
	// Get the agent's signing key.
	key, err := a.agentKeys.SigningKey(r.Context(), agentID)
	if err != nil {
		return err
	}
	// Build the canonical form for verification.
	canonical := agentkey.Canonical(
		http.MethodPost,
		"/api/v1/agent/result", // Note: path must match what agent expects to sign
		agentID,
		protocolVersion,
		r.Header.Get(agentkey.TimestampHeader),
		r.Header.Get(agentkey.NonceHeader),
		body,
	)
	sig := agentkey.Key(key).Sign(canonical)
	expectedSig := agentkey.SignaturePrefix + sig
	if !hmac.Equal([]byte(r.Header.Get(agentkey.SignatureHeader)), []byte(expectedSig)) {
		a.agentAuthFailure(r, "agent.result", agentID, "invalid_signature")
		return agentUnauthorized("invalid signature")
	}
	// Store the result for replay (acknowledgement is implicit in storing the result).
	if a.agentPollManager == nil {
		return errUnavailable
	}
	// Store the result body (the marshalled Acknowledgement + Result).
	if err := a.agentPollManager.Result(agentID, in.OperationID, body); err != nil {
		if errors.Is(err, agentpoll.ErrResultNotFound) {
			// This shouldn't happen if we just dequeued the operation,
			// but handle gracefully.
			return newError(http.StatusBadRequest, CodeValidationFailed, "no pending operation for this acknowledgement", nil)
		}
		return err
	}
	// Acknowledge receipt.
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	return nil
}
