// Package mail sends transactional email via SMTP when configured.
package mail

import "context"

// Message is a single outbound email.
type Message struct {
	To       string
	Subject  string
	TextBody string
	HTMLBody string
}

// Mailer delivers messages to recipients.
type Mailer interface {
	Send(ctx context.Context, msg Message) error
}
