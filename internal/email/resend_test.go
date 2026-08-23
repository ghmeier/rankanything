package email

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResendSenderBuildsTheExpectedRequest(t *testing.T) {
	t.Parallel()

	var (
		gotMethod string
		gotPath   string
		gotAuth   string
		gotBody   resendRequest
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		require.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	sender := newResendSender("test-api-key", "Rank Anything <hello@rankanything.app>", server.URL)

	err := sender.Send(context.Background(), Message{
		To:      "person@example.com",
		Subject: "Verify your email",
		HTML:    "<p>hi</p>",
		Text:    "hi",
	})
	require.NoError(t, err)

	assert.Equal(t, http.MethodPost, gotMethod)
	assert.Equal(t, "/emails", gotPath)
	assert.Equal(t, "Bearer test-api-key", gotAuth)
	assert.Equal(t, "Rank Anything <hello@rankanything.app>", gotBody.From)
	assert.Equal(t, []string{"person@example.com"}, gotBody.To)
	assert.Equal(t, "Verify your email", gotBody.Subject)
	assert.Equal(t, "<p>hi</p>", gotBody.HTML)
	assert.Equal(t, "hi", gotBody.Text)
}

func TestResendSenderReturnsErrorOnNonSuccessStatus(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"invalid api key"}`))
	}))
	defer server.Close()

	sender := newResendSender("bad-key", "hello@rankanything.app", server.URL)

	err := sender.Send(context.Background(), Message{To: "person@example.com"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "401")
}

func TestResendSenderNeverContactsRealResend(t *testing.T) {
	t.Parallel()

	// The sender always calls whatever baseURL it was built with. Building
	// it via newResendSender against an httptest.Server (rather than
	// NewResendSender's hardcoded resendAPIBaseURL) proves the adapter
	// makes no other host reachable in tests.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	sender := newResendSender("key", "from@example.com", server.URL)
	assert.NotEqual(t, resendAPIBaseURL, sender.baseURL)

	err := sender.Send(context.Background(), Message{To: "person@example.com"})
	assert.NoError(t, err)
}
