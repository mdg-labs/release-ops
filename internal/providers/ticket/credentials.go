package ticket

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// ParseKaneoSecret returns the API key from a decrypted Kaneo integration payload.
func ParseKaneoSecret(secret []byte) (string, error) {
	var creds struct {
		APIKey string `json:"api_key"`
	}
	if err := json.Unmarshal(secret, &creds); err != nil {
		return "", fmt.Errorf("parse integration secret: %w", err)
	}
	apiKey := strings.TrimSpace(creds.APIKey)
	if apiKey == "" {
		return "", errors.New("kaneo: api_key is required in integration secret")
	}
	return apiKey, nil
}

// ParseJiraSecret returns the email and API token from a decrypted Jira integration payload.
func ParseJiraSecret(secret []byte) (email, apiToken string, err error) {
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

// ParseLinearSecret returns the API key from a decrypted Linear integration payload.
func ParseLinearSecret(secret []byte) (string, error) {
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

// NewProviderFromIntegration builds the TicketProvider for a ticket integration from its
// kind, base URL and decrypted secret payload.
func NewProviderFromIntegration(kind string, baseURL *string, secret []byte, client *http.Client) (TicketProvider, error) {
	switch kind {
	case IntegrationKindKaneo:
		if baseURL == nil || strings.TrimSpace(*baseURL) == "" {
			return nil, errors.New("kaneo integration requires base_url")
		}
		apiKey, err := ParseKaneoSecret(secret)
		if err != nil {
			return nil, err
		}
		return NewKaneoProvider(*baseURL, apiKey, client)
	case IntegrationKindJira:
		if baseURL == nil || strings.TrimSpace(*baseURL) == "" {
			return nil, errors.New("jira integration requires base_url")
		}
		email, apiToken, err := ParseJiraSecret(secret)
		if err != nil {
			return nil, err
		}
		return NewJiraProvider(*baseURL, email, apiToken, client)
	case IntegrationKindLinear:
		apiKey, err := ParseLinearSecret(secret)
		if err != nil {
			return nil, err
		}
		return NewLinearProvider(apiKey, client)
	default:
		return nil, fmt.Errorf("unsupported ticket integration kind %q", kind)
	}
}
