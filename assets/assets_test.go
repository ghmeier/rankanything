package assets_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ghmeier/rankanything/assets"
	"github.com/ghmeier/rankanything/internal/render"
)

// The real templates must parse even when no database is available — this is
// the cheapest guard against a typo taking the whole app down at boot.
func TestTemplatesParse(t *testing.T) {
	_, err := render.New(assets.Templates())
	require.NoError(t, err)
}

func TestStaticContainsStylesheet(t *testing.T) {
	f, err := assets.Static().Open("css/app.css")
	require.NoError(t, err)
	assert.NoError(t, f.Close())
}
