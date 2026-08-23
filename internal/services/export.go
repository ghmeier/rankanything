package services

import (
	"encoding/csv"
	"io"
	"strconv"

	"github.com/ghmeier/rankanything/internal/db"
)

var boardCSVHeader = []string{"tier_title", "tier_position", "item_position", "item_title", "source_url", "image_url"}

// WriteBoardCSV writes one version's board as CSV in on-screen order: tiers
// by position, items by position within each tier, then the unranked tray
// last in the same order the tray renders (item creation order). An item
// placed in more than one tier produces one row per placement rather than
// one row per item, since each row describes where an item sits and a
// multi-tier item sits in more than one place.
func WriteBoardCSV(w io.Writer, board RankingBoard) error {
	itemsByID := make(map[int64]db.RankingItem, len(board.Items))
	for _, it := range board.Items {
		itemsByID[it.ID] = it
	}
	tiersByID := make(map[int64]db.RankingTier, len(board.Tiers))
	for _, t := range board.Tiers {
		tiersByID[t.ID] = t
	}

	cw := csv.NewWriter(w)
	if err := cw.Write(boardCSVHeader); err != nil {
		return err
	}

	// board.Placements already arrives ordered by tier position then item
	// position (see ListRankingItemTiersForVersion), the same order the
	// board renders tier rows and the items within them.
	placed := make(map[int64]bool, len(board.Placements))
	for _, p := range board.Placements {
		item, ok := itemsByID[p.RankingItemID]
		if !ok {
			continue
		}
		tier, ok := tiersByID[p.RankingTierID]
		if !ok {
			continue
		}
		placed[item.ID] = true
		row := []string{
			sanitizeCSVCell(tier.Title),
			strconv.Itoa(int(tier.Position)),
			strconv.Itoa(int(p.Position)),
			sanitizeCSVCell(item.Title),
			sanitizeCSVCell(stringOrEmpty(item.SourceUrl)),
			sanitizeCSVCell(stringOrEmpty(item.ImageSourceUrl)),
		}
		if err := cw.Write(row); err != nil {
			return err
		}
	}

	for _, it := range board.Items {
		if placed[it.ID] {
			continue
		}
		row := []string{
			"", "", "",
			sanitizeCSVCell(it.Title),
			sanitizeCSVCell(stringOrEmpty(it.SourceUrl)),
			sanitizeCSVCell(stringOrEmpty(it.ImageSourceUrl)),
		}
		if err := cw.Write(row); err != nil {
			return err
		}
	}

	cw.Flush()
	return cw.Error()
}

func stringOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// sanitizeCSVCell neutralizes CSV injection: a leading =, +, -, or @ opens a
// formula when the file is opened in Sheets or Excel, so a value that
// starts with one is prefixed with a single quote — the same escape those
// programs use themselves to force a cell to be read as text rather than
// evaluated.
func sanitizeCSVCell(s string) string {
	if s == "" {
		return s
	}
	switch s[0] {
	case '=', '+', '-', '@':
		return "'" + s
	}
	return s
}
