package app

import (
	"cmp"
	"errors"
	"io"
	"net/http"

	"github.com/ghmeier/rankanything/internal/services"
	"github.com/ghmeier/rankanything/internal/ui"
)

func (a *App) handleLanding(w http.ResponseWriter, r *http.Request) {
	props := ui.LandingPageProps{CSRFToken: a.Sessions.CSRFToken(r.Context())}
	a.render(w, r, http.StatusOK, ui.LandingPage(props))
}

const defaultPublicDescription = "A tier list ranked with Rank Anything."

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
	a.render(w, r, http.StatusOK, ui.PublicBoardPage(props))
}

func (a *App) publicBoardPageProps(base BaseView, board services.RankingBoard, slug string) ui.PublicBoardPageProps {
	tierItems := boardTierItems(board)

	props := ui.PublicBoardPageProps{
		CSRFToken: base.CSRFToken,
		LoggedIn:  base.User != nil,
		Flash:     base.Flash,
		Theme:     base.Theme,
		Title:     cmp.Or(board.Ranking.Name, "Untitled ranking") + " · Rank Anything",
		// Meta tags need plain text, not markdown.
		Description:  cmp.Or(ui.PlainText(board.Ranking.Description), defaultPublicDescription),
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
	for _, it := range board.UnplacedItems() {
		tray.Unassigned = append(tray.Unassigned, itemCardProps("", "", it, false))
	}
	props.ItemTray = tray

	return props
}

func (a *App) handleRobotsTxt(w http.ResponseWriter, r *http.Request) {
	a.serveStaticRoot(w, r, "robots.txt", "text/plain; charset=utf-8")
}

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
