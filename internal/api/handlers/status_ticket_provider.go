package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/mdg-labs/release-ops/internal/providers/ticket"
	"github.com/mdg-labs/release-ops/internal/store"
)

var errStatusIntegrationUnavailable = errors.New("integration unavailable")

type statusPhasicalPayload struct {
	APIKey string `json:"api_key"`
}

type statusJiraPayload struct {
	Email    string `json:"email"`
	APIToken string `json:"api_token"`
}

type statusLinearPayload struct {
	APIKey string `json:"api_key"`
}

func newTicketProvider(integration store.Integration, payload []byte, httpClient *http.Client) (ticket.TicketProvider, error) {
	switch integration.Kind {
	case "phasical":
		if integration.BaseURL == nil || *integration.BaseURL == "" {
			return nil, errors.New("phasical integration requires base_url")
		}
		var creds statusPhasicalPayload
		if err := json.Unmarshal(payload, &creds); err != nil {
			return nil, fmt.Errorf("parse phasical payload: %w", err)
		}
		return ticket.NewPhasicalProvider(*integration.BaseURL, creds.APIKey, httpClient)
	case "jira":
		if integration.BaseURL == nil || *integration.BaseURL == "" {
			return nil, errors.New("jira integration requires base_url")
		}
		var creds statusJiraPayload
		if err := json.Unmarshal(payload, &creds); err != nil {
			return nil, fmt.Errorf("parse jira payload: %w", err)
		}
		return ticket.NewJiraProvider(*integration.BaseURL, creds.Email, creds.APIToken, httpClient)
	case "linear":
		var creds statusLinearPayload
		if err := json.Unmarshal(payload, &creds); err != nil {
			return nil, fmt.Errorf("parse linear payload: %w", err)
		}
		return ticket.NewLinearProvider(creds.APIKey, httpClient)
	default:
		return nil, fmt.Errorf("unsupported ticket integration kind %q", integration.Kind)
	}
}
