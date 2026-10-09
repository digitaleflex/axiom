package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/digitaleflex/axiom/services/engine/internal/agentauth"
	"github.com/digitaleflex/axiom/services/engine/internal/agentkey"
	"github.com/digitaleflex/axiom/services/engine/internal/server"
)

// fakeAgentKeys issues fresh operation signing keys (ADR-0008) without the
// secret store, mirroring agentkey.Service: one new key per issue, so rotation
// is observable in the register/rotate responses.
type fakeAgentKeys struct{}

func (fakeAgentKeys) Issue(_ context.Context, _ string) (agentkey.Key, error) {
	key := make([]byte, agentkey.KeySize)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	return agentkey.Key(key), nil
}

// operationSigningKey asserts the response field: lowercase hex of exactly
// KeySize bytes, returned exactly once, never empty. The plaintext is the
// agent's to keep — the value is returned here so tests can compare rotation.
func assertOperationSigningKey(t *testing.T, body map[string]any) string {
	t.Helper()
	raw, _ := body["operationSigningKey"].(string)
	if raw == "" {
		t.Fatalf("registration response carries no operationSigningKey: %v", body)
	}
	if strings.ToLower(raw) != raw {
		t.Fatalf("operationSigningKey is not lowercase hex: %q", raw)
	}
	key, err := hex.DecodeString(raw)
	if err != nil || len(key) != agentkey.KeySize {
		t.Fatalf("operationSigningKey = %q, want %d bytes of lowercase hex", raw, agentkey.KeySize)
	}
	return raw
}

type agentHarness struct {
	*harness
	agents  *agentauth.Service
	servers map[string]string
	logBuf  *bytes.Buffer
}

func newAgentHarness(t *testing.T, buf *bytes.Buffer) *agentHarness {
	t.Helper()
	if buf == nil {
		buf = &bytes.Buffer{}
	}
	servers := map[string]string{"srv_pending": "pending", "srv_ready": "ready", "srv_revoked": "revoked"}
	agents := agentauth.NewService(
		agentauth.NewMemoryStore(),
		agentauth.WithServerLookup(agentauth.StaticServers(servers)),
	)
	handler := New(Deps{
		Log:       slog.New(slog.NewJSONHandler(buf, nil)),
		Auth:      NewTokenAuthenticator(token, Principal{UserID: "usr_1", Name: "Jane"}),
		Servers:   &fakeServers{items: []server.Record{{ID: "srv_pending", OwnerID: "usr_1", Status: server.StatusPending}}},
		Agents:    agents,
		AgentKeys: fakeAgentKeys{},
	})
	return &agentHarness{
		harness: &harness{t: t, handler: handler},
		agents:  agents,
		servers: servers,
		logBuf:  buf,
	}
}

func (h *agentHarness) bootstrap(serverID string) string {
	h.t.Helper()
	r := h.do("POST", "/api/v1/servers/"+serverID+"/bootstrap", nil, nil)
	expect(h.t, r, 200, "")
	tok, _ := r.body["token"].(string)
	if tok == "" || r.body["expiresAt"] == "" {
		h.t.Fatalf("bootstrap response = %v", r.body)
	}
	return tok
}

func (h *agentHarness) register(serverID, bootstrapToken string) resp {
	h.t.Helper()
	return h.do("POST", "/api/v1/agent/register",
		map[string]any{"serverId": serverID, "agentVersion": "0.1.0", "capabilities": []string{"docker", "traefik"}},
		map[string]string{"Authorization": "Bearer " + bootstrapToken})
}

func rotateHeaders(agentID, credential, nonce string, ts time.Time) map[string]string {
	return map[string]string{
		"Authorization": "Bearer " + credential,
		"X-Agent-ID":    agentID,
		"X-Nonce":       nonce,
		"X-Timestamp":   ts.UTC().Format(time.RFC3339),
	}
}

func TestBootstrapServer(t *testing.T) {
	h := newAgentHarness(t, nil)

	r := h.do("POST", "/api/v1/servers/srv_pending/bootstrap", nil, nil)
	expect(t, r, 200, "")
	if r.body["token"] == "" || r.body["expiresAt"] == "" {
		t.Fatalf("bootstrap = %v", r.body)
	}
	expect(t, h.do("POST", "/api/v1/servers/srv_ready/bootstrap", nil, nil), 409, CodeConflict)
	expect(t, h.do("POST", "/api/v1/servers/srv_missing/bootstrap", nil, nil), 404, CodeNotFound)
	// User authentication is still required for the bootstrap endpoint.
	expect(t, h.do("POST", "/api/v1/servers/srv_pending/bootstrap", nil, map[string]string{"Authorization": ""}), 401, CodeUnauthorized)
}

func TestAgentRegisterAndStatus(t *testing.T) {
	h := newAgentHarness(t, nil)
	tok := h.bootstrap("srv_pending")

	r := h.register("srv_pending", tok)
	expect(t, r, 201, "")
	agentID, _ := r.body["agentId"].(string)
	credential, _ := r.body["credential"].(string)
	if !strings.HasPrefix(agentID, "agent_") || credential == "" || r.body["negotiated"].(float64) != float64(agentauth.ProtocolVersion) {
		t.Fatalf("register = %v", r.body)
	}
	if r.body["serverId"] != "srv_pending" || r.body["credentialVersion"].(float64) != 1 {
		t.Fatalf("register binding = %v", r.body)
	}
	// ADR-0008: the register response carries the operation signing key,
	// lowercase hex, exactly once.
	assertOperationSigningKey(t, r.body)

	// Status by agent and by server.
	r = h.do("GET", "/api/v1/agent/status?agentId="+agentID, nil, nil)
	expect(t, r, 200, "")
	if r.body["status"] != agentauth.StatusRegistered || r.body["agentId"] != agentID {
		t.Fatalf("status = %v", r.body)
	}
	r = h.do("GET", "/api/v1/agent/status?serverId=srv_pending", nil, nil)
	expect(t, r, 200, "")
	if r.body["status"] != agentauth.StatusRegistered {
		t.Fatalf("status by server = %v", r.body)
	}
	// Unknown agent.
	r = h.do("GET", "/api/v1/agent/status?agentId=agent_ffffffffffffffffffffffff", nil, nil)
	expect(t, r, 200, "")
	if r.body["status"] != agentauth.StatusUnknown {
		t.Fatalf("unknown status = %v", r.body)
	}
	// Revoked server reads as revoked.
	h.servers["srv_pending"] = "revoked"
	r = h.do("GET", "/api/v1/agent/status?agentId="+agentID, nil, nil)
	expect(t, r, 200, "")
	if r.body["status"] != agentauth.StatusRevoked {
		t.Fatalf("revoked status = %v", r.body)
	}
	// Missing parameters is a client error.
	expect(t, h.do("GET", "/api/v1/agent/status", nil, nil), 400, CodeInvalidRequest)
}

func TestAgentRegisterRejectsBadCredentials(t *testing.T) {
	h := newAgentHarness(t, nil)

	// No Authorization header: the user token in the harness must NOT be
	// accepted for the agent endpoint.
	expect(t, h.do("POST", "/api/v1/agent/register",
		map[string]any{"serverId": "srv_pending", "agentVersion": "0.1.0", "capabilities": []string{"docker"}}, nil),
		401, CodeUnauthorized)
	expect(t, h.register("srv_pending", "bt_unknown"), 401, CodeUnauthorized)
	expect(t, h.do("POST", "/api/v1/agent/register",
		map[string]any{"serverId": "srv_pending", "agentVersion": "", "capabilities": []string{}},
		map[string]string{"Authorization": "Bearer bt_x"}), 422, CodeValidationFailed)

	tok := h.bootstrap("srv_pending")
	r := h.register("srv_pending", tok)
	expect(t, r, 201, "")
	// Double redeem of the same single-use token is rejected.
	expect(t, h.register("srv_pending", tok), 401, CodeUnauthorized)
	// Wrong server binding is rejected.
	tok2 := h.bootstrap("srv_pending")
	expect(t, h.register("srv_ready", tok2), 401, CodeUnauthorized)
}

func TestAgentReregistrationKeepsAgentID(t *testing.T) {
	h := newAgentHarness(t, nil)

	first := h.register("srv_pending", h.bootstrap("srv_pending"))
	expect(t, first, 201, "")
	second := h.register("srv_pending", h.bootstrap("srv_pending"))
	expect(t, second, 201, "")
	if first.body["agentId"] != second.body["agentId"] {
		t.Fatalf("agent id changed: %v != %v", first.body["agentId"], second.body["agentId"])
	}
	if second.body["credentialVersion"].(float64) != 2 {
		t.Fatalf("credential version = %v, want 2", second.body["credentialVersion"])
	}
}

func TestAgentRotate(t *testing.T) {
	h := newAgentHarness(t, nil)
	reg := h.register("srv_pending", h.bootstrap("srv_pending"))
	agentID, _ := reg.body["agentId"].(string)
	credential, _ := reg.body["credential"].(string)
	firstKey := assertOperationSigningKey(t, reg.body)

	r := h.do("POST", "/api/v1/agent/rotate", nil, rotateHeaders(agentID, credential, "nonce-1", time.Now().UTC()))
	expect(t, r, 200, "")
	next, _ := r.body["credential"].(string)
	if next == "" || next == credential || r.body["credentialVersion"].(float64) != 2 {
		t.Fatalf("rotate = %v", r.body)
	}
	// ADR-0008: rotation reissues the operation signing key the way it
	// reissues the credential.
	nextKey := assertOperationSigningKey(t, r.body)
	if nextKey == firstKey {
		t.Fatal("rotation must reissue the operation signing key")
	}

	// Missing credential/identity.
	expect(t, h.do("POST", "/api/v1/agent/rotate", nil, map[string]string{"Authorization": "", "X-Agent-ID": ""}), 401, CodeUnauthorized)
	// Invalid credential.
	expect(t, h.do("POST", "/api/v1/agent/rotate", nil, rotateHeaders(agentID, "ac_wrong", "nonce-2", time.Now().UTC())), 401, CodeUnauthorized)
	// Replayed nonce.
	h.do("POST", "/api/v1/agent/rotate", nil, rotateHeaders(agentID, next, "nonce-3", time.Now().UTC()))
	r = h.do("POST", "/api/v1/agent/rotate", nil, rotateHeaders(agentID, next, "nonce-3", time.Now().UTC()))
	expect(t, r, 401, CodeUnauthorized)
	if reason := errorReason(r); reason != "replay" {
		t.Fatalf("replay reason = %q", reason)
	}
	// Clock skew.
	r = h.do("POST", "/api/v1/agent/rotate", nil, rotateHeaders(agentID, next, "nonce-4", time.Now().UTC().Add(-10*time.Minute)))
	expect(t, r, 401, CodeUnauthorized)
	if reason := errorReason(r); reason != "clock_skew" {
		t.Fatalf("clock skew reason = %q", reason)
	}
}

func TestAgentRotateRejectedWhenServerRevoked(t *testing.T) {
	h := newAgentHarness(t, nil)
	reg := h.register("srv_pending", h.bootstrap("srv_pending"))
	agentID, _ := reg.body["agentId"].(string)
	credential, _ := reg.body["credential"].(string)

	h.servers["srv_pending"] = "revoked"
	r := h.do("POST", "/api/v1/agent/rotate", nil, rotateHeaders(agentID, credential, "nonce-r", time.Now().UTC()))
	expect(t, r, 401, CodeUnauthorized)
	if reason := errorReason(r); reason != "revoked" {
		t.Fatalf("revoked reason = %q", reason)
	}
}

func TestAgentCredentialsNeverLogged(t *testing.T) {
	h := newAgentHarness(t, nil)

	// A failed registration must not echo the presented token.
	h.register("srv_pending", "bt_supersecret_token_value")
	if strings.Contains(h.logBuf.String(), "bt_supersecret_token_value") {
		t.Fatalf("bootstrap token leaked into logs: %s", h.logBuf.String())
	}

	// A successful registration returns a credential but must not log it.
	tok := h.bootstrap("srv_pending")
	if strings.Contains(h.logBuf.String(), tok) {
		t.Fatalf("bootstrap token leaked into logs: %s", h.logBuf.String())
	}
	reg := h.register("srv_pending", tok)
	expect(t, reg, 201, "")
	credential, _ := reg.body["credential"].(string)
	if credential == "" {
		t.Fatal("expected a credential")
	}
	if strings.Contains(h.logBuf.String(), credential) {
		t.Fatalf("credential leaked into logs: %s", h.logBuf.String())
	}
	// The operation signing key is plaintext exactly once, in the response —
	// never in the logs (ADR-0008).
	signingKey := assertOperationSigningKey(t, reg.body)
	if strings.Contains(h.logBuf.String(), signingKey) {
		t.Fatalf("operation signing key leaked into logs: %s", h.logBuf.String())
	}
}

func errorReason(r resp) string {
	e, _ := r.body["error"].(map[string]any)
	d, _ := e["details"].(map[string]any)
	reason, _ := d["reason"].(string)
	return reason
}
