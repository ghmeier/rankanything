package app

import "net/http"

// registerPublicRoutes mounts routes with no ownership check: the "/"
// landing/dispatch route, and the SEO files (sitemap.xml, robots.txt). It
// will also carry the read-only public view once feat/public-share (wave 4)
// lands at GET /s/{public_slug} — that route must not run through
// RequireRankingAccess, since a public link has different access rules and
// must carry no edit affordances.
func (a *App) registerPublicRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /{$}", a.handleRoot)
	mux.HandleFunc("GET /robots.txt", a.handleRobotsTxt)
	mux.HandleFunc("GET /sitemap.xml", a.handleSitemapXML)
}

// handleRoot is GET / — a signed-in visitor goes straight to their
// rankings; a signed-out one lands on the marketing page.
func (a *App) handleRoot(w http.ResponseWriter, r *http.Request) {
	if userID := a.Sessions.UserID(r.Context()); userID != 0 {
		http.Redirect(w, r, "/me", http.StatusSeeOther)
		return
	}
	a.handleLanding(w, r)
}
