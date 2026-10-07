package handlers

import (
	"errors"
	"net/http"

	"github.com/mdg-labs/release-ops/internal/providers/ticket"
	"github.com/mdg-labs/release-ops/internal/store"
)

var errStatusIntegrationUnavailable = errors.New("integration unavailable")

func newTicketProvider(integration store.Integration, payload []byte, httpClient *http.Client) (ticket.TicketProvider, error) {
	return ticket.NewProviderFromIntegration(integration.Kind, integration.BaseURL, payload, httpClient)
}
