package services_test

import (
	"context"
	"testing"

	"github.com/ghmeier/rankanything/internal/db"
	"github.com/ghmeier/rankanything/internal/services"
	"github.com/ghmeier/rankanything/internal/testsupport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newThemeTestUser creates a user directly through Queries, bypassing
// UserService's own Register (which also logs the session in) — these tests
// only care about the theme_preference column.
func newThemeTestUser(t *testing.T, env *testsupport.Env, email string) db.User {
	t.Helper()
	user, err := env.Queries.CreateUser(context.Background(), db.CreateUserParams{
		Email:        email,
		PasswordHash: "original-hash",
	})
	require.NoError(t, err)
	return user
}

func TestNewlyRegisteredUserDefaultsToSystemTheme(t *testing.T) {
	t.Parallel()

	env := testsupport.NewEnv(t)
	user := newThemeTestUser(t, env, "defaults@example.com")

	assert.Equal(t, db.UserThemePreferenceSystem, user.ThemePreference)
}

func TestUpdateThemePreferenceRoundTrips(t *testing.T) {
	t.Parallel()

	env := testsupport.NewEnv(t)
	ctx := context.Background()
	user := newThemeTestUser(t, env, "roundtrip@example.com")

	updated, err := env.App.UserSvc.UpdateThemePreference(ctx, services.UpdateThemePreferenceRequest{
		UserID:     user.ID,
		Preference: db.UserThemePreferenceDark,
	})
	require.NoError(t, err)
	assert.Equal(t, db.UserThemePreferenceDark, updated.ThemePreference)

	stored, err := env.Queries.GetUserByID(ctx, user.ID)
	require.NoError(t, err)
	assert.Equal(t, db.UserThemePreferenceDark, stored.ThemePreference)
}

func TestUpdateThemePreferenceRejectsAnInvalidValue(t *testing.T) {
	t.Parallel()

	env := testsupport.NewEnv(t)
	ctx := context.Background()
	user := newThemeTestUser(t, env, "invalid@example.com")

	_, err := env.App.UserSvc.UpdateThemePreference(ctx, services.UpdateThemePreferenceRequest{
		UserID:     user.ID,
		Preference: db.UserThemePreference("sepia"),
	})

	assert.ErrorIs(t, err, services.ErrInvalidThemePreference)

	stored, err := env.Queries.GetUserByID(ctx, user.ID)
	require.NoError(t, err)
	assert.Equal(t, db.UserThemePreferenceSystem, stored.ThemePreference, "a rejected update must not touch the stored value")
}
