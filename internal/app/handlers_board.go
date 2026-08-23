package app

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/ghmeier/rankanything/internal/constants"
	"github.com/ghmeier/rankanything/internal/db"
	"github.com/ghmeier/rankanything/internal/services"
	"github.com/ghmeier/rankanything/internal/ui"
	"github.com/google/uuid"
)

// handleViewRanking is GET /r/{uuid} or GET /r/{uuid}/v/{short} — render the
// board for the version RequireRankingAccess resolved.
func (a *App) handleViewRanking(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rankingUUID := ctx.Value(constants.RankingUUIDKey).(uuid.UUID)
	version := ctx.Value(constants.RankingVersionKey).(db.RankingVersion)

	ranking, err := a.RankingSvc.GetRanking(ctx, rankingUUID)
	if err != nil {
		rankError(a, w, r, err)
		return
	}

	board, err := a.RankingSvc.GetBoard(ctx, ranking, version)
	if err != nil {
		a.serverError(w, r, err)
		return
	}

	if err := a.RenderRankingPage(w, r, board); err != nil {
		a.serverError(w, r, err)
	}
}

// handleUpdateRanking is POST /r/{uuid} — update title or description.
func (a *App) handleUpdateRanking(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rankingUUID := ctx.Value(constants.RankingUUIDKey).(uuid.UUID)

	updated, err := a.RankingSvc.UpdateRanking(ctx, services.UpdateRankingRequest{
		UUID:        rankingUUID,
		Name:        r.FormValue("title"),
		Description: r.FormValue("description"),
	})
	if err != nil {
		rankError(a, w, r, err)
		return
	}

	props := rankingMetaProps(rankingUUID.String(), updated)
	if err := renderComponent(w, r, http.StatusOK, ui.RankingMeta(props)); err != nil {
		a.serverError(w, r, err)
	}
}

// handleAddItem is POST /r/{uuid}/items — add a new item to the version
// being viewed.
func (a *App) handleAddItem(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rankingUUID := ctx.Value(constants.RankingUUIDKey).(uuid.UUID)
	version := ctx.Value(constants.RankingVersionKey).(db.RankingVersion)
	title := strings.TrimSpace(r.FormValue("label"))

	if title == "" {
		a.Render.Empty(w, http.StatusBadRequest)
		return
	}

	item, err := a.RankingSvc.AddItem(ctx, services.AddItemRequest{
		VersionID:      version.ID,
		Title:          title,
		ImageSourceURL: r.FormValue("image_url"),
	})
	if err != nil {
		a.serverError(w, r, err)
		return
	}

	if err = renderComponent(w, r, http.StatusOK, ui.ItemCard(itemCardProps(rankingUUID.String(), item))); err != nil {
		a.serverError(w, r, err)
	}
}

// handleDeleteItem is DELETE /r/{uuid}/items/{itemID}.
func (a *App) handleDeleteItem(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	version := ctx.Value(constants.RankingVersionKey).(db.RankingVersion)

	id, err := strconv.ParseInt(r.PathValue("itemID"), 10, 64)
	if err != nil {
		a.notFound(w, r)
		return
	}

	if err := a.RankingSvc.DeleteItem(ctx, services.DeleteItemRequest{VersionID: version.ID, ItemID: id}); err != nil {
		rankError(a, w, r, err)
		return
	}

	a.Render.Empty(w, http.StatusAccepted)
}

// handleAddTier is POST /r/{uuid}/tiers — add a new tier.
func (a *App) handleAddTier(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rankingUUID := ctx.Value(constants.RankingUUIDKey).(uuid.UUID)
	version := ctx.Value(constants.RankingVersionKey).(db.RankingVersion)

	tier, err := a.RankingSvc.AddTier(ctx, services.AddTierRequest{
		VersionID: version.ID,
		RankingID: version.RankingID,
		Title:     r.FormValue("label"),
		Color:     r.FormValue("color"),
	})
	if err != nil {
		rankError(a, w, r, err)
		return
	}

	if err := renderComponent(w, r, http.StatusOK, ui.TierRow(tierRowProps(rankingUUID.String(), tier, nil))); err != nil {
		a.serverError(w, r, err)
	}
}

// handleEditTier is POST /r/{uuid}/tiers/{tierID}/edit — enable editing a tier.
func (a *App) handleEditTier(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rankingUUID := ctx.Value(constants.RankingUUIDKey).(uuid.UUID)
	version := ctx.Value(constants.RankingVersionKey).(db.RankingVersion)

	id, err := strconv.ParseInt(r.PathValue("tierID"), 10, 64)
	if err != nil {
		a.notFound(w, r)
		return
	}

	tier, err := a.RankingSvc.GetTier(ctx, services.GetTierRequest{VersionID: version.ID, TierID: id})
	if err != nil {
		rankError(a, w, r, err)
		return
	}

	props := tierRowLabelProps(rankingUUID.String(), tier, true)
	if err := renderComponent(w, r, http.StatusAccepted, ui.TierRowLabel(props)); err != nil {
		a.serverError(w, r, err)
	}
}

// handleUpdateTier is PUT /r/{uuid}/tiers/{tierID} — rename or recolor.
func (a *App) handleUpdateTier(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rankingUUID := ctx.Value(constants.RankingUUIDKey).(uuid.UUID)
	version := ctx.Value(constants.RankingVersionKey).(db.RankingVersion)

	id, err := strconv.ParseInt(r.PathValue("tierID"), 10, 64)
	if err != nil {
		a.notFound(w, r)
		return
	}

	tier, err := a.RankingSvc.UpdateTier(ctx, services.UpdateTierRequest{
		VersionID: version.ID,
		TierID:    id,
		Title:     r.FormValue("label"),
		Color:     r.FormValue("color"),
	})
	if err != nil {
		rankError(a, w, r, err)
		return
	}

	props := tierRowLabelProps(rankingUUID.String(), tier, false)
	if err := renderComponent(w, r, http.StatusOK, ui.TierRowLabel(props)); err != nil {
		a.serverError(w, r, err)
	}
}

// handleDeleteTier is DELETE /r/{uuid}/tiers/{tierID} — remove a tier. Its
// items return to the unassigned tray rather than being deleted.
func (a *App) handleDeleteTier(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	version := ctx.Value(constants.RankingVersionKey).(db.RankingVersion)

	id, err := strconv.ParseInt(r.PathValue("tierID"), 10, 64)
	if err != nil {
		a.notFound(w, r)
		return
	}

	if err := a.RankingSvc.DeleteTier(ctx, services.DeleteTierRequest{VersionID: version.ID, TierID: id}); err != nil {
		rankError(a, w, r, err)
		return
	}

	a.Render.Empty(w, http.StatusAccepted)
}

// handleAddItemToTier is POST /r/{uuid}/tiers/{tierID}/items — place an item
// in a tier, via drag-and-drop.
func (a *App) handleAddItemToTier(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rankingUUID := ctx.Value(constants.RankingUUIDKey).(uuid.UUID)
	version := ctx.Value(constants.RankingVersionKey).(db.RankingVersion)

	tierID, err := strconv.ParseInt(r.PathValue("tierID"), 10, 64)
	if err != nil {
		a.notFound(w, r)
		return
	}
	itemID, err := strconv.ParseInt(r.FormValue("item_id"), 10, 64)
	if err != nil {
		a.notFound(w, r)
		return
	}

	item, err := a.RankingSvc.AddItemToTier(ctx, services.AddItemToTierRequest{
		VersionID: version.ID,
		TierID:    tierID,
		ItemID:    itemID,
	})
	if err != nil {
		if errors.Is(err, services.ErrInvalidTierPlacement) {
			a.Render.Empty(w, http.StatusConflict)
			return
		}
		rankError(a, w, r, err)
		return
	}

	if err = renderComponent(w, r, http.StatusOK, ui.ItemCard(itemCardProps(rankingUUID.String(), item))); err != nil {
		a.serverError(w, r, err)
	}
}

// rankError maps service errors to HTTP responses.
func rankError(a *App, w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, services.ErrRankingNotFound) {
		a.notFound(w, r)
		return
	}
	a.serverError(w, r, err)
}
