// Package email renders and sends the transactional mails behind a Sender
// interface, so callers never depend on how mail leaves the process.
package email

import "context"

// Message carries both bodies so a client that can't render HTML still shows
// the link.
type Message struct {
	To      string
	Subject string
	HTML    string
	Text    string
}

type Sender interface {
	Send(ctx context.Context, msg Message) error
}
