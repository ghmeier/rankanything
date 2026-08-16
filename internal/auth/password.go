package auth

import (
	"errors"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidCredentials = errors.New("Email or password is incorrect.")
	ErrWeakPassword       = errors.New("at least 8 characters")
	ErrInvalidEmail       = errors.New("Enter a valid email address.")
)

// HashPassword returns a bcrypt hash suitable for users.password_hash.
func HashPassword(plain string) (string, error) {
	if len([]rune(plain)) < 8 {
		return "", ErrWeakPassword
	}
	h, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	return string(h), err
}

// CheckPassword reports whether plain matches the stored hash.
func CheckPassword(hash, plain string) bool {
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
