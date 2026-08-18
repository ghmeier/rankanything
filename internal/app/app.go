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
	"github.com/ghmeier/rankanything/internal/render"
	"github.com/ghmeier/rankanything/internal/services"
)

// App holds everything the handlers need.
type App struct {
	Pool       *pgxpool.Pool
	Queries    *db.Queries
	Sessions   *auth.Sessions
	Render     *render.Renderer
	Logger     *slog.Logger
	Static     fs.FS
	UserSvc    *services.UserService
	RankingSvc *services.RankingsService
}

// Routes builds the fully wrapped handler for the app.
func (a *App) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(a.Static))))

	// Builder — the landing page is the product.
	mux.HandleFunc("GET /{$}", a.handleHome)
	mux.HandleFunc("GET /new", a.handleNew)
	mux.Handle("GET /r/{slug}", a.RequireRankingAccess(http.HandlerFunc(a.handleViewRanking)))
	mux.Handle("POST /r/{slug}", a.RequireRankingAccess(http.HandlerFunc(a.handleUpdateRanking)))
	mux.Handle("POST /r/{slug}/save", a.RequireRankingAccess(http.HandlerFunc(a.handleSave)))
	mux.Handle("POST /r/{slug}/items", a.RequireRankingAccess(http.HandlerFunc(a.handleAddItem)))
	mux.Handle("DELETE /r/{slug}/items/{itemID}", a.RequireRankingAccess(http.HandlerFunc(a.handleDeleteItem)))
	mux.Handle("POST /r/{slug}/tiers", a.RequireRankingAccess(http.HandlerFunc(a.handleAddTier)))
	mux.Handle("PUT /r/{slug}/tiers/{tierID}", a.RequireRankingAccess(http.HandlerFunc(a.handleUpdateTier)))
	mux.Handle("POST /r/{slug}/tiers/{tierID}/edit", a.RequireRankingAccess(http.HandlerFunc(a.handleEditTier)))
	mux.Handle("DELETE /r/{slug}/tiers/{tierID}", a.RequireRankingAccess(http.HandlerFunc(a.handleDeleteTier)))
	mux.Handle("PUT /r/{slug}/placements", a.RequireRankingAccess(http.HandlerFunc(a.handleSetPlacements)))
	mux.Handle("GET /rankings", a.RequireUser(http.HandlerFunc(a.handleRankings)))

	// Auth.
	mux.HandleFunc("GET /register", a.handleRegisterForm)
	mux.HandleFunc("POST /register", a.handleRegister)
	mux.HandleFunc("GET /login", a.handleLoginForm)
	mux.HandleFunc("POST /login", a.handleLogin)
	mux.HandleFunc("POST /logout", a.handleLogout)

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		if err := a.Pool.Ping(r.Context()); err != nil {
			http.Error(w, "unhealthy", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("ok"))
	})

	return auth.Chain(mux,
		auth.Recover(a.Logger),
		auth.RequestLog(a.Logger),
		http.NewCrossOriginProtection().Handler,
		a.Sessions.LoadAndSave,
		auth.CSRF(a.Sessions),
	)
}

// RequireUser gates handlers that only make sense for a signed-in user.
func (a *App) RequireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.Sessions.UserID(r.Context()) == 0 {
			http.Redirect(w, r, "/login?next="+r.URL.Path, http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})

}

func (a *App) RequireRankingAccess(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		slugStr := r.PathValue("slug")
		if slugStr == "" {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		slug, err := uuid.Parse(slugStr)
		if err != nil {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		userID := a.Sessions.UserID(ctx)
		draftKeys := a.Sessions.Drafts(ctx)

		if userId != 0 {

		}

		ctx = context.WithValue(ctx, constants.SlugKey, slug)

		next.ServeHTTP(w, r.WithContext(ctx))
	})

}
