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
	Pool     *pgxpool.Pool
	Queries  *db.Queries
	Sessions *auth.Sessions
	Render   *render.Renderer
	Logger   *slog.Logger
	Static   fs.FS
	UserSvc  *services.UserService
}

// Routes builds the fully wrapped handler for the app.
func (a *App) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(a.Static))))

	// Builder — the landing page is the product.
	mux.HandleFunc("GET /{$}", a.handleHome)
	mux.HandleFunc("GET /new", a.handleNew)
	mux.HandleFunc("GET /r/{slug}", a.handleBuilder)
	mux.HandleFunc("POST /r/{slug}", a.handleUpdateRanking)
	mux.HandleFunc("POST /r/{slug}/save", a.handleSave)
	mux.HandleFunc("POST /r/{slug}/items", a.handleAddItem)
	mux.HandleFunc("DELETE /r/{slug}/items/{itemID}", a.handleDeleteItem)
	mux.HandleFunc("POST /r/{slug}/tiers", a.handleAddTier)
	mux.HandleFunc("PUT /r/{slug}/tiers/{tierID}", a.handleUpdateTier)
	mux.HandleFunc("POST /r/{slug}/tiers/{tierID}/edit", a.handleEditTier)
	mux.HandleFunc("DELETE /r/{slug}/tiers/{tierID}", a.handleDeleteTier)
	mux.HandleFunc("PUT /r/{slug}/placements", a.handleSetPlacements)

	// Auth.
	mux.HandleFunc("GET /register", a.handleRegisterForm)
	mux.HandleFunc("POST /register", a.handleRegister)
	mux.HandleFunc("GET /login", a.handleLoginForm)
	mux.HandleFunc("POST /login", a.handleLogin)
	mux.HandleFunc("POST /logout", a.handleLogout)

	// Account.
	mux.Handle("GET /me", auth.RequireUser(a.Sessions)(http.HandlerFunc(a.handleMe)))

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
