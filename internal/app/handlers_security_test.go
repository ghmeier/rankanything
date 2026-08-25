package app_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/ghmeier/rankanything/internal/auth"
	"github.com/ghmeier/rankanything/internal/testsupport"
	"github.com/stretchr/testify/assert"
)

func TestAddTierRejectsInvalidColor(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()
	path := "/r/" + owner.Ranking.Uuid.String() + "/v/" + owner.Draft.ShortUuid + "/tiers"

	res := owner.HTMX(http.MethodPost, path, url.Values{
		"label": {"Bad"}, "color": {"not-a-color"},
	})
	assert.Equal(t, http.StatusUnprocessableEntity, res.Status)
}

func TestAddTierRejectsCSSInjection(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()
	path := "/r/" + owner.Ranking.Uuid.String() + "/v/" + owner.Draft.ShortUuid + "/tiers"

	res := owner.HTMX(http.MethodPost, path, url.Values{
		"label": {"Injected"}, "color": {"red; background-image: url(evil)"},
	})
	assert.Equal(t, http.StatusUnprocessableEntity, res.Status)
}

func TestAddTierAcceptsValidHexColor(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()
	path := "/r/" + owner.Ranking.Uuid.String() + "/v/" + owner.Draft.ShortUuid + "/tiers"

	res := owner.HTMX(http.MethodPost, path, url.Values{
		"label": {"Good"}, "color": {"#ff5500"},
	})
	assert.Equal(t, http.StatusOK, res.Status)
}

func TestLoginRateLimitsAfterRepeatedFailures(t *testing.T) {
	env := testsupport.NewEnv(t)

	rl := auth.NewRateLimiter()
	t.Cleanup(rl.Stop)
	env.App.RateLimiter = rl
	env.RebuildServer()

	c := env.NewClient()
	var lastStatus int
	for i := range 12 {
		res := c.Post("/login", url.Values{"email": {"nobody@example.com"}, "password": {"wrongpassword" + string(rune('0'+i))}})
		lastStatus = res.Status
		if lastStatus == http.StatusTooManyRequests {
			break
		}
	}
	assert.Equal(t, http.StatusTooManyRequests, lastStatus,
		"login should return 429 after exceeding the burst limit")
}
