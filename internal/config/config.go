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
	ResendAPIKey   string
	EmailFrom      string
	BaseURL        string
}

// Read configuration from the environment, including .env.
//
// A missing .env is not an error. Locally it carries the database URL and
// port; in production the host injects those as real environment variables
// and no such file is deployed, so requiring one here would crash-loop the
// container on every boot. Any other error reading the file is still worth
// surfacing, since it means a .env exists but could not be parsed.
func Load(logger *slog.Logger) (Config, error) {
	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		logger.Error("fatal", "err", err)
		return Config{}, err
	}

	c := Config{
		DatabaseURL:    os.Getenv("DATABASE_URL"),
		Addr:           ":" + envOr("PORT", "8001"),
		Env:            envOr("APP_ENV", "development"),
		SessionTimeout: 30 * 24 * time.Hour,
		ResendAPIKey:   os.Getenv("RESEND_API_KEY"),
		// TODO: Update the default sender here once we authenticate a domain.
		EmailFrom: envOr("EMAIL_FROM", "Rank Anything <onboarding@resend.dev>"),
		BaseURL:   envOr("BASE_URL", "http://localhost:8001"),
	}
	if c.DatabaseURL == "" {
		return Config{}, fmt.Errorf("config: DATABASE_URL is required")
	}
	return c, nil
}

func (c Config) IsProduction() bool { return strings.EqualFold(c.Env, "production") }

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
