package assets_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ghmeier/rankanything/assets"
)

func TestStaticContainsStylesheet(t *testing.T) {
	f, err := assets.Static().Open("css/app.css")
	require.NoError(t, err)
	assert.NoError(t, f.Close())
}
