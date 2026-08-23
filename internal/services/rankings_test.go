package services_test

import (
	"context"
	"testing"

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

// ---------------------------------------------------------------------------
// CreateForUser
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// GetRanking
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// ResolveVersion
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// UpdateRanking
// ---------------------------------------------------------------------------

func TestUpdateRankingChangesTitleAndDescription(t *testing.T) {
	t.Parallel()

	svc, ctx, ranking, _ := newOwnedRanking(t)

	updated, err := svc.UpdateRanking(ctx, services.UpdateRankingRequest{
		UUID:        ranking.Uuid,
		Name:        "My Ranking",
		Description: "A test ranking",
	})
	require.NoError(t, err)

	assert.Equal(t, "My Ranking", updated.Name)
	assert.Equal(t, "A test ranking", updated.Description)
}

func TestUpdateRankingUnknownUUIDNotFound(t *testing.T) {
	t.Parallel()

	svc, ctx, _, _ := newOwnedRanking(t)

	_, err := svc.UpdateRanking(ctx, services.UpdateRankingRequest{UUID: uuid.New(), Name: "Nope"})
	assert.ErrorIs(t, err, services.ErrRankingNotFound)
}

// ---------------------------------------------------------------------------
// AddItem / DeleteItem
// ---------------------------------------------------------------------------

func TestAddItemAddsToTheGivenVersion(t *testing.T) {
	t.Parallel()

	svc, ctx, _, version := newOwnedRanking(t)

	item, err := svc.AddItem(ctx, services.AddItemRequest{
		VersionID:      version.ID,
		Title:          "Pretzels",
		ImageSourceURL: "https://example.com/pretzel.jpg",
	})
	require.NoError(t, err)

	assert.Equal(t, "Pretzels", item.Title)
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

// ---------------------------------------------------------------------------
// AddTier / UpdateTier / GetTier / DeleteTier
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// AddItemToTier
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// GetBoard
// ---------------------------------------------------------------------------

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
	assert.Equal(t, "Rank me", board.Items[0].Title)
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
