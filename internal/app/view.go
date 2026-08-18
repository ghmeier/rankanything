package app

import (
	"net/http"

	"github.com/ghmeier/rankanything/internal/db"
	"github.com/ghmeier/rankanything/internal/render"
	"github.com/google/uuid"
)

// BaseView carries what the layout needs on every page.
type BaseView struct {
	User      *db.User
	CSRFToken string
	Flash     string
}

// BuilderView is the whole board: ranking, tiers with their items, and the
// unranked tray.
type BuilderView struct {
	BaseView
	Ranking          db.Ranking
	IsDraft          bool
	FormattedUpdated string
	Tiers            []TierView
	Unassigned       []db.RankedItem
	Error            string
}

// TierView pairs a tier with its ordered contents.
type TierView struct {
	Tier  db.RankingTier
	Items []db.RankedItem
}

type TierRowLabelView struct {
	Tier        db.RankingTier
	RankingSlug uuid.UUID
}

// AuthView backs the login and register pages.
type AuthView struct {
	BaseView
	Email string
	Next  string
	Error string
}

// AccountView backs /me.
type AccountView struct {
	BaseView
	Rankings []AccountRanking
}

// AccountRanking is a lightweight wrapper for rankings shown on /me.
type AccountRanking struct {
	db.Ranking
	FormattedUpdated string
}

func (a *App) base(r *http.Request) BaseView {
	ctx := r.Context()
	v := BaseView{CSRFToken: a.Sessions.CSRFToken(ctx), Flash: a.Sessions.PopFlash(ctx)}
	if id := a.Sessions.UserID(ctx); id != 0 {
		if u, err := a.Queries.GetUserByID(ctx, id); err == nil {
			v.User = &u
		}
	}
	return v
}

func (a *App) notFound(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "not found", http.StatusNotFound)
}

func (a *App) serverError(w http.ResponseWriter, r *http.Request, err error) {
	a.Logger.Error("handler error", "err", err, "path", r.URL.Path)
	http.Error(w, "internal server error", http.StatusInternalServerError)
}

// renderBoard answers htmx swaps with the board fragment and plain requests
// with the whole builder page.
func (a *App) renderBoard(w http.ResponseWriter, r *http.Request, view BuilderView) {
	var err error
	if render.IsHTMXRequest(r) {
		err = a.Render.Partial(w, http.StatusOK, "partials/board.html", view)
	} else {
		err = a.Render.Page(w, http.StatusOK, "pages/builder.html", view)
	}
	if err != nil {
		a.serverError(w, r, err)
	}
}
