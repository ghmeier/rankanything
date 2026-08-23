package app

import "net/http"

// registerPublicRoutes mounts routes with no ownership check: the "/"
// landing/dispatch route, the SEO files (sitemap.xml, robots.txt), and the
// read-only public share view at GET /s/{public_slug}. That route
// deliberately does not run through RequireRankingAccess — a public link
// has different access rules (anyone with the slug, not just the owner)
// and renders a page with no edit affordances at all.
func (a *App) registerPublicRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /{$}", a.handleRoot)
	mux.HandleFunc("GET /robots.txt", a.handleRobotsTxt)
	mux.HandleFunc("GET /sitemap.xml", a.handleSitemapXML)
	mux.HandleFunc("GET /s/{public_slug}", a.handlePublicRanking)
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
