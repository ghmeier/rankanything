package app

import (
	"errors"
	"fmt"
	"io"
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

func boardScope(r *http.Request) (uuid.UUID, db.RankingVersion) {
	ctx := r.Context()
	return ctx.Value(constants.RankingUUIDKey).(uuid.UUID),
		ctx.Value(constants.RankingVersionKey).(db.RankingVersion)
}

func (a *App) requireOwner(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		role := r.Context().Value(constants.RankingAccessRoleKey).(db.RankingShareRole)
		if role != db.RankingShareRoleOWNER {
			a.forbidden(w, r, "Only the owner can do this.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *App) requireEditor(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		role := r.Context().Value(constants.RankingAccessRoleKey).(db.RankingShareRole)
		if role == db.RankingShareRoleREADER {
			a.forbidden(w, r, "You don't have permission to edit this ranking.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

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

	if err := r.ParseForm(); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	req := services.UpdateRankingRequest{UUID: rankingUUID}
	if values, ok := r.Form["title"]; ok {
		req.Name = &values[0]
	}
	if values, ok := r.Form["description"]; ok {
		req.Description = &values[0]
	}

	updated, err := a.RankingSvc.UpdateRanking(r.Context(), req)
	if err != nil {
		a.rankError(w, r, err)
		return
	}

	props := rankingMetaProps(rankingUUID.String(), version.ShortUuid, updated, !version.PublishedAt.Valid)
	a.render(w, r, http.StatusOK, ui.RankingMeta(props))
}

func (a *App) handleViewDescription(w http.ResponseWriter, r *http.Request) {
	a.renderDescription(w, r, ui.RankingDescription)
}

func (a *App) handleEditDescription(w http.ResponseWriter, r *http.Request) {
	a.renderDescription(w, r, ui.RankingDescriptionForm)
}

func (a *App) handleUpdateDescription(w http.ResponseWriter, r *http.Request) {
	rankingUUID, version := boardScope(r)

	description := r.FormValue("description")
	updated, err := a.RankingSvc.UpdateRanking(r.Context(), services.UpdateRankingRequest{
		UUID:        rankingUUID,
		Description: &description,
	})
	if err != nil {
		a.rankError(w, r, err)
		return
	}

	props := rankingDescriptionProps(rankingUUID.String(), updated, !version.PublishedAt.Valid)
	a.render(w, r, http.StatusOK, ui.RankingDescription(props))
}

func (a *App) renderDescription(w http.ResponseWriter, r *http.Request, component func(ui.RankingDescriptionProps) templ.Component) {
	rankingUUID, version := boardScope(r)

	ranking, err := a.RankingSvc.GetRanking(r.Context(), rankingUUID)
	if err != nil {
		a.rankError(w, r, err)
		return
	}

	props := rankingDescriptionProps(rankingUUID.String(), ranking, !version.PublishedAt.Valid)
	a.render(w, r, http.StatusOK, component(props))
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
		SourceURL:      r.FormValue("source_url"),
	})
	if err != nil {
		a.rankError(w, r, err)
		return
	}

	card := ui.ItemCard(itemCardProps(rankingUUID.String(), version.ShortUuid, item, true))
	a.renderWithVersionActions(w, r, http.StatusOK, rankingUUID, version, card)
}

var allowedImageTypes = map[string]string{
	"image/jpeg": "jpg",
	"image/png":  "png",
	"image/gif":  "gif",
	"image/webp": "webp",
}

func (a *App) handleUploadItem(w http.ResponseWriter, r *http.Request) {
	rankingUUID, version := boardScope(r)

	if err := r.ParseMultipartForm(10 << 20); err != nil {
		http.Error(w, "Request too large.", http.StatusUnprocessableEntity)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "No file provided.", http.StatusUnprocessableEntity)
		return
	}
	defer file.Close()

	if header.Size > 5<<20 {
		http.Error(w, "File must be under 5 MB.", http.StatusUnprocessableEntity)
		return
	}

	sniffBuf := make([]byte, 512)
	n, _ := io.ReadFull(file, sniffBuf)
	contentType := http.DetectContentType(sniffBuf[:n])
	ext, ok := allowedImageTypes[contentType]
	if !ok {
		http.Error(w, "File must be JPEG, PNG, GIF, or WebP.", http.StatusUnprocessableEntity)
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		a.serverError(w, r, err)
		return
	}

	key := fmt.Sprintf("rankings/%s/items/%s.%s", rankingUUID, uuid.New(), ext)
	url, err := a.Storage.Upload(r.Context(), key, file, contentType)
	if err != nil {
		a.serverError(w, r, err)
		return
	}

	item, err := a.RankingSvc.AddItem(r.Context(), services.AddItemRequest{
		VersionID:      version.ID,
		ImageUploadURL: url,
	})
	if err != nil {
		a.rankError(w, r, err)
		return
	}

	card := ui.ItemCard(itemCardProps(rankingUUID.String(), version.ShortUuid, item, true))
	a.renderWithVersionActions(w, r, http.StatusOK, rankingUUID, version, card)
}

func (a *App) handleViewItem(w http.ResponseWriter, r *http.Request) {
	rankingUUID, version := boardScope(r)

	id, ok := a.pathID(w, r, "itemID")
	if !ok {
		return
	}

	item, err := a.RankingSvc.GetItem(r.Context(), services.GetItemRequest{VersionID: version.ID, ItemID: id})
	if err != nil {
		a.rankError(w, r, err)
		return
	}

	a.render(w, r, http.StatusOK, ui.ItemCard(itemCardProps(rankingUUID.String(), version.ShortUuid, item, true)))
}

func (a *App) handleEditItem(w http.ResponseWriter, r *http.Request) {
	rankingUUID, version := boardScope(r)

	id, ok := a.pathID(w, r, "itemID")
	if !ok {
		return
	}

	item, err := a.RankingSvc.GetItem(r.Context(), services.GetItemRequest{VersionID: version.ID, ItemID: id})
	if err != nil {
		a.rankError(w, r, err)
		return
	}

	a.render(w, r, http.StatusOK, ui.ItemCardForm(itemCardProps(rankingUUID.String(), version.ShortUuid, item, true)))
}

func (a *App) handleUpdateItem(w http.ResponseWriter, r *http.Request) {
	rankingUUID, version := boardScope(r)

	id, ok := a.pathID(w, r, "itemID")
	if !ok {
		return
	}

	title := strings.TrimSpace(r.FormValue("label"))
	if title == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	item, err := a.RankingSvc.UpdateItem(r.Context(), services.UpdateItemRequest{
		VersionID:      version.ID,
		ItemID:         id,
		Title:          title,
		ImageSourceURL: r.FormValue("image_url"),
		SourceURL:      r.FormValue("source_url"),
	})
	if err != nil {
		a.rankError(w, r, err)
		return
	}

	a.render(w, r, http.StatusOK, ui.ItemCard(itemCardProps(rankingUUID.String(), version.ShortUuid, item, true)))
}

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
	case errors.Is(err, services.ErrInvalidLink),
		errors.Is(err, services.ErrInvalidColor):
		w.WriteHeader(http.StatusUnprocessableEntity)
	case errors.Is(err, services.ErrTooManyItems),
		errors.Is(err, services.ErrTooManyTiers):
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
	default:
		a.serverError(w, r, err)
	}
}
