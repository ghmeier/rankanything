package app_test

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/ghmeier/rankanything/internal/db"
	"github.com/ghmeier/rankanything/internal/testsupport"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

// extractShareURL pulls the public share link out of the share modal's readonly
// input field. The enable-share handler returns ShareModalBody which contains
// an <input ... value="http://..."> with the public URL.
func extractShareURL(t *testing.T, body string) string {
	t.Helper()
	const marker = `readonly`
	idx := strings.Index(body, marker)
	require.NotEqual(t, -1, idx, "no readonly URL input in body")

	before := body[:idx]
	valueIdx := strings.LastIndex(before, `value="`)
	require.NotEqual(t, -1, valueIdx, "no value attribute before readonly input")
	start := valueIdx + len(`value="`)
	end := strings.Index(body[start:], `"`)
	require.NotEqual(t, -1, end, "unterminated value attribute")
	return body[start : start+end]
}

func TestShareControlBlockedWhenNothingIsPublished(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()
	verifyOwnerEmail(t, env, owner)

	res := owner.Get("/r/" + owner.Ranking.Uuid.String())

	require.Equal(t, http.StatusOK, res.Status)
	body := Body(res.Body)
	assert.Contains(t, body, "Publish at least one version.")
	assert.NotContains(t, body, "Verify your email.")
}

func TestShareControlBlockedWhenTheOwnerEmailIsUnverified(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()
	publishOwnerDraft(t, env, owner)

	res := owner.Get("/r/" + owner.Ranking.Uuid.String())

	require.Equal(t, http.StatusOK, res.Status)
	body := Body(res.Body)
	assert.Contains(t, body, "Verify your email.")
	assert.NotContains(t, body, "Publish at least one version.")
}

func TestShareControlOfferedWhenBothConditionsHold(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()
	publishOwnerDraft(t, env, owner)
	verifyOwnerEmail(t, env, owner)

	res := owner.Get("/r/" + owner.Ranking.Uuid.String())

	require.Equal(t, http.StatusOK, res.Status)
	body := Body(res.Body)
	assert.Contains(t, body, `hx-get="/r/`+owner.Ranking.Uuid.String()+`/share"`, "the Share button opens the modal")
	assert.NotContains(t, body, "To share this ranking:", "the blocked-reason tooltip only renders on an inert control")
}

func TestEnablingShareMintsASlugAndThePublicURLResolves(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()
	publishOwnerDraft(t, env, owner)
	verifyOwnerEmail(t, env, owner)

	res := owner.Post("/r/"+owner.Ranking.Uuid.String()+"/share/link", nil)
	require.Equal(t, http.StatusOK, res.Status)
	shareURL := extractShareURL(t, res.Body)
	require.Contains(t, shareURL, "/s/")

	path := shareURL[strings.Index(shareURL, "/s/"):]
	stranger := env.NewClient()
	page := stranger.Get(path)

	assert.Equal(t, http.StatusOK, page.Status)
	assert.Contains(t, Body(page.Body), owner.Ranking.Name)
}

func TestASharedBoardsMetaDescriptionIsPlainText(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()
	publishOwnerDraft(t, env, owner)
	verifyOwnerEmail(t, env, owner)

	owner.HTMX(http.MethodPost, "/r/"+owner.Ranking.Uuid.String()+"/description",
		url.Values{"description": {"Ranked **by hand**"}})

	res := owner.Post("/r/"+owner.Ranking.Uuid.String()+"/share/link", nil)
	require.Equal(t, http.StatusOK, res.Status)
	shareURL := extractShareURL(t, res.Body)

	page := env.NewClient().Get(shareURL[strings.Index(shareURL, "/s/"):])

	require.Equal(t, http.StatusOK, page.Status)
	body := Body(page.Body)
	assert.Contains(t, body, `content="Ranked by hand"`, "the social preview reads as words, not markdown")
	assert.Contains(t, body, "<strong>by hand</strong>", "the page itself still renders the markdown")
}

func TestDisablingShareKillsTheOldSlugPermanentlyAndResharingMintsANewOne(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()
	publishOwnerDraft(t, env, owner)
	verifyOwnerEmail(t, env, owner)

	enable := owner.Post("/r/"+owner.Ranking.Uuid.String()+"/share/link", nil)
	require.Equal(t, http.StatusOK, enable.Status)
	firstURL := extractShareURL(t, enable.Body)
	firstPath := firstURL[strings.Index(firstURL, "/s/"):]

	stranger := env.NewClient()
	require.Equal(t, http.StatusOK, stranger.Get(firstPath).Status, "the link works while it's live")

	disable := owner.Delete("/r/"+owner.Ranking.Uuid.String()+"/share/link", nil)
	require.Equal(t, http.StatusOK, disable.Status)
	disableBody := Body(disable.Body)
	assert.NotContains(t, disableBody, "Copy link", "stops offering the dead link")
	assert.Contains(t, disableBody, `hx-post="/r/`+owner.Ranking.Uuid.String()+`/share/link"`, "the toggle offers to re-enable")

	assert.Equal(t, http.StatusNotFound, env.NewClient().Get(firstPath).Status, "the old link must be dead")

	reenable := owner.Post("/r/"+owner.Ranking.Uuid.String()+"/share/link", nil)
	require.Equal(t, http.StatusOK, reenable.Status)
	secondURL := extractShareURL(t, reenable.Body)
	secondPath := secondURL[strings.Index(secondURL, "/s/"):]

	assert.NotEqual(t, firstPath, secondPath, "re-sharing mints a different slug")
	assert.Equal(t, http.StatusOK, env.NewClient().Get(secondPath).Status)
	assert.Equal(t, http.StatusNotFound, env.NewClient().Get(firstPath).Status, "the old link stays dead even after a fresh share exists")
}

func TestEnablingShareIsRejectedWhenTheRankingIsNotShareable(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()

	res := owner.Post("/r/"+owner.Ranking.Uuid.String()+"/share/link", nil)

	assert.Equal(t, http.StatusForbidden, res.Status, "a direct request can't bypass the publish and verification checks")
}

func sharePublicPath(t *testing.T, env *testsupport.Env, owner *testsupport.OwnerClient) string {
	t.Helper()
	res := owner.Post("/r/"+owner.Ranking.Uuid.String()+"/share/link", nil)
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
	// EnableLinkShare doesn't itself enforce ShareValidation (only the handler
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

func makeShareable(t *testing.T, env *testsupport.Env, owner *testsupport.OwnerClient) {
	t.Helper()
	publishOwnerDraft(t, env, owner)
	verifyOwnerEmail(t, env, owner)
}

func extractInviteToken(t *testing.T, env *testsupport.Env) string {
	t.Helper()
	msgs := env.EmailSink.Sent()
	require.NotEmpty(t, msgs, "expected an invite email")
	last := msgs[len(msgs)-1]
	u, err := url.Parse(extractInviteLink(t, last.Text))
	require.NoError(t, err)
	tok := u.Query().Get("token")
	require.NotEmpty(t, tok, "no token query param in invite link")
	return tok
}

func extractInviteLink(t *testing.T, text string) string {
	t.Helper()
	const marker = "Accept invitation: "
	idx := strings.Index(text, marker)
	require.NotEqual(t, -1, idx, "no invite link in email text")
	start := idx + len(marker)
	end := strings.Index(text[start:], "\n")
	if end == -1 {
		return strings.TrimSpace(text[start:])
	}
	return strings.TrimSpace(text[start : start+end])
}

func inviteUser(t *testing.T, env *testsupport.Env, owner *testsupport.OwnerClient, email string, role db.RankingShareRole) {
	t.Helper()
	res := owner.Post("/r/"+owner.Ranking.Uuid.String()+"/share/invites", url.Values{
		"email": {email},
		"role":  {string(role)},
	})
	require.Equal(t, http.StatusOK, res.Status)
}

func TestInviteByEmailSendsAnEmailAndListsTheShare(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()
	makeShareable(t, env, owner)

	invited := "friend+" + uuid.NewString() + "@example.com"
	inviteUser(t, env, owner, invited, db.RankingShareRoleREADER)

	msgs := env.EmailSink.Sent()
	var found bool
	for _, m := range msgs {
		if m.To == invited && strings.Contains(m.Subject, "shared a ranking") {
			found = true
			break
		}
	}
	assert.True(t, found, "an invite email should have been sent to the invited address")

	modal := owner.HTMX(http.MethodGet, "/r/"+owner.Ranking.Uuid.String()+"/share", nil)
	require.Equal(t, http.StatusOK, modal.Status)
	assert.Contains(t, modal.Body, invited, "the share modal should list the invited email")
}

func TestAcceptInviteGrantsAccessToRanking(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()
	makeShareable(t, env, owner)

	invitedEmail := "invited+" + uuid.NewString() + "@example.com"
	inviteUser(t, env, owner, invitedEmail, db.RankingShareRoleEDITOR)
	tok := extractInviteToken(t, env)

	invitee := registerClient(t, env)
	res := invitee.Get("/invite/" + tok)
	require.Equal(t, http.StatusSeeOther, res.Status)
	assert.Contains(t, res.Location(), "/r/"+owner.Ranking.Uuid.String())

	board := invitee.Get("/r/" + owner.Ranking.Uuid.String())
	assert.Equal(t, http.StatusOK, board.Status, "the invited user can now view the ranking")
}

func TestAcceptInviteTwiceReturnsAlreadyRedeemed(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()
	makeShareable(t, env, owner)

	invitedEmail := "double+" + uuid.NewString() + "@example.com"
	inviteUser(t, env, owner, invitedEmail, db.RankingShareRoleREADER)
	tok := extractInviteToken(t, env)

	first := registerClient(t, env)
	res := first.Get("/invite/" + tok)
	require.Equal(t, http.StatusSeeOther, res.Status)

	second := registerClient(t, env)
	res = second.Get("/invite/" + tok)
	require.Equal(t, http.StatusSeeOther, res.Status)
	assert.Equal(t, "/me", res.Location(), "a redeemed invite redirects to /me")
}

func TestRevokeShareRemovesAccess(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()
	makeShareable(t, env, owner)

	invitedEmail := "revoke+" + uuid.NewString() + "@example.com"
	inviteUser(t, env, owner, invitedEmail, db.RankingShareRoleREADER)
	tok := extractInviteToken(t, env)

	invitee := registerClient(t, env)
	invitee.Get("/invite/" + tok)

	shares, err := env.Queries.ListEmailSharesForRanking(context.Background(), owner.Ranking.ID)
	require.NoError(t, err)
	require.NotEmpty(t, shares)

	res := owner.Delete("/r/"+owner.Ranking.Uuid.String()+"/share/invites/"+strconv.FormatInt(shares[0].ID, 10), nil)
	require.Equal(t, http.StatusOK, res.Status)

	board := invitee.Get("/r/" + owner.Ranking.Uuid.String())
	assert.Equal(t, http.StatusNotFound, board.Status, "access revoked after share deletion")
}

func TestEditorCannotManageShares(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()
	makeShareable(t, env, owner)

	editorEmail := "editor+" + uuid.NewString() + "@example.com"
	inviteUser(t, env, owner, editorEmail, db.RankingShareRoleEDITOR)
	tok := extractInviteToken(t, env)

	editor := registerClient(t, env)
	editor.Get("/invite/" + tok)

	slug := owner.Ranking.Uuid.String()

	shareModal := editor.HTMX(http.MethodGet, "/r/"+slug+"/share", nil)
	assert.Equal(t, http.StatusForbidden, shareModal.Status, "editor cannot open the share modal")

	enableLink := editor.Post("/r/"+slug+"/share/link", nil)
	assert.Equal(t, http.StatusForbidden, enableLink.Status, "editor cannot enable sharing")

	invite := editor.Post("/r/"+slug+"/share/invites", url.Values{"email": {"another@example.com"}, "role": {"READER"}})
	assert.Equal(t, http.StatusForbidden, invite.Status, "editor cannot invite users")
}

func TestReaderCannotEditBoard(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()
	makeShareable(t, env, owner)

	readerEmail := "reader+" + uuid.NewString() + "@example.com"
	inviteUser(t, env, owner, readerEmail, db.RankingShareRoleREADER)
	tok := extractInviteToken(t, env)

	reader := registerClient(t, env)
	reader.Get("/invite/" + tok)

	slug := owner.Ranking.Uuid.String()
	short := owner.Draft.ShortUuid

	addItem := reader.HTMX(http.MethodPost, "/r/"+slug+"/v/"+short+"/items", url.Values{"label": {"Sneaky"}})
	assert.Equal(t, http.StatusForbidden, addItem.Status, "reader cannot add items")

	addTier := reader.HTMX(http.MethodPost, "/r/"+slug+"/v/"+short+"/tiers", url.Values{"label": {"Sneaky"}, "color": {"#ff0000"}})
	assert.Equal(t, http.StatusForbidden, addTier.Status, "reader cannot add tiers")
}

func TestReaderCanViewRanking(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()
	makeShareable(t, env, owner)

	readerEmail := "viewer+" + uuid.NewString() + "@example.com"
	inviteUser(t, env, owner, readerEmail, db.RankingShareRoleREADER)
	tok := extractInviteToken(t, env)

	reader := registerClient(t, env)
	reader.Get("/invite/" + tok)

	res := reader.Get("/r/" + owner.Ranking.Uuid.String())
	require.Equal(t, http.StatusOK, res.Status)
	assert.Contains(t, Body(res.Body), owner.Ranking.Name)
}
