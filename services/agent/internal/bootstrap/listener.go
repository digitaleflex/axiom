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
// #77 / ADR-0008: the Engine→Agent credential direction does not exist yet.
// agentauth (#77) owns the agent→Engine direction only and stores credential
// hashes, so the Engine holds no plaintext it could present to an agent. The
// composition root therefore injects refuseInbound, which authenticates
// nothing: every operation is rejected before it reaches the dispatcher. This
// is the same fail-closed stance the Engine takes with AGENT_NO_CREDENTIAL
// (services/engine/internal/bootstrap/bootstrap.go, unavailableCredential).
//
// The seam exists so #77 can inject a real authenticator without touching the
// listener or the dispatcher. Implementing one is an architecture decision
// (mTLS vs token-based agent credentials, ADR-0008) and is deliberately out of
// scope here.
type InboundAuthenticator interface {
	// Authenticate returns nil when the request may be dispatched, or
	// ErrUnauthenticated (or an error wrapping it) when it may not.
	Authenticate(r *http.Request) error
}

// refuseInbound is the production authenticator: it authenticates nothing.
type refuseInbound struct{}

func (refuseInbound) Authenticate(*http.Request) error {
	return ErrUnauthenticated
}

// dispatchTarget is the slice of the composition root the listener needs. It
// keeps the listener independently testable.
type dispatchTarget interface {
	Dispatch(ctx context.Context, op protocol.Operation) (protocol.Acknowledgement, protocol.Result)
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
