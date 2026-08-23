package app_test

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"testing"

	"github.com/ghmeier/rankanything/internal/db"
	"github.com/ghmeier/rankanything/internal/services"
	"github.com/ghmeier/rankanything/internal/testsupport"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAddItemReturnsBoardFragment(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()

	res := owner.HTMX(http.MethodPost, "/r/"+owner.Ranking.Uuid.String()+"/items", url.Values{"label": {"Pretzels"}})

	require.Equal(t, http.StatusOK, res.Status)
	assert.Contains(t, Body(res.Body), "Pretzels")
	assert.NotContains(t, Body(res.Body), "<html", "htmx swaps must not include the layout")
}

func TestAddItemRequiresLabel(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()

	res := owner.HTMX(http.MethodPost, "/r/"+owner.Ranking.Uuid.String()+"/items", url.Values{"label": {"   "}})

	assert.Equal(t, http.StatusBadRequest, res.Status)
}

func TestAddItemsToTier(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()
	ctx := context.Background()
	slug := owner.Ranking.Uuid

	owner.HTMX(http.MethodPost, "/r/"+slug.String()+"/items", url.Values{"label": {"Pretzels"}})
	owner.HTMX(http.MethodPost, "/r/"+slug.String()+"/items", url.Values{"label": {"Olives"}})

	tiers, err := env.Queries.ListRankingTiersForVersion(ctx, owner.Draft.ID)
	require.NoError(t, err)
	items, err := env.Queries.ListRankingItemsForVersion(ctx, owner.Draft.ID)
	require.NoError(t, err)
	require.Len(t, items, 2)

	form := url.Values{"item_id": {strconv.FormatInt(items[0].ID, 10)}}
	res := owner.HTMX(http.MethodPost, "/r/"+slug.String()+"/tiers/"+strconv.FormatInt(tiers[0].ID, 10)+"/items", form)
	require.Equal(t, http.StatusOK, res.Status)

	form = url.Values{"item_id": {strconv.FormatInt(items[1].ID, 10)}}
	res = owner.HTMX(http.MethodPost, "/r/"+slug.String()+"/tiers/"+strconv.FormatInt(tiers[0].ID, 10)+"/items", form)
	require.Equal(t, http.StatusOK, res.Status)

	placements, err := env.Queries.ListRankingItemTiersForTier(ctx, tiers[0].ID)
	require.NoError(t, err)
	require.Len(t, placements, 2)
}

func TestCustomTierLifecycle(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()
	ctx := context.Background()
	slug := owner.Ranking.Uuid

	res := owner.HTMX(http.MethodPost, "/r/"+slug.String()+"/tiers", url.Values{"label": {"F"}, "color": {"#111111"}})
	require.Equal(t, http.StatusOK, res.Status)
	assert.Contains(t, Body(res.Body), "F")

	tiers, err := env.Queries.ListRankingTiersForVersion(ctx, owner.Draft.ID)
	require.NoError(t, err)
	require.Len(t, tiers, len(services.DefaultTiers)+1)

	last := tiers[len(tiers)-1]
	assert.Equal(t, "F", last.Title)
	assert.Equal(t, "#111111", last.ColorHex)

	renamed := owner.HTMX(http.MethodPut, "/r/"+slug.String()+"/tiers/"+strconv.FormatInt(last.ID, 10),
		url.Values{"label": {"Trash"}, "color": {"#222222"}})
	require.Equal(t, http.StatusOK, renamed.Status)
	assert.Contains(t, Body(renamed.Body), "Trash")

	deleted := owner.HTMX(http.MethodDelete, "/r/"+slug.String()+"/tiers/"+strconv.FormatInt(last.ID, 10), nil)
	require.Equal(t, http.StatusAccepted, deleted.Status)
	assert.NotContains(t, Body(deleted.Body), "Trash")
}

func TestUpdateRankingTitleReturnsMetaPartial(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()

	res := owner.HTMX(http.MethodPost, "/r/"+owner.Ranking.Uuid.String(), url.Values{"title": {"Best snacks"}, "description": {"2026 edition"}})

	require.Equal(t, http.StatusOK, res.Status)
	assert.Contains(t, Body(res.Body), `value="Best snacks"`)
	assert.Contains(t, Body(res.Body), `value="2026 edition"`)
	assert.NotContains(t, Body(res.Body), "<html")
}

// ---------------------------------------------------------------------------
// Version resolution (RequireRankingAccess)
// ---------------------------------------------------------------------------

func TestViewRankingWithNoVersionInPathLoadsTheLiveVersion(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()

	res := owner.Get("/r/" + owner.Ranking.Uuid.String())

	require.Equal(t, http.StatusOK, res.Status)
	assert.Contains(t, Body(res.Body), "Draft version")
}

func TestViewRankingWithAPinnedShortUUIDLoadsThatVersion(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()

	res := owner.Get("/r/" + owner.Ranking.Uuid.String() + "/v/" + owner.Draft.ShortUuid)

	assert.Equal(t, http.StatusOK, res.Status)
}

func TestViewRankingWithAnUnknownShortUUIDIsNotFound(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()

	res := owner.Get("/r/" + owner.Ranking.Uuid.String() + "/v/nosuchid")

	assert.Equal(t, http.StatusNotFound, res.Status)
}

func TestOwnedRankingsAreNotReadableByOthers(t *testing.T) {
	env := testsupport.NewEnv(t)

	owner := env.NewOwnerClient()

	stranger := env.NewClient()
	require.Equal(t, http.StatusSeeOther, stranger.Post("/register",
		url.Values{"email": {"stranger@example.com"}, "password": {"supersecret"}}).Status)

	res := stranger.Get("/r/" + owner.Ranking.Uuid.String())
	assert.Equal(t, http.StatusNotFound, res.Status)

	write := stranger.Post("/r/"+owner.Ranking.Uuid.String(), url.Values{"title": {"Hijacked"}})
	assert.Equal(t, http.StatusNotFound, write.Status)
}

// ---------------------------------------------------------------------------
// Version dropdown
// ---------------------------------------------------------------------------

func TestBoardShowsPublishedDateForAPublishedVersion(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()
	ctx := context.Background()

	published, err := env.Queries.PublishRankingVersion(ctx, owner.Draft.ID)
	require.NoError(t, err)

	res := owner.Get("/r/" + owner.Ranking.Uuid.String() + "/v/" + published.ShortUuid)

	require.Equal(t, http.StatusOK, res.Status)
	assert.Contains(t, Body(res.Body), "Published "+published.PublishedAt.Time.Format("Jan 2, 2006"))
	assert.NotContains(t, Body(res.Body), "Draft version")
}

func TestBoardVersionDropdownListsEveryVersionAndMarksTheOneBeingViewed(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()
	ctx := context.Background()

	published, err := env.Queries.PublishRankingVersion(ctx, owner.Draft.ID)
	require.NoError(t, err)

	secondDraft, err := env.Queries.CreateRankingVersion(ctx, db.CreateRankingVersionParams{
		ShortUuid: uuid.NewString()[:8],
		RankingID: owner.Ranking.ID,
	})
	require.NoError(t, err)

	// Viewing the published version: its dropdown entry is marked as the
	// current one, the new draft's is present but unmarked.
	onPublished := owner.Get("/r/" + owner.Ranking.Uuid.String() + "/v/" + published.ShortUuid).Body
	assert.Contains(t, Body(onPublished), `aria-current="true"`)
	assert.Contains(t, Body(onPublished), "/r/"+owner.Ranking.Uuid.String()+"/v/"+secondDraft.ShortUuid)

	// Viewing the new draft: the live version (/r/{uuid} with no pinned
	// version) resolves to the published one per ResolveLiveRankingVersion,
	// so the draft has to be reached by its own short uuid to become "the
	// one being viewed".
	onDraft := owner.Get("/r/" + owner.Ranking.Uuid.String() + "/v/" + secondDraft.ShortUuid).Body
	assert.Contains(t, Body(onDraft), "Draft version")
	assert.Contains(t, Body(onDraft), "/r/"+owner.Ranking.Uuid.String()+"/v/"+published.ShortUuid)
}

// ---------------------------------------------------------------------------
// Drag-and-drop reordering
// ---------------------------------------------------------------------------

func TestReorderTierItemsPersistsTheGivenOrder(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()
	ctx := context.Background()
	slug := owner.Ranking.Uuid

	owner.HTMX(http.MethodPost, "/r/"+slug.String()+"/items", url.Values{"label": {"First"}})
	owner.HTMX(http.MethodPost, "/r/"+slug.String()+"/items", url.Values{"label": {"Second"}})
	tiers, err := env.Queries.ListRankingTiersForVersion(ctx, owner.Draft.ID)
	require.NoError(t, err)
	items, err := env.Queries.ListRankingItemsForVersion(ctx, owner.Draft.ID)
	require.NoError(t, err)
	require.Len(t, items, 2)

	tierPath := "/r/" + slug.String() + "/tiers/" + strconv.FormatInt(tiers[0].ID, 10)
	owner.HTMX(http.MethodPost, tierPath+"/items", url.Values{"item_id": {strconv.FormatInt(items[0].ID, 10)}})
	owner.HTMX(http.MethodPost, tierPath+"/items", url.Values{"item_id": {strconv.FormatInt(items[1].ID, 10)}})

	res := owner.HTMX(http.MethodPost, tierPath+"/items/reorder", url.Values{
		"item_id": {strconv.FormatInt(items[1].ID, 10), strconv.FormatInt(items[0].ID, 10)},
	})
	require.Equal(t, http.StatusOK, res.Status)

	placements, err := env.Queries.ListRankingItemTiersForTier(ctx, tiers[0].ID)
	require.NoError(t, err)
	require.Len(t, placements, 2)
	assert.Equal(t, items[1].ID, placements[0].RankingItemID, "the reordered position comes first")
	assert.Equal(t, items[0].ID, placements[1].RankingItemID)
}

func TestReorderTierItemsMovesAnItemFromAnotherTier(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()
	ctx := context.Background()
	slug := owner.Ranking.Uuid

	owner.HTMX(http.MethodPost, "/r/"+slug.String()+"/items", url.Values{"label": {"Migrating"}})
	tiers, err := env.Queries.ListRankingTiersForVersion(ctx, owner.Draft.ID)
	require.NoError(t, err)
	items, err := env.Queries.ListRankingItemsForVersion(ctx, owner.Draft.ID)
	require.NoError(t, err)
	require.Len(t, items, 1)

	owner.HTMX(http.MethodPost, "/r/"+slug.String()+"/tiers/"+strconv.FormatInt(tiers[0].ID, 10)+"/items",
		url.Values{"item_id": {strconv.FormatInt(items[0].ID, 10)}})

	res := owner.HTMX(http.MethodPost, "/r/"+slug.String()+"/tiers/"+strconv.FormatInt(tiers[1].ID, 10)+"/items/reorder",
		url.Values{"item_id": {strconv.FormatInt(items[0].ID, 10)}})
	require.Equal(t, http.StatusOK, res.Status)

	oldTier, err := env.Queries.ListRankingItemTiersForTier(ctx, tiers[0].ID)
	require.NoError(t, err)
	assert.Empty(t, oldTier)

	newTier, err := env.Queries.ListRankingItemTiersForTier(ctx, tiers[1].ID)
	require.NoError(t, err)
	require.Len(t, newTier, 1)
	assert.Equal(t, items[0].ID, newTier[0].RankingItemID)
}

func TestReorderTiersPersistsTheGivenOrder(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()
	ctx := context.Background()
	slug := owner.Ranking.Uuid

	tiers, err := env.Queries.ListRankingTiersForVersion(ctx, owner.Draft.ID)
	require.NoError(t, err)
	reversed := make(url.Values)
	for i := len(tiers) - 1; i >= 0; i-- {
		reversed["tier_id"] = append(reversed["tier_id"], strconv.FormatInt(tiers[i].ID, 10))
	}

	res := owner.HTMX(http.MethodPost, "/r/"+slug.String()+"/tiers/reorder", reversed)
	require.Equal(t, http.StatusOK, res.Status)
	assert.NotContains(t, Body(res.Body), "<html")

	reordered, err := env.Queries.ListRankingTiersForVersion(ctx, owner.Draft.ID)
	require.NoError(t, err)
	require.Len(t, reordered, len(tiers))
	for i, tier := range reordered {
		assert.Equal(t, tiers[len(tiers)-1-i].ID, tier.ID)
	}
}

func TestUnrankItemClearsItsTierPlacement(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()
	ctx := context.Background()
	slug := owner.Ranking.Uuid

	owner.HTMX(http.MethodPost, "/r/"+slug.String()+"/items", url.Values{"label": {"Placed"}})
	tiers, err := env.Queries.ListRankingTiersForVersion(ctx, owner.Draft.ID)
	require.NoError(t, err)
	items, err := env.Queries.ListRankingItemsForVersion(ctx, owner.Draft.ID)
	require.NoError(t, err)
	owner.HTMX(http.MethodPost, "/r/"+slug.String()+"/tiers/"+strconv.FormatInt(tiers[0].ID, 10)+"/items",
		url.Values{"item_id": {strconv.FormatInt(items[0].ID, 10)}})

	res := owner.HTMX(http.MethodPost, "/r/"+slug.String()+"/items/"+strconv.FormatInt(items[0].ID, 10)+"/unrank", nil)
	require.Equal(t, http.StatusOK, res.Status)
	assert.Contains(t, Body(res.Body), "Placed")

	placements, err := env.Queries.ListRankingItemTiersForVersion(ctx, owner.Draft.ID)
	require.NoError(t, err)
	assert.Empty(t, placements)
}

func TestDeleteTierReturnsItsItemsToTheTrayOutOfBand(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()
	ctx := context.Background()
	slug := owner.Ranking.Uuid

	tierRes := owner.HTMX(http.MethodPost, "/r/"+slug.String()+"/tiers", url.Values{"label": {"Doomed"}})
	require.Equal(t, http.StatusOK, tierRes.Status)
	tiers, err := env.Queries.ListRankingTiersForVersion(ctx, owner.Draft.ID)
	require.NoError(t, err)
	doomed := tiers[len(tiers)-1]

	owner.HTMX(http.MethodPost, "/r/"+slug.String()+"/items", url.Values{"label": {"Stranded"}})
	items, err := env.Queries.ListRankingItemsForVersion(ctx, owner.Draft.ID)
	require.NoError(t, err)
	owner.HTMX(http.MethodPost, "/r/"+slug.String()+"/tiers/"+strconv.FormatInt(doomed.ID, 10)+"/items",
		url.Values{"item_id": {strconv.FormatInt(items[0].ID, 10)}})

	res := owner.HTMX(http.MethodDelete, "/r/"+slug.String()+"/tiers/"+strconv.FormatInt(doomed.ID, 10), nil)
	require.Equal(t, http.StatusAccepted, res.Status)
	assert.Contains(t, Body(res.Body), `id="tray-items"`)
	assert.Contains(t, Body(res.Body), `hx-swap-oob="true"`)
	assert.Contains(t, Body(res.Body), "Stranded", "the deleted tier's item comes back in the tray fragment")

	placements, err := env.Queries.ListRankingItemTiersForVersion(ctx, owner.Draft.ID)
	require.NoError(t, err)
	assert.Empty(t, placements)
}

// ---------------------------------------------------------------------------
// Publishing and version branching
// ---------------------------------------------------------------------------

func TestPublishIsBlockedUntilTheGatePasses(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()

	res := owner.Post("/r/"+owner.Ranking.Uuid.String()+"/publish", nil)
	assert.Equal(t, http.StatusConflict, res.Status)
}

func TestPublishSucceedsAndRedirectsToThePublishedVersion(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()
	ctx := context.Background()
	slug := owner.Ranking.Uuid

	owner.HTMX(http.MethodPost, "/r/"+slug.String()+"/items", url.Values{"label": {"Ready"}})
	tiers, err := env.Queries.ListRankingTiersForVersion(ctx, owner.Draft.ID)
	require.NoError(t, err)
	items, err := env.Queries.ListRankingItemsForVersion(ctx, owner.Draft.ID)
	require.NoError(t, err)
	owner.HTMX(http.MethodPost, "/r/"+slug.String()+"/tiers/"+strconv.FormatInt(tiers[0].ID, 10)+"/items",
		url.Values{"item_id": {strconv.FormatInt(items[0].ID, 10)}})

	res := owner.Post("/r/"+slug.String()+"/publish", nil)
	require.Equal(t, http.StatusOK, res.Status)
	redirect := res.Header.Get("HX-Redirect")
	require.NotEmpty(t, redirect)
	assert.Equal(t, "/r/"+slug.String()+"/v/"+owner.Draft.ShortUuid, redirect)

	page := owner.Get(redirect)
	require.Equal(t, http.StatusOK, page.Status)
	assert.Contains(t, Body(page.Body), "Published ")
}

func TestCreateVersionFailsWhileViewingADraft(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()

	res := owner.Post("/r/"+owner.Ranking.Uuid.String()+"/versions", nil)
	assert.Equal(t, http.StatusConflict, res.Status)
}

func TestCreateVersionFromAPublishedVersionCopiesItsBoard(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()
	ctx := context.Background()
	slug := owner.Ranking.Uuid

	owner.HTMX(http.MethodPost, "/r/"+slug.String()+"/items", url.Values{"label": {"Carried over"}})
	tiers, err := env.Queries.ListRankingTiersForVersion(ctx, owner.Draft.ID)
	require.NoError(t, err)
	items, err := env.Queries.ListRankingItemsForVersion(ctx, owner.Draft.ID)
	require.NoError(t, err)
	owner.HTMX(http.MethodPost, "/r/"+slug.String()+"/tiers/"+strconv.FormatInt(tiers[0].ID, 10)+"/items",
		url.Values{"item_id": {strconv.FormatInt(items[0].ID, 10)}})
	require.Equal(t, http.StatusOK, owner.Post("/r/"+slug.String()+"/publish", nil).Status)

	res := owner.Post("/r/"+slug.String()+"/versions", nil)
	require.Equal(t, http.StatusOK, res.Status)
	redirect := res.Header.Get("HX-Redirect")
	require.NotEmpty(t, redirect)
	assert.NotEqual(t, "/r/"+slug.String()+"/v/"+owner.Draft.ShortUuid, redirect, "the new draft has its own short uuid")

	versions, err := env.Queries.ListRankingVersionsForRanking(ctx, owner.Ranking.ID)
	require.NoError(t, err)
	require.Len(t, versions, 2)

	var newDraft db.RankingVersion
	for _, v := range versions {
		if !v.PublishedAt.Valid {
			newDraft = v
		}
	}
	require.NotZero(t, newDraft.ID, "the branched version is a draft")

	draftItems, err := env.Queries.ListRankingItemsForVersion(ctx, newDraft.ID)
	require.NoError(t, err)
	require.Len(t, draftItems, 1)
	assert.Equal(t, "Carried over", draftItems[0].Title)
}

func TestCreateVersionFailsWhenADraftAlreadyExists(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()
	ctx := context.Background()
	slug := owner.Ranking.Uuid

	owner.HTMX(http.MethodPost, "/r/"+slug.String()+"/items", url.Values{"label": {"Ready"}})
	tiers, err := env.Queries.ListRankingTiersForVersion(ctx, owner.Draft.ID)
	require.NoError(t, err)
	items, err := env.Queries.ListRankingItemsForVersion(ctx, owner.Draft.ID)
	require.NoError(t, err)
	owner.HTMX(http.MethodPost, "/r/"+slug.String()+"/tiers/"+strconv.FormatInt(tiers[0].ID, 10)+"/items",
		url.Values{"item_id": {strconv.FormatInt(items[0].ID, 10)}})
	require.Equal(t, http.StatusOK, owner.Post("/r/"+slug.String()+"/publish", nil).Status)
	require.Equal(t, http.StatusOK, owner.Post("/r/"+slug.String()+"/versions", nil).Status)

	res := owner.Post("/r/"+slug.String()+"/versions", nil)
	assert.Equal(t, http.StatusConflict, res.Status, "the branch created above is still a draft in progress")
}

func TestViewingTheLiveRankingResolvesToTheMostRecentlyPublishedVersion(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()
	ctx := context.Background()

	published, err := env.Queries.PublishRankingVersion(ctx, owner.Draft.ID)
	require.NoError(t, err)
	_, err = env.Queries.CreateRankingVersion(ctx, db.CreateRankingVersionParams{
		ShortUuid: uuid.NewString()[:8],
		RankingID: owner.Ranking.ID,
	})
	require.NoError(t, err)

	res := owner.Get("/r/" + owner.Ranking.Uuid.String())

	require.Equal(t, http.StatusOK, res.Status)
	assert.Contains(t, Body(res.Body), "Published "+published.PublishedAt.Time.Format("Jan 2, 2006"))
}
