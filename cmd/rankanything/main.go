package main

import (
	"context"
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
	"github.com/ghmeier/rankanything/internal/app"
	"github.com/ghmeier/rankanything/internal/auth"
	"github.com/ghmeier/rankanything/internal/config"
	"github.com/ghmeier/rankanything/internal/db"
	"github.com/ghmeier/rankanything/internal/render"
	"github.com/jackc/pgx/v5/pgxpool"
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

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return err
	}

	sm := scs.New()
	sm.Store = pgxstore.New(pool)
	sm.Lifetime = cfg.SessionTimeout
	auth.CookieDefaults(sm, cfg.IsProduction())

	renderer, err := render.New(assets.Templates())
	if err != nil {
		return err
	}

	application := &app.App{
		Pool:     pool,
		Queries:  db.New(pool),
		Sessions: auth.NewSessions(sm),
		Render:   renderer,
		Logger:   logger,
		Static:   assets.Static(),
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
