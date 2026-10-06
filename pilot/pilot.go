// Package pilot issues and checks the anonymous pilot token. Only its
// SHA-256 is ever stored; the raw token is never logged.
package pilot

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
)

const (
	TokenLen = 22 // base64url of 16 bytes, unpadded
	Header   = "X-Pilot-Token"
)

// New returns a fresh token: 16 random bytes, base64url without padding.
func New() string {
	var b [16]byte
	rand.Read(b[:]) // never fails (crypto/rand panics instead)
	return base64.RawURLEncoding.EncodeToString(b[:])
}

// Valid reports whether tok is exactly TokenLen base64url characters that
// decode to exactly 16 bytes with zero padding bits (strict: one token per
// 16 bytes). The length check runs first, so huge inputs are cheap.
func Valid(tok string) bool {
	if len(tok) != TokenLen {
		return false
	}
	for i := range len(tok) {
		c := tok[i]
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	b, err := base64.RawURLEncoding.Strict().DecodeString(tok)
	return err == nil && len(b) == 16
}

// Hash returns hex(sha256(tok)); callers pass only Valid tokens.
func Hash(tok string) string {
	sum := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(sum[:])
}
