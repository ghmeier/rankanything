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

	// Tooltip, both placements.
	assert.Contains(t, body, "Sits above its trigger.")
	assert.Contains(t, body, "Sits beside its trigger.")
	assert.Contains(t, body, "Sits below its trigger.")

	// Dropdown, both trigger shapes, plus the current and disabled items.
	assert.Contains(t, body, "Version 2 — published")
	assert.Contains(t, body, `aria-current="true"`, "the current version renders as inert text")
	assert.Contains(t, body, `aria-label="More actions"`, "the icon trigger names itself")
	assert.Contains(t, body, "group-open/dropdown:rotate-180", "the labelled trigger's chevron flips while open")

	// SplitButton: the two halves square off the corners they share.
	assert.Contains(t, body, "rounded-r-none", "the action half")
	assert.Contains(t, body, "rounded-l-none", "the menu half")
	assert.Contains(t, body, `aria-label="More publish actions"`)

	assert.Contains(t, body, `aria-label="Rankings"`)
	assert.Contains(t, body, `aria-label="Sign out"`)

	assert.Contains(t, body, `href="/login"`)
	assert.Contains(t, body, `href="/register"`)
	assert.Contains(t, body, "Create account")

	// The theme toggle.
	assert.Contains(t, body, "rankanythingToggleTheme")
	assert.Contains(t, body, "Toggle theme")
}

func TestComponentsGalleryNotRegisteredInProduction(t *testing.T) {
	t.Setenv("APP_ENV", "production")

	env := testsupport.NewEnv(t)
	res := env.NewClient().Get("/components")

	assert.Equal(t, http.StatusSeeOther, res.Status)
}
