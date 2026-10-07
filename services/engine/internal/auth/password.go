package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"
)

// Password hashing uses PBKDF2-HMAC-SHA256.
//
// INTERIM DECISION (#125): the Engine module only depends on pgx, and no
// vetted password-hashing dependency (Argon2id / scrypt / bcrypt) has been
// approved yet. Rather than pull a new dependency, PBKDF2 is implemented here
// with the standard library only. It must be replaced by a memory-hard KDF
// (Argon2id) once a dependency is approved. Parameters: a per-user 128-bit
// random salt and 200,000 iterations (an interim floor; current OWASP guidance
// for PBKDF2-HMAC-SHA256 is higher). Verification is constant-time.
const (
	pbkdf2Iterations = 200_000
	pbkdf2KeyLength  = 32
	pbkdf2SaltLength = 16
	pbkdf2Prefix     = "pbkdf2-sha256"
)

// hashPassword derives an encoded hash of the form
// "pbkdf2-sha256$<iterations>$<salt>$<key>" (salt and key are base64 raw std).
func hashPassword(password string, iterations int) (string, error) {
	if iterations <= 0 {
		iterations = pbkdf2Iterations
	}
	salt := make([]byte, pbkdf2SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: password salt: %w", err)
	}
	key := pbkdf2SHA256([]byte(password), salt, iterations, pbkdf2KeyLength)
	return fmt.Sprintf("%s$%d$%s$%s", pbkdf2Prefix, iterations,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

// verifyPassword reports whether password matches encoded. Malformed or
// unsupported hashes never match.
func verifyPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != pbkdf2Prefix {
		return false
	}
	iterations, err := strconv.Atoi(parts[1])
	if err != nil || iterations <= 0 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil || len(salt) == 0 {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil || len(want) == 0 {
		return false
	}
	got := pbkdf2SHA256([]byte(password), salt, iterations, len(want))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// pbkdf2SHA256 is RFC 2898 PBKDF2 with HMAC-SHA256 as the pseudo-random
// function.
func pbkdf2SHA256(password, salt []byte, iterations, keyLen int) []byte {
	prf := hmac.New(sha256.New, password)
	hashLen := prf.Size()
	blocks := (keyLen + hashLen - 1) / hashLen
	derived := make([]byte, 0, blocks*hashLen)
	var index [4]byte
	for block := 1; block <= blocks; block++ {
		prf.Reset()
		prf.Write(salt)
		binary.BigEndian.PutUint32(index[:], uint32(block))
		prf.Write(index[:])
		u := prf.Sum(nil)
		t := make([]byte, hashLen)
		copy(t, u)
		for n := 1; n < iterations; n++ {
			prf.Reset()
			prf.Write(u)
			u = prf.Sum(u[:0])
			for i := range t {
				t[i] ^= u[i]
			}
		}
		derived = append(derived, t...)
	}
	return derived[:keyLen]
}
