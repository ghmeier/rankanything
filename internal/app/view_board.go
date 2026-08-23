package app

import (
	"net/http"

	"github.com/ghmeier/rankanything/internal/db"
	"github.com/ghmeier/rankanything/internal/services"
	"github.com/google/uuid"
)

// RankingView is the whole board: the ranking, the version being viewed, its
// tiers with their items, and the unranked tray.
type RankingView struct {
	BaseView
	Ranking    db.Ranking
	Version    db.RankingVersion
	IsDraft    bool
	Tiers      []TierView
	Unassigned []db.RankingItem
}

// TierView pairs a tier with its ordered contents.
type TierView struct {
	Tier  db.RankingTier
	Items []db.RankingItem
}

// RankingItemCard renders a single item card.
type RankingItemCard struct {
	Item        db.RankingItem
	RankingUUID uuid.UUID
}

// TierRowView renders a single tier row with its items (for fine-grained tier swaps).
type TierRowView struct {
	Tier        db.RankingTier
	Items       []db.RankingItem
	RankingUUID uuid.UUID
}

// TierRowLabelView renders just a tier's label, in either its display or
// editable form.
type TierRowLabelView struct {
	Tier        db.RankingTier
	RankingUUID uuid.UUID
}

// RenderRankingPage renders the whole board page for one version of a ranking.
func (a *App) RenderRankingPage(w http.ResponseWriter, r *http.Request, board services.RankingBoard) error {
	base := a.base(r)

	byID := make(map[int64]db.RankingItem, len(board.Items))
	for _, it := range board.Items {
		byID[it.ID] = it
	}
	placed := make(map[int64]bool, len(board.Placements))

	tierItems := make(map[int64][]db.RankingItem, len(board.Tiers))
	for _, p := range board.Placements {
		it, ok := byID[p.RankingItemID]
		if !ok {
			continue
		}
		placed[p.RankingItemID] = true
		tierItems[p.RankingTierID] = append(tierItems[p.RankingTierID], it)
	}

	view := RankingView{
		BaseView: base,
		Ranking:  board.Ranking,
		Version:  board.Version,
		IsDraft:  board.IsDraft,
	}
	for _, t := range board.Tiers {
		view.Tiers = append(view.Tiers, TierView{Tier: t, Items: tierItems[t.ID]})
	}
	for _, it := range board.Items {
		if !placed[it.ID] {
			view.Unassigned = append(view.Unassigned, it)
		}
	}
	return a.Render.Page(w, http.StatusOK, "pages/ranking.html", view)
}
