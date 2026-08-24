package app

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/a-h/templ"
	"github.com/ghmeier/rankanything/internal/constants"
	"github.com/ghmeier/rankanything/internal/db"
	"github.com/ghmeier/rankanything/internal/services"
	"github.com/ghmeier/rankanything/internal/ui"
	"github.com/google/uuid"
)

// boardScope reads back what RequireRankingAccess resolved, so it panics
// rather than misbehaving on a board route that forgot the middleware.
func boardScope(r *http.Request) (uuid.UUID, db.RankingVersion) {
	ctx := r.Context()
	return ctx.Value(constants.RankingUUIDKey).(uuid.UUID),
		ctx.Value(constants.RankingVersionKey).(db.RankingVersion)
}

// requireDraftVersion rejects writes to a published version, which is
// immutable. It must sit inside RequireRankingAccess, which is what resolves
// the version this reads from context.
func (a *App) requireDraftVersion(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		version := r.Context().Value(constants.RankingVersionKey).(db.RankingVersion)
		if version.PublishedAt.Valid {
			a.forbidden(w, r, "This version is published and can no longer be edited.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *App) handleViewRanking(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rankingUUID, version := boardScope(r)

	ranking, err := a.RankingSvc.GetRanking(ctx, rankingUUID)
	if err != nil {
		a.rankError(w, r, err)
		return
	}

	board, err := a.RankingSvc.GetBoard(ctx, ranking, version)
	if err != nil {
		a.serverError(w, r, err)
		return
	}

	a.renderRankingPage(w, r, board)
}

func (a *App) handleUpdateRanking(w http.ResponseWriter, r *http.Request) {
	rankingUUID, version := boardScope(r)

	updated, err := a.RankingSvc.UpdateRanking(r.Context(), services.UpdateRankingRequest{
		UUID:        rankingUUID,
		Name:        r.FormValue("title"),
		Description: r.FormValue("description"),
	})
	if err != nil {
		a.rankError(w, r, err)
		return
	}

	props := rankingMetaProps(rankingUUID.String(), version.ShortUuid, updated, !version.PublishedAt.Valid)
	a.render(w, r, http.StatusOK, ui.RankingMeta(props))
}

func (a *App) handleAddItem(w http.ResponseWriter, r *http.Request) {
	rankingUUID, version := boardScope(r)

	title := strings.TrimSpace(r.FormValue("label"))
	if title == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	item, err := a.RankingSvc.AddItem(r.Context(), services.AddItemRequest{
		VersionID:      version.ID,
		Title:          title,
		ImageSourceURL: r.FormValue("image_url"),
	})
	if err != nil {
		a.serverError(w, r, err)
		return
	}

	card := ui.ItemCard(itemCardProps(rankingUUID.String(), version.ShortUuid, item, true))
	a.renderWithVersionActions(w, r, http.StatusOK, rankingUUID, version, card)
}

// handleDeleteItem answers with nothing for the primary target, which is what
// htmx swaps in to remove the card — the same trick handleDeleteTier uses.
func (a *App) handleDeleteItem(w http.ResponseWriter, r *http.Request) {
	rankingUUID, version := boardScope(r)

	id, ok := a.pathID(w, r, "itemID")
	if !ok {
		return
	}

	if err := a.RankingSvc.DeleteItem(r.Context(), services.DeleteItemRequest{VersionID: version.ID, ItemID: id}); err != nil {
		a.rankError(w, r, err)
		return
	}

	a.renderWithVersionActions(w, r, http.StatusAccepted, rankingUUID, version)
}

func (a *App) handleAddTier(w http.ResponseWriter, r *http.Request) {
	rankingUUID, version := boardScope(r)

	tier, err := a.RankingSvc.AddTier(r.Context(), services.AddTierRequest{
		VersionID: version.ID,
		RankingID: version.RankingID,
		Title:     r.FormValue("label"),
		Color:     r.FormValue("color"),
	})
	if err != nil {
		a.rankError(w, r, err)
		return
	}

	row := ui.TierRow(tierRowProps(rankingUUID.String(), version.ShortUuid, tier, nil, true))
	a.renderWithVersionActions(w, r, http.StatusOK, rankingUUID, version, row)
}

func (a *App) handleEditTier(w http.ResponseWriter, r *http.Request) {
	rankingUUID, version := boardScope(r)

	id, ok := a.pathID(w, r, "tierID")
	if !ok {
		return
	}

	tier, err := a.RankingSvc.GetTier(r.Context(), services.GetTierRequest{VersionID: version.ID, TierID: id})
	if err != nil {
		a.rankError(w, r, err)
		return
	}

	props := tierRowLabelProps(rankingUUID.String(), version.ShortUuid, tier, true, true)
	a.render(w, r, http.StatusAccepted, ui.TierRowLabel(props))
}

func (a *App) handleUpdateTier(w http.ResponseWriter, r *http.Request) {
	rankingUUID, version := boardScope(r)

	id, ok := a.pathID(w, r, "tierID")
	if !ok {
		return
	}

	tier, err := a.RankingSvc.UpdateTier(r.Context(), services.UpdateTierRequest{
		VersionID: version.ID,
		TierID:    id,
		Title:     r.FormValue("label"),
		Color:     r.FormValue("color"),
	})
	if err != nil {
		a.rankError(w, r, err)
		return
	}

	props := tierRowLabelProps(rankingUUID.String(), version.ShortUuid, tier, false, true)
	a.render(w, r, http.StatusOK, ui.TierRowLabel(props))
}

// handleDeleteTier returns the tier's items to the tray rather than deleting
// them, so the response refreshes the tray out of band alongside the empty
// body that removes the row.
func (a *App) handleDeleteTier(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rankingUUID, version := boardScope(r)

	id, ok := a.pathID(w, r, "tierID")
	if !ok {
		return
	}

	if err := a.RankingSvc.DeleteTier(ctx, services.DeleteTierRequest{VersionID: version.ID, TierID: id}); err != nil {
		a.rankError(w, r, err)
		return
	}

	unranked, err := a.RankingSvc.ListUnrankedItems(ctx, version.ID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}

	tray := ui.TrayItemsProps{RankingUUID: rankingUUID.String(), OOB: true}
	for _, it := range unranked {
		tray.Items = append(tray.Items, itemCardProps(rankingUUID.String(), version.ShortUuid, it, true))
	}
	a.renderWithVersionActions(w, r, http.StatusAccepted, rankingUUID, version, ui.TrayItems(tray))
}

func (a *App) handleAddItemToTier(w http.ResponseWriter, r *http.Request) {
	rankingUUID, version := boardScope(r)

	tierID, ok := a.pathID(w, r, "tierID")
	if !ok {
		return
	}
	itemID, err := strconv.ParseInt(r.FormValue("item_id"), 10, 64)
	if err != nil {
		a.notFound(w, r)
		return
	}

	item, err := a.RankingSvc.AddItemToTier(r.Context(), services.AddItemToTierRequest{
		VersionID: version.ID,
		TierID:    tierID,
		ItemID:    itemID,
	})
	if err != nil {
		a.rankError(w, r, err)
		return
	}

	card := ui.ItemCard(itemCardProps(rankingUUID.String(), version.ShortUuid, item, true))
	a.renderWithVersionActions(w, r, http.StatusOK, rankingUUID, version, card)
}

// handleReorderTierItems takes a tier's full item order in one call, since a
// drag can pull the item in from another tier or the tray.
func (a *App) handleReorderTierItems(w http.ResponseWriter, r *http.Request) {
	rankingUUID, version := boardScope(r)

	tierID, ok := a.pathID(w, r, "tierID")
	if !ok {
		return
	}
	itemIDs, ok := a.formIDList(w, r, "item_id")
	if !ok {
		return
	}

	items, err := a.RankingSvc.ReorderTierItems(r.Context(), services.ReorderTierItemsRequest{
		VersionID: version.ID,
		TierID:    tierID,
		ItemIDs:   itemIDs,
	})
	if err != nil {
		a.rankError(w, r, err)
		return
	}

	props := ui.TierItemsProps{RankingUUID: rankingUUID.String(), TierID: tierID}
	for _, it := range items {
		props.Items = append(props.Items, itemCardProps(rankingUUID.String(), version.ShortUuid, it, true))
	}
	a.renderWithVersionActions(w, r, http.StatusOK, rankingUUID, version, ui.TierItems(props))
}

// handleReorderTiers re-renders every tier row so the client can swap the
// whole #tier-rows container.
func (a *App) handleReorderTiers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rankingUUID, version := boardScope(r)

	tierIDs, ok := a.formIDList(w, r, "tier_id")
	if !ok {
		return
	}

	if err := a.RankingSvc.ReorderTiers(ctx, services.ReorderTiersRequest{VersionID: version.ID, TierIDs: tierIDs}); err != nil {
		a.rankError(w, r, err)
		return
	}

	ranking, err := a.RankingSvc.GetRanking(ctx, rankingUUID)
	if err != nil {
		a.rankError(w, r, err)
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
		rowsProps.Tiers = append(rowsProps.Tiers, tierRowProps(rankingUUID.String(), version.ShortUuid, t, tierItems[t.ID], true))
	}
	a.render(w, r, http.StatusOK, ui.TierRows(rowsProps))
}

func (a *App) handleUnrankItem(w http.ResponseWriter, r *http.Request) {
	rankingUUID, version := boardScope(r)

	itemID, ok := a.pathID(w, r, "itemID")
	if !ok {
		return
	}

	item, err := a.RankingSvc.UnrankItem(r.Context(), services.UnrankItemRequest{VersionID: version.ID, ItemID: itemID})
	if err != nil {
		a.rankError(w, r, err)
		return
	}

	card := ui.ItemCard(itemCardProps(rankingUUID.String(), version.ShortUuid, item, true))
	a.renderWithVersionActions(w, r, http.StatusOK, rankingUUID, version, card)
}

// handlePublishVersion redirects rather than patching the page, because
// publishing changes the status text, the version dropdown, and the branch
// action all at once.
func (a *App) handlePublishVersion(w http.ResponseWriter, r *http.Request) {
	rankingUUID, version := boardScope(r)

	published, err := a.RankingSvc.PublishVersion(r.Context(), services.PublishVersionRequest{VersionID: version.ID})
	if err != nil {
		a.rankError(w, r, err)
		return
	}

	w.Header().Set("HX-Redirect", fmt.Sprintf("/r/%s/v/%s", rankingUUID, published.ShortUuid))
	w.WriteHeader(http.StatusOK)
}

func (a *App) handleCreateVersion(w http.ResponseWriter, r *http.Request) {
	rankingUUID, version := boardScope(r)

	if !version.PublishedAt.Valid {
		w.WriteHeader(http.StatusConflict)
		return
	}

	draft, err := a.RankingSvc.CreateVersionFromPublished(r.Context(), services.CreateVersionFromPublishedRequest{
		RankingID:       version.RankingID,
		SourceVersionID: version.ID,
	})
	if err != nil {
		a.rankError(w, r, err)
		return
	}

	w.Header().Set("HX-Redirect", fmt.Sprintf("/r/%s/v/%s", rankingUUID, draft.ShortUuid))
	w.WriteHeader(http.StatusOK)
}

// renderWithVersionActions renders a mutation's own fragments plus the
// publish action out of band, since any board mutation can flip the publish
// gate.
func (a *App) renderWithVersionActions(w http.ResponseWriter, r *http.Request, status int, rankingUUID uuid.UUID, version db.RankingVersion, fragments ...templ.Component) {
	actions, err := a.boardVersionActionsOOB(r.Context(), rankingUUID.String(), version)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.render(w, r, status, append(fragments, actions)...)
}

func (a *App) pathID(w http.ResponseWriter, r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil {
		a.notFound(w, r)
		return 0, false
	}
	return id, true
}

// formIDList reads repeated form fields (in drag order) as ids. A malformed
// one means a client bug, not a user-correctable error, so it rejects the
// whole request.
func (a *App) formIDList(w http.ResponseWriter, r *http.Request, name string) ([]int64, bool) {
	if err := r.ParseForm(); err != nil {
		a.serverError(w, r, err)
		return nil, false
	}

	values := r.Form[name]
	ids := make([]int64, len(values))
	for i, v := range values {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return nil, false
		}
		ids[i] = id
	}
	return ids, true
}

func (a *App) rankError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, services.ErrRankingNotFound):
		a.notFound(w, r)
	case errors.Is(err, services.ErrInvalidTierPlacement),
		errors.Is(err, services.ErrNotPublishable),
		errors.Is(err, services.ErrDraftAlreadyExists):
		w.WriteHeader(http.StatusConflict)
	default:
		a.serverError(w, r, err)
	}
}
