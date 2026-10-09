package operationkey

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Golden vector (wire contract, shared byte-for-byte with the Engine lane):
//
//	key (raw)  = ASCII "axiom-test-key"
//	method     = POST
//	path       = /api/v1/agent/operations
//	agentID    = agt_0123456789abcdef01234567
//	version    = 2
//	timestamp  = 2026-10-09T12:00:00Z
//	nonce      = 00112233445566778899aabbccddeeff
//	body       = {}
//	sha256(body) = 44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a
//	signature  = f6adba7b8e7e84811dc101769c40914d5b2ab7a9411ebed636b45925b909b6db
const (
	goldenKey        = "axiom-test-key"
	goldenMethod     = "POST"
	goldenPath       = "/api/v1/agent/operations"
	goldenAgentID    = "agt_0123456789abcdef01234567"
	goldenVersion    = 2
	goldenTimestamp  = "2026-10-09T12:00:00Z"
	goldenNonce      = "00112233445566778899aabbccddeeff"
	goldenBody       = "{}"
	goldenBodyDigest = "44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a"
	goldenSignature  = "f6adba7b8e7e84811dc101769c40914d5b2ab7a9411ebed636b45925b909b6db"
	goldenCanonical  = "AXIOM-HMAC-V1\nPOST\n/api/v1/agent/operations\nagt_0123456789abcdef01234567\n2\n2026-10-09T12:00:00Z\n00112233445566778899aabbccddeeff\n44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a"
)

// TestGoldenVector pins the wire contract byte-for-byte. If this fails, the
// agent and the Engine no longer sign the same bytes and every deployment
// would be refused (or worse, accepted unsigned).
func TestGoldenVector(t *testing.T) {
	if got := BodyDigest([]byte(goldenBody)); got != goldenBodyDigest {
		t.Fatalf("body digest = %q, want %q", got, goldenBodyDigest)
	}

	canonical := Canonical(goldenMethod, goldenPath, goldenAgentID, goldenVersion,
		goldenTimestamp, goldenNonce, []byte(goldenBody))
	if canonical != goldenCanonical {
		t.Fatalf("canonical mismatch:\n got %q\nwant %q", canonical, goldenCanonical)
	}
	if strings.HasSuffix(canonical, "\n") {
		t.Fatal("canonical form must not carry a trailing newline")
	}

	key := Key(goldenKey)
	if got := key.Sign(canonical); got != goldenSignature {
		t.Fatalf("signature = %q, want %q", got, goldenSignature)
	}
	if !key.Verify(canonical, goldenSignature) {
		t.Fatal("golden signature rejected")
	}

	// Negative cases: the verifier must refuse anything else.
	badSig := goldenSignature[:len(goldenSignature)-1] + "c"
	if key.Verify(canonical, badSig) {
		t.Fatal("tampered signature accepted")
	}
	if key.Verify(canonical, "") {
		t.Fatal("empty signature accepted")
	}
	if Key("axiom-test-key2").Verify(canonical, goldenSignature) {
		t.Fatal("signature accepted under the wrong key")
	}
	if key.Verify(canonical+" ", goldenSignature) {
		t.Fatal("tampered canonical accepted")
	}
	// A tampered body changes the digest line and must break the signature.
	tampered := Canonical(goldenMethod, goldenPath, goldenAgentID, goldenVersion,
		goldenTimestamp, goldenNonce, []byte(`{"x":1}`))
	if key.Verify(tampered, goldenSignature) {
		t.Fatal("signature accepted over a tampered body")
	}
}

func TestParseHex(t *testing.T) {
	raw := strings.Repeat("ab", KeySize)
	key, err := ParseHex(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(key) != KeySize || key.Hex() != raw {
		t.Fatalf("key = %x, want %s", key, raw)
	}

	for name, bad := range map[string]string{
		"empty":         "",
		"short":         strings.Repeat("ab", KeySize-1),
		"long":          strings.Repeat("ab", KeySize+1),
		"odd length":    strings.Repeat("ab", KeySize) + "a",
		"uppercase":     strings.ToUpper(raw),
		"mixed case":    strings.Repeat("Ab", KeySize),
		"non-hex":       strings.Repeat("zz", KeySize),
		"odd hex chars": strings.Repeat("a", KeySize*2-1) + "g",
	} {
		if _, err := ParseHex(bad); err == nil {
			t.Errorf("ParseHex(%s) accepted %q", name, bad)
		} else if !strings.Contains(err.Error(), ErrInvalidKey.Error()) {
			t.Errorf("ParseHex(%s) err = %v, want ErrInvalidKey", name, err)
		}
	}
}

func TestStoreRoundTripAndPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operation-key.json")
	s := NewStore(path)
	if _, err := s.Load(); err != ErrNoKey {
		t.Fatalf("load missing = %v, want ErrNoKey", err)
	}

	key := Key(goldenKey) // short raw key on purpose: the store is length-agnostic
	if err := s.Save(key); err != nil {
		t.Fatalf("save: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("operation key file perm = %o, want 600", perm)
	}

	if cur, ok := s.Current(); !ok || string(cur) != goldenKey {
		t.Fatalf("current = %q ok=%v", cur, ok)
	}
	reloaded, err := NewStore(path).Load()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if string(reloaded) != goldenKey {
		t.Fatalf("reloaded = %q, want %q", reloaded, goldenKey)
	}
}

func TestStoreSaveRejectsEmptyAndLoadRejectsCorrupt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operation-key.json")
	s := NewStore(path)
	if err := s.Save(nil); err == nil {
		t.Fatal("Save(nil) must fail")
	}

	if err := os.WriteFile(path, []byte("not json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(path).Load(); err == nil {
		t.Fatal("a corrupt key file must be an error, never a silent reset")
	}
	if err := os.WriteFile(path, []byte(`{"operationSigningKey":"NOTHEX"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(path).Load(); err == nil {
		t.Fatal("a malformed key must be an error")
	}
}
