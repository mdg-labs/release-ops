package mail

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	texttemplate "text/template"
)

//go:embed templates/*.tmpl
var templateFS embed.FS

const appName = "Release Ops"

// InvitationData is template input for invitation emails.
type InvitationData struct {
	ActionURL string
}

// PasswordResetData is template input for password-reset emails.
type PasswordResetData struct {
	ActionURL string
}

// EmailChangeData is template input for email-change confirmation emails.
type EmailChangeData struct {
	ActionURL string
	NewEmail  string
}

// BuildInvitation renders the invitation email message.
func BuildInvitation(to string, data InvitationData) (Message, error) {
	subject := fmt.Sprintf("You've been invited to %s", appName)
	textBody, err := renderTextTemplate("invitation.txt.tmpl", data)
	if err != nil {
		return Message{}, err
	}
	htmlBody, err := renderHTMLTemplate("invitation.html.tmpl", data)
	if err != nil {
		return Message{}, err
	}
	return Message{
		To:       to,
		Subject:  subject,
		TextBody: textBody,
		HTMLBody: htmlBody,
	}, nil
}

// BuildPasswordReset renders the password-reset email message.
func BuildPasswordReset(to string, data PasswordResetData) (Message, error) {
	subject := fmt.Sprintf("Reset your %s password", appName)
	textBody, err := renderTextTemplate("password_reset.txt.tmpl", data)
	if err != nil {
		return Message{}, err
	}
	htmlBody, err := renderHTMLTemplate("password_reset.html.tmpl", data)
	if err != nil {
		return Message{}, err
	}
	return Message{
		To:       to,
		Subject:  subject,
		TextBody: textBody,
		HTMLBody: htmlBody,
	}, nil
}

// BuildEmailChangeConfirmation renders the email-change confirmation message.
func BuildEmailChangeConfirmation(to string, data EmailChangeData) (Message, error) {
	subject := fmt.Sprintf("Confirm your new email for %s", appName)
	textBody, err := renderTextTemplate("email_change.txt.tmpl", data)
	if err != nil {
		return Message{}, err
	}
	htmlBody, err := renderHTMLTemplate("email_change.html.tmpl", data)
	if err != nil {
		return Message{}, err
	}
	return Message{
		To:       to,
		Subject:  subject,
		TextBody: textBody,
		HTMLBody: htmlBody,
	}, nil
}

func renderTextTemplate(name string, data any) (string, error) {
	raw, err := templateFS.ReadFile("templates/" + name)
	if err != nil {
		return "", fmt.Errorf("read template %s: %w", name, err)
	}
	tmpl, err := texttemplate.New(name).Parse(string(raw))
	if err != nil {
		return "", fmt.Errorf("parse template %s: %w", name, err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute template %s: %w", name, err)
	}
	return buf.String(), nil
}

func renderHTMLTemplate(name string, data any) (string, error) {
	raw, err := templateFS.ReadFile("templates/" + name)
	if err != nil {
		return "", fmt.Errorf("read template %s: %w", name, err)
	}
	tmpl, err := template.New(name).Parse(string(raw))
	if err != nil {
		return "", fmt.Errorf("parse template %s: %w", name, err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute template %s: %w", name, err)
	}
	return buf.String(), nil
}
