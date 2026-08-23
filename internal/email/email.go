// Package email renders and sends the two MVP transactional mails —
// verification and password reset — behind a Sender interface so callers
// never depend on how mail actually leaves the process. The Resend adapter
// and the dev log sink are the two implementations.
package email

import "context"

// Message is a fully rendered, ready-to-send mail. Both an HTML and a
// plain-text body are included so a client that can't render HTML still
// shows the link.
type Message struct {
	To      string
	Subject string
	HTML    string
	Text    string
}

// Sender delivers a rendered Message. Handlers hold this interface, never a
// concrete implementation, so tests can substitute the dev sink freely.
type Sender interface {
	Send(ctx context.Context, msg Message) error
}
