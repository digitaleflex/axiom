// Package longpoll implements the agent's long-poll client for the Engine
// (ADR-0008, C10/C11). The agent initiates every request: it POSTs to
// /api/v1/agent/poll and waits up to ~25s for the Engine to queue an
// operation; when one is queued the Engine answers 200 with a signed
// protocol.Operation. The agent then validates the signature, runs the
// operation through the dispatcher, and reports the outcome through
// POST /api/v1/agent/result.
//
// Wire contract (identical to the inbound Engine->Agent leg):
//
//  1. Each request carries Authorization, X-Agent-ID, X-Timestamp (RFC3339),
//     X-Nonce (random hex) and X-Axiom-Signature.
//  2. The signature is computed with the shared operation signing key
//     (operationkey.Store) over the canonical form AXIOM-HMAC-V1 and is
//     verified by the Engine with the same key. The client therefore signs
//     using operationkey.Sign and, when it receives a signed operation back
//     from the poll, verifies it using operationkey.Verify.
//
// See operationkey package for the canonical form:
//
//	canonical = AXIOM-HMAC-V1 + "\n" + METHOD + "\n" + PATH + "\n" +
//	            AGENT_ID + "\n" + PROTOCOL_VERSION + "\n" + TIMESTAMP +
//	            "\n" + NONCE + "\n" + sha256hex(BODY)
//
// Signatures are v1=<lowercase-hex-hmac-sha256>.
//
// Backoff: the client uses an exponential backoff (up to 60s) for transient
// transport failures (timeouts, connection errors, 4xx/5xx). A 204 poll
// ("nothing queued") is *not* an error: the client retries after a short
// 1s delay to avoid hammering the Engine while the hold may still be active.
package longpoll

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
	"net/http"
	"time"

	"github.com/digitaleflex/axiom/services/agent/internal/dispatcher"
	"github.com/digitaleflex/axiom/services/agent/internal/protocol"
	"github.com/digitaleflex/axiom/services/agent/internal/security/auth"
	"github.com/digitaleflex/axiom/services/agent/internal/security/operationkey"
)

const (
	// LongPollTimeout bounds the long-poll request. The Engine holds the
	// request for at most 25s (ADR-0008): the client waits slightly beyond 25s
	// so that slow-but-fine network delay is not mistaken for an empty poll.
	LongPollTimeout = 25*time.Second + 3*time.Second

	// minBackoff / maxBackoff bound the exponential backoff for transient
	// transport failures.
	minBackoff = 1 * time.Second
	maxBackoff = 60 * time.Second

	// EmptyPollDelay is the short pause after a 204 poll ("nothing queued")
	// before the next poll. A 204 is a normal condition, not a failure, so no
	// backoff is applied; the delay exists only to avoid flooding the Engine
	// while the hold may still be active.
	EmptyPollDelay = 1 * time.Second
)

// ErrPollEmpty signals that the Engine answered 204 with nothing queued. It is
// a normal condition; the caller must retry after EmptyPollDelay, not backoff.
var ErrPollEmpty = errors.New("longpoll: 204 poll, nothing queued")

// AckResult wraps the Acknowledgement and Result of a dispatched operation
// into the single body sent by POST /api/v1/agent/result. The two protocol
// envelopes are preserved separately so the Engine can correlate the ack and
// the result without re-parsing.
type AckResult struct {
	Acknowledgement protocol.Acknowledgement `json:"acknowledgement"`
	Result          protocol.Result          `json:"result"`
}

// LongPollClient implements the agent's long-poll client.
type LongPollClient struct {
	baseURL      string
	http         *http.Client
	auth         *auth.Client        // Bearer credential + nonce management
	operationKey *operationkey.Store // shared HMAC key, signs and verifies the wire
	agentID      string
	serverID     string
	dispatcher   *dispatcher.Dispatcher
	transportLog *slog.Logger

	// injectables for tests
	timeFunc  func() time.Time
	nonceFunc func() string
}

// NewLongPollClient builds the long-poll client for the Engine at baseURL.
// The agent must already hold a Bearer credential in auth and the operation
// signing key in operationKey (handed over at registration/rotation).
//
// agentID and serverID come from the local identity (identity.Store) and are
// needed to bind inbound operations and to sign outbound requests.
func NewLongPollClient(baseURL string, auth *auth.Client, operationKey *operationkey.Store,
	agentID, serverID string, dispatcher *dispatcher.Dispatcher, transportLog *slog.Logger) *LongPollClient {
	return &LongPollClient{
		baseURL:      baseURL,
		http:         &http.Client{Timeout: LongPollTimeout},
		auth:         auth,
		operationKey: operationKey,
		agentID:      agentID,
		serverID:     serverID,
		dispatcher:   dispatcher,
		transportLog: transportLog,
		timeFunc:     time.Now,
		nonceFunc:    randomNonce,
	}
}

// now returns the current UTC time (or the test override).
func (c *LongPollClient) currentTime() time.Time {
	if c.timeFunc != nil {
		return c.timeFunc()
	}
	return time.Now().UTC()
}

// randomNonce returns a 16-byte hex nonce for the X-Nonce header.
func randomNonce() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("longpoll: entropy source unavailable: " + err.Error())
	}
	return hex.EncodeToString(b[:])
}

// runPoll performs exactly one POST /api/v1/agent/poll: it builds the signed
// request, waits for the response and handles the three outcomes:
//
//   - 204: nothing queued -> ErrPollEmpty (retry after EmptyPollDelay).
//   - 200 + signed operation: verifies the signature, then delegates to
//     Dispatch, which runs the operation and returns the Acknowledgement and
//     Result (the result is also reported via POST /api/v1/agent/result).
//   - anything else / network error: returned as an error and the caller
//     applies exponential backoff.
//
// The request body is empty JSON; the signature is computed over that exact
// body.
func (c *LongPollClient) runPoll(ctx context.Context) (*protocol.Operation, error) {
	key, ok := c.operationKey.Current()
	if !ok {
		return nil, errors.New("longpoll: no operation signing key; refuse to poll")
	}

	// Build the empty poll body and sign it.
	body := json.RawMessage("{}")
	canonical := operationkey.Canonical(
		http.MethodPost,
		"/api/v1/agent/poll",
		c.agentID,
		protocol.Version,
		c.currentTime().Format(time.RFC3339),
		c.nonceFunc(),
		body,
	)
	signature := key.Sign(canonical)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/agent/poll", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("poll request: %w", err)
	}
	// Authenticate with the Bearer credential (same mechanism as the heartbeat).
	if err := c.auth.Sign(req); err != nil {
		return nil, fmt.Errorf("poll sign: %w", err)
	}
	// Operation signature over the body, on the same header the inbound
	// authenticator inspects.
	req.Header.Set(operationkey.SignatureHeader, operationkey.SignaturePrefix+signature)
	req.Header.Set(operationkey.TimestampHeader, req.Header.Get("X-Timestamp"))
	req.Header.Set(operationkey.NonceHeader, req.Header.Get("X-Nonce"))

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("poll request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("poll read body: %w", err)
	}

	switch resp.StatusCode {
	case http.StatusNoContent:
		return nil, ErrPollEmpty
	case http.StatusOK:
		// Decode the operation. Strict decode fails on any unexpected field,
		// so malformed envelopes are rejected immediately.
		var op protocol.Operation
		if err := json.Unmarshal(respBody, &op); err != nil {
			return nil, fmt.Errorf("poll decode operation: %w", err)
		}
		// Verify the signature the Engine attached, using the inbound-side
		// verify logic. The client has the same key the Engine used to sign.
		if err := c.verifyOperation(req, &op, respBody); err != nil {
			return nil, err
		}
		return &op, nil
	default:
		return nil, fmt.Errorf("poll returned status %d: %s", resp.StatusCode, string(respBody))
	}
}

// verifyOperation recomputes the canonical form of a received poll response
// and compares the Engine's X-Axiom-Signature against it, in constant time.
// It reuses the exact same verification path as the inbound authenticator
// (operationkey.Verify) so the two sides are byte-identical.
func (c *LongPollClient) verifyOperation(req *http.Request, op *protocol.Operation, raw []byte) error {
	key, ok := c.operationKey.Current()
	if !ok {
		return errors.New("longpoll: no operation signing key; refuse operation")
	}
	sig := req.Header.Get(operationkey.SignatureHeader)
	if sig == "" || !bytes.HasPrefix([]byte(sig), []byte(operationkey.SignaturePrefix)) {
		return errors.New("longpoll: missing or unversioned " + operationkey.SignatureHeader)
	}
	canonical := operationkey.Canonical(
		req.Method,
		req.URL.Path,
		c.agentID,
		op.Protocol,
		req.Header.Get(operationkey.TimestampHeader),
		req.Header.Get(operationkey.NonceHeader),
		raw,
	)
	if !key.Verify(canonical, sig[len(operationkey.SignaturePrefix):]) {
		return errors.New("longpoll: operation signature does not match")
	}
	return nil
}

// Poll runs a single long-poll cycle: it calls the Engine, handles the
// outcome and returns.
//
// The three possible outcomes are:
//
//   - ErrPollEmpty (204): the caller should retry after EmptyPollDelay.
//   - nil error: the Engine delivered an operation; the caller should call
//     Dispatcher.Dispatch and then post the result (the client itself may
//     do it, depending on the integration).
//   - other error: the caller should apply exponential backoff (until
//     maxBackoff).
func (c *LongPollClient) Poll(ctx context.Context) (*protocol.Operation, error) {
	op, err := c.runPoll(ctx)
	if err != nil {
		if errors.Is(err, ErrPollEmpty) {
			return nil, err
		}
		return nil, err
	}
	return op, nil
}

// Dispatch runs the operation through the mounted dispatcher and then
// reports the outcome to the Engine.
func (c *LongPollClient) Dispatch(ctx context.Context, op protocol.Operation) error {
	agentIdentity := protocol.AgentIdentity{
		AgentID:  c.agentID,
		ServerID: c.serverID,
	}

	// Step 1: run the operation through the dispatcher. The dispatcher
	// validates, dedupes, acknowledges and executes with the per-operation
	// timeout. The ack is returned so the client can report it.
	ack, result := c.dispatcher.Dispatch(ctx, op, agentIdentity)

	// Step 2: build the result body (single JSON object with the ack and the
	// result in separate, correlated fields).
	body, err := json.Marshal(AckResult{Acknowledgement: ack, Result: result})
	if err != nil {
		return fmt.Errorf("post-result: marshal ack+result: %w", err)
	}

	// Step 3: send the report to the Engine, signed with the operation key.
	if err := c.postResult(ctx, body); err != nil {
		return fmt.Errorf("post-result: %w", err)
	}
	return nil
}

// postResult performs POST /api/v1/agent/result with the operation key
// signature over the ack+result body.
func (c *LongPollClient) postResult(ctx context.Context, body []byte) error {
	key, ok := c.operationKey.Current()
	if !ok {
		return errors.New("longpoll: no operation signing key; refuse to report result")
	}
	canonical := operationkey.Canonical(
		http.MethodPost,
		"/api/v1/agent/result",
		c.agentID,
		protocol.Version,
		c.currentTime().Format(time.RFC3339),
		c.nonceFunc(),
		body,
	)
	signature := key.Sign(canonical)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/agent/result", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("post-result request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if err := c.auth.Sign(req); err != nil {
		return fmt.Errorf("post-result sign: %w", err)
	}
	req.Header.Set(operationkey.SignatureHeader, operationkey.SignaturePrefix+signature)
	req.Header.Set(operationkey.TimestampHeader, req.Header.Get("X-Timestamp"))
	req.Header.Set(operationkey.NonceHeader, req.Header.Get("X-Nonce"))

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("post-result request failed: %w", err)
	}
	defer resp.Body.Close()

	// The Engine is authoritative: any 4xx/5xx means the report was rejected
	// and the engine may still not know the outcome, so surface it as an
	// error for the backoff machinery.
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("post-result returned status %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// Run is the long-running loop that drives the long-poll transport.
// It blocks until the context is cancelled or fatal errors are encountered.
//
// The loop is structured as:
//
//	for each iteration:
//	   - call Poll; handle ErrPollEmpty with EmptyPollDelay, other errors with backoff.
//	   - if Poll returns an operation: Dispatch it, then optionally retry the
//	     loop immediately to stay responsive to backlogged operations.
func (c *LongPollClient) Run(ctx context.Context) {
	backoff := minBackoff
	for ctx.Err() == nil {
		op, err := c.Poll(ctx)
		if err != nil {
			if errors.Is(err, ErrPollEmpty) {
				// nothing queued – pause briefly to avoid hammering the Engine.
				select {
				case <-time.After(EmptyPollDelay):
				case <-ctx.Done():
					return
				}
				continue
			}
			c.transportLog.Warn("poll error, backing off", "error", err.Error())
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return
			}
			// exponential backoff, capped at maxBackoff
			if backoff < maxBackoff/2 {
				backoff *= 2
			} else {
				backoff = maxBackoff
			}
			continue
		}

		// Successfully retrieved an operation – dispatch it.
		if err := c.Dispatch(ctx, *op); err != nil {
			c.transportLog.Error("dispatch failed", "error", err.Error())
		}

		// Reset backoff to the minimum for the next empty poll.
		backoff = minBackoff
	}
}
