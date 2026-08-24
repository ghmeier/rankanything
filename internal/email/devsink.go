package email

import (
	"context"
	"log/slog"
	"sync"
)

// DevSink logs mail instead of sending it, so development and tests need no
// network. It logs the token-bearing link deliberately: the log is the only
// place to read it, and no API key means this never runs in production.
type DevSink struct {
	logger *slog.Logger

	mu   sync.Mutex
	sent []Message
}

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

// Sent returns every captured message, oldest first, for tests to assert on.
func (d *DevSink) Sent() []Message {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]Message(nil), d.sent...)
}
