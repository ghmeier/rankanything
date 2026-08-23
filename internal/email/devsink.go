package email

import (
	"context"
	"log/slog"
	"sync"
)

// DevSink logs the rendered mail instead of sending it, so local development
// and tests need neither network access nor a Resend API key.
//
// It deliberately logs the token-bearing link in the message body — the one
// exception to "never log a plaintext token" the rest of this codebase
// follows. That's safe here because the message logged IS the mail; nothing
// else reads this log for anything but local debugging, and DevSink only
// ever runs when no Resend API key is configured (never in production).
type DevSink struct {
	logger *slog.Logger

	mu   sync.Mutex
	sent []Message
}

// NewDevSink returns a sink that logs through logger.
func NewDevSink(logger *slog.Logger) *DevSink {
	return &DevSink{logger: logger}
}

func (d *DevSink) Send(_ context.Context, msg Message) error {
	d.logger.Info("dev email sink: not sent, logged only",
		"to", msg.To, "subject", msg.Subject, "text", msg.Text)

	d.mu.Lock()
	d.sent = append(d.sent, msg)
	d.mu.Unlock()
	return nil
}

// Sent returns every message captured so far, oldest first. It exists for
// tests to assert on what would have been sent.
func (d *DevSink) Sent() []Message {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]Message(nil), d.sent...)
}
