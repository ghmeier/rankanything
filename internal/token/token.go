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
	byteLength = 32

	VerificationTTL = time.Hour
	ResetTTL        = time.Hour
)

func Generate(ttl time.Duration) (plaintext, hash string, expiresAt time.Time, err error) {
	buf := make([]byte, byteLength)
	if _, err := rand.Read(buf); err != nil {
		return "", "", time.Time{}, fmt.Errorf("token: generate: %w", err)
	}

	plaintext = base64.RawURLEncoding.EncodeToString(buf)
	return plaintext, Hash(plaintext), time.Now().Add(ttl), nil
}

func Hash(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}

func Verify(plaintext, hash string, expiresAt, now time.Time, consumed bool) bool {
	matches := subtle.ConstantTimeCompare([]byte(Hash(plaintext)), []byte(hash)) == 1
	live := !consumed && now.Before(expiresAt)
	return matches && live
}
