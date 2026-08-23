package services_test

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ghmeier/rankanything/internal/auth"
	"github.com/ghmeier/rankanything/internal/db"
	"github.com/ghmeier/rankanything/internal/email"
	"github.com/ghmeier/rankanything/internal/services"
	"github.com/ghmeier/rankanything/internal/testsupport"
	"github.com/ghmeier/rankanything/internal/token"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newVerificationTestUser creates a user directly through Queries, bypassing
// UserService/HTTP entirely — these tests exercise VerificationService on
// its own, the same way rankings_test.go exercises RankingsService.
func newVerificationTestUser(t *testing.T, env *testsupport.Env, email string) db.User {
	t.Helper()
	user, err := env.Queries.CreateUser(context.Background(), db.CreateUserParams{
		Email:        email,
		PasswordHash: "original-hash",
	})
	require.NoError(t, err)
	return user
}

// extractToken pulls the plaintext token back out of a mailed message's
// link — the same value a user would copy out of their inbox by clicking
// it — so a test can feed it back into RedeemEmailVerification or
// RedeemPasswordReset without ever touching the stored hash directly.
func extractToken(t *testing.T, msg email.Message) string {
	t.Helper()
	for _, line := range strings.Split(msg.Text, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "http") {
			continue
		}
		u, err := url.Parse(line)
		require.NoError(t, err)
		return u.Query().Get("token")
	}
	t.Fatal("no link found in mailed message")
	return ""
}

func TestSendVerificationEmailStoresAHashedTokenAndMailsThePlaintextLink(t *testing.T) {
	t.Parallel()

	env := testsupport.NewEnv(t)
	ctx := context.Background()
	user := newVerificationTestUser(t, env, "verify@example.com")

	require.NoError(t, env.App.VerificationSvc.SendVerificationEmail(ctx, user.ID, user.Email))

	sent := env.EmailSink.Sent()
	require.Len(t, sent, 1)
	assert.Equal(t, user.Email, sent[0].To)

	plaintext := extractToken(t, sent[0])
	assert.NotEmpty(t, plaintext)

	row, err := env.Queries.GetEmailVerificationByTokenHash(ctx, token.Hash(plaintext))
	require.NoError(t, err)
	assert.Equal(t, user.ID, row.UserID)
	assert.False(t, row.IsVerified)
}

func TestRedeemEmailVerificationMarksTheUserVerified(t *testing.T) {
	t.Parallel()

	env := testsupport.NewEnv(t)
	ctx := context.Background()
	user := newVerificationTestUser(t, env, "redeem@example.com")
	require.NoError(t, env.App.VerificationSvc.SendVerificationEmail(ctx, user.ID, user.Email))
	plaintext := extractToken(t, env.EmailSink.Sent()[0])

	require.NoError(t, env.App.VerificationSvc.RedeemEmailVerification(ctx, plaintext))

	updated, err := env.Queries.GetUserByID(ctx, user.ID)
	require.NoError(t, err)
	assert.True(t, updated.EmailVerified)
}

func TestRedeemEmailVerificationRejectsAnUnknownToken(t *testing.T) {
	t.Parallel()

	env := testsupport.NewEnv(t)

	err := env.App.VerificationSvc.RedeemEmailVerification(context.Background(), "not-a-real-token")

	assert.ErrorIs(t, err, services.ErrTokenInvalid)
}

func TestRedeemEmailVerificationRejectsAnAlreadyUsedToken(t *testing.T) {
	t.Parallel()

	env := testsupport.NewEnv(t)
	ctx := context.Background()
	user := newVerificationTestUser(t, env, "reused@example.com")
	require.NoError(t, env.App.VerificationSvc.SendVerificationEmail(ctx, user.ID, user.Email))
	plaintext := extractToken(t, env.EmailSink.Sent()[0])
	require.NoError(t, env.App.VerificationSvc.RedeemEmailVerification(ctx, plaintext))

	err := env.App.VerificationSvc.RedeemEmailVerification(ctx, plaintext)

	assert.ErrorIs(t, err, services.ErrTokenInvalid)
}

func TestRedeemEmailVerificationRejectsAnExpiredToken(t *testing.T) {
	t.Parallel()

	env := testsupport.NewEnv(t)
	ctx := context.Background()
	user := newVerificationTestUser(t, env, "expired@example.com")

	plaintext, hash, _, err := token.Generate(token.VerificationTTL)
	require.NoError(t, err)
	_, err = env.Queries.CreateEmailVerification(ctx, db.CreateEmailVerificationParams{
		TokenHash: hash,
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(-time.Minute), Valid: true},
		UserID:    user.ID,
	})
	require.NoError(t, err)

	err = env.App.VerificationSvc.RedeemEmailVerification(ctx, plaintext)

	assert.ErrorIs(t, err, services.ErrTokenInvalid)
}

func TestRequestPasswordResetSendsALinkForARegisteredEmail(t *testing.T) {
	t.Parallel()

	env := testsupport.NewEnv(t)
	user := newVerificationTestUser(t, env, "reset@example.com")

	require.NoError(t, env.App.VerificationSvc.RequestPasswordReset(context.Background(), user.Email))

	sent := env.EmailSink.Sent()
	require.Len(t, sent, 1)
	assert.Equal(t, user.Email, sent[0].To)
}

func TestRequestPasswordResetIsSilentForAnUnregisteredEmail(t *testing.T) {
	t.Parallel()

	env := testsupport.NewEnv(t)

	err := env.App.VerificationSvc.RequestPasswordReset(context.Background(), "ghost@example.com")

	assert.NoError(t, err)
	assert.Empty(t, env.EmailSink.Sent())
}

func TestRedeemPasswordResetChangesThePasswordHash(t *testing.T) {
	t.Parallel()

	env := testsupport.NewEnv(t)
	ctx := context.Background()
	user := newVerificationTestUser(t, env, "changeme@example.com")
	require.NoError(t, env.App.VerificationSvc.RequestPasswordReset(ctx, user.Email))
	plaintext := extractToken(t, env.EmailSink.Sent()[0])
	newHash, err := auth.HashPassword("supersecret2")
	require.NoError(t, err)

	require.NoError(t, env.App.VerificationSvc.RedeemPasswordReset(ctx, plaintext, newHash))

	updated, err := env.Queries.GetUserByID(ctx, user.ID)
	require.NoError(t, err)
	assert.Equal(t, newHash, updated.PasswordHash)
	assert.True(t, auth.CheckPassword(updated.PasswordHash, "supersecret2"))
}

func TestRedeemPasswordResetRejectsAnAlreadyUsedToken(t *testing.T) {
	t.Parallel()

	env := testsupport.NewEnv(t)
	ctx := context.Background()
	user := newVerificationTestUser(t, env, "reusedreset@example.com")
	require.NoError(t, env.App.VerificationSvc.RequestPasswordReset(ctx, user.Email))
	plaintext := extractToken(t, env.EmailSink.Sent()[0])
	newHash, err := auth.HashPassword("supersecret2")
	require.NoError(t, err)
	require.NoError(t, env.App.VerificationSvc.RedeemPasswordReset(ctx, plaintext, newHash))

	err = env.App.VerificationSvc.RedeemPasswordReset(ctx, plaintext, newHash)

	assert.ErrorIs(t, err, services.ErrTokenInvalid)
}

func TestRedeemPasswordResetRejectsAnExpiredToken(t *testing.T) {
	t.Parallel()

	env := testsupport.NewEnv(t)
	ctx := context.Background()
	user := newVerificationTestUser(t, env, "expiredreset@example.com")

	plaintext, hash, _, err := token.Generate(token.ResetTTL)
	require.NoError(t, err)
	_, err = env.Queries.CreatePasswordReset(ctx, db.CreatePasswordResetParams{
		TokenHash: hash,
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(-time.Minute), Valid: true},
		UserID:    user.ID,
	})
	require.NoError(t, err)
	newHash, err := auth.HashPassword("supersecret2")
	require.NoError(t, err)

	err = env.App.VerificationSvc.RedeemPasswordReset(ctx, plaintext, newHash)

	assert.ErrorIs(t, err, services.ErrTokenInvalid)
}
