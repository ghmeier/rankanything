// Package app contains all the wiring for routes, middleware, handlers, and services.
package app

import (
	"context"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ghmeier/rankanything/internal/auth"
	"github.com/ghmeier/rankanything/internal/constants"
	"github.com/ghmeier/rankanything/internal/db"
	"github.com/ghmeier/rankanything/internal/email"
	"github.com/ghmeier/rankanything/internal/services"
	"github.com/ghmeier/rankanything/internal/ui"
)

type App struct {
	Pool         *pgxpool.Pool
	Queries      *db.Queries
	Sessions     *auth.Sessions
	Logger       *slog.Logger
	Static       fs.FS
	IsProduction bool
	RateLimiter  *auth.RateLimiter

	UserSvc         *services.UserService
	RankingSvc      *services.RankingsService
	EmailSvc        email.Sender
	ShareSvc        *services.ShareService
	VerificationSvc *services.VerificationService
}

func (a *App) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(a.Static))))
	// Pings the database because the failure worth catching is a container
	// that booted but cannot reach Postgres. The timeout keeps a hung
	// database from stalling the check past the host's own deadline.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		if err := a.Pool.Ping(ctx); err != nil {
			a.Logger.Error("health check failed", "err", err)
			http.Error(w, "unhealthy", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("ok"))
	})

	if !a.IsProduction {
		mux.HandleFunc("GET /components", ui.ComponentsHandler)
	}

	a.registerAuthRoutes(mux)
	a.registerRankingRoutes(mux)
	a.registerPublicRoutes(mux)
	a.registerAccountRoutes(mux)

	return auth.Chain(mux,
		auth.Recover(a.Logger),
		auth.RequestLog(a.Logger),
		http.NewCrossOriginProtection().Handler,
		a.Sessions.LoadAndSave,
		auth.CSRF(a.Sessions),
	)
}

// RequireUser redirects an anonymous request to the login page.
func (a *App) RequireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.Sessions.UserID(r.Context()) == 0 {
			target := "/login"

			if r.Method == http.MethodGet {
				target += "?next=" + r.URL.Path
			}

			http.Redirect(w, r, target, http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireRankingAccess parses the ranking uuid from the path, confirms the
// session's user owns it, and resolves which version the request addresses.
func (a *App) RequireRankingAccess(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		rankingUUID, err := uuid.Parse(r.PathValue("uuid"))
		if err != nil {
			a.notFound(w, r)
			return
		}

		ranking, err := a.RankingSvc.GetRanking(ctx, rankingUUID)
		if err != nil {
			a.notFound(w, r)
			return
		}

		if userID := a.Sessions.UserID(ctx); userID == 0 || ranking.UserID != userID {
			a.notFound(w, r)
			return
		}

		version, err := a.RankingSvc.ResolveVersion(ctx, ranking, r.PathValue("short"))
		if err != nil {
			a.notFound(w, r)
			return
		}

		ctx = context.WithValue(ctx, constants.RankingUUIDKey, rankingUUID)
		ctx = context.WithValue(ctx, constants.RankingVersionKey, version)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
