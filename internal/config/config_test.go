package config_test

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/ghmeier/rankanything/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeEnv drops a .env file in a fresh temp directory and chdirs the test
// there, so config.Load reads that file rather than the repository's own.
func writeEnv(t *testing.T, contents string) {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"), []byte(contents), 0o600))
	t.Chdir(dir)
}

// unsetenv truly removes key from the process environment for the test's
// duration, then restores whatever was there before. Unlike t.Setenv(key,
// ""), this leaves the key absent (not present-but-empty) so
// godotenv.Load's "don't override an existing var" behavior doesn't shadow
// the value in a test's .env file — a var this test set with t.Setenv in an
// earlier run would otherwise leak forward, since godotenv.Load mutates the
// real process environment and only t.Setenv's own restoration is
// test-scoped.
func unsetenv(t *testing.T, key string) {
	t.Helper()
	old, ok := os.LookupEnv(key)
	require.NoError(t, os.Unsetenv(key))
	t.Cleanup(func() {
		if ok {
			_ = os.Setenv(key, old)
		} else {
			_ = os.Unsetenv(key)
		}
	})
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestLoadDefaultsEmailFieldsWhenUnset(t *testing.T) {
	writeEnv(t, "DATABASE_URL=postgres://example\n")
	unsetenv(t, "DATABASE_URL")
	t.Setenv("RESEND_API_KEY", "")
	t.Setenv("EMAIL_FROM", "")
	t.Setenv("BASE_URL", "")

	cfg, err := config.Load(discardLogger())
	require.NoError(t, err)

	assert.Empty(t, cfg.ResendAPIKey, "no key configured means internal/email falls back to the dev sink")
	assert.Equal(t, "Rank Anything <onboarding@resend.dev>", cfg.EmailFrom)
	assert.Equal(t, "http://localhost:8001", cfg.BaseURL)
}

func TestLoadReadsEmailFieldsFromEnv(t *testing.T) {
	writeEnv(t, "DATABASE_URL=postgres://example\n")
	unsetenv(t, "DATABASE_URL")
	t.Setenv("RESEND_API_KEY", "re_test_key")
	t.Setenv("EMAIL_FROM", "Someone <someone@example.com>")
	t.Setenv("BASE_URL", "https://rankanything.app")

	cfg, err := config.Load(discardLogger())
	require.NoError(t, err)

	assert.Equal(t, "re_test_key", cfg.ResendAPIKey)
	assert.Equal(t, "Someone <someone@example.com>", cfg.EmailFrom)
	assert.Equal(t, "https://rankanything.app", cfg.BaseURL)
}

func TestLoadSucceedsWithNoEnvFileWhenTheEnvironmentSuppliesEverything(t *testing.T) {
	// Production has no .env: the host injects real environment variables,
	// so an empty working directory has to be a valid way to boot.
	t.Chdir(t.TempDir())
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("BASE_URL", "https://rankanything.example")

	cfg, err := config.Load(discardLogger())
	require.NoError(t, err)

	assert.Equal(t, "postgres://example", cfg.DatabaseURL)
	assert.Equal(t, "https://rankanything.example", cfg.BaseURL)
}

func TestLoadStillFailsWithoutDatabaseURL(t *testing.T) {
	writeEnv(t, "RESEND_API_KEY=re_test_key\n")
	// godotenv.Load sets process-wide env vars that outlive the test (unlike
	// t.Setenv), so an earlier test's DATABASE_URL can otherwise leak here.
	unsetenv(t, "DATABASE_URL")

	_, err := config.Load(discardLogger())

	assert.Error(t, err, "DATABASE_URL stays required even though the email fields are optional")
}
