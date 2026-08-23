package app

import (
	"cmp"
	"errors"
	"io"
	"net/http"

	"github.com/ghmeier/rankanything/internal/services"
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

const defaultPublicDescription = "A tier list ranked with Rank Anything."

// handlePublicRanking is GET /s/{public_slug} — the read-only view anyone
// with a share link can reach, no session or ownership required. A slug
// that doesn't resolve, or resolves to a ranking with nothing published,
// answers 404 rather than distinguishing the two: ShareService.
// ResolvePublicRanking already collapses them into the same error, the
// same way ErrRankingNotFound does for an owned ranking.
func (a *App) handlePublicRanking(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	slug := r.PathValue("public_slug")

	public, err := a.ShareSvc.ResolvePublicRanking(ctx, slug)
	if err != nil {
		if errors.Is(err, services.ErrShareNotPublic) {
			a.notFound(w, r)
			return
		}
		a.serverError(w, r, err)
		return
	}

	board, err := a.RankingSvc.GetBoard(ctx, public.Ranking, public.Version)
	if err != nil {
		a.serverError(w, r, err)
		return
	}

	props := a.publicBoardPageProps(a.base(r), board, slug)
	if err := renderComponent(w, r, http.StatusOK, ui.PublicBoardPage(props)); err != nil {
		a.serverError(w, r, err)
	}
}

// publicBoardPageProps assembles PublicBoardPage's props. It reuses the
// same tierRowProps/itemCardProps/boardTierItems/boardUnplacedItems helpers
// view_board.go builds the owner's board from, always with editable set to
// false — a public visitor never gets a drag handle, a delete control, or
// an add-item form, regardless of who they're signed in as.
func (a *App) publicBoardPageProps(base BaseView, board services.RankingBoard, slug string) ui.PublicBoardPageProps {
	tierItems := boardTierItems(board)

	props := ui.PublicBoardPageProps{
		CSRFToken:    base.CSRFToken,
		LoggedIn:     base.User != nil,
		Flash:        base.Flash,
		Theme:        base.Theme,
		Title:        cmp.Or(board.Ranking.Name, "Untitled ranking") + " · Rank Anything",
		Description:  cmp.Or(board.Ranking.Description, defaultPublicDescription),
		CanonicalURL: a.ShareSvc.PublicURL(slug),
		RankingMeta: ui.RankingMetaProps{
			Name:        board.Ranking.Name,
			Description: board.Ranking.Description,
			Editable:    false,
		},
	}
	for _, t := range board.Tiers {
		props.Tiers = append(props.Tiers, tierRowProps("", "", t, tierItems[t.ID], false))
	}

	tray := ui.ItemTrayProps{Editable: false}
	for _, it := range boardUnplacedItems(board) {
		tray.Unassigned = append(tray.Unassigned, itemCardProps("", "", it, false))
	}
	props.ItemTray = tray

	return props
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
