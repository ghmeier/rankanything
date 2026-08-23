package email

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDevSinkCapturesSentMessages(t *testing.T) {
	t.Parallel()

	sink := NewDevSink(slog.New(slog.NewTextHandler(io.Discard, nil)))

	msg := Message{To: "person@example.com", Subject: "Verify your email", HTML: "<p>hi</p>", Text: "hi"}
	err := sink.Send(context.Background(), msg)
	require.NoError(t, err)

	sent := sink.Sent()
	require.Len(t, sent, 1)
	assert.Equal(t, msg, sent[0])
}

func TestDevSinkCapturesMultipleMessagesInOrder(t *testing.T) {
	t.Parallel()

	sink := NewDevSink(slog.New(slog.NewTextHandler(io.Discard, nil)))

	first := Message{To: "a@example.com", Subject: "first"}
	second := Message{To: "b@example.com", Subject: "second"}
	require.NoError(t, sink.Send(context.Background(), first))
	require.NoError(t, sink.Send(context.Background(), second))

	sent := sink.Sent()
	require.Len(t, sent, 2)
	assert.Equal(t, "first", sent[0].Subject)
	assert.Equal(t, "second", sent[1].Subject)
}

func TestDevSinkNeedsNoNetwork(t *testing.T) {
	t.Parallel()

	// A DevSink built with no network access configured anywhere must still
	// succeed — that's the whole point of the sink.
	sink := NewDevSink(slog.New(slog.NewTextHandler(io.Discard, nil)))

	err := sink.Send(context.Background(), Message{To: "person@example.com"})

	assert.NoError(t, err)
}
