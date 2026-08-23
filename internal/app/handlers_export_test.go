package app_test

import (
	"context"
	"encoding/csv"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ghmeier/rankanything/internal/db"
	"github.com/ghmeier/rankanything/internal/testsupport"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// csvRows fetches an export and parses its body into rows, dropping the
// header row, so tests can assert on the data directly.
func csvRows(t *testing.T, res *testsupport.Response) [][]string {
	t.Helper()
	require.Equal(t, http.StatusOK, res.Status)
	rows, err := csv.NewReader(strings.NewReader(res.Body)).ReadAll()
	require.NoError(t, err)
	require.NotEmpty(t, rows)
	require.Equal(t, []string{"tier_title", "tier_position", "item_position", "item_title", "source_url", "image_url"}, rows[0])
	return rows[1:]
}

func TestExportBoardCSVRowsMatchOnScreenOrder(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()
	ctx := context.Background()
	slug := owner.Ranking.Uuid

	owner.HTMX(http.MethodPost, "/r/"+slug.String()+"/v/"+owner.Draft.ShortUuid+"/items", url.Values{"label": {"Second"}})
	owner.HTMX(http.MethodPost, "/r/"+slug.String()+"/v/"+owner.Draft.ShortUuid+"/items", url.Values{"label": {"First"}})

	tiers, err := env.Queries.ListRankingTiersForVersion(ctx, owner.Draft.ID)
	require.NoError(t, err)
	items, err := env.Queries.ListRankingItemsForVersion(ctx, owner.Draft.ID)
	require.NoError(t, err)
	require.Len(t, items, 2)
	second, first := items[0], items[1]

	// Place "First" ahead of "Second" in the same tier by placing First
	// second: AddItemToTier appends, so the placement order ends up First
	// then Second — the same order the tier row renders them in.
	owner.HTMX(http.MethodPost, "/r/"+slug.String()+"/v/"+owner.Draft.ShortUuid+"/tiers/"+strconv.FormatInt(tiers[0].ID, 10)+"/items",
		url.Values{"item_id": {strconv.FormatInt(first.ID, 10)}})
	owner.HTMX(http.MethodPost, "/r/"+slug.String()+"/v/"+owner.Draft.ShortUuid+"/tiers/"+strconv.FormatInt(tiers[0].ID, 10)+"/items",
		url.Values{"item_id": {strconv.FormatInt(second.ID, 10)}})

	res := owner.Get("/r/" + slug.String() + "/v/" + owner.Draft.ShortUuid + "/export")
	rows := csvRows(t, res)

	require.Len(t, rows, 2)
	assert.Equal(t, tiers[0].Title, rows[0][0])
	assert.Equal(t, "First", rows[0][3])
	assert.Equal(t, tiers[0].Title, rows[1][0])
	assert.Equal(t, "Second", rows[1][3])
}

func TestExportBoardCSVPutsUnrankedItemsLastWithNoTierOrPositions(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()
	ctx := context.Background()
	slug := owner.Ranking.Uuid

	owner.HTMX(http.MethodPost, "/r/"+slug.String()+"/v/"+owner.Draft.ShortUuid+"/items", url.Values{"label": {"Ranked"}})
	owner.HTMX(http.MethodPost, "/r/"+slug.String()+"/v/"+owner.Draft.ShortUuid+"/items", url.Values{"label": {"Unranked"}})

	tiers, err := env.Queries.ListRankingTiersForVersion(ctx, owner.Draft.ID)
	require.NoError(t, err)
	items, err := env.Queries.ListRankingItemsForVersion(ctx, owner.Draft.ID)
	require.NoError(t, err)
	ranked := items[0]
	owner.HTMX(http.MethodPost, "/r/"+slug.String()+"/v/"+owner.Draft.ShortUuid+"/tiers/"+strconv.FormatInt(tiers[0].ID, 10)+"/items",
		url.Values{"item_id": {strconv.FormatInt(ranked.ID, 10)}})

	res := owner.Get("/r/" + slug.String() + "/v/" + owner.Draft.ShortUuid + "/export")
	rows := csvRows(t, res)

	require.Len(t, rows, 2)
	last := rows[1]
	assert.Equal(t, "Unranked", last[3])
	assert.Equal(t, "", last[0], "unranked row has an empty tier_title")
	assert.Equal(t, "", last[1], "unranked row has an empty tier_position")
	assert.Equal(t, "", last[2], "unranked row has an empty item_position")
}

func TestExportBoardCSVNeutralizesLeadingFormulaCharacterInTitle(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()

	owner.HTMX(http.MethodPost, "/r/"+owner.Ranking.Uuid.String()+"/v/"+owner.Draft.ShortUuid+"/items",
		url.Values{"label": {"=cmd|'/c calc'!A1"}})

	res := owner.Get("/r/" + owner.Ranking.Uuid.String() + "/v/" + owner.Draft.ShortUuid + "/export")
	rows := csvRows(t, res)

	require.Len(t, rows, 1)
	assert.Equal(t, "'=cmd|'/c calc'!A1", rows[0][3], "leading = must be neutralized with a leading quote")
}

func TestExportSetsFilenameHeaderAndSanitizesSlashAndNonASCII(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()

	title := `Snacks/Ranked "Best" Ünïcode 🍿`
	owner.Post("/r/"+owner.Ranking.Uuid.String(), url.Values{"title": {title}, "description": {""}})

	res := owner.Get("/r/" + owner.Ranking.Uuid.String() + "/v/" + owner.Draft.ShortUuid + "/export")
	require.Equal(t, http.StatusOK, res.Status)

	disposition := res.Header.Get("Content-Disposition")
	require.NotEmpty(t, disposition)
	assert.True(t, strings.HasPrefix(disposition, "attachment;"))

	date := time.Now().UTC().Format("2006-01-02")
	assert.NotContains(t, disposition, "Snacks/Ranked", "the plain filename param must not carry the raw slash")
	assert.NotContains(t, disposition, "🍿", "the plain filename param must not carry raw non-ASCII bytes")
	assert.Contains(t, disposition, `filename="Snacks`)
	assert.Contains(t, disposition, date+".csv")
	assert.Contains(t, disposition, "filename*=UTF-8''")
	assert.Contains(t, disposition, url.PathEscape("Ünïcode"), "the RFC 5987 form must carry the percent-encoded non-ASCII characters")
	assert.NotContains(t, disposition, "filename*=UTF-8''Snacks/Ranked", "the RFC 5987 form must not carry a raw slash either")

	assert.Equal(t, "text/csv; charset=utf-8", res.Header.Get("Content-Type"))
}

func TestExportPinnedOlderVersionReturnsThatVersionsContentsNotTheLiveVersions(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()
	ctx := context.Background()
	slug := owner.Ranking.Uuid

	owner.HTMX(http.MethodPost, "/r/"+slug.String()+"/v/"+owner.Draft.ShortUuid+"/items", url.Values{"label": {"Old item"}})
	published, err := env.Queries.PublishRankingVersion(ctx, owner.Draft.ID)
	require.NoError(t, err)
	// now() is fixed for the lifetime of a transaction, and every query in
	// this test runs inside the same one (see testsupport.Pool), so both
	// publishes below would otherwise land on the identical timestamp and
	// ResolveLiveRankingVersion's ORDER BY published_at DESC would have
	// nothing to break the tie with. Backdating the first one guarantees
	// the second is unambiguously the live version.
	_, err = env.Tx.Exec(ctx, "UPDATE ranking_versions SET published_at = published_at - interval '1 hour' WHERE id = $1", published.ID)
	require.NoError(t, err)

	newDraft, err := env.Queries.CreateRankingVersion(ctx, db.CreateRankingVersionParams{
		ShortUuid: uuid.NewString()[:8],
		RankingID: owner.Ranking.ID,
	})
	require.NoError(t, err)
	owner.HTMX(http.MethodPost, "/r/"+slug.String()+"/v/"+newDraft.ShortUuid+"/items", url.Values{"label": {"New item"}})
	newPublished, err := env.Queries.PublishRankingVersion(ctx, newDraft.ID)
	require.NoError(t, err)

	live := csvRows(t, owner.Get("/r/"+slug.String()+"/export"))
	require.Len(t, live, 1)
	assert.Equal(t, "New item", live[0][3])

	pinned := csvRows(t, owner.Get("/r/"+slug.String()+"/v/"+published.ShortUuid+"/export"))
	require.Len(t, pinned, 1)
	assert.Equal(t, "Old item", pinned[0][3])

	// Sanity: the two versions really are distinct rows in the database, not
	// an artifact of GetBoard caching.
	assert.NotEqual(t, published.ID, newPublished.ID)
}

func TestExportIsForbiddenForAStranger(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()

	stranger := env.NewClient()
	require.Equal(t, http.StatusSeeOther, stranger.Post("/register",
		url.Values{"email": {"export-stranger@example.com"}, "password": {"supersecret"}}).Status)

	res := stranger.Get("/r/" + owner.Ranking.Uuid.String() + "/v/" + owner.Draft.ShortUuid + "/export")
	assert.Equal(t, http.StatusNotFound, res.Status)
}
