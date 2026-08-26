package services

import (
	"encoding/csv"
	"io"
	"strconv"

	"github.com/ghmeier/rankanything/internal/db"
)

var boardCSVHeader = []string{"tier_title", "tier_position", "item_position", "item_title", "source_url", "image_url"}

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
			sanitizeCSVCell(stringOrEmpty(item.Title)),
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
			sanitizeCSVCell(stringOrEmpty(it.Title)),
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

// Prefixes formula-trigger characters to prevent CSV injection.
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
