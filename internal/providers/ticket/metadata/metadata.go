// Package metadata fetches ticket-provider project, status, and priority options.
package metadata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Item is a selectable metadata option returned to the web UI.
type Item struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Label string `json:"label,omitempty"`
}

// ListResponse is the JSON body for ticket-metadata GET endpoints.
type ListResponse struct {
	Items   []Item `json:"items"`
	Message string `json:"message,omitempty"`
}

// Provider lists remote metadata for a ticket integration.
type Provider interface {
	ListWorkspaces(ctx context.Context) ([]Item, error)
	ListProjects(ctx context.Context, workspaceID string) ([]Item, error)
	ListStatuses(ctx context.Context, externalProjectID string) ([]Item, error)
	ListPriorities(ctx context.Context, externalProjectID string) ([]Item, error)
	ListIssueTypes(ctx context.Context, externalProjectID string) ([]Item, error)
}

// ErrUnsupported indicates the operation is not valid for the integration kind.
var ErrUnsupported = errors.New("metadata operation not supported for this integration kind")

// NewProvider builds a metadata provider for a ticket integration kind.
func NewProvider(kind string, baseURL *string, secret []byte, client *http.Client) (Provider, error) {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	switch kind {
	case "phasical":
		return newPhasicalProvider(baseURL, secret, client)
	case "jira":
		return newJiraProvider(baseURL, secret, client)
	case "linear":
		return newLinearProvider(secret, client)
	default:
		return nil, fmt.Errorf("unsupported ticket integration kind %q", kind)
	}
}

func parsePhasicalSecret(secret []byte) (string, error) {
	var creds struct {
		APIKey string `json:"api_key"`
	}
	if err := json.Unmarshal(secret, &creds); err != nil {
		return "", fmt.Errorf("parse integration secret: %w", err)
	}
	apiKey := strings.TrimSpace(creds.APIKey)
	if apiKey == "" {
		return "", errors.New("phasical: api_key is required in integration secret")
	}
	return apiKey, nil
}

func parseJiraSecret(secret []byte) (email, apiToken string, err error) {
	var creds struct {
		Email    string `json:"email"`
		APIToken string `json:"api_token"`
	}
	if err := json.Unmarshal(secret, &creds); err != nil {
		return "", "", fmt.Errorf("parse integration secret: %w", err)
	}
	email = strings.TrimSpace(creds.Email)
	apiToken = strings.TrimSpace(creds.APIToken)
	if email == "" {
		return "", "", errors.New("jira: email is required in integration secret")
	}
	if apiToken == "" {
		return "", "", errors.New("jira: api_token is required in integration secret")
	}
	return email, apiToken, nil
}

// NewLinearProviderWithEndpoint builds a Linear metadata provider for tests.
func NewLinearProviderWithEndpoint(secret []byte, endpoint string, client *http.Client) (Provider, error) {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return newLinearProviderWithEndpoint(secret, endpoint, client)
}

func parseLinearSecret(secret []byte) (string, error) {
	var creds struct {
		APIKey string `json:"api_key"`
	}
	if err := json.Unmarshal(secret, &creds); err != nil {
		return "", fmt.Errorf("parse integration secret: %w", err)
	}
	apiKey := strings.TrimSpace(creds.APIKey)
	if apiKey == "" {
		return "", errors.New("linear: api_key is required in integration secret")
	}
	return apiKey, nil
}

func requireBaseURL(baseURL *string, kind string) (string, error) {
	if baseURL == nil || strings.TrimSpace(*baseURL) == "" {
		return "", fmt.Errorf("%s: base_url is required", kind)
	}
	raw := strings.TrimRight(strings.TrimSpace(*baseURL), "/")
	if raw == "" {
		return "", fmt.Errorf("%s: base_url is required", kind)
	}
	return raw, nil
}

// normalizePhasicalAPIBase appends /api when missing (Kaneo-compatible Phasical hosts).
func normalizePhasicalAPIBase(raw string) string {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	if raw == "" {
		return raw
	}
	if strings.HasSuffix(raw, "/api") {
		return raw
	}
	return raw + "/api"
}
