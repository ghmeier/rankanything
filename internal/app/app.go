// Package app contains all the wiring for routes, middleware, handlers, and services.
package app

import (
	"context"
	"io/fs"
	"log/slog"
	"net/http"

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

	UserSvc         *services.UserService
	RankingSvc      *services.RankingsService
	EmailSvc        email.Sender
	ShareSvc        *services.ShareService
	VerificationSvc *services.VerificationService
}

// Routes builds the fully wrapped handler for the app.
func (a *App) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(a.Static))))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		if err := a.Pool.Ping(r.Context()); err != nil {
			http.Error(w, "unhealthy", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("ok"))
	})

	// The component gallery is a development tool; it never runs in production.
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

// RequireUser ensures a logged in user session for the request. If
// none is found, redirect to the login page for GET request and return
// forbidden otherwise.
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
