package app

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/ghmeier/rankanything/internal/db"
	"github.com/ghmeier/rankanything/internal/render"
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

// buildView assembles the full board for one ranking.
func (a *App) buildView(ctx context.Context, base BaseView, ranking db.Ranking) (BuilderView, error) {
	var formattedUpdated string
	if ranking.UpdatedAt.Valid {
		formattedUpdated = ranking.UpdatedAt.Time.Format("Jan 2, 2006")
	}
	tiers, err := a.Queries.ListTiers(ctx, ranking.ID)
	if err != nil {
		return BuilderView{}, err
	}
	items, err := a.Queries.ListRankingItems(ctx, ranking.ID)
	if err != nil {
		return BuilderView{}, err
	}
	placements, err := a.Queries.ListPlacements(ctx, ranking.ID)
	if err != nil {
		return BuilderView{}, err
	}

	byID := make(map[int64]db.RankedItem, len(items))
	for _, it := range items {
		byID[it.ID] = it
	}
	placed := make(map[int64]bool, len(placements))

	tierItems := make(map[int64][]db.RankedItem, len(tiers))
	for _, p := range placements {
		it, ok := byID[p.RankedItemID]
		if !ok {
			continue
		}
		placed[p.RankedItemID] = true
		tierItems[p.RankingTierID] = append(tierItems[p.RankingTierID], it)
	}

	view := BuilderView{
		BaseView:         base,
		Ranking:          ranking,
		IsDraft:          ranking.UserID == nil,
		FormattedUpdated: formattedUpdated,
	}
	for _, t := range tiers {
		view.Tiers = append(view.Tiers, TierView{Tier: t, Items: tierItems[t.ID]})
	}
	for _, it := range items {
		if !placed[it.ID] {
			view.Unassigned = append(view.Unassigned, it)
		}
	}
	return view, nil
}

// authorize loads a ranking and enforces privacy: owners see their own
// rankings, and the session that created an unclaimed draft keeps access to it.
// Everyone else gets a 404 so slugs stay unguessable.
func (a *App) authorize(w http.ResponseWriter, r *http.Request) (db.Ranking, bool) {
	ctx := r.Context()
	slug, err := uuid.Parse(r.PathValue("slug"))
	if err != nil {
		a.serverError(w, r, err)
		return db.Ranking{}, false
	}

	ranking, err := a.Queries.GetRankingBySlug(ctx, slug)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			a.notFound(w, r)
		} else {
			a.serverError(w, r, err)
		}
		return db.Ranking{}, false
	}

	if ranking.UserID != nil {
		if *ranking.UserID != a.Sessions.UserID(ctx) {
			a.notFound(w, r)
			return db.Ranking{}, false
		}
		return ranking, true
	}

	if !a.Sessions.OwnsDraft(ctx, ranking.Slug) {
		a.notFound(w, r)
		return db.Ranking{}, false
	}
	return ranking, true
}

func (a *App) notFound(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "not found", http.StatusNotFound)
}

func (a *App) serverError(w http.ResponseWriter, r *http.Request, err error) {
	a.Logger.Error("handler error", "err", err, "path", r.URL.Path)
	http.Error(w, "internal server error", http.StatusInternalServerError)
}

// slugParam parses the {slug} URL parameter as a UUID.
func (a *App) slugParam(r *http.Request) (uuid.UUID, error) {
	s := r.PathValue("slug")
	return uuid.Parse(s)
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
