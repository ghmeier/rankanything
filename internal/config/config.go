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

	R2AccountID      string
	R2AccessKeyID    string
	R2SecretAccessKey string
	R2BucketName     string
	R2PublicURL      string
}

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

		R2AccountID:      os.Getenv("R2_ACCOUNT_ID"),
		R2AccessKeyID:    os.Getenv("R2_ACCESS_KEY_ID"),
		R2SecretAccessKey: os.Getenv("R2_SECRET_ACCESS_KEY"),
		R2BucketName:     os.Getenv("R2_BUCKET_NAME"),
		R2PublicURL:      os.Getenv("R2_PUBLIC_URL"),
	}
	if c.DatabaseURL == "" {
		return Config{}, fmt.Errorf("config: DATABASE_URL is required")
	}
	return c, nil
}

func (c Config) IsProduction() bool { return strings.EqualFold(c.Env, "production") }

func (c Config) HasR2() bool {
	return c.R2AccountID != "" && c.R2AccessKeyID != "" && c.R2SecretAccessKey != "" && c.R2BucketName != "" && c.R2PublicURL != ""
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
