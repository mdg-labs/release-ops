package mail

import "errors"

// ErrSMTPNotConfigured is returned when SMTP_HOST is unset.
var ErrSMTPNotConfigured = errors.New("smtp not configured")
