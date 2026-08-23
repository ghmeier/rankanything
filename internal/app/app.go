// Package app wires the HTTP layer: routes, middleware, and handlers.
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
	"github.com/ghmeier/rankanything/internal/render"
	"github.com/ghmeier/rankanything/internal/services"
	"github.com/ghmeier/rankanything/internal/ui"
)

// App holds everything the handlers need.
type App struct {
	Pool     *pgxpool.Pool
	Queries  *db.Queries
	Sessions *auth.Sessions
	Render   *render.Renderer
	Logger   *slog.Logger
	Static   fs.FS

	// IsProduction drives the handful of things that must never run in a
	// live deployment (right now, just the /components dev route). It's
	// set once in cmd/rankanything/main.go from config.Config.IsProduction,
	// so there is exactly one source of truth for "are we in production"
	// rather than this package re-reading APP_ENV itself.
	IsProduction bool

	UserSvc    *services.UserService
	RankingSvc *services.RankingsService

	// Wired to nil until their owning wave 3/4 branch lands. Declaring the
	// field here means each branch changes its own line rather than every
	// sibling branch adding a field to this struct in parallel.
	EmailSvc email.Sender           // feat/auth-flows: verification + password-reset mail
	ShareSvc *services.ShareService // feat/public-share: is_public / public_slug toggling

	// VerificationSvc implements email verification and password reset on
	// top of EmailSvc. It's a separate field (rather than folding into
	// UserSvc) because it's the one place besides EmailSvc itself that this
	// branch adds to App, and keeping it separate means a sibling branch
	// never has to touch UserService's constructor call in main.go.
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

	return auth.Chain(mux,
		auth.Recover(a.Logger),
		auth.RequestLog(a.Logger),
		http.NewCrossOriginProtection().Handler,
		a.Sessions.LoadAndSave,
		auth.CSRF(a.Sessions),
	)
}

// RequireUser gates handlers that only make sense for a signed-in user. The
// post-login "next" redirect only carries the original path forward when
// the gated request was itself a GET — after signing in, the browser
// replays "next" as a GET, so a path that only answers POST (like /new)
// would be a dead end if it were forwarded.
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
// session's user owns it, and resolves which version the request addresses:
// the live version for /r/{uuid}, or the version pinned by /r/{uuid}/v/{short}.
// Both are stashed in the request context under constants.RankingUUIDKey and
// constants.RankingVersionKey — handlers on these routes read from context,
// never from PathValue, and can assume access is already granted.
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
