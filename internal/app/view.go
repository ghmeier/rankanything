package app

import (
	"net/http"

	"github.com/ghmeier/rankanything/internal/db"
	"github.com/ghmeier/rankanything/internal/services"
	"github.com/google/uuid"
)

// BaseView carries what the layout needs on every page.
type BaseView struct {
	User      *db.User
	CSRFToken string
	Flash     string
}

// RankingView is the whole board: ranking, tiers with their items, and the
// unranked tray.
type RankingView struct {
	BaseView
	Ranking    db.Ranking
	IsDraft    bool
	Tiers      []TierView
	Unassigned []db.RankedItem
}

// TierView pairs a tier with its ordered contents.
type TierView struct {
	Tier  db.RankingTier
	Items []db.RankedItem
}

type RankedItemCard struct {
	Item        db.RankedItem
	TierID      *int64 // nil means unranked.
	RankingSlug uuid.UUID
}

// TierRowView renders a single tier row with its items (for fine-grained tier swaps).
type TierRowView struct {
	Tier        db.RankingTier
	Items       []db.RankedItem
	RankingSlug uuid.UUID
}
type TierRowLabelView struct {
	Tier        db.RankingTier
	RankingSlug uuid.UUID
}

// EmptyTierRowView renders an empty div for OOB-swap to remove a tier row.
type EmptyTierRowView struct {
	TierID int64
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

func (a *App) RenderRankingPage(w http.ResponseWriter, r *http.Request, ranking services.RankingWithItems) error {
	base := a.base(r)

	byID := make(map[int64]db.RankedItem, len(ranking.Items))
	for _, it := range ranking.Items {
		byID[it.ID] = it
	}
	placed := make(map[int64]bool, len(ranking.Placements))

	tierItems := make(map[int64][]db.RankedItem, len(ranking.Tiers))
	for _, p := range ranking.Placements {
		it, ok := byID[p.RankedItemID]
		if !ok {
			continue
		}
		placed[p.RankedItemID] = true
		tierItems[p.RankingTierID] = append(tierItems[p.RankingTierID], it)
	}

	view := RankingView{
		BaseView: base,
		Ranking:  ranking.Ranking,
		IsDraft:  ranking.IsDraft,
	}
	for _, t := range ranking.Tiers {
		view.Tiers = append(view.Tiers, TierView{Tier: t, Items: tierItems[t.ID]})
	}
	for _, it := range ranking.Items {
		if !placed[it.ID] {
			view.Unassigned = append(view.Unassigned, it)
		}
	}
	return a.Render.Page(w, http.StatusOK, "pages/ranking.html", view)

}
