package app_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ghmeier/rankanything/internal/testsupport"
)

func TestComponentsGalleryRendersEveryVariant(t *testing.T) {
	env := testsupport.NewEnv(t)
	c := env.NewClient()

	res := c.Get("/components")
	assert.Equal(t, http.StatusOK, res.Status)

	body := Body(res.Body)

	// Button variants.
	assert.Contains(t, body, "bg-primary", "primary button variant")
	assert.Contains(t, body, "bg-danger", "destructive button variant")
	assert.Contains(t, body, "Tertiary")
	assert.Contains(t, body, `disabled class="`, "the disabled button variant renders the disabled attribute")

	// IconButton.
	assert.Contains(t, body, `aria-label="Edit"`)
	assert.Contains(t, body, `aria-label="Delete"`)

	// Input, including the error state.
	assert.Contains(t, body, `name="email"`)
	assert.Contains(t, body, "Password must be at least 8 characters")

	// Notice variants.
	assert.Contains(t, body, "Ranking saved.")
	assert.Contains(t, body, "Something went wrong. Try again.")

	// NavBar, both signed-out and signed-in states.
	assert.Contains(t, body, "Create account")
	assert.Contains(t, body, "Your rankings")
	assert.Contains(t, body, "Sign out")

	// The theme toggle.
	assert.Contains(t, body, "rankanythingToggleTheme")
	assert.Contains(t, body, "Toggle theme")
}

func TestComponentsGalleryNotRegisteredInProduction(t *testing.T) {
	t.Setenv("APP_ENV", "production")

	env := testsupport.NewEnv(t)
	res := env.NewClient().Get("/components")

	assert.Equal(t, http.StatusNotFound, res.Status)
}
