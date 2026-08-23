package app_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ghmeier/rankanything/internal/db"
	"github.com/ghmeier/rankanything/internal/testsupport"
)

// registerClient signs up a fresh user without creating a ranking for
// them, unlike Env.NewOwnerClient — the fixture the empty-state test needs.
func registerClient(t *testing.T, env *testsupport.Env) *testsupport.Client {
	t.Helper()
	c := env.NewClient()

	email := "index+" + uuid.NewString() + "@example.com"
	res := c.Post("/register", url.Values{"email": {email}, "password": {"supersecret"}})
	require.Equal(t, http.StatusSeeOther, res.Status)

	return c
}

// newTestShortUUID returns an 8-character identifier satisfying
// ranking_versions' short_uuid length check, for tests that add a second
// version row directly rather than through CreateForUser.
func newTestShortUUID() string {
	return strings.ReplaceAll(uuid.NewString(), "-", "")[:8]
}

func TestRankingsIndexShowsEmptyStateWithNoRankings(t *testing.T) {
	env := testsupport.NewEnv(t)
	c := registerClient(t, env)

	res := c.Get("/me")

	require.Equal(t, http.StatusOK, res.Status)
	body := Body(res.Body)
	assert.Contains(t, body, "Your rankings")
	assert.Contains(t, body, "Create your first ranking")
}

func TestRankingsIndexShowsADraftOnlyRanking(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()

	res := owner.Get("/me")

	require.Equal(t, http.StatusOK, res.Status)
	body := Body(res.Body)
	assert.Contains(t, body, owner.Ranking.Name)
	assert.Contains(t, body, "draft")
	assert.NotContains(t, body, "published", "a ranking that's never been published must not claim to be")
}

func TestRankingsIndexShowsAPublishedRanking(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()

	_, err := env.Queries.PublishRankingVersion(context.Background(), owner.Draft.ID)
	require.NoError(t, err)

	res := owner.Get("/me")

	require.Equal(t, http.StatusOK, res.Status)
	body := Body(res.Body)
	assert.Contains(t, body, "published")
	assert.NotContains(t, body, "draft in progress", "a ranking with nothing but a stale draft row must not claim work is in progress")
}

func TestRankingsIndexShowsAPublishedRankingWithANewerDraftInProgress(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()
	ctx := context.Background()

	_, err := env.Queries.PublishRankingVersion(ctx, owner.Draft.ID)
	require.NoError(t, err)
	_, err = env.Queries.CreateRankingVersion(ctx, db.CreateRankingVersionParams{
		ShortUuid: newTestShortUUID(),
		RankingID: owner.Ranking.ID,
	})
	require.NoError(t, err)

	res := owner.Get("/me")

	require.Equal(t, http.StatusOK, res.Status)
	body := Body(res.Body)
	assert.Contains(t, body, "published", "the last publish must still be visible")
	assert.Contains(t, body, "draft in progress", "the newer draft on top of the publish must be visible too")
}

