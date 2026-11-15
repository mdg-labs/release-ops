package tickettemplate

import (
	"encoding/json"
	"fmt"
	"strings"
)

const defaultContentTemplatesJSON = `{"title":"","description":"","updateComment":"","supersedeComment":""}`

// ParseContentTemplates unmarshals ticket_projects.content_templates JSON. A key the stored
// value lacks is empty, which selects the integration-kind default.
func ParseContentTemplates(raw string) (ContentTemplates, error) {
	if strings.TrimSpace(raw) == "" {
		raw = defaultContentTemplatesJSON
	}
	var parsed struct {
		Title            string `json:"title"`
		Description      string `json:"description"`
		UpdateComment    string `json:"updateComment"`
		SupersedeComment string `json:"supersedeComment"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return ContentTemplates{}, fmt.Errorf("parse content_templates: %w", err)
	}
	return ContentTemplates{
		Title:            parsed.Title,
		Description:      parsed.Description,
		UpdateComment:    parsed.UpdateComment,
		SupersedeComment: parsed.SupersedeComment,
	}, nil
}
