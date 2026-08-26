package app

import "net/http"

func (a *App) registerPublicRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /{$}", a.handleRoot)
	mux.HandleFunc("GET /robots.txt", a.handleRobotsTxt)
	mux.HandleFunc("GET /sitemap.xml", a.handleSitemapXML)
	mux.HandleFunc("GET /s/{public_slug}", a.handlePublicRanking)

	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})

}

func (a *App) handleRoot(w http.ResponseWriter, r *http.Request) {
	if userID := a.Sessions.UserID(r.Context()); userID != 0 {
		http.Redirect(w, r, "/me", http.StatusSeeOther)
		return
	}
	a.handleLanding(w, r)
}
