package app_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ghmeier/rankanything/internal/services"
	"github.com/ghmeier/rankanything/internal/testsupport"
)

// Body wraps an HTTP response body so that testify assertion errors show
// a truncated version instead of the full HTML.
type Body string

const truncLen = 500

func (b Body) String() string {
	s := string(b)
	if len(s) <= truncLen {
		return s
	}
	return s[:truncLen] + fmt.Sprintf("... (%d more bytes)", len(s)-truncLen)
}

func TestCSRFIsEnforced(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()

	res := owner.FormWithBogusCSRF(http.MethodPost, "/r/"+owner.Ranking.Uuid.String()+"/items", url.Values{"label": {"Pretzels"}})
	assert.Equal(t, http.StatusForbidden, res.Status)
}

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

func TestRegisterValidation(t *testing.T) {
	env := testsupport.NewEnv(t)

	t.Run("rejects a bad email", func(t *testing.T) {
		res := env.NewClient().Post("/register", url.Values{"email": {"nope"}, "password": {"supersecret"}})
		assert.Equal(t, http.StatusUnprocessableEntity, res.Status)
		assert.Contains(t, Body(res.Body), "Enter a valid email address.")
	})

	t.Run("rejects a short password", func(t *testing.T) {
		res := env.NewClient().Post("/register", url.Values{"email": {"ada@example.com"}, "password": {"short"}})
		assert.Equal(t, http.StatusUnprocessableEntity, res.Status)
		assert.Contains(t, Body(res.Body), "at least 8 characters")
	})

	t.Run("a signed-up user reaches their account page", func(t *testing.T) {
		c := env.NewClient()
		res := c.Post("/register", url.Values{"email": {"fresh@example.com"}, "password": {"supersecret"}})
		require.Equal(t, http.StatusSeeOther, res.Status)
		assert.Equal(t, "/me", res.Location())
	})

	t.Run("next carries the signed-up user onward when it's site-relative", func(t *testing.T) {
		c := env.NewClient()
		res := c.Post("/register", url.Values{"email": {"next@example.com"}, "password": {"supersecret"}, "next": {"/new"}})
		require.Equal(t, http.StatusSeeOther, res.Status)
		assert.Equal(t, "/new", res.Location())
	})

	t.Run("an off-site next is refused", func(t *testing.T) {
		c := env.NewClient()
		res := c.Post("/register", url.Values{
			"email": {"openredirect@example.com"}, "password": {"supersecret"}, "next": {"https://evil.example.com"},
		})
		require.Equal(t, http.StatusSeeOther, res.Status)
		assert.Equal(t, "/me", res.Location())
	})

	// A real unique-constraint violation aborts the shared test transaction
	// until rollback, so this runs last — any subtest after it that expects
	// a clean database would see Postgres's "current transaction is aborted"
	// instead of the behavior it's testing.
	t.Run("rejects a duplicate email", func(t *testing.T) {
		first := env.NewClient()
		require.Equal(t, http.StatusSeeOther,
			first.Post("/register", url.Values{"email": {"dup@example.com"}, "password": {"supersecret"}}).Status)

		res := env.NewClient().Post("/register", url.Values{"email": {"DUP@example.com"}, "password": {"supersecret"}})
		assert.Equal(t, http.StatusUnprocessableEntity, res.Status)
		assert.Contains(t, Body(res.Body), "already registered")
	})
}

func TestLogin(t *testing.T) {
	env := testsupport.NewEnv(t)
	require.Equal(t, http.StatusSeeOther, env.NewClient().Post("/register",
		url.Values{"email": {"ada@example.com"}, "password": {"supersecret"}}).Status)

	t.Run("wrong password is rejected", func(t *testing.T) {
		res := env.NewClient().Post("/login", url.Values{"email": {"ada@example.com"}, "password": {"nope12345"}})
		assert.Equal(t, http.StatusUnauthorized, res.Status)
		assert.Contains(t, Body(res.Body), "Email or password is incorrect.")
	})

	t.Run("unknown email gives the same message", func(t *testing.T) {
		res := env.NewClient().Post("/login", url.Values{"email": {"ghost@example.com"}, "password": {"supersecret"}})
		assert.Equal(t, http.StatusUnauthorized, res.Status)
		assert.Contains(t, Body(res.Body), "Email or password is incorrect.")
	})

	t.Run("valid credentials sign in and reach the account page", func(t *testing.T) {
		c := env.NewClient()
		res := c.Post("/login", url.Values{"email": {"Ada@Example.com"}, "password": {"supersecret"}})
		require.Equal(t, http.StatusSeeOther, res.Status)

		me := c.Get("/me")
		assert.Equal(t, http.StatusOK, me.Status)
		assert.Contains(t, Body(me.Body), "Your rankings")
	})

	t.Run("open redirects are refused", func(t *testing.T) {
		c := env.NewClient()
		res := c.Post("/login", url.Values{
			"email": {"ada@example.com"}, "password": {"supersecret"}, "next": {"https://evil.example.com"},
		})
		require.Equal(t, http.StatusSeeOther, res.Status)
		assert.True(t, strings.HasPrefix(res.Location(), "/"), "redirect stayed on site: %s", res.Location())
	})

	t.Run("a leading backslash is refused too", func(t *testing.T) {
		c := env.NewClient()
		res := c.Post("/login", url.Values{
			"email": {"ada@example.com"}, "password": {"supersecret"}, "next": {"/\\evil.example.com"},
		})
		require.Equal(t, http.StatusSeeOther, res.Status)
		assert.Equal(t, "/", res.Location())
	})
}

func TestSignedOutUsersCannotReachAccount(t *testing.T) {
	env := testsupport.NewEnv(t)

	res := env.NewClient().Get("/me")
	require.Equal(t, http.StatusSeeOther, res.Status)
	assert.Contains(t, res.Location(), "/login")
}

func TestNewRankingRequiresSignIn(t *testing.T) {
	env := testsupport.NewEnv(t)

	res := env.NewClient().Get("/new")
	require.Equal(t, http.StatusSeeOther, res.Status)
	assert.Equal(t, "/login?next=/new", res.Location())
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

func TestLogoutClearsTheSession(t *testing.T) {
	env := testsupport.NewEnv(t)
	c := env.NewClient()
	require.Equal(t, http.StatusSeeOther, c.Post("/register",
		url.Values{"email": {"ada@example.com"}, "password": {"supersecret"}}).Status)

	require.Equal(t, http.StatusSeeOther, c.Post("/logout", nil).Status)

	res := c.Get("/me")
	assert.Equal(t, http.StatusSeeOther, res.Status)
	assert.Contains(t, res.Location(), "/login")
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
