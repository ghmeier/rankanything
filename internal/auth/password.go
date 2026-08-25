package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidCredentials = errors.New("Email or password is incorrect.")
	ErrWeakPassword       = errors.New("at least 8 characters")
	ErrInvalidEmail       = errors.New("Enter a valid email address.")
)

// prehash defeats bcrypt's 72-byte truncation by feeding it a fixed-length
// SHA-256 hex digest (64 bytes) instead of the raw password.
func prehash(plain string) []byte {
	h := sha256.Sum256([]byte(plain))
	return []byte(hex.EncodeToString(h[:]))
}

// HashPassword returns a bcrypt hash suitable for users.password_hash.
func HashPassword(plain string) (string, error) {
	if len([]rune(plain)) < 8 {
		return "", ErrWeakPassword
	}
	h, err := bcrypt.GenerateFromPassword(prehash(plain), bcrypt.DefaultCost)
	return string(h), err
}

// CheckPassword reports whether plain matches the stored hash. It tries the
// current SHA-256+bcrypt scheme first, then falls back to raw bcrypt for
// hashes created before the prehash migration. Callers that see a successful
// fallback should re-hash the password with HashPassword to upgrade the row.
func CheckPassword(hash, plain string) bool {
	if bcrypt.CompareHashAndPassword([]byte(hash), prehash(plain)) == nil {
		return true
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}

// NormalizeEmail trims and lowercases; the column is citext, this keeps the
// value we store tidy.
func NormalizeEmail(email string) (string, error) {
	e := strings.ToLower(strings.TrimSpace(email))
	at := strings.Index(e, "@")
	if at <= 0 || at == len(e)-1 || strings.Contains(e, " ") || !strings.Contains(e[at:], ".") {
		return "", ErrInvalidEmail
	}
	return e, nil
}
