// Package token implements the single-use, expiring token primitive shared
// by email verification and password reset.
package token

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"time"
)

const (
	// byteLength is the amount of randomness read from crypto/rand before
	// encoding.
	byteLength = 32

	// How long verification email links stay valid.
	VerificationTTL = time.Hour
	// How long password reset links stay valid.
	ResetTTL = time.Hour
)

// Generate returns a new token hash string with a ttl.
func Generate(ttl time.Duration) (plaintext, hash string, expiresAt time.Time, err error) {
	buf := make([]byte, byteLength)
	if _, err := rand.Read(buf); err != nil {
		return "", "", time.Time{}, fmt.Errorf("token: generate: %w", err)
	}

	plaintext = base64.RawURLEncoding.EncodeToString(buf)
	return plaintext, Hash(plaintext), time.Now().Add(ttl), nil
}

// Hash returns the hex-encoded SHA-256 digest of plaintext.
func Hash(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}

// Verify reports whether plaintext is a valid token: its hash matches
// hash, it has not passed expiresAt, and it has not already been consumed.
func Verify(plaintext, hash string, expiresAt, now time.Time, consumed bool) bool {
	matches := subtle.ConstantTimeCompare([]byte(Hash(plaintext)), []byte(hash)) == 1
	live := !consumed && now.Before(expiresAt)
	return matches && live
}
