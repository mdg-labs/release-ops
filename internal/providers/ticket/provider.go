package ticket

import (
	"context"
	"encoding/json"
	"fmt"
)

// Classified status values returned by ClassifyStatus (specs §5.2, §6).
const (
	StatusOpen      = "open"
	StatusDone      = "done"
	StatusCancelled = "cancelled"
	StatusUnknown   = "unknown"
)

// On-open ticket policy values from ticket_projects.on_open_ticket_policy (specs §5.2.1).
const (
	PolicySupersede  = "supersede"
	PolicyMerge      = "merge"
	PolicySkipIfOpen = "skip_if_open"
)

// StatusMapping mirrors ticket_projects.status_mapping JSON (specs §5.3).
type StatusMapping struct {
	Open       []string `json:"open"`
	Done       []string `json:"done"`
	Cancelled  []string `json:"cancelled"`
	Superseded string   `json:"superseded"`
}

// TicketProject is the ticket target configuration for a monitored repo (specs §6).
type TicketProject struct {
	ID                 string
	IntegrationID      string
	ExternalProjectID  string
	CreateConfig       map[string]any
	StatusMapping      StatusMapping
	OnOpenTicketPolicy string // supersede | merge | skip_if_open
}

// TicketInput is passed to TicketProvider.CreateTicket (specs §6).
type TicketInput struct {
	Title       string
	Description string
	Project     TicketProject
}

// TicketProvider creates and updates tickets in Phasical, Jira, or Linear (specs §6).
type TicketProvider interface {
	CreateTicket(ctx context.Context, input TicketInput) (externalID string, err error)
	GetTicketStatus(ctx context.Context, externalID string) (status string, err error)
	UpdateTicketStatus(ctx context.Context, externalID, status string) error
	AddTicketComment(ctx context.Context, externalID, body string) error
	UpdateTicket(ctx context.Context, externalID string, title, description string) error
}

// ParseStatusMapping unmarshals a ticket_projects.status_mapping JSON blob.
func ParseStatusMapping(raw string) (StatusMapping, error) {
	var mapping StatusMapping
	if err := json.Unmarshal([]byte(raw), &mapping); err != nil {
		return StatusMapping{}, fmt.Errorf("parse status_mapping: %w", err)
	}
	return mapping, nil
}
