package main

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/alexedwards/scs/pgxstore"
	"github.com/alexedwards/scs/v2"
	"github.com/ghmeier/rankanything/assets"
	"github.com/ghmeier/rankanything/db/migrations"
	"github.com/ghmeier/rankanything/internal/app"
	"github.com/ghmeier/rankanything/internal/auth"
	"github.com/ghmeier/rankanything/internal/config"
	"github.com/ghmeier/rankanything/internal/db"
	"github.com/ghmeier/rankanything/internal/email"
	"github.com/ghmeier/rankanything/internal/services"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	if err := run(logger); err != nil {
		logger.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load(logger)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	p, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer p.Close()
	if err := p.Ping(ctx); err != nil {
		return err
	}

	if err := migrate(ctx, logger, cfg); err != nil {
		return err
	}

	sm := scs.New()
	sm.Store = pgxstore.New(p)
	sm.Lifetime = cfg.SessionTimeout
	auth.CookieDefaults(sm, cfg.IsProduction())

	q := db.New(p)
	s := auth.NewSessions(sm)
	emailSvc := email.NewSender(cfg.ResendAPIKey, cfg.EmailFrom, logger)

	rl := auth.NewRateLimiter()
	defer rl.Stop()

	application := &app.App{
		Pool:         p,
		Queries:      q,
		Sessions:     s,
		Logger:       logger,
		Static:       assets.Static(),
		IsProduction: cfg.IsProduction(),
		RateLimiter:  rl,
		UserSvc:      &services.UserService{Queries: q, Sessions: s},
		RankingSvc:   &services.RankingsService{Queries: q, Pool: p},
		EmailSvc:     emailSvc,
		ShareSvc:     &services.ShareService{Queries: q, BaseURL: cfg.BaseURL},
		VerificationSvc: &services.VerificationService{
			Queries:  q,
			Sender:   emailSvc,
			Sessions: s,
			BaseURL:  cfg.BaseURL,
		},
	}

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           application.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	logger.Info("listening", "addr", cfg.Addr, "env", cfg.Env)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func migrate(ctx context.Context, logger *slog.Logger, cfg config.Config) error {
	sqlDB, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer sqlDB.Close()

	goose.SetLogger(goose.NopLogger())
	logger.Info("applying migrations")
	if err := migrations.Up(ctx, sqlDB); err != nil {
		return err
	}
	logger.Info("migrations up to date")
	return nil
}
