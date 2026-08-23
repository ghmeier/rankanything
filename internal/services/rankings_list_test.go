package services_test

import (
	"context"
	"strings"
	"testing"

	db "github.com/ghmeier/rankanything/internal/db"
	"github.com/ghmeier/rankanything/internal/services"
	"github.com/ghmeier/rankanything/internal/testsupport"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestShortUUID returns an 8-character identifier satisfying
// ranking_versions' short_uuid length check, for tests that create a second
// version row directly rather than through CreateForUser.
func newTestShortUUID() string {
	return strings.ReplaceAll(uuid.NewString(), "-", "")[:8]
}

func TestListForUserReturnsEmptyWhenUserHasNoRankings(t *testing.T) {
	t.Parallel()

	env := testsupport.NewEnv(t)
	ctx := context.Background()
	svc := &services.RankingsService{Queries: env.Queries, Pool: env.Tx}

	user, err := env.Queries.CreateUser(ctx, db.CreateUserParams{
		Email: "lonely+" + uuid.NewString() + "@example.com", PasswordHash: "hash",
	})
	require.NoError(t, err)

	summaries, err := svc.ListForUser(ctx, services.ListForUserRequest{UserID: user.ID})

	require.NoError(t, err)
	assert.Empty(t, summaries)
}

func TestListForUserDescribesADraftOnlyRanking(t *testing.T) {
	t.Parallel()

	svc, ctx, ranking, draft := newOwnedRanking(t)

	summaries, err := svc.ListForUser(ctx, services.ListForUserRequest{UserID: ranking.UserID})

	require.NoError(t, err)
	require.Len(t, summaries, 1)
	assert.Equal(t, ranking.ID, summaries[0].Ranking.ID)
	assert.Nil(t, summaries[0].Published, "a freshly created ranking has never been published")
	require.NotNil(t, summaries[0].Draft)
	assert.Equal(t, draft.ID, summaries[0].Draft.ID)
}

func TestListForUserDescribesAPublishedRanking(t *testing.T) {
	t.Parallel()

	svc, ctx, ranking, draft := newOwnedRanking(t)
	published, err := svc.Queries.PublishRankingVersion(ctx, draft.ID)
	require.NoError(t, err)

	summaries, err := svc.ListForUser(ctx, services.ListForUserRequest{UserID: ranking.UserID})

	require.NoError(t, err)
	require.Len(t, summaries, 1)
	require.NotNil(t, summaries[0].Published)
	assert.Equal(t, published.ID, summaries[0].Published.ID)
	assert.Nil(t, summaries[0].Draft, "publishing the only version leaves no draft behind")
}

func TestListForUserDescribesAPublishedRankingWithANewerDraft(t *testing.T) {
	t.Parallel()

	svc, ctx, ranking, draft := newOwnedRanking(t)
	published, err := svc.Queries.PublishRankingVersion(ctx, draft.ID)
	require.NoError(t, err)

	newDraft, err := svc.Queries.CreateRankingVersion(ctx, db.CreateRankingVersionParams{
		ShortUuid: newTestShortUUID(),
		RankingID: ranking.ID,
	})
	require.NoError(t, err)

	summaries, err := svc.ListForUser(ctx, services.ListForUserRequest{UserID: ranking.UserID})

	require.NoError(t, err)
	require.Len(t, summaries, 1)
	require.NotNil(t, summaries[0].Published, "the last publish must still be reported")
	assert.Equal(t, published.ID, summaries[0].Published.ID)
	require.NotNil(t, summaries[0].Draft, "the newer draft must be reported alongside the publish")
	assert.Equal(t, newDraft.ID, summaries[0].Draft.ID)
}

func TestListForUserOnlyIncludesTheGivenUsersRankings(t *testing.T) {
	t.Parallel()

	env := testsupport.NewEnv(t)
	ctx := context.Background()
	svc := &services.RankingsService{Queries: env.Queries, Pool: env.Tx}

	userA, err := env.Queries.CreateUser(ctx, db.CreateUserParams{
		Email: "a+" + uuid.NewString() + "@example.com", PasswordHash: "hash",
	})
	require.NoError(t, err)
	userB, err := env.Queries.CreateUser(ctx, db.CreateUserParams{
		Email: "b+" + uuid.NewString() + "@example.com", PasswordHash: "hash",
	})
	require.NoError(t, err)

	rankingA, err := svc.CreateForUser(ctx, services.CreateForUserRequest{UserID: userA.ID})
	require.NoError(t, err)
	_, err = svc.CreateForUser(ctx, services.CreateForUserRequest{UserID: userB.ID})
	require.NoError(t, err)

	summaries, err := svc.ListForUser(ctx, services.ListForUserRequest{UserID: userA.ID})

	require.NoError(t, err)
	require.Len(t, summaries, 1)
	assert.Equal(t, rankingA.ID, summaries[0].Ranking.ID)
}
