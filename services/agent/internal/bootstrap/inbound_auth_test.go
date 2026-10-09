package bootstrap

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/digitaleflex/axiom/services/agent/internal/protocol"
	"github.com/digitaleflex/axiom/services/agent/internal/security/operationkey"
)

// Golden vector (wire contract, identical byte-for-byte to the Engine lane and
// to internal/security/operationkey): raw key = ASCII "axiom-test-key",
// method=POST, path=/api/v1/agent/operations,
// agentID=agt_0123456789abcdef01234567, version=2,
// timestamp=2026-10-09T12:00:00Z, nonce=00112233445566778899aabbccddeeff,
// body={}, sha256(body)=44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a,
// signature=f6adba7b8e7e84811dc101769c40914d5b2ab7a9411ebed636b45925b909b6db.
const (
	goldenKey       = "axiom-test-key"
	goldenAgentID   = "agt_0123456789abcdef01234567"
	goldenPath      = "/api/v1/agent/operations"
	goldenTimestamp = "2026-10-09T12:00:00Z"
	goldenNonce     = "00112233445566778899aabbccddeeff"
	goldenBody      = "{}"
	goldenSignature = "f6adba7b8e7e84811dc101769c40914d5b2ab7a9411ebed636b45925b909b6db"
)

// testSigningKeyHex is a well-formed 32-byte key in its wire form.
var testSigningKeyHex = strings.Repeat("ab", 32)

func goldenKeyStore(t *testing.T) *operationkey.Store {
	t.Helper()
	s := operationkey.NewStore(filepath.Join(t.TempDir(), "operation-key.json"))
	if err := s.Save(operationkey.Key(goldenKey)); err != nil {
		t.Fatal(err)
	}
	return s
}

// goldenRequest builds the exact request of the golden vector.
func goldenRequest(t *testing.T) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, goldenPath, strings.NewReader(goldenBody))
	req.Header.Set(operationkey.SignatureHeader, operationkey.SignaturePrefix+goldenSignature)
	req.Header.Set(operationkey.TimestampHeader, goldenTimestamp)
	req.Header.Set(operationkey.NonceHeader, goldenNonce)
	return req
}

// TestSignedInboundGoldenVector pins the wire contract on the verifier: the
// golden request authenticates and verifies, and any tampering is refused.
func TestSignedInboundGoldenVector(t *testing.T) {
	v := newSignedInbound(goldenKeyStore(t), goldenAgentID)
	v.Now = func() time.Time { return time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC) }

	req := goldenRequest(t)
	op := protocol.Operation{Envelope: protocol.Envelope{Protocol: 2}}
	raw := []byte(goldenBody)

	if err := v.Authenticate(req); err != nil {
		t.Fatalf("golden vector refused at Authenticate: %v", err)
	}
	if err := v.VerifyOperation(req, op, raw); err != nil {
		t.Fatalf("golden vector signature refused: %v", err)
	}

	// Anti-replay: the same nonce is refused on redelivery, even with a valid
	// signature.
	if err := v.VerifyOperation(req, op, raw); err == nil {
		t.Fatal("replayed nonce accepted")
	}

	// The signature covers the raw bytes: a tampered body breaks it.
	if err := v.VerifyOperation(req, op, []byte(`{"x":1}`)); err == nil {
		t.Fatal("tampered body accepted")
	}
	// The version line comes from the operation: a drift breaks the signature.
	if err := v.VerifyOperation(req, protocol.Operation{Envelope: protocol.Envelope{Protocol: 1}}, raw); err == nil {
		t.Fatal("version drift accepted")
	}
	// A tampered signature value is refused.
	tamperedSig := goldenRequest(t)
	tamperedSig.Header.Set(operationkey.SignatureHeader,
		operationkey.SignaturePrefix+goldenSignature[:len(goldenSignature)-1]+"c")
	if err := v.VerifyOperation(tamperedSig, op, raw); err == nil {
		t.Fatal("tampered signature accepted")
	}
	// A tampered canonical input (nonce header swapped after signing) breaks it.
	tamperedNonce := goldenRequest(t)
	tamperedNonce.Header.Set(operationkey.NonceHeader, "ff112233445566778899aabbccddeeff")
	if err := v.VerifyOperation(tamperedNonce, op, raw); err == nil {
		t.Fatal("tampered nonce accepted")
	}
	// The key is part of the secret: another key refuses the golden signature.
	other := newSignedInbound(goldenKeyStore(t), goldenAgentID)
	if err := other.keys.Save(operationkey.Key(goldenKey + "!")); err != nil {
		t.Fatal(err)
	}
	if err := other.VerifyOperation(goldenRequest(t), op, raw); err == nil {
		t.Fatal("signature accepted under the wrong key")
	}
}

// TestSignedInboundAuthenticateHeaderChecks covers the header-only gate that
// runs before the body is read.
func TestSignedInboundAuthenticateHeaderChecks(t *testing.T) {
	v := newSignedInbound(goldenKeyStore(t), goldenAgentID)
	v.Now = func() time.Time { return time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC) }

	base := func() *http.Request {
		req := httptest.NewRequest(http.MethodPost, goldenPath, nil)
		req.Header.Set(operationkey.SignatureHeader, operationkey.SignaturePrefix+"ab")
		req.Header.Set(operationkey.TimestampHeader, goldenTimestamp)
		req.Header.Set(operationkey.NonceHeader, "nonce-1")
		return req
	}
	if err := v.Authenticate(base()); err != nil {
		t.Fatalf("well-formed headers refused: %v", err)
	}

	for name, mutate := range map[string]func(r *http.Request){
		"no signature":       func(r *http.Request) { r.Header.Del(operationkey.SignatureHeader) },
		"unversioned":        func(r *http.Request) { r.Header.Set(operationkey.SignatureHeader, "deadbeef") },
		"empty signature":    func(r *http.Request) { r.Header.Set(operationkey.SignatureHeader, operationkey.SignaturePrefix) },
		"v2 prefix":          func(r *http.Request) { r.Header.Set(operationkey.SignatureHeader, "v2=ab") },
		"no timestamp":       func(r *http.Request) { r.Header.Del(operationkey.TimestampHeader) },
		"bad timestamp":      func(r *http.Request) { r.Header.Set(operationkey.TimestampHeader, "2026-10-09 12:00:00") },
		"stale timestamp":    func(r *http.Request) { r.Header.Set(operationkey.TimestampHeader, "2026-10-09T11:54:00Z") },
		"future timestamp":   func(r *http.Request) { r.Header.Set(operationkey.TimestampHeader, "2026-10-09T12:06:00Z") },
		"no nonce":           func(r *http.Request) { r.Header.Del(operationkey.NonceHeader) },
		"empty nonce":        func(r *http.Request) { r.Header.Set(operationkey.NonceHeader, "") },
		"whitespace nonce":   func(r *http.Request) { r.Header.Set(operationkey.NonceHeader, " ") },
		"timestamp one week": func(r *http.Request) { r.Header.Set(operationkey.TimestampHeader, "2026-10-02T12:00:00Z") },
	} {
		req := base()
		mutate(req)
		if err := v.Authenticate(req); err == nil {
			t.Errorf("%s: request authenticated", name)
		}
	}

	// The 5-minute window itself is accepted at the boundary.
	edge := base()
	edge.Header.Set(operationkey.TimestampHeader, "2026-10-09T12:05:00Z")
	if err := v.Authenticate(edge); err != nil {
		t.Errorf("timestamp at the skew boundary refused: %v", err)
	}

	// No key stored: everything is refused, headers or not.
	empty := newSignedInbound(operationkey.NewStore(filepath.Join(t.TempDir(), "none.json")), goldenAgentID)
	if err := empty.Authenticate(base()); err == nil {
		t.Fatal("an authenticator without a key must refuse")
	}
	if err := empty.VerifyOperation(base(), protocol.Operation{}, []byte(goldenBody)); err == nil {
		t.Fatal("an authenticator without a key must refuse verification")
	}
}

// TestRefuseInboundImplementsBothSteps is the parity guard: an agent without a
// registered key keeps answering 401 on the operation leg.
func TestRefuseInboundImplementsBothSteps(t *testing.T) {
	var a InboundAuthenticator = refuseInbound{}
	if err := a.Authenticate(&http.Request{}); err == nil {
		t.Fatal("refuseInbound.Authenticate must refuse")
	}
	if err := a.VerifyOperation(&http.Request{}, protocol.Operation{}, []byte("{}")); err == nil {
		t.Fatal("refuseInbound.VerifyOperation must refuse")
	}
}

// signRequest builds an operation request whose signature covers signOver
// while send is the body actually posted (they differ for tamper tests).
func signRequest(t *testing.T, path, agentID string, send, signOver []byte, key operationkey.Key, nonce string) *http.Request {
	t.Helper()
	ts := time.Now().UTC().Format(time.RFC3339)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(send))
	req.Header.Set(operationkey.TimestampHeader, ts)
	req.Header.Set(operationkey.NonceHeader, nonce)
	canonical := operationkey.Canonical(http.MethodPost, path, agentID, protocol.Version, ts, nonce, signOver)
	req.Header.Set(operationkey.SignatureHeader, operationkey.SignaturePrefix+key.Sign(canonical))
	return req
}

// TestAgentWithoutOperationKeyRefusesOperations: an agent that registered
// without an operation signing key stays closed (401 before the body is read),
// exactly like refuseInbound did before ADR-0008.
func TestAgentWithoutOperationKeyRefusesOperations(t *testing.T) {
	engine := newFakeEngine(t) // issues no operationSigningKey
	cfg := testConfig(t, engine.URL)
	app, err := New(context.Background(), cfg, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	app.Listener.log = discardLogger()

	body := validOperation()
	// Before registration.
	req := httptest.NewRequest(http.MethodPost, cfg.Listener.Path, bytes.NewReader(body))
	rr := httptest.NewRecorder()
	app.Listener.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("unsigned operation = %d, want 401", rr.Code)
	}

	// Registration issues no key: the agent keeps refuseInbound mounted.
	if err := app.register(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok := app.Listener.auth.(refuseInbound); !ok {
		t.Fatal("an agent without an operation signing key must keep refuseInbound")
	}
	if _, ok := app.OperationKeys.Current(); ok {
		t.Fatal("no key must be stored when the Engine issues none")
	}

	// Even a request carrying well-formed signature headers is refused.
	key, err := operationkey.ParseHex(testSigningKeyHex)
	if err != nil {
		t.Fatal(err)
	}
	req2 := signRequest(t, cfg.Listener.Path, app.IdentityID.AgentID, body, body, key, "00112233445566778899aabbccddeeff")
	rr2 := httptest.NewRecorder()
	app.Listener.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusUnauthorized {
		t.Fatalf("signed operation without a stored key = %d, want 401", rr2.Code)
	}
}

// TestRegistrationPersistsOperationKeyAndAcceptsSignedOperations is the
// end-to-end leg: the key from the registration response is persisted 0600
// before the verifier is mounted, a properly signed operation reaches the
// dispatcher, and every tampering is refused with 401.
func TestRegistrationPersistsOperationKeyAndAcceptsSignedOperations(t *testing.T) {
	engine := newFakeEngine(t)
	engine.operationSigningKey = testSigningKeyHex
	cfg := testConfig(t, engine.URL)
	app, err := New(context.Background(), cfg, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	app.Listener.log = discardLogger()

	if err := app.register(context.Background()); err != nil {
		t.Fatal(err)
	}

	// The key is persisted (0600) and the verifier is mounted.
	key, ok := app.OperationKeys.Current()
	if !ok || key.Hex() != testSigningKeyHex {
		t.Fatalf("stored key = %x ok=%v, want %s", []byte(key), ok, testSigningKeyHex)
	}
	info, err := os.Stat(cfg.OperationKeyPath())
	if err != nil {
		t.Fatalf("operation key file: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("operation key file perm = %o, want 600", perm)
	}
	if _, ok := app.Listener.auth.(*signedInbound); !ok {
		t.Fatal("registration must mount the HMAC verifier on the listener")
	}

	agentID := app.IdentityID.AgentID
	body := validOperation()

	// A properly signed operation reaches the real dispatcher (Docker is
	// absent on the test host, so the operation fails with a stable runtime
	// code — proving it travelled through the dispatcher).
	req := signRequest(t, cfg.Listener.Path, agentID, body, body, key, "00112233445566778899aabbccddeeff")
	rr := httptest.NewRecorder()
	app.Listener.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("signed operation = %d, want 200 (%s)", rr.Code, rr.Body.String())
	}
	var out struct {
		OperationID  string `json:"operationId"`
		DeploymentID string `json:"deploymentId"`
		Success      bool   `json:"success"`
		ErrorCode    string `json:"errorCode"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode result: %v (%s)", err, rr.Body.String())
	}
	if out.OperationID != testOperationID || out.DeploymentID != testDeploymentID {
		t.Fatalf("result names another operation: %s", rr.Body.String())
	}
	if out.Success || out.ErrorCode == "" {
		t.Fatalf("expected a stable runtime failure, got: %s", rr.Body.String())
	}

	// A tampered body: the signature was computed over the original bytes.
	tampered := bytes.Replace(body, []byte(`"port":3000`), []byte(`"port":3001`), 1)
	if bytes.Equal(tampered, body) {
		t.Fatal("tamper fixture did not change the body")
	}
	reqTampered := signRequest(t, cfg.Listener.Path, agentID, tampered, body, key, "10112233445566778899aabbccddeeff")
	rrT := httptest.NewRecorder()
	app.Listener.ServeHTTP(rrT, reqTampered)
	if rrT.Code != http.StatusUnauthorized {
		t.Fatalf("tampered body = %d, want 401", rrT.Code)
	}

	// A tampered signature over a clean body.
	reqBadSig := signRequest(t, cfg.Listener.Path, agentID, body, body, key, "20112233445566778899aabbccddeeff")
	reqBadSig.Header.Set(operationkey.SignatureHeader, operationkey.SignaturePrefix+strings.Repeat("0", 64))
	rrB := httptest.NewRecorder()
	app.Listener.ServeHTTP(rrB, reqBadSig)
	if rrB.Code != http.StatusUnauthorized {
		t.Fatalf("tampered signature = %d, want 401", rrB.Code)
	}

	// An unsigned request.
	reqPlain := httptest.NewRequest(http.MethodPost, cfg.Listener.Path, bytes.NewReader(body))
	rrP := httptest.NewRecorder()
	app.Listener.ServeHTTP(rrP, reqPlain)
	if rrP.Code != http.StatusUnauthorized {
		t.Fatalf("unsigned operation = %d, want 401", rrP.Code)
	}

	// Replay of the accepted request: byte-identical redelivery, valid
	// signature, same nonce — refused.
	replay := httptest.NewRequest(http.MethodPost, cfg.Listener.Path, bytes.NewReader(body))
	replay.Header = req.Header.Clone()
	rrR := httptest.NewRecorder()
	app.Listener.ServeHTTP(rrR, replay)
	if rrR.Code != http.StatusUnauthorized {
		t.Fatalf("replayed operation = %d, want 401", rrR.Code)
	}

	// A bad version prefix never verifies.
	reqV2 := signRequest(t, cfg.Listener.Path, agentID, body, body, key, "30112233445566778899aabbccddeeff")
	reqV2.Header.Set(operationkey.SignatureHeader, "v2="+strings.Repeat("0", 64))
	rrV := httptest.NewRecorder()
	app.Listener.ServeHTTP(rrV, reqV2)
	if rrV.Code != http.StatusUnauthorized {
		t.Fatalf("unversioned signature = %d, want 401", rrV.Code)
	}
}
