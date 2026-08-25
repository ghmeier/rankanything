package auth_test

import (
	"strings"
	"testing"

	"github.com/ghmeier/rankanything/internal/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

func TestHashPasswordRejectsShortInput(t *testing.T) {
	_, err := auth.HashPassword("short")
	assert.ErrorIs(t, err, auth.ErrWeakPassword)
}

func TestPasswordRoundTrip(t *testing.T) {
	hash, err := auth.HashPassword("supersecret")
	require.NoError(t, err)
	assert.True(t, auth.CheckPassword(hash, "supersecret"))
	assert.False(t, auth.CheckPassword(hash, "wrongpassword"))
}

func TestPasswordsLongerThan72BytesAreDistinguishable(t *testing.T) {
	base := strings.Repeat("a", 72)
	hash, err := auth.HashPassword(base + "X")
	require.NoError(t, err)

	assert.True(t, auth.CheckPassword(hash, base+"X"))
	assert.False(t, auth.CheckPassword(hash, base+"Y"),
		"passwords differing only after 72 bytes must not match")
}

func TestCheckPasswordAcceptsLegacyRawBcryptHash(t *testing.T) {
	plain := "supersecret"
	legacy, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.MinCost)
	require.NoError(t, err)

	assert.True(t, auth.CheckPassword(string(legacy), plain),
		"legacy hashes created before prehash migration must still verify")
}
