// Package secrets encrypts sensitive values at rest (foundation for #126).
// Values are sealed with AES-256-GCM; the associated data binds a ciphertext
// to its owner (e.g. a connection ID) so ciphertexts cannot be swapped.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
)

const version byte = 1

// ErrDecrypt is returned for any authentication or format failure.
var ErrDecrypt = errors.New("secrets: unable to decrypt value")

// Box seals and opens secrets with a single 256-bit key.
type Box struct{ aead cipher.AEAD }

// NewBox returns a Box for a 32-byte key.
func NewBox(key []byte) (*Box, error) {
	if len(key) != 32 {
		return nil, errors.New("secrets: key must be 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead: aead}, nil
}

// ParseKey decodes a base64 (standard or URL) encoded 32-byte key.
func ParseKey(s string) ([]byte, error) {
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if k, err := enc.DecodeString(s); err == nil && len(k) == 32 {
			return k, nil
		}
	}
	return nil, errors.New("secrets: key must be 32 bytes encoded in base64")
}

// Seal encrypts plaintext bound to aad. Output: version || nonce || ciphertext.
func (b *Box) Seal(plaintext, aad []byte) ([]byte, error) {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("secrets: nonce: %w", err)
	}
	out := make([]byte, 0, 1+len(nonce)+len(plaintext)+b.aead.Overhead())
	out = append(out, version)
	out = append(out, nonce...)
	return b.aead.Seal(out, nonce, plaintext, aad), nil
}

// Open decrypts a value produced by Seal with the same aad.
func (b *Box) Open(sealed, aad []byte) ([]byte, error) {
	ns := b.aead.NonceSize()
	if len(sealed) < 1+ns+b.aead.Overhead() || sealed[0] != version {
		return nil, ErrDecrypt
	}
	pt, err := b.aead.Open(nil, sealed[1:1+ns], sealed[1+ns:], aad)
	if err != nil {
		return nil, ErrDecrypt
	}
	return pt, nil
}
