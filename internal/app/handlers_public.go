package app

import (
	"io"
	"net/http"

	"github.com/ghmeier/rankanything/internal/ui"
)

// handleLanding is GET / for a signed-out visitor: the marketing page (hero,
// signup CTA, and a static preview of the S-F board). A signed-in visitor
// never sees it — registerPublicRoutes redirects to /me before this runs.
func (a *App) handleLanding(w http.ResponseWriter, r *http.Request) {
	props := ui.LandingPageProps{CSRFToken: a.Sessions.CSRFToken(r.Context())}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := ui.LandingPage(props).Render(r.Context(), w); err != nil {
		a.serverError(w, r, err)
	}
}

// handleRobotsTxt is GET /robots.txt. The file itself lives in
// assets/static (embedded through the existing //go:embed static in
// assets.go) so it ships in the binary like every other static asset, but
// it's served at the domain root rather than under /static/ — robots.txt
// and sitemap.xml are only honored by crawlers at the root.
func (a *App) handleRobotsTxt(w http.ResponseWriter, r *http.Request) {
	a.serveStaticRoot(w, r, "robots.txt", "text/plain; charset=utf-8")
}

// handleSitemapXML is GET /sitemap.xml. See handleRobotsTxt for why this
// isn't just served from under /static/.
func (a *App) handleSitemapXML(w http.ResponseWriter, r *http.Request) {
	a.serveStaticRoot(w, r, "sitemap.xml", "application/xml; charset=utf-8")
}

func (a *App) serveStaticRoot(w http.ResponseWriter, r *http.Request, name, contentType string) {
	f, err := a.Static.Open(name)
	if err != nil {
		a.notFound(w, r)
		return
	}
	defer f.Close()

	w.Header().Set("Content-Type", contentType)
	if _, err := io.Copy(w, f); err != nil {
		a.Logger.Error("serve static root file", "err", err, "file", name)
	}
}
