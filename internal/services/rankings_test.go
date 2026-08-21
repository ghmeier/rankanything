package services_test

import (
	"context"
	"testing"

	"github.com/ghmeier/rankanything/internal/auth"
	db "github.com/ghmeier/rankanything/internal/db"
	"github.com/ghmeier/rankanything/internal/services"
	"github.com/ghmeier/rankanything/internal/testsupport"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// svcWithCtx builds a RankingsService and starts a session in the returned
// context so that session-based methods (EnsureDraft, authorization checks,
// etc.) work correctly.
func svcWithCtx(t *testing.T) (*services.RankingsService, context.Context) {
	t.Helper()

	env := testsupport.NewEnv(t)
	svc := &services.RankingsService{
		Queries: env.Queries,
		Pool:    env.Tx,
	}

	// Start an empty session so Drafts() / UserID() work without panicking.
	ctx := testsupport.SessionContext(t, env.App.Sessions.SessionManager)

	return svc, ctx
}

// createTestUser creates a user in the test database and returns the user id.
func createTestUser(t *testing.T, q *db.Queries) int64 {
	t.Helper()
	pw, err := auth.HashPassword("testpassword")
	require.NoError(t, err)
	u, err := q.CreateUser(context.Background(), db.CreateUserParams{
		Email:        "testuser+" + uuid.NewString() + "@example.com",
		PasswordHash: pw,
	})
	require.NoError(t, err)
	return u.ID
}

// ---------------------------------------------------------------------------
// EnsureDraft
// ---------------------------------------------------------------------------

func TestEnsureDraftCreatesAndSeedsTiers(t *testing.T) {
	t.Parallel()

	svc, ctx := svcWithCtx(t)

	ranking, err := svc.EnsureDraft(ctx, services.EnsureDraftRequest{})
	require.NoError(t, err)

	assert.Equal(t, "Untitled ranking", ranking.Title)
	assert.True(t, ranking.IsDraft)
	assert.NotNil(t, ranking.Slug)

	// Default S/A/B/C/D tiers should be seeded.
	tiers, err := svc.Queries.ListTiers(ctx, ranking.ID)
	require.NoError(t, err)
	assert.Len(t, tiers, len(services.DefaultTiers))

	for i, expected := range services.DefaultTiers {
		assert.Equal(t, expected.Label, tiers[i].Label)
	}

	// The draft should exist in the database with no owner.
	_, err = svc.Queries.GetRankingBySlug(ctx, ranking.Slug)
	require.NoError(t, err)
}

// ---------------------------------------------------------------------------
// CreateForUser
// ---------------------------------------------------------------------------

func TestCreateForUserCreatesOwnedRankingWithTiers(t *testing.T) {
	t.Parallel()

	env := testsupport.NewEnv(t)
	ctx := testsupport.SessionContext(t, env.App.Sessions.SessionManager)
	svc := &services.RankingsService{
		Queries: env.Queries,
		Pool:    env.Tx,
	}

	userID := createTestUser(t, env.Queries)
	ranking, err := svc.CreateForUser(ctx, services.CreateForUserRequest{UserID: userID})
	require.NoError(t, err)

	assert.Equal(t, "Untitled ranking", ranking.Title)
	assert.Equal(t, userID, *ranking.UserID)

	tiers, err := svc.Queries.ListTiers(ctx, ranking.ID)
	require.NoError(t, err)
	assert.Len(t, tiers, len(services.DefaultTiers))
}

// ---------------------------------------------------------------------------
// GetRankingForSlug (authenticated user owns ranking)
// ---------------------------------------------------------------------------

func TestGetRankingForSlugReturnsOwnedRanking(t *testing.T) {
	t.Parallel()

	env := testsupport.NewEnv(t)
	ctx := testsupport.SessionContext(t, env.App.Sessions.SessionManager)

	// Create the user so the foreign key constraint is satisfied.
	password, err := auth.HashPassword("testpassword")
	require.NoError(t, err)
	user, err := env.Queries.CreateUser(ctx, db.CreateUserParams{
		Email:        "test@example.com",
		PasswordHash: password,
	})
	require.NoError(t, err)

	err = env.App.Sessions.LogIn(ctx, user.ID)
	require.NoError(t, err)

	svc := &services.RankingsService{
		Queries: env.Queries,
		Pool:    env.Tx,
	}

	ranking, err := svc.CreateForUser(ctx, services.CreateForUserRequest{UserID: user.ID})
	require.NoError(t, err)

	found, err := svc.GetRankingForSlug(ctx, ranking.Slug)
	require.NoError(t, err)

	assert.Equal(t, ranking.ID, found.ID)
	assert.False(t, found.IsDraft)
}

// ---------------------------------------------------------------------------
// GetRankingForSlug (anonymous user with draft)
// ---------------------------------------------------------------------------

func TestGetRankingForSlugReturnsDraftForSession(t *testing.T) {
	t.Parallel()

	svc, ctx := svcWithCtx(t)

	ranking, err := svc.EnsureDraft(ctx, services.EnsureDraftRequest{})
	require.NoError(t, err)

	found, err := svc.GetRankingForSlug(ctx, ranking.Slug)
	require.NoError(t, err)

	assert.Equal(t, ranking.ID, found.ID)
	assert.True(t, found.IsDraft)
}

// ---------------------------------------------------------------------------
// UpdateRanking
// ---------------------------------------------------------------------------

func TestUpdateRanking(t *testing.T) {
	t.Parallel()

	svc, ctx := svcWithCtx(t)

	ranking, err := svc.EnsureDraft(ctx, services.EnsureDraftRequest{})
	require.NoError(t, err)

	updated, err := svc.UpdateRanking(ctx, services.UpdateRankingRequest{
		Slug:        ranking.Slug,
		Title:       "My Ranking",
		Description: "A test ranking",
	})
	require.NoError(t, err)

	assert.Equal(t, "My Ranking", updated.Title)
	assert.Equal(t, "A test ranking", updated.Description)
}

func TestUpdateRankingPartial(t *testing.T) {
	t.Parallel()

	svc, ctx := svcWithCtx(t)

	ranking, err := svc.EnsureDraft(ctx, services.EnsureDraftRequest{})
	require.NoError(t, err)

	updated, err := svc.UpdateRanking(ctx, services.UpdateRankingRequest{
		Slug:  ranking.Slug,
		Title: "Only the title changed",
	})
	require.NoError(t, err)

	assert.Equal(t, "Only the title changed", updated.Title)
	assert.Empty(t, updated.Description)
}

// ---------------------------------------------------------------------------
// AddItem
// ---------------------------------------------------------------------------

func TestAddItem(t *testing.T) {
	t.Parallel()

	svc, ctx := svcWithCtx(t)

	ranking, err := svc.EnsureDraft(ctx, services.EnsureDraftRequest{})
	require.NoError(t, err)

	item, err := svc.AddItem(ctx, services.AddItemRequest{
		Slug:     ranking.Slug,
		Label:    "Pretzels",
		ImageURL: "https://example.com/pretzel.jpg",
	})
	require.NoError(t, err)

	assert.Equal(t, "Pretzels", item.Label)
	assert.Equal(t, "https://example.com/pretzel.jpg", item.ImageUrl)

	// Verify item is in the ranking.
	rankingItems, err := svc.Queries.ListRankingItems(ctx, ranking.ID)
	require.NoError(t, err)
	assert.Len(t, rankingItems, 1)
	assert.Equal(t, item.ID, rankingItems[0].ID)
}

func TestAddMultipleItems(t *testing.T) {
	t.Parallel()

	svc, ctx := svcWithCtx(t)

	ranking, err := svc.EnsureDraft(ctx, services.EnsureDraftRequest{})
	require.NoError(t, err)

	_, err = svc.AddItem(ctx, services.AddItemRequest{Slug: ranking.Slug, Label: "First"})
	require.NoError(t, err)
	_, err = svc.AddItem(ctx, services.AddItemRequest{Slug: ranking.Slug, Label: "Second"})
	require.NoError(t, err)

	items, err := svc.Queries.ListRankingItems(ctx, ranking.ID)
	require.NoError(t, err)
	assert.Len(t, items, 2)
	assert.Equal(t, "First", items[0].Label)
	assert.Equal(t, "Second", items[1].Label)
}

// ---------------------------------------------------------------------------
// DeleteItem
// ---------------------------------------------------------------------------

func TestDeleteItem(t *testing.T) {
	t.Parallel()

	svc, ctx := svcWithCtx(t)

	ranking, err := svc.EnsureDraft(ctx, services.EnsureDraftRequest{})
	require.NoError(t, err)

	item, err := svc.AddItem(ctx, services.AddItemRequest{Slug: ranking.Slug, Label: "ToDelete"})
	require.NoError(t, err)

	err = svc.DeleteItem(ctx, services.DeleteItemRequest{Slug: ranking.Slug, ItemID: item.ID})
	require.NoError(t, err)

	remaining, err := svc.Queries.ListRankingItems(ctx, ranking.ID)
	require.NoError(t, err)
	assert.Empty(t, remaining)
}

// ---------------------------------------------------------------------------
// AddTier
// ---------------------------------------------------------------------------

func TestAddTier(t *testing.T) {
	t.Parallel()

	svc, ctx := svcWithCtx(t)

	ranking, err := svc.EnsureDraft(ctx, services.EnsureDraftRequest{})
	require.NoError(t, err)

	initialCount := len(services.DefaultTiers)

	tier, err := svc.AddTier(ctx, services.AddTierRequest{
		Slug:  ranking.Slug,
		Label: "F",
		Color: "#111111",
	})
	require.NoError(t, err)

	assert.Equal(t, "F", tier.Label)
	assert.Equal(t, "#111111", tier.Color)

	tiers, err := svc.Queries.ListTiers(ctx, ranking.ID)
	require.NoError(t, err)
	assert.Len(t, tiers, initialCount+1)
	assert.Equal(t, "F", tiers[len(tiers)-1].Label)
}

func TestAddTierDefaults(t *testing.T) {
	t.Parallel()

	svc, ctx := svcWithCtx(t)

	ranking, err := svc.EnsureDraft(ctx, services.EnsureDraftRequest{})
	require.NoError(t, err)

	tier, err := svc.AddTier(ctx, services.AddTierRequest{Slug: ranking.Slug})
	require.NoError(t, err)

	assert.Equal(t, "New tier", tier.Label)
	assert.Equal(t, "#94a3b8", tier.Color)
}

// ---------------------------------------------------------------------------
// UpdateTier
// ---------------------------------------------------------------------------

func TestUpdateTier(t *testing.T) {
	t.Parallel()

	svc, ctx := svcWithCtx(t)

	ranking, err := svc.EnsureDraft(ctx, services.EnsureDraftRequest{})
	require.NoError(t, err)

	tiers, err := svc.Queries.ListTiers(ctx, ranking.ID)
	require.NoError(t, err)

	allowMultiple := true
	updated, err := svc.UpdateTier(ctx, services.UpdateTierRequest{
		Slug:          ranking.Slug,
		TierID:        tiers[0].ID,
		Label:         "Top",
		Color:         "#ff0000",
		AllowMultiple: &allowMultiple,
	})
	require.NoError(t, err)

	assert.Equal(t, "Top", updated.Label)
	assert.Equal(t, "#ff0000", updated.Color)
	assert.True(t, updated.AllowMultiple)
}

func TestUpdateTierKeepsUnchangedFields(t *testing.T) {
	t.Parallel()

	svc, ctx := svcWithCtx(t)

	ranking, err := svc.EnsureDraft(ctx, services.EnsureDraftRequest{})
	require.NoError(t, err)

	tiers, err := svc.Queries.ListTiers(ctx, ranking.ID)
	require.NoError(t, err)

	updated, err := svc.UpdateTier(ctx, services.UpdateTierRequest{
		Slug:   ranking.Slug,
		TierID: tiers[0].ID,
		Color:  "#aabbcc",
	})
	require.NoError(t, err)

	// Label and AllowMultiple should be unchanged.
	assert.Equal(t, tiers[0].Label, updated.Label)
	assert.Equal(t, tiers[0].AllowMultiple, updated.AllowMultiple)
	assert.Equal(t, "#aabbcc", updated.Color)
}

// ---------------------------------------------------------------------------
// GetTier
// ---------------------------------------------------------------------------

func TestGetTier(t *testing.T) {
	t.Parallel()

	svc, ctx := svcWithCtx(t)

	ranking, err := svc.EnsureDraft(ctx, services.EnsureDraftRequest{})
	require.NoError(t, err)

	tiers, err := svc.Queries.ListTiers(ctx, ranking.ID)
	require.NoError(t, err)

	tier, items, err := svc.GetTier(ctx, services.GetTierRequest{Slug: ranking.Slug, TierID: tiers[0].ID})
	require.NoError(t, err)

	assert.Equal(t, tiers[0].ID, tier.ID)
	assert.Equal(t, tiers[0].Label, tier.Label)
	assert.Empty(t, items) // no items yet
}

func TestGetTierWithItems(t *testing.T) {
	t.Parallel()

	svc, ctx := svcWithCtx(t)

	ranking, err := svc.EnsureDraft(ctx, services.EnsureDraftRequest{})
	require.NoError(t, err)

	// Add two items.
	item1, err := svc.AddItem(ctx, services.AddItemRequest{Slug: ranking.Slug, Label: "Alpha"})
	require.NoError(t, err)
	item2, err := svc.AddItem(ctx, services.AddItemRequest{Slug: ranking.Slug, Label: "Beta"})
	require.NoError(t, err)

	// Place them in the first tier.
	tiers, err := svc.Queries.ListTiers(ctx, ranking.ID)
	require.NoError(t, err)

	_, err = svc.AddItemToTier(ctx, services.AddItemToTierRequest{
		Slug:   ranking.Slug,
		TierID: tiers[0].ID,
		ItemID: item1.ID,
	})
	require.NoError(t, err)

	_, err = svc.AddItemToTier(ctx, services.AddItemToTierRequest{
		Slug:   ranking.Slug,
		TierID: tiers[0].ID,
		ItemID: item2.ID,
	})
	require.NoError(t, err)

	_, items, err := svc.GetTier(ctx, services.GetTierRequest{Slug: ranking.Slug, TierID: tiers[0].ID})
	require.NoError(t, err)

	assert.Len(t, items, 2)
}

func TestDeleteTier(t *testing.T) {
	t.Parallel()

	svc, ctx := svcWithCtx(t)

	ranking, err := svc.EnsureDraft(ctx, services.EnsureDraftRequest{})
	require.NoError(t, err)

	initialTiers, err := svc.Queries.ListTiers(ctx, ranking.ID)
	require.NoError(t, err)
	assert.Len(t, initialTiers, len(services.DefaultTiers))

	// Add a custom tier then delete it.
	custom, err := svc.AddTier(ctx, services.AddTierRequest{Slug: ranking.Slug, Label: "Extra"})
	require.NoError(t, err)

	err = svc.DeleteTier(ctx, services.DeleteTierRequest{Slug: ranking.Slug, TierID: custom.ID})
	require.NoError(t, err)

	tiers, err := svc.Queries.ListTiers(ctx, ranking.ID)
	require.NoError(t, err)
	assert.Len(t, tiers, len(services.DefaultTiers))
}

func TestAddItemsRespectsAllowMultiple(t *testing.T) {
	t.Parallel()

	svc, ctx := svcWithCtx(t)

	ranking, err := svc.EnsureDraft(ctx, services.EnsureDraftRequest{})
	require.NoError(t, err)

	itemA, err := svc.AddItem(ctx, services.AddItemRequest{Slug: ranking.Slug, Label: "A"})
	require.NoError(t, err)
	itemB, err := svc.AddItem(ctx, services.AddItemRequest{Slug: ranking.Slug, Label: "B"})
	require.NoError(t, err)

	tiers, err := svc.Queries.ListTiers(ctx, ranking.ID)
	require.NoError(t, err)

	_, err = svc.AddItemToTier(ctx, services.AddItemToTierRequest{
		Slug:   ranking.Slug,
		TierID: tiers[1].ID,
		ItemID: itemA.ID,
	})
	require.NoError(t, err)
	_, err = svc.AddItemToTier(
		ctx, services.AddItemToTierRequest{
			Slug:   ranking.Slug,
			TierID: tiers[1].ID,
			ItemID: itemB.ID,
		})
	assert.Error(t, err)
}

func TestSaveDraftUnownedDraftRedirectsToRegister(t *testing.T) {
	t.Parallel()

	svc, ctx := svcWithCtx(t)

	ranking, err := svc.EnsureDraft(ctx, services.EnsureDraftRequest{})
	require.NoError(t, err)

	result, err := svc.SaveDraft(ctx, services.SaveDraftRequest{
		Slug:   ranking.Slug,
		UserID: 0, // anonymous
	})
	require.NoError(t, err)

	assert.False(t, result.IsOwned)
	assert.Equal(t, "/register?next=/r/"+ranking.Slug.String(), result.Redirect)
}

func TestSaveDraftOwnedRanking(t *testing.T) {
	t.Parallel()

	env := testsupport.NewEnv(t)
	ctx := testsupport.SessionContext(t, env.App.Sessions.SessionManager)
	svc := &services.RankingsService{
		Queries: env.Queries,
		Pool:    env.Tx,
	}

	userID := createTestUser(t, env.Queries)
	ranking, err := svc.CreateForUser(ctx, services.CreateForUserRequest{UserID: userID})
	require.NoError(t, err)

	result, err := svc.SaveDraft(ctx, services.SaveDraftRequest{
		Slug:   ranking.Slug,
		UserID: 1,
	})
	require.NoError(t, err)

	assert.True(t, result.IsOwned)
	assert.Equal(t, "/r/"+ranking.Slug.String(), result.Redirect)
	assert.Equal(t, "Ranking saved!", result.Message)
}

func TestSaveDraftDraftWithSignedInUser(t *testing.T) {
	t.Parallel()

	env := testsupport.NewEnv(t)
	ctx := testsupport.SessionContext(t, env.App.Sessions.SessionManager)
	svc := &services.RankingsService{
		Queries: env.Queries,
		Pool:    env.Tx,
	}

	userID := createTestUser(t, env.Queries)

	ranking, err := svc.EnsureDraft(ctx, services.EnsureDraftRequest{})
	require.NoError(t, err)

	// Even though it's a draft, a signed-in user gets a success result.
	result, err := svc.SaveDraft(ctx, services.SaveDraftRequest{
		Slug:   ranking.Slug,
		UserID: userID,
	})
	require.NoError(t, err)

	assert.True(t, result.IsOwned)
	assert.Equal(t, "/r/"+ranking.Slug.String(), result.Redirect)
}

func TestGetRankingWithItems(t *testing.T) {
	t.Parallel()

	svc, ctx := svcWithCtx(t)

	ranking, err := svc.EnsureDraft(ctx, services.EnsureDraftRequest{})
	require.NoError(t, err)

	data, err := svc.GetRankingWithItems(ctx, ranking.Slug)
	require.NoError(t, err)

	assert.Equal(t, ranking.ID, data.Ranking.ID)
	assert.True(t, data.IsDraft)
	assert.NotEmpty(t, data.Tiers)
	assert.Len(t, data.Tiers, len(services.DefaultTiers))
}

func TestGetRankingWithItemsAndPlacements(t *testing.T) {
	t.Parallel()

	svc, ctx := svcWithCtx(t)

	ranking, err := svc.EnsureDraft(ctx, services.EnsureDraftRequest{})
	require.NoError(t, err)

	item, err := svc.AddItem(ctx, services.AddItemRequest{Slug: ranking.Slug, Label: "Rank me"})
	require.NoError(t, err)

	tiers, err := svc.Queries.ListTiers(ctx, ranking.ID)
	require.NoError(t, err)

	_, err = svc.AddItemToTier(ctx, services.AddItemToTierRequest{
		Slug:   ranking.Slug,
		TierID: tiers[0].ID,
		ItemID: item.ID,
	})
	require.NoError(t, err)

	data, err := svc.GetRankingWithItems(ctx, ranking.Slug)
	require.NoError(t, err)

	assert.Len(t, data.Items, 1)
	assert.Equal(t, "Rank me", data.Items[0].Label)
	assert.Len(t, data.Placements, 1)
	assert.Equal(t, item.ID, data.Placements[0].RankedItemID)
}

func TestGetRankingForSlugNotFound(t *testing.T) {
	t.Parallel()

	svc, ctx := svcWithCtx(t)

	_, err := svc.GetRankingForSlug(ctx, uuid.New())
	assert.ErrorIs(t, err, services.ErrRankingNotFound)
}

func TestUpdateRankingNotFound(t *testing.T) {
	t.Parallel()

	svc, ctx := svcWithCtx(t)

	_, err := svc.UpdateRanking(ctx, services.UpdateRankingRequest{
		Slug:  uuid.New(),
		Title: "Nope",
	})
	assert.Error(t, err)
}
