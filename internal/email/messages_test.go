package email

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVerificationMessageCarriesTheLink(t *testing.T) {
	t.Parallel()

	msg, err := VerificationMessage("person@example.com", "https://rankanything.app", "the-token")
	require.NoError(t, err)

	wantLink := "https://rankanything.app/verify-email?token=the-token"
	assert.Equal(t, "person@example.com", msg.To)
	assert.Contains(t, msg.HTML, wantLink)
	assert.Contains(t, msg.Text, wantLink)
	assert.NotEmpty(t, msg.Subject)
}

func TestPasswordResetMessageCarriesTheLinkAndEmail(t *testing.T) {
	t.Parallel()

	msg, err := PasswordResetMessage("person@example.com", "https://rankanything.app", "the-token")
	require.NoError(t, err)

	wantLink := "https://rankanything.app/reset-password?token=the-token"
	assert.Contains(t, msg.HTML, wantLink)
	assert.Contains(t, msg.Text, wantLink)
	assert.Contains(t, msg.HTML, "person@example.com")
	assert.Contains(t, msg.Text, "person@example.com")
}

func TestLinkEscapesTheToken(t *testing.T) {
	t.Parallel()

	link := Link("https://rankanything.app", "/verify-email", "a token/with special+chars")

	assert.False(t, strings.Contains(link, " "), "the raw token must not appear unescaped in the URL")
	assert.True(t, strings.HasPrefix(link, "https://rankanything.app/verify-email?token="))
}

func TestLinkTrimsTrailingSlashOnBaseURL(t *testing.T) {
	t.Parallel()

	link := Link("https://rankanything.app/", "/verify-email", "tok")

	assert.Equal(t, "https://rankanything.app/verify-email?token=tok", link)
}

func TestMessageEscapesHTMLInAddress(t *testing.T) {
	t.Parallel()

	msg, err := PasswordResetMessage(`"><script>alert(1)</script>@example.com`, "https://rankanything.app", "tok")
	require.NoError(t, err)

	assert.NotContains(t, msg.HTML, "<script>alert(1)</script>")
}
