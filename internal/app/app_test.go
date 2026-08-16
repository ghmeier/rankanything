package app_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ghmeier/rankanything/internal/app"
	"github.com/ghmeier/rankanything/internal/db"
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

// startDraft walks the anonymous entry point: GET / creates a draft and
// redirects into its builder.
func startDraft(t *testing.T, c *testsupport.Client) uuid.UUID {
	t.Helper()
	res := c.Get("/")
	require.Equal(t, http.StatusSeeOther, res.Status)
	slug := res.Slug()
	require.NotEmpty(t, slug)
	return slug
}

func TestHomeCreatesAnonymousDraft(t *testing.T) {
	env := testsupport.NewEnv(t)
	c := env.NewClient()

	slug := startDraft(t, c)

	page := c.Get("/r/" + slug.String())
	assert.Equal(t, http.StatusOK, page.Status)
	assert.Contains(t, Body(page.Body), "Untitled ranking")
	assert.Contains(t, Body(page.Body), "Unsaved draft")
	for _, tier := range app.DefaultTiers {
		assert.Contains(t, Body(page.Body), ">"+tier.Label+"<", "default tier %s should render", tier.Label)
	}
}

func TestHomeResumesExistingDraft(t *testing.T) {
	env := testsupport.NewEnv(t)
	c := env.NewClient()

	first := startDraft(t, c)
	second := c.Get("/")

	assert.Equal(t, "/r/"+first.String(), second.Location(), "the same session resumes its draft")
}

func TestDraftsArePrivateToTheirSession(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewClient()
	stranger := env.NewClient()

	slug := startDraft(t, owner)

	res := stranger.Get("/r/" + slug.String())
	assert.Equal(t, http.StatusNotFound, res.Status, "another visitor must not read the draft")
}

func TestCSRFIsEnforced(t *testing.T) {
	env := testsupport.NewEnv(t)
	c := env.NewClient()
	slug := startDraft(t, c)

	res := c.Post("/r/"+slug.String()+"/items", url.Values{"label": {"Pretzels"}, "csrf_token": {"bogus"}})
	assert.Equal(t, http.StatusForbidden, res.Status)
}

func TestAddItemReturnsBoardFragment(t *testing.T) {
	env := testsupport.NewEnv(t)
	c := env.NewClient()
	slug := startDraft(t, c)

	res := c.HTMX(http.MethodPost, "/r/"+slug.String()+"/items", url.Values{"label": {"Pretzels"}})

	require.Equal(t, http.StatusOK, res.Status)
	assert.Contains(t, Body(res.Body), "Pretzels")
	assert.NotContains(t, Body(res.Body), "<html", "htmx swaps must not include the layout")
}

func TestAddItemRequiresLabel(t *testing.T) {
	env := testsupport.NewEnv(t)
	c := env.NewClient()
	slug := startDraft(t, c)

	res := c.HTMX(http.MethodPost, "/r/"+slug.String()+"/items", url.Values{"label": {"   "}})

	assert.Equal(t, http.StatusUnprocessableEntity, res.Status)
	assert.Contains(t, Body(res.Body), "Give the item a name.")
}

func TestPlacementFlow(t *testing.T) {
	env := testsupport.NewEnv(t)
	c := env.NewClient()
	ctx := context.Background()
	slug := startDraft(t, c)

	c.HTMX(http.MethodPost, "/r/"+slug.String()+"/items", url.Values{"label": {"Pretzels"}})
	c.HTMX(http.MethodPost, "/r/"+slug.String()+"/items", url.Values{"label": {"Olives"}})

	ranking, err := env.Queries.GetRankingBySlug(ctx, slug)
	require.NoError(t, err)
	tiers, err := env.Queries.ListTiers(ctx, ranking.ID)
	require.NoError(t, err)
	items, err := env.Queries.ListRankingItems(ctx, ranking.ID)
	require.NoError(t, err)
	require.Len(t, items, 2)

	form := url.Values{"tier_id": {strconv.FormatInt(tiers[0].ID, 10)}}
	form.Add("item_id", strconv.FormatInt(items[1].ID, 10))
	form.Add("item_id", strconv.FormatInt(items[0].ID, 10))

	res := c.HTMX(http.MethodPut, "/r/"+slug.String()+"/placements", form)
	require.Equal(t, http.StatusOK, res.Status)

	placements, err := env.Queries.ListPlacements(ctx, ranking.ID)
	require.NoError(t, err)
	require.Len(t, placements, 2)
	assert.Equal(t, items[1].ID, placements[0].RankedItemID)

	t.Run("single-item tiers reject a second drop", func(t *testing.T) {
		_, err := env.Queries.UpdateTier(ctx, db.UpdateTierParams{ID: tiers[1].ID, Label: tiers[1].Label, Color: tiers[1].Color, Position: tiers[1].Position, AllowMultiple: false})
		require.NoError(t, err)

		full := url.Values{"tier_id": {strconv.FormatInt(tiers[1].ID, 10)}}
		full.Add("item_id", strconv.FormatInt(items[0].ID, 10))
		full.Add("item_id", strconv.FormatInt(items[1].ID, 10))

		res := c.HTMX(http.MethodPut, "/r/"+slug.String()+"/placements", full)
		assert.Equal(t, http.StatusUnprocessableEntity, res.Status)
		assert.Contains(t, Body(res.Body), "holds a single item")
	})

	t.Run("dropping into the tray unplaces items", func(t *testing.T) {
		tray := url.Values{"tier_id": {"0"}}
		tray.Add("item_id", strconv.FormatInt(items[0].ID, 10))
		tray.Add("item_id", strconv.FormatInt(items[1].ID, 10))

		res := c.HTMX(http.MethodPut, "/r/"+slug.String()+"/placements", tray)
		require.Equal(t, http.StatusOK, res.Status)

		placements, err := env.Queries.ListPlacements(ctx, ranking.ID)
		require.NoError(t, err)
		assert.Empty(t, placements)
	})
}

func TestCustomTierLifecycle(t *testing.T) {
	env := testsupport.NewEnv(t)
	c := env.NewClient()
	ctx := context.Background()
	slug := startDraft(t, c)

	res := c.HTMX(http.MethodPost, "/r/"+slug.String()+"/tiers", url.Values{"label": {"F"}, "color": {"#111111"}})
	require.Equal(t, http.StatusOK, res.Status)
	assert.Contains(t, Body(res.Body), `value="F"`)

	ranking, err := env.Queries.GetRankingBySlug(ctx, slug)
	require.NoError(t, err)
	tiers, err := env.Queries.ListTiers(ctx, ranking.ID)
	require.NoError(t, err)
	require.Len(t, tiers, len(app.DefaultTiers)+1)

	last := tiers[len(tiers)-1]
	assert.Equal(t, "F", last.Label)
	assert.Equal(t, "#111111", last.Color)

	renamed := c.HTMX(http.MethodPut, "/r/"+slug.String()+"/tiers/"+strconv.FormatInt(last.ID, 10),
		url.Values{"label": {"Trash"}, "color": {"#222222"}})
	require.Equal(t, http.StatusOK, renamed.Status)
	assert.Contains(t, Body(renamed.Body), `value="Trash"`)

	deleted := c.HTMX(http.MethodDelete, "/r/"+slug.String()+"/tiers/"+strconv.FormatInt(last.ID, 10), nil)
	require.Equal(t, http.StatusOK, deleted.Status)
	assert.NotContains(t, Body(deleted.Body), `value="Trash"`)
}

func TestSaveRequiresAccountThenClaimsDraft(t *testing.T) {
	env := testsupport.NewEnv(t)
	c := env.NewClient()
	ctx := context.Background()
	slug := startDraft(t, c)

	save := c.Post("/r/"+slug.String()+"/save", nil)
	require.Equal(t, http.StatusSeeOther, save.Status)
	assert.Equal(t, "/register?next=/r/"+slug.String(), save.Location())

	ranking, err := env.Queries.GetRankingBySlug(ctx, slug)
	require.NoError(t, err)
	assert.True(t, ranking.UserID == nil, "the draft stays unclaimed until sign-up")

	reg := c.Post("/register", url.Values{
		"email":    {"ada@example.com"},
		"password": {"supersecret"},
		"next":     {"/r/" + slug.String()},
	})
	require.Equal(t, http.StatusSeeOther, reg.Status)
	assert.Equal(t, "/r/"+slug.String(), reg.Location())

	claimed, err := env.Queries.GetRankingBySlug(ctx, slug)
	require.NoError(t, err)
	require.NotNil(t, claimed.UserID, "signing up claims the draft")

	page := c.Get("/r/" + slug.String())
	assert.Equal(t, http.StatusOK, page.Status)
	assert.Contains(t, Body(page.Body), "Saved")

	me := c.Get("/me")
	assert.Equal(t, http.StatusOK, me.Status)
	assert.Contains(t, Body(me.Body), "Untitled ranking")
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
}

func TestSignedOutUsersCannotReachAccount(t *testing.T) {
	env := testsupport.NewEnv(t)

	res := env.NewClient().Get("/me")
	require.Equal(t, http.StatusSeeOther, res.Status)
	assert.Contains(t, res.Location(), "/login")
}

func TestOwnedRankingsAreNotReadableByOthers(t *testing.T) {
	env := testsupport.NewEnv(t)

	owner := env.NewClient()
	slug := startDraft(t, owner)
	require.Equal(t, http.StatusSeeOther, owner.Post("/register",
		url.Values{"email": {"owner@example.com"}, "password": {"supersecret"}}).Status)

	stranger := env.NewClient()
	require.Equal(t, http.StatusSeeOther, stranger.Post("/register",
		url.Values{"email": {"stranger@example.com"}, "password": {"supersecret"}}).Status)

	res := stranger.Get("/r/" + slug.String())
	assert.Equal(t, http.StatusNotFound, res.Status)

	write := stranger.Post("/r/"+slug.String(), url.Values{"title": {"Hijacked"}})
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
	c := env.NewClient()
	slug := startDraft(t, c)

	res := c.HTMX(http.MethodPost, "/r/"+slug.String(), url.Values{"title": {"Best snacks"}, "description": {"2026 edition"}})

	require.Equal(t, http.StatusOK, res.Status)
	assert.Contains(t, Body(res.Body), `value="Best snacks"`)
	assert.Contains(t, Body(res.Body), `value="2026 edition"`)
	assert.NotContains(t, Body(res.Body), "<html")
}
