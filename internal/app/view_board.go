package app

import (
	"net/http"

	"github.com/ghmeier/rankanything/internal/db"
	"github.com/ghmeier/rankanything/internal/services"
	"github.com/ghmeier/rankanything/internal/ui"
)

// itemCardProps builds the props for one item card.
func itemCardProps(rankingUUID string, versionShortUUID string, item db.RankingItem) ui.ItemCardProps {
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
	}
}

// tierRowProps builds the props for one tier row, including its items.
func tierRowProps(rankingUUID string, versionShortUUID string, tier db.RankingTier, items []db.RankingItem) ui.TierRowProps {
	props := ui.TierRowProps{
		RankingUUID:      rankingUUID,
		VersionShortUUID: versionShortUUID,
		TierID:           tier.ID,
		Title:            tier.Title,
		ColorHex:         tier.ColorHex,
	}
	for _, it := range items {
		props.Items = append(props.Items, itemCardProps(rankingUUID, versionShortUUID, it))
	}
	return props
}

// tierRowLabelProps builds the props for a tier's label, in either its
// display or editable form.
func tierRowLabelProps(rankingUUID string, versionShortUUID string, tier db.RankingTier, editable bool) ui.TierRowLabelProps {
	return ui.TierRowLabelProps{
		RankingUUID:      rankingUUID,
		VersionShortUUID: versionShortUUID,
		TierID:           tier.ID,
		Title:            tier.Title,
		ColorHex:         tier.ColorHex,
		Editable:         editable,
	}
}

// rankingMetaProps builds the props for the title/description fields.
func rankingMetaProps(rankingUUID string, versionShortUUID string, ranking db.Ranking) ui.RankingMetaProps {
	return ui.RankingMetaProps{
		RankingUUID:      rankingUUID,
		VersionShortUUID: versionShortUUID,
		Name:             ranking.Name,
		Description:      ranking.Description,
	}
}

// boardVersionStatusText describes the version being viewed: a draft says
// so plainly, a published version says when it went live.
func boardVersionStatusText(v db.RankingVersion) string {
	if !v.PublishedAt.Valid {
		return "Draft version"
	}
	return "Published " + v.PublishedAt.Time.Format("Jan 2, 2006")
}

// boardVersionOptionLabel describes the same state, phrased for a dropdown
// entry rather than a standalone badge ("Draft" rather than "Draft
// version" — the menu it sits in already supplies that context).
func boardVersionOptionLabel(v db.RankingVersion) string {
	if !v.PublishedAt.Valid {
		return "Draft"
	}
	return "Published " + v.PublishedAt.Time.Format("Jan 2, 2006")
}

// boardVersionOptions builds the version-picker dropdown's entries, marking
// whichever one matches the version being viewed.
func boardVersionOptions(rankingUUID string, versions []db.RankingVersion, viewing db.RankingVersion) []ui.BoardVersionOption {
	options := make([]ui.BoardVersionOption, len(versions))
	for i, v := range versions {
		options[i] = ui.BoardVersionOption{
			URL:     "/r/" + rankingUUID + "/v/" + v.ShortUuid,
			Label:   boardVersionOptionLabel(v),
			Current: v.ID == viewing.ID,
		}
	}
	return options
}

// boardTierItems maps each tier id to the items placed in it, from a
// board's flat item and placement lists.
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

// boardUnplacedItems returns the items in a board with no tier placement —
// the unranked tray's contents.
func boardUnplacedItems(board services.RankingBoard) []db.RankingItem {
	placed := make(map[int64]bool, len(board.Placements))
	for _, p := range board.Placements {
		placed[p.RankingItemID] = true
	}
	var unplaced []db.RankingItem
	for _, it := range board.Items {
		if !placed[it.ID] {
			unplaced = append(unplaced, it)
		}
	}
	return unplaced
}

// boardVersionActionsProps builds the publish/branch action shown next to
// the version status: a draft's publish gate, or — for a published
// version — whether the ranking already has another draft in progress.
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

// boardPageProps assembles the whole board page's props from a fetched
// RankingBoard plus the ranking's other versions for the dropdown.
func boardPageProps(base BaseView, rankingUUID string, board services.RankingBoard, versions []db.RankingVersion, gate services.PublishGate) ui.BoardPageProps {
	tierItems := boardTierItems(board)

	props := ui.BoardPageProps{
		CSRFToken:     base.CSRFToken,
		LoggedIn:      base.User != nil,
		Flash:         base.Flash,
		RankingMeta:   rankingMetaProps(rankingUUID, board.Version.ShortUuid, board.Ranking),
		VersionStatus: boardVersionStatusText(board.Version),
		Versions:      boardVersionOptions(rankingUUID, versions, board.Version),
		VersionAction: boardVersionActionsProps(rankingUUID, board.Version, versions, gate),
		TierForm:      ui.TierFormProps{RankingUUID: rankingUUID, VersionShortUUID: board.Version.ShortUuid},
	}
	for _, t := range board.Tiers {
		props.Tiers = append(props.Tiers, tierRowProps(rankingUUID, board.Version.ShortUuid, t, tierItems[t.ID]))
	}

	tray := ui.ItemTrayProps{RankingUUID: rankingUUID, VersionShortUUID: board.Version.ShortUuid}
	for _, it := range boardUnplacedItems(board) {
		tray.Unassigned = append(tray.Unassigned, itemCardProps(rankingUUID, board.Version.ShortUuid, it))
	}
	props.ItemTray = tray

	return props
}

// RenderRankingPage renders the whole board page for one version of a
// ranking, including the version-picker dropdown listing every version of
// the ranking.
func (a *App) RenderRankingPage(w http.ResponseWriter, r *http.Request, board services.RankingBoard) error {
	ctx := r.Context()
	versions, err := a.RankingSvc.ListVersions(ctx, services.ListVersionsRequest{RankingID: board.Ranking.ID})
	if err != nil {
		return err
	}

	// The publish gate only matters for a draft; a published version has
	// nothing to gate, so skip the extra queries it costs to compute.
	var gate services.PublishGate
	if !board.Version.PublishedAt.Valid {
		gate, err = a.RankingSvc.EvaluatePublishGate(ctx, board.Version.ID)
		if err != nil {
			return err
		}
	}

	rankingUUID := board.Ranking.Uuid.String()
	props := boardPageProps(a.base(r), rankingUUID, board, versions, gate)
	return renderComponent(w, r, http.StatusOK, ui.BoardPage(props))
}
