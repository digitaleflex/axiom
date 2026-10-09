package bootstrap

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/digitaleflex/axiom/services/agent/internal/protocol"
	"github.com/digitaleflex/axiom/services/agent/internal/security/transport"
)

// MaxOperationBytes bounds an inbound operation body. It matches the Engine
// client's MaxRequestBytes; a larger body is rejected before any decoding.
const MaxOperationBytes = transport.MaxRequestBytes

// ErrUnauthenticated is the sentinel every inbound authenticator returns to
// reject an operation. The Engine maps HTTP 401 to AGENT_OPERATION_REJECTED.
var ErrUnauthenticated = errors.New("operation is not authenticated")

// InboundAuthenticator decides whether an inbound request may reach the
// dispatcher.
//
// #77 / ADR-0008 (C6/C8): the Engine→Agent leg is authenticated by an HMAC
// operation signing key per agent, handed over in clear exactly once at
// registration (operationkey). The Engine signs every operation it posts
// (X-Axiom-Signature / X-Timestamp / X-Nonce) and the agent verifies the
// signature over the exact bytes received before dispatching anything. The
// seam exists so the composition root can mount either the real verifier or
// the fail-closed refuseInbound without touching the listener or the
// dispatcher.
//
// Verification is split in two steps on purpose:
//
//   - Authenticate runs BEFORE the body is read and inspects headers only, so
//     an unauthenticated request can never make the agent allocate a body
//     buffer (401 before 413);
//   - VerifyOperation runs AFTER the strict decode and validation, when the
//     raw bytes and the decoded operation are both available, and checks the
//     HMAC over the canonical form (401 before Dispatch).
type InboundAuthenticator interface {
	// Authenticate returns nil when the request carries well-formed signature
	// headers, or ErrUnauthenticated (or an error wrapping it) when it does
	// not. It reads only r's headers: never r.Body.
	Authenticate(r *http.Request) error
	// VerifyOperation returns nil when the operation's HMAC signature is valid
	// for the raw bytes received, or ErrUnauthenticated (or an error wrapping
	// it) when it is not. It is the last gate before Dispatch.
	VerifyOperation(r *http.Request, op protocol.Operation, raw []byte) error
}

// refuseInbound is the production authenticator when no operation signing key
// is registered: it authenticates nothing. It implements both steps, so an
// agent without a key stays closed exactly like it did before ADR-0008 (401
// before the body is ever read).
type refuseInbound struct{}

func (refuseInbound) Authenticate(*http.Request) error { return ErrUnauthenticated }

func (refuseInbound) VerifyOperation(*http.Request, protocol.Operation, []byte) error {
	return ErrUnauthenticated
}

// dispatchTarget is the slice of the composition root the listener needs. It
// keeps the listener independently testable.
type dispatchTarget interface {
	Dispatch(ctx context.Context, op protocol.Operation) (protocol.Acknowledgement, protocol.Result)
	// AgentIdentity is the identity the inbound operation must bind to. It is
	// known independently of the request: never taken from the body.
	AgentIdentity() protocol.AgentIdentity
}

// operationsListener is the agent's inbound HTTP surface: the only way the
// Engine can hand work to this agent (docs/architecture/agent-protocol.md §6).
type operationsListener struct {
	path   string
	auth   InboundAuthenticator
	target dispatchTarget
	log    *slog.Logger

	mu       sync.RWMutex
	draining bool
}

var _ http.Handler = (*operationsListener)(nil)

// newOperationsListener builds the listener for one operation path.
func newOperationsListener(path string, auth InboundAuthenticator, target dispatchTarget) *operationsListener {
	return &operationsListener{path: path, auth: auth, target: target, log: slog.Default()}
}

// Handler returns the http.Handler serving the operation endpoint. Every other
// path answers 404: the agent exposes no other endpoint.
func (l *operationsListener) Handler() http.Handler { return l }

// SetDraining makes the listener refuse new operations while the process shuts
// down. In-flight operations still finish.
func (l *operationsListener) SetDraining() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.draining = true
}

func (l *operationsListener) isDraining() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.draining
}

// ServeHTTP implements http.Handler.
func (l *operationsListener) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != l.path {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeError(w, http.StatusMethodNotAllowed, protocol.CodeInvalidMessage, "operations are posted")
		return
	}
	if l.isDraining() {
		writeError(w, http.StatusServiceUnavailable, protocol.CodeInternal, "agent is shutting down")
		return
	}
	// Authenticate before reading the body: an unauthenticated request must not
	// be able to make the agent allocate anything.
	if err := l.auth.Authenticate(r); err != nil {
		l.log.Warn("operation refused: unauthenticated",
			"path", r.URL.Path, "remote", r.RemoteAddr)
		writeError(w, http.StatusUnauthorized, protocol.CodeUnauthorized, "operation refused")
		return
	}

	raw, err := transport.ReadAll(io.LimitReader(r.Body, MaxOperationBytes+1), MaxOperationBytes)
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, protocol.CodeInvalidMessage, "operation body is too large")
		return
	}

	var op protocol.Operation
	if err := decodeOperation(raw, &op); err != nil {
		writeError(w, http.StatusBadRequest, protocol.CodeInvalidMessage, "operation body is not a valid protocol operation")
		return
	}

	// The envelope, the closed operation set, the operation ID, the deployment
	// ID and the server-identity binding are validated before the signature is
	// even consulted; a malformed operation is a 400, never a dispatch. The
	// dispatcher re-checks all of it before anything executes.
	if err := op.Validate(time.Now().UTC(), l.target.AgentIdentity()); err != nil {
		code := protocol.CodeInvalidMessage
		switch {
		case errors.Is(err, protocol.ErrStale):
			code = protocol.CodeStaleMessage
		case errors.Is(err, protocol.ErrIncompleteScope):
			code = protocol.CodeIncompleteScope
		}
		writeError(w, http.StatusBadRequest, code, "operation body is not a valid protocol operation")
		return
	}

	// Signature over the exact bytes received (ADR-0008): the HMAC is checked
	// last, right before Dispatch, so no unsigned or tampered operation can
	// reach the dispatcher even when it decodes and validates cleanly.
	if err := l.auth.VerifyOperation(r, op, raw); err != nil {
		l.log.Warn("operation refused: invalid signature",
			"path", r.URL.Path, "remote", r.RemoteAddr)
		writeError(w, http.StatusUnauthorized, protocol.CodeUnauthorized, "operation refused")
		return
	}

	// The dispatcher validates the envelope, the closed operation set, the
	// operation ID, the deployment ID and the server-identity binding before
	// anything executes; rejections come back as an Acknowledgement with
	// Accepted=false and a stable reason.
	ack, result := l.target.Dispatch(r.Context(), op)
	if !ack.Accepted {
		writeJSON(w, http.StatusOK, ack)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// decodeOperation decodes strictly: an unknown field is a malformed message,
// never a silently ignored instruction.
func decodeOperation(raw []byte, op *protocol.Operation) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(op); err != nil {
		return err
	}
	if dec.More() {
		return errors.New("operation body carries trailing content")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError answers the protocol error envelope with a stable code and a
// message that carries no internal detail.
func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, protocol.Error{
		Envelope: protocol.Envelope{Protocol: protocol.Version, MessageID: "msg_http_error", SentAt: time.Now().UTC()},
		Code:     code,
		Message:  message,
	})
}
