package logging

import (
	"context"
	"io"
	"log/slog"
)

// Install makes a redacting handler over a text handler on w the default logger, which also
// routes the standard log package through it. Both processes call it before anything logs.
func Install(w io.Writer) *slog.Logger {
	logger := slog.New(NewHandler(slog.NewTextHandler(w, nil), Default))
	slog.SetDefault(logger)
	return logger
}

// PrintOnce logs msg at info level with secret left as it is, in that record only, and
// registers it afterwards, so every later line has it removed. It is the one way a secret
// reaches the log: the setup token the operator needs to read once at first start.
func PrintOnce(ctx context.Context, secret, msg string) {
	if secret == "" {
		slog.InfoContext(ctx, msg)
		return
	}
	slog.InfoContext(context.WithValue(ctx, revealKey{}, []string{secret}), msg)
	Register(secret)
}

// Redact returns s with the default registry's secrets and the secret shapes removed, for text
// that is written outside the logger, such as the error a command prints before it exits.
func Redact(s string) string {
	return redactString(Default.redactorNow(), s)
}
