package app_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/ghmeier/rankanything/internal/email"
	"github.com/ghmeier/rankanything/internal/testsupport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// extractToken pulls the plaintext token out of a mailed message's link —
// the same value a user would copy by clicking it — so a test can feed it
// back into GET /verify or POST /reset-password without ever touching the
// stored hash directly.
func extractToken(t *testing.T, msg email.Message) string {
	t.Helper()
	for _, line := range strings.Split(msg.Text, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "http") {
			continue
		}
		u, err := url.Parse(line)
		require.NoError(t, err)
		return u.Query().Get("token")
	}
	t.Fatal("no link found in mailed message")
	return ""
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
		// The target must be something a GET can land on: a redirect's
		// follow-up request is always a GET, which rules out /new now that
		// it only answers POST (see TestNewRankingRequiresSignIn). /me is
		// also register's default with no "next" at all, so this uses
		// /components instead to prove "next" actually overrides that
		// default rather than happening to match it.
		res := c.Post("/register", url.Values{"email": {"next@example.com"}, "password": {"supersecret"}, "next": {"/components"}})
		require.Equal(t, http.StatusSeeOther, res.Status)
		assert.Equal(t, "/components", res.Location())
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
	t.Run("rejects a duplicate email with next-step links instead of a bare refusal", func(t *testing.T) {
		first := env.NewClient()
		require.Equal(t, http.StatusSeeOther,
			first.Post("/register", url.Values{"email": {"dup@example.com"}, "password": {"supersecret"}}).Status)

		res := env.NewClient().Post("/register", url.Values{"email": {"DUP@example.com"}, "password": {"supersecret"}})
		assert.Equal(t, http.StatusUnprocessableEntity, res.Status)
		assert.Contains(t, Body(res.Body), "already registered")
		assert.Contains(t, Body(res.Body), `href="/login"`)
		assert.Contains(t, Body(res.Body), `href="/forgot-password"`)
	})
}

func TestRegisterSendsAVerificationEmail(t *testing.T) {
	env := testsupport.NewEnv(t)

	res := env.NewClient().Post("/register", url.Values{"email": {"newcomer@example.com"}, "password": {"supersecret"}})
	require.Equal(t, http.StatusSeeOther, res.Status)

	sent := env.EmailSink.Sent()
	require.Len(t, sent, 1)
	assert.Equal(t, "newcomer@example.com", sent[0].To)
	assert.Contains(t, sent[0].Text, "/verify?token=")
}

func TestVerifyEmail(t *testing.T) {
	t.Run("a valid token verifies the user and lands them on their rankings", func(t *testing.T) {
		env := testsupport.NewEnv(t)
		c := env.NewClient()
		require.Equal(t, http.StatusSeeOther, c.Post("/register",
			url.Values{"email": {"verifyme@example.com"}, "password": {"supersecret"}}).Status)

		token := extractToken(t, env.EmailSink.Sent()[0])
		res := c.Get("/verify?token=" + token)
		require.Equal(t, http.StatusSeeOther, res.Status)
		assert.Equal(t, "/me", res.Location())

		me := c.Get(res.Location())
		assert.NotContains(t, Body(me.Body), "Resend verification email")
	})

	t.Run("an unknown token redirects without verifying anyone", func(t *testing.T) {
		env := testsupport.NewEnv(t)
		c := env.NewClient()
		require.Equal(t, http.StatusSeeOther, c.Post("/register",
			url.Values{"email": {"stillunverified@example.com"}, "password": {"supersecret"}}).Status)

		res := c.Get("/verify?token=not-a-real-token")
		require.Equal(t, http.StatusSeeOther, res.Status)

		me := c.Get("/me")
		assert.Contains(t, Body(me.Body), "Resend verification email")
	})
}

func TestRankingsIndexShowsAVerificationNoticeUntilVerified(t *testing.T) {
	env := testsupport.NewEnv(t)
	owner := env.NewOwnerClient()

	res := owner.Get("/me")
	assert.Contains(t, Body(res.Body), "Resend verification email")
}

func TestResendVerification(t *testing.T) {
	t.Run("requires a signed-in user", func(t *testing.T) {
		env := testsupport.NewEnv(t)

		res := env.NewClient().Post("/resend-verification", nil)

		require.Equal(t, http.StatusSeeOther, res.Status)
		assert.Equal(t, "/login", res.Location())
	})

	t.Run("mails a fresh token and confirms it on the page", func(t *testing.T) {
		env := testsupport.NewEnv(t)
		owner := env.NewOwnerClient()
		require.Len(t, env.EmailSink.Sent(), 1, "registering already sent the first verification email")

		res := owner.HTMX(http.MethodPost, "/resend-verification", nil)

		assert.Equal(t, http.StatusOK, res.Status)
		assert.Contains(t, Body(res.Body), "Verification email sent")
		assert.Len(t, env.EmailSink.Sent(), 2)
	})
}

func TestForgotPassword(t *testing.T) {
	env := testsupport.NewEnv(t)
	require.Equal(t, http.StatusSeeOther, env.NewClient().Post("/register",
		url.Values{"email": {"hasaccount@example.com"}, "password": {"supersecret"}}).Status)
	sentBeforeRequests := len(env.EmailSink.Sent())

	t.Run("a registered address gets the neutral response and a mailed link", func(t *testing.T) {
		res := env.NewClient().HTMX(http.MethodPost, "/forgot-password", url.Values{"email": {"hasaccount@example.com"}})

		assert.Equal(t, http.StatusOK, res.Status)
		assert.Contains(t, Body(res.Body), "If an account exists for that address, we've sent a reset link.")
		assert.Len(t, env.EmailSink.Sent(), sentBeforeRequests+1)
	})

	t.Run("an unregistered address gets the identical response and no mail", func(t *testing.T) {
		res := env.NewClient().HTMX(http.MethodPost, "/forgot-password", url.Values{"email": {"ghost@example.com"}})

		assert.Equal(t, http.StatusOK, res.Status)
		assert.Contains(t, Body(res.Body), "If an account exists for that address, we've sent a reset link.")
		assert.Len(t, env.EmailSink.Sent(), sentBeforeRequests+1, "no mail went out for an address with no account")
	})

	t.Run("a malformed address is rejected before it ever reaches the neutral response", func(t *testing.T) {
		res := env.NewClient().HTMX(http.MethodPost, "/forgot-password", url.Values{"email": {"nope"}})

		assert.Equal(t, http.StatusUnprocessableEntity, res.Status)
		assert.Contains(t, Body(res.Body), "Enter a valid email address.")
	})
}

func TestResetPassword(t *testing.T) {
	t.Run("a valid token changes the password and the old one stops working", func(t *testing.T) {
		env := testsupport.NewEnv(t)
		require.Equal(t, http.StatusSeeOther, env.NewClient().Post("/register",
			url.Values{"email": {"resetme@example.com"}, "password": {"supersecret"}}).Status)

		require.Equal(t, http.StatusOK,
			env.NewClient().HTMX(http.MethodPost, "/forgot-password", url.Values{"email": {"resetme@example.com"}}).Status)
		sent := env.EmailSink.Sent()
		token := extractToken(t, sent[len(sent)-1])

		res := env.NewClient().HTMX(http.MethodPost, "/reset-password",
			url.Values{"token": {token}, "password": {"newpassword123"}})
		assert.Equal(t, http.StatusOK, res.Status)
		assert.Contains(t, Body(res.Body), "Password updated.")

		oldPassword := env.NewClient().Post("/login", url.Values{"email": {"resetme@example.com"}, "password": {"supersecret"}})
		assert.Equal(t, http.StatusUnauthorized, oldPassword.Status)

		newPassword := env.NewClient().Post("/login", url.Values{"email": {"resetme@example.com"}, "password": {"newpassword123"}})
		assert.Equal(t, http.StatusSeeOther, newPassword.Status)
	})

	t.Run("resetting a password ends the sessions opened with the old one", func(t *testing.T) {
		env := testsupport.NewEnv(t)
		stale := env.NewClient()
		require.Equal(t, http.StatusSeeOther, stale.Post("/register",
			url.Values{"email": {"stolen@example.com"}, "password": {"supersecret"}}).Status)
		require.Equal(t, http.StatusOK, stale.Get("/me").Status)

		require.Equal(t, http.StatusOK, env.NewClient().HTMX(http.MethodPost, "/forgot-password",
			url.Values{"email": {"stolen@example.com"}}).Status)
		sent := env.EmailSink.Sent()
		token := extractToken(t, sent[len(sent)-1])
		require.Equal(t, http.StatusOK, env.NewClient().HTMX(http.MethodPost, "/reset-password",
			url.Values{"token": {token}, "password": {"newpassword123"}}).Status)

		res := stale.Get("/me")

		assert.Equal(t, http.StatusSeeOther, res.Status)
		assert.Contains(t, res.Location(), "/login")
	})

	t.Run("a weak password is reported as weak when the token is good", func(t *testing.T) {
		env := testsupport.NewEnv(t)
		require.Equal(t, http.StatusSeeOther, env.NewClient().Post("/register",
			url.Values{"email": {"weak@example.com"}, "password": {"supersecret"}}).Status)
		require.Equal(t, http.StatusOK, env.NewClient().HTMX(http.MethodPost, "/forgot-password",
			url.Values{"email": {"weak@example.com"}}).Status)
		sent := env.EmailSink.Sent()
		token := extractToken(t, sent[len(sent)-1])

		res := env.NewClient().HTMX(http.MethodPost, "/reset-password",
			url.Values{"token": {token}, "password": {"short"}})

		assert.Equal(t, http.StatusUnprocessableEntity, res.Status)
		assert.Contains(t, Body(res.Body), "at least 8 characters")
	})

	// The pair of this test and the one above pins the order of the two
	// checks: a bad token wins over a bad password, which is only possible if
	// the token is verified before the password is hashed.
	t.Run("a bad token is reported as bad even when the password is also weak", func(t *testing.T) {
		env := testsupport.NewEnv(t)

		res := env.NewClient().HTMX(http.MethodPost, "/reset-password",
			url.Values{"token": {"not-a-real-token"}, "password": {"short"}})

		assert.Equal(t, http.StatusUnprocessableEntity, res.Status)
		assert.Contains(t, Body(res.Body), "expired or was already used")
		assert.NotContains(t, Body(res.Body), "at least 8 characters")
	})

	t.Run("an invalid token is rejected without changing anything", func(t *testing.T) {
		env := testsupport.NewEnv(t)

		res := env.NewClient().HTMX(http.MethodPost, "/reset-password",
			url.Values{"token": {"not-a-real-token"}, "password": {"newpassword123"}})

		assert.Equal(t, http.StatusUnprocessableEntity, res.Status)
		assert.Contains(t, Body(res.Body), "expired or was already used")
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
