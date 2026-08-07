package mail

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
)

func TestNoopMailerReturnsErrSMTPNotConfigured(t *testing.T) {

	err := NoopMailer{}.Send(context.Background(), Message{To: "user@example.com"})
	if !errors.Is(err, ErrSMTPNotConfigured) {
		t.Fatalf("Send() error = %v, want ErrSMTPNotConfigured", err)
	}
}

func TestMailConfigured(t *testing.T) {

	t.Setenv("SMTP_HOST", "")
	if MailConfigured() {
		t.Fatal("MailConfigured() = true with empty SMTP_HOST")
	}

	t.Setenv("SMTP_HOST", "smtp.example.com")
	if !MailConfigured() {
		t.Fatal("MailConfigured() = false with SMTP_HOST set")
	}
}

func TestLoadConfigFromEnvDefaults(t *testing.T) {

	clearSMTPEnv(t)
	t.Setenv("SMTP_HOST", "smtp.example.com")

	cfg, err := LoadConfigFromEnv()
	if err != nil {
		t.Fatalf("LoadConfigFromEnv() error = %v", err)
	}
	if cfg.Host != "smtp.example.com" {
		t.Fatalf("Host = %q", cfg.Host)
	}
	if cfg.Port != defaultSMTPPort {
		t.Fatalf("Port = %d, want %d", cfg.Port, defaultSMTPPort)
	}
	if !cfg.TLS {
		t.Fatal("TLS = false, want true")
	}
}

func TestLoadConfigFromEnvEmptyHost(t *testing.T) {

	clearSMTPEnv(t)

	cfg, err := LoadConfigFromEnv()
	if err != nil {
		t.Fatalf("LoadConfigFromEnv() error = %v", err)
	}
	if cfg.Host != "" {
		t.Fatalf("Host = %q, want empty", cfg.Host)
	}
}

func TestLoadConfigFromEnvCustomValues(t *testing.T) {

	clearSMTPEnv(t)
	t.Setenv("SMTP_HOST", "mail.example.com")
	t.Setenv("SMTP_PORT", "2525")
	t.Setenv("SMTP_USER", "smtp-user")
	t.Setenv("SMTP_PASSWORD", "smtp-pass")
	t.Setenv("SMTP_FROM", "release-ops@example.com")
	t.Setenv("SMTP_TLS", "false")

	cfg, err := LoadConfigFromEnv()
	if err != nil {
		t.Fatalf("LoadConfigFromEnv() error = %v", err)
	}
	if cfg.Port != 2525 {
		t.Fatalf("Port = %d, want 2525", cfg.Port)
	}
	if cfg.User != "smtp-user" || cfg.Password != "smtp-pass" {
		t.Fatalf("auth = %q/%q", cfg.User, cfg.Password)
	}
	if cfg.From != "release-ops@example.com" {
		t.Fatalf("From = %q", cfg.From)
	}
	if cfg.TLS {
		t.Fatal("TLS = true, want false")
	}
}

func TestLoadConfigFromEnvInvalidPort(t *testing.T) {

	clearSMTPEnv(t)
	t.Setenv("SMTP_HOST", "smtp.example.com")
	t.Setenv("SMTP_PORT", "not-a-number")

	_, err := LoadConfigFromEnv()
	if err == nil {
		t.Fatal("LoadConfigFromEnv() expected error for invalid SMTP_PORT")
	}
}

func TestNewFromEnvNoop(t *testing.T) {

	clearSMTPEnv(t)

	mailer, err := NewFromEnv()
	if err != nil {
		t.Fatalf("NewFromEnv() error = %v", err)
	}
	if _, ok := mailer.(NoopMailer); !ok {
		t.Fatalf("mailer type = %T, want NoopMailer", mailer)
	}
}

func TestNewFromEnvSMTP(t *testing.T) {

	clearSMTPEnv(t)
	t.Setenv("SMTP_HOST", "smtp.example.com")
	t.Setenv("SMTP_FROM", "release-ops@example.com")

	mailer, err := NewFromEnv()
	if err != nil {
		t.Fatalf("NewFromEnv() error = %v", err)
	}
	if _, ok := mailer.(*SMTPMailer); !ok {
		t.Fatalf("mailer type = %T, want *SMTPMailer", mailer)
	}
}

func clearSMTPEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"SMTP_HOST",
		"SMTP_PORT",
		"SMTP_USER",
		"SMTP_PASSWORD",
		"SMTP_FROM",
		"SMTP_TLS",
	} {
		t.Setenv(key, "")
	}
}

func TestBuildInvitationMessage(t *testing.T) {
	t.Parallel()

	msg, err := BuildInvitation("invitee@example.com", InvitationData{
		ActionURL: "https://release-ops.example.com/accept-invitation?token=abc",
	})
	if err != nil {
		t.Fatalf("BuildInvitation() error = %v", err)
	}
	if msg.To != "invitee@example.com" {
		t.Fatalf("To = %q", msg.To)
	}
	if !strings.Contains(msg.Subject, "invited") {
		t.Fatalf("Subject = %q", msg.Subject)
	}
	if !strings.Contains(msg.TextBody, "accept-invitation?token=abc") {
		t.Fatalf("TextBody = %q", msg.TextBody)
	}
	if !strings.Contains(msg.HTMLBody, "accept-invitation?token=abc") {
		t.Fatalf("HTMLBody = %q", msg.HTMLBody)
	}
	if !strings.Contains(msg.HTMLBody, "background-color:#262626") {
		t.Fatalf("HTMLBody missing primary button style: %q", msg.HTMLBody)
	}
	if !strings.Contains(msg.HTMLBody, "border-radius:10px") {
		t.Fatalf("HTMLBody missing card radius: %q", msg.HTMLBody)
	}
}

func TestBuildPasswordResetMessage(t *testing.T) {
	t.Parallel()

	msg, err := BuildPasswordReset("user@example.com", PasswordResetData{
		ActionURL: "https://release-ops.example.com/reset-password?token=abc",
	})
	if err != nil {
		t.Fatalf("BuildPasswordReset() error = %v", err)
	}
	if !strings.Contains(msg.Subject, "Reset") {
		t.Fatalf("Subject = %q", msg.Subject)
	}
	if !strings.Contains(msg.TextBody, "reset-password?token=abc") {
		t.Fatalf("TextBody = %q", msg.TextBody)
	}
	if !strings.Contains(msg.HTMLBody, "reset-password?token=abc") {
		t.Fatalf("HTMLBody = %q", msg.HTMLBody)
	}
}

func TestBuildEmailChangeConfirmationMessage(t *testing.T) {
	t.Parallel()

	msg, err := BuildEmailChangeConfirmation("new@example.com", EmailChangeData{
		ActionURL: "https://release-ops.example.com/confirm-email-change?token=abc",
		NewEmail:  "new@example.com",
	})
	if err != nil {
		t.Fatalf("BuildEmailChangeConfirmation() error = %v", err)
	}
	if msg.To != "new@example.com" {
		t.Fatalf("To = %q", msg.To)
	}
	if !strings.Contains(msg.TextBody, "new@example.com") {
		t.Fatalf("TextBody = %q", msg.TextBody)
	}
	if !strings.Contains(msg.HTMLBody, "confirm-email-change?token=abc") {
		t.Fatalf("HTMLBody = %q", msg.HTMLBody)
	}
}

func TestSMTPMailerSendRequiresFrom(t *testing.T) {
	t.Parallel()

	mailer := NewSMTPMailer(Config{
		Host: "smtp.example.com",
		Port: 587,
		TLS:  false,
	})

	err := mailer.Send(context.Background(), Message{
		To:       "user@example.com",
		Subject:  "Test",
		TextBody: "hello",
	})
	if err == nil || !strings.Contains(err.Error(), "SMTP_FROM") {
		t.Fatalf("Send() error = %v, want SMTP_FROM requirement", err)
	}
}

func TestEncodeMessageMultipart(t *testing.T) {
	t.Parallel()

	payload, err := encodeMessage("release-ops@example.com", Message{
		To:       "user@example.com",
		Subject:  "Test",
		TextBody: "plain",
		HTMLBody: "<p>html</p>",
	})
	if err != nil {
		t.Fatalf("encodeMessage() error = %v", err)
	}
	body := string(payload)
	for _, want := range []string{
		"multipart/alternative",
		"text/plain",
		"text/html",
		"plain",
		"<p>html</p>",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("payload missing %q: %s", want, body)
		}
	}
}

func TestSMTPMailerSendWithMockClient(t *testing.T) {
	t.Parallel()

	var gotFrom string
	var gotTo string
	var gotPayload string

	mailer := &SMTPMailer{
		cfg: Config{
			Host: "smtp.test",
			Port: 587,
			From: "release-ops@example.com",
			User: "smtp-user",
			TLS:  true,
		},
		dial: func(context.Context, string, string) (net.Conn, error) {
			return &pipeConn{}, nil
		},
		newClient: func(net.Conn, string) (smtpClient, error) {
			return &mockSMTPClient{
				onMail: func(from string) error {
					gotFrom = from
					return nil
				},
				onRcpt: func(to string) error {
					gotTo = to
					return nil
				},
				onData: func(payload []byte) error {
					gotPayload = string(payload)
					return nil
				},
			}, nil
		},
	}

	err := mailer.Send(context.Background(), Message{
		To:       "user@example.com",
		Subject:  "Test subject",
		TextBody: "plain body",
		HTMLBody: "<p>html body</p>",
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if gotFrom != "release-ops@example.com" {
		t.Fatalf("MAIL FROM = %q", gotFrom)
	}
	if gotTo != "user@example.com" {
		t.Fatalf("RCPT TO = %q", gotTo)
	}
	for _, want := range []string{"Test subject", "plain body", "<p>html body</p>"} {
		if !strings.Contains(gotPayload, want) {
			t.Fatalf("payload missing %q: %s", want, gotPayload)
		}
	}
}

func TestSMTPMailerSendIntegration(t *testing.T) {
	server := startTestSMTPServer(t)
	defer server.Close()

	mailer := NewSMTPMailer(Config{
		Host:     "127.0.0.1",
		Port:     server.Port,
		From:     "release-ops@example.com",
		User:     "smtp-user",
		Password: "smtp-pass",
		TLS:      false,
	})

	err := mailer.Send(context.Background(), Message{
		To:       "recipient@example.com",
		Subject:  "Integration test",
		TextBody: "hello from test server",
		HTMLBody: "<p>hello from test server</p>",
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	msg := server.WaitForMessage()
	if !strings.Contains(msg, "recipient@example.com") {
		t.Fatalf("server message missing recipient: %s", msg)
	}
	if !strings.Contains(msg, "Integration test") {
		t.Fatalf("server message missing subject: %s", msg)
	}
	if !strings.Contains(msg, "hello from test server") {
		t.Fatalf("server message missing body: %s", msg)
	}
}
