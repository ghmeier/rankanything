package app_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ghmeier/rankanything/internal/testsupport"
)

func TestSignedOutVisitorGetsLandingPageAtRoot(t *testing.T) {
	env := testsupport.NewEnv(t)
	c := env.NewClient()

	res := c.Get("/")

	assert.Equal(t, http.StatusOK, res.Status)
	body := Body(res.Body)
	assert.Contains(t, body, "Rank anything.", "hero text")
	assert.Contains(t, body, "Create your first ranking", "CTA into signup")
	assert.Contains(t, body, `href="/register"`, "CTA points at the signup page")
	assert.Contains(t, body, "free", "the pitch emphasizes rankings are free")
	assert.Contains(t, body, "Sign in", "navbar log-in affordance for a signed-out visitor")
}

func TestSignedInVisitorIsRedirectedAwayFromRoot(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()

	res := owner.Get("/")

	assert.Equal(t, http.StatusSeeOther, res.Status)
	assert.Equal(t, "/me", res.Location())
}

func TestRobotsTxtIsServed(t *testing.T) {
	env := testsupport.NewEnv(t)
	c := env.NewClient()

	res := c.Get("/robots.txt")

	assert.Equal(t, http.StatusOK, res.Status)
	assert.Contains(t, res.Header.Get("Content-Type"), "text/plain")
	assert.Contains(t, Body(res.Body), "User-agent")
	assert.Contains(t, Body(res.Body), "Sitemap:")
}

func TestSitemapXMLIsServed(t *testing.T) {
	env := testsupport.NewEnv(t)
	c := env.NewClient()

	res := c.Get("/sitemap.xml")

	assert.Equal(t, http.StatusOK, res.Status)
	assert.Contains(t, res.Header.Get("Content-Type"), "xml")
	assert.Contains(t, Body(res.Body), "<urlset")
	assert.Contains(t, Body(res.Body), "<loc>https://rankanything.app/</loc>")
}
