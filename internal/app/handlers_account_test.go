package app_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ghmeier/rankanything/internal/testsupport"
)

func TestAccountPageRequiresSignIn(t *testing.T) {
	env := testsupport.NewEnv(t)

	res := env.NewClient().Get("/account")

	require.Equal(t, http.StatusSeeOther, res.Status)
	assert.Equal(t, "/login?next=/account", res.Location())
}

func TestAccountPageDefaultsToSystemTheme(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()

	res := owner.Get("/account")

	require.Equal(t, http.StatusOK, res.Status)
	assert.Contains(t, Body(res.Body), `data-theme="system"`)
}

func TestUpdateThemeRendersTheRequestedThemeOnTheNextLoad(t *testing.T) {
	env := testsupport.NewEnv(t)

	for _, theme := range []string{"system", "light", "dark"} {
		t.Run(theme, func(t *testing.T) {
			owner := env.NewOwnerClient()

			update := owner.HTMX(http.MethodPost, "/account/theme", url.Values{"theme": {theme}})
			require.Equal(t, http.StatusOK, update.Status)
			assert.Contains(t, Body(update.Body), `checked`, "the fragment must mark the selected option checked")

			res := owner.Get("/account")
			require.Equal(t, http.StatusOK, res.Status)
			assert.Contains(t, Body(res.Body), `data-theme="`+theme+`"`)
		})
	}
}

func TestUpdateThemePersistsAcrossPagesUntilChangedAgain(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()

	res := owner.HTMX(http.MethodPost, "/account/theme", url.Values{"theme": {"dark"}})
	require.Equal(t, http.StatusOK, res.Status)

	me := owner.Get("/me")
	require.Equal(t, http.StatusOK, me.Status)
	assert.Contains(t, Body(me.Body), `data-theme="dark"`, "a page other than /account must also carry the stored preference")
}

func TestUpdateThemeRejectsAnUnknownValue(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()

	res := owner.HTMX(http.MethodPost, "/account/theme", url.Values{"theme": {"sepia"}})

	assert.Equal(t, http.StatusBadRequest, res.Status)
}

func TestSignedOutVisitorGetsNoThemeAttribute(t *testing.T) {
	env := testsupport.NewEnv(t)

	res := env.NewClient().Get("/login")

	require.Equal(t, http.StatusOK, res.Status)
	assert.Contains(t, Body(res.Body), `data-theme=""`, "a signed-out visitor keeps the prefers-color-scheme behavior, not an explicit theme")
}
