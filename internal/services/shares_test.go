package services_test

import (
	"strings"
	"testing"

	"github.com/ghmeier/rankanything/internal/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newShareService builds a ShareService sharing rankingSvc's Queries (and
// so its transaction), the way newOwnedRanking's caller already has one on
// hand from setting up the ranking itself.
func newShareService(rankingSvc *services.RankingsService) *services.ShareService {
	return &services.ShareService{Queries: rankingSvc.Queries, BaseURL: "https://test.rankanything.app"}
}

// ---------------------------------------------------------------------------
// EvaluateShareGate
// ---------------------------------------------------------------------------

func TestEvaluateShareGateBlocksWhenNothingIsPublished(t *testing.T) {
	t.Parallel()

	rankingSvc, ctx, ranking, _ := newOwnedRanking(t)
	shareSvc := newShareService(rankingSvc)
	_, err := rankingSvc.Queries.MarkUserEmailVerified(ctx, ranking.UserID)
	require.NoError(t, err)

	gate, err := shareSvc.EvaluateShareGate(ctx, ranking)

	require.NoError(t, err)
	assert.False(t, gate.Shareable)
	assert.Contains(t, gate.Reasons, "Publish at least one version.")
}

func TestEvaluateShareGateBlocksWhenTheOwnerEmailIsUnverified(t *testing.T) {
	t.Parallel()

	rankingSvc, ctx, ranking, version := newOwnedRanking(t)
	shareSvc := newShareService(rankingSvc)
	_, err := rankingSvc.Queries.PublishRankingVersion(ctx, version.ID)
	require.NoError(t, err)

	gate, err := shareSvc.EvaluateShareGate(ctx, ranking)

	require.NoError(t, err)
	assert.False(t, gate.Shareable)
	assert.Contains(t, gate.Reasons, "Verify your email.")
}

func TestEvaluateShareGateAllowsWhenBothConditionsHold(t *testing.T) {
	t.Parallel()

	rankingSvc, ctx, ranking, version := newOwnedRanking(t)
	shareSvc := newShareService(rankingSvc)
	_, err := rankingSvc.Queries.PublishRankingVersion(ctx, version.ID)
	require.NoError(t, err)
	_, err = rankingSvc.Queries.MarkUserEmailVerified(ctx, ranking.UserID)
	require.NoError(t, err)

	gate, err := shareSvc.EvaluateShareGate(ctx, ranking)

	require.NoError(t, err)
	assert.True(t, gate.Shareable)
	assert.Empty(t, gate.Reasons)
}

// ---------------------------------------------------------------------------
// Enabling and disabling the link share
// ---------------------------------------------------------------------------

func TestEnableLinkShareMintsAResolvableSlug(t *testing.T) {
	t.Parallel()

	rankingSvc, ctx, ranking, _ := newOwnedRanking(t)
	shareSvc := newShareService(rankingSvc)

	link, err := shareSvc.EnableLinkShare(ctx, ranking.ID)

	require.NoError(t, err)
	assert.True(t, link.IsPublic)
	assert.Contains(t, link.URL, "https://test.rankanything.app/s/")

	got, err := shareSvc.GetLinkShare(ctx, ranking.ID)
	require.NoError(t, err)
	assert.Equal(t, link, got)
}

func TestGetLinkShareReportsUnsharedForARankingWithNoShareRow(t *testing.T) {
	t.Parallel()

	rankingSvc, ctx, ranking, _ := newOwnedRanking(t)
	shareSvc := newShareService(rankingSvc)

	link, err := shareSvc.GetLinkShare(ctx, ranking.ID)

	require.NoError(t, err)
	assert.False(t, link.IsPublic)
	assert.Empty(t, link.URL)
}

func TestDisableLinkShareKillsTheOldSlugPermanentlyAndResharingMintsANewOne(t *testing.T) {
	t.Parallel()

	rankingSvc, ctx, ranking, version := newOwnedRanking(t)
	shareSvc := newShareService(rankingSvc)
	_, err := rankingSvc.Queries.PublishRankingVersion(ctx, version.ID)
	require.NoError(t, err)

	first, err := shareSvc.EnableLinkShare(ctx, ranking.ID)
	require.NoError(t, err)
	firstSlug := strings.TrimPrefix(first.URL, shareSvc.BaseURL+"/s/")

	require.NoError(t, shareSvc.DisableLinkShare(ctx, ranking.ID))

	link, err := shareSvc.GetLinkShare(ctx, ranking.ID)
	require.NoError(t, err)
	assert.False(t, link.IsPublic, "revoking clears is_public")

	_, err = shareSvc.ResolvePublicRanking(ctx, firstSlug)
	assert.ErrorIs(t, err, services.ErrShareNotPublic, "the revoked slug must never resolve again")

	second, err := shareSvc.EnableLinkShare(ctx, ranking.ID)
	require.NoError(t, err)
	assert.NotEqual(t, first.URL, second.URL, "re-sharing mints a different slug, not the old one")

	_, err = shareSvc.ResolvePublicRanking(ctx, firstSlug)
	assert.ErrorIs(t, err, services.ErrShareNotPublic, "the old slug stays dead even after a fresh share exists")
}

// ---------------------------------------------------------------------------
// ResolvePublicRanking
// ---------------------------------------------------------------------------

func TestResolvePublicRankingFailsForAnUnknownSlug(t *testing.T) {
	t.Parallel()

	rankingSvc, ctx, _, _ := newOwnedRanking(t)
	shareSvc := newShareService(rankingSvc)

	_, err := shareSvc.ResolvePublicRanking(ctx, "does-not-exist")

	assert.ErrorIs(t, err, services.ErrShareNotPublic)
}

func TestResolvePublicRankingFailsWhenTheRankingHasNothingPublished(t *testing.T) {
	t.Parallel()

	rankingSvc, ctx, ranking, _ := newOwnedRanking(t)
	shareSvc := newShareService(rankingSvc)

	link, err := shareSvc.EnableLinkShare(ctx, ranking.ID)
	require.NoError(t, err)
	slug := strings.TrimPrefix(link.URL, shareSvc.BaseURL+"/s/")

	_, err = shareSvc.ResolvePublicRanking(ctx, slug)

	assert.ErrorIs(t, err, services.ErrShareNotPublic, "a live slug with nothing published behind it still 404s")
}

func TestResolvePublicRankingReturnsTheLivePublishedVersion(t *testing.T) {
	t.Parallel()

	rankingSvc, ctx, ranking, version := newOwnedRanking(t)
	shareSvc := newShareService(rankingSvc)
	published, err := rankingSvc.Queries.PublishRankingVersion(ctx, version.ID)
	require.NoError(t, err)

	link, err := shareSvc.EnableLinkShare(ctx, ranking.ID)
	require.NoError(t, err)
	slug := strings.TrimPrefix(link.URL, shareSvc.BaseURL+"/s/")

	public, err := shareSvc.ResolvePublicRanking(ctx, slug)

	require.NoError(t, err)
	assert.Equal(t, ranking.ID, public.Ranking.ID)
	assert.Equal(t, published.ID, public.Version.ID)
}
