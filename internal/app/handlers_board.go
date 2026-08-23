package app

import (
	"errors"
	"fmt"
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
		empty(w, http.StatusBadRequest)
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

	empty(w, http.StatusAccepted)
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
// items return to the unassigned tray rather than being deleted: the empty
// response removes the tier row (hx-target="closest .tier-row" on the
// button), and the out-of-band #tray-items block refreshes the tray in the
// same swap.
func (a *App) handleDeleteTier(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rankingUUID := ctx.Value(constants.RankingUUIDKey).(uuid.UUID)
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

	unranked, err := a.RankingSvc.ListUnrankedItems(ctx, version.ID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}

	tray := ui.TrayItemsProps{RankingUUID: rankingUUID.String(), OOB: true}
	for _, it := range unranked {
		tray.Items = append(tray.Items, itemCardProps(rankingUUID.String(), it))
	}
	if err := renderComponent(w, r, http.StatusAccepted, ui.TrayItems(tray)); err != nil {
		a.serverError(w, r, err)
	}
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
			empty(w, http.StatusConflict)
			return
		}
		rankError(a, w, r, err)
		return
	}

	if err = renderComponent(w, r, http.StatusOK, ui.ItemCard(itemCardProps(rankingUUID.String(), item))); err != nil {
		a.serverError(w, r, err)
	}
}

// handleReorderTierItems is POST /r/{uuid}/tiers/{tierID}/items/reorder —
// drag-and-drop sets a tier's full item order in one call. The dragged item
// may be arriving from another tier or the tray; either way it's inserted
// at the position it landed. The response is the tier's item container,
// which the client (assets/static/js/board.js) swaps in directly — no
// out-of-band swap needed, since the source container's own removal was
// already reflected client-side by the drag itself.
func (a *App) handleReorderTierItems(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rankingUUID := ctx.Value(constants.RankingUUIDKey).(uuid.UUID)
	version := ctx.Value(constants.RankingVersionKey).(db.RankingVersion)

	tierID, err := strconv.ParseInt(r.PathValue("tierID"), 10, 64)
	if err != nil {
		a.notFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		a.serverError(w, r, err)
		return
	}
	itemIDs, err := parseIDList(r.Form["item_id"])
	if err != nil {
		empty(w, http.StatusBadRequest)
		return
	}

	items, err := a.RankingSvc.ReorderTierItems(ctx, services.ReorderTierItemsRequest{
		VersionID: version.ID,
		TierID:    tierID,
		ItemIDs:   itemIDs,
	})
	if err != nil {
		if errors.Is(err, services.ErrInvalidTierPlacement) {
			empty(w, http.StatusConflict)
			return
		}
		rankError(a, w, r, err)
		return
	}

	props := ui.TierItemsProps{RankingUUID: rankingUUID.String(), TierID: tierID}
	for _, it := range items {
		props.Items = append(props.Items, itemCardProps(rankingUUID.String(), it))
	}
	if err := renderComponent(w, r, http.StatusOK, ui.TierItems(props)); err != nil {
		a.serverError(w, r, err)
	}
}

// handleReorderTiers is POST /r/{uuid}/tiers/reorder — drag-and-drop sets
// the tier order in one call. The response re-renders every tier row so the
// client can swap the whole #tier-rows container.
func (a *App) handleReorderTiers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rankingUUID := ctx.Value(constants.RankingUUIDKey).(uuid.UUID)
	version := ctx.Value(constants.RankingVersionKey).(db.RankingVersion)

	if err := r.ParseForm(); err != nil {
		a.serverError(w, r, err)
		return
	}
	tierIDs, err := parseIDList(r.Form["tier_id"])
	if err != nil {
		empty(w, http.StatusBadRequest)
		return
	}

	if err := a.RankingSvc.ReorderTiers(ctx, services.ReorderTiersRequest{VersionID: version.ID, TierIDs: tierIDs}); err != nil {
		rankError(a, w, r, err)
		return
	}

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

	tierItems := boardTierItems(board)
	rowsProps := ui.TierRowsProps{}
	for _, t := range board.Tiers {
		rowsProps.Tiers = append(rowsProps.Tiers, tierRowProps(rankingUUID.String(), t, tierItems[t.ID]))
	}
	if err := renderComponent(w, r, http.StatusOK, ui.TierRows(rowsProps)); err != nil {
		a.serverError(w, r, err)
	}
}

// handleUnrankItem is POST /r/{uuid}/items/{itemID}/unrank — drag-and-drop
// dropping an item back on the tray clears its tier placement.
func (a *App) handleUnrankItem(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rankingUUID := ctx.Value(constants.RankingUUIDKey).(uuid.UUID)
	version := ctx.Value(constants.RankingVersionKey).(db.RankingVersion)

	itemID, err := strconv.ParseInt(r.PathValue("itemID"), 10, 64)
	if err != nil {
		a.notFound(w, r)
		return
	}

	item, err := a.RankingSvc.UnrankItem(ctx, services.UnrankItemRequest{VersionID: version.ID, ItemID: itemID})
	if err != nil {
		if errors.Is(err, services.ErrInvalidTierPlacement) {
			empty(w, http.StatusConflict)
			return
		}
		rankError(a, w, r, err)
		return
	}

	if err := renderComponent(w, r, http.StatusOK, ui.ItemCard(itemCardProps(rankingUUID.String(), item))); err != nil {
		a.serverError(w, r, err)
	}
}

// handlePublishVersion is POST /r/{uuid}/publish — publish the draft being
// viewed, refusing (409) when it fails the publish gate. The UI only shows
// the publish action when the gate already passes, so a 409 here means a
// stale page or a direct request. On success, HX-Redirect sends the browser
// to the now-published version, since publishing changes enough of the page
// (status text, the version dropdown, the branch action) that a full
// re-render is simpler than patching pieces.
func (a *App) handlePublishVersion(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rankingUUID := ctx.Value(constants.RankingUUIDKey).(uuid.UUID)
	version := ctx.Value(constants.RankingVersionKey).(db.RankingVersion)

	published, err := a.RankingSvc.PublishVersion(ctx, services.PublishVersionRequest{VersionID: version.ID})
	if err != nil {
		if errors.Is(err, services.ErrNotPublishable) {
			empty(w, http.StatusConflict)
			return
		}
		rankError(a, w, r, err)
		return
	}

	w.Header().Set("HX-Redirect", fmt.Sprintf("/r/%s/v/%s", rankingUUID, published.ShortUuid))
	w.WriteHeader(http.StatusOK)
}

// handleCreateVersion is POST /r/{uuid}/versions — branch a new draft off
// the published version being viewed. Refuses (409) if the version being
// viewed isn't published, or if the ranking already has a draft; the UI
// only shows the action when neither applies. HX-Redirect sends the browser
// to the new draft, for the same reason handlePublishVersion does.
func (a *App) handleCreateVersion(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rankingUUID := ctx.Value(constants.RankingUUIDKey).(uuid.UUID)
	version := ctx.Value(constants.RankingVersionKey).(db.RankingVersion)

	if !version.PublishedAt.Valid {
		empty(w, http.StatusConflict)
		return
	}

	draft, err := a.RankingSvc.CreateVersionFromPublished(ctx, services.CreateVersionFromPublishedRequest{
		RankingID:       version.RankingID,
		SourceVersionID: version.ID,
	})
	if err != nil {
		if errors.Is(err, services.ErrDraftAlreadyExists) {
			empty(w, http.StatusConflict)
			return
		}
		rankError(a, w, r, err)
		return
	}

	w.Header().Set("HX-Redirect", fmt.Sprintf("/r/%s/v/%s", rankingUUID, draft.ShortUuid))
	w.WriteHeader(http.StatusOK)
}

// parseIDList parses a batch of form values (repeated item_id or tier_id
// fields, in drag order) into int64 ids, rejecting the request outright if
// any of them isn't one — a malformed id here means a client bug, not a
// user-correctable error.
func parseIDList(values []string) ([]int64, error) {
	ids := make([]int64, len(values))
	for i, v := range values {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return nil, err
		}
		ids[i] = id
	}
	return ids, nil
}

// rankError maps service errors to HTTP responses.
func rankError(a *App, w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, services.ErrRankingNotFound) {
		a.notFound(w, r)
		return
	}
	a.serverError(w, r, err)
}
