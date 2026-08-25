package email

import (
	"context"
	"log/slog"
	"sync"
)

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

func (d *DevSink) Sent() []Message {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]Message(nil), d.sent...)
}
