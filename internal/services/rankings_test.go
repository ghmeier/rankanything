package services_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/ghmeier/rankanything/internal/db"
	"github.com/ghmeier/rankanything/internal/services"
	"github.com/ghmeier/rankanything/internal/testsupport"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newOwnedRanking builds a RankingsService against a fresh test database,
// creates a signed-in owner, and returns the service, a background context,
// the owned ranking, and its seeded draft version — the fixture every test
// below builds on.
func newOwnedRanking(t *testing.T) (*services.RankingsService, context.Context, db.Ranking, db.RankingVersion) {
	t.Helper()

	env := testsupport.NewEnv(t)
	ctx := context.Background()
	svc := &services.RankingsService{Queries: env.Queries, Pool: env.Tx}

	user, err := env.Queries.CreateUser(ctx, db.CreateUserParams{
		Email:        "owner+" + uuid.NewString() + "@example.com",
		PasswordHash: "hash",
	})
	require.NoError(t, err)

	ranking, err := svc.CreateForUser(ctx, services.CreateForUserRequest{UserID: user.ID})
	require.NoError(t, err)

	version, err := env.Queries.ResolveLiveRankingVersion(ctx, ranking.ID)
	require.NoError(t, err)

	return svc, ctx, ranking, version
}


func TestCreateForUserSeedsTheDraftVersionWithDefaultTiers(t *testing.T) {
	t.Parallel()

	svc, ctx, ranking, version := newOwnedRanking(t)

	assert.Equal(t, "Untitled ranking", ranking.Name)
	assert.False(t, version.PublishedAt.Valid, "a freshly created version has no published_at")

	tiers, err := svc.Queries.ListRankingTiersForVersion(ctx, version.ID)
	require.NoError(t, err)
	require.Len(t, tiers, len(services.DefaultTiers))
	for i, want := range services.DefaultTiers {
		assert.Equal(t, want.Label, tiers[i].Title)
		assert.Equal(t, want.Color, tiers[i].ColorHex)
	}
}


func TestGetRankingReturnsOwnedRanking(t *testing.T) {
	t.Parallel()

	svc, ctx, ranking, _ := newOwnedRanking(t)

	found, err := svc.GetRanking(ctx, ranking.Uuid)
	require.NoError(t, err)
	assert.Equal(t, ranking.ID, found.ID)
}

func TestGetRankingUnknownUUIDNotFound(t *testing.T) {
	t.Parallel()

	svc, ctx, _, _ := newOwnedRanking(t)

	_, err := svc.GetRanking(ctx, uuid.New())
	assert.ErrorIs(t, err, services.ErrRankingNotFound)
}


func TestResolveVersionWithNoShortUUIDReturnsTheLiveVersion(t *testing.T) {
	t.Parallel()

	svc, ctx, ranking, draft := newOwnedRanking(t)

	resolved, err := svc.ResolveVersion(ctx, ranking, "")
	require.NoError(t, err)
	assert.Equal(t, draft.ID, resolved.ID)
}

func TestResolveVersionPinsAVersionByItsShortUUID(t *testing.T) {
	t.Parallel()

	svc, ctx, ranking, draft := newOwnedRanking(t)

	resolved, err := svc.ResolveVersion(ctx, ranking, draft.ShortUuid)
	require.NoError(t, err)
	assert.Equal(t, draft.ID, resolved.ID)
}

func TestResolveVersionUnknownShortUUIDErrors(t *testing.T) {
	t.Parallel()

	svc, ctx, ranking, _ := newOwnedRanking(t)

	_, err := svc.ResolveVersion(ctx, ranking, "nosuchid")
	assert.Error(t, err)
}


func TestListVersionsReturnsOnlyTheGivenRankingsVersions(t *testing.T) {
	t.Parallel()

	svc, ctx, ranking, draft := newOwnedRanking(t)
	_, _, otherRanking, _ := newOwnedRanking(t)

	versions, err := svc.ListVersions(ctx, services.ListVersionsRequest{RankingID: ranking.ID})
	require.NoError(t, err)

	require.Len(t, versions, 1)
	assert.Equal(t, draft.ID, versions[0].ID)
	assert.NotEqual(t, otherRanking.ID, ranking.ID)
}

func TestListVersionsIncludesBothPublishedAndDraftVersions(t *testing.T) {
	t.Parallel()

	svc, ctx, ranking, draft := newOwnedRanking(t)

	published, err := svc.Queries.PublishRankingVersion(ctx, draft.ID)
	require.NoError(t, err)
	newDraft, err := svc.Queries.CreateRankingVersion(ctx, db.CreateRankingVersionParams{
		ShortUuid: uuid.NewString()[:8],
		RankingID: ranking.ID,
	})
	require.NoError(t, err)

	versions, err := svc.ListVersions(ctx, services.ListVersionsRequest{RankingID: ranking.ID})
	require.NoError(t, err)

	ids := make([]int64, len(versions))
	for i, v := range versions {
		ids[i] = v.ID
	}
	require.Len(t, ids, 2)
	assert.Contains(t, ids, published.ID)
	assert.Contains(t, ids, newDraft.ID)
}


func TestUpdateRankingChangesTitleAndDescription(t *testing.T) {
	t.Parallel()

	svc, ctx, ranking, _ := newOwnedRanking(t)

	updated, err := svc.UpdateRanking(ctx, services.UpdateRankingRequest{
		UUID:        ranking.Uuid,
		Name:        stringPtr("My Ranking"),
		Description: stringPtr("A test ranking"),
	})
	require.NoError(t, err)

	assert.Equal(t, "My Ranking", updated.Name)
	assert.Equal(t, "A test ranking", updated.Description)
}

func TestUpdateRankingLeavesAnOmittedFieldAlone(t *testing.T) {
	t.Parallel()

	svc, ctx, ranking, _ := newOwnedRanking(t)

	_, err := svc.UpdateRanking(ctx, services.UpdateRankingRequest{
		UUID:        ranking.Uuid,
		Name:        stringPtr("My Ranking"),
		Description: stringPtr("A test ranking"),
	})
	require.NoError(t, err)

	renamed, err := svc.UpdateRanking(ctx, services.UpdateRankingRequest{
		UUID: ranking.Uuid,
		Name: stringPtr("Renamed"),
	})
	require.NoError(t, err)

	assert.Equal(t, "Renamed", renamed.Name)
	assert.Equal(t, "A test ranking", renamed.Description, "a title edit carries no description and must not clear it")
}

func stringPtr(s string) *string { return &s }

func TestUpdateRankingUnknownUUIDNotFound(t *testing.T) {
	t.Parallel()

	svc, ctx, _, _ := newOwnedRanking(t)

	_, err := svc.UpdateRanking(ctx, services.UpdateRankingRequest{UUID: uuid.New(), Name: stringPtr("Nope")})
	assert.ErrorIs(t, err, services.ErrRankingNotFound)
}


func TestAddItemAddsToTheGivenVersion(t *testing.T) {
	t.Parallel()

	svc, ctx, _, version := newOwnedRanking(t)

	item, err := svc.AddItem(ctx, services.AddItemRequest{
		VersionID:      version.ID,
		Title:          "Pretzels",
		ImageSourceURL: "https://example.com/pretzel.jpg",
	})
	require.NoError(t, err)

	require.NotNil(t, item.Title)
	assert.Equal(t, "Pretzels", *item.Title)
	require.NotNil(t, item.ImageSourceUrl)
	assert.Equal(t, "https://example.com/pretzel.jpg", *item.ImageSourceUrl)

	items, err := svc.Queries.ListRankingItemsForVersion(ctx, version.ID)
	require.NoError(t, err)
	assert.Len(t, items, 1)
}

func TestAddItemWithoutAnImageLeavesItNil(t *testing.T) {
	t.Parallel()

	svc, ctx, _, version := newOwnedRanking(t)

	item, err := svc.AddItem(ctx, services.AddItemRequest{VersionID: version.ID, Title: "No image"})
	require.NoError(t, err)

	assert.Nil(t, item.ImageSourceUrl)
}

func TestAddItemStoresItsLink(t *testing.T) {
	t.Parallel()

	svc, ctx, _, version := newOwnedRanking(t)

	item, err := svc.AddItem(ctx, services.AddItemRequest{
		VersionID: version.ID,
		Title:     "Tartine",
		SourceURL: "https://tartinebakery.com",
	})
	require.NoError(t, err)

	require.NotNil(t, item.SourceUrl)
	assert.Equal(t, "https://tartinebakery.com", *item.SourceUrl)
}

func TestAddItemWithoutALinkLeavesItNil(t *testing.T) {
	t.Parallel()

	svc, ctx, _, version := newOwnedRanking(t)

	item, err := svc.AddItem(ctx, services.AddItemRequest{VersionID: version.ID, Title: "No link"})
	require.NoError(t, err)

	assert.Nil(t, item.SourceUrl)
}

func TestAddItemRejectsALinkThatIsNotHTTP(t *testing.T) {
	t.Parallel()

	svc, ctx, _, version := newOwnedRanking(t)

	_, err := svc.AddItem(ctx, services.AddItemRequest{
		VersionID: version.ID,
		Title:     "Hostile",
		SourceURL: "javascript:alert(1)",
	})
	assert.ErrorIs(t, err, services.ErrInvalidLink)
}

func TestUpdateItemChangesTitleAndLink(t *testing.T) {
	t.Parallel()

	svc, ctx, _, version := newOwnedRanking(t)

	item, err := svc.AddItem(ctx, services.AddItemRequest{VersionID: version.ID, Title: "Tartine"})
	require.NoError(t, err)

	updated, err := svc.UpdateItem(ctx, services.UpdateItemRequest{
		VersionID: version.ID,
		ItemID:    item.ID,
		Title:     "Tartine Manufactory",
		SourceURL: "https://tartinebakery.com/manufactory",
	})
	require.NoError(t, err)

	require.NotNil(t, updated.Title)
	assert.Equal(t, "Tartine Manufactory", *updated.Title)
	require.NotNil(t, updated.SourceUrl)
	assert.Equal(t, "https://tartinebakery.com/manufactory", *updated.SourceUrl)
}

func TestUpdateItemClearsALinkWhenTheFieldIsBlank(t *testing.T) {
	t.Parallel()

	svc, ctx, _, version := newOwnedRanking(t)

	item, err := svc.AddItem(ctx, services.AddItemRequest{
		VersionID: version.ID,
		Title:     "Tartine",
		SourceURL: "https://tartinebakery.com",
	})
	require.NoError(t, err)

	updated, err := svc.UpdateItem(ctx, services.UpdateItemRequest{
		VersionID: version.ID,
		ItemID:    item.ID,
		Title:     *item.Title,
	})
	require.NoError(t, err)

	assert.Nil(t, updated.SourceUrl)
}

func TestUpdateItemFromAnotherRankingsVersionIsRejected(t *testing.T) {
	t.Parallel()

	svc, ctx, versionA, versionB := twoOwnedRankings(t)

	item, err := svc.AddItem(ctx, services.AddItemRequest{VersionID: versionB.ID, Title: "Elsewhere"})
	require.NoError(t, err)

	_, err = svc.UpdateItem(ctx, services.UpdateItemRequest{
		VersionID: versionA.ID,
		ItemID:    item.ID,
		Title:     "Stolen",
	})
	assert.ErrorIs(t, err, services.ErrRankingNotFound)
}

func TestDeleteItemRemovesItAndItsTierPlacement(t *testing.T) {
	t.Parallel()

	svc, ctx, _, version := newOwnedRanking(t)

	item, err := svc.AddItem(ctx, services.AddItemRequest{VersionID: version.ID, Title: "ToDelete"})
	require.NoError(t, err)

	tiers, err := svc.Queries.ListRankingTiersForVersion(ctx, version.ID)
	require.NoError(t, err)
	_, err = svc.AddItemToTier(ctx, services.AddItemToTierRequest{VersionID: version.ID, TierID: tiers[0].ID, ItemID: item.ID})
	require.NoError(t, err)

	err = svc.DeleteItem(ctx, services.DeleteItemRequest{VersionID: version.ID, ItemID: item.ID})
	require.NoError(t, err)

	items, err := svc.Queries.ListRankingItemsForVersion(ctx, version.ID)
	require.NoError(t, err)
	assert.Empty(t, items)

	placements, err := svc.Queries.ListRankingItemTiersForVersion(ctx, version.ID)
	require.NoError(t, err)
	assert.Empty(t, placements)
}

func TestDeleteItemFromAnotherRankingsVersionIsRejected(t *testing.T) {
	t.Parallel()

	svc, ctx, versionA, versionB := twoOwnedRankings(t)

	item, err := svc.AddItem(ctx, services.AddItemRequest{VersionID: versionB.ID, Title: "Elsewhere"})
	require.NoError(t, err)

	err = svc.DeleteItem(ctx, services.DeleteItemRequest{VersionID: versionA.ID, ItemID: item.ID})
	assert.ErrorIs(t, err, services.ErrRankingNotFound)
}


func TestAddTierAppendsAtTheEnd(t *testing.T) {
	t.Parallel()

	svc, ctx, ranking, version := newOwnedRanking(t)
	initialCount := len(services.DefaultTiers)

	tier, err := svc.AddTier(ctx, services.AddTierRequest{
		VersionID: version.ID, RankingID: ranking.ID, Title: "F", Color: "#111111",
	})
	require.NoError(t, err)

	assert.Equal(t, "F", tier.Title)
	assert.Equal(t, "#111111", tier.ColorHex)
	assert.Equal(t, int16(initialCount), tier.Position)

	tiers, err := svc.Queries.ListRankingTiersForVersion(ctx, version.ID)
	require.NoError(t, err)
	assert.Len(t, tiers, initialCount+1)
}

func TestAddTierDefaultsTitleAndColorWhenBlank(t *testing.T) {
	t.Parallel()

	svc, ctx, ranking, version := newOwnedRanking(t)

	tier, err := svc.AddTier(ctx, services.AddTierRequest{VersionID: version.ID, RankingID: ranking.ID})
	require.NoError(t, err)

	assert.Equal(t, "New tier", tier.Title)
	assert.Equal(t, "#94a3b8", tier.ColorHex)
}

func TestUpdateTierChangesTitleAndColor(t *testing.T) {
	t.Parallel()

	svc, ctx, _, version := newOwnedRanking(t)
	tiers, err := svc.Queries.ListRankingTiersForVersion(ctx, version.ID)
	require.NoError(t, err)

	updated, err := svc.UpdateTier(ctx, services.UpdateTierRequest{
		VersionID: version.ID, TierID: tiers[0].ID, Title: "Top", Color: "#ff0000",
	})
	require.NoError(t, err)

	assert.Equal(t, "Top", updated.Title)
	assert.Equal(t, "#ff0000", updated.ColorHex)
}

func TestUpdateTierKeepsUnchangedFieldsWhenBlank(t *testing.T) {
	t.Parallel()

	svc, ctx, _, version := newOwnedRanking(t)
	tiers, err := svc.Queries.ListRankingTiersForVersion(ctx, version.ID)
	require.NoError(t, err)

	updated, err := svc.UpdateTier(ctx, services.UpdateTierRequest{
		VersionID: version.ID, TierID: tiers[0].ID, Color: "#aabbcc",
	})
	require.NoError(t, err)

	assert.Equal(t, tiers[0].Title, updated.Title)
	assert.Equal(t, "#aabbcc", updated.ColorHex)
}

func TestUpdateTierFromAnotherVersionIsRejected(t *testing.T) {
	t.Parallel()

	svc, ctx, versionA, versionB := twoOwnedRankings(t)
	tiersB, err := svc.Queries.ListRankingTiersForVersion(ctx, versionB.ID)
	require.NoError(t, err)

	_, err = svc.UpdateTier(ctx, services.UpdateTierRequest{VersionID: versionA.ID, TierID: tiersB[0].ID, Title: "Hijacked"})
	assert.ErrorIs(t, err, services.ErrRankingNotFound)
}

func TestGetTierReturnsTheRequestedTier(t *testing.T) {
	t.Parallel()

	svc, ctx, _, version := newOwnedRanking(t)
	tiers, err := svc.Queries.ListRankingTiersForVersion(ctx, version.ID)
	require.NoError(t, err)

	tier, err := svc.GetTier(ctx, services.GetTierRequest{VersionID: version.ID, TierID: tiers[0].ID})
	require.NoError(t, err)

	assert.Equal(t, tiers[0].ID, tier.ID)
	assert.Equal(t, tiers[0].Title, tier.Title)
}

func TestGetTierFromAnotherVersionIsRejected(t *testing.T) {
	t.Parallel()

	svc, ctx, versionA, versionB := twoOwnedRankings(t)
	tiersB, err := svc.Queries.ListRankingTiersForVersion(ctx, versionB.ID)
	require.NoError(t, err)

	_, err = svc.GetTier(ctx, services.GetTierRequest{VersionID: versionA.ID, TierID: tiersB[0].ID})
	assert.ErrorIs(t, err, services.ErrRankingNotFound)
}

func TestDeleteTierRemovesItAndReturnsItsItemsToTheTray(t *testing.T) {
	t.Parallel()

	svc, ctx, ranking, version := newOwnedRanking(t)
	initialCount := len(services.DefaultTiers)

	custom, err := svc.AddTier(ctx, services.AddTierRequest{VersionID: version.ID, RankingID: ranking.ID, Title: "Extra"})
	require.NoError(t, err)

	item, err := svc.AddItem(ctx, services.AddItemRequest{VersionID: version.ID, Title: "Stranded"})
	require.NoError(t, err)
	_, err = svc.AddItemToTier(ctx, services.AddItemToTierRequest{VersionID: version.ID, TierID: custom.ID, ItemID: item.ID})
	require.NoError(t, err)

	err = svc.DeleteTier(ctx, services.DeleteTierRequest{VersionID: version.ID, TierID: custom.ID})
	require.NoError(t, err)

	tiers, err := svc.Queries.ListRankingTiersForVersion(ctx, version.ID)
	require.NoError(t, err)
	assert.Len(t, tiers, initialCount)

	items, err := svc.Queries.ListRankingItemsForVersion(ctx, version.ID)
	require.NoError(t, err)
	require.Len(t, items, 1, "deleting the tier must not delete its item")

	placements, err := svc.Queries.ListRankingItemTiersForVersion(ctx, version.ID)
	require.NoError(t, err)
	assert.Empty(t, placements, "the item's placement is gone even though the item survives")
}

func TestDeleteTierFromAnotherVersionIsRejected(t *testing.T) {
	t.Parallel()

	svc, ctx, versionA, versionB := twoOwnedRankings(t)
	tiersB, err := svc.Queries.ListRankingTiersForVersion(ctx, versionB.ID)
	require.NoError(t, err)

	err = svc.DeleteTier(ctx, services.DeleteTierRequest{VersionID: versionA.ID, TierID: tiersB[0].ID})
	assert.ErrorIs(t, err, services.ErrRankingNotFound)
}


func TestAddItemToTierPlacesTheItem(t *testing.T) {
	t.Parallel()

	svc, ctx, _, version := newOwnedRanking(t)
	tiers, err := svc.Queries.ListRankingTiersForVersion(ctx, version.ID)
	require.NoError(t, err)
	item, err := svc.AddItem(ctx, services.AddItemRequest{VersionID: version.ID, Title: "Alpha"})
	require.NoError(t, err)

	_, err = svc.AddItemToTier(ctx, services.AddItemToTierRequest{VersionID: version.ID, TierID: tiers[0].ID, ItemID: item.ID})
	require.NoError(t, err)

	placements, err := svc.Queries.ListRankingItemTiersForTier(ctx, tiers[0].ID)
	require.NoError(t, err)
	require.Len(t, placements, 1)
	assert.Equal(t, item.ID, placements[0].RankingItemID)
}

func TestAddItemToTierMovesTheItemOutOfItsPriorTier(t *testing.T) {
	t.Parallel()

	svc, ctx, _, version := newOwnedRanking(t)
	tiers, err := svc.Queries.ListRankingTiersForVersion(ctx, version.ID)
	require.NoError(t, err)
	item, err := svc.AddItem(ctx, services.AddItemRequest{VersionID: version.ID, Title: "Alpha"})
	require.NoError(t, err)

	_, err = svc.AddItemToTier(ctx, services.AddItemToTierRequest{VersionID: version.ID, TierID: tiers[0].ID, ItemID: item.ID})
	require.NoError(t, err)
	_, err = svc.AddItemToTier(ctx, services.AddItemToTierRequest{VersionID: version.ID, TierID: tiers[1].ID, ItemID: item.ID})
	require.NoError(t, err)

	firstTierPlacements, err := svc.Queries.ListRankingItemTiersForTier(ctx, tiers[0].ID)
	require.NoError(t, err)
	assert.Empty(t, firstTierPlacements)

	secondTierPlacements, err := svc.Queries.ListRankingItemTiersForTier(ctx, tiers[1].ID)
	require.NoError(t, err)
	require.Len(t, secondTierPlacements, 1)
}

func TestAddItemToTierRejectsATierFromAnotherVersion(t *testing.T) {
	t.Parallel()

	svc, ctx, versionA, versionB := twoOwnedRankings(t)
	tiersB, err := svc.Queries.ListRankingTiersForVersion(ctx, versionB.ID)
	require.NoError(t, err)
	itemA, err := svc.AddItem(ctx, services.AddItemRequest{VersionID: versionA.ID, Title: "Alpha"})
	require.NoError(t, err)

	_, err = svc.AddItemToTier(ctx, services.AddItemToTierRequest{VersionID: versionA.ID, TierID: tiersB[0].ID, ItemID: itemA.ID})
	assert.ErrorIs(t, err, services.ErrInvalidTierPlacement)
}

func TestAddItemToTierRejectsAnItemFromAnotherVersion(t *testing.T) {
	t.Parallel()

	svc, ctx, versionA, versionB := twoOwnedRankings(t)
	tiersA, err := svc.Queries.ListRankingTiersForVersion(ctx, versionA.ID)
	require.NoError(t, err)
	itemB, err := svc.AddItem(ctx, services.AddItemRequest{VersionID: versionB.ID, Title: "Beta"})
	require.NoError(t, err)

	_, err = svc.AddItemToTier(ctx, services.AddItemToTierRequest{VersionID: versionA.ID, TierID: tiersA[0].ID, ItemID: itemB.ID})
	assert.ErrorIs(t, err, services.ErrInvalidTierPlacement)
}


func TestGetBoardListsTiersItemsAndPlacements(t *testing.T) {
	t.Parallel()

	svc, ctx, ranking, version := newOwnedRanking(t)
	item, err := svc.AddItem(ctx, services.AddItemRequest{VersionID: version.ID, Title: "Rank me"})
	require.NoError(t, err)
	tiers, err := svc.Queries.ListRankingTiersForVersion(ctx, version.ID)
	require.NoError(t, err)
	_, err = svc.AddItemToTier(ctx, services.AddItemToTierRequest{VersionID: version.ID, TierID: tiers[0].ID, ItemID: item.ID})
	require.NoError(t, err)

	board, err := svc.GetBoard(ctx, ranking, version)
	require.NoError(t, err)

	assert.True(t, board.IsDraft)
	assert.Len(t, board.Tiers, len(services.DefaultTiers))
	require.Len(t, board.Items, 1)
	require.NotNil(t, board.Items[0].Title)
	assert.Equal(t, "Rank me", *board.Items[0].Title)
	require.Len(t, board.Placements, 1)
	assert.Equal(t, item.ID, board.Placements[0].RankingItemID)
}

// twoOwnedRankings builds two separate signed-in owners within the same test
// database, so foreign keys resolve across them, and returns the shared
// service plus each ranking's draft version — the fixture the cross-version
// rejection tests build on.
func twoOwnedRankings(t *testing.T) (*services.RankingsService, context.Context, db.RankingVersion, db.RankingVersion) {
	t.Helper()

	env := testsupport.NewEnv(t)
	ctx := context.Background()
	svc := &services.RankingsService{Queries: env.Queries, Pool: env.Tx}

	userA, err := env.Queries.CreateUser(ctx, db.CreateUserParams{Email: "a+" + uuid.NewString() + "@example.com", PasswordHash: "hash"})
	require.NoError(t, err)
	userB, err := env.Queries.CreateUser(ctx, db.CreateUserParams{Email: "b+" + uuid.NewString() + "@example.com", PasswordHash: "hash"})
	require.NoError(t, err)

	rankingA, err := svc.CreateForUser(ctx, services.CreateForUserRequest{UserID: userA.ID})
	require.NoError(t, err)
	versionA, err := env.Queries.ResolveLiveRankingVersion(ctx, rankingA.ID)
	require.NoError(t, err)

	rankingB, err := svc.CreateForUser(ctx, services.CreateForUserRequest{UserID: userB.ID})
	require.NoError(t, err)
	versionB, err := env.Queries.ResolveLiveRankingVersion(ctx, rankingB.ID)
	require.NoError(t, err)

	return svc, ctx, versionA, versionB
}


func TestReorderTierItemsChangesOrderWithinATier(t *testing.T) {
	t.Parallel()

	svc, ctx, _, version := newOwnedRanking(t)
	tiers, err := svc.Queries.ListRankingTiersForVersion(ctx, version.ID)
	require.NoError(t, err)

	first, err := svc.AddItem(ctx, services.AddItemRequest{VersionID: version.ID, Title: "First"})
	require.NoError(t, err)
	second, err := svc.AddItem(ctx, services.AddItemRequest{VersionID: version.ID, Title: "Second"})
	require.NoError(t, err)
	_, err = svc.AddItemToTier(ctx, services.AddItemToTierRequest{VersionID: version.ID, TierID: tiers[0].ID, ItemID: first.ID})
	require.NoError(t, err)
	_, err = svc.AddItemToTier(ctx, services.AddItemToTierRequest{VersionID: version.ID, TierID: tiers[0].ID, ItemID: second.ID})
	require.NoError(t, err)

	items, err := svc.ReorderTierItems(ctx, services.ReorderTierItemsRequest{
		VersionID: version.ID, TierID: tiers[0].ID, ItemIDs: []int64{second.ID, first.ID},
	})
	require.NoError(t, err)
	require.Len(t, items, 2)

	placements, err := svc.Queries.ListRankingItemTiersForTier(ctx, tiers[0].ID)
	require.NoError(t, err)
	require.Len(t, placements, 2)
	assert.Equal(t, second.ID, placements[0].RankingItemID, "the reordered position comes first")
	assert.Equal(t, first.ID, placements[1].RankingItemID)
}

func TestReorderTierItemsInsertsAnItemDraggedInFromAnotherTier(t *testing.T) {
	t.Parallel()

	svc, ctx, _, version := newOwnedRanking(t)
	tiers, err := svc.Queries.ListRankingTiersForVersion(ctx, version.ID)
	require.NoError(t, err)

	item, err := svc.AddItem(ctx, services.AddItemRequest{VersionID: version.ID, Title: "Migrating"})
	require.NoError(t, err)
	_, err = svc.AddItemToTier(ctx, services.AddItemToTierRequest{VersionID: version.ID, TierID: tiers[0].ID, ItemID: item.ID})
	require.NoError(t, err)

	_, err = svc.ReorderTierItems(ctx, services.ReorderTierItemsRequest{
		VersionID: version.ID, TierID: tiers[1].ID, ItemIDs: []int64{item.ID},
	})
	require.NoError(t, err)

	oldTierPlacements, err := svc.Queries.ListRankingItemTiersForTier(ctx, tiers[0].ID)
	require.NoError(t, err)
	assert.Empty(t, oldTierPlacements, "the item leaves its old tier")

	newTierPlacements, err := svc.Queries.ListRankingItemTiersForTier(ctx, tiers[1].ID)
	require.NoError(t, err)
	require.Len(t, newTierPlacements, 1)
	assert.Equal(t, item.ID, newTierPlacements[0].RankingItemID)
}

func TestReorderTierItemsRejectsAnItemFromAnotherVersion(t *testing.T) {
	t.Parallel()

	svc, ctx, versionA, versionB := twoOwnedRankings(t)
	tiersA, err := svc.Queries.ListRankingTiersForVersion(ctx, versionA.ID)
	require.NoError(t, err)
	itemB, err := svc.AddItem(ctx, services.AddItemRequest{VersionID: versionB.ID, Title: "Elsewhere"})
	require.NoError(t, err)

	_, err = svc.ReorderTierItems(ctx, services.ReorderTierItemsRequest{
		VersionID: versionA.ID, TierID: tiersA[0].ID, ItemIDs: []int64{itemB.ID},
	})
	assert.ErrorIs(t, err, services.ErrInvalidTierPlacement)
}


func TestReorderTiersSetsTheGivenOrder(t *testing.T) {
	t.Parallel()

	svc, ctx, _, version := newOwnedRanking(t)
	tiers, err := svc.Queries.ListRankingTiersForVersion(ctx, version.ID)
	require.NoError(t, err)
	reversed := make([]int64, len(tiers))
	for i, tier := range tiers {
		reversed[len(tiers)-1-i] = tier.ID
	}

	err = svc.ReorderTiers(ctx, services.ReorderTiersRequest{VersionID: version.ID, TierIDs: reversed})
	require.NoError(t, err)

	reordered, err := svc.Queries.ListRankingTiersForVersion(ctx, version.ID)
	require.NoError(t, err)
	require.Len(t, reordered, len(tiers))
	for i, tier := range reordered {
		assert.Equal(t, reversed[i], tier.ID)
	}
}

func TestReorderTiersRejectsATierFromAnotherVersion(t *testing.T) {
	t.Parallel()

	svc, ctx, versionA, versionB := twoOwnedRankings(t)
	tiersB, err := svc.Queries.ListRankingTiersForVersion(ctx, versionB.ID)
	require.NoError(t, err)

	err = svc.ReorderTiers(ctx, services.ReorderTiersRequest{VersionID: versionA.ID, TierIDs: []int64{tiersB[0].ID}})
	assert.ErrorIs(t, err, services.ErrRankingNotFound)
}


func TestUnrankItemClearsItsTierPlacement(t *testing.T) {
	t.Parallel()

	svc, ctx, _, version := newOwnedRanking(t)
	tiers, err := svc.Queries.ListRankingTiersForVersion(ctx, version.ID)
	require.NoError(t, err)
	item, err := svc.AddItem(ctx, services.AddItemRequest{VersionID: version.ID, Title: "Placed"})
	require.NoError(t, err)
	_, err = svc.AddItemToTier(ctx, services.AddItemToTierRequest{VersionID: version.ID, TierID: tiers[0].ID, ItemID: item.ID})
	require.NoError(t, err)

	unranked, err := svc.UnrankItem(ctx, services.UnrankItemRequest{VersionID: version.ID, ItemID: item.ID})
	require.NoError(t, err)
	assert.Equal(t, item.ID, unranked.ID)

	placements, err := svc.Queries.ListRankingItemTiersForVersion(ctx, version.ID)
	require.NoError(t, err)
	assert.Empty(t, placements)
}

func TestUnrankItemFromAnotherVersionIsRejected(t *testing.T) {
	t.Parallel()

	svc, ctx, versionA, versionB := twoOwnedRankings(t)
	itemB, err := svc.AddItem(ctx, services.AddItemRequest{VersionID: versionB.ID, Title: "Elsewhere"})
	require.NoError(t, err)

	_, err = svc.UnrankItem(ctx, services.UnrankItemRequest{VersionID: versionA.ID, ItemID: itemB.ID})
	assert.ErrorIs(t, err, services.ErrInvalidTierPlacement)
}

func TestListUnrankedItemsExcludesPlacedItems(t *testing.T) {
	t.Parallel()

	svc, ctx, _, version := newOwnedRanking(t)
	tiers, err := svc.Queries.ListRankingTiersForVersion(ctx, version.ID)
	require.NoError(t, err)
	placed, err := svc.AddItem(ctx, services.AddItemRequest{VersionID: version.ID, Title: "Placed"})
	require.NoError(t, err)
	unplaced, err := svc.AddItem(ctx, services.AddItemRequest{VersionID: version.ID, Title: "Unplaced"})
	require.NoError(t, err)
	_, err = svc.AddItemToTier(ctx, services.AddItemToTierRequest{VersionID: version.ID, TierID: tiers[0].ID, ItemID: placed.ID})
	require.NoError(t, err)

	unranked, err := svc.ListUnrankedItems(ctx, version.ID)
	require.NoError(t, err)
	require.Len(t, unranked, 1)
	assert.Equal(t, unplaced.ID, unranked[0].ID)
}


func TestValidatePublishableBlocksWhenThereAreNoTiers(t *testing.T) {
	t.Parallel()

	svc, ctx, _, version := newOwnedRanking(t)
	tiers, err := svc.Queries.ListRankingTiersForVersion(ctx, version.ID)
	require.NoError(t, err)
	for _, tier := range tiers {
		require.NoError(t, svc.Queries.DeleteRankingTier(ctx, tier.ID))
	}

	validation, err := svc.ValidatePublishable(ctx, version.ID)
	require.NoError(t, err)

	assert.False(t, validation.Publishable)
	assert.Contains(t, validation.Reasons, "Add at least one tier.")
}

func TestValidatePublishableBlocksWhenThereAreNoItems(t *testing.T) {
	t.Parallel()

	svc, ctx, _, version := newOwnedRanking(t)

	validation, err := svc.ValidatePublishable(ctx, version.ID)
	require.NoError(t, err)

	assert.False(t, validation.Publishable)
	assert.Contains(t, validation.Reasons, "Add at least one item.")
}

func TestValidatePublishableBlocksWhenAnItemIsUnplaced(t *testing.T) {
	t.Parallel()

	svc, ctx, _, version := newOwnedRanking(t)
	tiers, err := svc.Queries.ListRankingTiersForVersion(ctx, version.ID)
	require.NoError(t, err)
	placed, err := svc.AddItem(ctx, services.AddItemRequest{VersionID: version.ID, Title: "Placed"})
	require.NoError(t, err)
	_, err = svc.AddItem(ctx, services.AddItemRequest{VersionID: version.ID, Title: "Unplaced"})
	require.NoError(t, err)
	_, err = svc.AddItemToTier(ctx, services.AddItemToTierRequest{VersionID: version.ID, TierID: tiers[0].ID, ItemID: placed.ID})
	require.NoError(t, err)

	validation, err := svc.ValidatePublishable(ctx, version.ID)
	require.NoError(t, err)

	assert.False(t, validation.Publishable)
	assert.Contains(t, validation.Reasons, "Place 1 more item into a tier.")
}

func TestValidatePublishablePassesWhenEveryItemIsPlaced(t *testing.T) {
	t.Parallel()

	svc, ctx, _, version := newOwnedRanking(t)
	tiers, err := svc.Queries.ListRankingTiersForVersion(ctx, version.ID)
	require.NoError(t, err)
	item, err := svc.AddItem(ctx, services.AddItemRequest{VersionID: version.ID, Title: "Placed"})
	require.NoError(t, err)
	_, err = svc.AddItemToTier(ctx, services.AddItemToTierRequest{VersionID: version.ID, TierID: tiers[0].ID, ItemID: item.ID})
	require.NoError(t, err)

	validation, err := svc.ValidatePublishable(ctx, version.ID)
	require.NoError(t, err)

	assert.True(t, validation.Publishable)
	assert.Empty(t, validation.Reasons)
}

func TestPublishVersionSucceedsWhenTheVersionIsPublishable(t *testing.T) {
	t.Parallel()

	svc, ctx, _, version := newOwnedRanking(t)
	tiers, err := svc.Queries.ListRankingTiersForVersion(ctx, version.ID)
	require.NoError(t, err)
	item, err := svc.AddItem(ctx, services.AddItemRequest{VersionID: version.ID, Title: "Placed"})
	require.NoError(t, err)
	_, err = svc.AddItemToTier(ctx, services.AddItemToTierRequest{VersionID: version.ID, TierID: tiers[0].ID, ItemID: item.ID})
	require.NoError(t, err)

	published, err := svc.PublishVersion(ctx, services.PublishVersionRequest{VersionID: version.ID})
	require.NoError(t, err)
	assert.True(t, published.PublishedAt.Valid)
}

func TestPublishVersionFailsWhenTheVersionIsNotPublishable(t *testing.T) {
	t.Parallel()

	svc, ctx, _, version := newOwnedRanking(t)

	_, err := svc.PublishVersion(ctx, services.PublishVersionRequest{VersionID: version.ID})
	assert.ErrorIs(t, err, services.ErrNotPublishable)
}


func TestCreateVersionFromPublishedCopiesTiersItemsAndPlacements(t *testing.T) {
	t.Parallel()

	svc, ctx, ranking, version := newOwnedRanking(t)
	tiers, err := svc.Queries.ListRankingTiersForVersion(ctx, version.ID)
	require.NoError(t, err)
	item, err := svc.AddItem(ctx, services.AddItemRequest{VersionID: version.ID, Title: "Carried over"})
	require.NoError(t, err)
	_, err = svc.AddItemToTier(ctx, services.AddItemToTierRequest{VersionID: version.ID, TierID: tiers[0].ID, ItemID: item.ID})
	require.NoError(t, err)
	published, err := svc.PublishVersion(ctx, services.PublishVersionRequest{VersionID: version.ID})
	require.NoError(t, err)

	draft, err := svc.CreateVersionFromPublished(ctx, services.CreateVersionFromPublishedRequest{
		RankingID: ranking.ID, SourceVersionID: published.ID,
	})
	require.NoError(t, err)
	assert.False(t, draft.PublishedAt.Valid)
	assert.NotEqual(t, published.ID, draft.ID)

	draftTiers, err := svc.Queries.ListRankingTiersForVersion(ctx, draft.ID)
	require.NoError(t, err)
	require.Len(t, draftTiers, len(tiers))
	assert.Equal(t, tiers[0].Title, draftTiers[0].Title)

	draftItems, err := svc.Queries.ListRankingItemsForVersion(ctx, draft.ID)
	require.NoError(t, err)
	require.Len(t, draftItems, 1)
	require.NotNil(t, draftItems[0].Title)
	assert.Equal(t, "Carried over", *draftItems[0].Title)

	draftPlacements, err := svc.Queries.ListRankingItemTiersForVersion(ctx, draft.ID)
	require.NoError(t, err)
	require.Len(t, draftPlacements, 1)
	assert.Equal(t, draftTiers[0].ID, draftPlacements[0].RankingTierID)
	assert.Equal(t, draftItems[0].ID, draftPlacements[0].RankingItemID)
}

func TestCreateVersionFromPublishedRejectsWhenADraftAlreadyExists(t *testing.T) {
	t.Parallel()

	svc, ctx, ranking, version := newOwnedRanking(t)

	_, err := svc.CreateVersionFromPublished(ctx, services.CreateVersionFromPublishedRequest{
		RankingID: ranking.ID, SourceVersionID: version.ID,
	})
	assert.ErrorIs(t, err, services.ErrDraftAlreadyExists, "the seeded draft itself is the conflicting one")
}


func publishedAt(t time.Time) db.RankingVersion {
	return db.RankingVersion{PublishedAt: pgtype.Timestamptz{Time: t, Valid: true}}
}

func TestFormatPublishedAtOmitsTheYearWithinTheLastYear(t *testing.T) {
	t.Parallel()

	recent := time.Now().AddDate(0, -1, 0)

	assert.Equal(t, recent.Format("Jan 2"), services.FormatPublishedAt(publishedAt(recent)))
}

func TestFormatPublishedAtIncludesTheYearBeyondTheLastYear(t *testing.T) {
	t.Parallel()

	old := time.Now().AddDate(-2, 0, 0)

	assert.Equal(t, old.Format("Jan 2, 2006"), services.FormatPublishedAt(publishedAt(old)))
}

func TestFormatPublishedAtIsEmptyForADraft(t *testing.T) {
	t.Parallel()

	assert.Empty(t, services.FormatPublishedAt(db.RankingVersion{}))
}
