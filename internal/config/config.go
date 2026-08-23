// Package config reads runtime settings from the environment.
package config

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	DatabaseURL    string
	Addr           string
	Env            string
	SessionTimeout time.Duration

	// ResendAPIKey, EmailFrom, and BaseURL configure outgoing mail. All
	// three are optional: an empty ResendAPIKey means internal/email picks
	// the dev log sink instead of the Resend adapter, so local development
	// and CI need neither a key nor network access to send verification and
	// password-reset mail.
	ResendAPIKey string
	EmailFrom    string
	BaseURL      string
}

// Load reads configuration, failing fast when DATABASE_URL is absent.
func Load(logger *slog.Logger) (Config, error) {
	err := godotenv.Load()
	if err != nil {
		logger.Error("fatal", "err", err)
		return Config{}, err
	}

	c := Config{
		DatabaseURL:    os.Getenv("DATABASE_URL"),
		Addr:           envOr("PORT", ":8001"),
		Env:            envOr("APP_ENV", "development"),
		SessionTimeout: 30 * 24 * time.Hour,

		// Optional: os.Getenv rather than envOr, so an unset key stays "" and
		// internal/email.NewSender falls back to its dev sink.
		ResendAPIKey: os.Getenv("RESEND_API_KEY"),
		// onboarding@resend.dev is Resend's own sandbox sender, usable
		// without verifying a domain first — a real default rather than a
		// placeholder, so RESEND_API_KEY alone is enough to send a live
		// test email. chore/deploy replaces it once the app's domain is
		// verified with Resend.
		EmailFrom: envOr("EMAIL_FROM", "Rank Anything <onboarding@resend.dev>"),
		BaseURL:   envOr("BASE_URL", "http://localhost:8001"),
	}
	if c.DatabaseURL == "" {
		return Config{}, fmt.Errorf("config: DATABASE_URL is required")
	}
	return c, nil
}

// IsProduction drives cookie hardening.
func (c Config) IsProduction() bool { return strings.EqualFold(c.Env, "production") }

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
