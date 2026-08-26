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

	res := owner.HTMX(http.MethodPost, "/r/"+owner.Ranking.Uuid.String()+"/v/"+owner.Draft.ShortUuid+"/items", url.Values{"label": {"Pretzels"}})

	require.Equal(t, http.StatusOK, res.Status)
	assert.Contains(t, Body(res.Body), "Pretzels")
	assert.NotContains(t, Body(res.Body), "<html", "htmx swaps must not include the layout")
}

func TestAddItemRequiresLabel(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()

	res := owner.HTMX(http.MethodPost, "/r/"+owner.Ranking.Uuid.String()+"/v/"+owner.Draft.ShortUuid+"/items", url.Values{"label": {"   "}})

	assert.Equal(t, http.StatusBadRequest, res.Status)
}

func TestAddItemWithALinkRendersAClickableCard(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()

	res := owner.HTMX(http.MethodPost, "/r/"+owner.Ranking.Uuid.String()+"/v/"+owner.Draft.ShortUuid+"/items",
		url.Values{"label": {"Tartine"}, "source_url": {"https://tartinebakery.com"}})

	require.Equal(t, http.StatusOK, res.Status)
	body := Body(res.Body)
	assert.Contains(t, body, `href="https://tartinebakery.com"`)
	assert.Contains(t, body, `rel="noopener noreferrer"`)
	assert.Contains(t, body, `draggable="false"`, "the anchor must not hijack the card's drag")
}

func TestAddItemRejectsALinkThatIsNotHTTP(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()

	res := owner.HTMX(http.MethodPost, "/r/"+owner.Ranking.Uuid.String()+"/v/"+owner.Draft.ShortUuid+"/items",
		url.Values{"label": {"Hostile"}, "source_url": {"javascript:alert(1)"}})

	assert.Equal(t, http.StatusUnprocessableEntity, res.Status)
}

func TestEditItemRendersTheFormAndUpdateSavesTheLink(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()
	ctx := context.Background()
	base := "/r/" + owner.Ranking.Uuid.String() + "/v/" + owner.Draft.ShortUuid

	owner.HTMX(http.MethodPost, base+"/items", url.Values{"label": {"Tartine"}})
	items, err := env.Queries.ListRankingItemsForVersion(ctx, owner.Draft.ID)
	require.NoError(t, err)
	require.Len(t, items, 1)
	itemPath := base + "/items/" + strconv.FormatInt(items[0].ID, 10)

	res := owner.HTMX(http.MethodGet, itemPath+"/edit", nil)
	require.Equal(t, http.StatusOK, res.Status)
	assert.Contains(t, Body(res.Body), `name="source_url"`)

	res = owner.HTMX(http.MethodPut, itemPath,
		url.Values{"label": {"Tartine"}, "source_url": {"https://tartinebakery.com"}})
	require.Equal(t, http.StatusOK, res.Status)
	assert.Contains(t, Body(res.Body), `href="https://tartinebakery.com"`)

	stored, err := env.Queries.GetRankingItem(ctx, items[0].ID)
	require.NoError(t, err)
	require.NotNil(t, stored.SourceUrl)
	assert.Equal(t, "https://tartinebakery.com", *stored.SourceUrl)
}

func TestUpdateItemIsRejectedAgainstAPublishedVersion(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()
	ctx := context.Background()
	base := "/r/" + owner.Ranking.Uuid.String() + "/v/" + owner.Draft.ShortUuid

	owner.HTMX(http.MethodPost, base+"/items", url.Values{"label": {"Tartine"}})
	items, err := env.Queries.ListRankingItemsForVersion(ctx, owner.Draft.ID)
	require.NoError(t, err)
	require.Len(t, items, 1)

	_, err = env.Queries.PublishRankingVersion(ctx, owner.Draft.ID)
	require.NoError(t, err)

	res := owner.HTMX(http.MethodPut, base+"/items/"+strconv.FormatInt(items[0].ID, 10),
		url.Values{"label": {"Renamed"}})

	assert.Equal(t, http.StatusForbidden, res.Status)
}

func TestAddItemsToTier(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()
	ctx := context.Background()
	slug := owner.Ranking.Uuid

	owner.HTMX(http.MethodPost, "/r/"+slug.String()+"/v/"+owner.Draft.ShortUuid+"/items", url.Values{"label": {"Pretzels"}})
	owner.HTMX(http.MethodPost, "/r/"+slug.String()+"/v/"+owner.Draft.ShortUuid+"/items", url.Values{"label": {"Olives"}})

	tiers, err := env.Queries.ListRankingTiersForVersion(ctx, owner.Draft.ID)
	require.NoError(t, err)
	items, err := env.Queries.ListRankingItemsForVersion(ctx, owner.Draft.ID)
	require.NoError(t, err)
	require.Len(t, items, 2)

	form := url.Values{"item_id": {strconv.FormatInt(items[0].ID, 10)}}
	res := owner.HTMX(http.MethodPost, "/r/"+slug.String()+"/v/"+owner.Draft.ShortUuid+"/tiers/"+strconv.FormatInt(tiers[0].ID, 10)+"/items", form)
	require.Equal(t, http.StatusOK, res.Status)

	form = url.Values{"item_id": {strconv.FormatInt(items[1].ID, 10)}}
	res = owner.HTMX(http.MethodPost, "/r/"+slug.String()+"/v/"+owner.Draft.ShortUuid+"/tiers/"+strconv.FormatInt(tiers[0].ID, 10)+"/items", form)
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

	res := owner.HTMX(http.MethodPost, "/r/"+slug.String()+"/v/"+owner.Draft.ShortUuid+"/tiers", url.Values{"label": {"F"}, "color": {"#111111"}})
	require.Equal(t, http.StatusOK, res.Status)
	assert.Contains(t, Body(res.Body), "F")

	tiers, err := env.Queries.ListRankingTiersForVersion(ctx, owner.Draft.ID)
	require.NoError(t, err)
	require.Len(t, tiers, len(services.DefaultTiers)+1)

	last := tiers[len(tiers)-1]
	assert.Equal(t, "F", last.Title)
	assert.Equal(t, "#111111", last.ColorHex)

	renamed := owner.HTMX(http.MethodPut, "/r/"+slug.String()+"/v/"+owner.Draft.ShortUuid+"/tiers/"+strconv.FormatInt(last.ID, 10),
		url.Values{"label": {"Trash"}, "color": {"#222222"}})
	require.Equal(t, http.StatusOK, renamed.Status)
	assert.Contains(t, Body(renamed.Body), "Trash")

	deleted := owner.HTMX(http.MethodDelete, "/r/"+slug.String()+"/v/"+owner.Draft.ShortUuid+"/tiers/"+strconv.FormatInt(last.ID, 10), nil)
	require.Equal(t, http.StatusAccepted, deleted.Status)
	assert.NotContains(t, Body(deleted.Body), "Trash")
}

func TestUpdateRankingTitleReturnsMetaPartial(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()

	res := owner.HTMX(http.MethodPost, "/r/"+owner.Ranking.Uuid.String(), url.Values{"title": {"Best snacks"}, "description": {"2026 edition"}})

	require.Equal(t, http.StatusOK, res.Status)
	assert.Contains(t, Body(res.Body), `value="Best snacks"`)
	assert.Contains(t, Body(res.Body), "<p>2026 edition</p>")
	assert.NotContains(t, Body(res.Body), "<html")
}


func TestViewRankingWithNoVersionInPathLoadsTheLiveVersion(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()

	res := owner.Get("/r/" + owner.Ranking.Uuid.String())

	require.Equal(t, http.StatusOK, res.Status)
	assert.Contains(t, Body(res.Body), "<span>Draft</span>")
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


func TestBoardVersionButtonReadsDraftWhileViewingTheDraft(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()

	res := owner.Get("/r/" + owner.Ranking.Uuid.String() + "/v/" + owner.Draft.ShortUuid)

	require.Equal(t, http.StatusOK, res.Status)
	assert.Contains(t, Body(res.Body), "<span>Draft</span>")
}

func TestBoardVersionButtonCarriesNumberAndPublishDateForAPublishedVersion(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()
	ctx := context.Background()

	published, err := env.Queries.PublishRankingVersion(ctx, owner.Draft.ID)
	require.NoError(t, err)

	res := owner.Get("/r/" + owner.Ranking.Uuid.String() + "/v/" + published.ShortUuid)

	require.Equal(t, http.StatusOK, res.Status)
	assert.Contains(t, Body(res.Body), "v1 · Published "+services.FormatPublishedAt(published))
	assert.NotContains(t, Body(res.Body), "<span>Draft</span>")
}

func TestBoardVersionNumberingFollowsPublishOrderAcrossThreeVersions(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()
	ctx := context.Background()

	first, err := env.Queries.PublishRankingVersion(ctx, owner.Draft.ID)
	require.NoError(t, err)

	secondDraft, err := env.Queries.CreateRankingVersion(ctx, db.CreateRankingVersionParams{
		ShortUuid: uuid.NewString()[:8],
		RankingID: owner.Ranking.ID,
	})
	require.NoError(t, err)
	second, err := env.Queries.PublishRankingVersion(ctx, secondDraft.ID)
	require.NoError(t, err)

	thirdDraft, err := env.Queries.CreateRankingVersion(ctx, db.CreateRankingVersionParams{
		ShortUuid: uuid.NewString()[:8],
		RankingID: owner.Ranking.ID,
	})
	require.NoError(t, err)
	third, err := env.Queries.PublishRankingVersion(ctx, thirdDraft.ID)
	require.NoError(t, err)

	body := Body(owner.Get("/r/" + owner.Ranking.Uuid.String() + "/v/" + third.ShortUuid).Body)
	assert.Contains(t, body, "v1 · Published "+services.FormatPublishedAt(first))
	assert.Contains(t, body, "v2 · Published "+services.FormatPublishedAt(second))
	assert.Contains(t, body, "v3 · Published "+services.FormatPublishedAt(third))
}

func TestBoardVersionButtonMatchesThePinnedVersionRatherThanTheLiveOne(t *testing.T) {
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

	onLive := Body(owner.Get("/r/" + owner.Ranking.Uuid.String()).Body)
	assert.Contains(t, onLive, "v1 · Published "+services.FormatPublishedAt(published))
	assert.NotContains(t, onLive, "<span>Draft</span>")

	onPinnedDraft := Body(owner.Get("/r/" + owner.Ranking.Uuid.String() + "/v/" + secondDraft.ShortUuid).Body)
	assert.Contains(t, onPinnedDraft, "<span>Draft</span>")
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

	onPublished := owner.Get("/r/" + owner.Ranking.Uuid.String() + "/v/" + published.ShortUuid).Body
	assert.Contains(t, Body(onPublished), `aria-current="true"`)
	assert.Contains(t, Body(onPublished), "/r/"+owner.Ranking.Uuid.String()+"/v/"+secondDraft.ShortUuid)

	onDraft := owner.Get("/r/" + owner.Ranking.Uuid.String() + "/v/" + secondDraft.ShortUuid).Body
	assert.Contains(t, Body(onDraft), "<span>Draft</span>")
	assert.Contains(t, Body(onDraft), "/r/"+owner.Ranking.Uuid.String()+"/v/"+published.ShortUuid)
}


func TestReorderTierItemsPersistsTheGivenOrder(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()
	ctx := context.Background()
	slug := owner.Ranking.Uuid

	owner.HTMX(http.MethodPost, "/r/"+slug.String()+"/v/"+owner.Draft.ShortUuid+"/items", url.Values{"label": {"First"}})
	owner.HTMX(http.MethodPost, "/r/"+slug.String()+"/v/"+owner.Draft.ShortUuid+"/items", url.Values{"label": {"Second"}})
	tiers, err := env.Queries.ListRankingTiersForVersion(ctx, owner.Draft.ID)
	require.NoError(t, err)
	items, err := env.Queries.ListRankingItemsForVersion(ctx, owner.Draft.ID)
	require.NoError(t, err)
	require.Len(t, items, 2)

	tierPath := "/r/" + slug.String() + "/v/" + owner.Draft.ShortUuid + "/tiers/" + strconv.FormatInt(tiers[0].ID, 10)
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

	owner.HTMX(http.MethodPost, "/r/"+slug.String()+"/v/"+owner.Draft.ShortUuid+"/items", url.Values{"label": {"Migrating"}})
	tiers, err := env.Queries.ListRankingTiersForVersion(ctx, owner.Draft.ID)
	require.NoError(t, err)
	items, err := env.Queries.ListRankingItemsForVersion(ctx, owner.Draft.ID)
	require.NoError(t, err)
	require.Len(t, items, 1)

	owner.HTMX(http.MethodPost, "/r/"+slug.String()+"/v/"+owner.Draft.ShortUuid+"/tiers/"+strconv.FormatInt(tiers[0].ID, 10)+"/items",
		url.Values{"item_id": {strconv.FormatInt(items[0].ID, 10)}})

	res := owner.HTMX(http.MethodPost, "/r/"+slug.String()+"/v/"+owner.Draft.ShortUuid+"/tiers/"+strconv.FormatInt(tiers[1].ID, 10)+"/items/reorder",
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

	res := owner.HTMX(http.MethodPost, "/r/"+slug.String()+"/v/"+owner.Draft.ShortUuid+"/tiers/reorder", reversed)
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

	owner.HTMX(http.MethodPost, "/r/"+slug.String()+"/v/"+owner.Draft.ShortUuid+"/items", url.Values{"label": {"Placed"}})
	tiers, err := env.Queries.ListRankingTiersForVersion(ctx, owner.Draft.ID)
	require.NoError(t, err)
	items, err := env.Queries.ListRankingItemsForVersion(ctx, owner.Draft.ID)
	require.NoError(t, err)
	owner.HTMX(http.MethodPost, "/r/"+slug.String()+"/v/"+owner.Draft.ShortUuid+"/tiers/"+strconv.FormatInt(tiers[0].ID, 10)+"/items",
		url.Values{"item_id": {strconv.FormatInt(items[0].ID, 10)}})

	res := owner.HTMX(http.MethodPost, "/r/"+slug.String()+"/v/"+owner.Draft.ShortUuid+"/items/"+strconv.FormatInt(items[0].ID, 10)+"/unrank", nil)
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

	tierRes := owner.HTMX(http.MethodPost, "/r/"+slug.String()+"/v/"+owner.Draft.ShortUuid+"/tiers", url.Values{"label": {"Doomed"}})
	require.Equal(t, http.StatusOK, tierRes.Status)
	tiers, err := env.Queries.ListRankingTiersForVersion(ctx, owner.Draft.ID)
	require.NoError(t, err)
	doomed := tiers[len(tiers)-1]

	owner.HTMX(http.MethodPost, "/r/"+slug.String()+"/v/"+owner.Draft.ShortUuid+"/items", url.Values{"label": {"Stranded"}})
	items, err := env.Queries.ListRankingItemsForVersion(ctx, owner.Draft.ID)
	require.NoError(t, err)
	owner.HTMX(http.MethodPost, "/r/"+slug.String()+"/v/"+owner.Draft.ShortUuid+"/tiers/"+strconv.FormatInt(doomed.ID, 10)+"/items",
		url.Values{"item_id": {strconv.FormatInt(items[0].ID, 10)}})

	res := owner.HTMX(http.MethodDelete, "/r/"+slug.String()+"/v/"+owner.Draft.ShortUuid+"/tiers/"+strconv.FormatInt(doomed.ID, 10), nil)
	require.Equal(t, http.StatusAccepted, res.Status)
	assert.Contains(t, Body(res.Body), `id="tray-items"`)
	assert.Contains(t, Body(res.Body), `hx-swap-oob="true"`)
	assert.Contains(t, Body(res.Body), "Stranded", "the deleted tier's item comes back in the tray fragment")

	placements, err := env.Queries.ListRankingItemTiersForVersion(ctx, owner.Draft.ID)
	require.NoError(t, err)
	assert.Empty(t, placements)
}


func TestPublishIsBlockedUntilTheVersionIsPublishable(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()

	res := owner.Post("/r/"+owner.Ranking.Uuid.String()+"/v/"+owner.Draft.ShortUuid+"/publish", nil)
	assert.Equal(t, http.StatusConflict, res.Status)
}

func TestPublishSucceedsAndRedirectsToThePublishedVersion(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()
	ctx := context.Background()
	slug := owner.Ranking.Uuid

	owner.HTMX(http.MethodPost, "/r/"+slug.String()+"/v/"+owner.Draft.ShortUuid+"/items", url.Values{"label": {"Ready"}})
	tiers, err := env.Queries.ListRankingTiersForVersion(ctx, owner.Draft.ID)
	require.NoError(t, err)
	items, err := env.Queries.ListRankingItemsForVersion(ctx, owner.Draft.ID)
	require.NoError(t, err)
	owner.HTMX(http.MethodPost, "/r/"+slug.String()+"/v/"+owner.Draft.ShortUuid+"/tiers/"+strconv.FormatInt(tiers[0].ID, 10)+"/items",
		url.Values{"item_id": {strconv.FormatInt(items[0].ID, 10)}})

	res := owner.Post("/r/"+slug.String()+"/v/"+owner.Draft.ShortUuid+"/publish", nil)
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

	owner.HTMX(http.MethodPost, "/r/"+slug.String()+"/v/"+owner.Draft.ShortUuid+"/items", url.Values{"label": {"Carried over"}})
	tiers, err := env.Queries.ListRankingTiersForVersion(ctx, owner.Draft.ID)
	require.NoError(t, err)
	items, err := env.Queries.ListRankingItemsForVersion(ctx, owner.Draft.ID)
	require.NoError(t, err)
	owner.HTMX(http.MethodPost, "/r/"+slug.String()+"/v/"+owner.Draft.ShortUuid+"/tiers/"+strconv.FormatInt(tiers[0].ID, 10)+"/items",
		url.Values{"item_id": {strconv.FormatInt(items[0].ID, 10)}})
	require.Equal(t, http.StatusOK, owner.Post("/r/"+slug.String()+"/v/"+owner.Draft.ShortUuid+"/publish", nil).Status)

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
	require.NotNil(t, draftItems[0].Title)
	assert.Equal(t, "Carried over", *draftItems[0].Title)
}

func TestCreateVersionFailsWhenADraftAlreadyExists(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()
	ctx := context.Background()
	slug := owner.Ranking.Uuid

	owner.HTMX(http.MethodPost, "/r/"+slug.String()+"/v/"+owner.Draft.ShortUuid+"/items", url.Values{"label": {"Ready"}})
	tiers, err := env.Queries.ListRankingTiersForVersion(ctx, owner.Draft.ID)
	require.NoError(t, err)
	items, err := env.Queries.ListRankingItemsForVersion(ctx, owner.Draft.ID)
	require.NoError(t, err)
	owner.HTMX(http.MethodPost, "/r/"+slug.String()+"/v/"+owner.Draft.ShortUuid+"/tiers/"+strconv.FormatInt(tiers[0].ID, 10)+"/items",
		url.Values{"item_id": {strconv.FormatInt(items[0].ID, 10)}})
	require.Equal(t, http.StatusOK, owner.Post("/r/"+slug.String()+"/v/"+owner.Draft.ShortUuid+"/publish", nil).Status)
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
	assert.Contains(t, Body(res.Body), "Published "+services.FormatPublishedAt(published))
}


func TestMutatingRequestsAgainstAPublishedVersionAreRejected(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()
	ctx := context.Background()
	slug := owner.Ranking.Uuid

	owner.HTMX(http.MethodPost, "/r/"+slug.String()+"/v/"+owner.Draft.ShortUuid+"/items", url.Values{"label": {"Ready"}})
	tiers, err := env.Queries.ListRankingTiersForVersion(ctx, owner.Draft.ID)
	require.NoError(t, err)
	items, err := env.Queries.ListRankingItemsForVersion(ctx, owner.Draft.ID)
	require.NoError(t, err)
	owner.HTMX(http.MethodPost, "/r/"+slug.String()+"/v/"+owner.Draft.ShortUuid+"/tiers/"+strconv.FormatInt(tiers[0].ID, 10)+"/items",
		url.Values{"item_id": {strconv.FormatInt(items[0].ID, 10)}})

	published, err := env.Queries.PublishRankingVersion(ctx, owner.Draft.ID)
	require.NoError(t, err)
	base := "/r/" + slug.String() + "/v/" + published.ShortUuid

	cases := []struct {
		name   string
		method string
		path   string
		form   url.Values
	}{
		{"add item", http.MethodPost, base + "/items", url.Values{"label": {"Blocked"}}},
		{"delete item", http.MethodDelete, base + "/items/" + strconv.FormatInt(items[0].ID, 10), nil},
		{"unrank item", http.MethodPost, base + "/items/" + strconv.FormatInt(items[0].ID, 10) + "/unrank", nil},
		{"add tier", http.MethodPost, base + "/tiers", url.Values{"label": {"Blocked"}, "color": {"#000000"}}},
		{"reorder tiers", http.MethodPost, base + "/tiers/reorder", url.Values{"tier_id": {strconv.FormatInt(tiers[0].ID, 10)}}},
		{"update tier", http.MethodPut, base + "/tiers/" + strconv.FormatInt(tiers[0].ID, 10), url.Values{"label": {"Blocked"}, "color": {"#000000"}}},
		{"delete tier", http.MethodDelete, base + "/tiers/" + strconv.FormatInt(tiers[0].ID, 10), nil},
		{"edit tier", http.MethodPost, base + "/tiers/" + strconv.FormatInt(tiers[0].ID, 10) + "/edit", nil},
		{"add item to tier", http.MethodPost, base + "/tiers/" + strconv.FormatInt(tiers[1].ID, 10) + "/items", url.Values{"item_id": {strconv.FormatInt(items[0].ID, 10)}}},
		{"reorder tier items", http.MethodPost, base + "/tiers/" + strconv.FormatInt(tiers[0].ID, 10) + "/items/reorder", url.Values{"item_id": {strconv.FormatInt(items[0].ID, 10)}}},
		{"publish", http.MethodPost, base + "/publish", nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := owner.HTMX(tc.method, tc.path, tc.form)
			assert.Equal(t, http.StatusForbidden, res.Status)
		})
	}
}

func TestAddItemSucceedsAgainstADraftVersion(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()

	res := owner.HTMX(http.MethodPost, "/r/"+owner.Ranking.Uuid.String()+"/v/"+owner.Draft.ShortUuid+"/items", url.Values{"label": {"Allowed"}})

	assert.Equal(t, http.StatusOK, res.Status)
}

func TestUpdatingTheRankingTitleStaysAllowedWhileViewingAPublishedVersion(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()
	ctx := context.Background()

	_, err := env.Queries.PublishRankingVersion(ctx, owner.Draft.ID)
	require.NoError(t, err)

	// handleUpdateRanking sits at POST /r/{uuid} — no version segment, since
	// the title and description aren't version-scoped — so this exercises
	// the same route regardless of which version the ranking's page happens
	// to be viewing.
	res := owner.HTMX(http.MethodPost, "/r/"+owner.Ranking.Uuid.String(),
		url.Values{"title": {"Renamed while published"}, "description": {""}})

	require.Equal(t, http.StatusOK, res.Status)
	assert.Contains(t, Body(res.Body), "Renamed while published", "the ranking's name isn't version-scoped, so this stays allowed")
}


func TestDescriptionRendersAsMarkdown(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()
	slug := owner.Ranking.Uuid.String()

	res := owner.HTMX(http.MethodPost, "/r/"+slug+"/description", url.Values{"description": {"Ranked **by hand**"}})
	require.Equal(t, http.StatusOK, res.Status)

	body := Body(res.Body)
	assert.Contains(t, body, "<strong>by hand</strong>")
	assert.NotContains(t, body, "**", "the markdown source doesn't reach the page")
}

func TestDescriptionMarkdownIsSanitized(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()
	slug := owner.Ranking.Uuid.String()

	form := url.Values{"description": {"<script>alert(1)</script> [x](javascript:alert(1))"}}
	res := owner.HTMX(http.MethodPost, "/r/"+slug+"/description", form)
	require.Equal(t, http.StatusOK, res.Status)

	body := Body(res.Body)
	assert.NotContains(t, body, "<script>")
	assert.NotContains(t, body, "javascript:")
}

func TestEditingTheDescriptionSwapsInATextarea(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()
	slug := owner.Ranking.Uuid.String()

	owner.HTMX(http.MethodPost, "/r/"+slug+"/description", url.Values{"description": {"Ranked by hand"}})

	res := owner.HTMX(http.MethodGet, "/r/"+slug+"/description/edit", nil)

	require.Equal(t, http.StatusOK, res.Status)
	body := Body(res.Body)
	assert.Contains(t, body, "<textarea")
	assert.Contains(t, body, "Ranked by hand", "the editor opens on the markdown source")

	cancelled := owner.HTMX(http.MethodGet, "/r/"+slug+"/description", nil)
	require.Equal(t, http.StatusOK, cancelled.Status)
	assert.NotContains(t, Body(cancelled.Body), "<textarea", "cancelling returns the reading state")
}

func TestSavingTheDescriptionLeavesTheTitleAlone(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()
	ctx := context.Background()
	slug := owner.Ranking.Uuid.String()

	owner.HTMX(http.MethodPost, "/r/"+slug, url.Values{"title": {"Best pastries"}})
	owner.HTMX(http.MethodPost, "/r/"+slug+"/description", url.Values{"description": {"Ranked by hand"}})

	stored, err := env.Queries.GetRankingByUUID(ctx, owner.Ranking.Uuid)
	require.NoError(t, err)
	assert.Equal(t, "Best pastries", stored.Name)
	assert.Equal(t, "Ranked by hand", stored.Description)
}

func TestAPublishedVersionRendersNoDescriptionEditor(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()
	ctx := context.Background()
	slug := owner.Ranking.Uuid.String()

	owner.HTMX(http.MethodPost, "/r/"+slug+"/description", url.Values{"description": {"Ranked by hand"}})
	published, err := env.Queries.PublishRankingVersion(ctx, owner.Draft.ID)
	require.NoError(t, err)

	res := owner.Get("/r/" + slug + "/v/" + published.ShortUuid)

	require.Equal(t, http.StatusOK, res.Status)
	body := Body(res.Body)
	assert.Contains(t, body, "Ranked by hand")
	assert.NotContains(t, body, `aria-label="Edit description"`)
}


func TestAddItemResponseCarriesThePublishActionOutOfBand(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()

	res := owner.HTMX(http.MethodPost, "/r/"+owner.Ranking.Uuid.String()+"/v/"+owner.Draft.ShortUuid+"/items", url.Values{"label": {"Pretzels"}})

	require.Equal(t, http.StatusOK, res.Status)
	body := Body(res.Body)
	assert.Contains(t, body, `id="board-version-actions"`)
	assert.Contains(t, body, `hx-swap-oob="true"`)
}

func TestDeleteTierResponseCarriesThePublishActionOutOfBand(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()
	ctx := context.Background()
	slug := owner.Ranking.Uuid

	tierRes := owner.HTMX(http.MethodPost, "/r/"+slug.String()+"/v/"+owner.Draft.ShortUuid+"/tiers", url.Values{"label": {"Doomed"}})
	require.Equal(t, http.StatusOK, tierRes.Status)
	tiers, err := env.Queries.ListRankingTiersForVersion(ctx, owner.Draft.ID)
	require.NoError(t, err)
	doomed := tiers[len(tiers)-1]

	res := owner.HTMX(http.MethodDelete, "/r/"+slug.String()+"/v/"+owner.Draft.ShortUuid+"/tiers/"+strconv.FormatInt(doomed.ID, 10), nil)
	require.Equal(t, http.StatusAccepted, res.Status)
	body := Body(res.Body)
	assert.Contains(t, body, `id="board-version-actions"`, "the publish action's own out-of-band fragment, alongside the tray's")
	assert.Contains(t, body, `id="tray-items"`)
}

func TestRenameTierDoesNotCarryThePublishActionOutOfBand(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()
	ctx := context.Background()
	slug := owner.Ranking.Uuid

	tiers, err := env.Queries.ListRankingTiersForVersion(ctx, owner.Draft.ID)
	require.NoError(t, err)

	res := owner.HTMX(http.MethodPut, "/r/"+slug.String()+"/v/"+owner.Draft.ShortUuid+"/tiers/"+strconv.FormatInt(tiers[0].ID, 10),
		url.Values{"label": {"Renamed"}, "color": {"#123456"}})

	require.Equal(t, http.StatusOK, res.Status)
	assert.NotContains(t, Body(res.Body), `id="board-version-actions"`, "renaming a tier can't flip whether the version is publishable")
}


// A bare `draggable` attribute is invalid; it must be "true" or "false".
func TestBoardRendersDraggableWithAnExplicitValue(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()
	ctx := context.Background()
	slug := owner.Ranking.Uuid

	owner.HTMX(http.MethodPost, "/r/"+slug.String()+"/v/"+owner.Draft.ShortUuid+"/items", url.Values{"label": {"Pretzels"}})

	draft := Body(owner.Get("/r/" + slug.String() + "/v/" + owner.Draft.ShortUuid).Body)
	assert.Contains(t, draft, `draggable="true"`)
	assert.NotRegexp(t, `draggable[\s>]`, draft, "a bare draggable attribute reads as auto, not true")

	published, err := env.Queries.PublishRankingVersion(ctx, owner.Draft.ID)
	require.NoError(t, err)

	locked := Body(owner.Get("/r/" + slug.String() + "/v/" + published.ShortUuid).Body)
	assert.Contains(t, locked, `draggable="false"`)
	assert.NotContains(t, locked, `draggable="true"`)
}

func TestPublishedVersionPageHidesEditingControls(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()
	ctx := context.Background()

	published, err := env.Queries.PublishRankingVersion(ctx, owner.Draft.ID)
	require.NoError(t, err)

	res := owner.Get("/r/" + owner.Ranking.Uuid.String() + "/v/" + published.ShortUuid)
	require.Equal(t, http.StatusOK, res.Status)

	body := Body(res.Body)
	assert.NotContains(t, body, `id="edit-tiers"`)
	assert.NotContains(t, body, "New tier label", "the add-tier form is hidden")
	assert.NotContains(t, body, "Add an item", "the add-item form is hidden")
	assert.NotContains(t, body, `aria-label="Ranking title"`, "the title is read-only text, not an input")
	assert.NotContains(t, body, `aria-label="Ranking description"`, "the description is read-only text, not an input")
}

func TestDraftVersionPageShowsEditingControls(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()

	res := owner.Get("/r/" + owner.Ranking.Uuid.String() + "/v/" + owner.Draft.ShortUuid)
	require.Equal(t, http.StatusOK, res.Status)

	body := Body(res.Body)
	assert.Contains(t, body, `id="edit-tiers"`)
	assert.Contains(t, body, "New tier label")
	assert.Contains(t, body, "Add an item")
	assert.Contains(t, body, `name="title"`)
}
