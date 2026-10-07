package mail

import "context"

// NoopMailer rejects sends because SMTP is not configured.
type NoopMailer struct{}

// Send always returns ErrSMTPNotConfigured.
func (NoopMailer) Send(_ context.Context, _ Message) error {
	return ErrSMTPNotConfigured
}
