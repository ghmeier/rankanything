package email

import (
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewSenderReturnsDevSinkWithoutAnAPIKey(t *testing.T) {
	t.Parallel()

	sender := NewSender("", "from@example.com", slog.New(slog.NewTextHandler(io.Discard, nil)))

	_, ok := sender.(*DevSink)
	assert.True(t, ok, "an empty API key must select the dev sink")
}

func TestNewSenderReturnsResendSenderWithAnAPIKey(t *testing.T) {
	t.Parallel()

	sender := NewSender("key", "from@example.com", slog.New(slog.NewTextHandler(io.Discard, nil)))

	_, ok := sender.(*ResendSender)
	assert.True(t, ok, "a configured API key must select the Resend adapter")
}
