package mail

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net"
	"net/smtp"
	"strings"
)

type smtpDialer func(ctx context.Context, network, addr string) (net.Conn, error)

// SMTPMailer sends email through an SMTP server.
type SMTPMailer struct {
	cfg   Config
	dial  smtpDialer
	newClient func(conn net.Conn, host string) (smtpClient, error)
}

type smtpClient interface {
	Extension(string) (bool, string)
	StartTLS(*tls.Config) error
	Auth(smtp.Auth) error
	Mail(string) error
	Rcpt(string) error
	Data() (smtpWriter, error)
	Quit() error
	Close() error
}

type smtpWriter interface {
	Write([]byte) (int, error)
	Close() error
}

type stdSMTPClient struct {
	*smtp.Client
}

func (c stdSMTPClient) Data() (smtpWriter, error) {
	return c.Client.Data()
}

// NewSMTPMailer constructs a mailer for the given SMTP configuration.
func NewSMTPMailer(cfg Config) *SMTPMailer {
	return &SMTPMailer{
		cfg:  cfg,
		dial: defaultDialer,
		newClient: func(conn net.Conn, host string) (smtpClient, error) {
			client, err := smtp.NewClient(conn, host)
			if err != nil {
				return nil, err
			}
			return stdSMTPClient{client}, nil
		},
	}
}

// Send delivers msg using SMTP.
func (m *SMTPMailer) Send(ctx context.Context, msg Message) error {
	if strings.TrimSpace(m.cfg.From) == "" {
		return fmt.Errorf("SMTP_FROM is required when SMTP_HOST is set")
	}
	if strings.TrimSpace(msg.To) == "" {
		return fmt.Errorf("recipient address is required")
	}
	if strings.TrimSpace(msg.Subject) == "" {
		return fmt.Errorf("subject is required")
	}
	if strings.TrimSpace(msg.TextBody) == "" && strings.TrimSpace(msg.HTMLBody) == "" {
		return fmt.Errorf("message body is required")
	}

	addr := fmt.Sprintf("%s:%d", m.cfg.Host, m.cfg.Port)
	conn, err := m.dial(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("smtp dial: %w", err)
	}

	client, err := m.newClient(conn, m.cfg.Host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("smtp client: %w", err)
	}
	defer func() {
		_ = client.Quit()
		_ = client.Close()
	}()

	if m.cfg.TLS {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(&tls.Config{ServerName: m.cfg.Host}); err != nil {
				return fmt.Errorf("smtp starttls: %w", err)
			}
		}
	}

	if m.cfg.User != "" {
		auth := smtp.PlainAuth("", m.cfg.User, m.cfg.Password, m.cfg.Host)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}

	if err := client.Mail(m.cfg.From); err != nil {
		return fmt.Errorf("smtp mail from: %w", err)
	}
	if err := client.Rcpt(msg.To); err != nil {
		return fmt.Errorf("smtp rcpt to: %w", err)
	}

	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}

	payload, err := encodeMessage(m.cfg.From, msg)
	if err != nil {
		_ = writer.Close()
		return err
	}
	if _, err := writer.Write(payload); err != nil {
		_ = writer.Close()
		return fmt.Errorf("smtp write: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("smtp data close: %w", err)
	}

	return nil
}

func defaultDialer(ctx context.Context, network, addr string) (net.Conn, error) {
	dialer := &net.Dialer{}
	return dialer.DialContext(ctx, network, addr)
}

func encodeMessage(from string, msg Message) ([]byte, error) {
	var buf bytes.Buffer
	boundary := "release-ops-" + randomBoundaryToken()

	buf.WriteString("From: ")
	buf.WriteString(from)
	buf.WriteString("\r\nTo: ")
	buf.WriteString(msg.To)
	buf.WriteString("\r\nSubject: ")
	buf.WriteString(msg.Subject)
	buf.WriteString("\r\nMIME-Version: 1.0\r\n")

	hasText := strings.TrimSpace(msg.TextBody) != ""
	hasHTML := strings.TrimSpace(msg.HTMLBody) != ""

	switch {
	case hasText && hasHTML:
		buf.WriteString("Content-Type: multipart/alternative; boundary=")
		buf.WriteString(boundary)
		buf.WriteString("\r\n\r\n")

		writePart(&buf, boundary, "text/plain; charset=UTF-8", msg.TextBody)
		writePart(&buf, boundary, "text/html; charset=UTF-8", msg.HTMLBody)

		buf.WriteString("--")
		buf.WriteString(boundary)
		buf.WriteString("--\r\n")
	case hasHTML:
		buf.WriteString("Content-Type: text/html; charset=UTF-8\r\n\r\n")
		buf.WriteString(msg.HTMLBody)
	default:
		buf.WriteString("Content-Type: text/plain; charset=UTF-8\r\n\r\n")
		buf.WriteString(msg.TextBody)
	}

	return buf.Bytes(), nil
}

func writePart(buf *bytes.Buffer, boundary, contentType, body string) {
	buf.WriteString("--")
	buf.WriteString(boundary)
	buf.WriteString("\r\nContent-Type: ")
	buf.WriteString(contentType)
	buf.WriteString("\r\nContent-Transfer-Encoding: 8bit\r\n\r\n")
	buf.WriteString(body)
	buf.WriteString("\r\n")
}

func randomBoundaryToken() string {
	return base64.RawURLEncoding.EncodeToString([]byte("release-ops-mail-boundary"))
}
