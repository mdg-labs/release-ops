package mail

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

const defaultSMTPPort = 587

// Config holds SMTP connection settings from the environment.
type Config struct {
	Host     string
	Port     int
	User     string
	Password string
	From     string
	TLS      bool
}

// MailConfigured reports whether outbound SMTP email is enabled.
func MailConfigured() bool {
	return strings.TrimSpace(os.Getenv("SMTP_HOST")) != ""
}

// LoadConfigFromEnv reads SMTP settings from the process environment.
func LoadConfigFromEnv() (Config, error) {
	host := strings.TrimSpace(os.Getenv("SMTP_HOST"))
	if host == "" {
		return Config{}, nil
	}

	port := defaultSMTPPort
	if raw := strings.TrimSpace(os.Getenv("SMTP_PORT")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			return Config{}, fmt.Errorf("SMTP_PORT: invalid integer %q", raw)
		}
		if n <= 0 {
			return Config{}, fmt.Errorf("SMTP_PORT: must be positive, got %d", n)
		}
		port = n
	}

	tls := true
	if raw := strings.TrimSpace(os.Getenv("SMTP_TLS")); raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			return Config{}, fmt.Errorf("SMTP_TLS: invalid boolean %q", raw)
		}
		tls = parsed
	}

	return Config{
		Host:     host,
		Port:     port,
		User:     os.Getenv("SMTP_USER"),
		Password: os.Getenv("SMTP_PASSWORD"),
		From:     strings.TrimSpace(os.Getenv("SMTP_FROM")),
		TLS:      tls,
	}, nil
}

// NewFromEnv returns an SMTP mailer when SMTP_HOST is set, otherwise NoopMailer.
func NewFromEnv() (Mailer, error) {
	cfg, err := LoadConfigFromEnv()
	if err != nil {
		return nil, err
	}
	if cfg.Host == "" {
		return NoopMailer{}, nil
	}
	return NewSMTPMailer(cfg), nil
}
