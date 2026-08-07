package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/mdg-labs/release-ops/internal/api/auth"
	"github.com/mdg-labs/release-ops/internal/store"
)

var validIntegrationKinds = map[string]struct{}{
	"github":   {},
	"gitlab":   {},
	"gitea":    {},
	"forgejo":  {},
	"codeberg": {},
	"phasical": {},
	"jira":     {},
	"linear":   {},
}

// IntegrationTester validates stored integration credentials.
type IntegrationTester interface {
	TestConnection(ctx context.Context, kind string, baseURL *string, secret []byte) error
}

// IntegrationHandlers serves integration HTTP endpoints.
type IntegrationHandlers struct {
	Integrations store.IntegrationRepository
	Tester       IntegrationTester
}

type integrationResponse struct {
	ID        string  `json:"id"`
	Kind      string  `json:"kind"`
	Name      string  `json:"name"`
	BaseURL   *string `json:"baseUrl"`
	HasSecret bool    `json:"hasSecret"`
	CreatedAt string  `json:"createdAt"`
	UpdatedAt string  `json:"updatedAt"`
}

type createIntegrationRequest struct {
	Kind    string  `json:"kind"`
	Name    string  `json:"name"`
	BaseURL *string `json:"baseUrl"`
	Secret  string  `json:"secret"`
}

type patchIntegrationRequest struct {
	Name    string  `json:"name"`
	BaseURL *string `json:"baseUrl"`
	Secret  *string `json:"secret"`
}

type testConnectionResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
}

// List handles GET /api/v1/integrations.
func (h *IntegrationHandlers) List(w http.ResponseWriter, r *http.Request) {
	items, err := h.Integrations.List(r.Context())
	if err != nil {
		auth.WriteError(w, "INTERNAL_ERROR", "failed to list integrations", http.StatusInternalServerError)
		return
	}

	resp := make([]integrationResponse, len(items))
	for i := range items {
		resp[i] = integrationFromStore(&items[i])
	}

	writeIntegrationsJSON(w, http.StatusOK, resp)
}

// Create handles POST /api/v1/integrations.
func (h *IntegrationHandlers) Create(w http.ResponseWriter, r *http.Request) {
	var req createIntegrationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		auth.WriteError(w, "VALIDATION_ERROR", "invalid JSON body", http.StatusBadRequest)
		return
	}

	if err := validateIntegrationKind(req.Kind); err != nil {
		auth.WriteError(w, "VALIDATION_ERROR", err.Error(), http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		auth.WriteError(w, "VALIDATION_ERROR", "name is required", http.StatusBadRequest)
		return
	}
	if req.Secret == "" {
		auth.WriteError(w, "VALIDATION_ERROR", "secret is required", http.StatusBadRequest)
		return
	}
	if err := validateBaseURL(req.Kind, req.BaseURL); err != nil {
		auth.WriteError(w, "VALIDATION_ERROR", err.Error(), http.StatusBadRequest)
		return
	}

	created, err := h.Integrations.Create(r.Context(), store.CreateIntegrationInput{
		Kind:    req.Kind,
		Name:    req.Name,
		BaseURL: req.BaseURL,
		Secret:  []byte(req.Secret),
	})
	if err != nil {
		auth.WriteError(w, "INTERNAL_ERROR", "failed to create integration", http.StatusInternalServerError)
		return
	}

	writeIntegrationsJSON(w, http.StatusCreated, integrationFromStore(created))
}

// Patch handles PATCH /api/v1/integrations/{id}.
func (h *IntegrationHandlers) Patch(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		auth.WriteError(w, "VALIDATION_ERROR", "integration id is required", http.StatusBadRequest)
		return
	}

	var req patchIntegrationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		auth.WriteError(w, "VALIDATION_ERROR", "invalid JSON body", http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		auth.WriteError(w, "VALIDATION_ERROR", "name is required", http.StatusBadRequest)
		return
	}

	existing, err := h.Integrations.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			auth.WriteError(w, "NOT_FOUND", "integration not found", http.StatusNotFound)
			return
		}
		auth.WriteError(w, "INTERNAL_ERROR", "failed to load integration", http.StatusInternalServerError)
		return
	}

	baseURL := req.BaseURL
	if baseURL == nil {
		baseURL = existing.BaseURL
	}
	if err := validateBaseURL(existing.Kind, baseURL); err != nil {
		auth.WriteError(w, "VALIDATION_ERROR", err.Error(), http.StatusBadRequest)
		return
	}

	input := store.UpdateIntegrationInput{
		Name:    req.Name,
		BaseURL: baseURL,
	}
	if req.Secret != nil {
		if *req.Secret == "" {
			auth.WriteError(w, "VALIDATION_ERROR", "secret cannot be empty", http.StatusBadRequest)
			return
		}
		secret := []byte(*req.Secret)
		input.Secret = secret
	}

	updated, err := h.Integrations.Update(r.Context(), id, input)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			auth.WriteError(w, "NOT_FOUND", "integration not found", http.StatusNotFound)
			return
		}
		auth.WriteError(w, "INTERNAL_ERROR", "failed to update integration", http.StatusInternalServerError)
		return
	}

	writeIntegrationsJSON(w, http.StatusOK, integrationFromStore(updated))
}

// Delete handles DELETE /api/v1/integrations/{id}.
func (h *IntegrationHandlers) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		auth.WriteError(w, "VALIDATION_ERROR", "integration id is required", http.StatusBadRequest)
		return
	}

	if _, err := h.Integrations.Get(r.Context(), id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			auth.WriteError(w, "NOT_FOUND", "integration not found", http.StatusNotFound)
			return
		}
		auth.WriteError(w, "INTERNAL_ERROR", "failed to load integration", http.StatusInternalServerError)
		return
	}

	refs, err := h.Integrations.CountReferences(r.Context(), id)
	if err != nil {
		auth.WriteError(w, "INTERNAL_ERROR", "failed to check integration references", http.StatusInternalServerError)
		return
	}
	if refs > 0 {
		auth.WriteError(w, "CONFLICT", "integration is referenced by repos or ticket projects", http.StatusConflict)
		return
	}

	if err := h.Integrations.Delete(r.Context(), id); err != nil {
		auth.WriteError(w, "INTERNAL_ERROR", "failed to delete integration", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// TestConnection handles POST /api/v1/integrations/{id}/test.
func (h *IntegrationHandlers) TestConnection(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		auth.WriteError(w, "VALIDATION_ERROR", "integration id is required", http.StatusBadRequest)
		return
	}
	if h.Tester == nil {
		auth.WriteError(w, "INTERNAL_ERROR", "integration tester not configured", http.StatusInternalServerError)
		return
	}

	integration, err := h.Integrations.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			auth.WriteError(w, "NOT_FOUND", "integration not found", http.StatusNotFound)
			return
		}
		auth.WriteError(w, "INTERNAL_ERROR", "failed to load integration", http.StatusInternalServerError)
		return
	}

	secret, err := h.Integrations.DecryptPayload(r.Context(), id)
	if err != nil {
		auth.WriteError(w, "INTERNAL_ERROR", "failed to decrypt integration credentials", http.StatusInternalServerError)
		return
	}

	if err := h.Tester.TestConnection(r.Context(), integration.Kind, integration.BaseURL, secret); err != nil {
		writeIntegrationsJSON(w, http.StatusOK, testConnectionResponse{
			Success: false,
			Message: err.Error(),
		})
		return
	}

	writeIntegrationsJSON(w, http.StatusOK, testConnectionResponse{Success: true})
}

func integrationFromStore(item *store.Integration) integrationResponse {
	return integrationResponse{
		ID:        item.ID,
		Kind:      item.Kind,
		Name:      item.Name,
		BaseURL:   item.BaseURL,
		HasSecret: item.HasSecret,
		CreatedAt: item.CreatedAt,
		UpdatedAt: item.UpdatedAt,
	}
}

func validateIntegrationKind(kind string) error {
	if _, ok := validIntegrationKinds[kind]; !ok {
		return errors.New("kind must be one of: github, gitlab, gitea, forgejo, codeberg, phasical, jira, linear")
	}
	return nil
}

func validateBaseURL(kind string, baseURL *string) error {
	requiresURL := kind == "gitlab" || kind == "gitea" || kind == "forgejo" || kind == "phasical" || kind == "jira"
	hasURL := baseURL != nil && *baseURL != ""
	switch {
	case requiresURL && !hasURL:
		return errors.New("baseUrl is required for this integration kind")
	case !requiresURL && hasURL:
		return errors.New("baseUrl must not be set for this integration kind")
	default:
		return nil
	}
}

func writeIntegrationsJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
