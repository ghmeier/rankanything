package services_test

import (
	"encoding/csv"
	"strings"
	"testing"

	"github.com/ghmeier/rankanything/internal/db"
	"github.com/ghmeier/rankanything/internal/services"
	"github.com/stretchr/testify/require"
)

// writeBoardCSV runs WriteBoardCSV and parses the result back into rows,
// dropping the header, so tests can assert on data rows directly.
func writeBoardCSV(t *testing.T, board services.RankingBoard) [][]string {
	t.Helper()

	var buf strings.Builder
	require.NoError(t, services.WriteBoardCSV(&buf, board))

	rows, err := csv.NewReader(strings.NewReader(buf.String())).ReadAll()
	require.NoError(t, err)
	require.NotEmpty(t, rows)
	require.Equal(t, []string{"tier_title", "tier_position", "item_position", "item_title", "source_url", "image_url"}, rows[0])
	return rows[1:]
}

func strPtr(s string) *string { return &s }

func TestWriteBoardCSVOrdersRowsTierThenItemPosition(t *testing.T) {
	t.Parallel()

	board := services.RankingBoard{
		Tiers: []db.RankingTier{
			{ID: 1, Position: 0, Title: "S"},
			{ID: 2, Position: 1, Title: "A"},
		},
		Items: []db.RankingItem{
			{ID: 10, Title: strPtr("Second in S")},
			{ID: 11, Title: strPtr("First in S")},
			{ID: 12, Title: strPtr("Only in A")},
		},
		Placements: []db.RankingItemTier{
			{RankingTierID: 1, RankingItemID: 11, Position: 0},
			{RankingTierID: 1, RankingItemID: 10, Position: 1},
			{RankingTierID: 2, RankingItemID: 12, Position: 0},
		},
	}

	rows := writeBoardCSV(t, board)

	require.Len(t, rows, 3)
	assertRow := func(row []string, tierTitle, itemTitle string) {
		require.Equal(t, tierTitle, row[0])
		require.Equal(t, itemTitle, row[3])
	}
	assertRow(rows[0], "S", "First in S")
	assertRow(rows[1], "S", "Second in S")
	assertRow(rows[2], "A", "Only in A")
}

func TestWriteBoardCSVPutsUnrankedItemsLastWithNoTierOrPositions(t *testing.T) {
	t.Parallel()

	board := services.RankingBoard{
		Tiers: []db.RankingTier{{ID: 1, Position: 0, Title: "S"}},
		Items: []db.RankingItem{
			{ID: 10, Title: strPtr("Ranked")},
			{ID: 11, Title: strPtr("Unranked")},
		},
		Placements: []db.RankingItemTier{
			{RankingTierID: 1, RankingItemID: 10, Position: 0},
		},
	}

	rows := writeBoardCSV(t, board)

	require.Len(t, rows, 2)
	last := rows[1]
	require.Equal(t, "Unranked", last[3])
	require.Equal(t, "", last[0], "unranked row has an empty tier_title")
	require.Equal(t, "", last[1], "unranked row has an empty tier_position")
	require.Equal(t, "", last[2], "unranked row has an empty item_position")
}

func TestWriteBoardCSVGivesAMultiTierItemOnePerPlacement(t *testing.T) {
	t.Parallel()

	board := services.RankingBoard{
		Tiers: []db.RankingTier{
			{ID: 1, Position: 0, Title: "S"},
			{ID: 2, Position: 1, Title: "A"},
		},
		Items: []db.RankingItem{{ID: 10, Title: strPtr("Everywhere")}},
		Placements: []db.RankingItemTier{
			{RankingTierID: 1, RankingItemID: 10, Position: 0},
			{RankingTierID: 2, RankingItemID: 10, Position: 0},
		},
	}

	rows := writeBoardCSV(t, board)

	require.Len(t, rows, 2, "one row per placement, not one row per item")
	require.Equal(t, "S", rows[0][0])
	require.Equal(t, "A", rows[1][0])
	require.Equal(t, "Everywhere", rows[0][3])
	require.Equal(t, "Everywhere", rows[1][3])
}

func TestWriteBoardCSVNeutralizesLeadingFormulaCharacters(t *testing.T) {
	t.Parallel()

	for _, dangerous := range []string{"=SUM(A1:A9)", "+1+1", "-1+1", "@SUM(A1:A9)"} {
		board := services.RankingBoard{
			Items: []db.RankingItem{{ID: 1, Title: &dangerous}},
		}

		rows := writeBoardCSV(t, board)

		require.Len(t, rows, 1)
		require.Equal(t, "'"+dangerous, rows[0][3], "leading formula character must be neutralized")
	}
}

func TestWriteBoardCSVLeavesOrdinaryTitlesUntouched(t *testing.T) {
	t.Parallel()

	board := services.RankingBoard{
		Items: []db.RankingItem{{ID: 1, Title: strPtr("Ordinary title")}},
	}

	rows := writeBoardCSV(t, board)

	require.Equal(t, "Ordinary title", rows[0][3])
}

func TestWriteBoardCSVIncludesSourceAndImageURLs(t *testing.T) {
	t.Parallel()

	board := services.RankingBoard{
		Tiers: []db.RankingTier{{ID: 1, Position: 0, Title: "S"}},
		Items: []db.RankingItem{
			{ID: 10, Title: strPtr("Ranked"), SourceUrl: strPtr("https://example.com/a"), ImageSourceUrl: strPtr("https://example.com/a.png")},
		},
		Placements: []db.RankingItemTier{{RankingTierID: 1, RankingItemID: 10, Position: 0}},
	}

	rows := writeBoardCSV(t, board)

	require.Equal(t, "https://example.com/a", rows[0][4])
	require.Equal(t, "https://example.com/a.png", rows[0][5])
}
