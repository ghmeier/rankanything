package app_test

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ghmeier/rankanything/internal/testsupport"
)

// publishOwnerDraft adds one item, places it in the first tier, and
// publishes the owner's draft — the minimum a version needs to pass
// RankingsService's publish gate. Mirrors
// TestPublishSucceedsAndRedirectsToThePublishedVersion in
// handlers_board_test.go.
func publishOwnerDraft(t *testing.T, env *testsupport.Env, owner *testsupport.OwnerClient) {
	t.Helper()
	ctx := context.Background()
	slug := owner.Ranking.Uuid.String()

	owner.HTMX(http.MethodPost, "/r/"+slug+"/v/"+owner.Draft.ShortUuid+"/items", url.Values{"label": {"Ready"}})
	tiers, err := env.Queries.ListRankingTiersForVersion(ctx, owner.Draft.ID)
	require.NoError(t, err)
	items, err := env.Queries.ListRankingItemsForVersion(ctx, owner.Draft.ID)
	require.NoError(t, err)
	owner.HTMX(http.MethodPost, "/r/"+slug+"/v/"+owner.Draft.ShortUuid+"/tiers/"+strconv.FormatInt(tiers[0].ID, 10)+"/items",
		url.Values{"item_id": {strconv.FormatInt(items[0].ID, 10)}})

	res := owner.Post("/r/"+slug+"/v/"+owner.Draft.ShortUuid+"/publish", nil)
	require.Equal(t, http.StatusOK, res.Status)
}

// verifyOwnerEmail marks the owner's email verified directly, bypassing the
// token redemption flow — a fixture, not what's under test.
func verifyOwnerEmail(t *testing.T, env *testsupport.Env, owner *testsupport.OwnerClient) {
	t.Helper()
	_, err := env.Queries.MarkUserEmailVerified(context.Background(), owner.Ranking.UserID)
	require.NoError(t, err)
}

// extractShareURL pulls the public share link out of the share control's
// readonly input, the way extractCSRF pulls the CSRF token out of
// hx-headers.
func extractShareURL(t *testing.T, body string) string {
	t.Helper()
	const marker = `id="share-link-input"`
	idx := strings.Index(body, marker)
	require.NotEqual(t, -1, idx, "share link input not found in body")
	rest := body[idx:]
	const valueMarker = `value="`
	vi := strings.Index(rest, valueMarker)
	require.NotEqual(t, -1, vi, "share link input has no value attribute")
	rest = rest[vi+len(valueMarker):]
	end := strings.Index(rest, `"`)
	require.NotEqual(t, -1, end, "unterminated value attribute")
	return rest[:end]
}

// ---------------------------------------------------------------------------
// The share control's gate
// ---------------------------------------------------------------------------

func TestShareControlBlockedWhenNothingIsPublished(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()
	verifyOwnerEmail(t, env, owner)

	res := owner.Get("/r/" + owner.Ranking.Uuid.String())

	require.Equal(t, http.StatusOK, res.Status)
	body := Body(res.Body)
	assert.Contains(t, body, "Publish a version before sharing a link.")
	assert.NotContains(t, body, "Verify your email before sharing a link.")
	assert.NotContains(t, body, "Create share link")
}

func TestShareControlBlockedWhenTheOwnerEmailIsUnverified(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()
	publishOwnerDraft(t, env, owner)

	res := owner.Get("/r/" + owner.Ranking.Uuid.String())

	require.Equal(t, http.StatusOK, res.Status)
	body := Body(res.Body)
	assert.Contains(t, body, "Verify your email before sharing a link.")
	assert.NotContains(t, body, "Publish a version before sharing a link.")
	assert.NotContains(t, body, "Create share link")
}

func TestShareControlOfferedWhenBothConditionsHold(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()
	publishOwnerDraft(t, env, owner)
	verifyOwnerEmail(t, env, owner)

	res := owner.Get("/r/" + owner.Ranking.Uuid.String())

	require.Equal(t, http.StatusOK, res.Status)
	body := Body(res.Body)
	assert.Contains(t, body, "Create share link")
	assert.NotContains(t, body, "Sharing isn't available yet")
}

// ---------------------------------------------------------------------------
// Toggling the link share
// ---------------------------------------------------------------------------

func TestEnablingShareMintsASlugAndThePublicURLResolves(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()
	publishOwnerDraft(t, env, owner)
	verifyOwnerEmail(t, env, owner)

	res := owner.Post("/r/"+owner.Ranking.Uuid.String()+"/share", nil)
	require.Equal(t, http.StatusOK, res.Status)
	shareURL := extractShareURL(t, res.Body)
	require.Contains(t, shareURL, "/s/")

	path := shareURL[strings.Index(shareURL, "/s/"):]
	stranger := env.NewClient()
	page := stranger.Get(path)

	assert.Equal(t, http.StatusOK, page.Status)
	assert.Contains(t, Body(page.Body), owner.Ranking.Name)
}

func TestDisablingShareKillsTheOldSlugPermanentlyAndResharingMintsANewOne(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()
	publishOwnerDraft(t, env, owner)
	verifyOwnerEmail(t, env, owner)

	enable := owner.Post("/r/"+owner.Ranking.Uuid.String()+"/share", nil)
	require.Equal(t, http.StatusOK, enable.Status)
	firstURL := extractShareURL(t, enable.Body)
	firstPath := firstURL[strings.Index(firstURL, "/s/"):]

	stranger := env.NewClient()
	require.Equal(t, http.StatusOK, stranger.Get(firstPath).Status, "the link works while it's live")

	disable := owner.Delete("/r/"+owner.Ranking.Uuid.String()+"/share", nil)
	require.Equal(t, http.StatusOK, disable.Status)
	assert.Contains(t, Body(disable.Body), "Create share link", "the control offers to share again, not the live link view")

	assert.Equal(t, http.StatusNotFound, env.NewClient().Get(firstPath).Status, "the old link must be dead")

	reenable := owner.Post("/r/"+owner.Ranking.Uuid.String()+"/share", nil)
	require.Equal(t, http.StatusOK, reenable.Status)
	secondURL := extractShareURL(t, reenable.Body)
	secondPath := secondURL[strings.Index(secondURL, "/s/"):]

	assert.NotEqual(t, firstPath, secondPath, "re-sharing mints a different slug")
	assert.Equal(t, http.StatusOK, env.NewClient().Get(secondPath).Status)
	assert.Equal(t, http.StatusNotFound, env.NewClient().Get(firstPath).Status, "the old link stays dead even after a fresh share exists")
}

func TestEnablingShareIsRejectedWhenTheGateFails(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()

	res := owner.Post("/r/"+owner.Ranking.Uuid.String()+"/share", nil)

	assert.Equal(t, http.StatusForbidden, res.Status, "a direct request can't bypass the publish/verification gate")
}

// ---------------------------------------------------------------------------
// The public route
// ---------------------------------------------------------------------------

func sharePublicPath(t *testing.T, env *testsupport.Env, owner *testsupport.OwnerClient) string {
	t.Helper()
	res := owner.Post("/r/"+owner.Ranking.Uuid.String()+"/share", nil)
	require.Equal(t, http.StatusOK, res.Status)
	shareURL := extractShareURL(t, res.Body)
	return shareURL[strings.Index(shareURL, "/s/"):]
}

func TestPublicPageCarriesNoEditAffordances(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()
	publishOwnerDraft(t, env, owner)
	verifyOwnerEmail(t, env, owner)
	path := sharePublicPath(t, env, owner)

	res := env.NewClient().Get(path)

	require.Equal(t, http.StatusOK, res.Status)
	body := Body(res.Body)
	assert.NotContains(t, body, "hx-delete", "no delete affordance on any item or tier")
	assert.NotContains(t, body, "Add an item")
	assert.NotContains(t, body, "Add tier")
	assert.NotContains(t, body, "Edit tiers")
	assert.NotContains(t, body, "board-version-actions")
	assert.NotContains(t, body, "Versions")
	assert.NotContains(t, body, "share-control")
	assert.NotContains(t, body, "board.js")
}

func TestPublicSlugThatDoesNotResolveIs404(t *testing.T) {
	env := testsupport.NewEnv(t)

	res := env.NewClient().Get("/s/does-not-exist")

	assert.Equal(t, http.StatusNotFound, res.Status)
}

func TestPublicSlugForARankingWithNoPublishedVersionIs404(t *testing.T) {
	// EnableLinkShare doesn't itself enforce ShareGate (only the handler
	// does), so a share row can in principle exist with is_public true but
	// nothing published behind it; the public route still has to 404 it.
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()

	_, err := env.App.ShareSvc.EnableLinkShare(context.Background(), owner.Ranking.ID)
	require.NoError(t, err)
	link, err := env.App.ShareSvc.GetLinkShare(context.Background(), owner.Ranking.ID)
	require.NoError(t, err)
	path := link.URL[strings.Index(link.URL, "/s/"):]

	res := env.NewClient().Get(path)

	assert.Equal(t, http.StatusNotFound, res.Status)
}

func TestStrangerCanReadThePublicPageButNotTheOwnerRoute(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()
	publishOwnerDraft(t, env, owner)
	verifyOwnerEmail(t, env, owner)
	path := sharePublicPath(t, env, owner)

	signedOut := env.NewClient()
	assert.Equal(t, http.StatusOK, signedOut.Get(path).Status, "a signed-out stranger can read the public link")
	assert.Equal(t, http.StatusNotFound, signedOut.Get("/r/"+owner.Ranking.Uuid.String()).Status, "a signed-out stranger can't reach the owner route")

	otherUser := registerClient(t, env)
	assert.Equal(t, http.StatusOK, otherUser.Get(path).Status, "a different signed-in user can read the public link")
	assert.Equal(t, http.StatusNotFound, otherUser.Get("/r/"+owner.Ranking.Uuid.String()).Status, "a different signed-in user can't reach the owner route")
}

func TestPublishingANewVersionChangesWhatTheLivePublicLinkShows(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()
	publishOwnerDraft(t, env, owner)
	verifyOwnerEmail(t, env, owner)
	path := sharePublicPath(t, env, owner)

	before := env.NewClient().Get(path)
	require.Equal(t, http.StatusOK, before.Status)
	assert.NotContains(t, Body(before.Body), "Added after republish")

	branch := owner.Post("/r/"+owner.Ranking.Uuid.String()+"/versions", nil)
	require.Equal(t, http.StatusOK, branch.Status)
	versions, err := env.Queries.ListRankingVersionsForRanking(context.Background(), owner.Ranking.ID)
	require.NoError(t, err)
	var draft = owner.Draft
	for _, v := range versions {
		if !v.PublishedAt.Valid {
			draft = v
			break
		}
	}
	require.NotEqual(t, owner.Draft.ID, draft.ID, "branching must have created a new draft")

	slug := owner.Ranking.Uuid.String()
	owner.HTMX(http.MethodPost, "/r/"+slug+"/v/"+draft.ShortUuid+"/items", url.Values{"label": {"Added after republish"}})
	tiers, err := env.Queries.ListRankingTiersForVersion(context.Background(), draft.ID)
	require.NoError(t, err)
	items, err := env.Queries.ListRankingItemsForVersion(context.Background(), draft.ID)
	require.NoError(t, err)
	newItem := items[len(items)-1]
	owner.HTMX(http.MethodPost, "/r/"+slug+"/v/"+draft.ShortUuid+"/tiers/"+strconv.FormatInt(tiers[0].ID, 10)+"/items",
		url.Values{"item_id": {strconv.FormatInt(newItem.ID, 10)}})

	res := owner.Post("/r/"+slug+"/v/"+draft.ShortUuid+"/publish", nil)
	require.Equal(t, http.StatusOK, res.Status)

	// The whole test runs inside one database transaction, and Postgres'
	// now() is frozen for its duration — both publishes above landed the
	// same published_at, so ResolveLiveRankingVersion's ORDER BY published_at
	// DESC has nothing to break the tie with. Nudging the second one forward
	// is test-only plumbing to get a deterministic "most recent", standing
	// in for the millisecond or more that separates two publishes for real.
	_, err = env.Tx.Exec(context.Background(), "UPDATE ranking_versions SET published_at = published_at + interval '1 second' WHERE id = $1", draft.ID)
	require.NoError(t, err)

	after := env.NewClient().Get(path)
	require.Equal(t, http.StatusOK, after.Status)
	assert.Contains(t, Body(after.Body), "Added after republish", "the same link now reflects the newly published version")
}
