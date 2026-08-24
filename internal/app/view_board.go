package app

import (
	"context"
	"net/http"
	"sort"

	"github.com/a-h/templ"
	"github.com/ghmeier/rankanything/internal/db"
	"github.com/ghmeier/rankanything/internal/services"
	"github.com/ghmeier/rankanything/internal/ui"
)

func itemCardProps(rankingUUID string, versionShortUUID string, item db.RankingItem, editable bool) ui.ItemCardProps {
	imageURL := ""
	if item.ImageSourceUrl != nil {
		imageURL = *item.ImageSourceUrl
	}
	return ui.ItemCardProps{
		RankingUUID:      rankingUUID,
		VersionShortUUID: versionShortUUID,
		ItemID:           item.ID,
		Title:            item.Title,
		ImageURL:         imageURL,
		Editable:         editable,
	}
}

func tierRowProps(rankingUUID string, versionShortUUID string, tier db.RankingTier, items []db.RankingItem, editable bool) ui.TierRowProps {
	props := ui.TierRowProps{
		RankingUUID:      rankingUUID,
		VersionShortUUID: versionShortUUID,
		TierID:           tier.ID,
		Title:            tier.Title,
		ColorHex:         tier.ColorHex,
		Editable:         editable,
	}
	for _, it := range items {
		props.Items = append(props.Items, itemCardProps(rankingUUID, versionShortUUID, it, editable))
	}
	return props
}

func tierRowLabelProps(rankingUUID string, versionShortUUID string, tier db.RankingTier, editable bool, boardEditable bool) ui.TierRowLabelProps {
	return ui.TierRowLabelProps{
		RankingUUID:      rankingUUID,
		VersionShortUUID: versionShortUUID,
		TierID:           tier.ID,
		Title:            tier.Title,
		ColorHex:         tier.ColorHex,
		Editable:         editable,
		BoardEditable:    boardEditable,
	}
}

func rankingMetaProps(rankingUUID string, versionShortUUID string, ranking db.Ranking, editable bool) ui.RankingMetaProps {
	return ui.RankingMetaProps{
		RankingUUID:      rankingUUID,
		VersionShortUUID: versionShortUUID,
		Name:             ranking.Name,
		Description:      ranking.Description,
		Editable:         editable,
	}
}

// boardVersionNumbers keys each published version by its 1-based publish
// order. Sorted in memory because a ranking has few versions.
func boardVersionNumbers(versions []db.RankingVersion) map[int64]int {
	published := make([]db.RankingVersion, 0, len(versions))
	for _, v := range versions {
		if v.PublishedAt.Valid {
			published = append(published, v)
		}
	}
	// Postgres freezes now() to the start of a transaction, so two publishes
	// in one transaction share a timestamp; id order matches publish order.
	sort.Slice(published, func(i, j int) bool {
		pi, pj := published[i], published[j]
		if !pi.PublishedAt.Time.Equal(pj.PublishedAt.Time) {
			return pi.PublishedAt.Time.Before(pj.PublishedAt.Time)
		}
		return pi.ID < pj.ID
	})

	numbers := make(map[int64]int, len(published))
	for i, v := range published {
		numbers[v.ID] = i + 1
	}
	return numbers
}

func boardVersionOptions(rankingUUID string, versions []db.RankingVersion, viewing db.RankingVersion) []ui.BoardVersionOption {
	numbers := boardVersionNumbers(versions)
	options := make([]ui.BoardVersionOption, len(versions))
	for i, v := range versions {
		options[i] = ui.BoardVersionOption{
			URL:     "/r/" + rankingUUID + "/v/" + v.ShortUuid,
			Label:   services.FormatVersionLabel(v, numbers[v.ID]),
			Current: v.ID == viewing.ID,
		}
	}
	return options
}

func boardTierItems(board services.RankingBoard) map[int64][]db.RankingItem {
	byID := make(map[int64]db.RankingItem, len(board.Items))
	for _, it := range board.Items {
		byID[it.ID] = it
	}
	tierItems := make(map[int64][]db.RankingItem, len(board.Tiers))
	for _, p := range board.Placements {
		it, ok := byID[p.RankingItemID]
		if !ok {
			continue
		}
		tierItems[p.RankingTierID] = append(tierItems[p.RankingTierID], it)
	}
	return tierItems
}

func boardVersionActionsProps(rankingUUID string, version db.RankingVersion, versions []db.RankingVersion, gate services.PublishGate) ui.BoardVersionActionsProps {
	props := ui.BoardVersionActionsProps{
		RankingUUID:      rankingUUID,
		VersionShortUUID: version.ShortUuid,
		IsDraft:          !version.PublishedAt.Valid,
		Publishable:      gate.Publishable,
		BlockedReasons:   gate.Reasons,
	}
	if props.IsDraft {
		return props
	}
	for _, v := range versions {
		if !v.PublishedAt.Valid {
			props.HasOtherDraft = true
			props.DraftURL = "/r/" + rankingUUID + "/v/" + v.ShortUuid
			break
		}
	}
	return props
}

func boardPageProps(base BaseView, rankingUUID string, board services.RankingBoard, versions []db.RankingVersion, gate services.PublishGate) ui.BoardPageProps {
	tierItems := boardTierItems(board)
	editable := !board.Version.PublishedAt.Valid

	props := ui.BoardPageProps{
		CSRFToken:     base.CSRFToken,
		LoggedIn:      base.User != nil,
		Flash:         base.Flash,
		Theme:         base.Theme,
		RankingMeta:   rankingMetaProps(rankingUUID, board.Version.ShortUuid, board.Ranking, editable),
		Versions:      boardVersionOptions(rankingUUID, versions, board.Version),
		VersionAction: boardVersionActionsProps(rankingUUID, board.Version, versions, gate),
		Editable:      editable,
		TierForm:      ui.TierFormProps{RankingUUID: rankingUUID, VersionShortUUID: board.Version.ShortUuid},

		// ShareControl needs queries this function doesn't make, so
		// renderRankingPage fills it in.
		ShareControl:  nil,
		ExportControl: ui.BoardExport(ui.BoardExportProps{RankingUUID: rankingUUID, VersionShortUUID: board.Version.ShortUuid}),
	}
	for _, t := range board.Tiers {
		props.Tiers = append(props.Tiers, tierRowProps(rankingUUID, board.Version.ShortUuid, t, tierItems[t.ID], editable))
	}

	tray := ui.ItemTrayProps{RankingUUID: rankingUUID, VersionShortUUID: board.Version.ShortUuid, Editable: editable}
	for _, it := range board.UnplacedItems() {
		tray.Unassigned = append(tray.Unassigned, itemCardProps(rankingUUID, board.Version.ShortUuid, it, editable))
	}
	props.ItemTray = tray

	return props
}

func shareControlProps(rankingUUID string, gate services.ShareGate, link services.LinkShare) ui.ShareControlProps {
	return ui.ShareControlProps{
		RankingUUID: rankingUUID,
		Shareable:   gate.Shareable,
		Reasons:     gate.Reasons,
		IsPublic:    link.IsPublic,
		PublicURL:   link.URL,
	}
}

func (a *App) renderRankingPage(w http.ResponseWriter, r *http.Request, board services.RankingBoard) {
	ctx := r.Context()
	versions, err := a.RankingSvc.ListVersions(ctx, services.ListVersionsRequest{RankingID: board.Ranking.ID})
	if err != nil {
		a.serverError(w, r, err)
		return
	}

	// A published version has nothing to gate, so skip the queries.
	var gate services.PublishGate
	if !board.Version.PublishedAt.Valid {
		gate, err = a.RankingSvc.EvaluatePublishGate(ctx, board.Version.ID)
		if err != nil {
			a.serverError(w, r, err)
			return
		}
	}

	// The share gate asks whether the ranking ever published anything, not
	// whether this request landed on a draft.
	shareGate, err := a.ShareSvc.EvaluateShareGate(ctx, board.Ranking)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	link, err := a.ShareSvc.GetLinkShare(ctx, board.Ranking.ID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}

	rankingUUID := board.Ranking.Uuid.String()
	props := boardPageProps(a.base(r), rankingUUID, board, versions, gate)
	props.ShareControl = ui.ShareControl(shareControlProps(rankingUUID, shareGate, link))
	a.render(w, r, http.StatusOK, ui.BoardPage(props))
}

// boardVersionActionsOOB always recomputes the gate rather than branching on
// version state, because requireDraftVersion means callers only see drafts.
func (a *App) boardVersionActionsOOB(ctx context.Context, rankingUUID string, version db.RankingVersion) (templ.Component, error) {
	gate, err := a.RankingSvc.EvaluatePublishGate(ctx, version.ID)
	if err != nil {
		return nil, err
	}
	props := boardVersionActionsProps(rankingUUID, version, nil, gate)
	props.OOB = true
	return ui.BoardVersionActions(props), nil
}
