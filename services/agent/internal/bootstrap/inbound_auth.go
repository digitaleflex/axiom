package bootstrap

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/digitaleflex/axiom/services/agent/internal/protocol"
	"github.com/digitaleflex/axiom/services/agent/internal/security/operationkey"
)

// NonceWindow is how long a nonce is remembered for replay rejection.
const NonceWindow = 10 * time.Minute

// signedInbound is the production InboundAuthenticator for the Engine→Agent
// leg (ADR-0008, C6/C8): it verifies the HMAC-SHA256 signature the Engine puts
// on every operation, under the operation signing key the agent persisted at
// registration (operationkey, 0600).
//
// The key is read from the store on every request, so a key handed over at
// credential rotation takes effect without rebuilding the authenticator; the
// key is always persisted before this authenticator is constructed or used.
// An agent with no key is never mounted with signedInbound (the composition
// root mounts refuseInbound), and signedInbound itself fails closed if the key
// ever disappears.
type signedInbound struct {
	keys *operationkey.Store
	// agentID is this agent's ID: it enters the canonical form as a value the
	// agent knows independently of the request, never one taken from the body.
	agentID string
	// Now is injectable for deterministic tests.
	Now func() time.Time

	mu sync.Mutex
	// dedupe rejects replayed nonces inside NonceWindow.
	dedupe *protocol.Dedupe
}

// newSignedInbound returns the HMAC verifier for the key held in keys. keys
// must already hold the key (persisted first, constructed after).
func newSignedInbound(keys *operationkey.Store, agentID string) *signedInbound {
	return &signedInbound{
		keys:    keys,
		agentID: agentID,
		Now:     func() time.Time { return time.Now().UTC() },
		dedupe:  protocol.NewDedupe(NonceWindow),
	}
}

func (s *signedInbound) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now().UTC()
}

// Authenticate inspects the signature headers only — never r.Body — so an
// unauthenticated request is refused (401) before the agent allocates anything
// for it. It checks: a signature header with the v1= scheme prefix, an
// X-Timestamp parseable as RFC3339 and inside protocol.MaxClockSkew, and a
// non-empty X-Nonce.
func (s *signedInbound) Authenticate(r *http.Request) error {
	if _, ok := s.keys.Current(); !ok {
		return fmt.Errorf("%w: no operation signing key", ErrUnauthenticated)
	}
	sig := r.Header.Get(operationkey.SignatureHeader)
	if !strings.HasPrefix(sig, operationkey.SignaturePrefix) ||
		len(sig) <= len(operationkey.SignaturePrefix) {
		return fmt.Errorf("%w: missing or unversioned %s", ErrUnauthenticated, operationkey.SignatureHeader)
	}
	ts := r.Header.Get(operationkey.TimestampHeader)
	parsed, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return fmt.Errorf("%w: %s is not RFC3339", ErrUnauthenticated, operationkey.TimestampHeader)
	}
	if delta := s.now().Sub(parsed); delta > protocol.MaxClockSkew || delta < -protocol.MaxClockSkew {
		return fmt.Errorf("%w: %s outside the freshness window", ErrUnauthenticated, operationkey.TimestampHeader)
	}
	if strings.TrimSpace(r.Header.Get(operationkey.NonceHeader)) == "" {
		return fmt.Errorf("%w: missing %s", ErrUnauthenticated, operationkey.NonceHeader)
	}
	return nil
}

// VerifyOperation recomputes the canonical form of the request and compares
// the HMAC-SHA256 under the stored key with the X-Axiom-Signature value
// (constant time, hmac.Equal). It runs after the strict decode and validation
// and before Dispatch: the version comes from the decoded operation, the
// timestamp and nonce from the exact header values, the digest from the raw
// bytes as received. A valid signature with a replayed nonce is refused too.
func (s *signedInbound) VerifyOperation(r *http.Request, op protocol.Operation, raw []byte) error {
	key, ok := s.keys.Current()
	if !ok {
		return fmt.Errorf("%w: no operation signing key", ErrUnauthenticated)
	}
	sig := r.Header.Get(operationkey.SignatureHeader)
	if !strings.HasPrefix(sig, operationkey.SignaturePrefix) {
		return fmt.Errorf("%w: missing %s", ErrUnauthenticated, operationkey.SignatureHeader)
	}
	canonical := operationkey.Canonical(
		r.Method,
		r.URL.Path,
		s.agentID,
		op.Protocol,
		r.Header.Get(operationkey.TimestampHeader),
		r.Header.Get(operationkey.NonceHeader),
		raw,
	)
	if !key.Verify(canonical, strings.TrimPrefix(sig, operationkey.SignaturePrefix)) {
		return fmt.Errorf("%w: signature does not match", ErrUnauthenticated)
	}

	// Anti-replay (C8): a nonce is single-use inside NonceWindow. The check is
	// recorded only after the signature is proven, so an unauthenticated peer
	// can never poison the nonce cache.
	nonce := r.Header.Get(operationkey.NonceHeader)
	s.mu.Lock()
	replay := s.dedupe.Check(nonce, s.now())
	s.mu.Unlock()
	if replay {
		return fmt.Errorf("%w: replayed nonce", ErrUnauthenticated)
	}
	return nil
}

// Both production authenticators implement the two-step InboundAuthenticator.
var (
	_ InboundAuthenticator = &signedInbound{}
	_ InboundAuthenticator = refuseInbound{}
)
