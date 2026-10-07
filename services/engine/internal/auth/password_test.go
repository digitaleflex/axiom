package auth

import (
	"encoding/hex"
	"strings"
	"testing"
)

// TestPBKDF2SHA256KnownVector checks the hand-rolled PBKDF2 against the
// PBKDF2-HMAC-SHA256 vectors from RFC 7914 §11.
func TestPBKDF2SHA256KnownVector(t *testing.T) {
	cases := []struct {
		password string
		salt     string
		iter     int
		want     string
	}{
		{"passwd", "salt", 1,
			"55ac046e56e3089fec1691c22544b605f94185216dde0465e68b9d57c20dacbc" +
				"49ca9cccf179b645991664b39d77ef317c71b845b1e30bd509112041d3a19783"},
		{"Password", "NaCl", 80000,
			"4ddcd8f60b98be21830cee5ef22701f9641a4418d04c0414aeff08876b34ab56" +
				"a1d425a1225833549adb841b51c9b3176a272bdebba1d078478f62b397f33c8d"},
	}
	for _, tc := range cases {
		got := pbkdf2SHA256([]byte(tc.password), []byte(tc.salt), tc.iter, 64)
		if hex.EncodeToString(got) != tc.want {
			t.Fatalf("pbkdf2(%q,%q,%d) mismatch:\n got %x\nwant %s", tc.password, tc.salt, tc.iter, got, tc.want)
		}
	}
}

func TestHashAndVerifyPassword(t *testing.T) {
	encoded, err := hashPassword("correct horse battery staple", 1000)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(encoded, pbkdf2Prefix+"$1000$") {
		t.Fatalf("unexpected encoding: %q", encoded)
	}
	if strings.Contains(encoded, "correct horse") {
		t.Fatal("hash must not contain the plaintext password")
	}
	if !verifyPassword(encoded, "correct horse battery staple") {
		t.Fatal("valid password must verify")
	}
	if verifyPassword(encoded, "wrong password") {
		t.Fatal("wrong password must not verify")
	}
	if verifyPassword("garbage", "correct horse battery staple") {
		t.Fatal("malformed hash must not verify")
	}
	// Two hashes of the same password differ (random per-user salt).
	other, err := hashPassword("correct horse battery staple", 1000)
	if err != nil {
		t.Fatal(err)
	}
	if other == encoded {
		t.Fatal("salts must be unique per hash")
	}
}
