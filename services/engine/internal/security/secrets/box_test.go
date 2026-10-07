package secrets

import (
	"bytes"
	"encoding/base64"
	"errors"
	"testing"
)

func key() []byte { return bytes.Repeat([]byte{7}, 32) }

func TestSealOpenRoundTrip(t *testing.T) {
	b, err := NewBox(key())
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := b.Seal([]byte("gho_secret"), []byte("ghc_1"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, []byte("gho_secret")) {
		t.Fatal("ciphertext contains plaintext")
	}
	got, err := b.Open(sealed, []byte("ghc_1"))
	if err != nil || string(got) != "gho_secret" {
		t.Fatalf("open = %q, %v", got, err)
	}
	again, _ := b.Seal([]byte("gho_secret"), []byte("ghc_1"))
	if bytes.Equal(sealed, again) {
		t.Fatal("nonces must differ")
	}
}

func TestOpenRejectsTamperingAndWrongBinding(t *testing.T) {
	b, _ := NewBox(key())
	sealed, _ := b.Seal([]byte("v"), []byte("ghc_1"))
	if _, err := b.Open(sealed, []byte("ghc_2")); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("wrong aad must fail, got %v", err)
	}
	tampered := append([]byte(nil), sealed...)
	tampered[len(tampered)-1] ^= 1
	if _, err := b.Open(tampered, []byte("ghc_1")); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("tampering must fail, got %v", err)
	}
	other, _ := NewBox(bytes.Repeat([]byte{8}, 32))
	if _, err := other.Open(sealed, []byte("ghc_1")); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("wrong key must fail, got %v", err)
	}
	if _, err := b.Open([]byte{1, 2}, nil); !errors.Is(err, ErrDecrypt) {
		t.Fatal("short input must fail")
	}
}

func TestParseKey(t *testing.T) {
	if _, err := ParseKey(base64.StdEncoding.EncodeToString(key())); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseKey(base64.RawURLEncoding.EncodeToString(key())); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseKey("c2hvcnQ="); err == nil {
		t.Fatal("short key must be rejected")
	}
	if _, err := NewBox([]byte("short")); err == nil {
		t.Fatal("NewBox must reject short keys")
	}
}
