package tickettemplate

import (
	"encoding/json"
	"fmt"
	"strings"
)

const defaultContentTemplatesJSON = `{"title":"","description":"","supersedeComment":""}`

// ParseContentTemplates unmarshals ticket_projects.content_templates JSON.
func ParseContentTemplates(raw string) (ContentTemplates, error) {
	if strings.TrimSpace(raw) == "" {
		raw = defaultContentTemplatesJSON
	}
	var parsed struct {
		Title            string `json:"title"`
		Description      string `json:"description"`
		SupersedeComment string `json:"supersedeComment"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return ContentTemplates{}, fmt.Errorf("parse content_templates: %w", err)
	}
	return ContentTemplates{
		Title:            parsed.Title,
		Description:      parsed.Description,
		SupersedeComment: parsed.SupersedeComment,
	}, nil
}
