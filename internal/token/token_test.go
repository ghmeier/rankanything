package token_test

import (
	"strings"
	"testing"
	"time"

	"github.com/ghmeier/rankanything/internal/token"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateReturnsHighEntropyUniquePlaintexts(t *testing.T) {
	t.Parallel()

	seen := make(map[string]struct{})
	for range 1000 {
		plaintext, _, _, err := token.Generate(token.VerificationTTL)
		require.NoError(t, err)

		assert.GreaterOrEqual(t, len(plaintext), 40, "plaintext should carry at least 256 bits of entropy once encoded")
		_, dup := seen[plaintext]
		assert.False(t, dup, "generate should not repeat a plaintext across calls")
		seen[plaintext] = struct{}{}
	}
}

func TestGenerateHashDoesNotContainPlaintext(t *testing.T) {
	t.Parallel()

	plaintext, hash, _, err := token.Generate(token.VerificationTTL)
	require.NoError(t, err)

	assert.NotEqual(t, plaintext, hash)
	assert.NotContains(t, hash, plaintext)
	assert.Equal(t, token.Hash(plaintext), hash, "the stored hash must be reproducible from the plaintext for lookups")
}

func TestGenerateSetsExpiryFromTTL(t *testing.T) {
	t.Parallel()

	before := time.Now()
	_, _, expiresAt, err := token.Generate(30 * time.Minute)
	require.NoError(t, err)
	after := time.Now()

	assert.True(t, expiresAt.After(before.Add(30*time.Minute)))
	assert.True(t, !expiresAt.After(after.Add(30*time.Minute)))
}

func TestVerifySucceedsForFreshUnconsumedToken(t *testing.T) {
	t.Parallel()

	plaintext, hash, expiresAt, err := token.Generate(token.VerificationTTL)
	require.NoError(t, err)

	ok := token.Verify(plaintext, hash, expiresAt, time.Now(), false)

	assert.True(t, ok)
}

func TestVerifyFailsOnWrongPlaintext(t *testing.T) {
	t.Parallel()

	_, hash, expiresAt, err := token.Generate(token.VerificationTTL)
	require.NoError(t, err)

	ok := token.Verify("not-the-real-token", hash, expiresAt, time.Now(), false)

	assert.False(t, ok)
}

func TestVerifyFailsOnceExpired(t *testing.T) {
	t.Parallel()

	plaintext, hash, expiresAt, err := token.Generate(time.Minute)
	require.NoError(t, err)

	ok := token.Verify(plaintext, hash, expiresAt, expiresAt.Add(time.Second), false)

	assert.False(t, ok)
}

func TestVerifyFailsWhenAlreadyConsumed(t *testing.T) {
	t.Parallel()

	plaintext, hash, expiresAt, err := token.Generate(token.VerificationTTL)
	require.NoError(t, err)

	ok := token.Verify(plaintext, hash, expiresAt, time.Now(), true)

	assert.False(t, ok)
}

func TestVerifyDoesNotDistinguishExpiredFromConsumed(t *testing.T) {
	t.Parallel()

	plaintext, hash, expiresAt, err := token.Generate(time.Minute)
	require.NoError(t, err)

	expired := token.Verify(plaintext, hash, expiresAt, expiresAt.Add(time.Second), false)
	consumed := token.Verify(plaintext, hash, expiresAt, expiresAt.Add(-time.Second), true)

	assert.Equal(t, expired, consumed)
	assert.False(t, expired)
}

func TestResetTTLMatchesVerificationTTL(t *testing.T) {
	t.Parallel()

	assert.Equal(t, token.VerificationTTL, token.ResetTTL)
}

func TestPlaintextIsURLSafe(t *testing.T) {
	t.Parallel()

	plaintext, _, _, err := token.Generate(token.VerificationTTL)
	require.NoError(t, err)

	assert.False(t, strings.ContainsAny(plaintext, "+/= "), "plaintext must be safe to embed in a query string unescaped")
}
