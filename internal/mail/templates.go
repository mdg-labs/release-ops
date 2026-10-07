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

// EmailPageData is shared HTML layout input aligned with apps/web/app/globals.css (:root).
type EmailPageData struct {
	AppName     string
	Title       string
	ActionURL   string
	ActionLabel string
	Footer      string
	NewEmail    string // optional — email-change only
}

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
	htmlBody, err := renderHTMLTemplate("invitation", EmailPageData{
		AppName:     appName,
		Title:       "You've been invited",
		ActionURL:   data.ActionURL,
		ActionLabel: "Accept your invitation",
		Footer:      "If you did not expect this invitation, you can ignore this email.",
	})
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
	htmlBody, err := renderHTMLTemplate("password_reset", EmailPageData{
		AppName:     appName,
		Title:       "Reset your password",
		ActionURL:   data.ActionURL,
		ActionLabel: "Reset your password",
		Footer:      "If you did not request a password reset, you can ignore this email.",
	})
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
	htmlBody, err := renderHTMLTemplate("email_change", EmailPageData{
		AppName:     appName,
		Title:       "Confirm your new email",
		ActionURL:   data.ActionURL,
		ActionLabel: "Confirm email change",
		Footer:      "If you did not request this change, you can ignore this email.",
		NewEmail:    data.NewEmail,
	})
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

func renderHTMLTemplate(templateName string, data any) (string, error) {
	// Parse layout with a single email template so per-email "body" blocks do not collide.
	tmpl, err := template.ParseFS(
		templateFS,
		"templates/layout.html.tmpl",
		"templates/"+templateName+".html.tmpl",
	)
	if err != nil {
		return "", fmt.Errorf("parse html templates: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, templateName, data); err != nil {
		return "", fmt.Errorf("execute template %s: %w", templateName, err)
	}
	return buf.String(), nil
}
