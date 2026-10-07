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

// Validate checks syntax for each non-empty custom template key (specs §5.4).
func (r *Renderer) Validate() error {
	checks := []struct {
		name string
		raw  string
	}{
		{"title", r.templates.Title},
		{"description", r.templates.Description},
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
	return r.render(r.resolveTitleTemplate(), ctx)
}

// RenderDescription renders the ticket description template.
func (r *Renderer) RenderDescription(ctx Context) (string, error) {
	return r.render(r.resolveDescriptionTemplate(), ctx)
}

// RenderSupersedeComment renders the supersede comment template.
func (r *Renderer) RenderSupersedeComment(ctx Context) (string, error) {
	return r.render(r.resolveSupersedeCommentTemplate(), ctx)
}

func (r *Renderer) resolveTitleTemplate() string {
	if strings.TrimSpace(r.templates.Title) == "" {
		return defaultTitleTemplate()
	}
	return r.templates.Title
}

func (r *Renderer) resolveDescriptionTemplate() string {
	if strings.TrimSpace(r.templates.Description) == "" {
		return defaultDescriptionTemplate(r.integrationKind)
	}
	return r.templates.Description
}

func (r *Renderer) resolveSupersedeCommentTemplate() string {
	if strings.TrimSpace(r.templates.SupersedeComment) == "" {
		return defaultSupersedeCommentTemplate(r.integrationKind)
	}
	return r.templates.SupersedeComment
}

func (r *Renderer) render(tmplStr string, ctx Context) (string, error) {
	if err := validateTemplate(tmplStr); err != nil {
		return "", err
	}
	tmpl, err := template.New("ticket").Funcs(templateFuncs()).Option("missingkey=zero").Parse(tmplStr)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, ctx); err != nil {
		return "", err
	}
	return strings.TrimRight(buf.String(), "\n"), nil
}

func validateTemplate(tmplStr string) error {
	_, err := template.New("validate").Funcs(templateFuncs()).Option("missingkey=zero").Parse(tmplStr)
	return err
}
