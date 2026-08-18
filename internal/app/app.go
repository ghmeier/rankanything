// Package app wires the HTTP layer: routes, middleware, and handlers.
package app

import (
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ghmeier/rankanything/internal/auth"
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
	auth.HandleRoute(mux, "GET /{$}", a.handleHome)
	auth.HandleRoute(mux, "GET /new", a.handleNew)
	auth.HandleRoute(mux, "GET /r/{slug}", a.handleBuilder)
	auth.HandleRoute(mux, "POST /r/{slug}", a.handleUpdateRanking)
	auth.HandleRoute(mux, "POST /r/{slug}/save", a.handleSave)
	auth.HandleRoute(mux, "POST /r/{slug}/items", a.handleAddItem)
	auth.HandleRoute(mux, "DELETE /r/{slug}/items/{itemID}", a.handleDeleteItem)
	auth.HandleRoute(mux, "POST /r/{slug}/tiers", a.handleAddTier)
	auth.HandleRoute(mux, "PUT /r/{slug}/tiers/{tierID}", a.handleUpdateTier)
	auth.HandleRoute(mux, "POST /r/{slug}/tiers/{tierID}/edit", a.handleEditTier)
	auth.HandleRoute(mux, "DELETE /r/{slug}/tiers/{tierID}", a.handleDeleteTier)
	auth.HandleRoute(mux, "PUT /r/{slug}/placements", a.handleSetPlacements)
	mux.Handle("GET /rankings", auth.RequireUser(a.Sessions)(http.HandlerFunc(a.handleRankings)))

	// Auth.
	auth.HandleRoute(mux, "GET /register", a.handleRegisterForm)
	auth.HandleRoute(mux, "POST /register", a.handleRegister)
	auth.HandleRoute(mux, "GET /login", a.handleLoginForm)
	auth.HandleRoute(mux, "POST /login", a.handleLogin)
	auth.HandleRoute(mux, "POST /logout", a.handleLogout)

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
