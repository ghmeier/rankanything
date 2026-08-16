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
