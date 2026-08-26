package app_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ghmeier/rankanything/internal/testsupport"
)

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

	res := owner.FormWithBogusCSRF(http.MethodPost, "/r/"+owner.Ranking.Uuid.String()+"/v/"+owner.Draft.ShortUuid+"/items", url.Values{"label": {"Pretzels"}})
	assert.Equal(t, http.StatusForbidden, res.Status)
}

func TestNewRankingRequiresSignIn(t *testing.T) {
	env := testsupport.NewEnv(t)

	res := env.NewClient().Post("/new", nil)

	require.Equal(t, http.StatusSeeOther, res.Status)
	assert.Equal(t, "/login", res.Location())
}

func TestNewRankingRejectsAPlainGET(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()

	res := owner.Get("/new")

	assert.Equal(t, http.StatusSeeOther, res.Status)
	assert.Equal(t, "/", res.Location())
}

func TestNewRankingCreatesARankingForTheSignedInOwner(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()

	before, err := env.Queries.ListRankingsByUser(context.Background(), owner.Ranking.UserID)
	require.NoError(t, err)

	res := owner.Post("/new", nil)
	require.Equal(t, http.StatusSeeOther, res.Status)
	assert.True(t, strings.HasPrefix(res.Location(), "/r/"))

	after, err := env.Queries.ListRankingsByUser(context.Background(), owner.Ranking.UserID)
	require.NoError(t, err)
	assert.Len(t, after, len(before)+1)
}

func TestNewRankingRequiresCSRF(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()

	res := owner.FormWithBogusCSRF(http.MethodPost, "/new", nil)

	assert.Equal(t, http.StatusForbidden, res.Status)
}
