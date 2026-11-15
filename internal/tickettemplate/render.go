package tickettemplate

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
)

// Renderer renders ticket content templates for one integration kind.
type Renderer struct {
	integrationKind string
	templates       ContentTemplates
}

// NewRenderer returns a renderer for the given integration kind and stored templates.
func NewRenderer(integrationKind string, templates ContentTemplates) *Renderer {
	return &Renderer{
		integrationKind: strings.TrimSpace(integrationKind),
		templates:       templates,
	}
}

// Validate checks each non-empty custom template key (specs §5.4): its syntax, and that it
// uses only variables of the update-PR context, so a template written for releases is
// refused with the variable to replace.
func (r *Renderer) Validate() error {
	checks := []struct {
		name string
		raw  string
	}{
		{"title", r.templates.Title},
		{"description", r.templates.Description},
		{"updateComment", r.templates.UpdateComment},
		{"supersedeComment", r.templates.SupersedeComment},
	}
	for _, check := range checks {
		if strings.TrimSpace(check.raw) == "" {
			continue
		}
		if err := validateTemplate(check.raw); err != nil {
			return fmt.Errorf("content_templates.%s: %w", check.name, err)
		}
	}
	return nil
}

// RenderTitle renders the ticket title template.
func (r *Renderer) RenderTitle(ctx Context) (string, error) {
	return r.render(orDefault(r.templates.Title, defaultTitle), ctx)
}

// RenderDescription renders the ticket description template.
func (r *Renderer) RenderDescription(ctx Context) (string, error) {
	return r.render(orDefault(r.templates.Description, defaultDescriptionTemplate(r.integrationKind)), ctx)
}

// RenderUpdateComment renders the note added to a ticket whose PR got a new version.
func (r *Renderer) RenderUpdateComment(ctx Context) (string, error) {
	return r.render(orDefault(r.templates.UpdateComment, defaultUpdateComment), ctx)
}

// RenderSupersedeComment renders the supersede comment template.
func (r *Renderer) RenderSupersedeComment(ctx Context) (string, error) {
	return r.render(orDefault(r.templates.SupersedeComment, defaultSupersedeComment), ctx)
}

func orDefault(custom, fallback string) string {
	if strings.TrimSpace(custom) == "" {
		return fallback
	}
	return custom
}

func (r *Renderer) render(tmplStr string, ctx Context) (string, error) {
	tmpl, err := parseTemplate(tmplStr)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, ctx); err != nil {
		return "", err
	}
	return strings.TrimRight(buf.String(), "\n"), nil
}

func parseTemplate(tmplStr string) (*template.Template, error) {
	tmpl, err := template.New("ticket").Funcs(templateFuncs()).Option("missingkey=zero").Parse(tmplStr)
	if err != nil {
		return nil, err
	}
	if err := checkVariables(tmpl); err != nil {
		return nil, err
	}
	return tmpl, nil
}

func validateTemplate(tmplStr string) error {
	_, err := parseTemplate(tmplStr)
	return err
}
