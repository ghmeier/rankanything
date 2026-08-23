// Package token implements the single-use, expiring token primitive shared
// by email verification and password reset. It is deliberately
// storage-agnostic: it knows how to generate, hash, and verify a token, but
// nothing about where the hash, expiry, or consumed flag live. Callers own
// persistence (a database row, for rankanything) and pass the stored fields
// back into Verify.
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
	// encoding. 32 bytes (256 bits) is far beyond what's brute-forceable,
	// so a single fast SHA-256 hash is sufficient below — there's no
	// low-entropy secret here for an attacker to dictionary-attack offline,
	// unlike a user-chosen password.
	byteLength = 32

	// VerificationTTL is how long an email verification link stays valid.
	VerificationTTL = time.Hour

	// ResetTTL is how long a password reset link stays valid. It matches
	// VerificationTTL: both are bearer credentials mailed to an address the
	// user is expected to check within minutes of requesting them, so
	// there's no usability reason to widen the window, and a shorter-lived
	// link narrows the time an intercepted email (a shared inbox, a stale
	// browser tab) stays exploitable.
	ResetTTL = time.Hour
)

// Generate returns fresh token material: a URL-safe plaintext for the
// emailed link, and its hash plus an absolute expiry for the caller to
// persist. The plaintext is returned exactly once — embed it in the
// outgoing email and then let it go out of scope. Never store or log it.
func Generate(ttl time.Duration) (plaintext, hash string, expiresAt time.Time, err error) {
	buf := make([]byte, byteLength)
	if _, err := rand.Read(buf); err != nil {
		return "", "", time.Time{}, fmt.Errorf("token: generate: %w", err)
	}

	plaintext = base64.RawURLEncoding.EncodeToString(buf)
	return plaintext, Hash(plaintext), time.Now().Add(ttl), nil
}

// Hash returns the hex-encoded SHA-256 digest of plaintext. It's exported so
// a caller verifying a token supplied by the user (e.g. from a query
// parameter) can hash it themselves for a lookup, in addition to Verify's
// own use of it.
func Hash(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}

// Verify reports whether plaintext redeems a stored token: its hash matches
// hash (compared in constant time), it has not passed expiresAt, and it has
// not already been consumed. A consumed token and an expired token report
// identically — false — so a caller can't distinguish "too late" from
// "already used" from "wrong token" by branching on the result.
func Verify(plaintext, hash string, expiresAt, now time.Time, consumed bool) bool {
	matches := subtle.ConstantTimeCompare([]byte(Hash(plaintext)), []byte(hash)) == 1
	live := !consumed && now.Before(expiresAt)
	return matches && live
}
